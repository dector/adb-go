//go:build !windows

package daemon

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
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

func shortSocketTempDir(t testing.TB) string {
	t.Helper()
	dir, err := os.MkdirTemp(os.TempDir(), "adbgo-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}
