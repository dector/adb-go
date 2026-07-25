//go:build linux

package usb

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"unsafe"
)

func TestOpenBulkTransportRejectsInvalidCandidate(t *testing.T) {
	if _, err := OpenBulkTransport(context.Background(), Candidate{}); err == nil {
		t.Fatal("OpenBulkTransport() error = nil, want invalid candidate error")
	}

	path := filepath.Join(t.TempDir(), "device")
	candidate := Candidate{DevicePath: path, InterfaceNumber: 1, BulkInEndpoint: 0x01, BulkOutEndpoint: 0x02}
	if _, err := OpenBulkTransport(context.Background(), candidate); err == nil {
		t.Fatal("OpenBulkTransport() error = nil, want invalid IN endpoint error")
	}

	candidate.BulkInEndpoint = 0x81
	candidate.BulkOutEndpoint = 0x82
	if _, err := OpenBulkTransport(context.Background(), candidate); err == nil {
		t.Fatal("OpenBulkTransport() error = nil, want invalid OUT endpoint error")
	}
}

func TestOpenBulkTransportMapsClaimPermissionError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "device")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	restore := replaceUSBFSIoctl(func(fd uintptr, request uintptr, arg uintptr) (int, error) {
		if request != usbdevfsClaimInterface {
			t.Fatalf("ioctl request = %#x, want claim %#x", request, usbdevfsClaimInterface)
		}
		return -1, syscall.EPERM
	})
	defer restore()

	candidate := Candidate{DevicePath: path, InterfaceNumber: 1, BulkInEndpoint: 0x81, BulkOutEndpoint: 0x02}
	_, err := OpenBulkTransport(context.Background(), candidate)
	if !errors.Is(err, syscall.EPERM) {
		t.Fatalf("OpenBulkTransport() error = %v, want errors.Is EPERM", err)
	}
}

func TestBulkTransportReadWriteAndCloseUseUSBFSIoctls(t *testing.T) {
	path := filepath.Join(t.TempDir(), "device")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatalf("OpenFile() error = %v", err)
	}

	var releases int
	var bulks []usbdevfsBulkTransfer
	restore := replaceUSBFSIoctl(func(fd uintptr, request uintptr, arg uintptr) (int, error) {
		switch request {
		case usbdevfsBulk:
			transfer := *(*usbdevfsBulkTransfer)(unsafe.Pointer(arg))
			bulks = append(bulks, transfer)
			return int(transfer.Length), nil
		case usbdevfsReleaseInterface:
			releases++
			iface := *(*uint32)(unsafe.Pointer(arg))
			if iface != 3 {
				t.Fatalf("release interface = %d, want 3", iface)
			}
			return 0, nil
		default:
			t.Fatalf("unexpected ioctl request %#x", request)
			return -1, syscall.EINVAL
		}
	})
	defer restore()

	transport := &linuxBulkTransport{file: file, path: path, iface: 3, inEndpoint: 0x81, outEndpoint: 0x02, claimed: true}
	buf := make([]byte, 8)
	if n, err := transport.Read(buf); n != len(buf) || err != nil {
		t.Fatalf("Read() = %d, %v; want %d, nil", n, err, len(buf))
	}
	payload := []byte{1, 2, 3}
	if n, err := transport.Write(payload); n != len(payload) || err != nil {
		t.Fatalf("Write() = %d, %v; want %d, nil", n, err, len(payload))
	}
	if err := transport.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := transport.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if releases != 1 {
		t.Fatalf("release calls = %d, want 1", releases)
	}
	if len(bulks) != 2 {
		t.Fatalf("bulk calls = %d, want 2", len(bulks))
	}
	if bulks[0].Endpoint != 0x81 || bulks[1].Endpoint != 0x02 {
		t.Fatalf("bulk endpoints = %#x, %#x; want 0x81, 0x02", bulks[0].Endpoint, bulks[1].Endpoint)
	}
}

func TestOpenBulkTransportHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := OpenBulkTransport(ctx, Candidate{DevicePath: "/dev/null", InterfaceNumber: 1, BulkInEndpoint: 0x81, BulkOutEndpoint: 0x02})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("OpenBulkTransport() error = %v, want context.Canceled", err)
	}
}

func replaceUSBFSIoctl(fn func(fd uintptr, request uintptr, arg uintptr) (int, error)) func() {
	old := usbfsIoctl
	usbfsIoctl = fn
	return func() { usbfsIoctl = old }
}
