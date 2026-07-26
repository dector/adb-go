package client

import (
	"context"
	"fmt"
)

// RebootMode selects a device reboot target supported by Client.Reboot.
type RebootMode string

const (
	// RebootNormal requests a normal Android reboot.
	RebootNormal RebootMode = ""

	// RebootBootloader requests a reboot into the device bootloader.
	RebootBootloader RebootMode = "bootloader"

	// RebootRecovery requests a reboot into Android recovery.
	RebootRecovery RebootMode = "recovery"
)

// Reboot asks the connected device to reboot into mode.
//
// adb-go uses the ADB "reboot:<mode>" service instead of a shell command so the
// request follows the same direct daemon service path as official adb. A normal
// reboot is encoded as "reboot:"; bootloader and recovery are encoded as
// "reboot:bootloader" and "reboot:recovery". The operation is disruptive: a
// successful request may close the device-side ADB connection immediately.
func (c *Client) Reboot(ctx context.Context, mode RebootMode) error {
	service, err := rebootService(mode)
	if err != nil {
		return err
	}

	stream, err := c.OpenService(ctx, service)
	if err != nil {
		return fmt.Errorf("adb reboot %s: %w", rebootModeLabel(mode), err)
	}
	if err := stream.Close(); err != nil {
		return fmt.Errorf("adb reboot %s: close service: %w", rebootModeLabel(mode), err)
	}
	return nil
}

func rebootService(mode RebootMode) (string, error) {
	switch mode {
	case RebootNormal, RebootBootloader, RebootRecovery:
		return "reboot:" + string(mode), nil
	default:
		return "", fmt.Errorf("adb reboot mode %q: %w", string(mode), ErrUnsupported)
	}
}

func rebootModeLabel(mode RebootMode) string {
	if mode == RebootNormal {
		return "normal"
	}
	return string(mode)
}
