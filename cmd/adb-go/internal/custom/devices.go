package custom

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/dector/adb-go/cmd/adb-go/internal/cliserver"
	"github.com/dector/adb-go/internal/server"
)

const devicesUsage = `Usage:
  adb-go devices [--socket PATH] [--plain]

Lists devices known by the local adb-gos server. This command asks the server
for its registered device table; it is not active USB/TCP discovery. To discover
locally visible connection targets, use:

  adb-go targets
  adb-go targets --scan
`

type devicesOutput struct {
	Devices []server.Device `json:"devices"`
}

func runDevices(args []string, stdout, stderr io.Writer) int {
	return runDevicesWithJSON(args, false, stdout, stderr)
}

func runDevicesWithJSON(args []string, jsonOutput bool, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("devices", flag.ContinueOnError)
	fs.SetOutput(stderr)
	socketPathFlag := fs.String("socket", "", "absolute adb-gos Unix domain socket path")
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

	socketPath, err := devicesServerSocketPath(strings.TrimSpace(*socketPathFlag))
	if err != nil {
		fmt.Fprintf(stderr, "adb-go devices: %v\n", err)
		return 1
	}
	resp, err := sendDevicesServerRequest(socketPath)
	if err != nil || !resp.OK {
		printDevicesServerError(stderr, socketPath, resp, err)
		return 1
	}
	var result devicesOutput
	if err := decodeServerResult(resp.Result, &result); err != nil {
		fmt.Fprintf(stderr, "adb-go devices: decode server response: %v\n", err)
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
		fmt.Fprintln(stdout, "No server-known devices.")
		fmt.Fprintln(stdout, "The server is reachable, but it has no registered devices. Use `adb-go targets` to see local connection targets.")
		return 0
	}
	if err := writeAlignedTable(stdout, headers, rows); err != nil {
		fmt.Fprintf(stderr, "adb-go devices: write table: %v\n", err)
		return 1
	}
	return 0
}

func devicesServerSocketPath(configured string) (string, error) {
	if configured != "" {
		return configured, nil
	}
	return server.DefaultSocketPath()
}

func sendDevicesServerRequest(socketPath string) (server.Response, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return cliserver.Send(ctx, socketPath, server.CommandDeviceList, nil, sendServerRequest)
}

func deviceTableRows(devices []server.Device) []tableRow {
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

func printDevicesServerError(stderr io.Writer, socketPath string, resp server.Response, err error) {
	if err != nil {
		fmt.Fprintf(stderr, "adb-go devices: cannot list server-known devices: adb-gos is not running or socket is unavailable at %s: %v\n", socketPath, err)
		fmt.Fprintln(stderr, "Hint: start the server process with `adb-go server service start` or inspect it with `adb-go server doctor`.")
		return
	}
	if resp.Error != nil {
		fmt.Fprintf(stderr, "adb-go devices: cannot list server-known devices: server error %s: %s\n", resp.Error.Code, resp.Error.Message)
		return
	}
	fmt.Fprintln(stderr, "adb-go devices: cannot list server-known devices: server returned an unsuccessful response")
}
