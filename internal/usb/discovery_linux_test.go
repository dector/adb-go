//go:build linux

package usb

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverADBInRootUsesFixtureDeviceTree(t *testing.T) {
	root := t.TempDir()
	devicePath := filepath.Join(root, "001", "002")
	if err := os.MkdirAll(filepath.Dir(devicePath), 0o777); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(devicePath, fixtureDescriptors(
		deviceDescriptor(0x18d1, 0x4ee7),
		configurationDescriptor(),
		interfaceDescriptorBytes(3, ADBInterfaceClass, ADBInterfaceSubClass, ADBInterfaceProtocol),
		endpointDescriptorBytes(0x81, EndpointTransferBulk),
		endpointDescriptorBytes(0x02, EndpointTransferBulk),
	), 0o666); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	candidates, err := DiscoverADBInRoot(context.Background(), root)
	if err != nil {
		t.Fatalf("DiscoverADBInRoot() error = %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("DiscoverADBInRoot() returned %d candidates, want 1", len(candidates))
	}
	if candidates[0].DevicePath != devicePath || candidates[0].BusNumber != 1 || candidates[0].DeviceNumber != 2 {
		t.Fatalf("candidate location = path %q bus %d device %d, want %q bus 1 device 2", candidates[0].DevicePath, candidates[0].BusNumber, candidates[0].DeviceNumber, devicePath)
	}
}

func TestParseLinuxDevicePathRejectsNonUSBPath(t *testing.T) {
	if _, _, ok := parseLinuxDevicePath("/dev/bus/usb", "/dev/bus/usb/not-a-bus"); ok {
		t.Fatal("parseLinuxDevicePath() ok = true, want false")
	}
}
