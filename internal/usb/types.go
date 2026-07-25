// Package usb contains internal USB discovery primitives for ADB transports.
package usb

import "errors"

const (
	ADBInterfaceClass    = 0xff
	ADBInterfaceSubClass = 0x42
	ADBInterfaceProtocol = 0x01

	EndpointDirectionIn  = 0x80
	EndpointTransferMask = 0x03
	EndpointTransferBulk = 0x02
)

var ErrUnsupported = errors.New("adb usb unsupported on this platform")

// Candidate describes one USB interface that looks like an ADB transport.
type Candidate struct {
	DevicePath      string
	BusNumber       int
	DeviceNumber    int
	VendorID        uint16
	ProductID       uint16
	InterfaceNumber uint8
	BulkInEndpoint  uint8
	BulkOutEndpoint uint8
}
