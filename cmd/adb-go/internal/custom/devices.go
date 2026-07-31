package custom

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/dector/adb-go/cmd/adb-go/internal/clidaemon"
	"github.com/dector/adb-go/internal/daemon"
)

const devicesUsage = `Usage:
  adb-go devices [--socket PATH] [--plain]

Lists devices known by the local adb-god daemon. This command asks the daemon
for its registered device table; it is not active USB/TCP discovery. To discover
locally visible connection targets, use:

  adb-go targets
  adb-go targets --scan
`

type devicesOutput struct {
	Devices []daemon.Device `json:"devices"`
}

func runDevices(args []string, stdout, stderr io.Writer) int {
	return runDevicesWithJSON(args, false, stdout, stderr)
}

func runDevicesWithJSON(args []string, jsonOutput bool, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("devices", flag.ContinueOnError)
	fs.SetOutput(stderr)
	socketPathFlag := fs.String("socket", "", "absolute adb-god Unix domain socket path")
	jsonFlag := fs.Bool("json", jsonOutput, "print machine-readable JSON")
	plainFlag := fs.Bool("plain", false, "print tab-separated plain output")
	fs.Usage = func() { fmt.Fprint(stderr, devicesUsage) }
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(stderr, "adb-go devices: unexpected arguments %q\n\n", fs.Args())
		fs.Usage()
		return 2
	}
	if *jsonFlag && *plainFlag {
		fmt.Fprint(stderr, "adb-go devices: choose only one output mode: --json or --plain\n\n")
		fs.Usage()
		return 2
	}

	socketPath, err := devicesDaemonSocketPath(strings.TrimSpace(*socketPathFlag))
	if err != nil {
		fmt.Fprintf(stderr, "adb-go devices: %v\n", err)
		return 1
	}
	resp, err := sendDevicesDaemonRequest(socketPath)
	if err != nil || !resp.OK {
		printDevicesDaemonError(stderr, socketPath, resp, err)
		return 1
	}
	var result devicesOutput
	if err := decodeDaemonResult(resp.Result, &result); err != nil {
		fmt.Fprintf(stderr, "adb-go devices: decode daemon response: %v\n", err)
		return 1
	}
	if *jsonFlag {
		if err := writeJSON(stdout, result); err != nil {
			fmt.Fprintf(stderr, "adb-go devices: encode JSON: %v\n", err)
			return 1
		}
		return 0
	}

	headers := []string{"SERIAL", "STATE", "TRANSPORT", "ADDRESS"}
	rows := deviceTableRows(result.Devices)
	if *plainFlag {
		if err := writePlainTable(stdout, headers, rows); err != nil {
			fmt.Fprintf(stderr, "adb-go devices: write table: %v\n", err)
			return 1
		}
		return 0
	}
	if len(rows) == 0 {
		fmt.Fprintln(stdout, "No daemon-known devices.")
		fmt.Fprintln(stdout, "The daemon is reachable, but it has no registered devices. Use `adb-go targets` to see local connection targets.")
		return 0
	}
	if err := writeAlignedTable(stdout, headers, rows); err != nil {
		fmt.Fprintf(stderr, "adb-go devices: write table: %v\n", err)
		return 1
	}
	return 0
}

func devicesDaemonSocketPath(configured string) (string, error) {
	if configured != "" {
		return configured, nil
	}
	return daemon.DefaultSocketPath()
}

func sendDevicesDaemonRequest(socketPath string) (daemon.Response, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return clidaemon.Send(ctx, socketPath, daemon.CommandDeviceList, nil, sendDaemonRequest)
}

func deviceTableRows(devices []daemon.Device) []tableRow {
	rows := make([]tableRow, 0, len(devices))
	for _, device := range devices {
		address := device.Address
		if address == "" {
			address = "-"
		}
		rows = append(rows, tableRow{device.Serial, device.State, device.Transport, address})
	}
	return rows
}

func printDevicesDaemonError(stderr io.Writer, socketPath string, resp daemon.Response, err error) {
	if err != nil {
		fmt.Fprintf(stderr, "adb-go devices: cannot list daemon-known devices: adb-god is not running or socket is unavailable at %s: %v\n", socketPath, err)
		fmt.Fprintln(stderr, "Hint: start the daemon with `adb-go daemon service start` or inspect it with `adb-go daemon doctor`.")
		return
	}
	if resp.Error != nil {
		fmt.Fprintf(stderr, "adb-go devices: cannot list daemon-known devices: daemon error %s: %s\n", resp.Error.Code, resp.Error.Message)
		return
	}
	fmt.Fprintln(stderr, "adb-go devices: cannot list daemon-known devices: daemon returned an unsuccessful response")
}
