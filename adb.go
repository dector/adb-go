// Package adb provides the public adb-go API.
package adb

import (
	"context"

	"github.com/dector/adb-go/auth"
	"github.com/dector/adb-go/client"
	"github.com/dector/adb-go/protocol"
)

// Client is a high-level ADB client connected to one device transport.
type Client = client.Client

// PullOptions controls PullFileWithOptions behavior.
type PullOptions = client.PullOptions

// InstallOptions controls InstallAPKWithOptions behavior.
type InstallOptions = client.InstallOptions

// LogcatOptions controls Client.Logcat behavior.
type LogcatOptions = client.LogcatOptions

// ConnectOptions controls optional high-level connection behavior.
type ConnectOptions = client.ConnectOptions

// AuthCredential supplies one ADB host credential to an authenticated
// connection handshake.
type AuthCredential = protocol.AuthCredential

// Credential is an explicit ADB host authentication credential.
type Credential = auth.Credential

// USBDevice describes one locally visible ADB-capable USB interface.
type USBDevice = client.USBDevice

// USBOptions selects an ADB-capable USB interface for ConnectUSB.
type USBOptions = client.USBOptions

// TCPTarget describes one TCP ADB connection target discovered by scanning.
type TCPTarget = client.TCPTarget

// TCPScanOptions controls TCP ADB target scanning.
type TCPScanOptions = client.TCPScanOptions

var (
	// ErrAuthRequired reports that the ADB peer requires authentication that is
	// not implemented by adb-go yet.
	ErrAuthRequired = client.ErrAuthRequired

	// ErrUnsupported reports that the requested operation is not supported yet.
	ErrUnsupported = client.ErrUnsupported

	// ErrDestinationExists reports that a pull destination already exists and
	// overwrite was not explicitly requested.
	ErrDestinationExists = client.ErrDestinationExists
)

// Connect connects to addr over TCP. If addr omits a port, the default ADB TCP
// port 5555 is used.
func Connect(ctx context.Context, addr string) (*Client, error) {
	return client.Connect(ctx, addr)
}

// ConnectWithOptions connects to addr over TCP using opts.
func ConnectWithOptions(ctx context.Context, addr string, opts ConnectOptions) (*Client, error) {
	return client.ConnectWithOptions(ctx, addr, opts)
}

// ConnectTCP connects to addr over TCP and performs the initial ADB CNXN
// handshake before returning.
func ConnectTCP(ctx context.Context, addr string) (*Client, error) {
	return client.ConnectTCP(ctx, addr)
}

// ConnectTCPWithOptions connects to addr over TCP using opts.
func ConnectTCPWithOptions(ctx context.Context, addr string, opts ConnectOptions) (*Client, error) {
	return client.ConnectTCPWithOptions(ctx, addr, opts)
}

// LoadPrivateKey loads an explicit ADB RSA private key from path.
func LoadPrivateKey(path string) (*Credential, error) {
	return auth.LoadPrivateKey(path)
}

// ConnectUSB connects to an ADB device over USB and performs the initial ADB
// CNXN handshake before returning. The initial USB backend is Linux-only.
func ConnectUSB(ctx context.Context, opts USBOptions) (*Client, error) {
	return client.ConnectUSB(ctx, opts)
}

// ListUSBDevices returns locally visible USB interfaces that match ADB's USB
// interface class/subclass/protocol. The initial USB backend is Linux-only.
func ListUSBDevices(ctx context.Context) ([]USBDevice, error) {
	return client.ListUSBDevices(ctx)
}

// ScanTCPTargets scans a small TCP port range for ADB endpoints. By default it
// scans localhost odd emulator ADB ports 5555..5585.
func ScanTCPTargets(ctx context.Context, opts TCPScanOptions) ([]TCPTarget, error) {
	return client.ScanTCPTargets(ctx, opts)
}
