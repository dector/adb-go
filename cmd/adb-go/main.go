package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	adb "github.com/dector/adb-go"
)

const usage = `adb-go is a pure-Go Android Debug Bridge client.

Usage:
  adb-go <command> [arguments]

Commands:
  help        Show this help message
  targets     List adb-go connection targets visible locally
  shell       Run a shell command on a connected device
  logcat      Stream Android log output from a connected device
  getprop     Read Android system properties from a connected device
  screencap   Save a PNG screenshot from a connected device
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

func printConnectError(stderr io.Writer, command, description string, err error) {
	if errors.Is(err, adb.ErrAuthRequired) {
		fmt.Fprintf(stderr, "adb-go %s: connect to %s: device requires authentication; pass --auth-key PATH for an existing ADB private key: %v\n", command, description, err)
		return
	}
	fmt.Fprintf(stderr, "adb-go %s: connect to %s: %v\n", command, description, err)
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
