package compat

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	adb "github.com/dector/adb-go"
	"github.com/dector/adb-go/internal/daemon"
)

func runReverse(args []string, opts globalOptions, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "adb: reverse requires an argument")
		return 1
	}

	norebind := false
	if args[0] == "--no-rebind" {
		norebind = true
		args = args[1:]
	}
	if len(args) == 0 {
		fmt.Fprintln(stderr, "adb: reverse requires an argument")
		return 1
	}

	switch args[0] {
	case "--list":
		if norebind {
			fmt.Fprintln(stderr, "adb: reverse: --no-rebind is only valid when creating a reverse")
			return 1
		}
		if len(args) != 1 {
			fmt.Fprintf(stderr, "adb: reverse --list: unexpected arguments %q\n", args[1:])
			return 1
		}
		return runReverseList(opts, stdout, stderr)
	case "--remove":
		if norebind {
			fmt.Fprintln(stderr, "adb: reverse: --no-rebind is only valid when creating a reverse")
			return 1
		}
		if len(args) != 2 {
			fmt.Fprintln(stderr, "adb: reverse --remove requires REMOTE")
			return 1
		}
		return runReverseRemove(opts, args[1], stdout, stderr)
	case "--remove-all":
		if norebind {
			fmt.Fprintln(stderr, "adb: reverse: --no-rebind is only valid when creating a reverse")
			return 1
		}
		if len(args) != 1 {
			fmt.Fprintf(stderr, "adb: reverse --remove-all: unexpected arguments %q\n", args[1:])
			return 1
		}
		return runReverseRemoveAll(opts, stdout, stderr)
	default:
		if len(args) != 2 {
			fmt.Fprintln(stderr, "adb: reverse requires REMOTE and LOCAL")
			return 1
		}
		return runReverseCreate(opts, args[0], args[1], norebind, stdout, stderr)
	}
}

func runReverseCreate(opts globalOptions, remoteService, localService string, norebind bool, stdout, stderr io.Writer) int {
	if _, err := adb.ParseReverseDeviceEndpoint(remoteService); err != nil {
		fmt.Fprintf(stderr, "adb: reverse: %v\n", err)
		return 1
	}
	if _, err := adb.ParseReverseHostEndpoint(localService); err != nil {
		fmt.Fprintf(stderr, "adb: reverse: %v\n", err)
		return 1
	}
	target, ok := resolveReverseTarget(opts, stderr)
	if !ok {
		return 1
	}
	resp, err := reverseDaemonRequest(daemon.CommandReverseCreate, daemon.ReverseCreateParams{
		Remote:   daemon.ReverseRemoteEndpoint{Service: strings.TrimSpace(remoteService)},
		Local:    daemon.ReverseLocalEndpoint{Service: strings.TrimSpace(localService)},
		Target:   daemon.ForwardTarget{Transport: "tcp", Address: target.TCPAddress},
		Norebind: norebind,
	})
	if err != nil || !resp.OK {
		printCompatReverseDaemonError(stderr, resp, err)
		return 1
	}
	_ = stdout // Official adb is silent when reverse creation succeeds.
	return 0
}

func runReverseList(opts globalOptions, stdout, stderr io.Writer) int {
	resp, err := reverseDaemonRequest(daemon.CommandReverseList, nil)
	if err != nil || !resp.OK {
		printCompatReverseDaemonError(stderr, resp, err)
		return 1
	}
	var result daemon.ReverseListResult
	if err := decodeDaemonResult(resp.Result, &result); err != nil {
		fmt.Fprintf(stderr, "adb: reverse --list: decode daemon response: %v\n", err)
		return 1
	}
	var filterAddress string
	if opts.selector.kind != targetSelectorNone {
		target, ok := resolveReverseTarget(opts, stderr)
		if !ok {
			return 1
		}
		filterAddress = target.TCPAddress
	}
	for _, r := range result.Reverses {
		if filterAddress != "" && r.Target.Address != filterAddress {
			continue
		}
		fmt.Fprintf(stdout, "%s %s %s\n", reverseCompatSerial(r.Target), r.Remote.Service, r.Local.Service)
	}
	return 0
}

func runReverseRemove(opts globalOptions, remoteService string, stdout, stderr io.Writer) int {
	if _, err := adb.ParseReverseDeviceEndpoint(remoteService); err != nil {
		fmt.Fprintf(stderr, "adb: reverse: %v\n", err)
		return 1
	}
	target, ok := resolveReverseTarget(opts, stderr)
	if !ok {
		return 1
	}
	id, ok := findReverseID(target.TCPAddress, strings.TrimSpace(remoteService), stderr)
	if !ok {
		return 1
	}
	resp, err := reverseDaemonRequest(daemon.CommandReverseRemove, daemon.ReverseRemoveParams{ID: id})
	if err != nil || !resp.OK {
		printCompatReverseDaemonError(stderr, resp, err)
		return 1
	}
	_ = stdout
	return 0
}

func runReverseRemoveAll(opts globalOptions, stdout, stderr io.Writer) int {
	target, ok := resolveReverseTarget(opts, stderr)
	if !ok {
		return 1
	}
	resp, err := reverseDaemonRequest(daemon.CommandReverseList, nil)
	if err != nil || !resp.OK {
		printCompatReverseDaemonError(stderr, resp, err)
		return 1
	}
	var result daemon.ReverseListResult
	if err := decodeDaemonResult(resp.Result, &result); err != nil {
		fmt.Fprintf(stderr, "adb: reverse --remove-all: decode daemon response: %v\n", err)
		return 1
	}
	for _, r := range result.Reverses {
		if r.Target.Address != target.TCPAddress {
			continue
		}
		resp, err := reverseDaemonRequest(daemon.CommandReverseRemove, daemon.ReverseRemoveParams{ID: r.ID})
		if err != nil || !resp.OK {
			printCompatReverseDaemonError(stderr, resp, err)
			return 1
		}
	}
	_ = stdout
	return 0
}

func resolveReverseTarget(opts globalOptions, stderr io.Writer) (compatTarget, bool) {
	target, err := resolveCompatTarget(context.Background(), opts)
	if err != nil {
		fmt.Fprintf(stderr, "adb: reverse: %v\n", err)
		return compatTarget{}, false
	}
	if target.State != daemon.DeviceStateDevice {
		fmt.Fprintf(stderr, "adb: reverse: device %s not available: %s\n", target.Serial, target.State)
		return compatTarget{}, false
	}
	if target.Transport != "tcp" || strings.TrimSpace(target.TCPAddress) == "" {
		fmt.Fprintln(stderr, "adb: reverse: USB reverse forwarding is not supported by adb-go compat yet")
		return compatTarget{}, false
	}
	return target, true
}

func findReverseID(targetAddress, remoteService string, stderr io.Writer) (string, bool) {
	resp, err := reverseDaemonRequest(daemon.CommandReverseList, nil)
	if err != nil || !resp.OK {
		printCompatReverseDaemonError(stderr, resp, err)
		return "", false
	}
	var result daemon.ReverseListResult
	if err := decodeDaemonResult(resp.Result, &result); err != nil {
		fmt.Fprintf(stderr, "adb: reverse --remove: decode daemon response: %v\n", err)
		return "", false
	}
	for _, r := range result.Reverses {
		if r.Target.Address == targetAddress && r.Remote.Service == remoteService {
			return r.ID, true
		}
	}
	fmt.Fprintln(stderr, "adb: reverse: listener not found")
	return "", false
}

func reverseDaemonRequest(command string, params any) (daemon.Response, error) {
	if _, err := ensureDaemon(context.Background(), globalOptions{}); err != nil {
		return daemon.Response{}, err
	}
	socketPath, err := defaultDaemonSocketPath()
	if err != nil {
		return daemon.Response{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), daemonStartupTimeout)
	defer cancel()
	return sendDaemonRequest(ctx, socketPath, daemon.Request{Version: daemon.ProtocolVersion, Command: command, Params: mustRawParams(params)})
}

func mustRawParams(params any) json.RawMessage {
	if params == nil {
		return nil
	}
	raw, err := json.Marshal(params)
	if err != nil {
		panic(err)
	}
	return raw
}

func decodeDaemonResult(result map[string]any, out any) error {
	body, err := json.Marshal(result)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, out)
}

func reverseCompatSerial(target daemon.ForwardTarget) string {
	if target.Transport == "tcp" {
		return target.Address
	}
	return target.Transport + ":" + target.Address
}

func printCompatReverseDaemonError(stderr io.Writer, resp daemon.Response, err error) {
	if err != nil {
		fmt.Fprintf(stderr, "adb: reverse: %v\n", err)
		return
	}
	if resp.Error != nil {
		fmt.Fprintf(stderr, "adb: reverse: %s\n", daemonErrorMessage(resp))
		return
	}
	fmt.Fprintln(stderr, "adb: reverse: daemon returned an unsuccessful response")
}
