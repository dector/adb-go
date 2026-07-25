// Package adb provides the public adb-go API.
package adb

import (
	"context"

	"github.com/dector/adb-go/client"
)

// Client is a high-level ADB client connected to one device transport.
type Client = client.Client

var (
	// ErrAuthRequired reports that the ADB peer requires authentication that is
	// not implemented by adb-go yet.
	ErrAuthRequired = client.ErrAuthRequired

	// ErrUnsupported reports that the requested operation is not supported yet.
	ErrUnsupported = client.ErrUnsupported
)

// Connect connects to addr over TCP. If addr omits a port, the default ADB TCP
// port 5555 is used.
func Connect(ctx context.Context, addr string) (*Client, error) {
	return client.Connect(ctx, addr)
}

// ConnectTCP connects to addr over TCP and performs the initial ADB CNXN
// handshake before returning.
func ConnectTCP(ctx context.Context, addr string) (*Client, error) {
	return client.ConnectTCP(ctx, addr)
}
