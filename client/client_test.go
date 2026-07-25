package client

import (
	"bytes"
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
	server.Handle("test:service", writeServiceOutput(t, "hello"))

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

func TestShellCapturesOutputAgainstFakeServer(t *testing.T) {
	server := fakeadb.Start(t)
	server.Handle("shell:printf hello", writeServiceOutput(t, "hello\n"))

	client, err := Connect(context.Background(), server.Addr())
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer client.Close()

	out, err := client.Shell(context.Background(), "printf hello")
	if err != nil {
		t.Fatalf("Shell() error = %v", err)
	}
	if string(out) != "hello\n" {
		t.Fatalf("Shell() output = %q, want hello newline", string(out))
	}
}

func TestShellStreamWritesToProvidedWriter(t *testing.T) {
	server := fakeadb.Start(t)
	server.Handle("shell:echo streamed", writeServiceOutput(t, "streamed\n"))

	client, err := Connect(context.Background(), server.Addr())
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer client.Close()

	var out bytes.Buffer
	if err := client.ShellStream(context.Background(), "echo streamed", &out); err != nil {
		t.Fatalf("ShellStream() error = %v", err)
	}
	if out.String() != "streamed\n" {
		t.Fatalf("ShellStream() output = %q, want streamed newline", out.String())
	}
}

func TestShellUsesExactSingleCommandString(t *testing.T) {
	server := fakeadb.Start(t)
	opened := make(chan string, 1)
	wantService := "shell:pm list packages | grep example"
	server.Handle(wantService, func(ctx context.Context, conn io.ReadWriter, open protocol.Message) {
		opened <- string(open.Payload[:len(open.Payload)-1])
		writeServiceOutput(t, "package:example\n")(ctx, conn, open)
	})

	client, err := Connect(context.Background(), server.Addr())
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer client.Close()

	if _, err := client.Shell(context.Background(), "pm list packages | grep example"); err != nil {
		t.Fatalf("Shell() error = %v", err)
	}
	if got := <-opened; got != wantService {
		t.Fatalf("opened service = %q, want %q", got, wantService)
	}
}

func TestShellStreamContextCancellationUnblocks(t *testing.T) {
	server := fakeadb.Start(t)
	server.Handle("shell:sleep forever", func(ctx context.Context, conn io.ReadWriter, open protocol.Message) {
		_ = protocol.WriteMessage(conn, protocol.Message{Command: protocol.CommandOKAY, Arg0: 42, Arg1: open.Arg0})
		<-ctx.Done()
	})

	client, err := Connect(context.Background(), server.Addr())
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- client.ShellStream(ctx, "sleep forever", io.Discard)
	}()

	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("ShellStream() error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("ShellStream() did not unblock after context cancellation")
	}
}

func writeServiceOutput(t testing.TB, output string) fakeadb.ServiceHandler {
	t.Helper()
	return func(ctx context.Context, conn io.ReadWriter, open protocol.Message) {
		remoteID := uint32(42)
		_ = protocol.WriteMessage(conn, protocol.Message{Command: protocol.CommandOKAY, Arg0: remoteID, Arg1: open.Arg0})
		_ = protocol.WriteMessage(conn, protocol.Message{Command: protocol.CommandWRTE, Arg0: remoteID, Arg1: open.Arg0, Payload: []byte(output)})
		_ = protocol.WriteMessage(conn, protocol.Message{Command: protocol.CommandCLSE, Arg0: remoteID, Arg1: open.Arg0})
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
