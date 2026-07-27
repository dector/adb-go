package daemon

import (
	"encoding/json"
	"testing"
)

func TestForwardRequestJSONParamsRoundTrip(t *testing.T) {
	params := ForwardCreateParams{
		Local:  ForwardLocalEndpoint{Network: "tcp", Address: "127.0.0.1:9000"},
		Remote: ForwardRemoteEndpoint{Service: "tcp:9001"},
		Target: ForwardTarget{Transport: "tcp", Address: "127.0.0.1:5555"},
	}
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("Marshal(params) error = %v", err)
	}
	body, err := json.Marshal(Request{Version: ProtocolVersion, ID: "c1", Command: CommandForwardCreate, Params: raw})
	if err != nil {
		t.Fatalf("Marshal(request) error = %v", err)
	}
	var req Request
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("Unmarshal(request) error = %v", err)
	}
	if req.Command != CommandForwardCreate || len(req.Params) == 0 {
		t.Fatalf("request = %#v, want forward_create with raw params", req)
	}
	got, protoErr := decodeForwardCreateParams(req.Params)
	if protoErr != nil {
		t.Fatalf("decodeForwardCreateParams() error = %#v", protoErr)
	}
	if got.Local != params.Local || got.Remote != params.Remote || got.Target != params.Target {
		t.Fatalf("decoded params = %#v, want %#v", got, params)
	}
}

func TestForwardValidationStableErrorCodes(t *testing.T) {
	valid := ForwardCreateParams{
		Local:  ForwardLocalEndpoint{Network: "tcp", Address: "127.0.0.1:0"},
		Remote: ForwardRemoteEndpoint{Service: "tcp:9000"},
		Target: ForwardTarget{Transport: "tcp", Address: "127.0.0.1:5555"},
	}
	if err := validateForwardCreateParams(valid); err != nil {
		t.Fatalf("validateForwardCreateParams(valid) error = %#v", err)
	}

	tests := []struct {
		name string
		mut  func(*ForwardCreateParams)
		code string
	}{
		{name: "local network", mut: func(p *ForwardCreateParams) { p.Local.Network = "localabstract" }, code: ErrorUnsupportedEndpoint},
		{name: "local non loopback", mut: func(p *ForwardCreateParams) { p.Local.Address = "0.0.0.0:9000" }, code: ErrorUnsupportedEndpoint},
		{name: "remote family", mut: func(p *ForwardCreateParams) { p.Remote.Service = "jdwp:123" }, code: ErrorUnsupportedEndpoint},
		{name: "remote port", mut: func(p *ForwardCreateParams) { p.Remote.Service = "tcp:0" }, code: ErrorUnsupportedEndpoint},
		{name: "target transport", mut: func(p *ForwardCreateParams) { p.Target.Transport = "usb" }, code: ErrorBadTarget},
		{name: "target address", mut: func(p *ForwardCreateParams) { p.Target.Address = "127.0.0.1" }, code: ErrorBadTarget},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			params := valid
			tc.mut(&params)
			err := validateForwardCreateParams(params)
			if err == nil || err.Code != tc.code {
				t.Fatalf("validateForwardCreateParams() error = %#v, want code %q", err, tc.code)
			}
		})
	}
}

func TestForwardRemoveValidation(t *testing.T) {
	local := ForwardLocalEndpoint{Network: "tcp", Address: "127.0.0.1:9000"}
	tests := []struct {
		name string
		in   ForwardRemoveParams
		ok   bool
	}{
		{name: "id", in: ForwardRemoveParams{ID: "fwd_1"}, ok: true},
		{name: "local", in: ForwardRemoveParams{Local: &local}, ok: true},
		{name: "none", in: ForwardRemoveParams{}, ok: false},
		{name: "both", in: ForwardRemoveParams{ID: "fwd_1", Local: &local}, ok: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(tc.in)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}
			_, protoErr := decodeForwardRemoveParams(raw)
			if tc.ok && protoErr != nil {
				t.Fatalf("decodeForwardRemoveParams() error = %#v, want nil", protoErr)
			}
			if !tc.ok && (protoErr == nil || protoErr.Code != ErrorBadRequest) {
				t.Fatalf("decodeForwardRemoveParams() error = %#v, want bad_request", protoErr)
			}
		})
	}
}
