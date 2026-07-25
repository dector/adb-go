package fakeadb

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"

	"github.com/dector/adb-go/protocol"
)

const deviceIdentityString = "device::fake"

// ServiceHandler handles a service opened by the ADB client.
//
// The OPEN message is provided so tests can inspect raw stream identifiers. A
// handler may use conn to exchange raw protocol messages until higher-level
// fake stream helpers are added.
type ServiceHandler func(ctx context.Context, conn io.ReadWriter, open protocol.Message)

type authRequirement struct {
	enabled           bool
	token             []byte
	acceptedSignature []byte
	acceptedPublicKey []byte
}

func (a authRequirement) accepts(msg protocol.Message) bool {
	switch msg.Arg0 {
	case protocol.AuthSignature:
		return len(a.acceptedSignature) > 0 && bytes.Equal(msg.Payload, a.acceptedSignature)
	case protocol.AuthRSAPublicKey:
		return len(a.acceptedPublicKey) > 0 && bytes.Equal(msg.Payload, a.acceptedPublicKey)
	default:
		return false
	}
}

// Server is an in-process fake ADB TCP server for tests.
type Server struct {
	ln net.Listener

	ctx    context.Context
	cancel context.CancelFunc

	mu       sync.Mutex
	handlers map[string]ServiceHandler
	conns    map[net.Conn]struct{}
	auth     authRequirement

	wg sync.WaitGroup
}

// Listen starts a fake ADB server listening on addr. Use "127.0.0.1:0" to pick
// an available local TCP port.
func Listen(addr string) (*Server, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{
		ln:       ln,
		ctx:      ctx,
		cancel:   cancel,
		handlers: make(map[string]ServiceHandler),
		conns:    make(map[net.Conn]struct{}),
	}
	s.wg.Add(1)
	go s.serve()
	return s, nil
}

// Start starts a fake ADB server for a test and registers cleanup with t.
func Start(t testing.TB) *Server {
	t.Helper()
	s, err := Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("start fake adb server: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("close fake adb server: %v", err)
		}
	})
	return s
}

// Addr returns the server's listen address.
func (s *Server) Addr() string {
	return s.ln.Addr().String()
}

// Handle registers h for service. Service names are the OPEN payload without
// the trailing NUL, for example "shell:echo ok" or "sync:".
func (s *Server) Handle(service string, h ServiceHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[service] = h
}

// RequireAuth configures the fake server to require ADB AUTH before completing
// CNXN. A client response is accepted when it matches either acceptedSignature
// as AUTH SIGNATURE or acceptedPublicKey as AUTH RSAPUBLICKEY. Nil accepted
// values are ignored.
func (s *Server) RequireAuth(token, acceptedSignature, acceptedPublicKey []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.auth = authRequirement{
		enabled:           true,
		token:             append([]byte(nil), token...),
		acceptedSignature: append([]byte(nil), acceptedSignature...),
		acceptedPublicKey: append([]byte(nil), acceptedPublicKey...),
	}
}

// Close stops the server, closes active connections, and waits for goroutines
// to exit.
func (s *Server) Close() error {
	s.cancel()
	err := s.ln.Close()

	s.mu.Lock()
	for conn := range s.conns {
		_ = conn.Close()
	}
	s.mu.Unlock()

	s.wg.Wait()
	if err != nil && !errors.Is(err, net.ErrClosed) {
		return err
	}
	return nil
}

func (s *Server) serve() {
	defer s.wg.Done()
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) || s.ctx.Err() != nil {
				return
			}
			continue
		}
		s.track(conn)
		s.wg.Add(1)
		go s.handleConn(conn)
	}
}

func (s *Server) handleConn(conn net.Conn) {
	defer s.wg.Done()
	defer s.untrack(conn)
	defer conn.Close()

	if err := s.handshake(conn); err != nil {
		return
	}

	for {
		msg, err := protocol.ReadMessage(conn)
		if err != nil {
			return
		}
		if msg.Command != protocol.CommandOPEN {
			continue
		}

		service := strings.TrimSuffix(string(msg.Payload), "\x00")
		handler := s.handler(service)
		if handler == nil {
			_ = protocol.WriteMessage(conn, protocol.Message{
				Command: protocol.CommandCLSE,
				Arg1:    msg.Arg0,
			})
			continue
		}
		handler(s.ctx, conn, msg)
	}
}

func (s *Server) handshake(conn net.Conn) error {
	request, err := protocol.ReadMessage(conn)
	if err != nil {
		return err
	}
	if request.Command != protocol.CommandCNXN {
		return fmt.Errorf("fake adb expected CNXN, got %#x", uint32(request.Command))
	}

	auth := s.authRequirement()
	if auth.enabled {
		if err := protocol.WriteMessage(conn, protocol.Message{Command: protocol.CommandAUTH, Arg0: protocol.AuthToken, Payload: auth.token}); err != nil {
			return err
		}
		response, err := protocol.ReadMessage(conn)
		if err != nil {
			return err
		}
		if !auth.accepts(response) {
			return protocol.WriteMessage(conn, protocol.Message{Command: protocol.CommandAUTH, Arg0: protocol.AuthToken, Payload: auth.token})
		}
	}

	return protocol.WriteMessage(conn, protocol.Message{
		Command: protocol.CommandCNXN,
		Arg0:    protocol.Version,
		Arg1:    protocol.MaxPayload,
		Payload: []byte(deviceIdentityString + "\x00"),
	})
}

func (s *Server) authRequirement() authRequirement {
	s.mu.Lock()
	defer s.mu.Unlock()
	return authRequirement{
		enabled:           s.auth.enabled,
		token:             append([]byte(nil), s.auth.token...),
		acceptedSignature: append([]byte(nil), s.auth.acceptedSignature...),
		acceptedPublicKey: append([]byte(nil), s.auth.acceptedPublicKey...),
	}
}

func (s *Server) handler(service string) ServiceHandler {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.handlers[service]
}

func (s *Server) track(conn net.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.conns[conn] = struct{}{}
}

func (s *Server) untrack(conn net.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.conns, conn)
}
