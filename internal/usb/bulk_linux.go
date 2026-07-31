//go:build linux

package usb

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"syscall"
	"unsafe"
)

const defaultUSBFSBulkTimeoutMillis = 0

const (
	ioctlNRBits   = 8
	ioctlTypeBits = 8
	ioctlSizeBits = 14
	ioctlDirBits  = 2

	ioctlNRShift   = 0
	ioctlTypeShift = ioctlNRShift + ioctlNRBits
	ioctlSizeShift = ioctlTypeShift + ioctlTypeBits
	ioctlDirShift  = ioctlSizeShift + ioctlSizeBits

	ioctlRead  = 2
	ioctlWrite = 1

	usbfsIOType = 'U'
)

var (
	usbdevfsBulk             = ioctl(ioctlRead|ioctlWrite, usbfsIOType, 2, unsafe.Sizeof(usbdevfsBulkTransfer{}))
	usbdevfsClaimInterface   = ioctl(ioctlRead, usbfsIOType, 15, unsafe.Sizeof(uint32(0)))
	usbdevfsReleaseInterface = ioctl(ioctlRead, usbfsIOType, 16, unsafe.Sizeof(uint32(0)))
)

type usbdevfsBulkTransfer struct {
	Endpoint uint32
	Length   uint32
	Timeout  uint32
	Data     uintptr
}

type linuxBulkTransport struct {
	file        *os.File
	path        string
	iface       uint32
	inEndpoint  uint8
	outEndpoint uint8

	mu        sync.Mutex
	closeOnce sync.Once
	closeErr  error
	claimed   bool
}

var usbfsIoctl = func(fd uintptr, request uintptr, arg unsafe.Pointer) (int, error) {
	r0, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, request, uintptr(arg))
	if errno != 0 {
		return int(r0), errno
	}
	return int(r0), nil
}

func ioctl(dir, typ, nr uintptr, size uintptr) uintptr {
	return (dir << ioctlDirShift) | (typ << ioctlTypeShift) | (nr << ioctlNRShift) | (size << ioctlSizeShift)
}

// OpenBulkTransport opens candidate's Linux usbfs device node, claims the ADB
// interface, and returns an io.ReadWriteCloser that moves bytes over the
// selected bulk endpoints.
func OpenBulkTransport(ctx context.Context, candidate Candidate) (io.ReadWriteCloser, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateBulkCandidate(candidate); err != nil {
		return nil, err
	}

	file, err := os.OpenFile(candidate.DevicePath, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("adb usb open %s: %w", candidate.DevicePath, err)
	}

	transport := &linuxBulkTransport{
		file:        file,
		path:        candidate.DevicePath,
		iface:       uint32(candidate.InterfaceNumber),
		inEndpoint:  candidate.BulkInEndpoint,
		outEndpoint: candidate.BulkOutEndpoint,
	}
	if err := transport.claimInterface(); err != nil {
		_ = file.Close()
		return nil, err
	}

	if done := ctx.Done(); done != nil {
		go func() {
			<-done
			_ = transport.Close()
		}()
	}

	return transport, nil
}

func validateBulkCandidate(candidate Candidate) error {
	if candidate.DevicePath == "" {
		return fmt.Errorf("adb usb open: device path is empty")
	}
	if candidate.BulkInEndpoint&EndpointDirectionIn == 0 {
		return fmt.Errorf("adb usb open %s: bulk IN endpoint %#02x is not an IN endpoint", candidate.DevicePath, candidate.BulkInEndpoint)
	}
	if candidate.BulkOutEndpoint&EndpointDirectionIn != 0 {
		return fmt.Errorf("adb usb open %s: bulk OUT endpoint %#02x is not an OUT endpoint", candidate.DevicePath, candidate.BulkOutEndpoint)
	}
	return nil
}

func (t *linuxBulkTransport) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	return t.bulk(t.inEndpoint, p)
}

func (t *linuxBulkTransport) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	return t.bulk(t.outEndpoint, p)
}

func (t *linuxBulkTransport) Close() error {
	t.closeOnce.Do(func() {
		// Do not take t.mu here. Read and Write may be blocked inside a usbfs
		// ioctl while holding that lock, and closing the device file is the
		// cancellation mechanism that unblocks those operations.
		if t.claimed {
			iface := t.iface
			if _, err := usbfsIoctl(t.file.Fd(), usbdevfsReleaseInterface, unsafe.Pointer(&iface)); err != nil {
				t.closeErr = fmt.Errorf("adb usb release interface %d on %s: %w", t.iface, t.path, err)
			}
			t.claimed = false
		}
		if err := t.file.Close(); err != nil && t.closeErr == nil {
			t.closeErr = fmt.Errorf("adb usb close %s: %w", t.path, err)
		}
	})
	return t.closeErr
}

func (t *linuxBulkTransport) claimInterface() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	iface := t.iface
	if _, err := usbfsIoctl(t.file.Fd(), usbdevfsClaimInterface, unsafe.Pointer(&iface)); err != nil {
		return fmt.Errorf("adb usb claim interface %d on %s: %w", t.iface, t.path, err)
	}
	t.claimed = true
	return nil
}

func (t *linuxBulkTransport) bulk(endpoint uint8, p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	transfer := usbdevfsBulkTransfer{
		Endpoint: uint32(endpoint),
		Length:   uint32(len(p)),
		Timeout:  defaultUSBFSBulkTimeoutMillis,
		Data:     uintptr(unsafe.Pointer(&p[0])),
	}
	n, err := usbfsIoctl(t.file.Fd(), usbdevfsBulk, unsafe.Pointer(&transfer))
	if err != nil {
		return 0, fmt.Errorf("adb usb bulk endpoint %#02x on %s: %w", endpoint, t.path, err)
	}
	return n, nil
}
