package custom

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	adb "github.com/dector/adb-go"
)

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
