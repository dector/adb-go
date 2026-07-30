package client_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	adb "github.com/dector/adb-go"
	"github.com/dector/adb-go/internal/fakeadb"
	"github.com/dector/adb-go/protocol"
)

func TestReverseEndpointValidation(t *testing.T) {
	if _, err := adb.ReverseDeviceTCP(0); err == nil || !strings.Contains(err.Error(), "out of range") {
		t.Fatalf("ReverseDeviceTCP(0) error = %v, want out of range", err)
	}
	if _, err := adb.ReverseHostTCP(65536); err == nil || !strings.Contains(err.Error(), "out of range") {
		t.Fatalf("ReverseHostTCP(65536) error = %v, want out of range", err)
	}
	if _, err := adb.ParseReverseDeviceEndpoint("localabstract:name"); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("ParseReverseDeviceEndpoint(localabstract) error = %v, want unsupported", err)
	}
	if _, err := adb.ParseReverseHostEndpoint("tcp:not-a-port"); err == nil || !strings.Contains(err.Error(), "invalid host tcp port") {
		t.Fatalf("ParseReverseHostEndpoint(bad port) error = %v, want invalid port", err)
	}
	if _, err := adb.ParseReverseDeviceEndpoint("tcp:0"); err == nil || !strings.Contains(err.Error(), "out of range") {
		t.Fatalf("ParseReverseDeviceEndpoint(tcp:0) error = %v, want unsupported auto port/out of range", err)
	}

	remote, err := adb.ParseReverseDeviceEndpoint("tcp:8081")
	if err != nil {
		t.Fatalf("ParseReverseDeviceEndpoint(tcp:8081): %v", err)
	}
	local, err := adb.ParseReverseHostEndpoint("tcp:9001")
	if err != nil {
		t.Fatalf("ParseReverseHostEndpoint(tcp:9001): %v", err)
	}
	if _, err := adb.ReverseDeviceTCP(8081); err != nil || remote == (adb.ReverseDeviceEndpoint{}) {
		t.Fatalf("ReverseDeviceTCP valid error = %v endpoint=%#v", err, remote)
	}
	if _, err := adb.ReverseHostTCP(9001); err != nil || local == (adb.ReverseHostEndpoint{}) {
		t.Fatalf("ReverseHostTCP valid error = %v endpoint=%#v", err, local)
	}
}

func TestReverseTCPRegistersBridgesAndCleansUp(t *testing.T) {
	hostLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen host target: %v", err)
	}
	defer hostLn.Close()
	hostPort := hostLn.Addr().(*net.TCPAddr).Port
	hostDone := make(chan error, 1)
	go func() {
		conn, err := hostLn.Accept()
		if err != nil {
			hostDone <- err
			return
		}
		defer conn.Close()
		buf := make([]byte, len("hello-host"))
		if _, err := io.ReadFull(conn, buf); err != nil {
			hostDone <- err
			return
		}
		if string(buf) != "hello-host" {
			hostDone <- fmt.Errorf("host read %q, want hello-host", buf)
			return
		}
		_, err = conn.Write([]byte("hello-device"))
		hostDone <- err
	}()

	server := fakeadb.Start(t)
	localService := fmt.Sprintf("tcp:%d", hostPort)
	registered := make(chan struct{}, 1)
	bridged := make(chan error, 1)
	cleaned := make(chan struct{}, 1)

	server.Handle("reverse:forward:tcp:8081;"+localService, func(ctx context.Context, conn io.ReadWriter, open protocol.Message) {
		writeClientTestMessage(t, conn, protocol.Message{Command: protocol.CommandOKAY, Arg0: 10, Arg1: open.Arg0})
		writeClientTestMessage(t, conn, protocol.Message{Command: protocol.CommandCLSE, Arg0: 10, Arg1: open.Arg0})
		registered <- struct{}{}

		writeClientTestMessage(t, conn, protocol.Message{Command: protocol.CommandOPEN, Arg0: 77, Payload: []byte(localService + "\x00")})
		okay, err := protocol.ReadMessage(conn)
		if err != nil {
			bridged <- fmt.Errorf("read accepted OKAY: %w", err)
			return
		}
		if okay.Command != protocol.CommandOKAY || okay.Arg1 != 77 || okay.Arg0 == 0 {
			bridged <- fmt.Errorf("accepted OKAY = %#v", okay)
			return
		}

		writeClientTestMessage(t, conn, protocol.Message{Command: protocol.CommandWRTE, Arg0: 77, Arg1: okay.Arg0, Payload: []byte("hello-host")})
		if ack, err := protocol.ReadMessage(conn); err != nil || ack.Command != protocol.CommandOKAY {
			bridged <- fmt.Errorf("read WRTE ack = %#v, %v", ack, err)
			return
		}
		reply, err := protocol.ReadMessage(conn)
		if err != nil {
			bridged <- fmt.Errorf("read reply WRTE: %w", err)
			return
		}
		if reply.Command != protocol.CommandWRTE || reply.Arg0 != okay.Arg0 || reply.Arg1 != 77 || string(reply.Payload) != "hello-device" {
			bridged <- fmt.Errorf("reply = %#v, want hello-device WRTE", reply)
			return
		}
		bridged <- nil
	})
	server.Handle("reverse:killforward:tcp:8081", func(ctx context.Context, conn io.ReadWriter, open protocol.Message) {
		writeClientTestMessage(t, conn, protocol.Message{Command: protocol.CommandOKAY, Arg0: 11, Arg1: open.Arg0})
		writeClientTestMessage(t, conn, protocol.Message{Command: protocol.CommandCLSE, Arg0: 11, Arg1: open.Arg0})
		cleaned <- struct{}{}
	})

	c, err := adb.Connect(context.Background(), server.Addr())
	if err != nil {
		t.Fatalf("connect fake adb: %v", err)
	}
	defer c.Close()
	remote, _ := adb.ReverseDeviceTCP(8081)
	local, _ := adb.ReverseHostTCP(hostPort)

	rev, err := c.ReverseTCP(context.Background(), remote, local)
	if err != nil {
		t.Fatalf("ReverseTCP: %v", err)
	}

	select {
	case <-registered:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for reverse registration")
	}
	select {
	case err := <-hostDone:
		if err != nil {
			t.Fatalf("host target error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for host target")
	}
	select {
	case err := <-bridged:
		if err != nil {
			t.Fatalf("fake adb bridge error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for reverse bridge")
	}

	if err := rev.Close(); err != nil {
		t.Fatalf("reverse close: %v", err)
	}
	select {
	case <-cleaned:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for reverse cleanup")
	}
}

func TestReverseTCPRegistrationFailureIncludesDeviceMessage(t *testing.T) {
	server := fakeadb.Start(t)
	server.Handle("reverse:forward:tcp:8081;tcp:9001", func(ctx context.Context, conn io.ReadWriter, open protocol.Message) {
		writeClientTestMessage(t, conn, protocol.Message{Command: protocol.CommandOKAY, Arg0: 20, Arg1: open.Arg0})
		writeClientTestMessage(t, conn, protocol.Message{Command: protocol.CommandWRTE, Arg0: 20, Arg1: open.Arg0, Payload: []byte("cannot bind listener")})
		writeClientTestMessage(t, conn, protocol.Message{Command: protocol.CommandCLSE, Arg0: 20, Arg1: open.Arg0})
	})

	c, err := adb.Connect(context.Background(), server.Addr())
	if err != nil {
		t.Fatalf("connect fake adb: %v", err)
	}
	defer c.Close()
	remote, _ := adb.ReverseDeviceTCP(8081)
	local, _ := adb.ReverseHostTCP(9001)

	_, err = c.ReverseTCP(context.Background(), remote, local)
	if err == nil || !strings.Contains(err.Error(), "cannot bind listener") {
		t.Fatalf("ReverseTCP error = %v, want device setup message", err)
	}
}

func writeClientTestMessage(t *testing.T, w io.Writer, msg protocol.Message) {
	t.Helper()
	if err := protocol.WriteMessage(w, msg); err != nil && !errors.Is(err, net.ErrClosed) {
		t.Errorf("write message: %v", err)
	}
}
