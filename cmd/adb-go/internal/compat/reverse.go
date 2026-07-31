package compat

import (
	"context"
	"fmt"
	"io"
	"strings"

	adb "github.com/dector/adb-go"
	"github.com/dector/adb-go/cmd/adb-go/internal/cliserver"
	"github.com/dector/adb-go/internal/server"
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
	resp, err := reverseServerRequest(server.CommandReverseCreate, server.ReverseCreateParams{
		Remote:   server.ReverseRemoteEndpoint{Service: strings.TrimSpace(remoteService)},
		Local:    server.ReverseLocalEndpoint{Service: strings.TrimSpace(localService)},
		Target:   server.ForwardTarget{Transport: "tcp", Address: target.TCPAddress},
		Norebind: norebind,
	})
	if err != nil || !resp.OK {
		printCompatReverseServerError(stderr, resp, err)
		return 1
	}
	_ = stdout // Official adb is silent when reverse creation succeeds.
	return 0
}

func runReverseList(opts globalOptions, stdout, stderr io.Writer) int {
	resp, err := reverseServerRequest(server.CommandReverseList, nil)
	if err != nil || !resp.OK {
		printCompatReverseServerError(stderr, resp, err)
		return 1
	}
	var result server.ReverseListResult
	if err := decodeServerResult(resp.Result, &result); err != nil {
		fmt.Fprintf(stderr, "adb: reverse --list: decode server response: %v\n", err)
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
	resp, err := reverseServerRequest(server.CommandReverseRemove, server.ReverseRemoveParams{ID: id})
	if err != nil || !resp.OK {
		printCompatReverseServerError(stderr, resp, err)
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
	resp, err := reverseServerRequest(server.CommandReverseList, nil)
	if err != nil || !resp.OK {
		printCompatReverseServerError(stderr, resp, err)
		return 1
	}
	var result server.ReverseListResult
	if err := decodeServerResult(resp.Result, &result); err != nil {
		fmt.Fprintf(stderr, "adb: reverse --remove-all: decode server response: %v\n", err)
		return 1
	}
	for _, r := range result.Reverses {
		if r.Target.Address != target.TCPAddress {
			continue
		}
		resp, err := reverseServerRequest(server.CommandReverseRemove, server.ReverseRemoveParams{ID: r.ID})
		if err != nil || !resp.OK {
			printCompatReverseServerError(stderr, resp, err)
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
	if target.State != server.DeviceStateDevice {
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
	resp, err := reverseServerRequest(server.CommandReverseList, nil)
	if err != nil || !resp.OK {
		printCompatReverseServerError(stderr, resp, err)
		return "", false
	}
	var result server.ReverseListResult
	if err := decodeServerResult(resp.Result, &result); err != nil {
		fmt.Fprintf(stderr, "adb: reverse --remove: decode server response: %v\n", err)
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

func reverseServerRequest(command string, params any) (server.Response, error) {
	if _, err := ensureServer(context.Background(), globalOptions{}); err != nil {
		return server.Response{}, err
	}
	socketPath, err := defaultServerSocketPath()
	if err != nil {
		return server.Response{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), serverStartupTimeout)
	defer cancel()
	return cliserver.Send(ctx, socketPath, command, params, sendServerRequest)
}

func decodeServerResult(result map[string]any, out any) error {
	return cliserver.DecodeResult(result, out)
}

func reverseCompatSerial(target server.ForwardTarget) string {
	if target.Transport == "tcp" {
		return target.Address
	}
	return target.Transport + ":" + target.Address
}

func printCompatReverseServerError(stderr io.Writer, resp server.Response, err error) {
	if err != nil {
		fmt.Fprintf(stderr, "adb: reverse: %v\n", err)
		return
	}
	if resp.Error != nil {
		fmt.Fprintf(stderr, "adb: reverse: %s\n", serverErrorMessage(resp))
		return
	}
	fmt.Fprintln(stderr, "adb: reverse: server returned an unsuccessful response")
}
