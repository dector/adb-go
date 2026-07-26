package client

import (
	"context"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"github.com/dector/adb-go/internal/fakeadb"
	"github.com/dector/adb-go/protocol"
)

func TestForwardTCPValidatesPort(t *testing.T) {
	for _, port := range []int{0, -1, 65536} {
		if _, err := ForwardTCP(port); err == nil {
			t.Fatalf("ForwardTCP(%d) error = nil, want error", port)
		}
	}

	target, err := ForwardTCP(4321)
	if err != nil {
		t.Fatalf("ForwardTCP(4321) error = %v", err)
	}
	if target.service != "tcp:4321" {
		t.Fatalf("ForwardTCP(4321) service = %q, want tcp:4321", target.service)
	}
}

func TestForwardLocalTCPBridgesConnectionToRemoteTCPService(t *testing.T) {
	server := fakeadb.Start(t)
	server.Handle("tcp:4321", echoADBService(t))

	client, err := Connect(context.Background(), server.Addr())
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer client.Close()

	target, err := ForwardTCP(4321)
	if err != nil {
		t.Fatalf("ForwardTCP() error = %v", err)
	}
	forward, err := client.ForwardLocalTCP(context.Background(), "127.0.0.1:0", target)
	if err != nil {
		t.Fatalf("ForwardLocalTCP() error = %v", err)
	}
	defer forward.Close()

	conn, err := net.Dial("tcp", forward.LocalAddr().String())
	if err != nil {
		t.Fatalf("Dial(forward.LocalAddr()) error = %v", err)
	}
	defer conn.Close()

	if _, err := conn.Write([]byte("ping")); err != nil {
		t.Fatalf("local Write() error = %v", err)
	}
	buf := make([]byte, len("echo:ping"))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("local ReadFull() error = %v", err)
	}
	if string(buf) != "echo:ping" {
		t.Fatalf("forwarded response = %q, want echo:ping", string(buf))
	}

	if err := conn.Close(); err != nil {
		t.Fatalf("local Close() error = %v", err)
	}
	if err := forward.Close(); err != nil {
		t.Fatalf("Forward.Close() error = %v", err)
	}
	if err := waitForward(t, forward); err != nil {
		t.Fatalf("Forward.Wait() error = %v", err)
	}
}

func TestForwardLocalTCPCloseStopsListener(t *testing.T) {
	server := fakeadb.Start(t)
	client, err := Connect(context.Background(), server.Addr())
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer client.Close()

	target, err := ForwardTCP(4321)
	if err != nil {
		t.Fatalf("ForwardTCP() error = %v", err)
	}
	forward, err := client.ForwardLocalTCP(context.Background(), "127.0.0.1:0", target)
	if err != nil {
		t.Fatalf("ForwardLocalTCP() error = %v", err)
	}
	addr := forward.LocalAddr().String()

	if err := forward.Close(); err != nil {
		t.Fatalf("Forward.Close() error = %v", err)
	}
	if err := waitForward(t, forward); err != nil {
		t.Fatalf("Forward.Wait() error = %v", err)
	}

	conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
	if err == nil {
		_ = conn.Close()
		t.Fatal("Dial() after Forward.Close() succeeded, want listener stopped")
	}
}

func echoADBService(t testing.TB) fakeadb.ServiceHandler {
	t.Helper()
	return func(ctx context.Context, conn io.ReadWriter, open protocol.Message) {
		remoteID := uint32(42)
		if err := protocol.WriteMessage(conn, protocol.Message{Command: protocol.CommandOKAY, Arg0: remoteID, Arg1: open.Arg0}); err != nil {
			t.Errorf("echo service OKAY error = %v", err)
			return
		}

		request, err := protocol.ReadMessage(conn)
		if err != nil {
			t.Errorf("echo service ReadMessage() error = %v", err)
			return
		}
		if request.Command != protocol.CommandWRTE {
			t.Errorf("echo service command = %#x, want WRTE", uint32(request.Command))
			return
		}
		if err := protocol.WriteMessage(conn, protocol.Message{Command: protocol.CommandOKAY, Arg0: remoteID, Arg1: open.Arg0}); err != nil {
			t.Errorf("echo service request OKAY error = %v", err)
			return
		}
		if err := protocol.WriteMessage(conn, protocol.Message{Command: protocol.CommandWRTE, Arg0: remoteID, Arg1: open.Arg0, Payload: []byte("echo:" + string(request.Payload))}); err != nil {
			t.Errorf("echo service WRTE error = %v", err)
			return
		}
	}
}

func waitForward(t testing.TB, forward *Forward) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- forward.Wait() }()
	select {
	case err := <-done:
		return err
	case <-time.After(2 * time.Second):
		return fmt.Errorf("forward did not stop")
	}
}
