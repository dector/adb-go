package client

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/dector/adb-go/internal/fakeadb"
	"github.com/dector/adb-go/protocol"
)

func TestRebootOpensNormalRebootService(t *testing.T) {
	server := fakeadb.Start(t)
	opened := make(chan string, 1)
	server.Handle("reboot:", rebootServiceHandler(t, opened))

	client, err := Connect(context.Background(), server.Addr())
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer client.Close()

	if err := client.Reboot(context.Background(), RebootNormal); err != nil {
		t.Fatalf("Reboot(normal) error = %v", err)
	}
	if got := waitOpenedService(t, opened); got != "reboot:" {
		t.Fatalf("opened service = %q, want reboot:", got)
	}
}

func TestRebootOpensSupportedModeServices(t *testing.T) {
	tests := []struct {
		name string
		mode RebootMode
		want string
	}{
		{name: "bootloader", mode: RebootBootloader, want: "reboot:bootloader"},
		{name: "recovery", mode: RebootRecovery, want: "reboot:recovery"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := fakeadb.Start(t)
			opened := make(chan string, 1)
			server.Handle(tt.want, rebootServiceHandler(t, opened))

			client, err := Connect(context.Background(), server.Addr())
			if err != nil {
				t.Fatalf("Connect() error = %v", err)
			}
			defer client.Close()

			if err := client.Reboot(context.Background(), tt.mode); err != nil {
				t.Fatalf("Reboot(%s) error = %v", tt.mode, err)
			}
			if got := waitOpenedService(t, opened); got != tt.want {
				t.Fatalf("opened service = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRebootRejectsUnsupportedMode(t *testing.T) {
	server := fakeadb.Start(t)

	client, err := Connect(context.Background(), server.Addr())
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer client.Close()

	err = client.Reboot(context.Background(), RebootMode("sideload"))
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Reboot(unsupported) error = %v, want ErrUnsupported", err)
	}
}

func TestRebootServiceHelpers(t *testing.T) {
	tests := []struct {
		mode RebootMode
		want string
	}{
		{mode: RebootNormal, want: "reboot:"},
		{mode: RebootBootloader, want: "reboot:bootloader"},
		{mode: RebootRecovery, want: "reboot:recovery"},
	}

	for _, tt := range tests {
		got, err := rebootService(tt.mode)
		if err != nil {
			t.Fatalf("rebootService(%q) error = %v", tt.mode, err)
		}
		if got != tt.want {
			t.Fatalf("rebootService(%q) = %q, want %q", tt.mode, got, tt.want)
		}
	}

	if _, err := rebootService(RebootMode("fastboot")); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("rebootService(unsupported) error = %v, want ErrUnsupported", err)
	}
}

func rebootServiceHandler(t testing.TB, opened chan<- string) fakeadb.ServiceHandler {
	t.Helper()
	return func(ctx context.Context, conn io.ReadWriter, open protocol.Message) {
		opened <- string(open.Payload[:len(open.Payload)-1])
		remoteID := uint32(42)
		_ = protocol.WriteMessage(conn, protocol.Message{Command: protocol.CommandOKAY, Arg0: remoteID, Arg1: open.Arg0})
		_ = protocol.WriteMessage(conn, protocol.Message{Command: protocol.CommandCLSE, Arg0: remoteID, Arg1: open.Arg0})
	}
}

func waitOpenedService(t testing.TB, opened <-chan string) string {
	t.Helper()
	select {
	case service := <-opened:
		return service
	case <-time.After(time.Second):
		t.Fatal("reboot service was not opened")
		return ""
	}
}
