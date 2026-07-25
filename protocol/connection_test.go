package protocol

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"errors"
	"net"
	"strings"
	"testing"

	"github.com/dector/adb-go/auth"
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
			Arg0:    AuthToken,
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

func TestConnectionHandshakeWithOptionsSignsAuthToken(t *testing.T) {
	key := generateTestRSAKey(t)
	cred, err := auth.NewCredential(key)
	if err != nil {
		t.Fatalf("NewCredential: %v", err)
	}
	token := []byte("challenge token")

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	serverErr := make(chan error, 1)
	go func() {
		if _, err := ReadMessage(serverConn); err != nil {
			serverErr <- err
			return
		}
		if err := WriteMessage(serverConn, Message{Command: CommandAUTH, Arg0: AuthToken, Payload: token}); err != nil {
			serverErr <- err
			return
		}
		response, err := ReadMessage(serverConn)
		if err != nil {
			serverErr <- err
			return
		}
		if response.Command != CommandAUTH || response.Arg0 != AuthSignature {
			serverErr <- errors.New("client did not send AUTH SIGNATURE")
			return
		}
		digest := sha1.Sum(token)
		if err := rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA1, digest[:], response.Payload); err != nil {
			serverErr <- err
			return
		}
		serverErr <- WriteMessage(serverConn, Message{Command: CommandCNXN, Arg0: Version, Arg1: MaxPayload, Payload: []byte("device::auth\x00")})
	}()

	response, err := NewConnection(clientConn).HandshakeWithOptions(context.Background(), HandshakeOptions{AuthCredentials: []AuthCredential{cred}})
	if err != nil {
		t.Fatalf("HandshakeWithOptions() error = %v", err)
	}
	if string(response.Payload) != "device::auth\x00" {
		t.Fatalf("HandshakeWithOptions() payload = %q", response.Payload)
	}
	if err := <-serverErr; err != nil {
		t.Fatalf("fake peer error = %v", err)
	}
}

func TestConnectionHandshakeWithOptionsOffersPublicKeyAfterSignatures(t *testing.T) {
	key := generateTestRSAKey(t)
	cred, err := auth.NewCredentialWithComment(key, "unit@test")
	if err != nil {
		t.Fatalf("NewCredentialWithComment: %v", err)
	}
	token := []byte("authorize me")

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	serverErr := make(chan error, 1)
	go func() {
		if _, err := ReadMessage(serverConn); err != nil {
			serverErr <- err
			return
		}
		if err := WriteMessage(serverConn, Message{Command: CommandAUTH, Arg0: AuthToken, Payload: token}); err != nil {
			serverErr <- err
			return
		}
		if _, err := ReadMessage(serverConn); err != nil { // signature attempt
			serverErr <- err
			return
		}
		if err := WriteMessage(serverConn, Message{Command: CommandAUTH, Arg0: AuthToken, Payload: token}); err != nil {
			serverErr <- err
			return
		}
		response, err := ReadMessage(serverConn)
		if err != nil {
			serverErr <- err
			return
		}
		if response.Command != CommandAUTH || response.Arg0 != AuthRSAPublicKey || !bytesEqual(response.Payload, cred.PublicKeyPayload()) {
			serverErr <- errors.New("client did not send expected AUTH RSAPUBLICKEY")
			return
		}
		serverErr <- WriteMessage(serverConn, Message{Command: CommandCNXN, Arg0: Version, Arg1: MaxPayload, Payload: []byte("device::authorized\x00")})
	}()

	response, err := NewConnection(clientConn).HandshakeWithOptions(context.Background(), HandshakeOptions{AuthCredentials: []AuthCredential{cred}})
	if err != nil {
		t.Fatalf("HandshakeWithOptions() error = %v", err)
	}
	if string(response.Payload) != "device::authorized\x00" {
		t.Fatalf("HandshakeWithOptions() payload = %q", response.Payload)
	}
	if err := <-serverErr; err != nil {
		t.Fatalf("fake peer error = %v", err)
	}
}

func TestConnectionHandshakeWithOptionsReturnsAuthRequiredAfterCredentialRejection(t *testing.T) {
	key := generateTestRSAKey(t)
	cred, err := auth.NewCredential(key)
	if err != nil {
		t.Fatalf("NewCredential: %v", err)
	}
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	go func() {
		_, _ = ReadMessage(serverConn)
		_ = WriteMessage(serverConn, Message{Command: CommandAUTH, Arg0: AuthToken, Payload: []byte("one")})
		_, _ = ReadMessage(serverConn) // signature
		_ = WriteMessage(serverConn, Message{Command: CommandAUTH, Arg0: AuthToken, Payload: []byte("two")})
		_, _ = ReadMessage(serverConn) // public key
		_ = WriteMessage(serverConn, Message{Command: CommandAUTH, Arg0: AuthToken, Payload: []byte("three")})
	}()

	_, err = NewConnection(clientConn).HandshakeWithOptions(context.Background(), HandshakeOptions{AuthCredentials: []AuthCredential{cred}})
	if !errors.Is(err, ErrAuthRequired) {
		t.Fatalf("HandshakeWithOptions() error = %v, want ErrAuthRequired", err)
	}
}

func TestConnectionHandshakeWithOptionsReturnsAuthRequiredForMalformedAUTH(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	go func() {
		_, _ = ReadMessage(serverConn)
		_ = WriteMessage(serverConn, Message{Command: CommandAUTH, Arg0: 99, Payload: []byte("bad")})
	}()

	_, err := NewConnection(clientConn).HandshakeWithOptions(context.Background(), HandshakeOptions{})
	if !errors.Is(err, ErrAuthRequired) {
		t.Fatalf("HandshakeWithOptions() error = %v, want ErrAuthRequired", err)
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

func generateTestRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	return key
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
