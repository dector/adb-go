package server

import "encoding/json"

const (
	// ProtocolVersion is the version of the initial adb-gos control protocol.
	ProtocolVersion = 1

	CommandPing     = "ping"
	CommandStatus   = "status"
	CommandShutdown = "shutdown"

	ErrorBadRequest         = "bad_request"
	ErrorUnsupportedVersion = "unsupported_version"
	ErrorUnknownCommand     = "unknown_command"
	ErrorInternal           = "internal_error"
)

type Request struct {
	Version int             `json:"version"`
	ID      string          `json:"id,omitempty"`
	Command string          `json:"command"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type Response struct {
	Version int            `json:"version"`
	ID      string         `json:"id,omitempty"`
	OK      bool           `json:"ok"`
	Result  map[string]any `json:"result,omitempty"`
	Error   *Error         `json:"error,omitempty"`
}

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
