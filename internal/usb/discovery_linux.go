//go:build linux

package usb

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
)

const defaultDeviceRoot = "/dev/bus/usb"

// DiscoverADB finds ADB-capable USB interfaces under the default Linux usbfs
// root, /dev/bus/usb.
func DiscoverADB(ctx context.Context) ([]Candidate, error) {
	return DiscoverADBInRoot(ctx, defaultDeviceRoot)
}

// DiscoverADBInRoot finds ADB-capable USB interfaces under root. It exists so
// discovery can be tested against fixture directories without USB hardware.
func DiscoverADBInRoot(ctx context.Context, root string) ([]Candidate, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var candidates []Candidate
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}

		bus, dev, ok := parseLinuxDevicePath(root, path)
		if !ok {
			return nil
		}

		data, err := readDescriptorFile(path)
		if err != nil {
			return fmt.Errorf("adb usb read descriptors %s: %w", path, err)
		}
		found, err := ParseADBCandidates(path, bus, dev, data)
		if err != nil {
			return fmt.Errorf("adb usb parse descriptors %s: %w", path, err)
		}
		candidates = append(candidates, found...)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("adb usb discover %s: %w", root, err)
	}
	return candidates, nil
}

func parseLinuxDevicePath(root, path string) (busNumber, deviceNumber int, ok bool) {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return 0, 0, false
	}
	busText := filepath.Dir(rel)
	devText := filepath.Base(rel)
	if busText == "." || busText == string(filepath.Separator) || devText == "." {
		return 0, 0, false
	}
	bus, err := strconv.Atoi(busText)
	if err != nil {
		return 0, 0, false
	}
	dev, err := strconv.Atoi(devText)
	if err != nil {
		return 0, 0, false
	}
	return bus, dev, true
}

func readDescriptorFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	return io.ReadAll(io.LimitReader(file, 1<<20))
}
