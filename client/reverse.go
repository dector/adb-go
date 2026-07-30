package client

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"

	"github.com/dector/adb-go/protocol"
)

// ReverseDeviceEndpoint describes the device-side listener created by reverse
// forwarding.
type ReverseDeviceEndpoint struct {
	service string
	port    int
}

// ReverseHostEndpoint describes the host-side TCP target reached by accepted
// reverse streams.
type ReverseHostEndpoint struct {
	service string
	addr    string
	port    int
}

// ReverseDeviceTCP returns a device-side TCP reverse endpoint for port. Port 0
// auto-selection is intentionally unsupported until adb-go can reliably report
// the selected device port across supported adbd versions.
func ReverseDeviceTCP(port int) (ReverseDeviceEndpoint, error) {
	if port < 1 || port > 65535 {
		return ReverseDeviceEndpoint{}, fmt.Errorf("adb reverse device tcp port %d out of range", port)
	}
	return ReverseDeviceEndpoint{service: fmt.Sprintf("tcp:%d", port), port: port}, nil
}

// ReverseHostTCP returns a host-side TCP reverse target for port on loopback.
func ReverseHostTCP(port int) (ReverseHostEndpoint, error) {
	if port < 1 || port > 65535 {
		return ReverseHostEndpoint{}, fmt.Errorf("adb reverse host tcp port %d out of range", port)
	}
	return ReverseHostEndpoint{
		service: fmt.Sprintf("tcp:%d", port),
		addr:    net.JoinHostPort("127.0.0.1", strconv.Itoa(port)),
		port:    port,
	}, nil
}

// ParseReverseDeviceEndpoint parses the first-slice reverse device endpoint
// syntax. Only tcp:PORT is supported.
func ParseReverseDeviceEndpoint(endpoint string) (ReverseDeviceEndpoint, error) {
	family, value, ok := strings.Cut(strings.TrimSpace(endpoint), ":")
	if !ok || family != "tcp" {
		return ReverseDeviceEndpoint{}, fmt.Errorf("adb reverse unsupported device endpoint %q: only tcp:PORT is supported", endpoint)
	}
	port, err := strconv.Atoi(value)
	if err != nil {
		return ReverseDeviceEndpoint{}, fmt.Errorf("adb reverse invalid device tcp port %q: %w", value, err)
	}
	return ReverseDeviceTCP(port)
}

// ParseReverseHostEndpoint parses the first-slice reverse host endpoint syntax.
// Only tcp:PORT is supported, and the target is dialed on host loopback.
func ParseReverseHostEndpoint(endpoint string) (ReverseHostEndpoint, error) {
	family, value, ok := strings.Cut(strings.TrimSpace(endpoint), ":")
	if !ok || family != "tcp" {
		return ReverseHostEndpoint{}, fmt.Errorf("adb reverse unsupported host endpoint %q: only tcp:PORT is supported", endpoint)
	}
	port, err := strconv.Atoi(value)
	if err != nil {
		return ReverseHostEndpoint{}, fmt.Errorf("adb reverse invalid host tcp port %q: %w", value, err)
	}
	return ReverseHostTCP(port)
}

// Reverse is an active process-scoped reverse forwarding session.
type Reverse struct {
	client *Client
	remote ReverseDeviceEndpoint
	local  ReverseHostEndpoint
	cancel context.CancelFunc

	mu     sync.Mutex
	closed bool
	active map[io.Closer]struct{}

	bridgeWG sync.WaitGroup
	done     chan error
}

// Close removes the device-side reverse registration and closes active reverse
// streams and host TCP connections.
func (r *Reverse) Close() error {
	if r == nil {
		return nil
	}
	if r.cancel != nil {
		r.cancel()
	}

	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	active := make([]io.Closer, 0, len(r.active))
	for closer := range r.active {
		active = append(active, closer)
	}
	r.mu.Unlock()

	if r.client != nil && r.client.conn != nil {
		r.client.conn.RemoveOpenHandler(r.local.service)
	}
	for _, closer := range active {
		_ = closer.Close()
	}

	var cleanupErr error
	if r.client != nil {
		cleanupErr = r.client.removeReverse(context.Background(), r.remote)
	}

	r.bridgeWG.Wait()
	select {
	case r.done <- cleanupErr:
	default:
	}
	return cleanupErr
}

// Wait blocks until the reverse session has stopped. It returns nil after a
// clean Close and returns cleanup errors reported by Close.
func (r *Reverse) Wait() error {
	if r == nil {
		return nil
	}
	return <-r.done
}

// ReverseTCP starts process-scoped reverse TCP forwarding from a device TCP
// listener to a host loopback TCP target. The forwarding exists only while the
// returned Reverse remains open and this process keeps running.
func (c *Client) ReverseTCP(ctx context.Context, remote ReverseDeviceEndpoint, local ReverseHostEndpoint) (*Reverse, error) {
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
		return nil, fmt.Errorf("adb reverse remote endpoint is empty")
	}
	if local.service == "" || local.addr == "" {
		return nil, fmt.Errorf("adb reverse host endpoint is empty")
	}

	reverseCtx, cancel := context.WithCancel(ctx)
	r := &Reverse{
		client: c,
		remote: remote,
		local:  local,
		cancel: cancel,
		active: make(map[io.Closer]struct{}),
		done:   make(chan error, 1),
	}

	if err := c.conn.HandleOpen(local.service, func(service string, stream *protocol.Stream) {
		if !r.startBridge(stream) {
			return
		}
		go r.handleStream(reverseCtx, stream)
	}); err != nil {
		cancel()
		return nil, fmt.Errorf("adb reverse handle %s: %w", local.service, err)
	}
	registered := false
	defer func() {
		if !registered {
			c.conn.RemoveOpenHandler(local.service)
			cancel()
		}
	}()

	if err := c.registerReverse(ctx, remote, local); err != nil {
		return nil, err
	}
	registered = true

	go func() {
		<-reverseCtx.Done()
		_ = r.Close()
	}()
	return r, nil
}

func (c *Client) registerReverse(ctx context.Context, remote ReverseDeviceEndpoint, local ReverseHostEndpoint) error {
	service := "reverse:forward:" + remote.service + ";" + local.service
	stream, err := c.OpenService(ctx, service)
	if err != nil {
		return fmt.Errorf("adb reverse register %s -> %s: %w", remote.service, local.service, err)
	}
	defer stream.Close()

	response, err := io.ReadAll(stream)
	if err != nil {
		return fmt.Errorf("adb reverse register %s -> %s: %w", remote.service, local.service, err)
	}
	if len(response) > 0 {
		return fmt.Errorf("adb reverse register %s -> %s failed: %s", remote.service, local.service, strings.TrimSpace(string(response)))
	}
	return nil
}

func (c *Client) removeReverse(ctx context.Context, remote ReverseDeviceEndpoint) error {
	if remote.service == "" {
		return nil
	}
	service := "reverse:killforward:" + remote.service
	stream, err := c.OpenService(ctx, service)
	if err != nil {
		return fmt.Errorf("adb reverse cleanup %s: %w", remote.service, err)
	}
	defer stream.Close()

	response, err := io.ReadAll(stream)
	if err != nil {
		return fmt.Errorf("adb reverse cleanup %s: %w", remote.service, err)
	}
	if len(response) > 0 {
		return fmt.Errorf("adb reverse cleanup %s failed: %s", remote.service, strings.TrimSpace(string(response)))
	}
	return nil
}

func (r *Reverse) startBridge(stream io.Closer) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		_ = stream.Close()
		return false
	}
	r.bridgeWG.Add(1)
	return true
}

func (r *Reverse) handleStream(ctx context.Context, stream io.ReadWriteCloser) {
	defer r.bridgeWG.Done()

	dialer := net.Dialer{}
	conn, err := dialer.DialContext(ctx, "tcp", r.local.addr)
	if err != nil {
		_ = stream.Close()
		return
	}

	r.addActive(stream)
	r.addActive(conn)
	defer r.removeActive(stream)
	defer r.removeActive(conn)

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

func (r *Reverse) addActive(closer io.Closer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		_ = closer.Close()
		return
	}
	r.active[closer] = struct{}{}
}

func (r *Reverse) removeActive(closer io.Closer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.active, closer)
}
