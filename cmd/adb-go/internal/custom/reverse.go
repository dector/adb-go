package custom

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"

	adb "github.com/dector/adb-go"
	"github.com/dector/adb-go/internal/server"
)

const reverseUsage = `Usage:
  adb-go reverse [--socket PATH] (--addr HOST[:PORT] | --usb [USB selection]) tcp:REMOTE_PORT tcp:LOCAL_PORT
  adb-go reverse [--socket PATH] --background --addr HOST[:PORT] [--norebind] tcp:REMOTE_PORT tcp:LOCAL_PORT
  adb-go reverse [--socket PATH] --list [--plain]
  adb-go reverse [--socket PATH] --remove tcp:REMOTE_PORT
  adb-go reverse [--socket PATH] --remove-id ID
  adb-go reverse [--socket PATH] --remove-all

Without server flags, starts foreground process-scoped reverse forwarding from a
TCP listener on the selected device to a TCP endpoint on host loopback. The
reverse exists only while this command keeps running. Press Ctrl+C to remove the
device-side reverse registration, close active bridge streams, and exit.

With --background, registers a server-owned in-memory reverse in adb-gos. The
background path currently supports explicit TCP ADB targets only; USB targets
and authentication-key persistence are intentionally out of scope. List and
remove flags inspect or remove server-owned reverses without selecting a device.

Only tcp:PORT endpoints are supported. Device-side tcp:0, host Unix sockets,
Android local socket namespaces, JDWP, vsock, and generic service endpoints are
not supported yet. For example:

  adb-go reverse --addr 127.0.0.1:5555 tcp:8081 tcp:3000
  adb-go reverse --auth-key ~/.android/adbkey --usb-path /dev/bus/usb/001/002 tcp:8081 tcp:3000
  adb-go reverse --background --addr 127.0.0.1:5555 tcp:8081 tcp:3000
  adb-go reverse --list
  adb-go reverse --remove tcp:8081
`

func runReverse(args []string, stdout, stderr io.Writer) int {
	return runReverseWithJSON(args, false, stdout, stderr)
}

func runReverseWithJSON(args []string, jsonOutput bool, stdout, stderr io.Writer) int {
	return runReverseWithOptions(args, cliOptions{JSON: jsonOutput}, stdout, stderr)
}

func runReverseWithOptions(args []string, opts cliOptions, stdout, stderr io.Writer) int {
	jsonOutput := opts.JSON
	out := newOutputPolicy(stdout, stderr, opts)
	fs := flag.NewFlagSet("reverse", flag.ContinueOnError)
	fs.SetOutput(stderr)
	conn := addConnectionFlags(fs)
	socketPathFlag := fs.String("socket", "", "absolute adb-gos Unix domain socket path for server-owned reverses")
	backgroundFlag := fs.Bool("background", false, "create a server-owned in-memory reverse and exit")
	norebindFlag := fs.Bool("norebind", false, "fail instead of replacing an existing server-owned reverse with the same remote endpoint")
	listFlag := fs.Bool("list", false, "list server-owned reverses")
	removeFlag := fs.String("remove", "", "remove the server-owned reverse with this remote endpoint, for example tcp:8081")
	removeIDFlag := fs.String("remove-id", "", "remove the server-owned reverse with this generated ID")
	removeAllFlag := fs.Bool("remove-all", false, "remove all server-owned reverses")
	jsonFlag := fs.Bool("json", jsonOutput, "print machine-readable JSON for server list/create/remove operations")
	plainFlag := fs.Bool("plain", false, "print tab-separated plain output for --list")
	fs.Usage = func() { fmt.Fprint(stderr, reverseUsage) }

	if err := fs.Parse(args); err != nil {
		return 2
	}

	jsonOutput = *jsonFlag
	plainOutput := *plainFlag
	if jsonOutput && plainOutput {
		fmt.Fprint(stderr, "adb-go reverse: choose only one output mode: --json or --plain\n\n")
		fs.Usage()
		return 2
	}
	serverOps := 0
	for _, enabled := range []bool{*backgroundFlag, *listFlag, strings.TrimSpace(*removeFlag) != "", strings.TrimSpace(*removeIDFlag) != "", *removeAllFlag} {
		if enabled {
			serverOps++
		}
	}
	if serverOps > 1 {
		fmt.Fprint(stderr, "adb-go reverse: choose only one server operation: --background, --list, --remove, --remove-id, or --remove-all\n\n")
		fs.Usage()
		return 2
	}
	if *norebindFlag && !*backgroundFlag {
		fmt.Fprint(stderr, "adb-go reverse: --norebind is only meaningful with --background\n\n")
		fs.Usage()
		return 2
	}
	if serverOps > 0 {
		socketPath, err := forwardServerSocketPath(strings.TrimSpace(*socketPathFlag))
		if err != nil {
			fmt.Fprintf(stderr, "adb-go reverse: %v\n", err)
			return 1
		}
		if *listFlag {
			if fs.NArg() != 0 {
				fmt.Fprintf(stderr, "adb-go reverse --list: unexpected arguments %q\n\n", fs.Args())
				fs.Usage()
				return 2
			}
			return runReverseServerList(socketPath, jsonOutput, plainOutput, stdout, stderr)
		}
		if strings.TrimSpace(*removeFlag) != "" {
			if fs.NArg() != 0 {
				fmt.Fprintf(stderr, "adb-go reverse --remove: unexpected arguments %q\n\n", fs.Args())
				fs.Usage()
				return 2
			}
			if _, err := adb.ParseReverseDeviceEndpoint(*removeFlag); err != nil {
				fmt.Fprintf(stderr, "adb-go reverse: %v\n\n", err)
				fs.Usage()
				return 2
			}
			return runReverseServerRemove(socketPath, server.ReverseRemoveParams{Remote: &server.ReverseRemoteEndpoint{Service: strings.TrimSpace(*removeFlag)}}, jsonOutput, out, stdout, stderr)
		}
		if strings.TrimSpace(*removeIDFlag) != "" {
			if fs.NArg() != 0 {
				fmt.Fprintf(stderr, "adb-go reverse --remove-id: unexpected arguments %q\n\n", fs.Args())
				fs.Usage()
				return 2
			}
			return runReverseServerRemove(socketPath, server.ReverseRemoveParams{ID: strings.TrimSpace(*removeIDFlag)}, jsonOutput, out, stdout, stderr)
		}
		if *removeAllFlag {
			if fs.NArg() != 0 {
				fmt.Fprintf(stderr, "adb-go reverse --remove-all: unexpected arguments %q\n\n", fs.Args())
				fs.Usage()
				return 2
			}
			return runReverseServerRemoveAll(socketPath, jsonOutput, out, stdout, stderr)
		}
	}
	if fs.NArg() != 2 {
		fmt.Fprint(stderr, "adb-go reverse: requires exactly REMOTE and LOCAL targets\n\n")
		fs.Usage()
		return 2
	}

	target, err := conn.target(fs)
	if err != nil {
		fmt.Fprintf(stderr, "adb-go reverse: %v\n\n", err)
		fs.Usage()
		return 2
	}
	remote, err := adb.ParseReverseDeviceEndpoint(fs.Arg(0))
	if err != nil {
		fmt.Fprintf(stderr, "adb-go reverse: %v\n\n", err)
		fs.Usage()
		return 2
	}
	local, err := adb.ParseReverseHostEndpoint(fs.Arg(1))
	if err != nil {
		fmt.Fprintf(stderr, "adb-go reverse: %v\n\n", err)
		fs.Usage()
		return 2
	}

	if *backgroundFlag {
		if target.usb || len(target.auth) != 0 {
			fmt.Fprint(stderr, "adb-go reverse: --background currently supports explicit unauthenticated TCP targets only; USB targets and --auth-key persistence are not implemented\n")
			return 2
		}
		targetAddr, err := normalizeForwardTargetTCPAddr(target.tcpAddr)
		if err != nil {
			fmt.Fprintf(stderr, "adb-go reverse: %v\n\n", err)
			fs.Usage()
			return 2
		}
		socketPath, err := forwardServerSocketPath(strings.TrimSpace(*socketPathFlag))
		if err != nil {
			fmt.Fprintf(stderr, "adb-go reverse: %v\n", err)
			return 1
		}
		return runReverseServerCreate(socketPath, fs.Arg(0), fs.Arg(1), targetAddr, *norebindFlag, jsonOutput, out, stdout, stderr)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	out.Verbosef("reverse parsed device listener %s and host target %s\n", fs.Arg(0), fs.Arg(1))
	client, err := connectTarget(ctx, "reverse", target, out)
	if err != nil {
		printConnectError(stderr, "reverse", target.description, err)
		return 1
	}
	defer client.Close()

	out.Verbosef("reverse registering device-side listener and host bridge\n")
	reverse, err := startReverse(ctx, client, remote, local)
	if err != nil {
		printCommandError(stderr, "reverse", err)
		return 1
	}
	closed := false
	defer func() {
		if !closed {
			_ = reverse.Close()
		}
	}()
	out.Infof("Reverse forwarding %s -> host %s. Press Ctrl+C to stop and remove the device-side listener.\n", fs.Arg(0), fs.Arg(1))

	if err := reverse.Wait(); err != nil {
		printCommandError(stderr, "reverse", err)
		return 1
	}
	closed = true
	if err := reverse.Close(); err != nil {
		printCommandError(stderr, "reverse", err)
		return 1
	}
	return 0
}

func runReverseServerCreate(socketPath, remoteService, localService, targetAddr string, norebind bool, jsonOutput bool, out outputPolicy, stdout, stderr io.Writer) int {
	out.Verbosef("reverse sending server create request to %s for %s -> %s via tcp:%s (norebind=%t)\n", socketPath, remoteService, localService, targetAddr, norebind)
	params := server.ReverseCreateParams{
		Remote:   server.ReverseRemoteEndpoint{Service: remoteService},
		Local:    server.ReverseLocalEndpoint{Service: localService},
		Target:   server.ForwardTarget{Transport: "tcp", Address: targetAddr},
		Norebind: norebind,
	}
	resp, err := sendForwardServerRequest(socketPath, server.CommandReverseCreate, params)
	if err != nil || !resp.OK {
		printReverseServerError(stderr, "create background reverse", socketPath, resp, err)
		return 1
	}
	var result server.ReverseCreateResult
	if err := decodeServerResult(resp.Result, &result); err != nil {
		fmt.Fprintf(stderr, "adb-go reverse: decode server create response: %v\n", err)
		return 1
	}
	if jsonOutput {
		if err := writeJSON(stdout, result); err != nil {
			fmt.Fprintf(stderr, "adb-go reverse: encode JSON: %v\n", err)
			return 1
		}
		return 0
	}
	out.Infof("Reverse %s listening on device %s -> host %s via tcp:%s\n", result.Reverse.ID, result.Reverse.Remote.Service, result.Reverse.Local.Service, result.Reverse.Target.Address)
	out.Infoln("Lifecycle: in-memory server-owned reverse; it is removed by --remove/--remove-all or adb-gos shutdown.")
	return 0
}

func runReverseServerList(socketPath string, jsonOutput bool, plainOutput bool, stdout, stderr io.Writer) int {
	resp, err := sendForwardServerRequest(socketPath, server.CommandReverseList, nil)
	if err != nil || !resp.OK {
		printReverseServerError(stderr, "list background reverses", socketPath, resp, err)
		return 1
	}
	var result server.ReverseListResult
	if err := decodeServerResult(resp.Result, &result); err != nil {
		fmt.Fprintf(stderr, "adb-go reverse: decode server list response: %v\n", err)
		return 1
	}
	if jsonOutput {
		if err := writeJSON(stdout, result); err != nil {
			fmt.Fprintf(stderr, "adb-go reverse: encode JSON: %v\n", err)
			return 1
		}
		return 0
	}
	if len(result.Reverses) == 0 {
		fmt.Fprintln(stdout, "No server-owned reverse forwards.")
		fmt.Fprintln(stdout, "Create one with: adb-go reverse --background --addr HOST[:PORT] tcp:REMOTE_PORT tcp:LOCAL_PORT")
		return 0
	}
	rows := make([]tableRow, 0, len(result.Reverses))
	for _, r := range result.Reverses {
		lastError := r.LastError
		if lastError == "" {
			lastError = "-"
		}
		rows = append(rows, tableRow{r.ID, string(r.State), r.Remote.Service, r.Local.Service, r.Target.Transport + ":" + r.Target.Address, strconv.Itoa(r.ActiveConnections), lastError})
	}
	headers := []string{"ID", "STATE", "REMOTE", "LOCAL", "TARGET", "ACTIVE", "LAST_ERROR"}
	if plainOutput {
		if err := writePlainTable(stdout, headers, rows); err != nil {
			fmt.Fprintf(stderr, "adb-go reverse: write table: %v\n", err)
			return 1
		}
		return 0
	}
	if err := writeAlignedTable(stdout, headers, rows); err != nil {
		fmt.Fprintf(stderr, "adb-go reverse: write table: %v\n", err)
		return 1
	}
	return 0
}

func runReverseServerRemove(socketPath string, params server.ReverseRemoveParams, jsonOutput bool, out outputPolicy, stdout, stderr io.Writer) int {
	resp, err := sendForwardServerRequest(socketPath, server.CommandReverseRemove, params)
	if err != nil || !resp.OK {
		printReverseServerError(stderr, "remove background reverse", socketPath, resp, err)
		return 1
	}
	var result server.ReverseRemoveResult
	if err := decodeServerResult(resp.Result, &result); err != nil {
		fmt.Fprintf(stderr, "adb-go reverse: decode server remove response: %v\n", err)
		return 1
	}
	if jsonOutput {
		if err := writeJSON(stdout, result); err != nil {
			fmt.Fprintf(stderr, "adb-go reverse: encode JSON: %v\n", err)
			return 1
		}
		return 0
	}
	out.Infof("Removed %d server-owned reverse(s).\n", result.Removed)
	return 0
}

func runReverseServerRemoveAll(socketPath string, jsonOutput bool, out outputPolicy, stdout, stderr io.Writer) int {
	resp, err := sendForwardServerRequest(socketPath, server.CommandReverseRemoveAll, nil)
	if err != nil || !resp.OK {
		printReverseServerError(stderr, "remove all background reverses", socketPath, resp, err)
		return 1
	}
	var result server.ReverseRemoveAllResult
	if err := decodeServerResult(resp.Result, &result); err != nil {
		fmt.Fprintf(stderr, "adb-go reverse: decode server remove-all response: %v\n", err)
		return 1
	}
	if jsonOutput {
		if err := writeJSON(stdout, result); err != nil {
			fmt.Fprintf(stderr, "adb-go reverse: encode JSON: %v\n", err)
			return 1
		}
		return 0
	}
	out.Infof("Removed %d server-owned reverse(s).\n", result.Removed)
	return 0
}

func printReverseServerError(stderr io.Writer, action, socketPath string, resp server.Response, err error) {
	if err != nil {
		fmt.Fprintf(stderr, "adb-go reverse: cannot %s: adb-gos is not running or socket is unavailable at %s: %v\n", action, socketPath, err)
		fmt.Fprintln(stderr, "Hint: start the server process with `adb-go server service start` or inspect it with `adb-go server doctor`.")
		return
	}
	if resp.Error != nil {
		fmt.Fprintf(stderr, "adb-go reverse: cannot %s: server error %s: %s\n", action, resp.Error.Code, resp.Error.Message)
		if resp.Error.Code == server.ErrorUnknownCommand {
			fmt.Fprintln(stderr, "Hint: adb-gos is too old for persistent reverse forwarding; upgrade adb-gos or inspect it with `adb-go server doctor`.")
		}
		return
	}
	fmt.Fprintf(stderr, "adb-go reverse: cannot %s: server returned an unsuccessful response\n", action)
}
