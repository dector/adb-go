package daemon

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/dector/adb-go/client"
)

type reverseRegistry struct {
	mu       sync.Mutex
	nextID   uint64
	byID     map[string]*reverseEntry
	byRemote map[string]string
}

type reverseEntry struct {
	reverse Reverse
	client  *client.Client
	session *client.Reverse
	cancel  context.CancelFunc
}

func newReverseRegistry() *reverseRegistry {
	return &reverseRegistry{byID: make(map[string]*reverseEntry), byRemote: make(map[string]string)}
}

func (r *reverseRegistry) create(params ReverseCreateParams) (Reverse, *Error) {
	if e := validateReverseCreateParams(params); e != nil {
		return Reverse{}, e
	}

	r.mu.Lock()
	if existingID, ok := r.byRemote[params.Remote.key()]; ok {
		if params.Norebind {
			r.mu.Unlock()
			return Reverse{}, &Error{Code: ErrorRebindDisallowed, Message: fmt.Sprintf("reverse for remote endpoint %s already exists", params.Remote.Service)}
		}
		existing := r.byID[existingID]
		r.removeLocked(existing)
	}
	r.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	setupCtx, setupCancel := context.WithTimeout(ctx, forwardSetupTimeout)
	adbClient, err := client.ConnectTCP(setupCtx, params.Target.Address)
	setupCancel()
	if err != nil {
		cancel()
		return Reverse{}, &Error{Code: ErrorRegistrationFailed, Message: fmt.Sprintf("connect target %s: %v", params.Target.Address, err)}
	}

	remote, err := client.ParseReverseDeviceEndpoint(params.Remote.Service)
	if err != nil {
		cancel()
		_ = adbClient.Close()
		return Reverse{}, &Error{Code: ErrorUnsupportedEndpoint, Message: err.Error()}
	}
	local, err := client.ParseReverseHostEndpoint(params.Local.Service)
	if err != nil {
		cancel()
		_ = adbClient.Close()
		return Reverse{}, &Error{Code: ErrorUnsupportedEndpoint, Message: err.Error()}
	}

	entry := &reverseEntry{
		reverse: Reverse{
			ID:        r.nextReverseID(),
			State:     ReverseStateListening,
			Remote:    params.Remote,
			Local:     params.Local,
			Target:    params.Target,
			Norebind:  params.Norebind,
			CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		},
		client: adbClient,
		cancel: cancel,
	}

	session, err := adbClient.ReverseTCPWithOptions(ctx, remote, local, client.ReverseOptions{
		Norebind: params.Norebind,
		OnActiveConnections: func(n int) {
			r.recordReverseActive(entry, n)
		},
		OnBridgeError: func(err error) {
			r.recordReverseError(entry, fmt.Sprintf("host dial failed: %v", err))
		},
	})
	if err != nil {
		cancel()
		_ = adbClient.Close()
		return Reverse{}, &Error{Code: ErrorRegistrationFailed, Message: err.Error()}
	}
	entry.session = session

	r.mu.Lock()
	defer r.mu.Unlock()
	if existingID, ok := r.byRemote[params.Remote.key()]; ok {
		if params.Norebind {
			_ = session.Close()
			_ = adbClient.Close()
			cancel()
			return Reverse{}, &Error{Code: ErrorRebindDisallowed, Message: fmt.Sprintf("reverse for remote endpoint %s already exists", params.Remote.Service)}
		}
		existing := r.byID[existingID]
		r.removeLocked(existing)
	}
	r.byID[entry.reverse.ID] = entry
	r.byRemote[entry.reverse.Remote.key()] = entry.reverse.ID
	return entry.reverse, nil
}

func (r *reverseRegistry) list() []Reverse {
	r.mu.Lock()
	defer r.mu.Unlock()
	reverses := make([]Reverse, 0, len(r.byID))
	for _, entry := range r.byID {
		reverses = append(reverses, entry.reverse)
	}
	return reverses
}

func (r *reverseRegistry) diagnostics() ReverseDiagnostics {
	r.mu.Lock()
	defer r.mu.Unlock()
	diagnostics := ReverseDiagnostics{Total: len(r.byID)}
	for _, entry := range r.byID {
		switch entry.reverse.State {
		case ReverseStateListening:
			diagnostics.Listening++
		case ReverseStateDegraded:
			diagnostics.Degraded++
		}
		diagnostics.ActiveConnections += entry.reverse.ActiveConnections
	}
	return diagnostics
}

func (r *reverseRegistry) remove(params ReverseRemoveParams) (int, *Error) {
	if params.ID == "" && params.Remote == nil || params.ID != "" && params.Remote != nil {
		return 0, &Error{Code: ErrorBadRequest, Message: "reverse_remove requires exactly one of id or remote"}
	}
	if params.Remote != nil {
		if e := validateReverseRemote(*params.Remote); e != nil {
			return 0, e
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	var id string
	if params.ID != "" {
		id = params.ID
	} else {
		id = r.byRemote[params.Remote.key()]
	}
	entry := r.byID[id]
	if entry == nil {
		return 0, &Error{Code: ErrorReverseNotFound, Message: "reverse not found"}
	}
	r.removeLocked(entry)
	return 1, nil
}

func (r *reverseRegistry) removeAll() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	removed := 0
	for _, entry := range r.byID {
		r.removeLocked(entry)
		removed++
	}
	return removed
}

func (r *reverseRegistry) close() { _ = r.removeAll() }

func (r *reverseRegistry) removeLocked(entry *reverseEntry) {
	delete(r.byID, entry.reverse.ID)
	delete(r.byRemote, entry.reverse.Remote.key())
	entry.reverse.State = ReverseStateStopped
	if entry.cancel != nil {
		entry.cancel()
	}
	if entry.session != nil {
		if err := entry.session.Close(); err != nil {
			entry.reverse.State = ReverseStateDegraded
			entry.reverse.LastError = fmt.Sprintf("cleanup failed: %v", err)
		}
	}
	if entry.client != nil {
		_ = entry.client.Close()
	}
}

func (r *reverseRegistry) nextReverseID() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextID++
	return "rev_" + strconv.FormatUint(r.nextID, 10)
}

func (r *reverseRegistry) recordReverseActive(entry *reverseEntry, active int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.byID[entry.reverse.ID] == nil {
		return
	}
	entry.reverse.ActiveConnections = active
	if entry.reverse.State == ReverseStateDegraded && entry.reverse.LastError == "" {
		entry.reverse.State = ReverseStateListening
	}
}

func (r *reverseRegistry) recordReverseError(entry *reverseEntry, message string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.byID[entry.reverse.ID] == nil {
		return
	}
	entry.reverse.State = ReverseStateDegraded
	entry.reverse.LastError = message
}
