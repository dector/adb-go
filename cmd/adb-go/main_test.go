package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	adb "github.com/dector/adb-go"
	"github.com/dector/adb-go/internal/fakeadb"
	"github.com/dector/adb-go/protocol"
)

func TestRunShowsUsageWithNoArgs(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := run(nil, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("run(nil) exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout.String(), "Usage:") {
		t.Fatalf("stdout = %q, want usage text", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestRunShowsUsageForHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := run([]string{"help"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("run(help) exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout.String(), "Commands:") {
		t.Fatalf("stdout = %q, want command list", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestRunUnknownCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := run([]string{"devices"}, &stdout, &stderr)

	if code != 2 {
		t.Fatalf("run(unknown) exit code = %d, want 2", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if got := stderr.String(); !strings.Contains(got, `unknown command "devices"`) || !strings.Contains(got, "Usage:") {
		t.Fatalf("stderr = %q, want unknown command error and usage", got)
	}
}

func TestRunTargetsScanListsDiscoveredTCPTargets(t *testing.T) {
	t.Setenv("ADB_GO_ADDR", "")
	var stdout, stderr bytes.Buffer
	restoreUSB := replaceListUSBDevices(func(ctx context.Context) ([]adb.USBDevice, error) {
		return nil, nil
	})
	defer restoreUSB()
	restoreScan := replaceScanTCPTargets(func(ctx context.Context, opts adb.TCPScanOptions) ([]adb.TCPTarget, error) {
		return []adb.TCPTarget{{Addr: "127.0.0.1:5555", Host: "127.0.0.1", Port: 5555}, {Addr: "127.0.0.1:5557", Host: "127.0.0.1", Port: 5557, AuthRequired: true}}, nil
	})
	defer restoreScan()

	code := run([]string{"targets", "--scan"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("run(targets --scan) exit code = %d, want 0; stderr = %q", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	got := stdout.String()
	for _, want := range []string{"TRANSPORT", "tcp\t--addr 127.0.0.1:5555\tscanned localhost emulator port", "tcp\t--addr 127.0.0.1:5557\tscanned localhost emulator port, auth required"} {
		if !strings.Contains(got, want) {
			t.Fatalf("stdout = %q, want substring %q", got, want)
		}
	}
}

func TestRunTargetsReportsScanErrors(t *testing.T) {
	t.Setenv("ADB_GO_ADDR", "")
	var stdout, stderr bytes.Buffer
	restoreUSB := replaceListUSBDevices(func(ctx context.Context) ([]adb.USBDevice, error) {
		return nil, nil
	})
	defer restoreUSB()
	restoreScan := replaceScanTCPTargets(func(ctx context.Context, opts adb.TCPScanOptions) ([]adb.TCPTarget, error) {
		return nil, errors.New("boom")
	})
	defer restoreScan()

	code := run([]string{"targets", "--scan"}, &stdout, &stderr)

	if code != 1 {
		t.Fatalf("run(targets --scan) exit code = %d, want 1", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if got := stderr.String(); !strings.Contains(got, "scan TCP targets: boom") {
		t.Fatalf("stderr = %q, want scan error", got)
	}
}

func TestRunTargetsListsEnvAndUSBTargets(t *testing.T) {
	t.Setenv("ADB_GO_ADDR", "127.0.0.1:5555")
	var stdout, stderr bytes.Buffer
	restore := replaceListUSBDevices(func(ctx context.Context) ([]adb.USBDevice, error) {
		return []adb.USBDevice{{DevicePath: "/dev/bus/usb/001/002", BusNumber: 1, DeviceNumber: 2, VendorID: 0x18d1, ProductID: 0x4ee7, InterfaceNumber: 3, BulkInEndpoint: 0x81, BulkOutEndpoint: 0x02}}, nil
	})
	defer restore()

	code := run([]string{"targets"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("run(targets) exit code = %d, want 0; stderr = %q", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	got := stdout.String()
	for _, want := range []string{"TRANSPORT", "tcp\t--addr 127.0.0.1:5555", "usb\t--usb-path /dev/bus/usb/001/002", "vid:pid=18d1:4ee7", "endpoints=in:0x81,out:0x02"} {
		if !strings.Contains(got, want) {
			t.Fatalf("stdout = %q, want substring %q", got, want)
		}
	}
}

func TestRunTargetsHandlesUnsupportedUSBWithoutTargets(t *testing.T) {
	t.Setenv("ADB_GO_ADDR", "")
	var stdout, stderr bytes.Buffer
	restore := replaceListUSBDevices(func(ctx context.Context) ([]adb.USBDevice, error) {
		return nil, adb.ErrUnsupported
	})
	defer restore()

	code := run([]string{"targets"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("run(targets unsupported USB) exit code = %d, want 0; stderr = %q", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	if got := stdout.String(); !strings.Contains(got, "No adb-go connection targets found") || !strings.Contains(got, "USB target discovery is Linux-only") {
		t.Fatalf("stdout = %q, want no-targets and unsupported USB guidance", got)
	}
}

func TestRunTargetsReportsUSBErrors(t *testing.T) {
	t.Setenv("ADB_GO_ADDR", "")
	var stdout, stderr bytes.Buffer
	restore := replaceListUSBDevices(func(ctx context.Context) ([]adb.USBDevice, error) {
		return nil, errors.New("permission denied")
	})
	defer restore()

	code := run([]string{"targets"}, &stdout, &stderr)

	if code != 1 {
		t.Fatalf("run(targets USB error) exit code = %d, want 1", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if got := stderr.String(); !strings.Contains(got, "list USB devices: permission denied") {
		t.Fatalf("stderr = %q, want USB error", got)
	}
}

func TestConnectionOptionsTargetFromAddrFlag(t *testing.T) {
	t.Setenv("ADB_GO_ADDR", "192.0.2.10:5555")
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	conn := addConnectionFlags(fs)
	if err := fs.Parse([]string{"--addr", "127.0.0.1:5555"}); err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	target, err := conn.target(fs)
	if err != nil {
		t.Fatalf("target() error = %v", err)
	}
	if target.usb {
		t.Fatal("target().usb = true, want false")
	}
	if target.tcpAddr != "127.0.0.1:5555" {
		t.Fatalf("target().tcpAddr = %q, want flag value", target.tcpAddr)
	}
}

func TestConnectionOptionsMissingTarget(t *testing.T) {
	t.Setenv("ADB_GO_ADDR", "")
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	conn := addConnectionFlags(fs)
	if err := fs.Parse(nil); err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if _, err := conn.target(fs); err == nil {
		t.Fatal("target() error = nil, want missing target error")
	}
}

func TestConnectionOptionsTargetFromEnvWhenFlagAbsent(t *testing.T) {
	t.Setenv("ADB_GO_ADDR", "  127.0.0.1:5555  ")
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	conn := addConnectionFlags(fs)
	if err := fs.Parse(nil); err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	target, err := conn.target(fs)
	if err != nil {
		t.Fatalf("target() error = %v", err)
	}
	if target.tcpAddr != "127.0.0.1:5555" {
		t.Fatalf("target().tcpAddr = %q, want trimmed env value", target.tcpAddr)
	}
}

func TestConnectionOptionsAddrFlagPreventsEnvFallback(t *testing.T) {
	t.Setenv("ADB_GO_ADDR", "127.0.0.1:5555")
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	conn := addConnectionFlags(fs)
	if err := fs.Parse([]string{"--addr", "  "}); err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if _, err := conn.target(fs); err == nil {
		t.Fatal("target() error = nil, want empty --addr error")
	}
}

func TestConnectionOptionsUSBSelection(t *testing.T) {
	t.Setenv("ADB_GO_ADDR", "")
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	conn := addConnectionFlags(fs)
	if err := fs.Parse([]string{"--usb-path", "/dev/bus/usb/001/002", "--usb-vid", "18d1", "--usb-pid", "0x4ee7"}); err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	target, err := conn.target(fs)
	if err != nil {
		t.Fatalf("target() error = %v", err)
	}
	if !target.usb {
		t.Fatal("target().usb = false, want true")
	}
	if target.usbOptions.DevicePath != "/dev/bus/usb/001/002" {
		t.Fatalf("USB DevicePath = %q, want fixture path", target.usbOptions.DevicePath)
	}
	if target.usbOptions.VendorID != 0x18d1 || target.usbOptions.ProductID != 0x4ee7 {
		t.Fatalf("USB VID/PID = %#x/%#x, want 0x18d1/0x4ee7", target.usbOptions.VendorID, target.usbOptions.ProductID)
	}
}

func TestConnectionOptionsAuthKeyLoadsCredentialForTCPAndUSB(t *testing.T) {
	keyPath := writeADBKeyFile(t)

	fs := flag.NewFlagSet("tcp", flag.ContinueOnError)
	conn := addConnectionFlags(fs)
	if err := fs.Parse([]string{"--addr", "127.0.0.1:5555", "--auth-key", keyPath}); err != nil {
		t.Fatalf("Parse(TCP) error = %v", err)
	}
	tcpTarget, err := conn.target(fs)
	if err != nil {
		t.Fatalf("target(TCP) error = %v", err)
	}
	if len(tcpTarget.auth) != 1 {
		t.Fatalf("TCP auth credentials = %d, want 1", len(tcpTarget.auth))
	}

	fs = flag.NewFlagSet("usb", flag.ContinueOnError)
	conn = addConnectionFlags(fs)
	if err := fs.Parse([]string{"--usb-path", "/dev/bus/usb/001/002", "--auth-key", keyPath}); err != nil {
		t.Fatalf("Parse(USB) error = %v", err)
	}
	usbTarget, err := conn.target(fs)
	if err != nil {
		t.Fatalf("target(USB) error = %v", err)
	}
	if len(usbTarget.auth) != 1 || len(usbTarget.usbOptions.AuthCredentials) != 1 {
		t.Fatalf("USB auth credentials target=%d options=%d, want 1/1", len(usbTarget.auth), len(usbTarget.usbOptions.AuthCredentials))
	}
}

func TestConnectionOptionsAuthKeyReportsLoadError(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	conn := addConnectionFlags(fs)
	if err := fs.Parse([]string{"--addr", "127.0.0.1:5555", "--auth-key", filepath.Join(t.TempDir(), "missing")}); err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	_, err := conn.target(fs)
	if err == nil || !strings.Contains(err.Error(), "load --auth-key") {
		t.Fatalf("target() error = %v, want auth key load error", err)
	}
}

func TestConnectionOptionsRejectsTCPAndUSBCombination(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	conn := addConnectionFlags(fs)
	if err := fs.Parse([]string{"--addr", "127.0.0.1:5555", "--usb"}); err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if _, err := conn.target(fs); err == nil {
		t.Fatal("target() error = nil, want TCP/USB conflict error")
	}
}

func TestRunShellUsesUSBConnection(t *testing.T) {
	var stdout, stderr bytes.Buffer
	var gotTarget connectionTarget
	restore := replaceConnectDevice(func(ctx context.Context, target connectionTarget) (deviceClient, error) {
		gotTarget = target
		return fakeCLIClient{shellOutput: "usb shell\n"}, nil
	})
	defer restore()

	code := run([]string{"shell", "--usb-path", "/dev/bus/usb/001/002", "echo", "hello"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("run(shell --usb-path) exit code = %d, want 0; stderr = %q", code, stderr.String())
	}
	if !gotTarget.usb || gotTarget.usbOptions.DevicePath != "/dev/bus/usb/001/002" {
		t.Fatalf("connect target = %+v, want USB path selection", gotTarget)
	}
	if stdout.String() != "usb shell\n" {
		t.Fatalf("stdout = %q, want USB shell output", stdout.String())
	}
}

func TestRunShellAuthRequiredSuggestsAuthKey(t *testing.T) {
	var stdout, stderr bytes.Buffer
	restore := replaceConnectDevice(func(ctx context.Context, target connectionTarget) (deviceClient, error) {
		return nil, adb.ErrAuthRequired
	})
	defer restore()

	code := run([]string{"shell", "--addr", "127.0.0.1:5555", "echo", "hello"}, &stdout, &stderr)

	if code != 1 {
		t.Fatalf("run(shell auth required) exit code = %d, want 1", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if got := stderr.String(); !strings.Contains(got, "device requires authentication") || !strings.Contains(got, "--auth-key PATH") {
		t.Fatalf("stderr = %q, want auth-key guidance", got)
	}
}

func TestRunRejectsConflictingTCPAndUSBFlags(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := run([]string{"shell", "--addr", "127.0.0.1:5555", "--usb", "echo", "hello"}, &stdout, &stderr)

	if code != 2 {
		t.Fatalf("run(conflicting connection flags) exit code = %d, want 2", code)
	}
	if got := stderr.String(); !strings.Contains(got, "cannot combine TCP --addr with USB") {
		t.Fatalf("stderr = %q, want TCP/USB conflict error", got)
	}
}

func TestRunShellMissingAddr(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := run([]string{"shell", "echo", "hello"}, &stdout, &stderr)

	if code != 2 {
		t.Fatalf("run(shell missing addr) exit code = %d, want 2", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if got := stderr.String(); !strings.Contains(got, "missing required --addr") || !strings.Contains(got, "adb-go shell --addr") {
		t.Fatalf("stderr = %q, want missing addr usage error", got)
	}
}

func TestRunShellMissingCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := run([]string{"shell", "--addr", "127.0.0.1:5555"}, &stdout, &stderr)

	if code != 2 {
		t.Fatalf("run(shell missing command) exit code = %d, want 2", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if got := stderr.String(); !strings.Contains(got, "missing shell command") || !strings.Contains(got, "adb-go shell --addr") {
		t.Fatalf("stderr = %q, want missing command usage error", got)
	}
}

func TestRunShellStreamsOutput(t *testing.T) {
	server := fakeadb.Start(t)
	server.Handle("shell:echo hello", writeCLIShellOutput(t, "hello\n"))
	var stdout, stderr bytes.Buffer

	code := run([]string{"shell", "--addr", server.Addr(), "echo", "hello"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("run(shell) exit code = %d, want 0; stderr = %q", code, stderr.String())
	}
	if stdout.String() != "hello\n" {
		t.Fatalf("stdout = %q, want hello newline", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestRunShellJoinsCommandArguments(t *testing.T) {
	server := fakeadb.Start(t)
	opened := make(chan string, 1)
	wantService := "shell:pm list packages | grep example"
	server.Handle(wantService, func(ctx context.Context, conn io.ReadWriter, open protocol.Message) {
		opened <- strings.TrimSuffix(string(open.Payload), "\x00")
		writeCLIShellOutput(t, "package:example\n")(ctx, conn, open)
	})
	var stdout, stderr bytes.Buffer

	code := run([]string{"shell", "--addr", server.Addr(), "pm", "list", "packages", "|", "grep", "example"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("run(shell) exit code = %d, want 0; stderr = %q", code, stderr.String())
	}
	if got := <-opened; got != wantService {
		t.Fatalf("opened service = %q, want %q", got, wantService)
	}
	if stdout.String() != "package:example\n" {
		t.Fatalf("stdout = %q, want package output", stdout.String())
	}
}

func TestRunShellConnectFailure(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := run([]string{"shell", "--addr", "127.0.0.1:1", "echo", "hello"}, &stdout, &stderr)

	if code != 1 {
		t.Fatalf("run(shell connect failure) exit code = %d, want 1", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if got := stderr.String(); !strings.Contains(got, "connect to 127.0.0.1:1") {
		t.Fatalf("stderr = %q, want connect error", got)
	}
}

func TestRunLogcatMissingAddr(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := run([]string{"logcat"}, &stdout, &stderr)

	if code != 2 {
		t.Fatalf("run(logcat missing addr) exit code = %d, want 2", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if got := stderr.String(); !strings.Contains(got, "missing required --addr") || !strings.Contains(got, "adb-go logcat --addr") {
		t.Fatalf("stderr = %q, want missing addr usage error", got)
	}
}

func TestRunLogcatRejectsUnexpectedArguments(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := run([]string{"logcat", "--addr", "127.0.0.1:5555", "*:I"}, &stdout, &stderr)

	if code != 2 {
		t.Fatalf("run(logcat unexpected arg) exit code = %d, want 2", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if got := stderr.String(); !strings.Contains(got, "unexpected arguments") || !strings.Contains(got, "adb-go logcat --addr") {
		t.Fatalf("stderr = %q, want unexpected arguments usage error", got)
	}
}

func TestRunLogcatUsesConnectionFlagsAndDumpOption(t *testing.T) {
	keyPath := writeADBKeyFile(t)
	var stdout, stderr bytes.Buffer
	var gotTarget connectionTarget
	var gotOpts adb.LogcatOptions
	restore := replaceConnectDevice(func(ctx context.Context, target connectionTarget) (deviceClient, error) {
		gotTarget = target
		return fakeCLIClient{logcat: func(ctx context.Context, stdout io.Writer, opts adb.LogcatOptions) error {
			gotOpts = opts
			_, err := io.WriteString(stdout, "dumped log\n")
			return err
		}}, nil
	})
	defer restore()

	code := run([]string{"logcat", "--addr", "127.0.0.1:5555", "--auth-key", keyPath, "--dump"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("run(logcat --dump) exit code = %d, want 0; stderr = %q", code, stderr.String())
	}
	if stdout.String() != "dumped log\n" {
		t.Fatalf("stdout = %q, want dumped log", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	if gotTarget.tcpAddr != "127.0.0.1:5555" || gotTarget.usb {
		t.Fatalf("connect target = %+v, want TCP target", gotTarget)
	}
	if len(gotTarget.auth) != 1 {
		t.Fatalf("target auth credentials = %d, want 1", len(gotTarget.auth))
	}
	if !gotOpts.Dump {
		t.Fatalf("LogcatOptions.Dump = false, want true")
	}
}

func TestRunLogcatUsesUSBConnection(t *testing.T) {
	var stdout, stderr bytes.Buffer
	var gotTarget connectionTarget
	restore := replaceConnectDevice(func(ctx context.Context, target connectionTarget) (deviceClient, error) {
		gotTarget = target
		return fakeCLIClient{logcat: func(ctx context.Context, stdout io.Writer, opts adb.LogcatOptions) error {
			if opts.Dump {
				t.Fatalf("LogcatOptions.Dump = true, want false")
			}
			_, err := io.WriteString(stdout, "usb log\n")
			return err
		}}, nil
	})
	defer restore()

	code := run([]string{"logcat", "--usb-path", "/dev/bus/usb/001/002"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("run(logcat --usb-path) exit code = %d, want 0; stderr = %q", code, stderr.String())
	}
	if !gotTarget.usb || gotTarget.usbOptions.DevicePath != "/dev/bus/usb/001/002" {
		t.Fatalf("connect target = %+v, want USB path selection", gotTarget)
	}
	if stdout.String() != "usb log\n" {
		t.Fatalf("stdout = %q, want USB log output", stdout.String())
	}
}

func TestRunLogcatStreamsOutput(t *testing.T) {
	server := fakeadb.Start(t)
	server.Handle("shell:logcat", writeCLIShellOutput(t, "01-02 03:04:05.678  123  456 I Tag: hello\n"))
	var stdout, stderr bytes.Buffer

	code := run([]string{"logcat", "--addr", server.Addr()}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("run(logcat) exit code = %d, want 0; stderr = %q", code, stderr.String())
	}
	if stdout.String() != "01-02 03:04:05.678  123  456 I Tag: hello\n" {
		t.Fatalf("stdout = %q, want log line", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestRunLogcatReportsStreamingFailure(t *testing.T) {
	var stdout, stderr bytes.Buffer
	restore := replaceConnectDevice(func(ctx context.Context, target connectionTarget) (deviceClient, error) {
		return fakeCLIClient{logcat: func(ctx context.Context, stdout io.Writer, opts adb.LogcatOptions) error {
			return errors.New("stream broke")
		}}, nil
	})
	defer restore()

	code := run([]string{"logcat", "--addr", "127.0.0.1:5555"}, &stdout, &stderr)

	if code != 1 {
		t.Fatalf("run(logcat failure) exit code = %d, want 1", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if got := stderr.String(); !strings.Contains(got, "stream broke") {
		t.Fatalf("stderr = %q, want stream failure", got)
	}
}

func TestRunPushMissingAddr(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := run([]string{"push", "./local.txt", "/data/local/tmp/local.txt"}, &stdout, &stderr)

	if code != 2 {
		t.Fatalf("run(push missing addr) exit code = %d, want 2", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if got := stderr.String(); !strings.Contains(got, "missing required --addr") || !strings.Contains(got, "adb-go push --addr") {
		t.Fatalf("stderr = %q, want missing addr usage error", got)
	}
}

func TestRunPushWrongArgCount(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "missing remote path", args: []string{"push", "--addr", "127.0.0.1:5555", "./local.txt"}},
		{name: "extra argument", args: []string{"push", "--addr", "127.0.0.1:5555", "./local.txt", "/remote.txt", "extra"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer

			code := run(tt.args, &stdout, &stderr)

			if code != 2 {
				t.Fatalf("run(push wrong arg count) exit code = %d, want 2", code)
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", stdout.String())
			}
			if got := stderr.String(); !strings.Contains(got, "requires exactly LOCAL_PATH and REMOTE_PATH") || !strings.Contains(got, "adb-go push --addr") {
				t.Fatalf("stderr = %q, want arg count usage error", got)
			}
		})
	}
}

func TestRunPushTransfersFile(t *testing.T) {
	server := fakeadb.Start(t)
	result := make(chan cliSyncPushResult, 1)
	server.Handle("sync:", cliSyncPushHandler(t, "/data/local/tmp/local.txt", result))
	localPath := filepath.Join(t.TempDir(), "local.txt")
	if err := os.WriteFile(localPath, []byte("hello pushed from cli"), 0o666); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	var stdout, stderr bytes.Buffer

	code := run([]string{"push", "--addr", server.Addr(), localPath, "/data/local/tmp/local.txt"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("run(push) exit code = %d, want 0; stderr = %q", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	got := <-result
	if got.contents != "hello pushed from cli" {
		t.Fatalf("pushed contents = %q, want hello pushed from cli", got.contents)
	}
	if got.path != "/data/local/tmp/local.txt" {
		t.Fatalf("pushed path = %q, want /data/local/tmp/local.txt", got.path)
	}
}

func TestRunInstallAPKMissingAddr(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := run([]string{"install-apk", "./app.apk"}, &stdout, &stderr)

	if code != 2 {
		t.Fatalf("run(install-apk missing addr) exit code = %d, want 2", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if got := stderr.String(); !strings.Contains(got, "missing required --addr") || !strings.Contains(got, "adb-go install-apk --addr") {
		t.Fatalf("stderr = %q, want missing addr usage error", got)
	}
}

func TestRunInstallAPKWrongArgCount(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "missing local apk", args: []string{"install-apk", "--addr", "127.0.0.1:5555"}},
		{name: "extra argument", args: []string{"install-apk", "--addr", "127.0.0.1:5555", "./app.apk", "extra"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer

			code := run(tt.args, &stdout, &stderr)

			if code != 2 {
				t.Fatalf("run(install-apk wrong arg count) exit code = %d, want 2", code)
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", stdout.String())
			}
			if got := stderr.String(); !strings.Contains(got, "requires exactly LOCAL_APK") || !strings.Contains(got, "adb-go install-apk --addr") {
				t.Fatalf("stderr = %q, want arg count usage error", got)
			}
		})
	}
}

func TestRunInstallAPKUsesConnectionFlagsAndReplaceOption(t *testing.T) {
	keyPath := writeADBKeyFile(t)
	var stdout, stderr bytes.Buffer
	var gotTarget connectionTarget
	var gotPath string
	var gotOpts adb.InstallOptions
	restore := replaceConnectDevice(func(ctx context.Context, target connectionTarget) (deviceClient, error) {
		gotTarget = target
		return fakeCLIClient{installAPKWithOptions: func(ctx context.Context, localPath string, opts adb.InstallOptions) error {
			gotPath = localPath
			gotOpts = opts
			return nil
		}}, nil
	})
	defer restore()

	code := run([]string{"install-apk", "--addr", "127.0.0.1:5555", "--auth-key", keyPath, "--replace", "./app.apk"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("run(install-apk --replace) exit code = %d, want 0; stderr = %q", code, stderr.String())
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("stdout/stderr = %q/%q, want empty", stdout.String(), stderr.String())
	}
	if gotTarget.tcpAddr != "127.0.0.1:5555" || gotTarget.usb {
		t.Fatalf("connect target = %+v, want TCP target", gotTarget)
	}
	if len(gotTarget.auth) != 1 {
		t.Fatalf("target auth credentials = %d, want 1", len(gotTarget.auth))
	}
	if gotPath != "./app.apk" {
		t.Fatalf("install path = %q, want ./app.apk", gotPath)
	}
	if !gotOpts.Replace {
		t.Fatalf("InstallOptions.Replace = false, want true")
	}
}

func TestRunInstallAPKUsesUSBConnection(t *testing.T) {
	var stdout, stderr bytes.Buffer
	var gotTarget connectionTarget
	restore := replaceConnectDevice(func(ctx context.Context, target connectionTarget) (deviceClient, error) {
		gotTarget = target
		return fakeCLIClient{installAPK: func(ctx context.Context, localPath string) error {
			if localPath != "./app.apk" {
				t.Fatalf("install path = %q, want ./app.apk", localPath)
			}
			return nil
		}}, nil
	})
	defer restore()

	code := run([]string{"install-apk", "--usb-path", "/dev/bus/usb/001/002", "./app.apk"}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("run(install-apk --usb-path) exit code = %d, want 0; stderr = %q", code, stderr.String())
	}
	if !gotTarget.usb || gotTarget.usbOptions.DevicePath != "/dev/bus/usb/001/002" {
		t.Fatalf("connect target = %+v, want USB path selection", gotTarget)
	}
}

func TestRunInstallAPKReportsPackageManagerFailure(t *testing.T) {
	var stdout, stderr bytes.Buffer
	restore := replaceConnectDevice(func(ctx context.Context, target connectionTarget) (deviceClient, error) {
		return fakeCLIClient{installAPK: func(ctx context.Context, localPath string) error {
			return errors.New("adb install apk \"./app.apk\": package manager failed: Failure [INSTALL_FAILED_ALREADY_EXISTS]")
		}}, nil
	})
	defer restore()

	code := run([]string{"install-apk", "--addr", "127.0.0.1:5555", "./app.apk"}, &stdout, &stderr)

	if code != 1 {
		t.Fatalf("run(install-apk failure) exit code = %d, want 1", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if got := stderr.String(); !strings.Contains(got, "package manager failed") || !strings.Contains(got, "INSTALL_FAILED_ALREADY_EXISTS") {
		t.Fatalf("stderr = %q, want package-manager failure output", got)
	}
}

func TestRunPullMissingAddr(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := run([]string{"pull", "/data/local/tmp/remote.txt", "./remote.txt"}, &stdout, &stderr)

	if code != 2 {
		t.Fatalf("run(pull missing addr) exit code = %d, want 2", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if got := stderr.String(); !strings.Contains(got, "missing required --addr") || !strings.Contains(got, "adb-go pull --addr") {
		t.Fatalf("stderr = %q, want missing addr usage error", got)
	}
}

func TestRunPullWrongArgCount(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "missing local path", args: []string{"pull", "--addr", "127.0.0.1:5555", "/data/local/tmp/remote.txt"}},
		{name: "extra argument", args: []string{"pull", "--addr", "127.0.0.1:5555", "/remote.txt", "./local.txt", "extra"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer

			code := run(tt.args, &stdout, &stderr)

			if code != 2 {
				t.Fatalf("run(pull wrong arg count) exit code = %d, want 2", code)
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", stdout.String())
			}
			if got := stderr.String(); !strings.Contains(got, "requires exactly REMOTE_PATH and LOCAL_PATH") || !strings.Contains(got, "adb-go pull --addr") {
				t.Fatalf("stderr = %q, want arg count usage error", got)
			}
		})
	}
}

func TestRunPullTransfersFile(t *testing.T) {
	server := fakeadb.Start(t)
	requested := make(chan string, 1)
	server.Handle("sync:", cliSyncPullHandler(t, "/data/local/tmp/remote.txt", "hello pulled from cli", requested))
	localPath := filepath.Join(t.TempDir(), "remote.txt")
	var stdout, stderr bytes.Buffer

	code := run([]string{"pull", "--addr", server.Addr(), "/data/local/tmp/remote.txt", localPath}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("run(pull) exit code = %d, want 0; stderr = %q", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	if got := <-requested; got != "/data/local/tmp/remote.txt" {
		t.Fatalf("pulled remote path = %q, want /data/local/tmp/remote.txt", got)
	}
	contents, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(contents) != "hello pulled from cli" {
		t.Fatalf("pulled contents = %q, want hello pulled from cli", string(contents))
	}
}

func TestRunPullOverwriteReplacesExistingDestination(t *testing.T) {
	server := fakeadb.Start(t)
	requested := make(chan string, 1)
	server.Handle("sync:", cliSyncPullHandler(t, "/data/local/tmp/remote.txt", "replacement", requested))
	localPath := filepath.Join(t.TempDir(), "remote.txt")
	if err := os.WriteFile(localPath, []byte("old contents"), 0o666); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	var stdout, stderr bytes.Buffer

	code := run([]string{"pull", "--addr", server.Addr(), "--overwrite", "/data/local/tmp/remote.txt", localPath}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("run(pull --overwrite) exit code = %d, want 0; stderr = %q", code, stderr.String())
	}
	if got := <-requested; got != "/data/local/tmp/remote.txt" {
		t.Fatalf("pulled remote path = %q, want /data/local/tmp/remote.txt", got)
	}
	contents, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(contents) != "replacement" {
		t.Fatalf("pulled contents after overwrite = %q, want replacement", string(contents))
	}
}

func writeCLIShellOutput(t testing.TB, output string) fakeadb.ServiceHandler {
	t.Helper()
	return func(ctx context.Context, conn io.ReadWriter, open protocol.Message) {
		remoteID := uint32(42)
		_ = protocol.WriteMessage(conn, protocol.Message{Command: protocol.CommandOKAY, Arg0: remoteID, Arg1: open.Arg0})
		_ = protocol.WriteMessage(conn, protocol.Message{Command: protocol.CommandWRTE, Arg0: remoteID, Arg1: open.Arg0, Payload: []byte(output)})
		_ = protocol.WriteMessage(conn, protocol.Message{Command: protocol.CommandCLSE, Arg0: remoteID, Arg1: open.Arg0})
	}
}

type cliSyncPushResult struct {
	path     string
	contents string
}

func cliSyncPushHandler(t testing.TB, wantPath string, result chan<- cliSyncPushResult) fakeadb.ServiceHandler {
	t.Helper()
	return func(ctx context.Context, conn io.ReadWriter, open protocol.Message) {
		remoteID := uint32(42)
		_ = protocol.WriteMessage(conn, protocol.Message{Command: protocol.CommandOKAY, Arg0: remoteID, Arg1: open.Arg0})

		var got cliSyncPushResult
		for {
			request, err := protocol.ReadMessage(conn)
			if err != nil {
				t.Errorf("cli sync push handler ReadMessage() error = %v", err)
				return
			}
			if request.Command != protocol.CommandWRTE {
				t.Errorf("cli sync push handler command = %#x, want WRTE", uint32(request.Command))
				return
			}
			_ = protocol.WriteMessage(conn, protocol.Message{Command: protocol.CommandOKAY, Arg0: remoteID, Arg1: open.Arg0})

			id, size, payload := parseCLISyncPacket(t, request.Payload)
			switch id {
			case "SEND":
				comma := bytes.LastIndexByte(payload, ',')
				if comma < 0 {
					t.Errorf("SEND payload = %q, want path,mode", string(payload))
					return
				}
				got.path = string(payload[:comma])
				if got.path != wantPath {
					t.Errorf("SEND path = %q, want %q", got.path, wantPath)
					return
				}
			case "DATA":
				got.contents += string(payload)
			case "DONE":
				if size == 0 {
					t.Errorf("DONE mtime = 0, want local file modification time")
					return
				}
				_ = protocol.WriteMessage(conn, protocol.Message{Command: protocol.CommandWRTE, Arg0: remoteID, Arg1: open.Arg0, Payload: cliSyncPacket("OKAY", nil)})
				_ = protocol.WriteMessage(conn, protocol.Message{Command: protocol.CommandCLSE, Arg0: remoteID, Arg1: open.Arg0})
				result <- got
				return
			default:
				t.Errorf("cli sync push request id = %q, want SEND/DATA/DONE", id)
				return
			}
		}
	}
}

func cliSyncPullHandler(t testing.TB, wantPath, contents string, requested chan<- string) fakeadb.ServiceHandler {
	t.Helper()
	return func(ctx context.Context, conn io.ReadWriter, open protocol.Message) {
		remoteID := uint32(42)
		_ = protocol.WriteMessage(conn, protocol.Message{Command: protocol.CommandOKAY, Arg0: remoteID, Arg1: open.Arg0})

		request, err := protocol.ReadMessage(conn)
		if err != nil {
			t.Errorf("cli sync pull handler ReadMessage() error = %v", err)
			return
		}
		if request.Command != protocol.CommandWRTE {
			t.Errorf("cli sync pull handler command = %#x, want WRTE", uint32(request.Command))
			return
		}
		_ = protocol.WriteMessage(conn, protocol.Message{Command: protocol.CommandOKAY, Arg0: remoteID, Arg1: open.Arg0})

		id, _, payload := parseCLISyncPacket(t, request.Payload)
		if id != "RECV" {
			t.Errorf("cli sync pull request id = %q, want RECV", id)
			return
		}
		path := string(payload)
		if path != wantPath {
			t.Errorf("RECV path = %q, want %q", path, wantPath)
			return
		}
		requested <- path

		_ = protocol.WriteMessage(conn, protocol.Message{Command: protocol.CommandWRTE, Arg0: remoteID, Arg1: open.Arg0, Payload: cliSyncPacket("DATA", []byte(contents))})
		_ = protocol.WriteMessage(conn, protocol.Message{Command: protocol.CommandWRTE, Arg0: remoteID, Arg1: open.Arg0, Payload: cliSyncPacket("DONE", nil)})
		_ = protocol.WriteMessage(conn, protocol.Message{Command: protocol.CommandCLSE, Arg0: remoteID, Arg1: open.Arg0})
	}
}

func parseCLISyncPacket(t testing.TB, payload []byte) (id string, size uint32, data []byte) {
	t.Helper()
	if len(payload) < 8 {
		t.Fatalf("sync packet payload length = %d, want at least 8", len(payload))
	}
	id = string(payload[:4])
	size = binary.LittleEndian.Uint32(payload[4:8])
	data = payload[8:]
	if id != "DONE" && len(data) != int(size) {
		t.Fatalf("sync packet data length = %d, want %d", len(data), size)
	}
	return id, size, data
}

func cliSyncPacket(id string, payload []byte) []byte {
	packet := make([]byte, 8+len(payload))
	copy(packet[:4], id)
	binary.LittleEndian.PutUint32(packet[4:8], uint32(len(payload)))
	copy(packet[8:], payload)
	return packet
}

func writeADBKeyFile(t testing.TB) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	data := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	path := filepath.Join(t.TempDir(), "adbkey")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("WriteFile(adbkey): %v", err)
	}
	return path
}

type fakeCLIClient struct {
	shellOutput           string
	logcat                func(ctx context.Context, stdout io.Writer, opts adb.LogcatOptions) error
	installAPK            func(ctx context.Context, localPath string) error
	installAPKWithOptions func(ctx context.Context, localPath string, opts adb.InstallOptions) error
}

func (c fakeCLIClient) Close() error { return nil }

func (c fakeCLIClient) ShellStream(ctx context.Context, cmd string, stdout io.Writer) error {
	_, err := io.WriteString(stdout, c.shellOutput)
	return err
}

func (c fakeCLIClient) Logcat(ctx context.Context, stdout io.Writer, opts adb.LogcatOptions) error {
	if c.logcat != nil {
		return c.logcat(ctx, stdout, opts)
	}
	return nil
}

func (c fakeCLIClient) PushFile(ctx context.Context, localPath, remotePath string) error { return nil }

func (c fakeCLIClient) PullFile(ctx context.Context, remotePath, localPath string) error { return nil }

func (c fakeCLIClient) PullFileWithOptions(ctx context.Context, remotePath, localPath string, opts adb.PullOptions) error {
	return nil
}

func (c fakeCLIClient) InstallAPK(ctx context.Context, localPath string) error {
	if c.installAPK != nil {
		return c.installAPK(ctx, localPath)
	}
	return nil
}

func (c fakeCLIClient) InstallAPKWithOptions(ctx context.Context, localPath string, opts adb.InstallOptions) error {
	if c.installAPKWithOptions != nil {
		return c.installAPKWithOptions(ctx, localPath, opts)
	}
	return nil
}

func replaceConnectDevice(fn func(context.Context, connectionTarget) (deviceClient, error)) func() {
	old := connectDevice
	connectDevice = fn
	return func() { connectDevice = old }
}

func replaceListUSBDevices(fn func(context.Context) ([]adb.USBDevice, error)) func() {
	old := listUSBDevices
	listUSBDevices = fn
	return func() { listUSBDevices = old }
}

func replaceScanTCPTargets(fn func(context.Context, adb.TCPScanOptions) ([]adb.TCPTarget, error)) func() {
	old := scanTCPTargets
	scanTCPTargets = fn
	return func() { scanTCPTargets = old }
}
