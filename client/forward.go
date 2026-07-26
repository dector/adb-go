package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"

	"github.com/dector/adb-go/protocol"
)

// ForwardTarget describes the device-side endpoint opened for each accepted
// host connection.
type ForwardTarget struct {
	service string
}

// ForwardTCP returns a device TCP forwarding target for port. Each accepted
// local connection opens a fresh ADB service stream named "tcp:<port>".
func ForwardTCP(port int) (ForwardTarget, error) {
	if port < 1 || port > 65535 {
		return ForwardTarget{}, fmt.Errorf("adb forward tcp port %d out of range", port)
	}
	return ForwardTarget{service: fmt.Sprintf("tcp:%d", port)}, nil
}

// Forward is an active process-scoped local forwarding session.
type Forward struct {
	ln     net.Listener
	cancel context.CancelFunc

	mu     sync.Mutex
	closed bool
	active map[io.Closer]struct{}

	bridgeWG sync.WaitGroup
	done     chan error
}

// LocalAddr returns the bound local listener address. It is useful when the
// caller requested an automatically selected port with an address such as
// "127.0.0.1:0".
func (f *Forward) LocalAddr() net.Addr {
	if f == nil || f.ln == nil {
		return nil
	}
	return f.ln.Addr()
}

// Close stops accepting new local connections and closes currently active
// local connections and ADB streams. Wait reports nil for this intentional
// shutdown path.
func (f *Forward) Close() error {
	if f == nil {
		return nil
	}
	if f.cancel != nil {
		f.cancel()
	}

	f.mu.Lock()
	if f.closed {
		f.mu.Unlock()
		return nil
	}
	f.closed = true
	active := make([]io.Closer, 0, len(f.active))
	for closer := range f.active {
		active = append(active, closer)
	}
	f.mu.Unlock()

	var err error
	if f.ln != nil {
		err = f.ln.Close()
	}
	for _, closer := range active {
		_ = closer.Close()
	}
	return err
}

// Wait blocks until the forwarding listener and all active bridges have
// stopped. It returns setup/accept-loop errors, but returns nil after Close or
// context cancellation stops the forward.
func (f *Forward) Wait() error {
	if f == nil {
		return nil
	}
	return <-f.done
}

// ForwardLocalTCP starts process-scoped forwarding from a local TCP listener to
// a device-side forwarding target. localAddr is a Go TCP listen address such as
// "127.0.0.1:9000" or "127.0.0.1:0". The forwarding exists only while the
// returned Forward remains open and this process keeps running; it is not an
// adb-server-compatible persistent "adb forward" registration.
func (c *Client) ForwardLocalTCP(ctx context.Context, localAddr string, remote ForwardTarget) (*Forward, error) {
	if c == nil || c.conn == nil {
		return nil, protocol.ErrDeviceClosed
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if remote.service == "" {
		return nil, fmt.Errorf("adb forward remote target is empty")
	}

	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", localAddr)
	if err != nil {
		return nil, fmt.Errorf("adb forward listen %q: %w", localAddr, err)
	}
	forwardCtx, cancel := context.WithCancel(ctx)
	f := &Forward{
		ln:     ln,
		cancel: cancel,
		active: make(map[io.Closer]struct{}),
		done:   make(chan error, 1),
	}

	go f.acceptLoop(forwardCtx, c, remote.service)
	go func() {
		<-forwardCtx.Done()
		_ = f.Close()
	}()
	return f, nil
}

func (f *Forward) acceptLoop(ctx context.Context, c *Client, remoteService string) {
	var err error
	for {
		conn, acceptErr := f.ln.Accept()
		if acceptErr != nil {
			if ctx.Err() != nil || f.isClosed() || errors.Is(acceptErr, net.ErrClosed) {
				err = nil
			} else {
				err = fmt.Errorf("adb forward accept: %w", acceptErr)
			}
			break
		}
		f.bridgeWG.Add(1)
		go f.handleConn(ctx, c, remoteService, conn)
	}
	f.bridgeWG.Wait()
	f.done <- err
	close(f.done)
}

func (f *Forward) handleConn(ctx context.Context, c *Client, remoteService string, conn net.Conn) {
	defer f.bridgeWG.Done()

	stream, err := c.OpenService(ctx, remoteService)
	if err != nil {
		_ = conn.Close()
		return
	}

	f.addActive(conn)
	f.addActive(stream)
	defer f.removeActive(conn)
	defer f.removeActive(stream)

	var once sync.Once
	closeBoth := func() {
		_ = conn.Close()
		_ = stream.Close()
	}

	var copies sync.WaitGroup
	copies.Add(2)
	go func() {
		defer copies.Done()
		_, _ = io.Copy(stream, conn)
		once.Do(closeBoth)
	}()
	go func() {
		defer copies.Done()
		_, _ = io.Copy(conn, stream)
		once.Do(closeBoth)
	}()
	copies.Wait()
}

func (f *Forward) addActive(closer io.Closer) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		_ = closer.Close()
		return
	}
	f.active[closer] = struct{}{}
}

func (f *Forward) removeActive(closer io.Closer) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.active, closer)
}

func (f *Forward) isClosed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closed
}
