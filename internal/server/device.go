package server

import (
	"encoding/json"
	"fmt"
	"net"
	"sort"
)

const (
	CommandDeviceList     = "device_list"
	CommandDeviceRegister = "device_register"

	DeviceStateDevice  = "device"
	DeviceStateOffline = "offline"
)

// Device describes one server-known ADB transport for host-side CLI listing and
// future target selection. The server intentionally stores transport metadata,
// not an open ADB protocol connection.
type Device struct {
	Serial    string `json:"serial"`
	State     string `json:"state"`
	Transport string `json:"transport"`
	Address   string `json:"address,omitempty"`
}

type DeviceRegisterParams struct {
	Serial    string `json:"serial,omitempty"`
	Transport string `json:"transport"`
	Address   string `json:"address,omitempty"`
	State     string `json:"state,omitempty"`
}

type deviceRegistry struct {
	bySerial map[string]Device
}

func newDeviceRegistry() *deviceRegistry {
	return &deviceRegistry{bySerial: make(map[string]Device)}
}

func (r *deviceRegistry) register(params DeviceRegisterParams) (Device, *Error) {
	device, errResp := validateDeviceRegisterParams(params)
	if errResp != nil {
		return Device{}, errResp
	}
	r.bySerial[device.Serial] = device
	return device, nil
}

func (r *deviceRegistry) list() []Device {
	devices := make([]Device, 0, len(r.bySerial))
	for _, device := range r.bySerial {
		devices = append(devices, device)
	}
	sort.Slice(devices, func(i, j int) bool { return devices[i].Serial < devices[j].Serial })
	return devices
}

func decodeDeviceRegisterParams(raw json.RawMessage) (DeviceRegisterParams, *Error) {
	if len(raw) == 0 {
		return DeviceRegisterParams{}, &Error{Code: ErrorBadRequest, Message: "device_register requires params"}
	}
	var params DeviceRegisterParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return DeviceRegisterParams{}, &Error{Code: ErrorBadRequest, Message: fmt.Sprintf("invalid device_register params: %v", err)}
	}
	return params, nil
}

func validateDeviceRegisterParams(params DeviceRegisterParams) (Device, *Error) {
	if params.Transport != "tcp" {
		return Device{}, &Error{Code: ErrorBadRequest, Message: fmt.Sprintf("unsupported device transport %q", params.Transport)}
	}
	if params.Address == "" {
		return Device{}, &Error{Code: ErrorBadRequest, Message: "device_register tcp transport requires address"}
	}
	if _, _, err := net.SplitHostPort(params.Address); err != nil {
		return Device{}, &Error{Code: ErrorBadRequest, Message: fmt.Sprintf("invalid tcp device address %q", params.Address)}
	}
	state := params.State
	if state == "" {
		state = DeviceStateDevice
	}
	if state != DeviceStateDevice && state != DeviceStateOffline {
		return Device{}, &Error{Code: ErrorBadRequest, Message: fmt.Sprintf("unsupported device state %q", state)}
	}
	serial := params.Serial
	if serial == "" {
		serial = params.Address
	}
	return Device{Serial: serial, State: state, Transport: params.Transport, Address: params.Address}, nil
}
