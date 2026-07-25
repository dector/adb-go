package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"

	"github.com/dector/adb-go/protocol"
)

type transportDialer interface {
	DialTransport(ctx context.Context) (io.ReadWriteCloser, error)
	ConnectDescription() string
}

type tcpTransportDialer struct {
	addr   string
	dialer *net.Dialer
}

func newTCPTransportDialer(addr string) (*tcpTransportDialer, error) {
	networkAddr, err := normalizeTCPAddr(addr)
	if err != nil {
		return nil, err
	}
	return &tcpTransportDialer{addr: networkAddr, dialer: &net.Dialer{}}, nil
}

func (d *tcpTransportDialer) DialTransport(ctx context.Context) (io.ReadWriteCloser, error) {
	return d.dialer.DialContext(ctx, "tcp", d.addr)
}

func (d *tcpTransportDialer) ConnectDescription() string {
	return fmt.Sprintf("tcp %q", d.addr)
}

// ConnectOptions controls optional high-level connection behavior.
type ConnectOptions struct {
	// AuthCredentials contains explicit ADB host credentials used when a device
	// requires authentication. Leave empty to preserve the default behavior of
	// returning ErrAuthRequired for AUTH challenges.
	AuthCredentials []protocol.AuthCredential
}

func connectWithTransport(ctx context.Context, dialer transportDialer) (*Client, error) {
	return connectWithTransportOptions(ctx, dialer, ConnectOptions{})
}

func connectWithTransportOptions(ctx context.Context, dialer transportDialer, opts ConnectOptions) (*Client, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if dialer == nil {
		return nil, fmt.Errorf("adb connect: transport dialer is nil")
	}

	desc := dialer.ConnectDescription()
	rawConn, err := dialer.DialTransport(ctx)
	if err != nil {
		return nil, fmt.Errorf("adb connect %s: %w", desc, err)
	}

	protoConn := protocol.NewConnection(rawConn)
	if _, err := protoConn.HandshakeWithOptions(ctx, protocol.HandshakeOptions{AuthCredentials: opts.AuthCredentials}); err != nil {
		_ = protoConn.Close()
		if errors.Is(err, protocol.ErrAuthRequired) {
			return nil, fmt.Errorf("adb connect %s: %w", desc, ErrAuthRequired)
		}
		return nil, fmt.Errorf("adb connect %s: %w", desc, err)
	}

	return &Client{conn: protoConn}, nil
}
