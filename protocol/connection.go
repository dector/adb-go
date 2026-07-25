package protocol

import (
	"context"
	"fmt"
	"io"
)

const (
	// Version is the ADB protocol version used by adb-go for the initial CNXN
	// handshake.
	Version uint32 = 0x01000000

	// MaxPayload is the maximum payload size advertised by adb-go during the
	// initial CNXN handshake.
	MaxPayload uint32 = 4096

	// SystemIdentityString is the host identity advertised by adb-go during the
	// initial CNXN handshake. The on-wire CNXN payload appends a trailing NUL.
	SystemIdentityString = "host::adb-go"
)

// Connection is a low-level ADB protocol connection around an io.ReadWriter.
type Connection struct {
	rw     io.ReadWriter
	closer io.Closer
}

// NewConnection returns a low-level ADB protocol connection using rw.
func NewConnection(rw io.ReadWriter) *Connection {
	conn := &Connection{rw: rw}
	if closer, ok := rw.(io.Closer); ok {
		conn.closer = closer
	}
	return conn
}

// Close closes the underlying connection when it implements io.Closer.
func (c *Connection) Close() error {
	if c.closer == nil {
		return nil
	}
	return c.closer.Close()
}

// Handshake performs the initial ADB CNXN exchange and returns the peer's CNXN
// message on success.
//
// If the peer replies with AUTH, Handshake returns an AuthRequiredError that
// matches ErrAuthRequired with errors.Is. RSA authentication is intentionally
// not implemented in v0; future auth support should continue from the returned
// AUTH message's payload.
func (c *Connection) Handshake(ctx context.Context) (Message, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Message{}, err
	}

	stopCancelCloser := c.closeOnCancel(ctx)
	defer stopCancelCloser()

	request := Message{
		Command: CommandCNXN,
		Arg0:    Version,
		Arg1:    MaxPayload,
		Payload: []byte(SystemIdentityString + "\x00"),
	}
	if err := WriteMessage(c.rw, request); err != nil {
		return Message{}, c.contextualError(ctx, "write cnxn", err)
	}

	response, err := ReadMessage(c.rw)
	if err != nil {
		return Message{}, c.contextualError(ctx, "read handshake response", err)
	}

	switch response.Command {
	case CommandCNXN:
		return response, nil
	case CommandAUTH:
		// TODO(auth): Implement RSA token signing/key exchange for authenticated
		// ADB devices.
		return Message{}, &AuthRequiredError{Message: response}
	default:
		return Message{}, fmt.Errorf("adb handshake unexpected response command %#x", uint32(response.Command))
	}
}

func (c *Connection) closeOnCancel(ctx context.Context) func() {
	if ctx.Done() == nil || c.closer == nil {
		return func() {}
	}

	stop := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = c.Close()
		case <-stop:
		}
	}()
	return func() { close(stop) }
}

func (c *Connection) contextualError(ctx context.Context, op string, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return fmt.Errorf("adb handshake %s canceled: %w", op, ctxErr)
	}
	return fmt.Errorf("adb handshake %s: %w", op, err)
}
