package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"testing"

	"github.com/dector/adb-go/internal/usb"
	"github.com/dector/adb-go/protocol"
)

func TestConnectUSBPerformsHandshakeThroughSelectedTransport(t *testing.T) {
	clientSide, serverSide := net.Pipe()
	defer serverSide.Close()

	restore := replaceUSBHooks(
		func(ctx context.Context) ([]usb.Candidate, error) {
			return []usb.Candidate{{DevicePath: "/dev/bus/usb/001/002", BusNumber: 1, DeviceNumber: 2, VendorID: 0x18d1, ProductID: 0x4ee7, InterfaceNumber: 3, BulkInEndpoint: 0x81, BulkOutEndpoint: 0x02}}, nil
		},
		func(ctx context.Context, candidate usb.Candidate) (io.ReadWriteCloser, error) {
			if candidate.DevicePath != "/dev/bus/usb/001/002" {
				return nil, fmt.Errorf("candidate path = %q, want fixture path", candidate.DevicePath)
			}
			return clientSide, nil
		},
	)
	defer restore()

	done := make(chan error, 1)
	go func() {
		req, err := protocol.ReadMessage(serverSide)
		if err != nil {
			done <- err
			return
		}
		if req.Command != protocol.CommandCNXN {
			done <- fmt.Errorf("first command = %#x, want CNXN", uint32(req.Command))
			return
		}
		done <- protocol.WriteMessage(serverSide, protocol.Message{Command: protocol.CommandCNXN, Arg0: protocol.Version, Arg1: protocol.MaxPayload, Payload: []byte("device::usb-test\x00")})
	}()

	client, err := ConnectUSB(context.Background(), USBOptions{})
	if err != nil {
		t.Fatalf("ConnectUSB() error = %v", err)
	}
	defer client.Close()
	if err := <-done; err != nil {
		t.Fatalf("USB handshake server error = %v", err)
	}
}

func TestConnectUSBMapsAuthRequired(t *testing.T) {
	clientSide, serverSide := net.Pipe()
	defer serverSide.Close()

	restore := replaceUSBHooks(
		func(ctx context.Context) ([]usb.Candidate, error) {
			return []usb.Candidate{{DevicePath: "/dev/bus/usb/001/002", BulkInEndpoint: 0x81, BulkOutEndpoint: 0x02}}, nil
		},
		func(ctx context.Context, candidate usb.Candidate) (io.ReadWriteCloser, error) { return clientSide, nil },
	)
	defer restore()

	done := make(chan error, 1)
	go func() {
		if _, err := protocol.ReadMessage(serverSide); err != nil {
			done <- err
			return
		}
		done <- protocol.WriteMessage(serverSide, protocol.Message{Command: protocol.CommandAUTH, Arg0: 1, Payload: []byte("token")})
	}()

	client, err := ConnectUSB(context.Background(), USBOptions{})
	if err == nil {
		_ = client.Close()
		t.Fatal("ConnectUSB() error = nil, want auth required")
	}
	if !errors.Is(err, ErrAuthRequired) {
		t.Fatalf("ConnectUSB() error = %v, want errors.Is ErrAuthRequired", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("USB auth server error = %v", err)
	}
}

func TestSelectUSBCandidateRequiresSelectionWhenAmbiguous(t *testing.T) {
	restore := replaceUSBHooks(
		func(ctx context.Context) ([]usb.Candidate, error) {
			return []usb.Candidate{
				{DevicePath: "/dev/bus/usb/001/002", BusNumber: 1, DeviceNumber: 2, VendorID: 0x18d1, ProductID: 0x4ee7},
				{DevicePath: "/dev/bus/usb/001/003", BusNumber: 1, DeviceNumber: 3, VendorID: 0x18d1, ProductID: 0x4ee8},
			}, nil
		},
		func(ctx context.Context, candidate usb.Candidate) (io.ReadWriteCloser, error) {
			t.Fatal("openUSBTransport should not be called for ambiguous selection")
			return nil, nil
		},
	)
	defer restore()

	if _, err := selectUSBCandidate(context.Background(), USBOptions{}); err == nil {
		t.Fatal("selectUSBCandidate() error = nil, want ambiguity error")
	}
}

func TestSelectUSBCandidateFiltersByOptions(t *testing.T) {
	restore := replaceUSBHooks(
		func(ctx context.Context) ([]usb.Candidate, error) {
			return []usb.Candidate{
				{DevicePath: "/dev/bus/usb/001/002", BusNumber: 1, DeviceNumber: 2, VendorID: 0x18d1, ProductID: 0x4ee7},
				{DevicePath: "/dev/bus/usb/001/003", BusNumber: 1, DeviceNumber: 3, VendorID: 0x18d1, ProductID: 0x4ee8},
			}, nil
		},
		func(ctx context.Context, candidate usb.Candidate) (io.ReadWriteCloser, error) { return nil, nil },
	)
	defer restore()

	candidate, err := selectUSBCandidate(context.Background(), USBOptions{DevicePath: "/dev/bus/usb/001/003"})
	if err != nil {
		t.Fatalf("selectUSBCandidate(DevicePath) error = %v", err)
	}
	if candidate.DeviceNumber != 3 {
		t.Fatalf("selected device = %d, want 3", candidate.DeviceNumber)
	}

	candidate, err = selectUSBCandidate(context.Background(), USBOptions{BusNumber: 1, DeviceNumber: 2, VendorID: 0x18d1, ProductID: 0x4ee7})
	if err != nil {
		t.Fatalf("selectUSBCandidate(bus/device + vid/pid) error = %v", err)
	}
	if candidate.DeviceNumber != 2 {
		t.Fatalf("selected device = %d, want 2", candidate.DeviceNumber)
	}
}

func TestConnectUSBOptionValidationAndUnsupportedMapping(t *testing.T) {
	if _, err := selectUSBCandidate(context.Background(), USBOptions{BusNumber: 1}); err == nil {
		t.Fatal("selectUSBCandidate(BusNumber only) error = nil, want validation error")
	}
	if _, err := selectUSBCandidate(context.Background(), USBOptions{VendorID: 0x18d1}); err == nil {
		t.Fatal("selectUSBCandidate(VendorID only) error = nil, want validation error")
	}
	if _, err := selectUSBCandidate(context.Background(), USBOptions{Serial: "abc"}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("selectUSBCandidate(Serial) error = %v, want ErrUnsupported", err)
	}

	restore := replaceUSBHooks(
		func(ctx context.Context) ([]usb.Candidate, error) { return nil, usb.ErrUnsupported },
		func(ctx context.Context, candidate usb.Candidate) (io.ReadWriteCloser, error) { return nil, nil },
	)
	defer restore()
	if _, err := ConnectUSB(context.Background(), USBOptions{}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("ConnectUSB() error = %v, want ErrUnsupported", err)
	}
}

func TestSelectUSBCandidateNoMatches(t *testing.T) {
	restore := replaceUSBHooks(
		func(ctx context.Context) ([]usb.Candidate, error) { return nil, nil },
		func(ctx context.Context, candidate usb.Candidate) (io.ReadWriteCloser, error) { return nil, nil },
	)
	defer restore()

	if _, err := selectUSBCandidate(context.Background(), USBOptions{}); err == nil {
		t.Fatal("selectUSBCandidate() error = nil, want no-device error")
	}
}

func replaceUSBHooks(discover func(context.Context) ([]usb.Candidate, error), open func(context.Context, usb.Candidate) (io.ReadWriteCloser, error)) func() {
	oldDiscover := discoverUSBCandidates
	oldOpen := openUSBTransport
	discoverUSBCandidates = discover
	openUSBTransport = open
	return func() {
		discoverUSBCandidates = oldDiscover
		openUSBTransport = oldOpen
	}
}
