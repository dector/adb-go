package compat

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"time"

	adb "github.com/dector/adb-go"
	"github.com/dector/adb-go/cmd/adb-go/internal/cliserver"
	"github.com/dector/adb-go/internal/server"
)

var (
	defaultServerSocketPath = server.DefaultSocketPath
	sendServerRequest       = server.Send
	startServerProcess      = startADBGoServerProcess
	listUSBDevices          = adb.ListUSBDevices
)

const serverStartupTimeout = 2 * time.Second

func runStartServer(args []string, opts globalOptions, stdout, stderr io.Writer) int {
	if len(args) != 0 {
		fmt.Fprintf(stderr, "adb: start-server: unexpected arguments %q\n", args)
		return 1
	}
	started, err := ensureServer(context.Background(), opts)
	if err != nil {
		fmt.Fprintf(stderr, "adb: failed to start server: %v\n", err)
		return 1
	}
	if started {
		fmt.Fprintln(stdout, "* server started successfully")
	}
	return 0
}

func runKillServer(args []string, opts globalOptions, stdout, stderr io.Writer) int {
	if len(args) != 0 {
		fmt.Fprintf(stderr, "adb: kill-server: unexpected arguments %q\n", args)
		return 1
	}
	socketPath, err := defaultServerSocketPath()
	if err != nil {
		fmt.Fprintf(stderr, "adb: kill-server: %v\n", err)
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), serverStartupTimeout)
	defer cancel()
	resp, err := cliserver.Send(ctx, socketPath, server.CommandShutdown, nil, sendServerRequest)
	if err != nil {
		// Official adb treats killing an absent server as success. Match that CLI
		// contract: after this command returns, no reachable server is required.
		return 0
	}
	if !resp.OK {
		fmt.Fprintf(stderr, "adb: kill-server: server error: %s\n", serverErrorMessage(resp))
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
	if _, err := ensureServer(context.Background(), opts); err != nil {
		fmt.Fprintf(stderr, "adb: devices: failed to start server: %v\n", err)
		return 1
	}
	socketPath, err := defaultServerSocketPath()
	if err != nil {
		fmt.Fprintf(stderr, "adb: devices: %v\n", err)
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), serverStartupTimeout)
	defer cancel()
	resp, err := cliserver.Send(ctx, socketPath, server.CommandDeviceList, nil, sendServerRequest)
	if err != nil {
		fmt.Fprintf(stderr, "adb: devices: query server: %v\n", err)
		return 1
	}
	if !resp.OK {
		fmt.Fprintf(stderr, "adb: devices: server error: %s\n", serverErrorMessage(resp))
		return 1
	}
	devices, err := decodeServerDevices(resp.Result["devices"])
	if err != nil {
		fmt.Fprintf(stderr, "adb: devices: decode server response: %v\n", err)
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

func ensureServer(ctx context.Context, opts globalOptions) (bool, error) {
	_ = opts // adb-go compat accepts -H/-P syntactically but uses local server internals.
	socketPath, err := defaultServerSocketPath()
	if err != nil {
		return false, err
	}
	probeCtx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	resp, err := cliserver.Send(probeCtx, socketPath, server.CommandPing, nil, sendServerRequest)
	cancel()
	if err == nil && resp.OK {
		return false, nil
	}
	if err := startServerProcess(ctx, socketPath); err != nil {
		return false, err
	}
	deadline := time.Now().Add(serverStartupTimeout)
	for time.Now().Before(deadline) {
		probeCtx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
		resp, err := cliserver.Send(probeCtx, socketPath, server.CommandPing, nil, sendServerRequest)
		cancel()
		if err == nil && resp.OK {
			return true, nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return true, fmt.Errorf("server did not become ready at %s", socketPath)
}

func startADBGoServerProcess(ctx context.Context, socketPath string) error {
	cmd := exec.CommandContext(ctx, "adb-gos", "--socket", socketPath)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start adb-gos: %w", err)
	}
	if err := cmd.Process.Release(); err != nil {
		return fmt.Errorf("release adb-gos process: %w", err)
	}
	return nil
}

func decodeServerDevices(v any) ([]server.Device, error) {
	var devices []server.Device
	if err := cliserver.DecodeValue(v, &devices); err != nil {
		return nil, err
	}
	return devices, nil
}

func serverErrorMessage(resp server.Response) string {
	if resp.Error == nil {
		return "unsuccessful response"
	}
	if resp.Error.Message == "" {
		return resp.Error.Code
	}
	return resp.Error.Code + ": " + resp.Error.Message
}
