package compat

import (
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
)

// Version is intentionally a package variable so release builds can inject the
// same Git-derived version that the custom adb-go CLI reports. Compat mode
// prints adb-shaped version text, but it still identifies itself as adb-go.
var Version = "dev"

const helpText = `Android Debug Bridge version 1.0.41 (adb-go compat)

Usage: adb [global options] command [command options]

global options:
 -H HOST    adb server host name [default=localhost]
 -P PORT    adb server port [default=5037]
 -h         show this help message
 --help     show this help message
 -s SERIAL  use device with given serial
 -d         use USB device (error if multiple devices connected)
 -e         use TCP/emulator device (error if multiple devices connected)

host commands:
 help         show this help message
 version      show version num
 start-server ensure adb-go server is running
 kill-server  stop adb-go server
 devices      list connected devices
 get-state    print selected device state
 reverse      manage device-to-host reverse socket connections

This adb-go compatibility mode is a host-side adb CLI skeleton. Unsupported
commands will be added incrementally as adb-compatible behavior is implemented.
`

type globalOptions struct {
	host     string
	port     string
	selector compatTargetSelector
}

var getEnv = os.Getenv

// Run executes adb-compatible CLI mode. Compat mode is intentionally independent
// from the custom adb-go UX so future commands can track the official adb CLI
// surface without inheriting adb-go-specific flags or command names.
func Run(args []string, stdout, stderr io.Writer) int {
	opts, command, commandArgs, err := parseGlobalOptions(args)
	if err != nil {
		fmt.Fprintf(stderr, "adb: %v\n", err)
		return 1
	}

	switch command {
	case "", "help", "--help", "-h":
		if len(commandArgs) != 0 {
			fmt.Fprintf(stderr, "adb: %s: unexpected arguments %q\n", command, commandArgs)
			return 1
		}
		fmt.Fprint(stdout, helpText)
		return 0
	case "version":
		return runVersion(commandArgs, opts, stdout, stderr)
	case "start-server":
		return runStartServer(commandArgs, opts, stdout, stderr)
	case "kill-server":
		return runKillServer(commandArgs, opts, stdout, stderr)
	case "devices":
		return runDevices(commandArgs, opts, stdout, stderr)
	case "get-state":
		return runGetState(commandArgs, opts, stdout, stderr)
	case "reverse":
		return runReverse(commandArgs, opts, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "adb: unknown command %s\n", command)
		return 1
	}
}

func parseGlobalOptions(args []string) (globalOptions, string, []string, error) {
	opts := globalOptions{host: "localhost", port: "5037"}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "-H":
			if i+1 >= len(args) || args[i+1] == "" {
				return opts, "", nil, fmt.Errorf("option -H requires an argument")
			}
			opts.host = args[i+1]
			i++
		case strings.HasPrefix(arg, "-H") && len(arg) > len("-H"):
			opts.host = strings.TrimPrefix(arg, "-H")
		case arg == "-P":
			if i+1 >= len(args) || args[i+1] == "" {
				return opts, "", nil, fmt.Errorf("option -P requires an argument")
			}
			opts.port = args[i+1]
			i++
		case strings.HasPrefix(arg, "-P") && len(arg) > len("-P"):
			opts.port = strings.TrimPrefix(arg, "-P")
		case arg == "--help" || arg == "-h":
			applyAndroidSerialDefault(&opts)
			return opts, arg, args[i+1:], nil
		case arg == "-s":
			if i+1 >= len(args) || args[i+1] == "" {
				return opts, "", nil, fmt.Errorf("option -s requires an argument")
			}
			if err := opts.selector.setSerial(args[i+1]); err != nil {
				return opts, "", nil, err
			}
			i++
		case strings.HasPrefix(arg, "-s") && len(arg) > len("-s"):
			if err := opts.selector.setSerial(strings.TrimPrefix(arg, "-s")); err != nil {
				return opts, "", nil, err
			}
		case arg == "-d":
			if err := opts.selector.setUSB(); err != nil {
				return opts, "", nil, err
			}
		case arg == "-e":
			if err := opts.selector.setEmulator(); err != nil {
				return opts, "", nil, err
			}
		case strings.HasPrefix(arg, "-"):
			return opts, "", nil, fmt.Errorf("unknown option %s", arg)
		default:
			applyAndroidSerialDefault(&opts)
			return opts, arg, args[i+1:], nil
		}
	}
	applyAndroidSerialDefault(&opts)
	return opts, "", nil, nil
}

func applyAndroidSerialDefault(opts *globalOptions) {
	if opts.selector.kind != targetSelectorNone {
		return
	}
	if serial := getEnv("ANDROID_SERIAL"); serial != "" {
		opts.selector = compatTargetSelector{kind: targetSelectorSerial, serial: serial}
	}
}

func runVersion(args []string, opts globalOptions, stdout, stderr io.Writer) int {
	if len(args) != 0 {
		fmt.Fprintf(stderr, "adb: version: unexpected arguments %q\n", args)
		return 1
	}
	fmt.Fprint(stdout, formatVersion(opts))
	return 0
}

func formatVersion(opts globalOptions) string {
	return fmt.Sprintf("Android Debug Bridge version 1.0.41 (adb-go compat)\nVersion %s\nInstalled as adb-go compat mode\nRunning on %s %s\nServer target %s:%s\n", Version, runtime.GOOS, runtime.GOARCH, opts.host, opts.port)
}
