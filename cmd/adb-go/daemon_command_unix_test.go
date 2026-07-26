//go:build !windows

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dector/adb-go/internal/daemon"
)

func TestRunDaemonRequiresCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"daemon"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("run(daemon) exit code = %d, want 2", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if got := stderr.String(); !strings.Contains(got, "requires COMMAND") || !strings.Contains(got, "Usage:") {
		t.Fatalf("stderr = %q, want usage error", got)
	}
}

func TestRunDaemonRejectsUnknownCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"daemon", "devices"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("run(daemon devices) exit code = %d, want 2", code)
	}
	if got := stderr.String(); !strings.Contains(got, `unknown daemon command "devices"`) {
		t.Fatalf("stderr = %q, want unknown command error", got)
	}
}

func TestRunDaemonReportsUnavailableSocket(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.sock")
	var stdout, stderr bytes.Buffer
	code := run([]string{"daemon", "--socket", path, "status"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("run(daemon status unavailable) exit code = %d, want 1", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if got := stderr.String(); !strings.Contains(got, "daemon is not running or socket is unavailable") || !strings.Contains(got, path) {
		t.Fatalf("stderr = %q, want unavailable socket error with path", got)
	}
}

func TestRunDaemonPingStatusAndStopWithSocketFlag(t *testing.T) {
	server, socketPath, wait := startDaemonCommandTestServer(t)
	defer wait()

	var pingOut, pingErr bytes.Buffer
	code := run([]string{"daemon", "--socket", socketPath, "ping"}, &pingOut, &pingErr)
	if code != 0 {
		t.Fatalf("run(daemon ping) exit code = %d, want 0; stderr = %q", code, pingErr.String())
	}
	if strings.TrimSpace(pingOut.String()) != "pong" {
		t.Fatalf("ping stdout = %q, want pong", pingOut.String())
	}

	var statusOut, statusErr bytes.Buffer
	code = run([]string{"daemon", "--socket", socketPath, "status"}, &statusOut, &statusErr)
	if code != 0 {
		t.Fatalf("run(daemon status) exit code = %d, want 0; stderr = %q", code, statusErr.String())
	}
	gotStatus := statusOut.String()
	for _, want := range []string{"state: running", "pid:", "socketPath: " + socketPath, "protocolVersion: 1", "uptimeMillis:"} {
		if !strings.Contains(gotStatus, want) {
			t.Fatalf("status stdout = %q, want substring %q", gotStatus, want)
		}
	}

	var stopOut, stopErr bytes.Buffer
	code = run([]string{"daemon", "--socket", socketPath, "stop"}, &stopOut, &stopErr)
	if code != 0 {
		t.Fatalf("run(daemon stop) exit code = %d, want 0; stderr = %q", code, stopErr.String())
	}
	if !strings.Contains(stopOut.String(), "adb-god shutting down") {
		t.Fatalf("stop stdout = %q, want shutdown message", stopOut.String())
	}
	select {
	case <-server.Done():
	case <-time.After(time.Second):
		t.Fatal("daemon did not stop")
	}
	if _, err := os.Lstat(socketPath); !os.IsNotExist(err) {
		t.Fatalf("socket after stop: err = %v, want not exist", err)
	}
}

func TestRunDaemonUsesConfiguredEnvironmentSocketPath(t *testing.T) {
	_, socketPath, wait := startDaemonCommandTestServer(t)
	defer wait()
	t.Setenv(daemon.EnvSocket, socketPath)

	var stdout, stderr bytes.Buffer
	code := run([]string{"daemon", "ping"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run(daemon ping with env socket) exit code = %d, want 0; stderr = %q", code, stderr.String())
	}
	if strings.TrimSpace(stdout.String()) != "pong" {
		t.Fatalf("stdout = %q, want pong", stdout.String())
	}
}

func TestRunDaemonInstallWritesSystemdUserUnit(t *testing.T) {
	adbGodPath := filepath.Join(t.TempDir(), "adb-god")
	if err := os.WriteFile(adbGodPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write fake adb-god: %v", err)
	}
	socketPath := filepath.Join(t.TempDir(), "adb-god.sock")
	unitDir := filepath.Join(t.TempDir(), "systemd", "user")

	var stdout, stderr bytes.Buffer
	code := run([]string{"daemon", "--socket", socketPath, "install", "--adb-god", adbGodPath, "--unit-dir", unitDir, "--no-enable"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run(daemon install) exit code = %d, want 0; stderr = %q", code, stderr.String())
	}
	unitPath := filepath.Join(unitDir, "adb-god.service")
	unit, err := os.ReadFile(unitPath)
	if err != nil {
		t.Fatalf("read unit: %v", err)
	}
	gotUnit := string(unit)
	for _, want := range []string{
		"[Unit]",
		"Description=adb-go daemon",
		"ExecStart=\"" + adbGodPath + "\" -socket \"" + socketPath + "\"",
		"Restart=on-failure",
		"WantedBy=default.target",
	} {
		if !strings.Contains(gotUnit, want) {
			t.Fatalf("unit = %q, want substring %q", gotUnit, want)
		}
	}
	if !strings.Contains(stdout.String(), "installed "+unitPath) || !strings.Contains(stdout.String(), "systemctl enable/start skipped") {
		t.Fatalf("stdout = %q, want install and skipped messages", stdout.String())
	}
}

func TestRunDaemonInstallRunsSystemctlUserCommands(t *testing.T) {
	adbGodPath := filepath.Join(t.TempDir(), "adb-god")
	if err := os.WriteFile(adbGodPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write fake adb-god: %v", err)
	}
	systemctlLog := filepath.Join(t.TempDir(), "systemctl.log")
	systemctlPath := filepath.Join(t.TempDir(), "systemctl")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + strconv.Quote(systemctlLog) + "\n"
	if err := os.WriteFile(systemctlPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake systemctl: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"daemon", "--socket", filepath.Join(t.TempDir(), "adb-god.sock"), "install", "--adb-god", adbGodPath, "--unit-dir", filepath.Join(t.TempDir(), "units"), "--systemctl", systemctlPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run(daemon install) exit code = %d, want 0; stderr = %q", code, stderr.String())
	}
	logBytes, err := os.ReadFile(systemctlLog)
	if err != nil {
		t.Fatalf("read systemctl log: %v", err)
	}
	got := string(logBytes)
	if !strings.Contains(got, "--user daemon-reload\n") || !strings.Contains(got, "--user enable --now adb-god.service\n") {
		t.Fatalf("systemctl log = %q, want daemon-reload and enable --now", got)
	}
	if !strings.Contains(stdout.String(), "adb-god.service enabled and started") {
		t.Fatalf("stdout = %q, want enabled message", stdout.String())
	}
}

func startDaemonCommandTestServer(t *testing.T) (*daemon.Server, string, func()) {
	t.Helper()
	socketPath := filepath.Join(t.TempDir(), "adb-god.sock")
	server, err := daemon.NewServer(daemon.Options{SocketPath: socketPath})
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
