package protocol

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
)

func TestConnectionHandshakeSucceeds(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	serverErr := make(chan error, 1)
	go func() {
		request, err := ReadMessage(serverConn)
		if err != nil {
			serverErr <- err
			return
		}
		if request.Command != CommandCNXN {
			serverErr <- errors.New("request was not CNXN")
			return
		}
		if request.Arg0 != Version || request.Arg1 != MaxPayload {
			serverErr <- errors.New("request advertised unexpected version or max payload")
			return
		}
		if string(request.Payload) != SystemIdentityString+"\x00" {
			serverErr <- errors.New("request advertised unexpected system identity")
			return
		}
		serverErr <- WriteMessage(serverConn, Message{
			Command: CommandCNXN,
			Arg0:    Version,
			Arg1:    MaxPayload,
			Payload: []byte("device::fake\x00"),
		})
	}()

	response, err := NewConnection(clientConn).Handshake(context.Background())
	if err != nil {
		t.Fatalf("Handshake() error = %v", err)
	}
	if response.Command != CommandCNXN || string(response.Payload) != "device::fake\x00" {
		t.Fatalf("Handshake() = %#v, want fake CNXN", response)
	}
	if err := <-serverErr; err != nil {
		t.Fatalf("fake peer error = %v", err)
	}
}

func TestConnectionHandshakeAuthRequired(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	go func() {
		_, _ = ReadMessage(serverConn)
		_ = WriteMessage(serverConn, Message{
			Command: CommandAUTH,
			Arg0:    1,
			Payload: []byte("token"),
		})
	}()

	_, err := NewConnection(clientConn).Handshake(context.Background())
	if !errors.Is(err, ErrAuthRequired) {
		t.Fatalf("Handshake() error = %v, want ErrAuthRequired", err)
	}
	var authErr *AuthRequiredError
	if !errors.As(err, &authErr) {
		t.Fatalf("Handshake() error = %T, want AuthRequiredError", err)
	}
	if authErr.Message.Command != CommandAUTH || string(authErr.Message.Payload) != "token" {
		t.Fatalf("AuthRequiredError.Message = %#v, want raw AUTH token", authErr.Message)
	}
}

func TestConnectionHandshakeUnexpectedResponse(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	go func() {
		_, _ = ReadMessage(serverConn)
		_ = WriteMessage(serverConn, Message{Command: CommandWRTE, Payload: []byte("nope")})
	}()

	_, err := NewConnection(clientConn).Handshake(context.Background())
	if err == nil || !strings.Contains(err.Error(), "unexpected response") {
		t.Fatalf("Handshake() error = %v, want unexpected response error", err)
	}
}
