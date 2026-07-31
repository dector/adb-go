//go:build !windows

package custom

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dector/adb-go/internal/server"
)

func TestRunServerRequiresCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"server"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("Run(server) exit code = %d, want 2", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if got := stderr.String(); !strings.Contains(got, "requires COMMAND") || !strings.Contains(got, "Usage:") {
		t.Fatalf("stderr = %q, want usage error", got)
	}
}

func TestRunServerRejectsUnknownCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"server", "devices"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("Run(server devices) exit code = %d, want 2", code)
	}
	if got := stderr.String(); !strings.Contains(got, `unknown server command "devices"`) {
		t.Fatalf("stderr = %q, want unknown command error", got)
	}
}

func TestRunServerReportsUnavailableSocket(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.sock")
	var stdout, stderr bytes.Buffer
	code := Run([]string{"server", "--socket", path, "status"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("Run(server status unavailable) exit code = %d, want 1", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if got := stderr.String(); !strings.Contains(got, "server is not running or socket is unavailable") || !strings.Contains(got, path) {
		t.Fatalf("stderr = %q, want unavailable socket error with path", got)
	}
}

func TestRunServerPingStatusAndStopWithSocketFlag(t *testing.T) {
	server, socketPath, wait := startServerCommandTestServer(t)
	defer wait()

	var pingOut, pingErr bytes.Buffer
	code := Run([]string{"server", "--socket", socketPath, "ping"}, &pingOut, &pingErr)
	if code != 0 {
		t.Fatalf("Run(server ping) exit code = %d, want 0; stderr = %q", code, pingErr.String())
	}
	if strings.TrimSpace(pingOut.String()) != "pong" {
		t.Fatalf("ping stdout = %q, want pong", pingOut.String())
	}

	var statusOut, statusErr bytes.Buffer
	code = Run([]string{"server", "--socket", socketPath, "status"}, &statusOut, &statusErr)
	if code != 0 {
		t.Fatalf("Run(server status) exit code = %d, want 0; stderr = %q", code, statusErr.String())
	}
	gotStatus := statusOut.String()
	for _, want := range []string{"state: running", "pid:", "socketPath: " + socketPath, "protocolVersion: 1", "uptimeMillis:", "forwardTotal: 0", "forwardListening: 0", "forwardDegraded: 0", "forwardActiveConnections: 0"} {
		if !strings.Contains(gotStatus, want) {
			t.Fatalf("status stdout = %q, want substring %q", gotStatus, want)
		}
	}

	var stopOut, stopErr bytes.Buffer
	code = Run([]string{"server", "--socket", socketPath, "stop"}, &stopOut, &stopErr)
	if code != 0 {
		t.Fatalf("Run(server stop) exit code = %d, want 0; stderr = %q", code, stopErr.String())
	}
	if !strings.Contains(stopOut.String(), "adb-gos shutting down") {
		t.Fatalf("stop stdout = %q, want shutdown message", stopOut.String())
	}
	select {
	case <-server.Done():
	case <-time.After(time.Second):
		t.Fatal("server did not stop")
	}
	if _, err := os.Lstat(socketPath); !os.IsNotExist(err) {
		t.Fatalf("socket after stop: err = %v, want not exist", err)
	}
}

func TestRunServerUsesConfiguredEnvironmentSocketPath(t *testing.T) {
	_, socketPath, wait := startServerCommandTestServer(t)
	defer wait()
	t.Setenv(server.EnvSocket, socketPath)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"server", "ping"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run(server ping with env socket) exit code = %d, want 0; stderr = %q", code, stderr.String())
	}
	if strings.TrimSpace(stdout.String()) != "pong" {
		t.Fatalf("stdout = %q, want pong", stdout.String())
	}
}

func TestRunServerDoctorReportsRespondingServer(t *testing.T) {
	_, socketPath, wait := startServerCommandTestServer(t)
	defer wait()
	systemctlPath := writeFakeSystemctlStatus(t, filepath.Join(t.TempDir(), "systemctl.log"), "active", "enabled", 0, 0)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"server", "--socket", socketPath, "doctor", "--systemctl", systemctlPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run(server doctor) exit code = %d, want 0; stderr = %q", code, stderr.String())
	}
	got := stdout.String()
	wantSubstrings := []string{"socketPath: " + socketPath, "socketExists: true", "socketType: unix", "serverProtocol: responding", "serverState: running", "serverProtocolVersion: 1", "forwardTotal: 0", "forwardDegraded: 0", "hints: none"}
	if runtime.GOOS == "linux" {
		wantSubstrings = append(wantSubstrings, "systemdActive: active", "systemdEnabled: enabled")
	} else {
		wantSubstrings = append(wantSubstrings, "systemdUserService: unsupported on this platform")
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Fatalf("doctor stdout = %q, want substring %q", got, want)
		}
	}
}

func TestRunServerDoctorHintsDegradedForwardingState(t *testing.T) {
	_, socketPath, wait := startServerCommandTestServer(t)
	defer wait()
	systemctlPath := writeFakeSystemctlStatus(t, filepath.Join(t.TempDir(), "systemctl.log"), "active", "enabled", 0, 0)

	created, err := server.Send(context.Background(), socketPath, server.Request{Version: server.ProtocolVersion, Command: server.CommandForwardCreate, Params: mustServerCommandJSON(t, server.ForwardCreateParams{
		Local:  server.ForwardLocalEndpoint{Network: "tcp", Address: "127.0.0.1:0"},
		Remote: server.ForwardRemoteEndpoint{Service: "tcp:9001"},
		Target: server.ForwardTarget{Transport: "tcp", Address: freeLoopbackAddress(t)},
	})})
	if err != nil || !created.OK {
		t.Fatalf("create degraded-test forward response = %#v, err = %v", created, err)
	}
	var createResult server.ForwardCreateResult
	decodeServerCommandResult(t, created.Result, &createResult)
	local := createResult.Forward.Local.Address
	conn, err := net.DialTimeout("tcp", local, time.Second)
	if err != nil {
		t.Fatalf("dial forward listener: %v", err)
	}
	_ = conn.Close()
	waitUntil(t, func() bool {
		status, err := server.Send(context.Background(), socketPath, server.Request{Version: server.ProtocolVersion, Command: server.CommandStatus})
		if err != nil || !status.OK {
			return false
		}
		diagnostics, ok := serverForwardDiagnostics(status.Result)
		return ok && diagnostics.Total == 1 && diagnostics.Degraded == 1
	})

	var stdout, stderr bytes.Buffer
	code := Run([]string{"server", "--socket", socketPath, "doctor", "--systemctl", systemctlPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run(server doctor degraded) exit code = %d, want 0; stderr = %q", code, stderr.String())
	}
	got := stdout.String()
	for _, want := range []string{"forwardTotal: 1", "forwardDegraded: 1", "forward --list", "last setup errors"} {
		if !strings.Contains(got, want) {
			t.Fatalf("doctor stdout = %q, want substring %q", got, want)
		}
	}
}

func TestRunServerDoctorReportsMissingSocket(t *testing.T) {
	socketPath := filepath.Join(shortSocketTempDir(t), "missing.sock")
	systemctlPath := writeFakeSystemctlStatus(t, filepath.Join(t.TempDir(), "systemctl.log"), "inactive", "disabled", 3, 1)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"server", "--socket", socketPath, "doctor", "--systemctl", systemctlPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run(server doctor missing socket) exit code = %d, want 0; stderr = %q", code, stderr.String())
	}
	got := stdout.String()
	wantSubstrings := []string{"socketPath: " + socketPath, "socketExists: false", "serverProtocol: not responding", "No server socket exists", "service start", "service install"}
	if runtime.GOOS == "linux" {
		wantSubstrings = append(wantSubstrings, "systemdActive: inactive", "systemdEnabled: disabled")
	} else {
		wantSubstrings = append(wantSubstrings, "systemdUserService: unsupported on this platform")
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Fatalf("doctor stdout = %q, want substring %q", got, want)
		}
	}
}

func TestRunServerDoctorReportsNonServerSocket(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "adb-gos.sock")
	if err := os.WriteFile(socketPath, []byte("not a socket"), 0o644); err != nil {
		t.Fatalf("write fake socket path: %v", err)
	}
	systemctlPath := writeFakeSystemctlStatus(t, filepath.Join(t.TempDir(), "systemctl.log"), "active", "enabled", 0, 0)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"server", "--socket", socketPath, "doctor", "--systemctl", systemctlPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run(server doctor non-server socket) exit code = %d, want 0; stderr = %q", code, stderr.String())
	}
	got := stdout.String()
	for _, want := range []string{"socketExists: true", "serverProtocol: not responding", "exists but is not a Unix socket"} {
		if !strings.Contains(got, want) {
			t.Fatalf("doctor stdout = %q, want substring %q", got, want)
		}
	}
}

func TestRunServerInstallWritesSystemdUserUnit(t *testing.T) {
	requireLinux(t)
	adbGosPath := filepath.Join(t.TempDir(), "adb-gos")
	if err := os.WriteFile(adbGosPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write fake adb-gos: %v", err)
	}
	socketPath := filepath.Join(t.TempDir(), "adb-gos.sock")
	unitDir := filepath.Join(t.TempDir(), "systemd", "user")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"server", "--socket", socketPath, "service", "install", "--adb-gos", adbGosPath, "--unit-dir", unitDir, "--no-enable"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run(server service install) exit code = %d, want 0; stderr = %q", code, stderr.String())
	}
	unitPath := filepath.Join(unitDir, "adb-gos.service")
	unit, err := os.ReadFile(unitPath)
	if err != nil {
		t.Fatalf("read unit: %v", err)
	}
	gotUnit := string(unit)
	for _, want := range []string{
		"[Unit]",
		"Description=adb-go server",
		"ExecStart=\"" + adbGosPath + "\" -socket \"" + socketPath + "\"",
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

func TestRunServerInstallRunsSystemctlUserCommands(t *testing.T) {
	requireLinux(t)
	adbGosPath := filepath.Join(t.TempDir(), "adb-gos")
	if err := os.WriteFile(adbGosPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write fake adb-gos: %v", err)
	}
	systemctlLog := filepath.Join(t.TempDir(), "systemctl.log")
	systemctlPath := filepath.Join(t.TempDir(), "systemctl")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + strconv.Quote(systemctlLog) + "\n"
	if err := os.WriteFile(systemctlPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake systemctl: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := Run([]string{"server", "--socket", filepath.Join(t.TempDir(), "adb-gos.sock"), "service", "install", "--adb-gos", adbGosPath, "--unit-dir", filepath.Join(t.TempDir(), "units"), "--systemctl", systemctlPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run(server service install) exit code = %d, want 0; stderr = %q", code, stderr.String())
	}
	logBytes, err := os.ReadFile(systemctlLog)
	if err != nil {
		t.Fatalf("read systemctl log: %v", err)
	}
	got := string(logBytes)
	if !strings.Contains(got, "--user daemon-reload\n") || !strings.Contains(got, "--user enable --now adb-gos.service\n") {
		t.Fatalf("systemctl log = %q, want daemon-reload and enable --now", got)
	}
	if !strings.Contains(stdout.String(), "adb-gos.service enabled and started") {
		t.Fatalf("stdout = %q, want enabled message", stdout.String())
	}
}

func TestRunServerServiceReinstallRewritesUnitReloadsEnablesAndRestarts(t *testing.T) {
	requireLinux(t)
	oldServerPath := filepath.Join(t.TempDir(), "old-adb-gos")
	newServerPath := filepath.Join(t.TempDir(), "new-adb-gos")
	if err := os.WriteFile(newServerPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write fake adb-gos: %v", err)
	}
	oldSocketPath := filepath.Join(t.TempDir(), "old.sock")
	newSocketPath := filepath.Join(t.TempDir(), "new.sock")
	unitDir := filepath.Join(t.TempDir(), "units")
	if err := os.MkdirAll(unitDir, 0o755); err != nil {
		t.Fatalf("create unit dir: %v", err)
	}
	unitPath := filepath.Join(unitDir, "adb-gos.service")
	if err := os.WriteFile(unitPath, []byte(adbGosSystemdUnit(oldServerPath, oldSocketPath)), 0o644); err != nil {
		t.Fatalf("write old unit: %v", err)
	}
	systemctlLog := filepath.Join(t.TempDir(), "systemctl.log")
	systemctlPath := writeFakeSystemctl(t, systemctlLog)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"server", "--socket", newSocketPath, "service", "reinstall", "--adb-gos", newServerPath, "--unit-dir", unitDir, "--systemctl", systemctlPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run(server service reinstall) exit code = %d, want 0; stderr = %q", code, stderr.String())
	}
	unit, err := os.ReadFile(unitPath)
	if err != nil {
		t.Fatalf("read rewritten unit: %v", err)
	}
	gotUnit := string(unit)
	for _, want := range []string{
		"ExecStart=\"" + newServerPath + "\" -socket \"" + newSocketPath + "\"",
		"Restart=on-failure",
		"WantedBy=default.target",
	} {
		if !strings.Contains(gotUnit, want) {
			t.Fatalf("unit = %q, want substring %q", gotUnit, want)
		}
	}
	if strings.Contains(gotUnit, oldServerPath) || strings.Contains(gotUnit, oldSocketPath) {
		t.Fatalf("unit = %q, want old server and socket paths replaced", gotUnit)
	}
	logBytes, err := os.ReadFile(systemctlLog)
	if err != nil {
		t.Fatalf("read systemctl log: %v", err)
	}
	wantLog := "--user daemon-reload\n--user enable adb-gos.service\n--user restart adb-gos.service\n"
	if string(logBytes) != wantLog {
		t.Fatalf("systemctl log = %q, want %q", string(logBytes), wantLog)
	}
	if !strings.Contains(stdout.String(), "reinstalled "+unitPath) || !strings.Contains(stdout.String(), "enabled and restarted") {
		t.Fatalf("stdout = %q, want reinstall messages", stdout.String())
	}
}

func TestRunServerServiceLifecycleCommands(t *testing.T) {
	requireLinux(t)
	for _, tc := range []struct {
		command string
		stdout  string
	}{
		{command: "start", stdout: "adb-gos.service started"},
		{command: "stop", stdout: "adb-gos.service stopped"},
		{command: "restart", stdout: "adb-gos.service restarted"},
	} {
		t.Run(tc.command, func(t *testing.T) {
			systemctlLog := filepath.Join(t.TempDir(), "systemctl.log")
			systemctlPath := writeFakeSystemctl(t, systemctlLog)

			var stdout, stderr bytes.Buffer
			code := Run([]string{"server", "service", tc.command, "--systemctl", systemctlPath}, &stdout, &stderr)
			if code != 0 {
				t.Fatalf("Run(server service %s) exit code = %d, want 0; stderr = %q", tc.command, code, stderr.String())
			}
			logBytes, err := os.ReadFile(systemctlLog)
			if err != nil {
				t.Fatalf("read systemctl log: %v", err)
			}
			wantLog := "--user " + tc.command + " adb-gos.service\n"
			if string(logBytes) != wantLog {
				t.Fatalf("systemctl log = %q, want %q", string(logBytes), wantLog)
			}
			if strings.TrimSpace(stdout.String()) != tc.stdout {
				t.Fatalf("stdout = %q, want %q", stdout.String(), tc.stdout)
			}
		})
	}
}

func TestRunServerServiceStatusReportsActiveAndEnabled(t *testing.T) {
	requireLinux(t)
	systemctlLog := filepath.Join(t.TempDir(), "systemctl.log")
	systemctlPath := writeFakeSystemctlStatus(t, systemctlLog, "active", "enabled", 0, 0)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"server", "service", "status", "--systemctl", systemctlPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run(server service status) exit code = %d, want 0; stderr = %q", code, stderr.String())
	}
	logBytes, err := os.ReadFile(systemctlLog)
	if err != nil {
		t.Fatalf("read systemctl log: %v", err)
	}
	wantLog := "--user is-active adb-gos.service\n--user is-enabled adb-gos.service\n"
	if string(logBytes) != wantLog {
		t.Fatalf("systemctl log = %q, want %q", string(logBytes), wantLog)
	}
	wantOut := "active: active\nenabled: enabled\n"
	if stdout.String() != wantOut {
		t.Fatalf("stdout = %q, want %q", stdout.String(), wantOut)
	}
}

func TestRunServerServiceStatusReportsInactiveAndDisabled(t *testing.T) {
	requireLinux(t)
	systemctlPath := writeFakeSystemctlStatus(t, filepath.Join(t.TempDir(), "systemctl.log"), "inactive", "disabled", 3, 1)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"server", "service", "status", "--systemctl", systemctlPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run(server service status) exit code = %d, want 0; stderr = %q", code, stderr.String())
	}
	wantOut := "active: inactive\nenabled: disabled\n"
	if stdout.String() != wantOut {
		t.Fatalf("stdout = %q, want %q", stdout.String(), wantOut)
	}
}

func TestRunServerServiceLogsInvokesJournalctlWithDefaultShape(t *testing.T) {
	requireLinux(t)
	journalctlLog := filepath.Join(t.TempDir(), "journalctl.log")
	journalctlPath := writeFakeJournalctl(t, journalctlLog, "recent server log\n")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"server", "service", "logs", "--journalctl", journalctlPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run(server service logs) exit code = %d, want 0; stderr = %q", code, stderr.String())
	}
	logBytes, err := os.ReadFile(journalctlLog)
	if err != nil {
		t.Fatalf("read journalctl log: %v", err)
	}
	wantLog := "--user -u adb-gos.service -n 100 --no-pager\n"
	if string(logBytes) != wantLog {
		t.Fatalf("journalctl log = %q, want %q", string(logBytes), wantLog)
	}
	if stdout.String() != "recent server log\n" {
		t.Fatalf("stdout = %q, want fake journal output", stdout.String())
	}
}

func TestRunServerServiceLogsHonorsLinesAndFollow(t *testing.T) {
	requireLinux(t)
	journalctlLog := filepath.Join(t.TempDir(), "journalctl.log")
	journalctlPath := writeFakeJournalctl(t, journalctlLog, "follow log\n")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"server", "service", "logs", "--journalctl", journalctlPath, "--lines", "25", "--follow"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run(server service logs --lines --follow) exit code = %d, want 0; stderr = %q", code, stderr.String())
	}
	logBytes, err := os.ReadFile(journalctlLog)
	if err != nil {
		t.Fatalf("read journalctl log: %v", err)
	}
	wantLog := "--user -u adb-gos.service -n 25 --no-pager -f\n"
	if string(logBytes) != wantLog {
		t.Fatalf("journalctl log = %q, want %q", string(logBytes), wantLog)
	}
	if stdout.String() != "follow log\n" {
		t.Fatalf("stdout = %q, want fake journal output", stdout.String())
	}
}

func TestRunServerServiceLogsRejectsNegativeLines(t *testing.T) {
	requireLinux(t)
	var stdout, stderr bytes.Buffer
	code := Run([]string{"server", "service", "logs", "--lines", "-1"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("Run(server service logs --lines -1) exit code = %d, want 2", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if got := stderr.String(); !strings.Contains(got, "--lines must be zero or greater") {
		t.Fatalf("stderr = %q, want lines validation error", got)
	}
}

func TestRunServerServiceUninstallDisablesStopsRemovesUnitAndReloads(t *testing.T) {
	requireLinux(t)
	unitDir := filepath.Join(t.TempDir(), "units")
	if err := os.MkdirAll(unitDir, 0o755); err != nil {
		t.Fatalf("create unit dir: %v", err)
	}
	unitPath := filepath.Join(unitDir, "adb-gos.service")
	if err := os.WriteFile(unitPath, []byte("[Service]\n"), 0o644); err != nil {
		t.Fatalf("write unit: %v", err)
	}
	systemctlLog := filepath.Join(t.TempDir(), "systemctl.log")
	systemctlPath := writeFakeSystemctl(t, systemctlLog)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"server", "service", "uninstall", "--unit-dir", unitDir, "--systemctl", systemctlPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run(server service uninstall) exit code = %d, want 0; stderr = %q", code, stderr.String())
	}
	if _, err := os.Stat(unitPath); !os.IsNotExist(err) {
		t.Fatalf("unit after uninstall: err = %v, want not exist", err)
	}
	logBytes, err := os.ReadFile(systemctlLog)
	if err != nil {
		t.Fatalf("read systemctl log: %v", err)
	}
	got := string(logBytes)
	if !strings.Contains(got, "--user disable --now adb-gos.service\n") || !strings.Contains(got, "--user daemon-reload\n") {
		t.Fatalf("systemctl log = %q, want disable --now and daemon-reload", got)
	}
	if !strings.Contains(stdout.String(), "disabled and stopped") || !strings.Contains(stdout.String(), "removed "+unitPath) {
		t.Fatalf("stdout = %q, want uninstall message", stdout.String())
	}
}

func writeFakeJournalctl(t *testing.T, logPath, output string) string {
	t.Helper()
	journalctlPath := filepath.Join(t.TempDir(), "journalctl")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + strconv.Quote(logPath) + "\ncat <<'ADB_GO_FAKE_JOURNALCTL_OUTPUT'\n" + output + "ADB_GO_FAKE_JOURNALCTL_OUTPUT\n"
	if err := os.WriteFile(journalctlPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake journalctl: %v", err)
	}
	return journalctlPath
}

func writeFakeSystemctl(t *testing.T, logPath string) string {
	t.Helper()
	systemctlPath := filepath.Join(t.TempDir(), "systemctl")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + strconv.Quote(logPath) + "\n"
	if err := os.WriteFile(systemctlPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake systemctl: %v", err)
	}
	return systemctlPath
}

func writeFakeSystemctlStatus(t *testing.T, logPath, active, enabled string, activeCode, enabledCode int) string {
	t.Helper()
	systemctlPath := filepath.Join(t.TempDir(), "systemctl")
	script := strings.Join([]string{
		"#!/bin/sh",
		"printf '%s\\n' \"$*\" >> " + strconv.Quote(logPath),
		"case \"$*\" in",
		"  '--user is-active adb-gos.service') printf '%s\\n' " + strconv.Quote(active) + "; exit " + strconv.Itoa(activeCode) + " ;;",
		"  '--user is-enabled adb-gos.service') printf '%s\\n' " + strconv.Quote(enabled) + "; exit " + strconv.Itoa(enabledCode) + " ;;",
		"  *) exit 99 ;;",
		"esac",
		"",
	}, "\n")
	if err := os.WriteFile(systemctlPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake systemctl status: %v", err)
	}
	return systemctlPath
}

func waitUntil(t *testing.T, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition was not met before timeout")
}

func freeLoopbackAddress(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen on free loopback port: %v", err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatalf("close free loopback listener: %v", err)
	}
	return addr
}

func mustServerCommandJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	body, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal JSON: %v", err)
	}
	return body
}

func decodeServerCommandResult(t *testing.T, result map[string]any, v any) {
	t.Helper()
	body, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal server result: %v", err)
	}
	if err := json.Unmarshal(body, v); err != nil {
		t.Fatalf("decode server result: %v", err)
	}
}

func requireLinux(t testing.TB) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("systemd user service commands are supported on Linux only")
	}
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

func startServerCommandTestServer(t *testing.T) (*server.Server, string, func()) {
	t.Helper()
	socketPath := filepath.Join(shortSocketTempDir(t), "d.sock")
	server, err := server.NewServer(server.Options{SocketPath: socketPath})
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
