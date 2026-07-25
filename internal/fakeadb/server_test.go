package fakeadb

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"io"
	"net"
	"testing"
	"time"

	"github.com/dector/adb-go/auth"
	"github.com/dector/adb-go/protocol"
)

func TestServerAcceptsConnectionAndHandshakes(t *testing.T) {
	s := Start(t)

	conn, err := net.Dial("tcp", s.Addr())
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer conn.Close()

	response, err := protocol.NewConnection(conn).Handshake(context.Background())
	if err != nil {
		t.Fatalf("Handshake() error = %v", err)
	}
	if response.Command != protocol.CommandCNXN || string(response.Payload) != deviceIdentityString+"\x00" {
		t.Fatalf("Handshake() = %#v, want fake device CNXN", response)
	}
}

func TestServerCanRequireAuthBeforeHandshake(t *testing.T) {
	s := Start(t)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	cred, err := auth.NewCredential(key)
	if err != nil {
		t.Fatalf("NewCredential: %v", err)
	}
	token := []byte("fake token")
	digest := sha1.Sum(token)
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA1, digest[:])
	if err != nil {
		t.Fatalf("SignPKCS1v15: %v", err)
	}
	s.RequireAuth(token, signature, nil)

	conn, err := net.Dial("tcp", s.Addr())
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer conn.Close()

	response, err := protocol.NewConnection(conn).HandshakeWithOptions(context.Background(), protocol.HandshakeOptions{AuthCredentials: []protocol.AuthCredential{cred}})
	if err != nil {
		t.Fatalf("HandshakeWithOptions() error = %v", err)
	}
	if response.Command != protocol.CommandCNXN || string(response.Payload) != deviceIdentityString+"\x00" {
		t.Fatalf("HandshakeWithOptions() = %#v, want fake device CNXN", response)
	}
}

func TestServerDispatchesRegisteredServiceHandler(t *testing.T) {
	s := Start(t)
	handled := make(chan protocol.Message, 1)
	s.Handle("shell:echo ok", func(ctx context.Context, conn io.ReadWriter, open protocol.Message) { handled <- open })

	conn, err := net.Dial("tcp", s.Addr())
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer conn.Close()

	if _, err := protocol.NewConnection(conn).Handshake(context.Background()); err != nil {
		t.Fatalf("Handshake() error = %v", err)
	}
	if err := protocol.WriteMessage(conn, protocol.Message{
		Command: protocol.CommandOPEN,
		Arg0:    42,
		Payload: []byte("shell:echo ok\x00"),
	}); err != nil {
		t.Fatalf("WriteMessage(OPEN) error = %v", err)
	}

	select {
	case msg := <-handled:
		if msg.Arg0 != 42 || string(msg.Payload) != "shell:echo ok\x00" {
			t.Fatalf("handled OPEN = %#v, want registered service OPEN", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("registered service handler was not called")
	}
}

func TestServerShutsDownCleanly(t *testing.T) {
	s := Start(t)
	conn, err := net.Dial("tcp", s.Addr())
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer conn.Close()

	if err := s.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if _, err := net.DialTimeout("tcp", s.Addr(), 100*time.Millisecond); err == nil {
		t.Fatal("DialTimeout() after Close succeeded, want error")
	}
}
