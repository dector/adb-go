package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/dector/adb-go/client"
)

type forwardRegistry struct {
	mu      sync.Mutex
	nextID  uint64
	byID    map[string]*forwardEntry
	byLocal map[string]string
}

type forwardEntry struct {
	forward  Forward
	listener net.Listener
	active   map[*forwardConn]struct{}
	done     chan struct{}
}

type forwardConn struct {
	mu     sync.Mutex
	host   net.Conn
	client *client.Client
	stream io.ReadWriteCloser
}

const forwardSetupTimeout = 10 * time.Second

func newForwardRegistry() *forwardRegistry {
	return &forwardRegistry{
		byID:    make(map[string]*forwardEntry),
		byLocal: make(map[string]string),
	}
}

func (r *forwardRegistry) create(params ForwardCreateParams) (Forward, *Error) {
	if e := validateForwardCreateParams(params); e != nil {
		return Forward{}, e
	}

	r.mu.Lock()
	if existingID, ok := r.byLocal[params.Local.key()]; ok {
		if params.Norebind {
			r.mu.Unlock()
			return Forward{}, &Error{Code: ErrorRebindDisallowed, Message: fmt.Sprintf("forward for local endpoint %s already exists", params.Local.display())}
		}
		existing := r.byID[existingID]
		r.removeLocked(existing)
	}
	r.mu.Unlock()

	ln, err := net.Listen(params.Local.Network, params.Local.Address)
	if err != nil {
		return Forward{}, listenError(params.Local, err)
	}

	actualLocal := params.Local
	actualLocal.Address = ln.Addr().String()
	entry := &forwardEntry{
		forward: Forward{
			ID:                r.nextForwardID(),
			State:             ForwardStateListening,
			Local:             actualLocal,
			Remote:            params.Remote,
			Target:            params.Target,
			Norebind:          params.Norebind,
			CreatedAt:         time.Now().UTC().Format(time.RFC3339Nano),
			ActiveConnections: 0,
		},
		listener: ln,
		active:   make(map[*forwardConn]struct{}),
		done:     make(chan struct{}),
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if existingID, ok := r.byLocal[actualLocal.key()]; ok {
		_ = ln.Close()
		if params.Norebind {
			return Forward{}, &Error{Code: ErrorRebindDisallowed, Message: fmt.Sprintf("forward for local endpoint %s already exists", actualLocal.display())}
		}
		existing := r.byID[existingID]
		r.removeLocked(existing)
	}
	r.byID[entry.forward.ID] = entry
	r.byLocal[entry.forward.Local.key()] = entry.forward.ID
	go r.acceptAndBridge(entry)
	return entry.forward, nil
}

func (r *forwardRegistry) list() []Forward {
	r.mu.Lock()
	defer r.mu.Unlock()
	forwards := make([]Forward, 0, len(r.byID))
	for _, entry := range r.byID {
		forwards = append(forwards, entry.forward)
	}
	return forwards
}

func (r *forwardRegistry) diagnostics() ForwardDiagnostics {
	r.mu.Lock()
	defer r.mu.Unlock()
	diagnostics := ForwardDiagnostics{Total: len(r.byID)}
	for _, entry := range r.byID {
		switch entry.forward.State {
		case ForwardStateListening:
			diagnostics.Listening++
		case ForwardStateDegraded:
			diagnostics.Degraded++
		}
		diagnostics.ActiveConnections += entry.forward.ActiveConnections
	}
	return diagnostics
}

func (r *forwardRegistry) remove(params ForwardRemoveParams) (int, *Error) {
	if params.ID == "" && params.Local == nil || params.ID != "" && params.Local != nil {
		return 0, &Error{Code: ErrorBadRequest, Message: "forward_remove requires exactly one of id or local"}
	}
	if params.Local != nil {
		if e := validateForwardLocal(*params.Local); e != nil {
			return 0, e
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	var id string
	if params.ID != "" {
		id = params.ID
	} else {
		id = r.byLocal[params.Local.key()]
	}
	entry := r.byID[id]
	if entry == nil {
		return 0, &Error{Code: ErrorForwardNotFound, Message: "forward not found"}
	}
	r.removeLocked(entry)
	return 1, nil
}

func (r *forwardRegistry) removeAll() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	removed := 0
	for _, entry := range r.byID {
		r.removeLocked(entry)
		removed++
	}
	return removed
}

func (r *forwardRegistry) close() {
	_ = r.removeAll()
}

func (r *forwardRegistry) removeLocked(entry *forwardEntry) {
	delete(r.byID, entry.forward.ID)
	delete(r.byLocal, entry.forward.Local.key())
	entry.forward.State = ForwardStateStopped
	_ = entry.listener.Close()
	for active := range entry.active {
		active.close()
	}
}

func (r *forwardRegistry) nextForwardID() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextID++
	return "fwd_" + strconv.FormatUint(r.nextID, 10)
}

func (r *forwardRegistry) acceptAndBridge(entry *forwardEntry) {
	for {
		conn, err := entry.listener.Accept()
		if err != nil {
			close(entry.done)
			return
		}
		active := &forwardConn{host: conn}
		r.addActive(entry, active)
		go r.bridge(entry, active)
	}
}

func (r *forwardRegistry) bridge(entry *forwardEntry, active *forwardConn) {
	defer r.removeActive(entry, active)
	defer active.close()

	ctx, cancel := context.WithTimeout(context.Background(), forwardSetupTimeout)
	defer cancel()

	adbClient, err := client.ConnectTCP(ctx, entry.forward.Target.Address)
	if err != nil {
		r.recordForwardError(entry, fmt.Sprintf("connect target %s: %v", entry.forward.Target.Address, err))
		return
	}
	active.setClient(adbClient)

	stream, err := adbClient.OpenService(ctx, entry.forward.Remote.Service)
	if err != nil {
		r.recordForwardError(entry, fmt.Sprintf("open remote service %s: %v", entry.forward.Remote.Service, err))
		return
	}
	active.setStream(stream)
	r.recordForwardSuccess(entry)

	copyDone := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(stream, active.host)
		copyDone <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(active.host, stream)
		copyDone <- struct{}{}
	}()
	<-copyDone
	active.close()
	<-copyDone
}

func (r *forwardRegistry) addActive(entry *forwardEntry, active *forwardConn) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry.active[active] = struct{}{}
	entry.forward.ActiveConnections = len(entry.active)
}

func (r *forwardRegistry) removeActive(entry *forwardEntry, active *forwardConn) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(entry.active, active)
	entry.forward.ActiveConnections = len(entry.active)
}

func (r *forwardRegistry) recordForwardError(entry *forwardEntry, message string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry.forward.State = ForwardStateDegraded
	entry.forward.LastError = message
}

func (r *forwardRegistry) recordForwardSuccess(entry *forwardEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry.forward.State = ForwardStateListening
	entry.forward.LastError = ""
}

func (c *forwardConn) setClient(adbClient *client.Client) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.client = adbClient
}

func (c *forwardConn) setStream(stream io.ReadWriteCloser) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stream = stream
}

func (c *forwardConn) close() {
	c.mu.Lock()
	stream := c.stream
	adbClient := c.client
	host := c.host
	c.mu.Unlock()
	if stream != nil {
		_ = stream.Close()
	}
	if adbClient != nil {
		_ = adbClient.Close()
	}
	if host != nil {
		_ = host.Close()
	}
}

func listenError(local ForwardLocalEndpoint, err error) *Error {
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return &Error{Code: ErrorAddressInUse, Message: fmt.Sprintf("listen on local endpoint %s: %v", local.display(), err)}
	}
	return &Error{Code: ErrorUnsupportedEndpoint, Message: fmt.Sprintf("listen on local endpoint %s: %v", local.display(), err)}
}

func (e ForwardLocalEndpoint) key() string {
	return e.Network + ":" + e.Address
}

func (e ForwardLocalEndpoint) display() string {
	return e.Network + ":" + e.Address
}
