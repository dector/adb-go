package server

import (
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
)

const (
	CommandForwardCreate    = "forward_create"
	CommandForwardList      = "forward_list"
	CommandForwardRemove    = "forward_remove"
	CommandForwardRemoveAll = "forward_remove_all"

	ErrorAddressInUse        = "address_in_use"
	ErrorUnsupportedEndpoint = "unsupported_endpoint"
	ErrorBadTarget           = "bad_target"
	ErrorRebindDisallowed    = "rebind_disallowed"
	ErrorForwardNotFound     = "forward_not_found"
)

const (
	ForwardStateListening = "listening"
	ForwardStateDegraded  = "degraded"
	ForwardStateStopped   = "stopped"
)

// ForwardCreateParams is the request payload for CommandForwardCreate.
// M57 validates this model only; listener ownership is added by later milestones.
type ForwardCreateParams struct {
	Local    ForwardLocalEndpoint  `json:"local"`
	Remote   ForwardRemoteEndpoint `json:"remote"`
	Target   ForwardTarget         `json:"target"`
	Norebind bool                  `json:"norebind,omitempty"`
}

// ForwardRemoveParams is the request payload for CommandForwardRemove. Exactly
// one selector must be set: ID or Local.
type ForwardRemoveParams struct {
	ID    string                `json:"id,omitempty"`
	Local *ForwardLocalEndpoint `json:"local,omitempty"`
}

type ForwardLocalEndpoint struct {
	Network string `json:"network"`
	Address string `json:"address"`
}

type ForwardRemoteEndpoint struct {
	Service string `json:"service"`
}

type ForwardTarget struct {
	Transport string `json:"transport"`
	Address   string `json:"address"`
}

type Forward struct {
	ID                string                `json:"id,omitempty"`
	State             string                `json:"state"`
	Local             ForwardLocalEndpoint  `json:"local"`
	Remote            ForwardRemoteEndpoint `json:"remote"`
	Target            ForwardTarget         `json:"target"`
	Norebind          bool                  `json:"norebind,omitempty"`
	CreatedAt         string                `json:"createdAt,omitempty"`
	ActiveConnections int                   `json:"activeConnections"`
	LastError         string                `json:"lastError,omitempty"`
}

type ForwardCreateResult struct {
	Forward Forward `json:"forward"`
}

type ForwardListResult struct {
	Forwards []Forward `json:"forwards"`
}

type ForwardRemoveResult struct {
	Removed int `json:"removed"`
}

type ForwardRemoveAllResult struct {
	Removed int `json:"removed"`
}

// ForwardDiagnostics is the concise server-wide forwarding summary exposed by
// server status and doctor. Detailed mappings intentionally remain behind the
// forward_list command so status stays safe and compact for support requests.
type ForwardDiagnostics struct {
	Total             int `json:"total"`
	Listening         int `json:"listening"`
	Degraded          int `json:"degraded"`
	ActiveConnections int `json:"activeConnections"`
}

func decodeForwardCreateParams(raw json.RawMessage) (ForwardCreateParams, *Error) {
	if len(raw) == 0 {
		return ForwardCreateParams{}, &Error{Code: ErrorBadRequest, Message: "missing forward_create params"}
	}
	var params ForwardCreateParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return ForwardCreateParams{}, &Error{Code: ErrorBadRequest, Message: "malformed forward_create params"}
	}
	if e := validateForwardCreateParams(params); e != nil {
		return ForwardCreateParams{}, e
	}
	return params, nil
}

func decodeForwardRemoveParams(raw json.RawMessage) (ForwardRemoveParams, *Error) {
	if len(raw) == 0 {
		return ForwardRemoveParams{}, &Error{Code: ErrorBadRequest, Message: "missing forward_remove params"}
	}
	var params ForwardRemoveParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return ForwardRemoveParams{}, &Error{Code: ErrorBadRequest, Message: "malformed forward_remove params"}
	}
	idSet := strings.TrimSpace(params.ID) != ""
	localSet := params.Local != nil
	if idSet == localSet {
		return ForwardRemoveParams{}, &Error{Code: ErrorBadRequest, Message: "forward_remove requires exactly one of id or local"}
	}
	if localSet {
		if e := validateForwardLocal(*params.Local); e != nil {
			return ForwardRemoveParams{}, e
		}
	}
	return params, nil
}

func validateForwardCreateParams(params ForwardCreateParams) *Error {
	if e := validateForwardLocal(params.Local); e != nil {
		return e
	}
	if e := validateForwardRemote(params.Remote); e != nil {
		return e
	}
	if e := validateForwardTarget(params.Target); e != nil {
		return e
	}
	return nil
}

func validateForwardLocal(local ForwardLocalEndpoint) *Error {
	if local.Network != "tcp" {
		return &Error{Code: ErrorUnsupportedEndpoint, Message: fmt.Sprintf("unsupported local endpoint network %q", local.Network)}
	}
	host, port, err := splitHostPort(local.Address)
	if err != nil {
		return &Error{Code: ErrorUnsupportedEndpoint, Message: fmt.Sprintf("invalid local tcp address %q", local.Address)}
	}
	if port < 0 || port > 65535 {
		return &Error{Code: ErrorUnsupportedEndpoint, Message: fmt.Sprintf("invalid local tcp port %d", port)}
	}
	if host == "" {
		return &Error{Code: ErrorUnsupportedEndpoint, Message: "local tcp address must include a host"}
	}
	if !isLoopbackHost(host) {
		return &Error{Code: ErrorUnsupportedEndpoint, Message: fmt.Sprintf("local tcp address %q is not loopback", local.Address)}
	}
	return nil
}

func validateForwardRemote(remote ForwardRemoteEndpoint) *Error {
	portText, ok := strings.CutPrefix(remote.Service, "tcp:")
	if !ok {
		return &Error{Code: ErrorUnsupportedEndpoint, Message: fmt.Sprintf("unsupported remote service %q", remote.Service)}
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return &Error{Code: ErrorUnsupportedEndpoint, Message: fmt.Sprintf("invalid remote tcp service %q", remote.Service)}
	}
	return nil
}

func validateForwardTarget(target ForwardTarget) *Error {
	if target.Transport != "tcp" {
		return &Error{Code: ErrorBadTarget, Message: fmt.Sprintf("unsupported target transport %q", target.Transport)}
	}
	host, port, err := splitHostPort(target.Address)
	if err != nil || host == "" || port < 1 || port > 65535 {
		return &Error{Code: ErrorBadTarget, Message: fmt.Sprintf("invalid tcp target address %q", target.Address)}
	}
	return nil
}

func splitHostPort(address string) (string, int, error) {
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		return "", 0, err
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		return "", 0, err
	}
	return host, port, nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
