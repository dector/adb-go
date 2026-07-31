package custom

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"

	adb "github.com/dector/adb-go"
	"github.com/dector/adb-go/internal/daemon"
)

const reverseUsage = `Usage:
  adb-go reverse [--socket PATH] (--addr HOST[:PORT] | --usb [USB selection]) tcp:REMOTE_PORT tcp:LOCAL_PORT
  adb-go reverse [--socket PATH] --background --addr HOST[:PORT] [--norebind] tcp:REMOTE_PORT tcp:LOCAL_PORT
  adb-go reverse [--socket PATH] --list
  adb-go reverse [--socket PATH] --remove tcp:REMOTE_PORT
  adb-go reverse [--socket PATH] --remove-id ID
  adb-go reverse [--socket PATH] --remove-all

Without daemon flags, starts foreground process-scoped reverse forwarding from a
TCP listener on the selected device to a TCP endpoint on host loopback. The
reverse exists only while this command keeps running. Press Ctrl+C to remove the
device-side reverse registration, close active bridge streams, and exit.

With --background, registers a daemon-owned in-memory reverse in adb-god. The
background path currently supports explicit TCP ADB targets only; USB targets
and authentication-key persistence are intentionally out of scope. List and
remove flags inspect or remove daemon-owned reverses without selecting a device.

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
	fs := flag.NewFlagSet("reverse", flag.ContinueOnError)
	fs.SetOutput(stderr)
	conn := addConnectionFlags(fs)
	socketPathFlag := fs.String("socket", "", "absolute adb-god Unix domain socket path for daemon-owned reverses")
	backgroundFlag := fs.Bool("background", false, "create a daemon-owned in-memory reverse and exit")
	norebindFlag := fs.Bool("norebind", false, "fail instead of replacing an existing daemon-owned reverse with the same remote endpoint")
	listFlag := fs.Bool("list", false, "list daemon-owned reverses")
	removeFlag := fs.String("remove", "", "remove the daemon-owned reverse with this remote endpoint, for example tcp:8081")
	removeIDFlag := fs.String("remove-id", "", "remove the daemon-owned reverse with this generated ID")
	removeAllFlag := fs.Bool("remove-all", false, "remove all daemon-owned reverses")
	fs.Usage = func() { fmt.Fprint(stderr, reverseUsage) }

	if err := fs.Parse(args); err != nil {
		return 2
	}

	daemonOps := 0
	for _, enabled := range []bool{*backgroundFlag, *listFlag, strings.TrimSpace(*removeFlag) != "", strings.TrimSpace(*removeIDFlag) != "", *removeAllFlag} {
		if enabled {
			daemonOps++
		}
	}
	if daemonOps > 1 {
		fmt.Fprint(stderr, "adb-go reverse: choose only one daemon operation: --background, --list, --remove, --remove-id, or --remove-all\n\n")
		fs.Usage()
		return 2
	}
	if *norebindFlag && !*backgroundFlag {
		fmt.Fprint(stderr, "adb-go reverse: --norebind is only meaningful with --background\n\n")
		fs.Usage()
		return 2
	}
	if daemonOps > 0 {
		socketPath, err := forwardDaemonSocketPath(strings.TrimSpace(*socketPathFlag))
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
			return runReverseDaemonList(socketPath, stdout, stderr)
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
			return runReverseDaemonRemove(socketPath, daemon.ReverseRemoveParams{Remote: &daemon.ReverseRemoteEndpoint{Service: strings.TrimSpace(*removeFlag)}}, stdout, stderr)
		}
		if strings.TrimSpace(*removeIDFlag) != "" {
			if fs.NArg() != 0 {
				fmt.Fprintf(stderr, "adb-go reverse --remove-id: unexpected arguments %q\n\n", fs.Args())
				fs.Usage()
				return 2
			}
			return runReverseDaemonRemove(socketPath, daemon.ReverseRemoveParams{ID: strings.TrimSpace(*removeIDFlag)}, stdout, stderr)
		}
		if *removeAllFlag {
			if fs.NArg() != 0 {
				fmt.Fprintf(stderr, "adb-go reverse --remove-all: unexpected arguments %q\n\n", fs.Args())
				fs.Usage()
				return 2
			}
			return runReverseDaemonRemoveAll(socketPath, stdout, stderr)
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
		socketPath, err := forwardDaemonSocketPath(strings.TrimSpace(*socketPathFlag))
		if err != nil {
			fmt.Fprintf(stderr, "adb-go reverse: %v\n", err)
			return 1
		}
		return runReverseDaemonCreate(socketPath, fs.Arg(0), fs.Arg(1), targetAddr, *norebindFlag, stdout, stderr)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	client, err := connectDevice(ctx, target)
	if err != nil {
		printConnectError(stderr, "reverse", target.description, err)
		return 1
	}
	defer client.Close()

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
	fmt.Fprintf(stdout, "Reverse forwarding %s -> host %s. Press Ctrl+C to stop and remove the device-side listener.\n", fs.Arg(0), fs.Arg(1))

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

func runReverseDaemonCreate(socketPath, remoteService, localService, targetAddr string, norebind bool, stdout, stderr io.Writer) int {
	params := daemon.ReverseCreateParams{
		Remote:   daemon.ReverseRemoteEndpoint{Service: remoteService},
		Local:    daemon.ReverseLocalEndpoint{Service: localService},
		Target:   daemon.ForwardTarget{Transport: "tcp", Address: targetAddr},
		Norebind: norebind,
	}
	resp, err := sendForwardDaemonRequest(socketPath, daemon.CommandReverseCreate, params)
	if err != nil || !resp.OK {
		printReverseDaemonError(stderr, "create background reverse", socketPath, resp, err)
		return 1
	}
	var result daemon.ReverseCreateResult
	if err := decodeDaemonResult(resp.Result, &result); err != nil {
		fmt.Fprintf(stderr, "adb-go reverse: decode daemon create response: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "Reverse %s listening on device %s -> host %s via tcp:%s\n", result.Reverse.ID, result.Reverse.Remote.Service, result.Reverse.Local.Service, result.Reverse.Target.Address)
	fmt.Fprintln(stdout, "Lifecycle: in-memory daemon-owned reverse; it is removed by --remove/--remove-all or adb-god shutdown.")
	return 0
}

func runReverseDaemonList(socketPath string, stdout, stderr io.Writer) int {
	resp, err := sendForwardDaemonRequest(socketPath, daemon.CommandReverseList, nil)
	if err != nil || !resp.OK {
		printReverseDaemonError(stderr, "list background reverses", socketPath, resp, err)
		return 1
	}
	var result daemon.ReverseListResult
	if err := decodeDaemonResult(resp.Result, &result); err != nil {
		fmt.Fprintf(stderr, "adb-go reverse: decode daemon list response: %v\n", err)
		return 1
	}
	if len(result.Reverses) == 0 {
		fmt.Fprintln(stdout, "No daemon-owned reverses.")
		return 0
	}
	fmt.Fprintln(stdout, "ID\tSTATE\tREMOTE\tLOCAL\tTARGET\tACTIVE\tLAST_ERROR")
	for _, r := range result.Reverses {
		fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\t%s:%s\t%d\t%s\n", r.ID, r.State, r.Remote.Service, r.Local.Service, r.Target.Transport, r.Target.Address, r.ActiveConnections, r.LastError)
	}
	return 0
}

func runReverseDaemonRemove(socketPath string, params daemon.ReverseRemoveParams, stdout, stderr io.Writer) int {
	resp, err := sendForwardDaemonRequest(socketPath, daemon.CommandReverseRemove, params)
	if err != nil || !resp.OK {
		printReverseDaemonError(stderr, "remove background reverse", socketPath, resp, err)
		return 1
	}
	var result daemon.ReverseRemoveResult
	if err := decodeDaemonResult(resp.Result, &result); err != nil {
		fmt.Fprintf(stderr, "adb-go reverse: decode daemon remove response: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "Removed %d daemon-owned reverse(s).\n", result.Removed)
	return 0
}

func runReverseDaemonRemoveAll(socketPath string, stdout, stderr io.Writer) int {
	resp, err := sendForwardDaemonRequest(socketPath, daemon.CommandReverseRemoveAll, nil)
	if err != nil || !resp.OK {
		printReverseDaemonError(stderr, "remove all background reverses", socketPath, resp, err)
		return 1
	}
	var result daemon.ReverseRemoveAllResult
	if err := decodeDaemonResult(resp.Result, &result); err != nil {
		fmt.Fprintf(stderr, "adb-go reverse: decode daemon remove-all response: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "Removed %d daemon-owned reverse(s).\n", result.Removed)
	return 0
}

func printReverseDaemonError(stderr io.Writer, action, socketPath string, resp daemon.Response, err error) {
	if err != nil {
		fmt.Fprintf(stderr, "adb-go reverse: cannot %s: adb-god is not running or socket is unavailable at %s: %v\n", action, socketPath, err)
		fmt.Fprintln(stderr, "Hint: start the daemon with `adb-go daemon service start` or inspect it with `adb-go daemon doctor`.")
		return
	}
	if resp.Error != nil {
		fmt.Fprintf(stderr, "adb-go reverse: cannot %s: daemon error %s: %s\n", action, resp.Error.Code, resp.Error.Message)
		if resp.Error.Code == daemon.ErrorUnknownCommand {
			fmt.Fprintln(stderr, "Hint: adb-god is too old for persistent reverse forwarding; upgrade adb-god or inspect it with `adb-go daemon doctor`.")
		}
		return
	}
	fmt.Fprintf(stderr, "adb-go reverse: cannot %s: daemon returned an unsuccessful response\n", action)
}
