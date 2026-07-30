package compat

import (
	"bytes"
	"context"
	"encoding/json"
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
			assertContains(t, stdout, " -s SERIAL  use device with given serial")
			assertContains(t, stdout, " -d         use USB device")
			assertContains(t, stdout, " -e         use TCP/emulator device")
			assertContains(t, stdout, " devices      list connected devices")
			assertContains(t, stdout, " get-state    print selected device state")
			assertContains(t, stdout, " reverse      manage device-to-host reverse socket connections")
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

func TestParseTargetSelectors(t *testing.T) {
	t.Setenv("ANDROID_SERIAL", "env-serial")

	tests := []struct {
		name       string
		args       []string
		wantKind   targetSelectorKind
		wantSerial string
		wantCmd    string
		wantErr    string
	}{
		{name: "serial separate", args: []string{"-s", "device-1", "get-state"}, wantKind: targetSelectorSerial, wantSerial: "device-1", wantCmd: "get-state"},
		{name: "serial joined", args: []string{"-sdevice-2", "get-state"}, wantKind: targetSelectorSerial, wantSerial: "device-2", wantCmd: "get-state"},
		{name: "usb", args: []string{"-d", "get-state"}, wantKind: targetSelectorUSB, wantCmd: "get-state"},
		{name: "emulator", args: []string{"-e", "get-state"}, wantKind: targetSelectorEmulator, wantCmd: "get-state"},
		{name: "android serial", args: []string{"get-state"}, wantKind: targetSelectorSerial, wantSerial: "env-serial", wantCmd: "get-state"},
		{name: "serial beats android serial", args: []string{"-s", "flag-serial", "get-state"}, wantKind: targetSelectorSerial, wantSerial: "flag-serial", wantCmd: "get-state"},
		{name: "no command still help with selector", args: []string{"-s", "device-1"}, wantKind: targetSelectorSerial, wantSerial: "device-1", wantCmd: ""},
		{name: "missing serial", args: []string{"-s"}, wantErr: "option -s requires an argument"},
		{name: "conflicting selectors", args: []string{"-d", "-e", "get-state"}, wantErr: "more than one device/emulator selector specified"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts, cmd, _, err := parseGlobalOptions(tt.args)
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("parseGlobalOptions() error = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseGlobalOptions() error = %v", err)
			}
			if cmd != tt.wantCmd {
				t.Fatalf("command = %q, want %q", cmd, tt.wantCmd)
			}
			if opts.selector.kind != tt.wantKind || opts.selector.serial != tt.wantSerial {
				t.Fatalf("selector = (%d, %q), want (%d, %q)", opts.selector.kind, opts.selector.serial, tt.wantKind, tt.wantSerial)
			}
		})
	}
}

func TestRunSelectorWithoutCommandShowsHelp(t *testing.T) {
	stdout, stderr, code := runForTest("-s", "device-1")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
	assertContains(t, stdout, "Usage: adb [global options] command [command options]")
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

func TestRunGetStateResolvesSelectedTargets(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		devices  []daemon.Device
		usb      []adb.USBDevice
		wantOut  string
		wantErr  string
		wantCode int
	}{
		{name: "serial selects tcp device", args: []string{"-s", "tcp-1", "get-state"}, devices: []daemon.Device{{Serial: "tcp-1", State: daemon.DeviceStateDevice, Transport: "tcp", Address: "127.0.0.1:5555"}}, wantOut: "device\n"},
		{name: "android serial selects tcp device", args: []string{"get-state"}, devices: []daemon.Device{{Serial: "env-serial", State: daemon.DeviceStateOffline, Transport: "tcp", Address: "127.0.0.1:5555"}}, wantOut: "offline\n"},
		{name: "usb selector selects usb device", args: []string{"-d", "get-state"}, usb: []adb.USBDevice{{DevicePath: "/dev/bus/usb/001/002", BusNumber: 1, DeviceNumber: 2}}, wantOut: "device\n"},
		{name: "usb serial selects usb device", args: []string{"-s", "usb:001:002", "get-state"}, usb: []adb.USBDevice{{DevicePath: "/dev/bus/usb/001/002", BusNumber: 1, DeviceNumber: 2}}, wantOut: "device\n"},
		{name: "emulator selector selects tcp device", args: []string{"-e", "get-state"}, devices: []daemon.Device{{Serial: "127.0.0.1:5555", State: daemon.DeviceStateDevice, Transport: "tcp", Address: "127.0.0.1:5555"}}, wantOut: "device\n"},
		{name: "missing selected serial", args: []string{"-s", "missing", "get-state"}, wantErr: "adb: get-state: device \"missing\" not found\n", wantCode: 1},
		{name: "ambiguous default", args: []string{"get-state"}, devices: []daemon.Device{{Serial: "one", State: daemon.DeviceStateDevice, Transport: "tcp", Address: "127.0.0.1:5555"}, {Serial: "two", State: daemon.DeviceStateDevice, Transport: "tcp", Address: "127.0.0.1:5556"}}, wantErr: "adb: get-state: more than one device/emulator\n", wantCode: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("ANDROID_SERIAL", "")
			if strings.Contains(tt.name, "android serial") {
				t.Setenv("ANDROID_SERIAL", "env-serial")
			}
			fake := installFakeDaemon(t)
			fake.running = true
			fake.devices = tt.devices
			restoreUSB := replaceCompatListUSBDevices(func(ctx context.Context) ([]adb.USBDevice, error) { return tt.usb, nil })
			defer restoreUSB()

			stdout, stderr, code := runForTest(tt.args...)

			wantCode := tt.wantCode
			if wantCode == 0 && tt.wantErr == "" {
				wantCode = 0
			}
			if code != wantCode {
				t.Fatalf("exit code = %d, want %d (stderr %q)", code, wantCode, stderr)
			}
			if stdout != tt.wantOut {
				t.Fatalf("stdout = %q, want %q", stdout, tt.wantOut)
			}
			if stderr != tt.wantErr {
				t.Fatalf("stderr = %q, want %q", stderr, tt.wantErr)
			}
		})
	}
}

func TestCompatIgnoresADBGoAddrForTargetSelection(t *testing.T) {
	t.Setenv("ADB_GO_ADDR", "127.0.0.1:5555")
	fake := installFakeDaemon(t)
	fake.running = true

	stdout, stderr, code := runForTest("get-state")

	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty", stdout)
	}
	if stderr != "adb: get-state: no device/emulator found\n" {
		t.Fatalf("stderr = %q, want no-device error proving ADB_GO_ADDR is ignored", stderr)
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

func TestRunReverseCreateListRemoveAndRemoveAll(t *testing.T) {
	fake := installFakeDaemon(t)
	fake.running = true
	fake.devices = []daemon.Device{{Serial: "emulator-5555", State: daemon.DeviceStateDevice, Transport: "tcp", Address: "127.0.0.1:5555"}}

	stdout, stderr, code := runForTest("-s", "emulator-5555", "reverse", "tcp:8081", "tcp:3000")
	if code != 0 || stdout != "" || stderr != "" {
		t.Fatalf("create = code %d stdout %q stderr %q, want silent success", code, stdout, stderr)
	}
	if len(fake.reverses) != 1 || fake.reverses[0].Remote.Service != "tcp:8081" || fake.reverses[0].Local.Service != "tcp:3000" || fake.reverses[0].Target.Address != "127.0.0.1:5555" {
		t.Fatalf("created reverses = %#v", fake.reverses)
	}

	stdout, stderr, code = runForTest("reverse", "--list")
	if code != 0 || stderr != "" {
		t.Fatalf("list = code %d stderr %q", code, stderr)
	}
	if stdout != "127.0.0.1:5555 tcp:8081 tcp:3000\n" {
		t.Fatalf("list stdout = %q, want adb-shaped reverse row", stdout)
	}

	stdout, stderr, code = runForTest("-s", "emulator-5555", "reverse", "--remove", "tcp:8081")
	if code != 0 || stdout != "" || stderr != "" {
		t.Fatalf("remove = code %d stdout %q stderr %q, want silent success", code, stdout, stderr)
	}
	if len(fake.reverses) != 0 {
		t.Fatalf("reverses after remove = %#v, want empty", fake.reverses)
	}

	_, _, _ = runForTest("-s", "emulator-5555", "reverse", "tcp:8082", "tcp:3001")
	stdout, stderr, code = runForTest("reverse", "--remove-all")
	if code != 0 || stdout != "" || stderr != "" {
		t.Fatalf("remove-all = code %d stdout %q stderr %q, want silent success", code, stdout, stderr)
	}
	if len(fake.reverses) != 0 {
		t.Fatalf("reverses after remove-all = %#v, want empty", fake.reverses)
	}
}

func TestRunReverseSelectorAndValidationErrors(t *testing.T) {
	t.Run("unsupported endpoint", func(t *testing.T) {
		fake := installFakeDaemon(t)
		fake.running = true
		fake.devices = []daemon.Device{{Serial: "tcp-1", State: daemon.DeviceStateDevice, Transport: "tcp", Address: "127.0.0.1:5555"}}

		stdout, stderr, code := runForTest("-s", "tcp-1", "reverse", "localabstract:name", "tcp:3000")
		if code != 1 || stdout != "" {
			t.Fatalf("code/stdout = %d/%q, want 1/empty", code, stdout)
		}
		assertContains(t, stderr, "adb: reverse: adb reverse unsupported device endpoint")
	})

	t.Run("usb target unsupported", func(t *testing.T) {
		fake := installFakeDaemon(t)
		fake.running = true
		restoreUSB := replaceCompatListUSBDevices(func(ctx context.Context) ([]adb.USBDevice, error) {
			return []adb.USBDevice{{DevicePath: "/dev/bus/usb/001/002", BusNumber: 1, DeviceNumber: 2}}, nil
		})
		defer restoreUSB()

		stdout, stderr, code := runForTest("-d", "reverse", "tcp:8081", "tcp:3000")
		if code != 1 || stdout != "" {
			t.Fatalf("code/stdout = %d/%q, want 1/empty", code, stdout)
		}
		if stderr != "adb: reverse: USB reverse forwarding is not supported by adb-go compat yet\n" {
			t.Fatalf("stderr = %q", stderr)
		}
	})

	t.Run("no rebind maps daemon conflict", func(t *testing.T) {
		fake := installFakeDaemon(t)
		fake.running = true
		fake.devices = []daemon.Device{{Serial: "tcp-1", State: daemon.DeviceStateDevice, Transport: "tcp", Address: "127.0.0.1:5555"}}
		_, _, _ = runForTest("-s", "tcp-1", "reverse", "tcp:8081", "tcp:3000")

		stdout, stderr, code := runForTest("-s", "tcp-1", "reverse", "--no-rebind", "tcp:8081", "tcp:3001")
		if code != 1 || stdout != "" {
			t.Fatalf("code/stdout = %d/%q, want 1/empty", code, stdout)
		}
		assertContains(t, stderr, "adb: reverse: rebind_disallowed")
	})
}

func runForTest(args ...string) (stdout string, stderr string, code int) {
	var out, err bytes.Buffer
	code = Run(args, &out, &err)
	return out.String(), err.String(), code
}

type fakeCompatDaemon struct {
	running  bool
	starts   int
	devices  []daemon.Device
	reverses []daemon.Reverse
	nextRev  int
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
		case daemon.CommandReverseCreate:
			var params daemon.ReverseCreateParams
			if err := json.Unmarshal(req.Params, &params); err != nil {
				return daemon.Response{Version: daemon.ProtocolVersion, OK: false, Error: &daemon.Error{Code: daemon.ErrorBadRequest, Message: err.Error()}}, nil
			}
			for i, r := range fake.reverses {
				if r.Remote.Service == params.Remote.Service {
					if params.Norebind {
						return daemon.Response{Version: daemon.ProtocolVersion, OK: false, Error: &daemon.Error{Code: daemon.ErrorRebindDisallowed, Message: "reverse for remote endpoint " + params.Remote.Service + " already exists"}}, nil
					}
					fake.reverses = append(fake.reverses[:i], fake.reverses[i+1:]...)
					break
				}
			}
			fake.nextRev++
			rev := daemon.Reverse{ID: "rev_" + string(rune('0'+fake.nextRev)), State: daemon.ReverseStateListening, Remote: params.Remote, Local: params.Local, Target: params.Target, Norebind: params.Norebind}
			fake.reverses = append(fake.reverses, rev)
			return daemon.Response{Version: daemon.ProtocolVersion, OK: true, Result: map[string]any{"reverse": rev}}, nil
		case daemon.CommandReverseList:
			return daemon.Response{Version: daemon.ProtocolVersion, OK: true, Result: map[string]any{"reverses": fake.reverses}}, nil
		case daemon.CommandReverseRemove:
			var params daemon.ReverseRemoveParams
			if err := json.Unmarshal(req.Params, &params); err != nil {
				return daemon.Response{Version: daemon.ProtocolVersion, OK: false, Error: &daemon.Error{Code: daemon.ErrorBadRequest, Message: err.Error()}}, nil
			}
			for i, r := range fake.reverses {
				if (params.ID != "" && r.ID == params.ID) || (params.Remote != nil && r.Remote.Service == params.Remote.Service) {
					fake.reverses = append(fake.reverses[:i], fake.reverses[i+1:]...)
					return daemon.Response{Version: daemon.ProtocolVersion, OK: true, Result: map[string]any{"removed": 1}}, nil
				}
			}
			return daemon.Response{Version: daemon.ProtocolVersion, OK: false, Error: &daemon.Error{Code: daemon.ErrorReverseNotFound, Message: "reverse not found"}}, nil
		case daemon.CommandReverseRemoveAll:
			removed := len(fake.reverses)
			fake.reverses = nil
			return daemon.Response{Version: daemon.ProtocolVersion, OK: true, Result: map[string]any{"removed": removed}}, nil
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
