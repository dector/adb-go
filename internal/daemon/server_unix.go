//go:build !windows

package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var ErrAlreadyRunning = errors.New("adb-god daemon already running")

type Options struct {
	SocketPath string
}

type Server struct {
	socketPath string
	listener   net.Listener
	start      time.Time
	forwards   *forwardRegistry

	shutdownOnce sync.Once
	done         chan struct{}
}

func NewServer(opts Options) (*Server, error) {
	path := opts.SocketPath
	if path == "" {
		var err error
		path, err = DefaultSocketPath()
		if err != nil {
			return nil, err
		}
	}
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("daemon socket path must be absolute")
	}
	return &Server{socketPath: path, forwards: newForwardRegistry(), done: make(chan struct{})}, nil
}

func (s *Server) SocketPath() string { return s.socketPath }

func (s *Server) Listen() error {
	if err := os.MkdirAll(filepath.Dir(s.socketPath), 0o700); err != nil {
		return fmt.Errorf("create daemon socket directory: %w", err)
	}
	if err := s.prepareSocketPath(); err != nil {
		return err
	}
	ln, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return fmt.Errorf("listen on daemon socket %q: %w", s.socketPath, err)
	}
	s.listener = ln
	s.start = time.Now()
	return nil
}

func (s *Server) Serve(ctx context.Context) error {
	if s.listener == nil {
		return fmt.Errorf("daemon server is not listening")
	}
	go func() {
		<-ctx.Done()
		_ = s.Shutdown()
	}()
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.done:
				return nil
			default:
			}
			return fmt.Errorf("accept daemon connection: %w", err)
		}
		go s.handleConn(conn)
	}
}

func (s *Server) Done() <-chan struct{} { return s.done }

func (s *Server) Shutdown() error {
	var err error
	s.shutdownOnce.Do(func() {
		close(s.done)
		if s.listener != nil {
			err = s.listener.Close()
		}
		if s.forwards != nil {
			s.forwards.close()
		}
		if stat, statErr := os.Lstat(s.socketPath); statErr == nil && stat.Mode()&os.ModeSocket != 0 {
			if removeErr := os.Remove(s.socketPath); removeErr != nil && err == nil {
				err = removeErr
			}
		}
	})
	return err
}

func (s *Server) prepareSocketPath() error {
	stat, err := os.Lstat(s.socketPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("inspect daemon socket path: %w", err)
	}
	if stat.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("daemon socket path %q exists and is not a Unix socket", s.socketPath)
	}
	ctx, cancel := probeContext()
	defer cancel()
	resp, err := Ping(ctx, s.socketPath)
	if err == nil && resp.Version == ProtocolVersion {
		return fmt.Errorf("%w at %s", ErrAlreadyRunning, s.socketPath)
	}
	if err == nil {
		return fmt.Errorf("daemon socket path %q is in use by an incompatible listener", s.socketPath)
	}
	if removeErr := os.Remove(s.socketPath); removeErr != nil {
		return fmt.Errorf("remove stale daemon socket %q: %w", s.socketPath, removeErr)
	}
	return nil
}

func (s *Server) handleConn(conn net.Conn) {
	defer conn.Close()
	var req Request
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		_ = json.NewEncoder(conn).Encode(Response{Version: ProtocolVersion, OK: false, Error: &Error{Code: ErrorBadRequest, Message: "malformed JSON request"}})
		return
	}
	resp := s.handleRequest(req)
	_ = json.NewEncoder(conn).Encode(resp)
	if req.Version == ProtocolVersion && req.Command == CommandShutdown && resp.OK {
		go func() { _ = s.Shutdown() }()
	}
}

func (s *Server) handleRequest(req Request) Response {
	resp := Response{Version: ProtocolVersion, ID: req.ID}
	if req.Command == "" {
		resp.Error = &Error{Code: ErrorBadRequest, Message: "missing required command field"}
		return resp
	}
	if req.Version != ProtocolVersion {
		resp.Error = &Error{Code: ErrorUnsupportedVersion, Message: fmt.Sprintf("unsupported daemon protocol version %d", req.Version)}
		return resp
	}
	switch req.Command {
	case CommandPing:
		resp.OK = true
		resp.Result = map[string]any{"message": "pong"}
	case CommandStatus:
		resp.OK = true
		forwardDiagnostics := s.forwards.diagnostics()
		resp.Result = map[string]any{
			"state":              "running",
			"pid":                os.Getpid(),
			"socketPath":         s.socketPath,
			"protocolVersion":    ProtocolVersion,
			"uptimeMillis":       time.Since(s.start).Milliseconds(),
			"forwardDiagnostics": forwardDiagnostics,
		}
	case CommandShutdown:
		resp.OK = true
		resp.Result = map[string]any{"message": "shutting_down"}
	case CommandForwardCreate:
		params, errResp := decodeForwardCreateParams(req.Params)
		if errResp != nil {
			resp.Error = errResp
			break
		}
		forward, forwardErr := s.forwards.create(params)
		if forwardErr != nil {
			resp.Error = forwardErr
			break
		}
		resp.OK = true
		resp.Result = map[string]any{"forward": forward}
	case CommandForwardList:
		resp.OK = true
		resp.Result = map[string]any{"forwards": s.forwards.list()}
	case CommandForwardRemove:
		params, errResp := decodeForwardRemoveParams(req.Params)
		if errResp != nil {
			resp.Error = errResp
			break
		}
		removed, forwardErr := s.forwards.remove(params)
		if forwardErr != nil {
			resp.Error = forwardErr
			break
		}
		resp.OK = true
		resp.Result = map[string]any{"removed": removed}
	case CommandForwardRemoveAll:
		resp.OK = true
		resp.Result = map[string]any{"removed": s.forwards.removeAll()}
	default:
		resp.Error = &Error{Code: ErrorUnknownCommand, Message: fmt.Sprintf("unknown daemon command %q", req.Command)}
	}
	return resp
}
