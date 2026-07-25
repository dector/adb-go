//go:build linux

package usb

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/dector/adb-go/protocol"
)

func TestLinuxUSBIntegrationHandshake(t *testing.T) {
	if os.Getenv("ADB_GO_USB_INTEGRATION") != "1" {
		t.Skip("set ADB_GO_USB_INTEGRATION=1 to run the Linux USB integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	candidates, err := DiscoverADB(ctx)
	if err != nil {
		t.Fatalf("DiscoverADB() error = %v", err)
	}
	if len(candidates) == 0 {
		t.Skip("no ADB USB candidates found under /dev/bus/usb")
	}

	transport, err := OpenBulkTransport(ctx, candidates[0])
	if err != nil {
		t.Fatalf("OpenBulkTransport(%+v) error = %v", candidates[0], err)
	}
	conn := protocol.NewConnection(transport)
	defer conn.Close()

	if _, err := conn.Handshake(ctx); err != nil && !errors.Is(err, protocol.ErrAuthRequired) {
		t.Fatalf("Handshake() error = %v, want nil or AUTH-required response", err)
	}
}
