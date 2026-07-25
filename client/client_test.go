package client

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/dector/adb-go/internal/fakeadb"
	"github.com/dector/adb-go/protocol"
)

func TestNormalizeTCPAddr(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "host without port", in: "127.0.0.1", want: "127.0.0.1:5555"},
		{name: "host with port", in: "127.0.0.1:1234", want: "127.0.0.1:1234"},
		{name: "hostname without port", in: "localhost", want: "localhost:5555"},
		{name: "ipv6 without port", in: "::1", want: "[::1]:5555"},
		{name: "bracketed ipv6 with port", in: "[::1]:1234", want: "[::1]:1234"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeTCPAddr(tt.in)
			if err != nil {
				t.Fatalf("normalizeTCPAddr() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("normalizeTCPAddr() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNormalizeTCPAddrEmpty(t *testing.T) {
	if _, err := normalizeTCPAddr("   "); err == nil {
		t.Fatal("normalizeTCPAddr(empty) error = nil, want error")
	}
}

func TestConnectSucceedsAgainstFakeServer(t *testing.T) {
	server := fakeadb.Start(t)

	client, err := Connect(context.Background(), server.Addr())
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestConnectAuthResponse(t *testing.T) {
	addr, closeServer := startAuthServer(t)
	defer closeServer()

	client, err := Connect(context.Background(), addr)
	if err == nil {
		_ = client.Close()
		t.Fatal("Connect() error = nil, want auth required")
	}
	if !errors.Is(err, ErrAuthRequired) {
		t.Fatalf("Connect() error = %v, want errors.Is ErrAuthRequired", err)
	}
}

func TestOpenServiceAgainstFakeServer(t *testing.T) {
	server := fakeadb.Start(t)
	server.Handle("test:service", func(ctx context.Context, conn io.ReadWriter, open protocol.Message) {
		_ = protocol.WriteMessage(conn, protocol.Message{
			Command: protocol.CommandOKAY,
			Arg0:    42,
			Arg1:    open.Arg0,
		})
		_ = protocol.WriteMessage(conn, protocol.Message{
			Command: protocol.CommandWRTE,
			Arg0:    42,
			Arg1:    open.Arg0,
			Payload: []byte("hello"),
		})
	})

	client, err := Connect(context.Background(), server.Addr())
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer client.Close()

	stream, err := client.OpenService(context.Background(), "test:service")
	if err != nil {
		t.Fatalf("OpenService() error = %v", err)
	}
	defer stream.Close()

	buf := make([]byte, len("hello"))
	if _, err := io.ReadFull(stream, buf); err != nil {
		t.Fatalf("ReadFull() error = %v", err)
	}
	if string(buf) != "hello" {
		t.Fatalf("read payload = %q, want hello", string(buf))
	}
}

func startAuthServer(t *testing.T) (addr string, closeServer func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = protocol.ReadMessage(conn)
		_ = protocol.WriteMessage(conn, protocol.Message{
			Command: protocol.CommandAUTH,
			Arg0:    1,
			Payload: []byte("token"),
		})
	}()
	return ln.Addr().String(), func() {
		_ = ln.Close()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("auth server did not stop")
		}
	}
}
