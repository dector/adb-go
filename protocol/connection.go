package protocol

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/sha1"
	"fmt"
	"io"
	"sync"
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

// AuthCredential supplies one ADB host credential to the protocol handshake.
// It is intentionally an interface so the protocol package does not need to
// know where credentials came from.
type AuthCredential interface {
	Signer() crypto.Signer
	PublicKeyPayload() []byte
}

// HandshakeOptions controls optional connection-handshake behavior.
type HandshakeOptions struct {
	// AuthCredentials contains explicit host credentials to try when a peer sends
	// AUTH TOKEN. Credentials are tried in order. When signatures are exhausted,
	// the first credential's public-key payload is offered once so the device can
	// show its authorization prompt.
	AuthCredentials []AuthCredential
}

// Connection is a low-level ADB protocol connection around an io.ReadWriter.
type Connection struct {
	rw     io.ReadWriter
	closer io.Closer

	writeMu sync.Mutex

	mu            sync.Mutex
	streams       map[uint32]*Stream
	openHandlers  map[string]OpenHandler
	nextLocalID   uint32
	readerStarted bool
	readerErr     error
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
	c.closeAllStreams(ErrDeviceClosed)
	if c.closer == nil {
		return nil
	}
	return c.closer.Close()
}

// Handshake performs the initial ADB CNXN exchange and returns the peer's CNXN
// message on success.
//
// If the peer replies with AUTH, Handshake returns an AuthRequiredError that
// matches ErrAuthRequired with errors.Is. Use HandshakeWithOptions to supply
// explicit credentials for authenticated devices.
func (c *Connection) Handshake(ctx context.Context) (Message, error) {
	return c.HandshakeWithOptions(ctx, HandshakeOptions{})
}

// HandshakeWithOptions performs the initial ADB CNXN exchange and, when
// configured, responds to AUTH TOKEN challenges with explicit host credentials.
func (c *Connection) HandshakeWithOptions(ctx context.Context, opts HandshakeOptions) (Message, error) {
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

	return c.readHandshakeResponses(ctx, opts)
}

func (c *Connection) readHandshakeResponses(ctx context.Context, opts HandshakeOptions) (Message, error) {
	credentialIndex := 0
	publicKeyOffered := false

	for {
		response, err := ReadMessage(c.rw)
		if err != nil {
			return Message{}, c.contextualError(ctx, "read handshake response", err)
		}

		switch response.Command {
		case CommandCNXN:
			return response, nil
		case CommandAUTH:
			if response.Arg0 != AuthToken {
				return Message{}, &AuthRequiredError{Message: response}
			}

			reply, ok, err := nextAuthReply(response.Payload, opts.AuthCredentials, &credentialIndex, &publicKeyOffered)
			if err != nil {
				return Message{}, c.contextualError(ctx, "prepare auth reply", err)
			}
			if !ok {
				return Message{}, &AuthRequiredError{Message: response}
			}
			if err := WriteMessage(c.rw, reply); err != nil {
				return Message{}, c.contextualError(ctx, "write auth reply", err)
			}
		default:
			return Message{}, fmt.Errorf("adb handshake unexpected response command %#x", uint32(response.Command))
		}
	}
}

func nextAuthReply(token []byte, credentials []AuthCredential, credentialIndex *int, publicKeyOffered *bool) (Message, bool, error) {
	for *credentialIndex < len(credentials) {
		credential := credentials[*credentialIndex]
		(*credentialIndex)++
		if credential == nil || credential.Signer() == nil {
			continue
		}

		digest := sha1.Sum(token)
		signature, err := credential.Signer().Sign(rand.Reader, digest[:], crypto.SHA1)
		if err != nil {
			return Message{}, false, err
		}
		return Message{Command: CommandAUTH, Arg0: AuthSignature, Payload: signature}, true, nil
	}

	if !*publicKeyOffered {
		for _, credential := range credentials {
			if credential == nil {
				continue
			}
			payload := credential.PublicKeyPayload()
			if len(payload) == 0 {
				continue
			}
			*publicKeyOffered = true
			return Message{Command: CommandAUTH, Arg0: AuthRSAPublicKey, Payload: payload}, true, nil
		}
		*publicKeyOffered = true
	}

	return Message{}, false, nil
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
