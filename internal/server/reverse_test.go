package server

import (
	"encoding/json"
	"testing"
)

func TestReverseRequestJSONParamsRoundTrip(t *testing.T) {
	params := ReverseCreateParams{
		Remote: ReverseRemoteEndpoint{Service: "tcp:8081"},
		Local:  ReverseLocalEndpoint{Service: "tcp:3000"},
		Target: ForwardTarget{Transport: "tcp", Address: "127.0.0.1:5555"},
	}
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("Marshal(params) error = %v", err)
	}
	body, err := json.Marshal(Request{Version: ProtocolVersion, ID: "r1", Command: CommandReverseCreate, Params: raw})
	if err != nil {
		t.Fatalf("Marshal(request) error = %v", err)
	}
	var req Request
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("Unmarshal(request) error = %v", err)
	}
	got, protoErr := decodeReverseCreateParams(req.Params)
	if protoErr != nil {
		t.Fatalf("decodeReverseCreateParams() error = %#v", protoErr)
	}
	if got.Remote != params.Remote || got.Local != params.Local || got.Target != params.Target {
		t.Fatalf("decoded params = %#v, want %#v", got, params)
	}
}

func TestReverseValidationStableErrorCodes(t *testing.T) {
	valid := ReverseCreateParams{
		Remote: ReverseRemoteEndpoint{Service: "tcp:8081"},
		Local:  ReverseLocalEndpoint{Service: "tcp:3000"},
		Target: ForwardTarget{Transport: "tcp", Address: "127.0.0.1:5555"},
	}
	if err := validateReverseCreateParams(valid); err != nil {
		t.Fatalf("validateReverseCreateParams(valid) error = %#v", err)
	}

	tests := []struct {
		name string
		mut  func(*ReverseCreateParams)
		code string
	}{
		{name: "remote family", mut: func(p *ReverseCreateParams) { p.Remote.Service = "localabstract:name" }, code: ErrorUnsupportedEndpoint},
		{name: "remote port", mut: func(p *ReverseCreateParams) { p.Remote.Service = "tcp:0" }, code: ErrorUnsupportedEndpoint},
		{name: "local family", mut: func(p *ReverseCreateParams) { p.Local.Service = "jdwp:123" }, code: ErrorUnsupportedEndpoint},
		{name: "target transport", mut: func(p *ReverseCreateParams) { p.Target.Transport = "usb" }, code: ErrorBadTarget},
		{name: "target address", mut: func(p *ReverseCreateParams) { p.Target.Address = "127.0.0.1" }, code: ErrorBadTarget},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			params := valid
			tc.mut(&params)
			err := validateReverseCreateParams(params)
			if err == nil || err.Code != tc.code {
				t.Fatalf("validateReverseCreateParams() error = %#v, want code %q", err, tc.code)
			}
		})
	}
}

func TestReverseRemoveValidation(t *testing.T) {
	remote := ReverseRemoteEndpoint{Service: "tcp:8081"}
	tests := []struct {
		name string
		in   ReverseRemoveParams
		ok   bool
	}{
		{name: "id", in: ReverseRemoveParams{ID: "rev_1"}, ok: true},
		{name: "remote", in: ReverseRemoveParams{Remote: &remote}, ok: true},
		{name: "none", in: ReverseRemoveParams{}, ok: false},
		{name: "both", in: ReverseRemoveParams{ID: "rev_1", Remote: &remote}, ok: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(tc.in)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}
			_, protoErr := decodeReverseRemoveParams(raw)
			if tc.ok && protoErr != nil {
				t.Fatalf("decodeReverseRemoveParams() error = %#v, want nil", protoErr)
			}
			if !tc.ok && (protoErr == nil || protoErr.Code != ErrorBadRequest) {
				t.Fatalf("decodeReverseRemoveParams() error = %#v, want bad_request", protoErr)
			}
		})
	}
}
