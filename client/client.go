package client

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"

	"github.com/dector/adb-go/protocol"
)

const defaultTCPPort = "5555"

// Client is a high-level ADB client connected to one device transport.
type Client struct {
	conn *protocol.Connection
}

// Connect connects to addr over TCP. If addr omits a port, the default ADB TCP
// port 5555 is used.
func Connect(ctx context.Context, addr string) (*Client, error) {
	return ConnectTCP(ctx, addr)
}

// ConnectTCP connects to addr over TCP and performs the initial ADB CNXN
// handshake before returning. Authentication is not implemented in v0; an AUTH
// response is reported as ErrAuthRequired.
func ConnectTCP(ctx context.Context, addr string) (*Client, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	networkAddr, err := normalizeTCPAddr(addr)
	if err != nil {
		return nil, err
	}

	dialer := &net.Dialer{}
	rawConn, err := dialer.DialContext(ctx, "tcp", networkAddr)
	if err != nil {
		return nil, fmt.Errorf("adb connect tcp %q: %w", networkAddr, err)
	}

	protoConn := protocol.NewConnection(rawConn)
	if _, err := protoConn.Handshake(ctx); err != nil {
		_ = protoConn.Close()
		if errors.Is(err, protocol.ErrAuthRequired) {
			return nil, fmt.Errorf("adb connect tcp %q: %w", networkAddr, ErrAuthRequired)
		}
		return nil, fmt.Errorf("adb connect tcp %q: %w", networkAddr, err)
	}

	return &Client{conn: protoConn}, nil
}

// Close closes the underlying ADB connection.
func (c *Client) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

// OpenService opens a raw ADB service stream, such as "shell:echo ok" or
// "sync:". The returned stream implements io.ReadWriteCloser.
func (c *Client) OpenService(ctx context.Context, service string) (io.ReadWriteCloser, error) {
	if c == nil || c.conn == nil {
		return nil, protocol.ErrDeviceClosed
	}
	stream, err := c.conn.OpenContext(ctx, service)
	if err != nil {
		return nil, err
	}
	return stream, nil
}

// Shell executes cmd through the ADB "shell:<cmd>" service and returns the
// complete stdout/stderr byte stream produced by the device shell.
func (c *Client) Shell(ctx context.Context, cmd string) ([]byte, error) {
	var out bytes.Buffer
	if err := c.ShellStream(ctx, cmd, &out); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// ShellStream executes cmd through the ADB "shell:<cmd>" service and streams
// the shell output into stdout as it arrives.
func (c *Client) ShellStream(ctx context.Context, cmd string, stdout io.Writer) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if stdout == nil {
		return fmt.Errorf("adb shell stdout writer is nil")
	}

	stream, err := c.OpenService(ctx, "shell:"+cmd)
	if err != nil {
		return err
	}
	defer stream.Close()

	stopCancelCloser := closeOnCancel(ctx, stream)
	defer stopCancelCloser()

	_, err = io.Copy(stdout, stream)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return fmt.Errorf("adb shell %q canceled: %w", cmd, ctxErr)
	}
	if err != nil {
		return fmt.Errorf("adb shell %q: %w", cmd, err)
	}
	return nil
}

func closeOnCancel(ctx context.Context, closer io.Closer) func() {
	if ctx.Done() == nil || closer == nil {
		return func() {}
	}

	stop := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = closer.Close()
		case <-stop:
		}
	}()
	return func() { close(stop) }
}

func normalizeTCPAddr(addr string) (string, error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return "", fmt.Errorf("adb tcp address is empty")
	}

	host, port, err := net.SplitHostPort(addr)
	if err == nil {
		if port == "" {
			port = defaultTCPPort
		}
		return net.JoinHostPort(host, port), nil
	}

	// A bare IPv6 literal contains multiple colons but no bracketed port.
	if strings.Count(addr, ":") > 1 && !strings.HasPrefix(addr, "[") {
		return net.JoinHostPort(addr, defaultTCPPort), nil
	}

	// If there is exactly one colon, assume the caller supplied host:port and let
	// DialContext report invalid or unknown ports with its usual error messages.
	if strings.Count(addr, ":") == 1 {
		parts := strings.SplitN(addr, ":", 2)
		if parts[1] != "" {
			return addr, nil
		}
		return net.JoinHostPort(parts[0], defaultTCPPort), nil
	}

	return net.JoinHostPort(addr, defaultTCPPort), nil
}
