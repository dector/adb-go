package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	adb "github.com/dector/adb-go"
	"github.com/dector/adb-go/internal/daemon"
)

const usage = `adb-go is a pure-Go Android Debug Bridge client.

Usage:
  adb-go <command> [arguments]

Commands:
  help        Show this help message
  version     Print adb-go build and runtime information
  targets     List adb-go connection targets visible locally
  shell       Run a shell command on a connected device
  logcat      Stream Android log output from a connected device
  getprop     Read Android system properties from a connected device
  screencap   Save a PNG screenshot from a connected device
  reboot      Reboot a connected device
  forward     Forward local TCP connections to a device TCP endpoint
  daemon      Control the local adb-god daemon
  push        Push one local file to a connected device
  pull        Pull one remote file from a connected device
  install-apk Install one local APK on a connected device

adb-go is not a full replacement for the official adb binary yet. The CLI is a
thin wrapper around the adb-go library and will grow command coverage gradually.
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

type deviceClient interface {
	Close() error
	ShellStream(ctx context.Context, cmd string, stdout io.Writer) error
	Logcat(ctx context.Context, stdout io.Writer, opts adb.LogcatOptions) error
	GetProp(ctx context.Context, name string) (string, error)
	Properties(ctx context.Context) (map[string]string, error)
	Screencap(ctx context.Context) ([]byte, error)
	Reboot(ctx context.Context, mode adb.RebootMode) error
	PushFile(ctx context.Context, localPath, remotePath string) error
	PullFile(ctx context.Context, remotePath, localPath string) error
	PullFileWithOptions(ctx context.Context, remotePath, localPath string, opts adb.PullOptions) error
	InstallAPK(ctx context.Context, localPath string) error
	InstallAPKWithOptions(ctx context.Context, localPath string, opts adb.InstallOptions) error
}

type connectionOptions struct {
	addr      *string
	usb       *bool
	serial    *string
	usbPath   *string
	usbBus    *int
	usbDevice *int
	usbVID    *string
	usbPID    *string
	authKey   *string
}

type connectionTarget struct {
	description string
	tcpAddr     string
	usb         bool
	usbOptions  adb.USBOptions
	auth        []adb.AuthCredential
}

var connectDevice = func(ctx context.Context, target connectionTarget) (deviceClient, error) {
	if target.usb {
		return adb.ConnectUSB(ctx, target.usbOptions)
	}
	return adb.ConnectWithOptions(ctx, target.tcpAddr, adb.ConnectOptions{AuthCredentials: target.auth})
}

var listUSBDevices = adb.ListUSBDevices
var scanTCPTargets = adb.ScanTCPTargets
var currentTime = time.Now

// version is intentionally a package variable so release builds can inject a
// concrete value with Go's standard linker flag, for example:
//
//	go build -ldflags "-X main.version=v0.1.0" ./cmd/adb-go
var version = "dev"

type forwardSession interface {
	LocalAddr() net.Addr
	Close() error
	Wait() error
}

var startForward = func(ctx context.Context, client deviceClient, localAddr string, remote adb.ForwardTarget) (forwardSession, error) {
	forwarder, ok := client.(interface {
		ForwardLocalTCP(context.Context, string, adb.ForwardTarget) (*adb.Forward, error)
	})
	if !ok {
		return nil, fmt.Errorf("connected client does not support forwarding")
	}
	return forwarder.ForwardLocalTCP(ctx, localAddr, remote)
}

func addConnectionFlags(fs *flag.FlagSet) connectionOptions {
	return connectionOptions{
		addr:      fs.String("addr", "", "explicit TCP device address, for example 127.0.0.1:5555"),
		usb:       fs.Bool("usb", false, "connect over USB instead of TCP; Linux-only initially"),
		serial:    fs.String("serial", "", "USB serial selector; reserved for future string descriptor support"),
		usbPath:   fs.String("usb-path", "", "Linux usbfs device path, for example /dev/bus/usb/001/002"),
		usbBus:    fs.Int("usb-bus", 0, "Linux usbfs bus number for USB selection"),
		usbDevice: fs.Int("usb-device", 0, "Linux usbfs device number for USB selection"),
		usbVID:    fs.String("usb-vid", "", "USB vendor ID, for example 18d1 or 0x18d1"),
		usbPID:    fs.String("usb-pid", "", "USB product ID, for example 4ee7 or 0x4ee7"),
		authKey:   fs.String("auth-key", "", "explicit ADB RSA private key path for authenticated devices"),
	}
}

func (o connectionOptions) target(fs *flag.FlagSet) (connectionTarget, error) {
	authCredentials, err := o.authCredentials()
	if err != nil {
		return connectionTarget{}, err
	}

	addrProvided := flagWasProvided(fs, "addr")
	usbRequested := *o.usb || flagWasProvided(fs, "usb-path") || flagWasProvided(fs, "serial") || flagWasProvided(fs, "usb-bus") || flagWasProvided(fs, "usb-device") || flagWasProvided(fs, "usb-vid") || flagWasProvided(fs, "usb-pid")
	if addrProvided && usbRequested {
		return connectionTarget{}, fmt.Errorf("cannot combine TCP --addr with USB selection flags")
	}

	if usbRequested {
		opts := adb.USBOptions{
			DevicePath:   strings.TrimSpace(*o.usbPath),
			Serial:       strings.TrimSpace(*o.serial),
			BusNumber:    *o.usbBus,
			DeviceNumber: *o.usbDevice,
		}
		opts.AuthCredentials = authCredentials
		if opts.VendorID, err = parseUSBID("--usb-vid", *o.usbVID); err != nil {
			return connectionTarget{}, err
		}
		if opts.ProductID, err = parseUSBID("--usb-pid", *o.usbPID); err != nil {
			return connectionTarget{}, err
		}
		return connectionTarget{description: "USB device", usb: true, usbOptions: opts, auth: authCredentials}, nil
	}

	if addrProvided {
		addr := strings.TrimSpace(*o.addr)
		if addr == "" {
			return connectionTarget{}, fmt.Errorf("missing required --addr value")
		}
		return connectionTarget{description: addr, tcpAddr: addr, auth: authCredentials}, nil
	}

	addr := strings.TrimSpace(os.Getenv("ADB_GO_ADDR"))
	if addr != "" {
		return connectionTarget{description: addr, tcpAddr: addr, auth: authCredentials}, nil
	}
	return connectionTarget{}, fmt.Errorf("missing required --addr, ADB_GO_ADDR, or USB selection")
}

func (o connectionOptions) authCredentials() ([]adb.AuthCredential, error) {
	path := strings.TrimSpace(*o.authKey)
	if path == "" {
		return nil, nil
	}
	credential, err := adb.LoadPrivateKey(path)
	if err != nil {
		return nil, fmt.Errorf("load --auth-key: %w", err)
	}
	return []adb.AuthCredential{credential}, nil
}

func parseUSBID(name, value string) (uint16, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	base := 0
	if !strings.HasPrefix(value, "0x") && !strings.HasPrefix(value, "0X") {
		base = 16
	}
	id, err := strconv.ParseUint(value, base, 16)
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q", name, value)
	}
	return uint16(id), nil
}

func flagWasProvided(fs *flag.FlagSet, name string) bool {
	provided := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			provided = true
		}
	})
	return provided
}

const targetsUsage = `Usage:
  adb-go targets [--scan]

Lists adb-go connection targets visible from the local machine. This is an
adb-go-specific alternative to "adb devices", not a clone of the official adb
server's device list. It can show ADB_GO_ADDR as a TCP target and, on Linux,
USB interfaces discovered under /dev/bus/usb. With --scan, it also scans local
emulator TCP ports 127.0.0.1:5555..5585, odd ports only.
`

func runTargets(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("targets", flag.ContinueOnError)
	scan := fs.Bool("scan", false, "scan localhost emulator TCP ports 5555..5585, odd ports only")
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, targetsUsage) }
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprint(stderr, "adb-go targets: unexpected arguments\n\n")
		fs.Usage()
		return 2
	}

	rows := []string{}
	if addr := strings.TrimSpace(os.Getenv("ADB_GO_ADDR")); addr != "" {
		rows = append(rows, fmt.Sprintf("tcp\t--addr %s\tfrom ADB_GO_ADDR", addr))
	}
	if *scan {
		targets, err := scanTCPTargets(context.Background(), adb.TCPScanOptions{})
		if err != nil {
			fmt.Fprintf(stderr, "adb-go targets: scan TCP targets: %v\n", err)
			return 1
		}
		for _, target := range targets {
			details := "scanned localhost emulator port"
			if target.AuthRequired {
				details += ", auth required"
			}
			rows = append(rows, fmt.Sprintf("tcp\t--addr %s\t%s", target.Addr, details))
		}
	}

	usbUnsupported := false
	devices, err := listUSBDevices(context.Background())
	if err != nil {
		if errors.Is(err, adb.ErrUnsupported) {
			usbUnsupported = true
		} else if os.IsNotExist(err) {
			// Treat a missing /dev/bus/usb tree as "no local USB targets" rather
			// than a hard failure. This keeps the command useful in containers and
			// minimal Linux environments without usbfs mounted.
		} else {
			fmt.Fprintf(stderr, "adb-go targets: list USB devices: %v\n", err)
			return 1
		}
	}
	for _, device := range devices {
		rows = append(rows, fmt.Sprintf("usb\t--usb-path %s\tbus=%03d device=%03d vid:pid=%04x:%04x interface=%d endpoints=in:%#02x,out:%#02x",
			device.DevicePath,
			device.BusNumber,
			device.DeviceNumber,
			device.VendorID,
			device.ProductID,
			device.InterfaceNumber,
			device.BulkInEndpoint,
			device.BulkOutEndpoint,
		))
	}

	if len(rows) == 0 {
		fmt.Fprintln(stdout, "No adb-go connection targets found.")
		fmt.Fprintln(stdout, "TCP targets are explicit: pass --addr HOST[:PORT], set ADB_GO_ADDR, or use targets --scan for local emulators.")
		if usbUnsupported {
			fmt.Fprintln(stdout, "USB target discovery is Linux-only in this version.")
		} else {
			fmt.Fprintln(stdout, "USB target discovery requires an ADB-capable device visible under /dev/bus/usb and sufficient permissions.")
		}
		return 0
	}

	fmt.Fprintln(stdout, "TRANSPORT\tSELECTOR\tDETAILS")
	for _, row := range rows {
		fmt.Fprintln(stdout, row)
	}
	return 0
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stdout, usage)
		return 0
	}

	switch args[0] {
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return 0
	case "version":
		return runVersion(args[1:], stdout, stderr)
	case "targets":
		return runTargets(args[1:], stdout, stderr)
	case "shell":
		return runShell(args[1:], stdout, stderr)
	case "logcat":
		return runLogcat(args[1:], stdout, stderr)
	case "getprop":
		return runGetProp(args[1:], stdout, stderr)
	case "screencap":
		return runScreencap(args[1:], stdout, stderr)
	case "reboot":
		return runReboot(args[1:], stdout, stderr)
	case "forward":
		return runForward(args[1:], stdout, stderr)
	case "daemon":
		return runDaemon(args[1:], stdout, stderr)
	case "push":
		return runPush(args[1:], stdout, stderr)
	case "pull":
		return runPull(args[1:], stdout, stderr)
	case "install-apk":
		return runInstallAPK(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "adb-go: unknown command %q\n\n", args[0])
		fmt.Fprint(stderr, usage)
		return 2
	}
}

type versionInfo struct {
	Version   string
	GoVersion string
	GOOS      string
	GOARCH    string
}

func currentVersionInfo() versionInfo {
	return versionInfo{
		Version:   version,
		GoVersion: runtime.Version(),
		GOOS:      runtime.GOOS,
		GOARCH:    runtime.GOARCH,
	}
}

func formatVersion(info versionInfo) string {
	return fmt.Sprintf("adb-go: %s\ngo: %s\nos: %s\narch: %s\n", info.Version, info.GoVersion, info.GOOS, info.GOARCH)
}

const versionUsage = `Usage:
  adb-go version

Prints concise adb-go build and Go runtime information for support requests.
Release builds can set the adb-go version at build time with:

  go build -ldflags "-X main.version=v0.1.0" ./cmd/adb-go
`

func runVersion(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("version", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, versionUsage) }
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprint(stderr, "adb-go version: unexpected arguments\n\n")
		fs.Usage()
		return 2
	}
	fmt.Fprint(stdout, formatVersion(currentVersionInfo()))
	return 0
}

func printConnectError(stderr io.Writer, command, description string, err error) {
	if errors.Is(err, adb.ErrAuthRequired) {
		fmt.Fprintf(stderr, "adb-go %s: connect to %s: device requires authentication; pass --auth-key PATH for an existing ADB private key: %v\n", command, description, err)
		return
	}
	fmt.Fprintf(stderr, "adb-go %s: connect to %s: %v\n", command, description, err)
}

const daemonUsage = `Usage:
  adb-go daemon [--socket PATH] COMMAND

Controls the local adb-god daemon over its Unix domain socket. This command
only exposes daemon process controls; adb-god does not persist devices,
transports, forwards, sessions, or authentication state yet.

Commands:
  doctor   Print read-only daemon socket and service diagnostics
  ping     Check whether adb-god responds to the control protocol
  status   Print basic adb-god process metadata
  stop     Request graceful adb-god shutdown
  service  Manage the adb-god system service integration

Socket path selection uses --socket when provided, otherwise ADB_GO_DAEMON_SOCKET,
then XDG_RUNTIME_DIR, then a per-user temporary directory. The service install
command writes ~/.config/systemd/user/adb-god.service by default and runs
systemctl --user daemon-reload followed by systemctl --user enable --now.
`

func runDaemon(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("daemon", flag.ContinueOnError)
	fs.SetOutput(stderr)
	socketPathFlag := fs.String("socket", "", "absolute Unix domain socket path")
	fs.Usage = func() { fmt.Fprint(stderr, daemonUsage) }
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() == 0 {
		fmt.Fprint(stderr, "adb-go daemon: requires COMMAND\n\n")
		fs.Usage()
		return 2
	}

	socketPath := strings.TrimSpace(*socketPathFlag)
	if socketPath == "" {
		var err error
		socketPath, err = daemon.DefaultSocketPath()
		if err != nil {
			fmt.Fprintf(stderr, "adb-go daemon: %v\n", err)
			return 1
		}
	}

	command := fs.Arg(0)
	commandArgs := fs.Args()[1:]
	if command == "service" {
		return runDaemonService(commandArgs, socketPath, stdout, stderr)
	}
	if command == "doctor" {
		return runDaemonDoctor(commandArgs, socketPath, stdout, stderr)
	}
	switch command {
	case daemon.CommandPing, daemon.CommandStatus, "stop":
		if len(commandArgs) != 0 {
			fmt.Fprintf(stderr, "adb-go daemon %s: unexpected arguments %q\n\n", command, commandArgs)
			fs.Usage()
			return 2
		}
	default:
		fmt.Fprintf(stderr, "adb-go daemon: unknown daemon command %q\n\n", command)
		fs.Usage()
		return 2
	}

	protocolCommand := command
	if command == "stop" {
		protocolCommand = daemon.CommandShutdown
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	resp, err := daemon.Send(ctx, socketPath, daemon.Request{Version: daemon.ProtocolVersion, Command: protocolCommand})
	if err != nil {
		fmt.Fprintf(stderr, "adb-go daemon %s: daemon is not running or socket is unavailable at %s: %v\n", command, socketPath, err)
		return 1
	}
	if !resp.OK {
		if resp.Error != nil {
			fmt.Fprintf(stderr, "adb-go daemon %s: daemon error %s: %s\n", command, resp.Error.Code, resp.Error.Message)
		} else {
			fmt.Fprintf(stderr, "adb-go daemon %s: daemon returned an unsuccessful response\n", command)
		}
		return 1
	}

	switch command {
	case daemon.CommandPing:
		fmt.Fprintln(stdout, "pong")
	case daemon.CommandStatus:
		printDaemonStatus(stdout, resp.Result)
	case "stop":
		fmt.Fprintln(stdout, "adb-god shutting down")
	}
	return 0
}

func printDaemonStatus(stdout io.Writer, result map[string]any) {
	fmt.Fprintf(stdout, "state: %v\n", result["state"])
	fmt.Fprintf(stdout, "pid: %v\n", result["pid"])
	fmt.Fprintf(stdout, "socketPath: %v\n", result["socketPath"])
	fmt.Fprintf(stdout, "protocolVersion: %v\n", result["protocolVersion"])
	fmt.Fprintf(stdout, "uptimeMillis: %v\n", result["uptimeMillis"])
}

const daemonDoctorUsage = `Usage:
  adb-go daemon [--socket PATH] doctor [--systemctl PATH]

Prints read-only diagnostics for the local adb-god daemon. The command reports
which socket path adb-go resolved, whether that path exists, whether a
compatible daemon responds to the socket protocol, and, on Linux when systemctl
is available, adb-god.service active/enabled state.
`

func runDaemonDoctor(args []string, socketPath string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("daemon doctor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	systemctlFlag := fs.String("systemctl", "systemctl", "systemctl binary path")
	fs.Usage = func() { fmt.Fprint(stderr, daemonDoctorUsage) }
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(stderr, "adb-go daemon doctor: unexpected arguments %q\n\n", fs.Args())
		fs.Usage()
		return 2
	}

	hints := []string{}
	fmt.Fprintf(stdout, "socketPath: %s\n", socketPath)
	if info, err := os.Lstat(socketPath); err == nil {
		fmt.Fprintln(stdout, "socketExists: true")
		if info.Mode()&os.ModeSocket == 0 {
			fmt.Fprintf(stdout, "socketType: %s\n", info.Mode().Type())
			hints = append(hints, "The socket path exists but is not a Unix socket; inspect it before starting adb-god with this path.")
		} else {
			fmt.Fprintln(stdout, "socketType: unix")
		}
	} else if errors.Is(err, os.ErrNotExist) {
		fmt.Fprintln(stdout, "socketExists: false")
		hints = append(hints, "No daemon socket exists at this path; start adb-god with a matching -socket path or run adb-go daemon service start if the service is installed.")
	} else {
		fmt.Fprintf(stdout, "socketExists: unknown (%v)\n", err)
		hints = append(hints, "adb-go could not inspect the socket path; check parent directory permissions.")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	resp, err := daemon.Send(ctx, socketPath, daemon.Request{Version: daemon.ProtocolVersion, Command: daemon.CommandStatus})
	if err != nil {
		fmt.Fprintf(stdout, "daemonProtocol: not responding (%v)\n", err)
		hints = append(hints, "No compatible adb-god daemon answered the control protocol; verify the daemon process and socket path match.")
	} else if !resp.OK {
		fmt.Fprintln(stdout, "daemonProtocol: error")
		if resp.Error != nil {
			fmt.Fprintf(stdout, "daemonError: %s: %s\n", resp.Error.Code, resp.Error.Message)
		}
		hints = append(hints, "The daemon socket answered but returned an error; check that adb-go and adb-god are compatible builds.")
	} else {
		fmt.Fprintln(stdout, "daemonProtocol: responding")
		fmt.Fprintf(stdout, "daemonState: %v\n", resp.Result["state"])
		fmt.Fprintf(stdout, "daemonProtocolVersion: %v\n", resp.Result["protocolVersion"])
	}

	if runtime.GOOS == "linux" {
		active, enabled, ok, hint := daemonDoctorSystemdStatus(strings.TrimSpace(*systemctlFlag))
		if ok {
			fmt.Fprintf(stdout, "systemdActive: %s\n", active)
			fmt.Fprintf(stdout, "systemdEnabled: %s\n", enabled)
			if active != "active" {
				hints = append(hints, "adb-god.service is not active; run adb-go daemon service start to start the installed user service.")
			}
			if enabled != "enabled" {
				hints = append(hints, "adb-god.service is not enabled; run adb-go daemon service install or systemctl --user enable adb-god.service to start it automatically.")
			}
		} else {
			fmt.Fprintln(stdout, "systemdUserService: unavailable")
			if hint != "" {
				hints = append(hints, hint)
			}
		}
	} else {
		fmt.Fprintln(stdout, "systemdUserService: unsupported on this platform")
	}

	if len(hints) == 0 {
		fmt.Fprintln(stdout, "hints: none")
	} else {
		fmt.Fprintln(stdout, "hints:")
		for _, hint := range hints {
			fmt.Fprintf(stdout, "  - %s\n", hint)
		}
	}
	return 0
}

func daemonDoctorSystemdStatus(systemctl string) (active, enabled string, ok bool, hint string) {
	if systemctl == "" {
		systemctl = "systemctl"
	}
	if !systemctlAvailable(systemctl) {
		return "", "", false, "systemctl is not available on PATH; install systemd tools or pass --systemctl PATH to inspect adb-god.service."
	}
	active, activeErr := systemctlUserOutput(systemctl, "is-active", "adb-god.service")
	enabled, enabledErr := systemctlUserOutput(systemctl, "is-enabled", "adb-god.service")
	if active == "" && activeErr != nil {
		return "", "", false, fmt.Sprintf("systemctl could not read adb-god.service active state: %v", activeErr)
	}
	if enabled == "" && enabledErr != nil {
		return "", "", false, fmt.Sprintf("systemctl could not read adb-god.service enabled state: %v", enabledErr)
	}
	return active, enabled, true, ""
}

func systemctlAvailable(systemctl string) bool {
	if strings.ContainsRune(systemctl, os.PathSeparator) {
		info, err := os.Stat(systemctl)
		return err == nil && !info.IsDir()
	}
	_, err := exec.LookPath(systemctl)
	return err == nil
}

const daemonServiceUsage = `Usage:
  adb-go daemon [--socket PATH] service COMMAND

Manages adb-god's host service-manager integration. Currently implemented:

  install    Install and start adb-god as a systemd user service on Linux
  reinstall  Rewrite, reload, enable, and restart the systemd user service
  start      Start adb-god.service with systemd --user
  stop       Stop adb-god.service with systemd --user
  restart    Restart adb-god.service with systemd --user
  status     Report adb-god.service active/enabled state with systemd --user
  logs       Show recent adb-god.service logs with journalctl --user
  uninstall  Disable, stop, and remove the systemd user service

These host service commands are separate from daemon protocol commands like
ping/status/shutdown.
`

const daemonServiceInstallUsage = `Usage:
  adb-go daemon [--socket PATH] service install [--adb-god PATH] [--unit-dir DIR] [--systemctl PATH] [--no-enable]

Installs adb-god as a systemd user service on Linux. The command writes an
adb-god.service unit for the current user. Unless --no-enable is set, it then
reloads the user systemd manager and enables/starts the service with:

  systemctl --user daemon-reload
  systemctl --user enable --now adb-god.service
`

const daemonServiceReinstallUsage = `Usage:
  adb-go daemon [--socket PATH] service reinstall [--adb-god PATH] [--unit-dir DIR] [--systemctl PATH]

Rewrites the adb-god systemd user unit on Linux, reloads the user systemd
manager, enables the service, and restarts adb-god.service. Use reinstall after
changing the adb-god binary path, changing the daemon socket path, or upgrading
a locally built daemon binary whose service unit should be refreshed.
`

const daemonServiceLifecycleUsage = `Usage:
  adb-go daemon service COMMAND [--systemctl PATH]

Starts, stops, or restarts adb-god.service using systemd --user. COMMAND must be
start, stop, or restart.
`

const daemonServiceStatusUsage = `Usage:
  adb-go daemon service status [--systemctl PATH]

Reports adb-god.service state from the current user's systemd manager. This is
host service-manager state, not the live daemon socket protocol status.
`

const daemonServiceLogsUsage = `Usage:
  adb-go daemon service logs [--journalctl PATH] [--lines N] [--follow]

Shows adb-god.service logs from the current user's systemd journal. By default,
it prints the most recent 100 entries without opening a pager. Pass --lines N to
choose a different number of recent entries, or --follow to keep streaming new
entries after the initial output.
`

const daemonServiceUninstallUsage = `Usage:
  adb-go daemon service uninstall [--unit-dir DIR] [--systemctl PATH] [--keep-unit]

Disables and stops adb-god.service using systemd --user, removes the systemd
user unit file, and reloads the user systemd manager. Use --keep-unit to leave
the unit file in place after disabling/stopping the service.
`

func runDaemonService(args []string, socketPath string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("daemon service", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, daemonServiceUsage) }
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() == 0 {
		fmt.Fprint(stderr, "adb-go daemon service: requires COMMAND\n\n")
		fs.Usage()
		return 2
	}

	command := fs.Arg(0)
	commandArgs := fs.Args()[1:]
	switch command {
	case "install":
		return runDaemonServiceInstall(commandArgs, socketPath, stdout, stderr)
	case "reinstall":
		return runDaemonServiceReinstall(commandArgs, socketPath, stdout, stderr)
	case "start", "stop", "restart":
		return runDaemonServiceLifecycle(command, commandArgs, stdout, stderr)
	case "status":
		return runDaemonServiceStatus(commandArgs, stdout, stderr)
	case "logs":
		return runDaemonServiceLogs(commandArgs, stdout, stderr)
	case "uninstall":
		return runDaemonServiceUninstall(commandArgs, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "adb-go daemon service: unknown service command %q\n\n", command)
		fs.Usage()
		return 2
	}
}

func runDaemonServiceInstall(args []string, socketPath string, stdout, stderr io.Writer) int {
	if runtime.GOOS != "linux" {
		fmt.Fprintln(stderr, "adb-go daemon service install: systemd user services are supported on Linux only")
		return 1
	}
	fs := flag.NewFlagSet("daemon service install", flag.ContinueOnError)
	fs.SetOutput(stderr)
	adbGodPathFlag := fs.String("adb-god", "", "absolute path to the adb-god binary")
	unitDirFlag := fs.String("unit-dir", "", "systemd user unit directory")
	systemctlFlag := fs.String("systemctl", "systemctl", "systemctl binary path")
	noEnableFlag := fs.Bool("no-enable", false, "write the unit but do not run systemctl")
	fs.Usage = func() { fmt.Fprint(stderr, daemonServiceInstallUsage) }
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(stderr, "adb-go daemon service install: unexpected arguments %q\n\n", fs.Args())
		fs.Usage()
		return 2
	}

	adbGodPath, err := resolveADBGodPath(strings.TrimSpace(*adbGodPathFlag))
	if err != nil {
		fmt.Fprintf(stderr, "adb-go daemon service install: %v\n", err)
		return 1
	}
	unitDir, err := systemdUserUnitDir(strings.TrimSpace(*unitDirFlag))
	if err != nil {
		fmt.Fprintf(stderr, "adb-go daemon service install: %v\n", err)
		return 1
	}
	unitPath, err := writeDaemonServiceUnit(unitDir, adbGodPath, socketPath)
	if err != nil {
		fmt.Fprintf(stderr, "adb-go daemon service install: %v\n", err)
		return 1
	}

	if !*noEnableFlag {
		if code := runSystemctlUser(stderr, *systemctlFlag, "daemon-reload"); code != 0 {
			return code
		}
		if code := runSystemctlUser(stderr, *systemctlFlag, "enable", "--now", "adb-god.service"); code != 0 {
			return code
		}
	}

	fmt.Fprintf(stdout, "installed %s\n", unitPath)
	if *noEnableFlag {
		fmt.Fprintln(stdout, "systemctl enable/start skipped")
	} else {
		fmt.Fprintln(stdout, "adb-god.service enabled and started for the current user")
	}
	return 0
}

func runDaemonServiceReinstall(args []string, socketPath string, stdout, stderr io.Writer) int {
	if runtime.GOOS != "linux" {
		fmt.Fprintln(stderr, "adb-go daemon service reinstall: systemd user services are supported on Linux only")
		return 1
	}
	fs := flag.NewFlagSet("daemon service reinstall", flag.ContinueOnError)
	fs.SetOutput(stderr)
	adbGodPathFlag := fs.String("adb-god", "", "absolute path to the adb-god binary")
	unitDirFlag := fs.String("unit-dir", "", "systemd user unit directory")
	systemctlFlag := fs.String("systemctl", "systemctl", "systemctl binary path")
	fs.Usage = func() { fmt.Fprint(stderr, daemonServiceReinstallUsage) }
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(stderr, "adb-go daemon service reinstall: unexpected arguments %q\n\n", fs.Args())
		fs.Usage()
		return 2
	}

	adbGodPath, err := resolveADBGodPath(strings.TrimSpace(*adbGodPathFlag))
	if err != nil {
		fmt.Fprintf(stderr, "adb-go daemon service reinstall: %v\n", err)
		return 1
	}
	unitDir, err := systemdUserUnitDir(strings.TrimSpace(*unitDirFlag))
	if err != nil {
		fmt.Fprintf(stderr, "adb-go daemon service reinstall: %v\n", err)
		return 1
	}
	unitPath, err := writeDaemonServiceUnit(unitDir, adbGodPath, socketPath)
	if err != nil {
		fmt.Fprintf(stderr, "adb-go daemon service reinstall: %v\n", err)
		return 1
	}

	if code := runSystemctlUser(stderr, *systemctlFlag, "daemon-reload"); code != 0 {
		return code
	}
	if code := runSystemctlUser(stderr, *systemctlFlag, "enable", "adb-god.service"); code != 0 {
		return code
	}
	if code := runSystemctlUser(stderr, *systemctlFlag, "restart", "adb-god.service"); code != 0 {
		return code
	}

	fmt.Fprintf(stdout, "reinstalled %s\n", unitPath)
	fmt.Fprintln(stdout, "adb-god.service enabled and restarted for the current user")
	return 0
}

func runDaemonServiceLifecycle(command string, args []string, stdout, stderr io.Writer) int {
	if runtime.GOOS != "linux" {
		fmt.Fprintf(stderr, "adb-go daemon service %s: systemd user services are supported on Linux only\n", command)
		return 1
	}
	fs := flag.NewFlagSet("daemon service "+command, flag.ContinueOnError)
	fs.SetOutput(stderr)
	systemctlFlag := fs.String("systemctl", "systemctl", "systemctl binary path")
	fs.Usage = func() { fmt.Fprint(stderr, daemonServiceLifecycleUsage) }
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(stderr, "adb-go daemon service %s: unexpected arguments %q\n\n", command, fs.Args())
		fs.Usage()
		return 2
	}
	if code := runSystemctlUser(stderr, *systemctlFlag, command, "adb-god.service"); code != 0 {
		return code
	}
	fmt.Fprintf(stdout, "adb-god.service %s\n", serviceLifecyclePastTense(command))
	return 0
}

func serviceLifecyclePastTense(command string) string {
	switch command {
	case "start":
		return "started"
	case "stop":
		return "stopped"
	case "restart":
		return "restarted"
	default:
		return command
	}
}

func runDaemonServiceStatus(args []string, stdout, stderr io.Writer) int {
	if runtime.GOOS != "linux" {
		fmt.Fprintln(stderr, "adb-go daemon service status: systemd user services are supported on Linux only")
		return 1
	}
	fs := flag.NewFlagSet("daemon service status", flag.ContinueOnError)
	fs.SetOutput(stderr)
	systemctlFlag := fs.String("systemctl", "systemctl", "systemctl binary path")
	fs.Usage = func() { fmt.Fprint(stderr, daemonServiceStatusUsage) }
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(stderr, "adb-go daemon service status: unexpected arguments %q\n\n", fs.Args())
		fs.Usage()
		return 2
	}

	active, err := systemctlUserOutput(*systemctlFlag, "is-active", "adb-god.service")
	if err != nil {
		fmt.Fprintf(stderr, "adb-go daemon service status: %v\n", err)
		return 1
	}
	enabled, err := systemctlUserOutput(*systemctlFlag, "is-enabled", "adb-god.service")
	if err != nil {
		fmt.Fprintf(stderr, "adb-go daemon service status: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "active: %s\n", active)
	fmt.Fprintf(stdout, "enabled: %s\n", enabled)
	return 0
}

func runDaemonServiceLogs(args []string, stdout, stderr io.Writer) int {
	if runtime.GOOS != "linux" {
		fmt.Fprintln(stderr, "adb-go daemon service logs: systemd user service logs are supported on Linux only")
		return 1
	}
	fs := flag.NewFlagSet("daemon service logs", flag.ContinueOnError)
	fs.SetOutput(stderr)
	journalctlFlag := fs.String("journalctl", "journalctl", "journalctl binary path")
	linesFlag := fs.Int("lines", 100, "number of recent journal entries to print before following")
	followFlag := fs.Bool("follow", false, "keep streaming new journal entries")
	fs.Usage = func() { fmt.Fprint(stderr, daemonServiceLogsUsage) }
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(stderr, "adb-go daemon service logs: unexpected arguments %q\n\n", fs.Args())
		fs.Usage()
		return 2
	}
	if *linesFlag < 0 {
		fmt.Fprintln(stderr, "adb-go daemon service logs: --lines must be zero or greater")
		return 2
	}

	cmdArgs := []string{"--user", "-u", "adb-god.service", "-n", strconv.Itoa(*linesFlag), "--no-pager"}
	if *followFlag {
		cmdArgs = append(cmdArgs, "-f")
	}
	cmd := exec.Command(strings.TrimSpace(*journalctlFlag), cmdArgs...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(stderr, "adb-go daemon service logs: %s %s failed: %v\n", *journalctlFlag, strings.Join(cmdArgs, " "), err)
		return 1
	}
	return 0
}

func runDaemonServiceUninstall(args []string, stdout, stderr io.Writer) int {
	if runtime.GOOS != "linux" {
		fmt.Fprintln(stderr, "adb-go daemon service uninstall: systemd user services are supported on Linux only")
		return 1
	}
	fs := flag.NewFlagSet("daemon service uninstall", flag.ContinueOnError)
	fs.SetOutput(stderr)
	unitDirFlag := fs.String("unit-dir", "", "systemd user unit directory")
	systemctlFlag := fs.String("systemctl", "systemctl", "systemctl binary path")
	keepUnitFlag := fs.Bool("keep-unit", false, "disable and stop the service but do not remove the unit file")
	fs.Usage = func() { fmt.Fprint(stderr, daemonServiceUninstallUsage) }
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(stderr, "adb-go daemon service uninstall: unexpected arguments %q\n\n", fs.Args())
		fs.Usage()
		return 2
	}

	if code := runSystemctlUser(stderr, *systemctlFlag, "disable", "--now", "adb-god.service"); code != 0 {
		return code
	}
	unitDir, err := systemdUserUnitDir(strings.TrimSpace(*unitDirFlag))
	if err != nil {
		fmt.Fprintf(stderr, "adb-go daemon service uninstall: %v\n", err)
		return 1
	}
	unitPath := filepath.Join(unitDir, "adb-god.service")
	if !*keepUnitFlag {
		if err := os.Remove(unitPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			fmt.Fprintf(stderr, "adb-go daemon service uninstall: remove %s: %v\n", unitPath, err)
			return 1
		}
	}
	if code := runSystemctlUser(stderr, *systemctlFlag, "daemon-reload"); code != 0 {
		return code
	}
	if *keepUnitFlag {
		fmt.Fprintln(stdout, "adb-god.service disabled and stopped; unit file kept")
	} else {
		fmt.Fprintf(stdout, "adb-god.service disabled and stopped; removed %s\n", unitPath)
	}
	return 0
}

func writeDaemonServiceUnit(unitDir, adbGodPath, socketPath string) (string, error) {
	unitPath := filepath.Join(unitDir, "adb-god.service")
	unit := adbGodSystemdUnit(adbGodPath, socketPath)
	if err := os.MkdirAll(unitDir, 0o755); err != nil {
		return "", fmt.Errorf("create systemd user unit directory: %w", err)
	}
	if err := os.WriteFile(unitPath, []byte(unit), 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", unitPath, err)
	}
	return unitPath, nil
}

func resolveADBGodPath(configured string) (string, error) {
	if configured != "" {
		abs, err := filepath.Abs(configured)
		if err != nil {
			return "", fmt.Errorf("resolve adb-god path: %w", err)
		}
		return abs, nil
	}
	if found, err := exec.LookPath("adb-god"); err == nil {
		return filepath.Abs(found)
	}
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locate adb-god: adb-god is not on PATH and current executable path is unavailable")
	}
	candidate := filepath.Join(filepath.Dir(exe), "adb-god")
	if _, err := os.Stat(candidate); err == nil {
		return candidate, nil
	}
	return "", fmt.Errorf("locate adb-god: pass --adb-god PATH or install adb-god next to adb-go")
}

func systemdUserUnitDir(configured string) (string, error) {
	if configured != "" {
		return filepath.Abs(configured)
	}
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("locate systemd user unit directory: %w", err)
		}
		configHome = filepath.Join(home, ".config")
	}
	return filepath.Join(configHome, "systemd", "user"), nil
}

func adbGodSystemdUnit(adbGodPath, socketPath string) string {
	return strings.Join([]string{
		"[Unit]",
		"Description=adb-go daemon",
		"Documentation=https://github.com/dector/adb-go",
		"",
		"[Service]",
		"Type=simple",
		"ExecStart=" + systemdQuote(adbGodPath) + " -socket " + systemdQuote(socketPath),
		"Restart=on-failure",
		"RestartSec=2s",
		"",
		"[Install]",
		"WantedBy=default.target",
		"",
	}, "\n")
}

func systemdQuote(s string) string {
	return strconv.Quote(s)
}

func runSystemctlUser(stderr io.Writer, systemctl string, args ...string) int {
	cmdArgs := append([]string{"--user"}, args...)
	cmd := exec.Command(systemctl, cmdArgs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Fprintf(stderr, "adb-go daemon service install: %s %s failed: %v", systemctl, strings.Join(cmdArgs, " "), err)
		if len(out) > 0 {
			fmt.Fprintf(stderr, ": %s", strings.TrimSpace(string(out)))
		}
		fmt.Fprintln(stderr)
		return 1
	}
	return 0
}

func systemctlUserOutput(systemctl string, args ...string) (string, error) {
	cmdArgs := append([]string{"--user"}, args...)
	cmd := exec.Command(systemctl, cmdArgs...)
	out, err := cmd.CombinedOutput()
	status := strings.TrimSpace(string(out))
	if status != "" {
		return status, nil
	}
	if err != nil {
		return "", fmt.Errorf("%s %s failed: %w", systemctl, strings.Join(cmdArgs, " "), err)
	}
	return "", nil
}

const shellUsage = `Usage:
  adb-go shell (--addr HOST[:PORT] | --usb [USB selection]) COMMAND [ARG...]

Runs one shell command on the selected ADB device. TCP addresses come from
--addr, or from ADB_GO_ADDR when --addr is omitted. USB support is Linux-only
initially; select USB with --usb, --usb-path, --usb-bus/--usb-device,
--usb-vid/--usb-pid, or --serial. COMMAND and all following arguments are joined
with spaces and sent as one shell command string, for example:

  adb-go shell --addr 127.0.0.1:5555 echo hello
  adb-go shell --auth-key ~/.android/adbkey --usb-path /dev/bus/usb/001/002 getprop ro.product.model
`

func runShell(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("shell", flag.ContinueOnError)
	fs.SetOutput(stderr)
	conn := addConnectionFlags(fs)
	fs.Usage = func() { fmt.Fprint(stderr, shellUsage) }

	if err := fs.Parse(args); err != nil {
		return 2
	}
	target, err := conn.target(fs)
	if err != nil {
		fmt.Fprintf(stderr, "adb-go shell: %v\n\n", err)
		fs.Usage()
		return 2
	}
	if fs.NArg() == 0 {
		fmt.Fprint(stderr, "adb-go shell: missing shell command\n\n")
		fs.Usage()
		return 2
	}

	cmd := strings.Join(fs.Args(), " ")
	client, err := connectDevice(context.Background(), target)
	if err != nil {
		printConnectError(stderr, "shell", target.description, err)
		return 1
	}
	defer client.Close()

	if err := client.ShellStream(context.Background(), cmd, stdout); err != nil {
		fmt.Fprintf(stderr, "adb-go shell: %v\n", err)
		return 1
	}
	return 0
}

const logcatUsage = `Usage:
  adb-go logcat (--addr HOST[:PORT] | --usb [USB selection]) [--dump]

Streams Android logcat output from the selected ADB device to stdout. TCP
addresses come from --addr, or from ADB_GO_ADDR when --addr is omitted. USB
support is Linux-only initially. By default, logcat follows the device log
stream until the device closes it or the process is interrupted. Pass --dump to
request logcat's dump-and-exit mode, equivalent to logcat -d. For example:

  adb-go logcat --addr 127.0.0.1:5555
  adb-go logcat --dump --auth-key ~/.android/adbkey --usb-path /dev/bus/usb/001/002
`

func runLogcat(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("logcat", flag.ContinueOnError)
	fs.SetOutput(stderr)
	conn := addConnectionFlags(fs)
	dump := fs.Bool("dump", false, "dump the log and exit by passing logcat -d")
	fs.Usage = func() { fmt.Fprint(stderr, logcatUsage) }

	if err := fs.Parse(args); err != nil {
		return 2
	}
	target, err := conn.target(fs)
	if err != nil {
		fmt.Fprintf(stderr, "adb-go logcat: %v\n\n", err)
		fs.Usage()
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprint(stderr, "adb-go logcat: unexpected arguments\n\n")
		fs.Usage()
		return 2
	}

	client, err := connectDevice(context.Background(), target)
	if err != nil {
		printConnectError(stderr, "logcat", target.description, err)
		return 1
	}
	defer client.Close()

	if err := client.Logcat(context.Background(), stdout, adb.LogcatOptions{Dump: *dump}); err != nil {
		fmt.Fprintf(stderr, "adb-go logcat: %v\n", err)
		return 1
	}
	return 0
}

const getPropUsage = `Usage:
  adb-go getprop (--addr HOST[:PORT] | --usb [USB selection]) [PROPERTY]

Reads Android system properties from the selected ADB device. With PROPERTY,
prints that single property value. With no PROPERTY, prints all properties in
stable name order using getprop's standard [name]: [value] format. TCP
addresses come from --addr, or from ADB_GO_ADDR when --addr is omitted. USB
support is Linux-only initially. For example:

  adb-go getprop --addr 127.0.0.1:5555 ro.product.model
  adb-go getprop --auth-key ~/.android/adbkey --usb-path /dev/bus/usb/001/002
`

func runGetProp(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("getprop", flag.ContinueOnError)
	fs.SetOutput(stderr)
	conn := addConnectionFlags(fs)
	fs.Usage = func() { fmt.Fprint(stderr, getPropUsage) }

	if err := fs.Parse(args); err != nil {
		return 2
	}
	target, err := conn.target(fs)
	if err != nil {
		fmt.Fprintf(stderr, "adb-go getprop: %v\n\n", err)
		fs.Usage()
		return 2
	}
	if fs.NArg() > 1 {
		fmt.Fprint(stderr, "adb-go getprop: accepts at most one PROPERTY argument\n\n")
		fs.Usage()
		return 2
	}

	client, err := connectDevice(context.Background(), target)
	if err != nil {
		printConnectError(stderr, "getprop", target.description, err)
		return 1
	}
	defer client.Close()

	if fs.NArg() == 1 {
		value, err := client.GetProp(context.Background(), fs.Arg(0))
		if err != nil {
			fmt.Fprintf(stderr, "adb-go getprop: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, value)
		return 0
	}

	props, err := client.Properties(context.Background())
	if err != nil {
		fmt.Fprintf(stderr, "adb-go getprop: %v\n", err)
		return 1
	}
	names := make([]string, 0, len(props))
	for name := range props {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Fprintf(stdout, "[%s]: [%s]\n", name, props[name])
	}
	return 0
}

const screencapUsage = `Usage:
  adb-go screencap (--addr HOST[:PORT] | --usb [USB selection]) [--overwrite] [LOCAL_PNG]

Captures one PNG screenshot from the selected ADB device. When LOCAL_PNG is
omitted, adb-go writes to a timestamped file in the current directory named like
screen-yyyymmdd-hhmmssmmm.png, for example screen-20260102-030405123.png. TCP
addresses come from --addr, or from ADB_GO_ADDR when --addr is omitted. USB
support is Linux-only initially.

By default, screencap refuses to replace an existing local file. Pass
--overwrite to replace the selected path deliberately. For example:

  adb-go screencap --addr 127.0.0.1:5555
  adb-go screencap --addr 127.0.0.1:5555 ./screen.png
  adb-go screencap --overwrite --auth-key ~/.android/adbkey --usb-path /dev/bus/usb/001/002 ./screen.png
`

func runScreencap(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("screencap", flag.ContinueOnError)
	fs.SetOutput(stderr)
	conn := addConnectionFlags(fs)
	overwrite := fs.Bool("overwrite", false, "replace an existing local PNG destination")
	fs.Usage = func() { fmt.Fprint(stderr, screencapUsage) }

	if err := fs.Parse(args); err != nil {
		return 2
	}
	target, err := conn.target(fs)
	if err != nil {
		fmt.Fprintf(stderr, "adb-go screencap: %v\n\n", err)
		fs.Usage()
		return 2
	}
	if fs.NArg() > 1 {
		fmt.Fprint(stderr, "adb-go screencap: accepts at most one LOCAL_PNG argument\n\n")
		fs.Usage()
		return 2
	}

	localPath := defaultScreencapPath(currentTime())
	if fs.NArg() == 1 {
		localPath = fs.Arg(0)
	}

	client, err := connectDevice(context.Background(), target)
	if err != nil {
		printConnectError(stderr, "screencap", target.description, err)
		return 1
	}
	defer client.Close()

	png, err := client.Screencap(context.Background())
	if err != nil {
		fmt.Fprintf(stderr, "adb-go screencap: %v\n", err)
		return 1
	}
	if err := writeScreencapFile(localPath, png, *overwrite); err != nil {
		fmt.Fprintf(stderr, "adb-go screencap: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "%s\n", localPath)
	return 0
}

func defaultScreencapPath(t time.Time) string {
	return fmt.Sprintf("screen-%s%03d.png", t.Format("20060102-150405"), t.Nanosecond()/int(time.Millisecond))
}

func writeScreencapFile(localPath string, png []byte, overwrite bool) error {
	flags := os.O_WRONLY | os.O_CREATE
	if overwrite {
		flags |= os.O_TRUNC
	} else {
		flags |= os.O_EXCL
	}
	file, err := os.OpenFile(localPath, flags, 0o666)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("destination %q: %w", localPath, adb.ErrDestinationExists)
		}
		return fmt.Errorf("destination %q: %w", localPath, err)
	}
	ok := false
	defer func() {
		if !ok && !overwrite {
			_ = os.Remove(localPath)
		}
	}()
	if _, err := file.Write(png); err != nil {
		_ = file.Close()
		return fmt.Errorf("write destination %q: %w", localPath, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close destination %q: %w", localPath, err)
	}
	ok = true
	return nil
}

const rebootUsage = `Usage:
  adb-go reboot (--addr HOST[:PORT] | --usb [USB selection]) [MODE]

Requests an immediate reboot of the selected ADB device. With no MODE, adb-go
requests a normal Android reboot. Supported modes are bootloader and recovery.
TCP addresses come from --addr, or from ADB_GO_ADDR when --addr is omitted. USB
support is Linux-only initially. For example:

  adb-go reboot --addr 127.0.0.1:5555
  adb-go reboot --addr 127.0.0.1:5555 bootloader
  adb-go reboot --auth-key ~/.android/adbkey --usb-path /dev/bus/usb/001/002 recovery
`

func runReboot(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("reboot", flag.ContinueOnError)
	fs.SetOutput(stderr)
	conn := addConnectionFlags(fs)
	fs.Usage = func() { fmt.Fprint(stderr, rebootUsage) }

	if err := fs.Parse(args); err != nil {
		return 2
	}
	target, err := conn.target(fs)
	if err != nil {
		fmt.Fprintf(stderr, "adb-go reboot: %v\n\n", err)
		fs.Usage()
		return 2
	}
	if fs.NArg() > 1 {
		fmt.Fprint(stderr, "adb-go reboot: accepts at most one MODE argument\n\n")
		fs.Usage()
		return 2
	}

	mode := adb.RebootNormal
	if fs.NArg() == 1 {
		parsed, err := parseRebootMode(fs.Arg(0))
		if err != nil {
			fmt.Fprintf(stderr, "adb-go reboot: %v\n\n", err)
			fs.Usage()
			return 2
		}
		mode = parsed
	}

	client, err := connectDevice(context.Background(), target)
	if err != nil {
		printConnectError(stderr, "reboot", target.description, err)
		return 1
	}
	defer client.Close()

	if err := client.Reboot(context.Background(), mode); err != nil {
		fmt.Fprintf(stderr, "adb-go reboot: %v\n", err)
		return 1
	}
	return 0
}

func parseRebootMode(value string) (adb.RebootMode, error) {
	switch value {
	case "", "normal":
		return adb.RebootNormal, nil
	case string(adb.RebootBootloader):
		return adb.RebootBootloader, nil
	case string(adb.RebootRecovery):
		return adb.RebootRecovery, nil
	default:
		return "", fmt.Errorf("unsupported reboot mode %q; supported modes are normal, bootloader, recovery", value)
	}
}

const forwardUsage = `Usage:
  adb-go forward (--addr HOST[:PORT] | --usb [USB selection]) tcp:LOCAL_PORT tcp:REMOTE_PORT

Starts foreground, process-scoped forwarding from a local TCP listener to a
TCP endpoint on the selected device. This differs from official "adb forward":
adb-go does not register persistent mappings in an adb server. The forward
exists only while this command keeps running; stop the process to remove it.

Use tcp:0 as LOCAL_PORT to ask the OS for an available local port. adb-go
prints the bound local listener address before it starts waiting. For example:

  adb-go forward --addr 127.0.0.1:5555 tcp:9000 tcp:8000
  adb-go forward --auth-key ~/.android/adbkey --usb-path /dev/bus/usb/001/002 tcp:0 tcp:8000
`

func runForward(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("forward", flag.ContinueOnError)
	fs.SetOutput(stderr)
	conn := addConnectionFlags(fs)
	fs.Usage = func() { fmt.Fprint(stderr, forwardUsage) }

	if err := fs.Parse(args); err != nil {
		return 2
	}
	target, err := conn.target(fs)
	if err != nil {
		fmt.Fprintf(stderr, "adb-go forward: %v\n\n", err)
		fs.Usage()
		return 2
	}
	if fs.NArg() != 2 {
		fmt.Fprint(stderr, "adb-go forward: requires exactly LOCAL and REMOTE targets\n\n")
		fs.Usage()
		return 2
	}

	localAddr, err := parseForwardLocalTCP(fs.Arg(0))
	if err != nil {
		fmt.Fprintf(stderr, "adb-go forward: %v\n\n", err)
		fs.Usage()
		return 2
	}
	remote, err := parseForwardRemoteTCP(fs.Arg(1))
	if err != nil {
		fmt.Fprintf(stderr, "adb-go forward: %v\n\n", err)
		fs.Usage()
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	client, err := connectDevice(ctx, target)
	if err != nil {
		printConnectError(stderr, "forward", target.description, err)
		return 1
	}
	defer client.Close()

	forward, err := startForward(ctx, client, localAddr, remote)
	if err != nil {
		fmt.Fprintf(stderr, "adb-go forward: %v\n", err)
		return 1
	}
	defer forward.Close()
	fmt.Fprintf(stdout, "Forwarding %s -> %s. Press Ctrl+C to stop.\n", forward.LocalAddr(), fs.Arg(1))

	if err := forward.Wait(); err != nil {
		fmt.Fprintf(stderr, "adb-go forward: %v\n", err)
		return 1
	}
	return 0
}

func parseForwardLocalTCP(value string) (string, error) {
	port, err := parseForwardTCPPort("local", value)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("127.0.0.1:%d", port), nil
}

func parseForwardRemoteTCP(value string) (adb.ForwardTarget, error) {
	port, err := parseForwardTCPPort("remote", value)
	if err != nil {
		return adb.ForwardTarget{}, err
	}
	remote, err := adb.ForwardTCP(port)
	if err != nil {
		return adb.ForwardTarget{}, err
	}
	return remote, nil
}

func parseForwardTCPPort(kind, value string) (int, error) {
	if !strings.HasPrefix(value, "tcp:") {
		return 0, fmt.Errorf("unsupported %s forward target %q; only tcp:PORT is supported", kind, value)
	}
	portText := strings.TrimSpace(strings.TrimPrefix(value, "tcp:"))
	if portText == "" {
		return 0, fmt.Errorf("missing %s tcp port in %q", kind, value)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		return 0, fmt.Errorf("invalid %s tcp port in %q", kind, value)
	}
	if port < 0 || port > 65535 || (kind == "remote" && port == 0) {
		return 0, fmt.Errorf("%s tcp port %d out of range", kind, port)
	}
	return port, nil
}

const pushUsage = `Usage:
  adb-go push (--addr HOST[:PORT] | --usb [USB selection]) LOCAL_PATH REMOTE_PATH

Pushes exactly one local file to the selected ADB device. TCP addresses come from
--addr, or from ADB_GO_ADDR when --addr is omitted. USB support is Linux-only
initially. For example:

  adb-go push --addr 127.0.0.1:5555 ./local.txt /data/local/tmp/local.txt
  adb-go push --auth-key ~/.android/adbkey --usb-path /dev/bus/usb/001/002 ./local.txt /data/local/tmp/local.txt
`

func runPush(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("push", flag.ContinueOnError)
	fs.SetOutput(stderr)
	conn := addConnectionFlags(fs)
	fs.Usage = func() { fmt.Fprint(stderr, pushUsage) }

	if err := fs.Parse(args); err != nil {
		return 2
	}
	target, err := conn.target(fs)
	if err != nil {
		fmt.Fprintf(stderr, "adb-go push: %v\n\n", err)
		fs.Usage()
		return 2
	}
	if fs.NArg() != 2 {
		fmt.Fprint(stderr, "adb-go push: requires exactly LOCAL_PATH and REMOTE_PATH\n\n")
		fs.Usage()
		return 2
	}

	localPath, remotePath := fs.Arg(0), fs.Arg(1)
	client, err := connectDevice(context.Background(), target)
	if err != nil {
		printConnectError(stderr, "push", target.description, err)
		return 1
	}
	defer client.Close()

	if err := client.PushFile(context.Background(), localPath, remotePath); err != nil {
		fmt.Fprintf(stderr, "adb-go push: %v\n", err)
		return 1
	}
	return 0
}

const pullUsage = `Usage:
  adb-go pull (--addr HOST[:PORT] | --usb [USB selection]) [--overwrite] REMOTE_PATH LOCAL_PATH

Pulls exactly one remote file from the selected ADB device. TCP addresses come
from --addr, or from ADB_GO_ADDR when --addr is omitted. USB support is
Linux-only initially. By default, the command refuses to replace an existing
local destination; pass --overwrite to replace it deliberately. For example:

  adb-go pull --addr 127.0.0.1:5555 /data/local/tmp/remote.txt ./remote.txt
  adb-go pull --auth-key ~/.android/adbkey --usb-path /dev/bus/usb/001/002 /data/local/tmp/remote.txt ./remote.txt
`

const installAPKUsage = `Usage:
  adb-go install-apk (--addr HOST[:PORT] | --usb [USB selection]) [--replace] LOCAL_APK

Installs exactly one local APK on the selected ADB device. This is an
adb-go-specific helper, not a full clone of "adb install". It pushes the APK to
a temporary path under /data/local/tmp, runs pm install, and asks the device to
remove the temporary APK afterward. TCP addresses come from --addr, or from
ADB_GO_ADDR when --addr is omitted. USB support is Linux-only initially. Pass
--replace to allow replacing an already-installed app via pm install -r. For
example:

  adb-go install-apk --addr 127.0.0.1:5555 ./app.apk
  adb-go install-apk --replace --auth-key ~/.android/adbkey --usb-path /dev/bus/usb/001/002 ./app.apk
`

func runInstallAPK(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("install-apk", flag.ContinueOnError)
	fs.SetOutput(stderr)
	conn := addConnectionFlags(fs)
	replace := fs.Bool("replace", false, "allow package replacement with pm install -r")
	fs.Usage = func() { fmt.Fprint(stderr, installAPKUsage) }

	if err := fs.Parse(args); err != nil {
		return 2
	}
	target, err := conn.target(fs)
	if err != nil {
		fmt.Fprintf(stderr, "adb-go install-apk: %v\n\n", err)
		fs.Usage()
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprint(stderr, "adb-go install-apk: requires exactly LOCAL_APK\n\n")
		fs.Usage()
		return 2
	}

	localPath := fs.Arg(0)
	client, err := connectDevice(context.Background(), target)
	if err != nil {
		printConnectError(stderr, "install-apk", target.description, err)
		return 1
	}
	defer client.Close()

	if *replace {
		err = client.InstallAPKWithOptions(context.Background(), localPath, adb.InstallOptions{Replace: true})
	} else {
		err = client.InstallAPK(context.Background(), localPath)
	}
	if err != nil {
		fmt.Fprintf(stderr, "adb-go install-apk: %v\n", err)
		return 1
	}
	return 0
}

func runPull(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("pull", flag.ContinueOnError)
	fs.SetOutput(stderr)
	conn := addConnectionFlags(fs)
	overwrite := fs.Bool("overwrite", false, "replace an existing local destination")
	fs.Usage = func() { fmt.Fprint(stderr, pullUsage) }

	if err := fs.Parse(args); err != nil {
		return 2
	}
	target, err := conn.target(fs)
	if err != nil {
		fmt.Fprintf(stderr, "adb-go pull: %v\n\n", err)
		fs.Usage()
		return 2
	}
	if fs.NArg() != 2 {
		fmt.Fprint(stderr, "adb-go pull: requires exactly REMOTE_PATH and LOCAL_PATH\n\n")
		fs.Usage()
		return 2
	}

	remotePath, localPath := fs.Arg(0), fs.Arg(1)
	client, err := connectDevice(context.Background(), target)
	if err != nil {
		printConnectError(stderr, "pull", target.description, err)
		return 1
	}
	defer client.Close()

	if *overwrite {
		err = client.PullFileWithOptions(context.Background(), remotePath, localPath, adb.PullOptions{Overwrite: true})
	} else {
		err = client.PullFile(context.Background(), remotePath, localPath)
	}
	if err != nil {
		fmt.Fprintf(stderr, "adb-go pull: %v\n", err)
		return 1
	}
	return 0
}
