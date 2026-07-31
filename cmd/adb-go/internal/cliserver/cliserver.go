package cliserver

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dector/adb-go/internal/server"
)

// Sender sends one server protocol request to socketPath.
type Sender func(ctx context.Context, socketPath string, req server.Request) (server.Response, error)

// Send encodes params, builds a server request, and sends it with sender.
func Send(ctx context.Context, socketPath, command string, params any, sender Sender) (server.Response, error) {
	var raw json.RawMessage
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return server.Response{}, fmt.Errorf("encode server params: %w", err)
		}
		raw = data
	}
	return sender(ctx, socketPath, server.Request{Version: server.ProtocolVersion, Command: command, Params: raw})
}

// DecodeResult decodes a server response result map into out.
func DecodeResult(result map[string]any, out any) error {
	data, err := json.Marshal(result)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

// DecodeValue decodes a single server response value into out.
func DecodeValue(value any, out any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}
