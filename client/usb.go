package client

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/dector/adb-go/internal/usb"
)

// USBOptions selects an ADB-capable USB interface for ConnectUSB.
//
// The initial USB implementation is Linux-only and discovers devices from
// /dev/bus/usb. Leave all fields zero to connect only when exactly one ADB USB
// interface is present. Set one or more fields to narrow selection when
// multiple devices are connected.
type USBOptions struct {
	// DevicePath is a Linux usbfs path such as /dev/bus/usb/001/002.
	DevicePath string

	// BusNumber and DeviceNumber select a Linux usbfs bus/device number pair,
	// for example bus 1 device 2 for /dev/bus/usb/001/002. If either field is
	// set, both fields must be set.
	BusNumber    int
	DeviceNumber int

	// VendorID and ProductID select by USB device IDs. If either field is set,
	// both fields must be set.
	VendorID  uint16
	ProductID uint16

	// Serial is reserved for future USB string descriptor support. Serial-based
	// selection is not implemented yet.
	Serial string
}

type usbTransportDialer struct {
	opts USBOptions
}

var (
	discoverUSBCandidates = usb.DiscoverADB
	openUSBTransport      = usb.OpenBulkTransport
)

// ConnectUSB connects to an ADB device over USB and performs the initial ADB
// CNXN handshake before returning. The initial USB backend is Linux-only. On
// unsupported platforms ConnectUSB returns an error matching ErrUnsupported.
func ConnectUSB(ctx context.Context, opts USBOptions) (*Client, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return connectWithTransport(ctx, usbTransportDialer{opts: opts})
}

func (d usbTransportDialer) DialTransport(ctx context.Context) (io.ReadWriteCloser, error) {
	candidate, err := selectUSBCandidate(ctx, d.opts)
	if err != nil {
		return nil, err
	}
	transport, err := openUSBTransport(ctx, candidate)
	if err != nil {
		if errors.Is(err, usb.ErrUnsupported) {
			return nil, ErrUnsupported
		}
		return nil, err
	}
	return transport, nil
}

func (d usbTransportDialer) ConnectDescription() string {
	return "usb"
}

func selectUSBCandidate(ctx context.Context, opts USBOptions) (usb.Candidate, error) {
	if err := validateUSBOptions(opts); err != nil {
		return usb.Candidate{}, err
	}

	candidates, err := discoverUSBCandidates(ctx)
	if err != nil {
		if errors.Is(err, usb.ErrUnsupported) {
			return usb.Candidate{}, ErrUnsupported
		}
		return usb.Candidate{}, err
	}

	matches := candidates[:0]
	for _, candidate := range candidates {
		if usbCandidateMatches(candidate, opts) {
			matches = append(matches, candidate)
		}
	}

	switch len(matches) {
	case 0:
		return usb.Candidate{}, fmt.Errorf("adb usb: no matching ADB USB device found")
	case 1:
		return matches[0], nil
	default:
		return usb.Candidate{}, fmt.Errorf("adb usb: %d matching ADB USB devices found; specify DevicePath, BusNumber/DeviceNumber, or VendorID/ProductID", len(matches))
	}
}

func validateUSBOptions(opts USBOptions) error {
	if opts.Serial != "" {
		return fmt.Errorf("adb usb serial selection %q: %w", opts.Serial, ErrUnsupported)
	}
	if (opts.BusNumber == 0) != (opts.DeviceNumber == 0) {
		return fmt.Errorf("adb usb selection requires both BusNumber and DeviceNumber")
	}
	if (opts.VendorID == 0) != (opts.ProductID == 0) {
		return fmt.Errorf("adb usb selection requires both VendorID and ProductID")
	}
	return nil
}

func usbCandidateMatches(candidate usb.Candidate, opts USBOptions) bool {
	if opts.DevicePath != "" && candidate.DevicePath != opts.DevicePath {
		return false
	}
	if opts.BusNumber != 0 && (candidate.BusNumber != opts.BusNumber || candidate.DeviceNumber != opts.DeviceNumber) {
		return false
	}
	if opts.VendorID != 0 && (candidate.VendorID != opts.VendorID || candidate.ProductID != opts.ProductID) {
		return false
	}
	return true
}
