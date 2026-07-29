package compat

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	adb "github.com/dector/adb-go"
	"github.com/dector/adb-go/internal/daemon"
)

func TestRunHelpForms(t *testing.T) {
	for _, args := range [][]string{nil, []string{"help"}, []string{"--help"}, []string{"-h"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			stdout, stderr, code := runForTest(args...)

			if code != 0 {
				t.Fatalf("exit code = %d, want 0", code)
			}
			if stderr != "" {
				t.Fatalf("stderr = %q, want empty", stderr)
			}
			assertContains(t, stdout, "Android Debug Bridge version 1.0.41 (adb-go compat)")
			assertContains(t, stdout, "Usage: adb [global options] command [command options]")
			assertContains(t, stdout, " -H HOST    adb server host name [default=localhost]")
			assertContains(t, stdout, " help         show this help message")
			assertContains(t, stdout, " version      show version num")
			assertContains(t, stdout, " start-server ensure adb-go daemon is running")
			assertContains(t, stdout, " devices      list connected devices")
			assertNotContains(t, stdout, "targets")
			assertNotContains(t, stdout, "install-apk")
			assertNotContains(t, stdout, "ADB_GO_ADDR")
		})
	}
}

func TestRunVersion(t *testing.T) {
	oldVersion := Version
	Version = "test-version"
	defer func() { Version = oldVersion }()

	stdout, stderr, code := runForTest("version")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
	assertContains(t, stdout, "Android Debug Bridge version 1.0.41 (adb-go compat)\n")
	assertContains(t, stdout, "Version test-version\n")
	assertContains(t, stdout, "Installed as adb-go compat mode\n")
	assertContains(t, stdout, "Server target localhost:5037\n")
}

func TestRunVersionAcceptsHostAndPortGlobalOptions(t *testing.T) {
	stdout, stderr, code := runForTest("-H", "192.0.2.10", "-P", "7777", "version")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
	assertContains(t, stdout, "Server target 192.0.2.10:7777\n")
}

func TestRunVersionAcceptsJoinedHostAndPortGlobalOptions(t *testing.T) {
	stdout, stderr, code := runForTest("-Hlocalhost", "-P5038", "version")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
	assertContains(t, stdout, "Server target localhost:5038\n")
}

func TestRunUnknownCommand(t *testing.T) {
	stdout, stderr, code := runForTest("definitely-unknown")

	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty", stdout)
	}
	if stderr != "adb: unknown command definitely-unknown\n" {
		t.Fatalf("stderr = %q, want adb-shaped unknown command", stderr)
	}
}

func TestRunRejectsCustomOnlyFlags(t *testing.T) {
	for _, flag := range []string{"--addr", "--usb"} {
		t.Run(flag, func(t *testing.T) {
			stdout, stderr, code := runForTest(flag, "127.0.0.1:5555", "version")

			if code != 1 {
				t.Fatalf("exit code = %d, want 1", code)
			}
			if stdout != "" {
				t.Fatalf("stdout = %q, want empty", stdout)
			}
			assertContains(t, stderr, "adb: unknown option "+flag)
		})
	}
}

func TestRunGlobalHostOptionRequiresValue(t *testing.T) {
	stdout, stderr, code := runForTest("-H")

	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty", stdout)
	}
	if stderr != "adb: option -H requires an argument\n" {
		t.Fatalf("stderr = %q, want missing-argument error", stderr)
	}
}

func TestRunStartServerStartsAbsentDaemon(t *testing.T) {
	fake := installFakeDaemon(t)

	stdout, stderr, code := runForTest("start-server")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr %q)", code, stderr)
	}
	if stdout != "* daemon started successfully\n" {
		t.Fatalf("stdout = %q, want start message", stdout)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
	if fake.starts != 1 {
		t.Fatalf("daemon starts = %d, want 1", fake.starts)
	}
}

func TestRunStartServerIsQuietWhenDaemonAlreadyRunning(t *testing.T) {
	fake := installFakeDaemon(t)
	fake.running = true

	stdout, stderr, code := runForTest("start-server")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if stdout != "" || stderr != "" {
		t.Fatalf("stdout/stderr = %q/%q, want empty", stdout, stderr)
	}
	if fake.starts != 0 {
		t.Fatalf("daemon starts = %d, want 0", fake.starts)
	}
}

func TestRunKillServerStopsDaemonAndAllowsAbsentDaemon(t *testing.T) {
	fake := installFakeDaemon(t)
	fake.running = true

	stdout, stderr, code := runForTest("kill-server")
	if code != 0 || stdout != "" || stderr != "" {
		t.Fatalf("kill running = code %d stdout %q stderr %q, want silent success", code, stdout, stderr)
	}
	if fake.running {
		t.Fatal("daemon still running after kill-server")
	}

	stdout, stderr, code = runForTest("kill-server")
	if code != 0 || stdout != "" || stderr != "" {
		t.Fatalf("kill absent = code %d stdout %q stderr %q, want silent success", code, stdout, stderr)
	}
}

func TestRunDevicesAutoStartsAndPrintsNoDeviceHeader(t *testing.T) {
	fake := installFakeDaemon(t)

	stdout, stderr, code := runForTest("devices")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr %q)", code, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
	if stdout != "List of devices attached\n\n" {
		t.Fatalf("stdout = %q, want official no-device shape", stdout)
	}
	if fake.starts != 1 {
		t.Fatalf("daemon starts = %d, want 1", fake.starts)
	}
}

func TestRunDevicesPrintsDaemonTCPAndUSBDevices(t *testing.T) {
	fake := installFakeDaemon(t)
	fake.running = true
	fake.devices = []daemon.Device{{Serial: "127.0.0.1:5555", State: daemon.DeviceStateDevice, Transport: "tcp", Address: "127.0.0.1:5555"}}
	restoreUSB := replaceCompatListUSBDevices(func(ctx context.Context) ([]adb.USBDevice, error) {
		return []adb.USBDevice{{BusNumber: 1, DeviceNumber: 2}}, nil
	})
	defer restoreUSB()

	stdout, stderr, code := runForTest("devices")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr %q)", code, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
	assertContains(t, stdout, "List of devices attached\n")
	assertContains(t, stdout, "127.0.0.1:5555\tdevice\n")
	assertContains(t, stdout, "usb:001:002\tdevice\n")
}

func runForTest(args ...string) (stdout string, stderr string, code int) {
	var out, err bytes.Buffer
	code = Run(args, &out, &err)
	return out.String(), err.String(), code
}

type fakeCompatDaemon struct {
	running bool
	starts  int
	devices []daemon.Device
}

func installFakeDaemon(t *testing.T) *fakeCompatDaemon {
	t.Helper()
	fake := &fakeCompatDaemon{}
	oldDefault := defaultDaemonSocketPath
	oldSend := sendDaemonRequest
	oldStart := startDaemonProcess
	oldUSB := listUSBDevices
	defaultDaemonSocketPath = func() (string, error) { return "/tmp/adb-go-test.sock", nil }
	sendDaemonRequest = func(ctx context.Context, socketPath string, req daemon.Request) (daemon.Response, error) {
		if !fake.running {
			return daemon.Response{}, errors.New("daemon unavailable")
		}
		switch req.Command {
		case daemon.CommandPing:
			return daemon.Response{Version: daemon.ProtocolVersion, OK: true, Result: map[string]any{"message": "pong"}}, nil
		case daemon.CommandShutdown:
			fake.running = false
			return daemon.Response{Version: daemon.ProtocolVersion, OK: true, Result: map[string]any{"message": "shutting_down"}}, nil
		case daemon.CommandDeviceList:
			return daemon.Response{Version: daemon.ProtocolVersion, OK: true, Result: map[string]any{"devices": fake.devices}}, nil
		default:
			return daemon.Response{Version: daemon.ProtocolVersion, OK: false, Error: &daemon.Error{Code: daemon.ErrorUnknownCommand, Message: "unknown"}}, nil
		}
	}
	startDaemonProcess = func(ctx context.Context, socketPath string) error {
		fake.starts++
		fake.running = true
		return nil
	}
	listUSBDevices = func(ctx context.Context) ([]adb.USBDevice, error) { return nil, adb.ErrUnsupported }
	t.Cleanup(func() {
		defaultDaemonSocketPath = oldDefault
		sendDaemonRequest = oldSend
		startDaemonProcess = oldStart
		listUSBDevices = oldUSB
	})
	return fake
}

func replaceCompatListUSBDevices(fn func(context.Context) ([]adb.USBDevice, error)) func() {
	old := listUSBDevices
	listUSBDevices = fn
	return func() { listUSBDevices = old }
}

func assertContains(t *testing.T, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Fatalf("got %q, want to contain %q", got, want)
	}
}

func assertNotContains(t *testing.T, got, unwanted string) {
	t.Helper()
	if strings.Contains(got, unwanted) {
		t.Fatalf("got %q, want not to contain %q", got, unwanted)
	}
}
