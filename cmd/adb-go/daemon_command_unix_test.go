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

func TestRunDaemonDoctorReportsRespondingDaemon(t *testing.T) {
	_, socketPath, wait := startDaemonCommandTestServer(t)
	defer wait()
	systemctlPath := writeFakeSystemctlStatus(t, filepath.Join(t.TempDir(), "systemctl.log"), "active", "enabled", 0, 0)

	var stdout, stderr bytes.Buffer
	code := run([]string{"daemon", "--socket", socketPath, "doctor", "--systemctl", systemctlPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run(daemon doctor) exit code = %d, want 0; stderr = %q", code, stderr.String())
	}
	got := stdout.String()
	for _, want := range []string{"socketPath: " + socketPath, "socketExists: true", "socketType: unix", "daemonProtocol: responding", "daemonState: running", "daemonProtocolVersion: 1", "systemdActive: active", "systemdEnabled: enabled", "hints: none"} {
		if !strings.Contains(got, want) {
			t.Fatalf("doctor stdout = %q, want substring %q", got, want)
		}
	}
}

func TestRunDaemonDoctorReportsMissingSocket(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "missing.sock")
	systemctlPath := writeFakeSystemctlStatus(t, filepath.Join(t.TempDir(), "systemctl.log"), "inactive", "disabled", 3, 1)

	var stdout, stderr bytes.Buffer
	code := run([]string{"daemon", "--socket", socketPath, "doctor", "--systemctl", systemctlPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run(daemon doctor missing socket) exit code = %d, want 0; stderr = %q", code, stderr.String())
	}
	got := stdout.String()
	for _, want := range []string{"socketPath: " + socketPath, "socketExists: false", "daemonProtocol: not responding", "systemdActive: inactive", "systemdEnabled: disabled", "No daemon socket exists", "service start", "service install"} {
		if !strings.Contains(got, want) {
			t.Fatalf("doctor stdout = %q, want substring %q", got, want)
		}
	}
}

func TestRunDaemonDoctorReportsNonDaemonSocket(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "adb-god.sock")
	if err := os.WriteFile(socketPath, []byte("not a socket"), 0o644); err != nil {
		t.Fatalf("write fake socket path: %v", err)
	}
	systemctlPath := writeFakeSystemctlStatus(t, filepath.Join(t.TempDir(), "systemctl.log"), "active", "enabled", 0, 0)

	var stdout, stderr bytes.Buffer
	code := run([]string{"daemon", "--socket", socketPath, "doctor", "--systemctl", systemctlPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run(daemon doctor non-daemon socket) exit code = %d, want 0; stderr = %q", code, stderr.String())
	}
	got := stdout.String()
	for _, want := range []string{"socketExists: true", "daemonProtocol: not responding", "exists but is not a Unix socket"} {
		if !strings.Contains(got, want) {
			t.Fatalf("doctor stdout = %q, want substring %q", got, want)
		}
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
	code := run([]string{"daemon", "--socket", socketPath, "service", "install", "--adb-god", adbGodPath, "--unit-dir", unitDir, "--no-enable"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run(daemon service install) exit code = %d, want 0; stderr = %q", code, stderr.String())
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
	code := run([]string{"daemon", "--socket", filepath.Join(t.TempDir(), "adb-god.sock"), "service", "install", "--adb-god", adbGodPath, "--unit-dir", filepath.Join(t.TempDir(), "units"), "--systemctl", systemctlPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run(daemon service install) exit code = %d, want 0; stderr = %q", code, stderr.String())
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

func TestRunDaemonServiceReinstallRewritesUnitReloadsEnablesAndRestarts(t *testing.T) {
	oldDaemonPath := filepath.Join(t.TempDir(), "old-adb-god")
	newDaemonPath := filepath.Join(t.TempDir(), "new-adb-god")
	if err := os.WriteFile(newDaemonPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write fake adb-god: %v", err)
	}
	oldSocketPath := filepath.Join(t.TempDir(), "old.sock")
	newSocketPath := filepath.Join(t.TempDir(), "new.sock")
	unitDir := filepath.Join(t.TempDir(), "units")
	if err := os.MkdirAll(unitDir, 0o755); err != nil {
		t.Fatalf("create unit dir: %v", err)
	}
	unitPath := filepath.Join(unitDir, "adb-god.service")
	if err := os.WriteFile(unitPath, []byte(adbGodSystemdUnit(oldDaemonPath, oldSocketPath)), 0o644); err != nil {
		t.Fatalf("write old unit: %v", err)
	}
	systemctlLog := filepath.Join(t.TempDir(), "systemctl.log")
	systemctlPath := writeFakeSystemctl(t, systemctlLog)

	var stdout, stderr bytes.Buffer
	code := run([]string{"daemon", "--socket", newSocketPath, "service", "reinstall", "--adb-god", newDaemonPath, "--unit-dir", unitDir, "--systemctl", systemctlPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run(daemon service reinstall) exit code = %d, want 0; stderr = %q", code, stderr.String())
	}
	unit, err := os.ReadFile(unitPath)
	if err != nil {
		t.Fatalf("read rewritten unit: %v", err)
	}
	gotUnit := string(unit)
	for _, want := range []string{
		"ExecStart=\"" + newDaemonPath + "\" -socket \"" + newSocketPath + "\"",
		"Restart=on-failure",
		"WantedBy=default.target",
	} {
		if !strings.Contains(gotUnit, want) {
			t.Fatalf("unit = %q, want substring %q", gotUnit, want)
		}
	}
	if strings.Contains(gotUnit, oldDaemonPath) || strings.Contains(gotUnit, oldSocketPath) {
		t.Fatalf("unit = %q, want old daemon and socket paths replaced", gotUnit)
	}
	logBytes, err := os.ReadFile(systemctlLog)
	if err != nil {
		t.Fatalf("read systemctl log: %v", err)
	}
	wantLog := "--user daemon-reload\n--user enable adb-god.service\n--user restart adb-god.service\n"
	if string(logBytes) != wantLog {
		t.Fatalf("systemctl log = %q, want %q", string(logBytes), wantLog)
	}
	if !strings.Contains(stdout.String(), "reinstalled "+unitPath) || !strings.Contains(stdout.String(), "enabled and restarted") {
		t.Fatalf("stdout = %q, want reinstall messages", stdout.String())
	}
}

func TestRunDaemonServiceLifecycleCommands(t *testing.T) {
	for _, tc := range []struct {
		command string
		stdout  string
	}{
		{command: "start", stdout: "adb-god.service started"},
		{command: "stop", stdout: "adb-god.service stopped"},
		{command: "restart", stdout: "adb-god.service restarted"},
	} {
		t.Run(tc.command, func(t *testing.T) {
			systemctlLog := filepath.Join(t.TempDir(), "systemctl.log")
			systemctlPath := writeFakeSystemctl(t, systemctlLog)

			var stdout, stderr bytes.Buffer
			code := run([]string{"daemon", "service", tc.command, "--systemctl", systemctlPath}, &stdout, &stderr)
			if code != 0 {
				t.Fatalf("run(daemon service %s) exit code = %d, want 0; stderr = %q", tc.command, code, stderr.String())
			}
			logBytes, err := os.ReadFile(systemctlLog)
			if err != nil {
				t.Fatalf("read systemctl log: %v", err)
			}
			wantLog := "--user " + tc.command + " adb-god.service\n"
			if string(logBytes) != wantLog {
				t.Fatalf("systemctl log = %q, want %q", string(logBytes), wantLog)
			}
			if strings.TrimSpace(stdout.String()) != tc.stdout {
				t.Fatalf("stdout = %q, want %q", stdout.String(), tc.stdout)
			}
		})
	}
}

func TestRunDaemonServiceStatusReportsActiveAndEnabled(t *testing.T) {
	systemctlLog := filepath.Join(t.TempDir(), "systemctl.log")
	systemctlPath := writeFakeSystemctlStatus(t, systemctlLog, "active", "enabled", 0, 0)

	var stdout, stderr bytes.Buffer
	code := run([]string{"daemon", "service", "status", "--systemctl", systemctlPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run(daemon service status) exit code = %d, want 0; stderr = %q", code, stderr.String())
	}
	logBytes, err := os.ReadFile(systemctlLog)
	if err != nil {
		t.Fatalf("read systemctl log: %v", err)
	}
	wantLog := "--user is-active adb-god.service\n--user is-enabled adb-god.service\n"
	if string(logBytes) != wantLog {
		t.Fatalf("systemctl log = %q, want %q", string(logBytes), wantLog)
	}
	wantOut := "active: active\nenabled: enabled\n"
	if stdout.String() != wantOut {
		t.Fatalf("stdout = %q, want %q", stdout.String(), wantOut)
	}
}

func TestRunDaemonServiceStatusReportsInactiveAndDisabled(t *testing.T) {
	systemctlPath := writeFakeSystemctlStatus(t, filepath.Join(t.TempDir(), "systemctl.log"), "inactive", "disabled", 3, 1)

	var stdout, stderr bytes.Buffer
	code := run([]string{"daemon", "service", "status", "--systemctl", systemctlPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run(daemon service status) exit code = %d, want 0; stderr = %q", code, stderr.String())
	}
	wantOut := "active: inactive\nenabled: disabled\n"
	if stdout.String() != wantOut {
		t.Fatalf("stdout = %q, want %q", stdout.String(), wantOut)
	}
}

func TestRunDaemonServiceLogsInvokesJournalctlWithDefaultShape(t *testing.T) {
	journalctlLog := filepath.Join(t.TempDir(), "journalctl.log")
	journalctlPath := writeFakeJournalctl(t, journalctlLog, "recent daemon log\n")

	var stdout, stderr bytes.Buffer
	code := run([]string{"daemon", "service", "logs", "--journalctl", journalctlPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run(daemon service logs) exit code = %d, want 0; stderr = %q", code, stderr.String())
	}
	logBytes, err := os.ReadFile(journalctlLog)
	if err != nil {
		t.Fatalf("read journalctl log: %v", err)
	}
	wantLog := "--user -u adb-god.service -n 100 --no-pager\n"
	if string(logBytes) != wantLog {
		t.Fatalf("journalctl log = %q, want %q", string(logBytes), wantLog)
	}
	if stdout.String() != "recent daemon log\n" {
		t.Fatalf("stdout = %q, want fake journal output", stdout.String())
	}
}

func TestRunDaemonServiceLogsHonorsLinesAndFollow(t *testing.T) {
	journalctlLog := filepath.Join(t.TempDir(), "journalctl.log")
	journalctlPath := writeFakeJournalctl(t, journalctlLog, "follow log\n")

	var stdout, stderr bytes.Buffer
	code := run([]string{"daemon", "service", "logs", "--journalctl", journalctlPath, "--lines", "25", "--follow"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run(daemon service logs --lines --follow) exit code = %d, want 0; stderr = %q", code, stderr.String())
	}
	logBytes, err := os.ReadFile(journalctlLog)
	if err != nil {
		t.Fatalf("read journalctl log: %v", err)
	}
	wantLog := "--user -u adb-god.service -n 25 --no-pager -f\n"
	if string(logBytes) != wantLog {
		t.Fatalf("journalctl log = %q, want %q", string(logBytes), wantLog)
	}
	if stdout.String() != "follow log\n" {
		t.Fatalf("stdout = %q, want fake journal output", stdout.String())
	}
}

func TestRunDaemonServiceLogsRejectsNegativeLines(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"daemon", "service", "logs", "--lines", "-1"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("run(daemon service logs --lines -1) exit code = %d, want 2", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if got := stderr.String(); !strings.Contains(got, "--lines must be zero or greater") {
		t.Fatalf("stderr = %q, want lines validation error", got)
	}
}

func TestRunDaemonServiceUninstallDisablesStopsRemovesUnitAndReloads(t *testing.T) {
	unitDir := filepath.Join(t.TempDir(), "units")
	if err := os.MkdirAll(unitDir, 0o755); err != nil {
		t.Fatalf("create unit dir: %v", err)
	}
	unitPath := filepath.Join(unitDir, "adb-god.service")
	if err := os.WriteFile(unitPath, []byte("[Service]\n"), 0o644); err != nil {
		t.Fatalf("write unit: %v", err)
	}
	systemctlLog := filepath.Join(t.TempDir(), "systemctl.log")
	systemctlPath := writeFakeSystemctl(t, systemctlLog)

	var stdout, stderr bytes.Buffer
	code := run([]string{"daemon", "service", "uninstall", "--unit-dir", unitDir, "--systemctl", systemctlPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run(daemon service uninstall) exit code = %d, want 0; stderr = %q", code, stderr.String())
	}
	if _, err := os.Stat(unitPath); !os.IsNotExist(err) {
		t.Fatalf("unit after uninstall: err = %v, want not exist", err)
	}
	logBytes, err := os.ReadFile(systemctlLog)
	if err != nil {
		t.Fatalf("read systemctl log: %v", err)
	}
	got := string(logBytes)
	if !strings.Contains(got, "--user disable --now adb-god.service\n") || !strings.Contains(got, "--user daemon-reload\n") {
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
		"  '--user is-active adb-god.service') printf '%s\\n' " + strconv.Quote(active) + "; exit " + strconv.Itoa(activeCode) + " ;;",
		"  '--user is-enabled adb-god.service') printf '%s\\n' " + strconv.Quote(enabled) + "; exit " + strconv.Itoa(enabledCode) + " ;;",
		"  *) exit 99 ;;",
		"esac",
		"",
	}, "\n")
	if err := os.WriteFile(systemctlPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake systemctl status: %v", err)
	}
	return systemctlPath
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
