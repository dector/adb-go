//go:build !windows

package daemon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/dector/adb-go/internal/fakeadb"
	"github.com/dector/adb-go/protocol"
)

func TestServerHandlesReverseProtocolModel(t *testing.T) {
	fake := fakeadb.Start(t)
	registered := make(chan struct{}, 1)
	cleaned := make(chan struct{}, 1)
	registerHandler := func(ctx context.Context, conn io.ReadWriter, open protocol.Message) {
		writeDaemonReverseTestMessage(t, conn, protocol.Message{Command: protocol.CommandOKAY, Arg0: 10, Arg1: open.Arg0})
		writeDaemonReverseTestMessage(t, conn, protocol.Message{Command: protocol.CommandCLSE, Arg0: 10, Arg1: open.Arg0})
		registered <- struct{}{}
	}
	fake.Handle("reverse:forward:norebind:tcp:8081;tcp:3000", registerHandler)
	fake.Handle("reverse:forward:tcp:8081;tcp:3000", registerHandler)
	fake.Handle("reverse:killforward:tcp:8081", func(ctx context.Context, conn io.ReadWriter, open protocol.Message) {
		writeDaemonReverseTestMessage(t, conn, protocol.Message{Command: protocol.CommandOKAY, Arg0: 11, Arg1: open.Arg0})
		writeDaemonReverseTestMessage(t, conn, protocol.Message{Command: protocol.CommandCLSE, Arg0: 11, Arg1: open.Arg0})
		cleaned <- struct{}{}
	})
	_, socketPath, wait := startTestServer(t)
	defer wait()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	created, err := Send(ctx, socketPath, Request{Version: ProtocolVersion, ID: "c1", Command: CommandReverseCreate, Params: mustJSON(t, ReverseCreateParams{
		Remote:   ReverseRemoteEndpoint{Service: "tcp:8081"},
		Local:    ReverseLocalEndpoint{Service: "tcp:3000"},
		Target:   ForwardTarget{Transport: "tcp", Address: fake.Addr()},
		Norebind: true,
	})})
	if err != nil || !created.OK {
		t.Fatalf("reverse_create daemon = %#v, err = %v; want ok", created, err)
	}
	select {
	case <-registered:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for reverse registration")
	}
	reverse := resultReverse(t, created)
	id, _ := reverse["id"].(string)
	if id == "" || reverse["state"] != ReverseStateListening || reverse["lastError"] != nil {
		t.Fatalf("reverse_create reverse = %#v, want id/listening/no last error", reverse)
	}

	list, err := Send(ctx, socketPath, Request{Version: ProtocolVersion, ID: "l1", Command: CommandReverseList})
	if err != nil {
		t.Fatalf("reverse_list daemon: %v", err)
	}
	if reverses, ok := list.Result["reverses"].([]any); !list.OK || !ok || len(reverses) != 1 {
		t.Fatalf("reverse_list response = %#v, want one reverse", list)
	}

	blocked, err := Send(ctx, socketPath, Request{Version: ProtocolVersion, Command: CommandReverseCreate, Params: mustJSON(t, ReverseCreateParams{
		Remote:   ReverseRemoteEndpoint{Service: "tcp:8081"},
		Local:    ReverseLocalEndpoint{Service: "tcp:3000"},
		Target:   ForwardTarget{Transport: "tcp", Address: fake.Addr()},
		Norebind: true,
	})})
	if err != nil {
		t.Fatalf("norebind reverse_create: %v", err)
	}
	if blocked.OK || blocked.Error == nil || blocked.Error.Code != ErrorRebindDisallowed {
		t.Fatalf("norebind response = %#v, want rebind_disallowed", blocked)
	}

	remote := ReverseRemoteEndpoint{Service: "tcp:8081"}
	removed, err := Send(ctx, socketPath, Request{Version: ProtocolVersion, ID: "r1", Command: CommandReverseRemove, Params: mustJSON(t, ReverseRemoveParams{Remote: &remote})})
	if err != nil || !removed.OK || removed.Result["removed"] != float64(1) {
		t.Fatalf("reverse_remove response = %#v, err = %v; want removed 1", removed, err)
	}
	select {
	case <-cleaned:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for reverse cleanup")
	}

	missing, err := Send(ctx, socketPath, Request{Version: ProtocolVersion, Command: CommandReverseRemove, Params: mustJSON(t, ReverseRemoveParams{ID: id})})
	if err != nil {
		t.Fatalf("missing reverse_remove: %v", err)
	}
	if missing.OK || missing.Error == nil || missing.Error.Code != ErrorReverseNotFound {
		t.Fatalf("missing remove response = %#v, want reverse_not_found", missing)
	}

	recreated, err := Send(ctx, socketPath, Request{Version: ProtocolVersion, Command: CommandReverseCreate, Params: mustJSON(t, ReverseCreateParams{
		Remote: ReverseRemoteEndpoint{Service: "tcp:8081"},
		Local:  ReverseLocalEndpoint{Service: "tcp:3000"},
		Target: ForwardTarget{Transport: "tcp", Address: fake.Addr()},
	})})
	if err != nil || !recreated.OK {
		t.Fatalf("recreate reverse = %#v, err = %v; want ok", recreated, err)
	}
	select {
	case <-registered:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for second reverse registration")
	}
	removeAll, err := Send(ctx, socketPath, Request{Version: ProtocolVersion, Command: CommandReverseRemoveAll})
	if err != nil || !removeAll.OK || removeAll.Result["removed"] != float64(1) {
		t.Fatalf("reverse_remove_all response = %#v, err = %v; want removed 1", removeAll, err)
	}
	select {
	case <-cleaned:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for remove-all reverse cleanup")
	}
}

func TestServerShutdownClosesReverseRegistrations(t *testing.T) {
	fake := fakeadb.Start(t)
	registered := make(chan struct{}, 1)
	cleaned := make(chan struct{}, 1)
	fake.Handle("reverse:forward:tcp:8081;tcp:3000", func(ctx context.Context, conn io.ReadWriter, open protocol.Message) {
		writeDaemonReverseTestMessage(t, conn, protocol.Message{Command: protocol.CommandOKAY, Arg0: 30, Arg1: open.Arg0})
		writeDaemonReverseTestMessage(t, conn, protocol.Message{Command: protocol.CommandCLSE, Arg0: 30, Arg1: open.Arg0})
		registered <- struct{}{}
	})
	fake.Handle("reverse:killforward:tcp:8081", func(ctx context.Context, conn io.ReadWriter, open protocol.Message) {
		writeDaemonReverseTestMessage(t, conn, protocol.Message{Command: protocol.CommandOKAY, Arg0: 31, Arg1: open.Arg0})
		writeDaemonReverseTestMessage(t, conn, protocol.Message{Command: protocol.CommandCLSE, Arg0: 31, Arg1: open.Arg0})
		cleaned <- struct{}{}
	})
	server, socketPath, wait := startTestServer(t)
	defer wait()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	created, err := Send(ctx, socketPath, Request{Version: ProtocolVersion, Command: CommandReverseCreate, Params: mustJSON(t, ReverseCreateParams{
		Remote: ReverseRemoteEndpoint{Service: "tcp:8081"},
		Local:  ReverseLocalEndpoint{Service: "tcp:3000"},
		Target: ForwardTarget{Transport: "tcp", Address: fake.Addr()},
	})})
	if err != nil || !created.OK {
		t.Fatalf("reverse_create = %#v, err = %v; want ok", created, err)
	}
	select {
	case <-registered:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for reverse registration")
	}
	if err := server.Shutdown(); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	select {
	case <-cleaned:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for shutdown reverse cleanup")
	}
}

func TestServerReverseBridgeTracksActive(t *testing.T) {
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
		_, err = conn.Write([]byte("hello-device"))
		hostDone <- err
	}()

	fake := fakeadb.Start(t)
	registered := make(chan struct{}, 1)
	bridged := make(chan error, 1)
	localService := fmt.Sprintf("tcp:%d", hostPort)
	fake.Handle("reverse:forward:tcp:8081;"+localService, func(ctx context.Context, conn io.ReadWriter, open protocol.Message) {
		writeDaemonReverseTestMessage(t, conn, protocol.Message{Command: protocol.CommandOKAY, Arg0: 20, Arg1: open.Arg0})
		writeDaemonReverseTestMessage(t, conn, protocol.Message{Command: protocol.CommandCLSE, Arg0: 20, Arg1: open.Arg0})
		registered <- struct{}{}

		writeDaemonReverseTestMessage(t, conn, protocol.Message{Command: protocol.CommandOPEN, Arg0: 77, Payload: []byte(localService + "\x00")})
		okay, err := protocol.ReadMessage(conn)
		if err != nil {
			bridged <- fmt.Errorf("read accepted OKAY: %w", err)
			return
		}
		writeDaemonReverseTestMessage(t, conn, protocol.Message{Command: protocol.CommandWRTE, Arg0: 77, Arg1: okay.Arg0, Payload: []byte("hello-host")})
		if ack, err := protocol.ReadMessage(conn); err != nil || ack.Command != protocol.CommandOKAY {
			bridged <- fmt.Errorf("read WRTE ack = %#v, %v", ack, err)
			return
		}
		reply, err := protocol.ReadMessage(conn)
		if err != nil {
			bridged <- fmt.Errorf("read reply WRTE: %w", err)
			return
		}
		if string(reply.Payload) != "hello-device" {
			bridged <- fmt.Errorf("reply payload = %q, want hello-device", reply.Payload)
			return
		}
		bridged <- nil
	})
	fake.Handle("reverse:killforward:tcp:8081", func(ctx context.Context, conn io.ReadWriter, open protocol.Message) {
		writeDaemonReverseTestMessage(t, conn, protocol.Message{Command: protocol.CommandOKAY, Arg0: 21, Arg1: open.Arg0})
		writeDaemonReverseTestMessage(t, conn, protocol.Message{Command: protocol.CommandCLSE, Arg0: 21, Arg1: open.Arg0})
	})
	_, socketPath, wait := startTestServer(t)
	defer wait()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	created, err := Send(ctx, socketPath, Request{Version: ProtocolVersion, Command: CommandReverseCreate, Params: mustJSON(t, ReverseCreateParams{
		Remote: ReverseRemoteEndpoint{Service: "tcp:8081"},
		Local:  ReverseLocalEndpoint{Service: localService},
		Target: ForwardTarget{Transport: "tcp", Address: fake.Addr()},
	})})
	if err != nil || !created.OK {
		t.Fatalf("reverse_create = %#v, err = %v; want ok", created, err)
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
			t.Fatalf("bridge error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for bridge")
	}

	waitFor(t, time.Second, func() bool {
		list, err := Send(ctx, socketPath, Request{Version: ProtocolVersion, Command: CommandReverseList})
		if err != nil || !list.OK {
			return false
		}
		reverse := list.Result["reverses"].([]any)[0].(map[string]any)
		return reverse["activeConnections"] == float64(0) && reverse["state"] == ReverseStateListening
	})
}

func TestServerReverseHostDialFailureMarksDegraded(t *testing.T) {
	fake := fakeadb.Start(t)
	registered := make(chan struct{}, 1)
	_, closedPortText, err := net.SplitHostPort(freeLoopbackAddress(t))
	if err != nil {
		t.Fatalf("split free loopback address: %v", err)
	}
	localService := "tcp:" + closedPortText
	fake.Handle("reverse:forward:tcp:8081;"+localService, func(ctx context.Context, conn io.ReadWriter, open protocol.Message) {
		writeDaemonReverseTestMessage(t, conn, protocol.Message{Command: protocol.CommandOKAY, Arg0: 40, Arg1: open.Arg0})
		writeDaemonReverseTestMessage(t, conn, protocol.Message{Command: protocol.CommandCLSE, Arg0: 40, Arg1: open.Arg0})
		registered <- struct{}{}
		writeDaemonReverseTestMessage(t, conn, protocol.Message{Command: protocol.CommandOPEN, Arg0: 88, Payload: []byte(localService + "\x00")})
	})
	fake.Handle("reverse:killforward:tcp:8081", func(ctx context.Context, conn io.ReadWriter, open protocol.Message) {
		writeDaemonReverseTestMessage(t, conn, protocol.Message{Command: protocol.CommandOKAY, Arg0: 41, Arg1: open.Arg0})
		writeDaemonReverseTestMessage(t, conn, protocol.Message{Command: protocol.CommandCLSE, Arg0: 41, Arg1: open.Arg0})
	})
	_, socketPath, wait := startTestServer(t)
	defer wait()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	created, err := Send(ctx, socketPath, Request{Version: ProtocolVersion, Command: CommandReverseCreate, Params: mustJSON(t, ReverseCreateParams{
		Remote: ReverseRemoteEndpoint{Service: "tcp:8081"},
		Local:  ReverseLocalEndpoint{Service: localService},
		Target: ForwardTarget{Transport: "tcp", Address: fake.Addr()},
	})})
	if err != nil || !created.OK {
		t.Fatalf("reverse_create = %#v, err = %v; want ok", created, err)
	}
	select {
	case <-registered:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for reverse registration")
	}
	waitFor(t, time.Second, func() bool {
		list, err := Send(ctx, socketPath, Request{Version: ProtocolVersion, Command: CommandReverseList})
		if err != nil || !list.OK {
			return false
		}
		reverse := list.Result["reverses"].([]any)[0].(map[string]any)
		lastError, _ := reverse["lastError"].(string)
		return reverse["state"] == ReverseStateDegraded && strings.Contains(lastError, "host dial failed")
	})
}

func TestServerReverseCreateFailureAndValidationErrors(t *testing.T) {
	_, socketPath, wait := startTestServer(t)
	defer wait()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	resp, err := Send(ctx, socketPath, Request{Version: ProtocolVersion, Command: CommandReverseCreate, Params: mustJSON(t, ReverseCreateParams{
		Remote: ReverseRemoteEndpoint{Service: "localabstract:name"},
		Local:  ReverseLocalEndpoint{Service: "tcp:3000"},
		Target: ForwardTarget{Transport: "tcp", Address: "127.0.0.1:5555"},
	})})
	if err != nil {
		t.Fatalf("bad reverse_create: %v", err)
	}
	if resp.OK || resp.Error == nil || resp.Error.Code != ErrorUnsupportedEndpoint {
		t.Fatalf("bad endpoint response = %#v, want unsupported_endpoint", resp)
	}

	resp, err = Send(ctx, socketPath, Request{Version: ProtocolVersion, Command: CommandReverseCreate, Params: mustJSON(t, ReverseCreateParams{
		Remote: ReverseRemoteEndpoint{Service: "tcp:8081"},
		Local:  ReverseLocalEndpoint{Service: "tcp:3000"},
		Target: ForwardTarget{Transport: "tcp", Address: freeLoopbackAddress(t)},
	})})
	if err != nil {
		t.Fatalf("down target reverse_create: %v", err)
	}
	if resp.OK || resp.Error == nil || resp.Error.Code != ErrorRegistrationFailed || !strings.Contains(resp.Error.Message, "connect target") {
		t.Fatalf("down target response = %#v, want registration_failed connect error", resp)
	}
}

func resultReverse(t testing.TB, resp Response) map[string]any {
	t.Helper()
	reverse, ok := resp.Result["reverse"].(map[string]any)
	if !ok {
		t.Fatalf("response result = %#v, want reverse object", resp.Result)
	}
	return reverse
}

func writeDaemonReverseTestMessage(t testing.TB, w io.Writer, msg protocol.Message) {
	t.Helper()
	if err := protocol.WriteMessage(w, msg); err != nil && !errors.Is(err, net.ErrClosed) {
		t.Errorf("write message: %v", err)
	}
}
