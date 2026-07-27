//go:build !windows

package daemon

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dector/adb-go/internal/fakeadb"
	"github.com/dector/adb-go/protocol"
)

func TestDefaultSocketPathUsesAbsoluteOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "adb-god.sock")
	t.Setenv(EnvSocket, path)
	got, err := DefaultSocketPath()
	if err != nil {
		t.Fatalf("DefaultSocketPath() error = %v", err)
	}
	if got != path {
		t.Fatalf("DefaultSocketPath() = %q, want %q", got, path)
	}
}

func TestDefaultSocketPathRejectsRelativeOverride(t *testing.T) {
	t.Setenv(EnvSocket, "relative.sock")
	if _, err := DefaultSocketPath(); err == nil {
		t.Fatal("DefaultSocketPath() error = nil, want relative path error")
	}
}

func TestServerPingStatusShutdownAndCleanup(t *testing.T) {
	server, socketPath, wait := startTestServer(t)
	defer wait()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	ping, err := Send(ctx, socketPath, Request{Version: ProtocolVersion, ID: "p1", Command: CommandPing})
	if err != nil {
		t.Fatalf("ping daemon: %v", err)
	}
	if !ping.OK || ping.ID != "p1" || ping.Result["message"] != "pong" {
		t.Fatalf("ping response = %#v, want pong with echoed id", ping)
	}

	status, err := Send(ctx, socketPath, Request{Version: ProtocolVersion, ID: "s1", Command: CommandStatus})
	if err != nil {
		t.Fatalf("status daemon: %v", err)
	}
	if !status.OK || status.ID != "s1" {
		t.Fatalf("status response = %#v, want ok with echoed id", status)
	}
	for _, key := range []string{"state", "pid", "socketPath", "protocolVersion", "uptimeMillis"} {
		if _, ok := status.Result[key]; !ok {
			t.Fatalf("status result missing %q: %#v", key, status.Result)
		}
	}
	if status.Result["state"] != "running" || status.Result["socketPath"] != socketPath {
		t.Fatalf("status result = %#v, want running status for socket", status.Result)
	}

	shutdown, err := Send(ctx, socketPath, Request{Version: ProtocolVersion, ID: "q1", Command: CommandShutdown})
	if err != nil {
		t.Fatalf("shutdown daemon: %v", err)
	}
	if !shutdown.OK || shutdown.Result["message"] != "shutting_down" {
		t.Fatalf("shutdown response = %#v, want accepted shutdown", shutdown)
	}
	select {
	case <-server.Done():
	case <-time.After(time.Second):
		t.Fatal("server did not shut down")
	}
	if _, err := os.Lstat(socketPath); !os.IsNotExist(err) {
		t.Fatalf("socket file after shutdown: err = %v, want not exist", err)
	}
}

func TestServerHandlesForwardingProtocolModel(t *testing.T) {
	_, socketPath, wait := startTestServer(t)
	defer wait()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	createParams := mustJSON(t, ForwardCreateParams{
		Local:    ForwardLocalEndpoint{Network: "tcp", Address: "127.0.0.1:9000"},
		Remote:   ForwardRemoteEndpoint{Service: "tcp:9001"},
		Target:   ForwardTarget{Transport: "tcp", Address: "127.0.0.1:5555"},
		Norebind: true,
	})
	created, err := Send(ctx, socketPath, Request{Version: ProtocolVersion, ID: "c1", Command: CommandForwardCreate, Params: createParams})
	if err != nil {
		t.Fatalf("forward_create daemon: %v", err)
	}
	if !created.OK || created.ID != "c1" {
		t.Fatalf("forward_create response = %#v, want ok with echoed id", created)
	}
	forward, ok := created.Result["forward"].(map[string]any)
	if !ok {
		t.Fatalf("forward_create result = %#v, want forward object", created.Result)
	}
	if forward["state"] != ForwardStateListening || forward["id"] == "" || forward["lastError"] != nil {
		t.Fatalf("forward_create forward = %#v, want daemon-owned listening forward", forward)
	}

	list, err := Send(ctx, socketPath, Request{Version: ProtocolVersion, ID: "l1", Command: CommandForwardList})
	if err != nil {
		t.Fatalf("forward_list daemon: %v", err)
	}
	if !list.OK || list.ID != "l1" {
		t.Fatalf("forward_list response = %#v, want ok with echoed id", list)
	}
	if _, ok := list.Result["forwards"].([]any); !ok {
		t.Fatalf("forward_list result = %#v, want forwards array", list.Result)
	}

	remove, err := Send(ctx, socketPath, Request{Version: ProtocolVersion, ID: "r1", Command: CommandForwardRemove, Params: mustJSON(t, ForwardRemoveParams{ID: "fwd_missing"})})
	if err != nil {
		t.Fatalf("forward_remove daemon: %v", err)
	}
	if remove.OK || remove.Error == nil || remove.Error.Code != ErrorForwardNotFound {
		t.Fatalf("forward_remove response = %#v, want forward_not_found", remove)
	}

	removeAll, err := Send(ctx, socketPath, Request{Version: ProtocolVersion, ID: "ra1", Command: CommandForwardRemoveAll})
	if err != nil {
		t.Fatalf("forward_remove_all daemon: %v", err)
	}
	if !removeAll.OK || removeAll.Result["removed"] != float64(1) {
		t.Fatalf("forward_remove_all response = %#v, want removed 1", removeAll)
	}
}

func TestServerManagesDaemonForwardListeners(t *testing.T) {
	_, socketPath, wait := startTestServer(t)
	defer wait()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	created, err := Send(ctx, socketPath, Request{Version: ProtocolVersion, ID: "c1", Command: CommandForwardCreate, Params: mustJSON(t, ForwardCreateParams{
		Local:  ForwardLocalEndpoint{Network: "tcp", Address: "127.0.0.1:0"},
		Remote: ForwardRemoteEndpoint{Service: "tcp:9001"},
		Target: ForwardTarget{Transport: "tcp", Address: "127.0.0.1:5555"},
	})})
	if err != nil {
		t.Fatalf("forward_create daemon: %v", err)
	}
	if !created.OK {
		t.Fatalf("forward_create response = %#v, want ok", created)
	}
	forward := resultForward(t, created)
	id, _ := forward["id"].(string)
	local := resultLocal(t, forward)
	if id == "" || forward["state"] != ForwardStateListening || local["address"] == "127.0.0.1:0" {
		t.Fatalf("created forward = %#v, want id, listening state, and actual tcp:0 address", forward)
	}

	conn, err := net.DialTimeout("tcp", local["address"].(string), time.Second)
	if err != nil {
		t.Fatalf("dial daemon-owned forward listener: %v", err)
	}
	_ = conn.Close()

	list, err := Send(ctx, socketPath, Request{Version: ProtocolVersion, ID: "l1", Command: CommandForwardList})
	if err != nil {
		t.Fatalf("forward_list daemon: %v", err)
	}
	forwards, ok := list.Result["forwards"].([]any)
	if !list.OK || !ok || len(forwards) != 1 {
		t.Fatalf("forward_list response = %#v, want one forward", list)
	}

	removeByLocalEndpoint := ForwardLocalEndpoint{Network: "tcp", Address: local["address"].(string)}
	removeByLocal, err := Send(ctx, socketPath, Request{Version: ProtocolVersion, ID: "rl1", Command: CommandForwardRemove, Params: mustJSON(t, ForwardRemoveParams{Local: &removeByLocalEndpoint})})
	if err != nil {
		t.Fatalf("forward_remove by local daemon: %v", err)
	}
	if !removeByLocal.OK || removeByLocal.Result["removed"] != float64(1) {
		t.Fatalf("forward_remove by local response = %#v, want removed 1", removeByLocal)
	}
	if conn, err := net.DialTimeout("tcp", local["address"].(string), 100*time.Millisecond); err == nil {
		_ = conn.Close()
		t.Fatal("dial removed forward listener succeeded, want listener closed")
	}

	recreated, err := Send(ctx, socketPath, Request{Version: ProtocolVersion, ID: "c2", Command: CommandForwardCreate, Params: mustJSON(t, ForwardCreateParams{
		Local:  ForwardLocalEndpoint{Network: "tcp", Address: "127.0.0.1:0"},
		Remote: ForwardRemoteEndpoint{Service: "tcp:9001"},
		Target: ForwardTarget{Transport: "tcp", Address: "127.0.0.1:5555"},
	})})
	if err != nil || !recreated.OK {
		t.Fatalf("second forward_create = %#v, err = %v; want ok", recreated, err)
	}
	id, _ = resultForward(t, recreated)["id"].(string)
	removeByID, err := Send(ctx, socketPath, Request{Version: ProtocolVersion, ID: "ri1", Command: CommandForwardRemove, Params: mustJSON(t, ForwardRemoveParams{ID: id})})
	if err != nil {
		t.Fatalf("forward_remove by id daemon: %v", err)
	}
	if !removeByID.OK || removeByID.Result["removed"] != float64(1) {
		t.Fatalf("forward_remove by id response = %#v, want removed 1", removeByID)
	}
}

func TestServerForwardRebindNorebindAddressInUseAndRemoveAll(t *testing.T) {
	_, socketPath, wait := startTestServer(t)
	defer wait()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	base := ForwardCreateParams{
		Local:  ForwardLocalEndpoint{Network: "tcp", Address: freeLoopbackAddress(t)},
		Remote: ForwardRemoteEndpoint{Service: "tcp:9001"},
		Target: ForwardTarget{Transport: "tcp", Address: "127.0.0.1:5555"},
	}
	first, err := Send(ctx, socketPath, Request{Version: ProtocolVersion, Command: CommandForwardCreate, Params: mustJSON(t, base)})
	if err != nil || !first.OK {
		t.Fatalf("first forward_create = %#v, err = %v; want ok", first, err)
	}
	firstID, _ := resultForward(t, first)["id"].(string)

	norebind := base
	norebind.Norebind = true
	blocked, err := Send(ctx, socketPath, Request{Version: ProtocolVersion, Command: CommandForwardCreate, Params: mustJSON(t, norebind)})
	if err != nil {
		t.Fatalf("norebind forward_create: %v", err)
	}
	if blocked.OK || blocked.Error == nil || blocked.Error.Code != ErrorRebindDisallowed {
		t.Fatalf("norebind response = %#v, want rebind_disallowed", blocked)
	}

	rebound, err := Send(ctx, socketPath, Request{Version: ProtocolVersion, Command: CommandForwardCreate, Params: mustJSON(t, base)})
	if err != nil || !rebound.OK {
		t.Fatalf("rebind forward_create = %#v, err = %v; want ok", rebound, err)
	}
	reboundID, _ := resultForward(t, rebound)["id"].(string)
	if reboundID == "" || reboundID == firstID {
		t.Fatalf("rebound id = %q, first id = %q; want replacement forward", reboundID, firstID)
	}

	busyLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("create busy listener: %v", err)
	}
	defer busyLn.Close()
	busy := base
	busy.Local.Address = busyLn.Addr().String()
	busyResp, err := Send(ctx, socketPath, Request{Version: ProtocolVersion, Command: CommandForwardCreate, Params: mustJSON(t, busy)})
	if err != nil {
		t.Fatalf("busy forward_create: %v", err)
	}
	if busyResp.OK || busyResp.Error == nil || busyResp.Error.Code != ErrorAddressInUse {
		t.Fatalf("busy response = %#v, want address_in_use", busyResp)
	}

	removeAll, err := Send(ctx, socketPath, Request{Version: ProtocolVersion, Command: CommandForwardRemoveAll})
	if err != nil {
		t.Fatalf("forward_remove_all daemon: %v", err)
	}
	if !removeAll.OK || removeAll.Result["removed"] != float64(1) {
		t.Fatalf("forward_remove_all response = %#v, want removed 1", removeAll)
	}
}

func TestServerShutdownClosesForwardListeners(t *testing.T) {
	server, socketPath, wait := startTestServer(t)
	defer wait()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	created, err := Send(ctx, socketPath, Request{Version: ProtocolVersion, Command: CommandForwardCreate, Params: mustJSON(t, ForwardCreateParams{
		Local:  ForwardLocalEndpoint{Network: "tcp", Address: "127.0.0.1:0"},
		Remote: ForwardRemoteEndpoint{Service: "tcp:9001"},
		Target: ForwardTarget{Transport: "tcp", Address: "127.0.0.1:5555"},
	})})
	if err != nil || !created.OK {
		t.Fatalf("forward_create = %#v, err = %v; want ok", created, err)
	}
	local := resultLocal(t, resultForward(t, created))["address"].(string)
	if err := server.Shutdown(); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if conn, err := net.DialTimeout("tcp", local, 100*time.Millisecond); err == nil {
		_ = conn.Close()
		t.Fatal("dial forward listener after shutdown succeeded, want closed listener")
	}
}

func TestServerBridgesForwardToFakeADBTarget(t *testing.T) {
	fake := fakeadb.Start(t)
	fake.Handle("tcp:9001", func(ctx context.Context, conn io.ReadWriter, open protocol.Message) {
		const remoteID = 42
		if err := protocol.WriteMessage(conn, protocol.Message{Command: protocol.CommandOKAY, Arg0: remoteID, Arg1: open.Arg0}); err != nil {
			return
		}
		for {
			msg, err := protocol.ReadMessage(conn)
			if err != nil || msg.Command == protocol.CommandCLSE {
				return
			}
			if msg.Command != protocol.CommandWRTE {
				continue
			}
			_ = protocol.WriteMessage(conn, protocol.Message{Command: protocol.CommandOKAY, Arg0: remoteID, Arg1: msg.Arg0})
			_ = protocol.WriteMessage(conn, protocol.Message{Command: protocol.CommandWRTE, Arg0: remoteID, Arg1: msg.Arg0, Payload: []byte("device:" + string(msg.Payload))})
		}
	})
	_, socketPath, wait := startTestServer(t)
	defer wait()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	created, err := Send(ctx, socketPath, Request{Version: ProtocolVersion, Command: CommandForwardCreate, Params: mustJSON(t, ForwardCreateParams{
		Local:  ForwardLocalEndpoint{Network: "tcp", Address: "127.0.0.1:0"},
		Remote: ForwardRemoteEndpoint{Service: "tcp:9001"},
		Target: ForwardTarget{Transport: "tcp", Address: fake.Addr()},
	})})
	if err != nil || !created.OK {
		t.Fatalf("forward_create = %#v, err = %v; want ok", created, err)
	}
	local := resultLocal(t, resultForward(t, created))["address"].(string)

	host, err := net.DialTimeout("tcp", local, time.Second)
	if err != nil {
		t.Fatalf("dial forward listener: %v", err)
	}
	defer host.Close()
	_ = host.SetDeadline(time.Now().Add(time.Second))
	if _, err := host.Write([]byte("hello")); err != nil {
		t.Fatalf("write host payload: %v", err)
	}
	buf := make([]byte, len("device:hello"))
	if _, err := io.ReadFull(host, buf); err != nil {
		t.Fatalf("read bridged device payload: %v", err)
	}
	if string(buf) != "device:hello" {
		t.Fatalf("bridged payload = %q, want device:hello", string(buf))
	}

	waitFor(t, time.Second, func() bool {
		list, err := Send(ctx, socketPath, Request{Version: ProtocolVersion, Command: CommandForwardList})
		if err != nil || !list.OK {
			return false
		}
		forwards := list.Result["forwards"].([]any)
		forward := forwards[0].(map[string]any)
		return forward["activeConnections"] == float64(1) && forward["lastError"] == nil && forward["state"] == ForwardStateListening
	})
}

func TestServerForwardBridgeFailureKeepsListenerDegraded(t *testing.T) {
	_, socketPath, wait := startTestServer(t)
	defer wait()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	target := freeLoopbackAddress(t)

	created, err := Send(ctx, socketPath, Request{Version: ProtocolVersion, Command: CommandForwardCreate, Params: mustJSON(t, ForwardCreateParams{
		Local:  ForwardLocalEndpoint{Network: "tcp", Address: "127.0.0.1:0"},
		Remote: ForwardRemoteEndpoint{Service: "tcp:9001"},
		Target: ForwardTarget{Transport: "tcp", Address: target},
	})})
	if err != nil || !created.OK {
		t.Fatalf("forward_create = %#v, err = %v; want ok", created, err)
	}
	local := resultLocal(t, resultForward(t, created))["address"].(string)

	conn, err := net.DialTimeout("tcp", local, time.Second)
	if err != nil {
		t.Fatalf("dial forward listener with down target: %v", err)
	}
	_ = conn.Close()

	waitFor(t, time.Second, func() bool {
		list, err := Send(ctx, socketPath, Request{Version: ProtocolVersion, Command: CommandForwardList})
		if err != nil || !list.OK {
			return false
		}
		forward := list.Result["forwards"].([]any)[0].(map[string]any)
		lastError, _ := forward["lastError"].(string)
		return forward["state"] == ForwardStateDegraded && strings.Contains(lastError, "connect target")
	})
	second, err := net.DialTimeout("tcp", local, time.Second)
	if err != nil {
		t.Fatalf("dial degraded forward listener again: %v", err)
	}
	_ = second.Close()
}

func TestServerForwardRemoveAndShutdownCloseActiveBridgeConnections(t *testing.T) {
	fake := fakeadb.Start(t)
	fake.Handle("tcp:9002", func(ctx context.Context, conn io.ReadWriter, open protocol.Message) {
		const remoteID = 77
		if err := protocol.WriteMessage(conn, protocol.Message{Command: protocol.CommandOKAY, Arg0: remoteID, Arg1: open.Arg0}); err != nil {
			return
		}
		for {
			msg, err := protocol.ReadMessage(conn)
			if err != nil || msg.Command == protocol.CommandCLSE {
				return
			}
			if msg.Command == protocol.CommandWRTE {
				_ = protocol.WriteMessage(conn, protocol.Message{Command: protocol.CommandOKAY, Arg0: remoteID, Arg1: msg.Arg0})
			}
		}
	})
	server, socketPath, wait := startTestServer(t)
	defer wait()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	created, err := Send(ctx, socketPath, Request{Version: ProtocolVersion, Command: CommandForwardCreate, Params: mustJSON(t, ForwardCreateParams{
		Local:  ForwardLocalEndpoint{Network: "tcp", Address: "127.0.0.1:0"},
		Remote: ForwardRemoteEndpoint{Service: "tcp:9002"},
		Target: ForwardTarget{Transport: "tcp", Address: fake.Addr()},
	})})
	if err != nil || !created.OK {
		t.Fatalf("forward_create = %#v, err = %v; want ok", created, err)
	}
	forward := resultForward(t, created)
	id := forward["id"].(string)
	local := resultLocal(t, forward)["address"].(string)

	host, err := net.DialTimeout("tcp", local, time.Second)
	if err != nil {
		t.Fatalf("dial forward listener: %v", err)
	}
	_ = host.SetDeadline(time.Now().Add(time.Second))
	if _, err := host.Write([]byte("keepalive")); err != nil {
		t.Fatalf("write host payload: %v", err)
	}
	waitFor(t, time.Second, func() bool {
		list, err := Send(ctx, socketPath, Request{Version: ProtocolVersion, Command: CommandForwardList})
		if err != nil || !list.OK {
			return false
		}
		forward := list.Result["forwards"].([]any)[0].(map[string]any)
		return forward["activeConnections"] == float64(1)
	})

	removed, err := Send(ctx, socketPath, Request{Version: ProtocolVersion, Command: CommandForwardRemove, Params: mustJSON(t, ForwardRemoveParams{ID: id})})
	if err != nil || !removed.OK {
		t.Fatalf("forward_remove = %#v, err = %v; want ok", removed, err)
	}
	buf := make([]byte, 1)
	if _, err := host.Read(buf); err == nil {
		t.Fatal("read after removing active forward succeeded, want closed connection")
	}
	_ = host.Close()

	created, err = Send(ctx, socketPath, Request{Version: ProtocolVersion, Command: CommandForwardCreate, Params: mustJSON(t, ForwardCreateParams{
		Local:  ForwardLocalEndpoint{Network: "tcp", Address: "127.0.0.1:0"},
		Remote: ForwardRemoteEndpoint{Service: "tcp:9002"},
		Target: ForwardTarget{Transport: "tcp", Address: fake.Addr()},
	})})
	if err != nil || !created.OK {
		t.Fatalf("second forward_create = %#v, err = %v; want ok", created, err)
	}
	local = resultLocal(t, resultForward(t, created))["address"].(string)
	host, err = net.DialTimeout("tcp", local, time.Second)
	if err != nil {
		t.Fatalf("dial second forward listener: %v", err)
	}
	_ = host.SetDeadline(time.Now().Add(time.Second))
	if _, err := host.Write([]byte("keepalive")); err != nil {
		t.Fatalf("write second host payload: %v", err)
	}
	waitFor(t, time.Second, func() bool {
		list, err := Send(ctx, socketPath, Request{Version: ProtocolVersion, Command: CommandForwardList})
		if err != nil || !list.OK {
			return false
		}
		return list.Result["forwards"].([]any)[0].(map[string]any)["activeConnections"] == float64(1)
	})
	if err := server.Shutdown(); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if _, err := host.Read(buf); err == nil {
		t.Fatal("read after daemon shutdown succeeded, want closed connection")
	}
	_ = host.Close()
}

func TestServerForwardingProtocolValidationErrors(t *testing.T) {
	_, socketPath, wait := startTestServer(t)
	defer wait()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	badLocal := mustJSON(t, ForwardCreateParams{
		Local:  ForwardLocalEndpoint{Network: "tcp", Address: "192.0.2.1:9000"},
		Remote: ForwardRemoteEndpoint{Service: "tcp:9001"},
		Target: ForwardTarget{Transport: "tcp", Address: "127.0.0.1:5555"},
	})
	resp, err := Send(ctx, socketPath, Request{Version: ProtocolVersion, ID: "bad-local", Command: CommandForwardCreate, Params: badLocal})
	if err != nil {
		t.Fatalf("forward_create bad local daemon: %v", err)
	}
	if resp.OK || resp.Error == nil || resp.Error.Code != ErrorUnsupportedEndpoint {
		t.Fatalf("bad local response = %#v, want unsupported_endpoint", resp)
	}

	badTarget := mustJSON(t, ForwardCreateParams{
		Local:  ForwardLocalEndpoint{Network: "tcp", Address: "127.0.0.1:9000"},
		Remote: ForwardRemoteEndpoint{Service: "tcp:9001"},
		Target: ForwardTarget{Transport: "usb", Address: "/dev/bus/usb/001/002"},
	})
	resp, err = Send(ctx, socketPath, Request{Version: ProtocolVersion, ID: "bad-target", Command: CommandForwardCreate, Params: badTarget})
	if err != nil {
		t.Fatalf("forward_create bad target daemon: %v", err)
	}
	if resp.OK || resp.Error == nil || resp.Error.Code != ErrorBadTarget {
		t.Fatalf("bad target response = %#v, want bad_target", resp)
	}

	resp, err = Send(ctx, socketPath, Request{Version: ProtocolVersion, ID: "bad-remove", Command: CommandForwardRemove, Params: mustJSON(t, ForwardRemoveParams{})})
	if err != nil {
		t.Fatalf("forward_remove bad selector daemon: %v", err)
	}
	if resp.OK || resp.Error == nil || resp.Error.Code != ErrorBadRequest {
		t.Fatalf("bad remove response = %#v, want bad_request", resp)
	}
}

func TestServerReturnsStructuredProtocolErrors(t *testing.T) {
	_, socketPath, wait := startTestServer(t)
	defer wait()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	badVersion, err := Send(ctx, socketPath, Request{Version: 99, ID: "v", Command: CommandPing})
	if err != nil {
		t.Fatalf("bad version request: %v", err)
	}
	if badVersion.OK || badVersion.Error == nil || badVersion.Error.Code != ErrorUnsupportedVersion || badVersion.ID != "v" {
		t.Fatalf("bad version response = %#v, want unsupported_version error", badVersion)
	}

	unknown, err := Send(ctx, socketPath, Request{Version: ProtocolVersion, ID: "u", Command: "devices"})
	if err != nil {
		t.Fatalf("unknown command request: %v", err)
	}
	if unknown.OK || unknown.Error == nil || unknown.Error.Code != ErrorUnknownCommand || unknown.ID != "u" {
		t.Fatalf("unknown command response = %#v, want unknown_command error", unknown)
	}

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("dial daemon: %v", err)
	}
	if _, err := conn.Write([]byte("not-json\n")); err != nil {
		t.Fatalf("write malformed request: %v", err)
	}
	var malformed Response
	if err := json.NewDecoder(conn).Decode(&malformed); err != nil {
		t.Fatalf("decode malformed response: %v", err)
	}
	_ = conn.Close()
	if malformed.OK || malformed.Error == nil || malformed.Error.Code != ErrorBadRequest {
		t.Fatalf("malformed response = %#v, want bad_request error", malformed)
	}
}

func TestServerStartupHandlesStaleSocketAndRefusesFilesOrRunningDaemon(t *testing.T) {
	dir := shortSocketTempDir(t)
	stalePath := filepath.Join(dir, "stale.sock")
	ln, err := net.Listen("unix", stalePath)
	if err != nil {
		t.Fatalf("create stale socket: %v", err)
	}
	if err := ln.Close(); err != nil {
		t.Fatalf("close stale listener: %v", err)
	}
	staleServer, err := NewServer(Options{SocketPath: stalePath})
	if err != nil {
		t.Fatalf("NewServer(stale) error = %v", err)
	}
	if err := staleServer.Listen(); err != nil {
		t.Fatalf("Listen() with stale socket error = %v", err)
	}
	_ = staleServer.Shutdown()

	filePath := filepath.Join(dir, "not-a-socket")
	if err := os.WriteFile(filePath, []byte("keep me"), 0o600); err != nil {
		t.Fatalf("write file at socket path: %v", err)
	}
	fileServer, err := NewServer(Options{SocketPath: filePath})
	if err != nil {
		t.Fatalf("NewServer(file) error = %v", err)
	}
	if err := fileServer.Listen(); err == nil {
		t.Fatal("Listen() with regular file error = nil, want refusal")
	}

	running, runningPath, wait := startTestServer(t)
	defer wait()
	second, err := NewServer(Options{SocketPath: runningPath})
	if err != nil {
		t.Fatalf("NewServer(second) error = %v", err)
	}
	if err := second.Listen(); err == nil {
		t.Fatal("second Listen() error = nil, want already running")
	}
	_ = running.Shutdown()
}

func startTestServer(t *testing.T) (*Server, string, func()) {
	t.Helper()
	socketPath := filepath.Join(shortSocketTempDir(t), "d.sock")
	server, err := NewServer(Options{SocketPath: socketPath})
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	if err := server.Listen(); err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(context.Background()) }()
	wait := func() {
		_ = server.Shutdown()
		select {
		case err := <-serveErr:
			if err != nil {
				t.Fatalf("Serve() error = %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("Serve() did not return")
		}
	}
	return server, socketPath, wait
}

func resultForward(t testing.TB, resp Response) map[string]any {
	t.Helper()
	forward, ok := resp.Result["forward"].(map[string]any)
	if !ok {
		t.Fatalf("response result = %#v, want forward object", resp.Result)
	}
	return forward
}

func resultLocal(t testing.TB, forward map[string]any) map[string]any {
	t.Helper()
	local, ok := forward["local"].(map[string]any)
	if !ok {
		t.Fatalf("forward = %#v, want local object", forward)
	}
	return local
}

func freeLoopbackAddress(t testing.TB) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen on free loopback address: %v", err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatalf("close free loopback listener: %v", err)
	}
	return addr
}

func waitFor(t testing.TB, timeout time.Duration, pred func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if pred() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	if pred() {
		return
	}
	t.Fatalf("condition was not met within %s", timeout)
}

func mustJSON(t testing.TB, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("Marshal(%T): %v", v, err)
	}
	return b
}

func shortSocketTempDir(t testing.TB) string {
	t.Helper()
	dir, err := os.MkdirTemp(os.TempDir(), "adbgo-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}
