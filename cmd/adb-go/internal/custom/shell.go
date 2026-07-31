package custom

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	adb "github.com/dector/adb-go"
)

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
		printCommandError(stderr, "shell", err)
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
		printCommandError(stderr, "logcat", err)
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
	return runGetPropWithJSON(args, false, stdout, stderr)
}

func runGetPropWithJSON(args []string, jsonOutput bool, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("getprop", flag.ContinueOnError)
	fs.SetOutput(stderr)
	conn := addConnectionFlags(fs)
	jsonFlag := fs.Bool("json", jsonOutput, "print machine-readable JSON")
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
	jsonOutput = *jsonFlag
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
			printCommandError(stderr, "getprop", err)
			return 1
		}
		if jsonOutput {
			if err := writeJSON(stdout, map[string]string{"name": fs.Arg(0), "value": value}); err != nil {
				fmt.Fprintf(stderr, "adb-go getprop: encode JSON: %v\n", err)
				return 1
			}
			return 0
		}
		fmt.Fprintln(stdout, value)
		return 0
	}

	props, err := client.Properties(context.Background())
	if err != nil {
		printCommandError(stderr, "getprop", err)
		return 1
	}
	if jsonOutput {
		if err := writeJSON(stdout, props); err != nil {
			fmt.Fprintf(stderr, "adb-go getprop: encode JSON: %v\n", err)
			return 1
		}
		return 0
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
		printCommandError(stderr, "screencap", err)
		return 1
	}
	if err := writeScreencapFile(localPath, png, *overwrite); err != nil {
		printCommandError(stderr, "screencap", err)
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
		printCommandError(stderr, "reboot", err)
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
