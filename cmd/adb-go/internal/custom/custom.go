package custom

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"runtime"
	"syscall"

	adb "github.com/dector/adb-go"
)

const usage = `adb-go is a pure-Go Android Debug Bridge client.

Usage:
  adb-go [--quiet | --verbose] <command> [arguments]

Commands:
  help        Show this help message
  version     Print adb-go build and runtime information
  targets     List adb-go connection targets visible locally
  devices     List devices known by the local adb-gos server
  shell       Run a shell command on a connected device
  logcat      Stream Android log output from a connected device
  getprop     Read Android system properties from a connected device
  screencap   Save a PNG screenshot from a connected device
  reboot      Reboot a connected device
  forward     Forward local TCP connections to a device TCP endpoint
  reverse     Reverse device TCP connections to a host TCP endpoint
  server      Control the local adb-gos server process
  push        Push one local file to a connected device
  pull        Pull one remote file from a connected device
  install-apk Install one local APK on a connected device

Global options:
  --quiet     Suppress non-error informational status messages. Command payloads
              such as shell/logcat stdout, listings, JSON/plain output, version
              information, and chosen file paths still print.
  --verbose   Print extra human-facing diagnostics to stderr without changing
              command payload stdout. Cannot be combined with --quiet.
  --json      Print machine-readable JSON for supported commands.

adb-go is not a full replacement for the official adb binary yet. The CLI is a
thin wrapper around the adb-go library and will grow command coverage gradually.
`

// version is intentionally a package variable so release builds can inject a
// concrete value with Go's standard linker flag. tools/git-version.sh derives
// the project convention from Git tags for release/snapshot builds.
var Version = "dev"

type commandRunner func([]string, cliOptions, io.Writer, io.Writer) int

var commandRunners = map[string]commandRunner{
	"version": func(args []string, _ cliOptions, stdout, stderr io.Writer) int {
		return runVersion(args, stdout, stderr)
	},
	"targets": func(args []string, opts cliOptions, stdout, stderr io.Writer) int {
		return runTargetsWithJSON(args, opts.JSON, stdout, stderr)
	},
	"devices": func(args []string, opts cliOptions, stdout, stderr io.Writer) int {
		return runDevicesWithJSON(args, opts.JSON, stdout, stderr)
	},
	"shell": func(args []string, opts cliOptions, stdout, stderr io.Writer) int {
		return runShellWithOptions(args, opts, stdout, stderr)
	},
	"logcat": func(args []string, opts cliOptions, stdout, stderr io.Writer) int {
		return runLogcatWithOptions(args, opts, stdout, stderr)
	},
	"getprop": func(args []string, opts cliOptions, stdout, stderr io.Writer) int {
		return runGetPropWithOptions(args, opts, stdout, stderr)
	},
	"screencap": func(args []string, opts cliOptions, stdout, stderr io.Writer) int {
		return runScreencapWithOptions(args, opts, stdout, stderr)
	},
	"reboot": func(args []string, opts cliOptions, stdout, stderr io.Writer) int {
		return runRebootWithOptions(args, opts, stdout, stderr)
	},
	"forward": func(args []string, opts cliOptions, stdout, stderr io.Writer) int {
		return runForwardWithOptions(args, opts, stdout, stderr)
	},
	"reverse": func(args []string, opts cliOptions, stdout, stderr io.Writer) int {
		return runReverseWithOptions(args, opts, stdout, stderr)
	},
	"server": func(args []string, opts cliOptions, stdout, stderr io.Writer) int {
		return runServerWithJSON(args, opts.JSON, stdout, stderr)
	},
	"push": func(args []string, opts cliOptions, stdout, stderr io.Writer) int {
		return runPushWithOptions(args, opts, stdout, stderr)
	},
	"pull": func(args []string, opts cliOptions, stdout, stderr io.Writer) int {
		return runPullWithOptions(args, opts, stdout, stderr)
	},
	"install-apk": func(args []string, opts cliOptions, stdout, stderr io.Writer) int {
		return runInstallAPKWithOptions(args, opts, stdout, stderr)
	},
}

func Run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("adb-go", flag.ContinueOnError)
	fs.SetOutput(stderr)
	jsonOutput := fs.Bool("json", false, "print machine-readable JSON for supported commands")
	quietOutput := fs.Bool("quiet", false, "suppress non-error informational status messages")
	verboseOutput := fs.Bool("verbose", false, "print extra diagnostics to stderr")
	helpOutput := fs.Bool("help", false, "show this help message")
	shortHelpOutput := fs.Bool("h", false, "show this help message")
	fs.Usage = func() { fmt.Fprint(stderr, usage) }
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *helpOutput || *shortHelpOutput {
		fmt.Fprint(stdout, usage)
		return 0
	}
	if *quietOutput && *verboseOutput {
		fmt.Fprint(stderr, "adb-go: choose only one output mode: --quiet or --verbose\n\n")
		fs.Usage()
		return 2
	}

	command, commandArgs, ok := splitFlagSetCommand(fs)
	if !ok {
		fmt.Fprint(stdout, usage)
		return 0
	}
	if command == "help" || command == "-h" || command == "--help" {
		fmt.Fprint(stdout, usage)
		return 0
	}

	runner, ok := commandRunners[command]
	if !ok {
		fmt.Fprintf(stderr, "adb-go: unknown command %q\n\n", command)
		fmt.Fprint(stderr, usage)
		return 2
	}
	return runner(commandArgs, cliOptions{JSON: *jsonOutput, Quiet: *quietOutput, Verbose: *verboseOutput}, stdout, stderr)
}

type versionInfo struct {
	Version   string
	GoVersion string
	GOOS      string
	GOARCH    string
}

func currentVersionInfo() versionInfo {
	return versionInfo{
		Version:   Version,
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
Release builds can set the adb-go version at build time with the Git-derived
project convention:

  version=$(./tools/git-version.sh)
  go build -ldflags "-X main.version=${version}" ./cmd/adb-go
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
	fmt.Fprintf(stderr, "adb-go %s: connect to %s: %s\n", command, description, formatCLIError(err))
}

func printCommandError(stderr io.Writer, command string, err error) {
	fmt.Fprintf(stderr, "adb-go %s: %s\n", command, formatCLIError(err))
}

func formatCLIError(err error) string {
	switch {
	case errors.Is(err, adb.ErrAuthRequired):
		return fmt.Sprintf("device requires authentication; pass --auth-key PATH for an existing ADB private key: %v", err)
	case errors.Is(err, adb.ErrDestinationExists):
		return fmt.Sprintf("destination already exists; pass --overwrite to replace it deliberately: %v", err)
	case errors.Is(err, adb.ErrUnsupported):
		return fmt.Sprintf("operation unsupported in this adb-go build; check whether the requested transport or selector is implemented for this platform: %v", err)
	case isConnectionRefused(err):
		return fmt.Sprintf("connection refused; check that the device/emulator is running ADB TCP at this address, or run `adb-go targets --scan` for local emulators: %v", err)
	case isTimeout(err):
		return fmt.Sprintf("operation timed out; check that the target address is reachable, the device is online, and USB permissions are sufficient: %v", err)
	case errors.Is(err, os.ErrNotExist):
		return fmt.Sprintf("local file or directory not found; check the path and parent directory: %v", err)
	default:
		return err.Error()
	}
}

func isConnectionRefused(err error) bool {
	return errors.Is(err, syscall.ECONNREFUSED)
}

func isTimeout(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) || os.IsTimeout(err) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
