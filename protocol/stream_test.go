package protocol_test

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

func TestOpenServiceSendsOPEN(t *testing.T) {
	server := fakeadb.Start(t)
	opened := make(chan protocol.Message, 1)
	server.Handle("echo:", func(ctx context.Context, conn io.ReadWriter, open protocol.Message) {
		opened <- open
		writeMessage(t, conn, protocol.Message{Command: protocol.CommandOKAY, Arg0: 42, Arg1: open.Arg0})
	})

	conn := connectFake(t, server.Addr())
	defer conn.Close()

	stream, err := conn.Open("echo:")
	if err != nil {
		t.Fatalf("open service: %v", err)
	}
	defer stream.Close()

	select {
	case open := <-opened:
		if open.Command != protocol.CommandOPEN {
			t.Fatalf("command = %#x, want OPEN", uint32(open.Command))
		}
		if got, want := string(open.Payload), "echo:\x00"; got != want {
			t.Fatalf("payload = %q, want %q", got, want)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for OPEN")
	}
}

func TestOpenServiceOKAYEstablishesStreamAndWriteSendsWRTE(t *testing.T) {
	server := fakeadb.Start(t)
	written := make(chan protocol.Message, 1)
	server.Handle("sink:", func(ctx context.Context, conn io.ReadWriter, open protocol.Message) {
		writeMessage(t, conn, protocol.Message{Command: protocol.CommandOKAY, Arg0: 7, Arg1: open.Arg0})
		msg, err := protocol.ReadMessage(conn)
		if err != nil {
			t.Errorf("read client write: %v", err)
			return
		}
		written <- msg
	})

	conn := connectFake(t, server.Addr())
	defer conn.Close()
	stream, err := conn.Open("sink:")
	if err != nil {
		t.Fatalf("open service: %v", err)
	}

	if n, err := stream.Write([]byte("hello")); err != nil || n != 5 {
		t.Fatalf("stream write = %d, %v; want 5, nil", n, err)
	}

	select {
	case msg := <-written:
		if msg.Command != protocol.CommandWRTE {
			t.Fatalf("command = %#x, want WRTE", uint32(msg.Command))
		}
		if msg.Arg1 != 7 {
			t.Fatalf("remote arg = %d, want 7", msg.Arg1)
		}
		if string(msg.Payload) != "hello" {
			t.Fatalf("payload = %q, want hello", msg.Payload)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for WRTE")
	}
}

func TestStreamReadReceivesWRTEFromCorrectStream(t *testing.T) {
	server := fakeadb.Start(t)
	server.Handle("one:", sendPayloadHandler(t, 11, "first"))
	server.Handle("two:", sendPayloadHandler(t, 22, "second"))

	conn := connectFake(t, server.Addr())
	defer conn.Close()

	one, err := conn.Open("one:")
	if err != nil {
		t.Fatalf("open one: %v", err)
	}
	two, err := conn.Open("two:")
	if err != nil {
		t.Fatalf("open two: %v", err)
	}

	assertReadAll(t, one, "first")
	assertReadAll(t, two, "second")
}

func TestPeerOPENIsAcceptedAndRoutedToHandler(t *testing.T) {
	clientConn, deviceConn := net.Pipe()
	defer clientConn.Close()
	defer deviceConn.Close()

	conn := protocol.NewConnection(clientConn)
	handled := make(chan error, 1)
	if err := conn.HandleOpen("tcp:8081", func(service string, stream *protocol.Stream) {
		if service != "tcp:8081" {
			handled <- errors.New("handler received unexpected service")
			return
		}
		buf := make([]byte, len("ping"))
		if _, err := io.ReadFull(stream, buf); err != nil {
			handled <- err
			return
		}
		if string(buf) != "ping" {
			handled <- errors.New("handler read unexpected payload")
			return
		}
		if _, err := stream.Write([]byte("pong")); err != nil {
			handled <- err
			return
		}
		handled <- stream.Close()
	}); err != nil {
		t.Fatalf("HandleOpen: %v", err)
	}

	writeMessage(t, deviceConn, protocol.Message{Command: protocol.CommandOPEN, Arg0: 99, Payload: []byte("tcp:8081\x00")})
	okay, err := protocol.ReadMessage(deviceConn)
	if err != nil {
		t.Fatalf("read OKAY: %v", err)
	}
	if okay.Command != protocol.CommandOKAY || okay.Arg1 != 99 || okay.Arg0 == 0 {
		t.Fatalf("OKAY = %#v, want host local id and device id 99", okay)
	}

	writeMessage(t, deviceConn, protocol.Message{Command: protocol.CommandWRTE, Arg0: 99, Arg1: okay.Arg0, Payload: []byte("ping")})
	ack, err := protocol.ReadMessage(deviceConn)
	if err != nil {
		t.Fatalf("read WRTE ACK: %v", err)
	}
	if ack.Command != protocol.CommandOKAY || ack.Arg0 != okay.Arg0 || ack.Arg1 != 99 {
		t.Fatalf("WRTE ACK = %#v, want OKAY for accepted stream", ack)
	}
	reply, err := protocol.ReadMessage(deviceConn)
	if err != nil {
		t.Fatalf("read handler reply: %v", err)
	}
	if reply.Command != protocol.CommandWRTE || reply.Arg0 != okay.Arg0 || reply.Arg1 != 99 || string(reply.Payload) != "pong" {
		t.Fatalf("handler reply = %#v, want WRTE pong", reply)
	}
	closeMsg, err := protocol.ReadMessage(deviceConn)
	if err != nil {
		t.Fatalf("read handler close: %v", err)
	}
	if closeMsg.Command != protocol.CommandCLSE || closeMsg.Arg0 != okay.Arg0 || closeMsg.Arg1 != 99 {
		t.Fatalf("handler close = %#v, want CLSE for accepted stream", closeMsg)
	}

	select {
	case err := <-handled:
		if err != nil {
			t.Fatalf("handler error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for handler")
	}
}

func TestPeerOPENWithoutHandlerIsClosed(t *testing.T) {
	clientConn, deviceConn := net.Pipe()
	defer clientConn.Close()
	defer deviceConn.Close()

	conn := protocol.NewConnection(clientConn)
	if err := conn.HandleOpen("tcp:registered", func(string, *protocol.Stream) {}); err != nil {
		t.Fatalf("HandleOpen: %v", err)
	}

	writeMessage(t, deviceConn, protocol.Message{Command: protocol.CommandOPEN, Arg0: 123, Payload: []byte("tcp:missing\x00")})
	msg, err := protocol.ReadMessage(deviceConn)
	if err != nil {
		t.Fatalf("read CLSE: %v", err)
	}
	if msg.Command != protocol.CommandCLSE || msg.Arg1 != 123 {
		t.Fatalf("message = %#v, want CLSE for remote stream 123", msg)
	}
}

func TestRemoteCLSEClosesStream(t *testing.T) {
	server := fakeadb.Start(t)
	server.Handle("close:", func(ctx context.Context, conn io.ReadWriter, open protocol.Message) {
		writeMessage(t, conn, protocol.Message{Command: protocol.CommandOKAY, Arg0: 9, Arg1: open.Arg0})
		writeMessage(t, conn, protocol.Message{Command: protocol.CommandCLSE, Arg0: 9, Arg1: open.Arg0})
	})

	conn := connectFake(t, server.Addr())
	defer conn.Close()
	stream, err := conn.Open("close:")
	if err != nil {
		t.Fatalf("open service: %v", err)
	}

	buf := make([]byte, 1)
	_, err = stream.Read(buf)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("read error = %v, want EOF", err)
	}
}

func TestOpenServiceCLSEBeforeOKAYFails(t *testing.T) {
	server := fakeadb.Start(t)

	conn := connectFake(t, server.Addr())
	defer conn.Close()
	_, err := conn.Open("missing:")
	if !errors.Is(err, protocol.ErrDeviceClosed) {
		t.Fatalf("open error = %v, want ErrDeviceClosed", err)
	}
}

func connectFake(t *testing.T, addr string) *protocol.Connection {
	t.Helper()
	raw, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial fake adb: %v", err)
	}
	conn := protocol.NewConnection(raw)
	if _, err := conn.Handshake(context.Background()); err != nil {
		t.Fatalf("handshake: %v", err)
	}
	return conn
}

func sendPayloadHandler(t *testing.T, remoteID uint32, payload string) fakeadb.ServiceHandler {
	t.Helper()
	return func(ctx context.Context, conn io.ReadWriter, open protocol.Message) {
		writeMessage(t, conn, protocol.Message{Command: protocol.CommandOKAY, Arg0: remoteID, Arg1: open.Arg0})
		writeMessage(t, conn, protocol.Message{Command: protocol.CommandWRTE, Arg0: remoteID, Arg1: open.Arg0, Payload: []byte(payload)})
	}
}

func assertReadAll(t *testing.T, r io.Reader, want string) {
	t.Helper()
	buf := make([]byte, len(want))
	if _, err := io.ReadFull(r, buf); err != nil {
		t.Fatalf("read stream: %v", err)
	}
	if string(buf) != want {
		t.Fatalf("read = %q, want %q", buf, want)
	}
}

func writeMessage(t *testing.T, w io.Writer, msg protocol.Message) {
	t.Helper()
	if err := protocol.WriteMessage(w, msg); err != nil {
		t.Errorf("write message: %v", err)
	}
}
