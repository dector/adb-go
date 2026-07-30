package daemon

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

const (
	CommandReverseCreate    = "reverse_create"
	CommandReverseList      = "reverse_list"
	CommandReverseRemove    = "reverse_remove"
	CommandReverseRemoveAll = "reverse_remove_all"

	ErrorReverseNotFound    = "reverse_not_found"
	ErrorRegistrationFailed = "registration_failed"
)

const (
	ReverseStateListening = "listening"
	ReverseStateDegraded  = "degraded"
	ReverseStateStopped   = "stopped"
)

// ReverseCreateParams is the request payload for CommandReverseCreate. The
// first implementation supports TCP ADB targets and tcp:PORT reverse endpoints.
type ReverseCreateParams struct {
	Remote   ReverseRemoteEndpoint `json:"remote"`
	Local    ReverseLocalEndpoint  `json:"local"`
	Target   ForwardTarget         `json:"target"`
	Norebind bool                  `json:"norebind,omitempty"`
}

// ReverseRemoveParams is the request payload for CommandReverseRemove. Exactly
// one selector must be set: ID or Remote.
type ReverseRemoveParams struct {
	ID     string                 `json:"id,omitempty"`
	Remote *ReverseRemoteEndpoint `json:"remote,omitempty"`
}

type ReverseRemoteEndpoint struct {
	Service string `json:"service"`
}

type ReverseLocalEndpoint struct {
	Service string `json:"service"`
}

type Reverse struct {
	ID                string                `json:"id,omitempty"`
	State             string                `json:"state"`
	Remote            ReverseRemoteEndpoint `json:"remote"`
	Local             ReverseLocalEndpoint  `json:"local"`
	Target            ForwardTarget         `json:"target"`
	Norebind          bool                  `json:"norebind,omitempty"`
	CreatedAt         string                `json:"createdAt,omitempty"`
	ActiveConnections int                   `json:"activeConnections"`
	LastError         string                `json:"lastError,omitempty"`
}

type ReverseCreateResult struct {
	Reverse Reverse `json:"reverse"`
}

type ReverseListResult struct {
	Reverses []Reverse `json:"reverses"`
}

type ReverseRemoveResult struct {
	Removed int `json:"removed"`
}

type ReverseRemoveAllResult struct {
	Removed int `json:"removed"`
}

type ReverseDiagnostics struct {
	Total             int `json:"total"`
	Listening         int `json:"listening"`
	Degraded          int `json:"degraded"`
	ActiveConnections int `json:"activeConnections"`
}

func decodeReverseCreateParams(raw json.RawMessage) (ReverseCreateParams, *Error) {
	if len(raw) == 0 {
		return ReverseCreateParams{}, &Error{Code: ErrorBadRequest, Message: "missing reverse_create params"}
	}
	var params ReverseCreateParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return ReverseCreateParams{}, &Error{Code: ErrorBadRequest, Message: "malformed reverse_create params"}
	}
	if e := validateReverseCreateParams(params); e != nil {
		return ReverseCreateParams{}, e
	}
	return params, nil
}

func decodeReverseRemoveParams(raw json.RawMessage) (ReverseRemoveParams, *Error) {
	if len(raw) == 0 {
		return ReverseRemoveParams{}, &Error{Code: ErrorBadRequest, Message: "missing reverse_remove params"}
	}
	var params ReverseRemoveParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return ReverseRemoveParams{}, &Error{Code: ErrorBadRequest, Message: "malformed reverse_remove params"}
	}
	idSet := strings.TrimSpace(params.ID) != ""
	remoteSet := params.Remote != nil
	if idSet == remoteSet {
		return ReverseRemoveParams{}, &Error{Code: ErrorBadRequest, Message: "reverse_remove requires exactly one of id or remote"}
	}
	if remoteSet {
		if e := validateReverseRemote(*params.Remote); e != nil {
			return ReverseRemoveParams{}, e
		}
	}
	return params, nil
}

func validateReverseCreateParams(params ReverseCreateParams) *Error {
	if e := validateReverseRemote(params.Remote); e != nil {
		return e
	}
	if e := validateReverseLocal(params.Local); e != nil {
		return e
	}
	if e := validateForwardTarget(params.Target); e != nil {
		return e
	}
	return nil
}

func validateReverseRemote(remote ReverseRemoteEndpoint) *Error {
	if err := validateTCPService("remote", remote.Service); err != nil {
		return err
	}
	return nil
}

func validateReverseLocal(local ReverseLocalEndpoint) *Error {
	if err := validateTCPService("local", local.Service); err != nil {
		return err
	}
	return nil
}

func validateTCPService(kind, service string) *Error {
	portText, ok := strings.CutPrefix(strings.TrimSpace(service), "tcp:")
	if !ok {
		return &Error{Code: ErrorUnsupportedEndpoint, Message: fmt.Sprintf("unsupported reverse %s endpoint %q", kind, service)}
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return &Error{Code: ErrorUnsupportedEndpoint, Message: fmt.Sprintf("invalid reverse %s tcp endpoint %q", kind, service)}
	}
	return nil
}

func (e ReverseRemoteEndpoint) key() string { return e.Service }
