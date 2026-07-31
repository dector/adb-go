package clidaemon

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dector/adb-go/internal/daemon"
)

// Sender sends one daemon protocol request to socketPath.
type Sender func(ctx context.Context, socketPath string, req daemon.Request) (daemon.Response, error)

// Send encodes params, builds a daemon request, and sends it with sender.
func Send(ctx context.Context, socketPath, command string, params any, sender Sender) (daemon.Response, error) {
	var raw json.RawMessage
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return daemon.Response{}, fmt.Errorf("encode daemon params: %w", err)
		}
		raw = data
	}
	return sender(ctx, socketPath, daemon.Request{Version: daemon.ProtocolVersion, Command: command, Params: raw})
}

// DecodeResult decodes a daemon response result map into out.
func DecodeResult(result map[string]any, out any) error {
	data, err := json.Marshal(result)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

// DecodeValue decodes a single daemon response value into out.
func DecodeValue(value any, out any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}
