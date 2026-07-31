package compat

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"time"

	adb "github.com/dector/adb-go"
	"github.com/dector/adb-go/cmd/adb-go/internal/clidaemon"
	"github.com/dector/adb-go/internal/daemon"
)

var (
	defaultDaemonSocketPath = daemon.DefaultSocketPath
	sendDaemonRequest       = daemon.Send
	startDaemonProcess      = startADBGoDaemonProcess
	listUSBDevices          = adb.ListUSBDevices
)

const daemonStartupTimeout = 2 * time.Second

func runStartServer(args []string, opts globalOptions, stdout, stderr io.Writer) int {
	if len(args) != 0 {
		fmt.Fprintf(stderr, "adb: start-server: unexpected arguments %q\n", args)
		return 1
	}
	started, err := ensureDaemon(context.Background(), opts)
	if err != nil {
		fmt.Fprintf(stderr, "adb: failed to start daemon: %v\n", err)
		return 1
	}
	if started {
		fmt.Fprintln(stdout, "* daemon started successfully")
	}
	return 0
}

func runKillServer(args []string, opts globalOptions, stdout, stderr io.Writer) int {
	if len(args) != 0 {
		fmt.Fprintf(stderr, "adb: kill-server: unexpected arguments %q\n", args)
		return 1
	}
	socketPath, err := defaultDaemonSocketPath()
	if err != nil {
		fmt.Fprintf(stderr, "adb: kill-server: %v\n", err)
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), daemonStartupTimeout)
	defer cancel()
	resp, err := clidaemon.Send(ctx, socketPath, daemon.CommandShutdown, nil, sendDaemonRequest)
	if err != nil {
		// Official adb treats killing an absent server as success. Match that CLI
		// contract: after this command returns, no reachable server is required.
		return 0
	}
	if !resp.OK {
		fmt.Fprintf(stderr, "adb: kill-server: daemon error: %s\n", daemonErrorMessage(resp))
		return 1
	}
	_ = opts
	return 0
}

func runDevices(args []string, opts globalOptions, stdout, stderr io.Writer) int {
	if len(args) != 0 {
		fmt.Fprintf(stderr, "adb: devices: unexpected arguments %q\n", args)
		return 1
	}
	if _, err := ensureDaemon(context.Background(), opts); err != nil {
		fmt.Fprintf(stderr, "adb: devices: failed to start daemon: %v\n", err)
		return 1
	}
	socketPath, err := defaultDaemonSocketPath()
	if err != nil {
		fmt.Fprintf(stderr, "adb: devices: %v\n", err)
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), daemonStartupTimeout)
	defer cancel()
	resp, err := clidaemon.Send(ctx, socketPath, daemon.CommandDeviceList, nil, sendDaemonRequest)
	if err != nil {
		fmt.Fprintf(stderr, "adb: devices: query daemon: %v\n", err)
		return 1
	}
	if !resp.OK {
		fmt.Fprintf(stderr, "adb: devices: daemon error: %s\n", daemonErrorMessage(resp))
		return 1
	}
	devices, err := decodeDaemonDevices(resp.Result["devices"])
	if err != nil {
		fmt.Fprintf(stderr, "adb: devices: decode daemon response: %v\n", err)
		return 1
	}
	usbDevices, err := listUSBDevices(ctx)
	if err != nil && !errors.Is(err, adb.ErrUnsupported) {
		fmt.Fprintf(stderr, "adb: devices: list USB devices: %v\n", err)
		return 1
	}

	fmt.Fprintln(stdout, "List of devices attached")
	for _, device := range devices {
		fmt.Fprintf(stdout, "%s\t%s\n", device.Serial, device.State)
	}
	for _, device := range usbDevices {
		fmt.Fprintf(stdout, "usb:%03d:%03d\tdevice\n", device.BusNumber, device.DeviceNumber)
	}
	fmt.Fprintln(stdout)
	return 0
}

func ensureDaemon(ctx context.Context, opts globalOptions) (bool, error) {
	_ = opts // adb-go compat accepts -H/-P syntactically but uses local daemon internals.
	socketPath, err := defaultDaemonSocketPath()
	if err != nil {
		return false, err
	}
	probeCtx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	resp, err := clidaemon.Send(probeCtx, socketPath, daemon.CommandPing, nil, sendDaemonRequest)
	cancel()
	if err == nil && resp.OK {
		return false, nil
	}
	if err := startDaemonProcess(ctx, socketPath); err != nil {
		return false, err
	}
	deadline := time.Now().Add(daemonStartupTimeout)
	for time.Now().Before(deadline) {
		probeCtx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
		resp, err := clidaemon.Send(probeCtx, socketPath, daemon.CommandPing, nil, sendDaemonRequest)
		cancel()
		if err == nil && resp.OK {
			return true, nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return true, fmt.Errorf("daemon did not become ready at %s", socketPath)
}

func startADBGoDaemonProcess(ctx context.Context, socketPath string) error {
	cmd := exec.CommandContext(ctx, "adb-god", "--socket", socketPath)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start adb-god: %w", err)
	}
	if err := cmd.Process.Release(); err != nil {
		return fmt.Errorf("release adb-god process: %w", err)
	}
	return nil
}

func decodeDaemonDevices(v any) ([]daemon.Device, error) {
	var devices []daemon.Device
	if err := clidaemon.DecodeValue(v, &devices); err != nil {
		return nil, err
	}
	return devices, nil
}

func daemonErrorMessage(resp daemon.Response) string {
	if resp.Error == nil {
		return "unsuccessful response"
	}
	if resp.Error.Message == "" {
		return resp.Error.Code
	}
	return resp.Error.Code + ": " + resp.Error.Message
}
