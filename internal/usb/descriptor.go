package usb

import (
	"encoding/binary"
	"fmt"
)

const (
	descriptorTypeDevice        = 0x01
	descriptorTypeConfiguration = 0x02
	descriptorTypeInterface     = 0x04
	descriptorTypeEndpoint      = 0x05
)

type interfaceDescriptor struct {
	number   uint8
	class    uint8
	subclass uint8
	protocol uint8
}

type endpointDescriptor struct {
	address    uint8
	attributes uint8
}

// ParseADBCandidates parses raw USB descriptor bytes and returns ADB-capable
// interfaces found in the descriptor stream.
func ParseADBCandidates(devicePath string, busNumber, deviceNumber int, data []byte) ([]Candidate, error) {
	if len(data) < 2 {
		return nil, fmt.Errorf("usb descriptor data too short")
	}

	var vendorID, productID uint16
	var active *candidateBuilder
	var candidates []Candidate

	flush := func() {
		if active == nil {
			return
		}
		if active.bulkIn != 0 && active.bulkOut != 0 {
			candidates = append(candidates, Candidate{
				DevicePath:      devicePath,
				BusNumber:       busNumber,
				DeviceNumber:    deviceNumber,
				VendorID:        vendorID,
				ProductID:       productID,
				InterfaceNumber: active.iface.number,
				BulkInEndpoint:  active.bulkIn,
				BulkOutEndpoint: active.bulkOut,
			})
		}
		active = nil
	}

	for offset := 0; offset < len(data); {
		length := int(data[offset])
		if length == 0 {
			return nil, fmt.Errorf("usb descriptor at offset %d has zero length", offset)
		}
		if offset+length > len(data) {
			return nil, fmt.Errorf("usb descriptor at offset %d length %d exceeds data length %d", offset, length, len(data))
		}

		desc := data[offset : offset+length]
		typ := desc[1]
		switch typ {
		case descriptorTypeDevice:
			if length < 18 {
				return nil, fmt.Errorf("usb device descriptor at offset %d length %d too short", offset, length)
			}
			vendorID = binary.LittleEndian.Uint16(desc[8:10])
			productID = binary.LittleEndian.Uint16(desc[10:12])
		case descriptorTypeConfiguration:
			flush()
		case descriptorTypeInterface:
			flush()
			iface, ok := parseInterfaceDescriptor(desc)
			if ok && isADBInterface(iface) {
				active = &candidateBuilder{iface: iface}
			}
		case descriptorTypeEndpoint:
			if active != nil {
				if ep, ok := parseEndpointDescriptor(desc); ok && isBulkEndpoint(ep) {
					if ep.address&EndpointDirectionIn != 0 {
						active.bulkIn = ep.address
					} else {
						active.bulkOut = ep.address
					}
				}
			}
		}

		offset += length
	}
	flush()

	return candidates, nil
}

type candidateBuilder struct {
	iface   interfaceDescriptor
	bulkIn  uint8
	bulkOut uint8
}

func parseInterfaceDescriptor(desc []byte) (interfaceDescriptor, bool) {
	if len(desc) < 9 {
		return interfaceDescriptor{}, false
	}
	return interfaceDescriptor{
		number:   desc[2],
		class:    desc[5],
		subclass: desc[6],
		protocol: desc[7],
	}, true
}

func parseEndpointDescriptor(desc []byte) (endpointDescriptor, bool) {
	if len(desc) < 7 {
		return endpointDescriptor{}, false
	}
	return endpointDescriptor{address: desc[2], attributes: desc[3]}, true
}

func isADBInterface(iface interfaceDescriptor) bool {
	return iface.class == ADBInterfaceClass && iface.subclass == ADBInterfaceSubClass && iface.protocol == ADBInterfaceProtocol
}

func isBulkEndpoint(ep endpointDescriptor) bool {
	return ep.attributes&EndpointTransferMask == EndpointTransferBulk
}
