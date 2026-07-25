package usb

import "testing"

func TestParseADBCandidatesFindsADBInterface(t *testing.T) {
	candidates, err := ParseADBCandidates("/dev/bus/usb/001/002", 1, 2, fixtureDescriptors(
		deviceDescriptor(0x18d1, 0x4ee7),
		configurationDescriptor(),
		interfaceDescriptorBytes(0, 0x08, 0x06, 0x50),
		endpointDescriptorBytes(0x83, EndpointTransferBulk),
		endpointDescriptorBytes(0x04, EndpointTransferBulk),
		interfaceDescriptorBytes(1, ADBInterfaceClass, ADBInterfaceSubClass, ADBInterfaceProtocol),
		endpointDescriptorBytes(0x81, EndpointTransferBulk),
		endpointDescriptorBytes(0x02, EndpointTransferBulk),
	))
	if err != nil {
		t.Fatalf("ParseADBCandidates() error = %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("ParseADBCandidates() returned %d candidates, want 1", len(candidates))
	}

	got := candidates[0]
	if got.DevicePath != "/dev/bus/usb/001/002" || got.BusNumber != 1 || got.DeviceNumber != 2 {
		t.Fatalf("candidate location = path %q bus %d device %d, want /dev/bus/usb/001/002 bus 1 device 2", got.DevicePath, got.BusNumber, got.DeviceNumber)
	}
	if got.VendorID != 0x18d1 || got.ProductID != 0x4ee7 {
		t.Fatalf("candidate IDs = %04x:%04x, want 18d1:4ee7", got.VendorID, got.ProductID)
	}
	if got.InterfaceNumber != 1 {
		t.Fatalf("candidate interface = %d, want 1", got.InterfaceNumber)
	}
	if got.BulkInEndpoint != 0x81 || got.BulkOutEndpoint != 0x02 {
		t.Fatalf("candidate endpoints = in %#x out %#x, want in 0x81 out 0x02", got.BulkInEndpoint, got.BulkOutEndpoint)
	}
}

func TestParseADBCandidatesSkipsADBInterfaceMissingBulkOut(t *testing.T) {
	candidates, err := ParseADBCandidates("fixture", 0, 0, fixtureDescriptors(
		deviceDescriptor(0x18d1, 0x4ee7),
		configurationDescriptor(),
		interfaceDescriptorBytes(1, ADBInterfaceClass, ADBInterfaceSubClass, ADBInterfaceProtocol),
		endpointDescriptorBytes(0x81, EndpointTransferBulk),
	))
	if err != nil {
		t.Fatalf("ParseADBCandidates() error = %v", err)
	}
	if len(candidates) != 0 {
		t.Fatalf("ParseADBCandidates() returned %d candidates, want 0", len(candidates))
	}
}

func TestParseADBCandidatesRejectsMalformedDescriptor(t *testing.T) {
	_, err := ParseADBCandidates("fixture", 0, 0, []byte{9, descriptorTypeInterface, 1})
	if err == nil {
		t.Fatal("ParseADBCandidates() error = nil, want malformed descriptor error")
	}
}

func fixtureDescriptors(parts ...[]byte) []byte {
	var out []byte
	for _, part := range parts {
		out = append(out, part...)
	}
	return out
}

func deviceDescriptor(vendorID, productID uint16) []byte {
	return []byte{
		18, descriptorTypeDevice,
		0x00, 0x02,
		0, 0, 0, 64,
		byte(vendorID), byte(vendorID >> 8),
		byte(productID), byte(productID >> 8),
		0x00, 0x01,
		1, 2, 3, 1,
	}
}

func configurationDescriptor() []byte {
	return []byte{9, descriptorTypeConfiguration, 32, 0, 1, 1, 0, 0x80, 50}
}

func interfaceDescriptorBytes(number, class, subclass, protocol uint8) []byte {
	return []byte{9, descriptorTypeInterface, number, 0, 2, class, subclass, protocol, 0}
}

func endpointDescriptorBytes(address, attributes uint8) []byte {
	return []byte{7, descriptorTypeEndpoint, address, attributes, 64, 0, 0}
}
