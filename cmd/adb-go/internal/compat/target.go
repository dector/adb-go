package compat

import (
	"context"
	"errors"
	"fmt"
	"io"

	adb "github.com/dector/adb-go"
	"github.com/dector/adb-go/cmd/adb-go/internal/clidaemon"
	"github.com/dector/adb-go/internal/daemon"
)

type targetSelectorKind int

const (
	targetSelectorNone targetSelectorKind = iota
	targetSelectorSerial
	targetSelectorUSB
	targetSelectorEmulator
)

type compatTargetSelector struct {
	kind   targetSelectorKind
	serial string
}

func (s *compatTargetSelector) setSerial(serial string) error {
	if s.kind != targetSelectorNone {
		return fmt.Errorf("more than one device/emulator selector specified")
	}
	s.kind = targetSelectorSerial
	s.serial = serial
	return nil
}

func (s *compatTargetSelector) setUSB() error {
	if s.kind != targetSelectorNone {
		return fmt.Errorf("more than one device/emulator selector specified")
	}
	s.kind = targetSelectorUSB
	return nil
}

func (s *compatTargetSelector) setEmulator() error {
	if s.kind != targetSelectorNone {
		return fmt.Errorf("more than one device/emulator selector specified")
	}
	s.kind = targetSelectorEmulator
	return nil
}

type compatTarget struct {
	Serial     string
	State      string
	Transport  string
	TCPAddress string
	USBOptions adb.USBOptions
}

func runGetState(args []string, opts globalOptions, stdout, stderr io.Writer) int {
	if len(args) != 0 {
		fmt.Fprintf(stderr, "adb: get-state: unexpected arguments %q\n", args)
		return 1
	}
	target, err := resolveCompatTarget(context.Background(), opts)
	if err != nil {
		fmt.Fprintf(stderr, "adb: get-state: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, target.State)
	return 0
}

func resolveCompatTarget(ctx context.Context, opts globalOptions) (compatTarget, error) {
	targets, err := listCompatTargets(ctx, opts)
	if err != nil {
		return compatTarget{}, err
	}

	selector := opts.selector
	switch selector.kind {
	case targetSelectorSerial:
		for _, target := range targets {
			if target.Serial == selector.serial {
				return target, nil
			}
		}
		return compatTarget{}, fmt.Errorf("device %q not found", selector.serial)
	case targetSelectorUSB:
		return selectOnlyTarget(targets, func(t compatTarget) bool { return t.Transport == "usb" }, "USB")
	case targetSelectorEmulator:
		return selectOnlyTarget(targets, func(t compatTarget) bool { return t.Transport == "tcp" }, "TCP/emulator")
	default:
		return selectOnlyTarget(targets, func(compatTarget) bool { return true }, "device/emulator")
	}
}

func selectOnlyTarget(targets []compatTarget, keep func(compatTarget) bool, label string) (compatTarget, error) {
	var matches []compatTarget
	for _, target := range targets {
		if keep(target) {
			matches = append(matches, target)
		}
	}
	switch len(matches) {
	case 0:
		return compatTarget{}, fmt.Errorf("no %s found", label)
	case 1:
		return matches[0], nil
	default:
		return compatTarget{}, fmt.Errorf("more than one %s", label)
	}
}

func listCompatTargets(ctx context.Context, opts globalOptions) ([]compatTarget, error) {
	if _, err := ensureDaemon(ctx, opts); err != nil {
		return nil, fmt.Errorf("failed to start daemon: %w", err)
	}
	socketPath, err := defaultDaemonSocketPath()
	if err != nil {
		return nil, err
	}
	resp, err := clidaemon.Send(ctx, socketPath, daemon.CommandDeviceList, nil, sendDaemonRequest)
	if err != nil {
		return nil, fmt.Errorf("query daemon: %w", err)
	}
	if !resp.OK {
		return nil, fmt.Errorf("daemon error: %s", daemonErrorMessage(resp))
	}
	devices, err := decodeDaemonDevices(resp.Result["devices"])
	if err != nil {
		return nil, fmt.Errorf("decode daemon response: %w", err)
	}
	targets := make([]compatTarget, 0, len(devices))
	for _, device := range devices {
		targets = append(targets, compatTargetFromDaemonDevice(device))
	}
	usbDevices, err := listUSBDevices(ctx)
	if err != nil && !errors.Is(err, adb.ErrUnsupported) {
		return nil, fmt.Errorf("list USB devices: %w", err)
	}
	for _, device := range usbDevices {
		targets = append(targets, compatTargetFromUSBDevice(device))
	}
	return targets, nil
}

func compatTargetFromDaemonDevice(device daemon.Device) compatTarget {
	return compatTarget{Serial: device.Serial, State: device.State, Transport: device.Transport, TCPAddress: device.Address}
}

func compatTargetFromUSBDevice(device adb.USBDevice) compatTarget {
	serial := fmt.Sprintf("usb:%03d:%03d", device.BusNumber, device.DeviceNumber)
	return compatTarget{
		Serial:    serial,
		State:     daemon.DeviceStateDevice,
		Transport: "usb",
		USBOptions: adb.USBOptions{
			DevicePath:   device.DevicePath,
			BusNumber:    device.BusNumber,
			DeviceNumber: device.DeviceNumber,
			VendorID:     device.VendorID,
			ProductID:    device.ProductID,
		},
	}
}
