package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	adb "github.com/dector/adb-go"
)

const usage = `adb-go is a pure-Go Android Debug Bridge client.

Usage:
  adb-go <command> [arguments]

Commands:
  help        Show this help message
  shell       Run a shell command on a connected device
  push        Push one local file to a connected device
  pull        Pull one remote file from a connected device

adb-go is not a full replacement for the official adb binary yet. The CLI is a
thin wrapper around the adb-go library and will grow command coverage gradually.
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

type deviceClient interface {
	Close() error
	ShellStream(ctx context.Context, cmd string, stdout io.Writer) error
	PushFile(ctx context.Context, localPath, remotePath string) error
	PullFile(ctx context.Context, remotePath, localPath string) error
	PullFileWithOptions(ctx context.Context, remotePath, localPath string, opts adb.PullOptions) error
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
}

type connectionTarget struct {
	description string
	tcpAddr     string
	usb         bool
	usbOptions  adb.USBOptions
}

var connectDevice = func(ctx context.Context, target connectionTarget) (deviceClient, error) {
	if target.usb {
		return adb.ConnectUSB(ctx, target.usbOptions)
	}
	return adb.Connect(ctx, target.tcpAddr)
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
	}
}

func (o connectionOptions) target(fs *flag.FlagSet) (connectionTarget, error) {
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
		var err error
		if opts.VendorID, err = parseUSBID("--usb-vid", *o.usbVID); err != nil {
			return connectionTarget{}, err
		}
		if opts.ProductID, err = parseUSBID("--usb-pid", *o.usbPID); err != nil {
			return connectionTarget{}, err
		}
		return connectionTarget{description: "USB device", usb: true, usbOptions: opts}, nil
	}

	if addrProvided {
		addr := strings.TrimSpace(*o.addr)
		if addr == "" {
			return connectionTarget{}, fmt.Errorf("missing required --addr value")
		}
		return connectionTarget{description: addr, tcpAddr: addr}, nil
	}

	addr := strings.TrimSpace(os.Getenv("ADB_GO_ADDR"))
	if addr != "" {
		return connectionTarget{description: addr, tcpAddr: addr}, nil
	}
	return connectionTarget{}, fmt.Errorf("missing required --addr, ADB_GO_ADDR, or USB selection")
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

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stdout, usage)
		return 0
	}

	switch args[0] {
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return 0
	case "shell":
		return runShell(args[1:], stdout, stderr)
	case "push":
		return runPush(args[1:], stdout, stderr)
	case "pull":
		return runPull(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "adb-go: unknown command %q\n\n", args[0])
		fmt.Fprint(stderr, usage)
		return 2
	}
}

const shellUsage = `Usage:
  adb-go shell (--addr HOST[:PORT] | --usb [USB selection]) COMMAND [ARG...]

Runs one shell command on the selected ADB device. TCP addresses come from
--addr, or from ADB_GO_ADDR when --addr is omitted. USB support is Linux-only
initially; select USB with --usb, --usb-path, --usb-bus/--usb-device,
--usb-vid/--usb-pid, or --serial. COMMAND and all following arguments are joined
with spaces and sent as one shell command string, for example:

  adb-go shell --addr 127.0.0.1:5555 echo hello
  adb-go shell --usb-path /dev/bus/usb/001/002 getprop ro.product.model
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
		fmt.Fprintf(stderr, "adb-go shell: connect to %s: %v\n", target.description, err)
		return 1
	}
	defer client.Close()

	if err := client.ShellStream(context.Background(), cmd, stdout); err != nil {
		fmt.Fprintf(stderr, "adb-go shell: %v\n", err)
		return 1
	}
	return 0
}

const pushUsage = `Usage:
  adb-go push (--addr HOST[:PORT] | --usb [USB selection]) LOCAL_PATH REMOTE_PATH

Pushes exactly one local file to the selected ADB device. TCP addresses come from
--addr, or from ADB_GO_ADDR when --addr is omitted. USB support is Linux-only
initially. For example:

  adb-go push --addr 127.0.0.1:5555 ./local.txt /data/local/tmp/local.txt
  adb-go push --usb-path /dev/bus/usb/001/002 ./local.txt /data/local/tmp/local.txt
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
		fmt.Fprintf(stderr, "adb-go push: connect to %s: %v\n", target.description, err)
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
  adb-go pull --usb-path /dev/bus/usb/001/002 /data/local/tmp/remote.txt ./remote.txt
`

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
		fmt.Fprintf(stderr, "adb-go pull: connect to %s: %v\n", target.description, err)
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
