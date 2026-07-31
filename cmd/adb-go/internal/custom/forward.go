package custom

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	adb "github.com/dector/adb-go"
	"github.com/dector/adb-go/cmd/adb-go/internal/clidaemon"
	"github.com/dector/adb-go/internal/daemon"
)

type forwardSession interface {
	LocalAddr() net.Addr
	Close() error
	Wait() error
}

type reverseSession interface {
	Close() error
	Wait() error
}

var startForward = func(ctx context.Context, client deviceClient, localAddr string, remote adb.ForwardTarget) (forwardSession, error) {
	forwarder, ok := client.(interface {
		ForwardLocalTCP(context.Context, string, adb.ForwardTarget) (*adb.Forward, error)
	})
	if !ok {
		return nil, fmt.Errorf("connected client does not support forwarding")
	}
	return forwarder.ForwardLocalTCP(ctx, localAddr, remote)
}

var startReverse = func(ctx context.Context, client deviceClient, remote adb.ReverseDeviceEndpoint, local adb.ReverseHostEndpoint) (reverseSession, error) {
	reverser, ok := client.(interface {
		ReverseTCP(context.Context, adb.ReverseDeviceEndpoint, adb.ReverseHostEndpoint) (*adb.Reverse, error)
	})
	if !ok {
		return nil, fmt.Errorf("connected client does not support reverse forwarding")
	}
	return reverser.ReverseTCP(ctx, remote, local)
}

const forwardUsage = `Usage:
  adb-go forward [--socket PATH] (--addr HOST[:PORT] | --usb [USB selection]) tcp:LOCAL_PORT tcp:REMOTE_PORT
  adb-go forward [--socket PATH] --background --addr HOST[:PORT] [--norebind] tcp:LOCAL_PORT tcp:REMOTE_PORT
  adb-go forward [--socket PATH] --list [--plain]
  adb-go forward [--socket PATH] --remove tcp:LOCAL_PORT
  adb-go forward [--socket PATH] --remove-id ID
  adb-go forward [--socket PATH] --remove-all

Without daemon flags, starts foreground process-scoped forwarding from a local
TCP listener to a TCP endpoint on the selected device. This behavior is
unchanged: the forward exists only while this command keeps running.

With --background, registers a daemon-owned in-memory forward in adb-god. The
background path currently supports explicit TCP ADB targets only; USB targets
and authentication-key persistence are intentionally out of scope. List and
remove flags inspect or remove daemon-owned forwards without selecting a device.

Use tcp:0 as LOCAL_PORT to ask the OS for an available local port. adb-go
prints the bound local listener address before it starts waiting. For example:

  adb-go forward --addr 127.0.0.1:5555 tcp:9000 tcp:8000
  adb-go forward --auth-key ~/.android/adbkey --usb-path /dev/bus/usb/001/002 tcp:0 tcp:8000
  adb-go forward --background --addr 127.0.0.1:5555 tcp:0 tcp:8000
  adb-go forward --list
  adb-go forward --remove tcp:9000
`

func runForward(args []string, stdout, stderr io.Writer) int {
	return runForwardWithJSON(args, false, stdout, stderr)
}

func runForwardWithJSON(args []string, jsonOutput bool, stdout, stderr io.Writer) int {
	return runForwardWithOptions(args, cliOptions{JSON: jsonOutput}, stdout, stderr)
}

func runForwardWithOptions(args []string, opts cliOptions, stdout, stderr io.Writer) int {
	jsonOutput := opts.JSON
	out := newOutputPolicy(stdout, stderr, opts)
	fs := flag.NewFlagSet("forward", flag.ContinueOnError)
	fs.SetOutput(stderr)
	conn := addConnectionFlags(fs)
	socketPathFlag := fs.String("socket", "", "absolute adb-god Unix domain socket path for daemon-owned forwards")
	backgroundFlag := fs.Bool("background", false, "create a daemon-owned in-memory forward and exit")
	norebindFlag := fs.Bool("norebind", false, "fail instead of replacing an existing daemon-owned forward with the same local endpoint")
	listFlag := fs.Bool("list", false, "list daemon-owned forwards")
	removeFlag := fs.String("remove", "", "remove the daemon-owned forward with this local endpoint, for example tcp:9000")
	removeIDFlag := fs.String("remove-id", "", "remove the daemon-owned forward with this generated ID")
	removeAllFlag := fs.Bool("remove-all", false, "remove all daemon-owned forwards")
	jsonFlag := fs.Bool("json", jsonOutput, "print machine-readable JSON for daemon list/create/remove operations")
	plainFlag := fs.Bool("plain", false, "print tab-separated plain output for --list")
	fs.Usage = func() { fmt.Fprint(stderr, forwardUsage) }

	if err := fs.Parse(args); err != nil {
		return 2
	}

	jsonOutput = *jsonFlag
	plainOutput := *plainFlag
	if jsonOutput && plainOutput {
		fmt.Fprint(stderr, "adb-go forward: choose only one output mode: --json or --plain\n\n")
		fs.Usage()
		return 2
	}
	daemonOps := 0
	for _, enabled := range []bool{*backgroundFlag, *listFlag, strings.TrimSpace(*removeFlag) != "", strings.TrimSpace(*removeIDFlag) != "", *removeAllFlag} {
		if enabled {
			daemonOps++
		}
	}
	if daemonOps > 1 {
		fmt.Fprint(stderr, "adb-go forward: choose only one daemon operation: --background, --list, --remove, --remove-id, or --remove-all\n\n")
		fs.Usage()
		return 2
	}

	if *norebindFlag && !*backgroundFlag {
		fmt.Fprint(stderr, "adb-go forward: --norebind is only meaningful with --background\n\n")
		fs.Usage()
		return 2
	}
	socketPath := ""
	if daemonOps > 0 {
		var err error
		socketPath, err = forwardDaemonSocketPath(strings.TrimSpace(*socketPathFlag))
		if err != nil {
			fmt.Fprintf(stderr, "adb-go forward: %v\n", err)
			return 1
		}
	}
	if *listFlag {
		if fs.NArg() != 0 {
			fmt.Fprintf(stderr, "adb-go forward --list: unexpected arguments %q\n\n", fs.Args())
			fs.Usage()
			return 2
		}
		return runForwardDaemonList(socketPath, jsonOutput, plainOutput, stdout, stderr)
	}
	if strings.TrimSpace(*removeFlag) != "" {
		if fs.NArg() != 0 {
			fmt.Fprintf(stderr, "adb-go forward --remove: unexpected arguments %q\n\n", fs.Args())
			fs.Usage()
			return 2
		}
		localAddr, err := parseForwardLocalTCP(*removeFlag)
		if err != nil {
			fmt.Fprintf(stderr, "adb-go forward: %v\n\n", err)
			fs.Usage()
			return 2
		}
		return runForwardDaemonRemove(socketPath, daemon.ForwardRemoveParams{Local: &daemon.ForwardLocalEndpoint{Network: "tcp", Address: localAddr}}, jsonOutput, out, stdout, stderr)
	}
	if strings.TrimSpace(*removeIDFlag) != "" {
		if fs.NArg() != 0 {
			fmt.Fprintf(stderr, "adb-go forward --remove-id: unexpected arguments %q\n\n", fs.Args())
			fs.Usage()
			return 2
		}
		return runForwardDaemonRemove(socketPath, daemon.ForwardRemoveParams{ID: strings.TrimSpace(*removeIDFlag)}, jsonOutput, out, stdout, stderr)
	}
	if *removeAllFlag {
		if fs.NArg() != 0 {
			fmt.Fprintf(stderr, "adb-go forward --remove-all: unexpected arguments %q\n\n", fs.Args())
			fs.Usage()
			return 2
		}
		return runForwardDaemonRemoveAll(socketPath, jsonOutput, out, stdout, stderr)
	}

	target, err := conn.target(fs)
	if err != nil {
		fmt.Fprintf(stderr, "adb-go forward: %v\n\n", err)
		fs.Usage()
		return 2
	}
	if fs.NArg() != 2 {
		fmt.Fprint(stderr, "adb-go forward: requires exactly LOCAL and REMOTE targets\n\n")
		fs.Usage()
		return 2
	}

	localAddr, err := parseForwardLocalTCP(fs.Arg(0))
	if err != nil {
		fmt.Fprintf(stderr, "adb-go forward: %v\n\n", err)
		fs.Usage()
		return 2
	}
	remote, err := parseForwardRemoteTCP(fs.Arg(1))
	if err != nil {
		fmt.Fprintf(stderr, "adb-go forward: %v\n\n", err)
		fs.Usage()
		return 2
	}

	if *backgroundFlag {
		if target.usb || len(target.auth) != 0 {
			fmt.Fprint(stderr, "adb-go forward: --background currently supports explicit unauthenticated TCP targets only; USB targets and --auth-key persistence are not implemented\n")
			return 2
		}
		targetAddr, err := normalizeForwardTargetTCPAddr(target.tcpAddr)
		if err != nil {
			fmt.Fprintf(stderr, "adb-go forward: %v\n\n", err)
			fs.Usage()
			return 2
		}
		return runForwardDaemonCreate(socketPath, localAddr, fs.Arg(1), targetAddr, *norebindFlag, jsonOutput, out, stdout, stderr)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	out.Verbosef("forward parsed local listener %s and remote service %s\n", localAddr, fs.Arg(1))
	client, err := connectTarget(ctx, "forward", target, out)
	if err != nil {
		printConnectError(stderr, "forward", target.description, err)
		return 1
	}
	defer client.Close()

	out.Verbosef("forward starting foreground listener and ADB service bridge\n")
	forward, err := startForward(ctx, client, localAddr, remote)
	if err != nil {
		printCommandError(stderr, "forward", err)
		return 1
	}
	defer forward.Close()
	out.Infof("Forwarding %s -> %s. Press Ctrl+C to stop.\n", forward.LocalAddr(), fs.Arg(1))

	if err := forward.Wait(); err != nil {
		printCommandError(stderr, "forward", err)
		return 1
	}
	return 0
}

func forwardDaemonSocketPath(configured string) (string, error) {
	if configured != "" {
		return configured, nil
	}
	return daemon.DefaultSocketPath()
}

func normalizeForwardTargetTCPAddr(addr string) (string, error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return "", fmt.Errorf("background forward TCP target address is empty")
	}
	if host, port, err := net.SplitHostPort(addr); err == nil {
		if port == "" {
			port = "5555"
		}
		return net.JoinHostPort(host, port), nil
	}
	if strings.Count(addr, ":") > 1 && !strings.HasPrefix(addr, "[") {
		return net.JoinHostPort(addr, "5555"), nil
	}
	if strings.Count(addr, ":") == 1 {
		parts := strings.SplitN(addr, ":", 2)
		if parts[1] != "" {
			return addr, nil
		}
		return net.JoinHostPort(parts[0], "5555"), nil
	}
	return net.JoinHostPort(addr, "5555"), nil
}

func runForwardDaemonCreate(socketPath, localAddr, remoteService, targetAddr string, norebind bool, jsonOutput bool, out outputPolicy, stdout, stderr io.Writer) int {
	out.Verbosef("forward sending daemon create request to %s for %s -> %s via tcp:%s (norebind=%t)\n", socketPath, localAddr, remoteService, targetAddr, norebind)
	params := daemon.ForwardCreateParams{
		Local:    daemon.ForwardLocalEndpoint{Network: "tcp", Address: localAddr},
		Remote:   daemon.ForwardRemoteEndpoint{Service: remoteService},
		Target:   daemon.ForwardTarget{Transport: "tcp", Address: targetAddr},
		Norebind: norebind,
	}
	resp, err := sendForwardDaemonRequest(socketPath, daemon.CommandForwardCreate, params)
	if err != nil || !resp.OK {
		printForwardDaemonError(stderr, "create background forward", socketPath, resp, err)
		return 1
	}
	var result daemon.ForwardCreateResult
	if err := decodeDaemonResult(resp.Result, &result); err != nil {
		fmt.Fprintf(stderr, "adb-go forward: decode daemon create response: %v\n", err)
		return 1
	}
	if jsonOutput {
		if err := writeJSON(stdout, result); err != nil {
			fmt.Fprintf(stderr, "adb-go forward: encode JSON: %v\n", err)
			return 1
		}
		return 0
	}
	out.Infof("Forward %s listening on %s -> %s via tcp:%s\n", result.Forward.ID, result.Forward.Local.Address, result.Forward.Remote.Service, result.Forward.Target.Address)
	out.Infoln("Lifecycle: in-memory daemon-owned forward; it is removed by --remove/--remove-all or adb-god shutdown.")
	return 0
}

func runForwardDaemonList(socketPath string, jsonOutput bool, plainOutput bool, stdout, stderr io.Writer) int {
	resp, err := sendForwardDaemonRequest(socketPath, daemon.CommandForwardList, nil)
	if err != nil || !resp.OK {
		printForwardDaemonError(stderr, "list background forwards", socketPath, resp, err)
		return 1
	}
	var result daemon.ForwardListResult
	if err := decodeDaemonResult(resp.Result, &result); err != nil {
		fmt.Fprintf(stderr, "adb-go forward: decode daemon list response: %v\n", err)
		return 1
	}
	if jsonOutput {
		if err := writeJSON(stdout, result); err != nil {
			fmt.Fprintf(stderr, "adb-go forward: encode JSON: %v\n", err)
			return 1
		}
		return 0
	}
	if len(result.Forwards) == 0 {
		fmt.Fprintln(stdout, "No daemon-owned forwards.")
		fmt.Fprintln(stdout, "Create one with: adb-go forward --background --addr HOST[:PORT] tcp:LOCAL_PORT tcp:REMOTE_PORT")
		return 0
	}
	rows := make([]tableRow, 0, len(result.Forwards))
	for _, f := range result.Forwards {
		lastError := f.LastError
		if lastError == "" {
			lastError = "-"
		}
		rows = append(rows, tableRow{f.ID, string(f.State), f.Local.Address, f.Remote.Service, f.Target.Transport + ":" + f.Target.Address, strconv.Itoa(f.ActiveConnections), lastError})
	}
	headers := []string{"ID", "STATE", "LOCAL", "REMOTE", "TARGET", "ACTIVE", "LAST_ERROR"}
	if plainOutput {
		if err := writePlainTable(stdout, headers, rows); err != nil {
			fmt.Fprintf(stderr, "adb-go forward: write table: %v\n", err)
			return 1
		}
		return 0
	}
	if err := writeAlignedTable(stdout, headers, rows); err != nil {
		fmt.Fprintf(stderr, "adb-go forward: write table: %v\n", err)
		return 1
	}
	return 0
}

func runForwardDaemonRemove(socketPath string, params daemon.ForwardRemoveParams, jsonOutput bool, out outputPolicy, stdout, stderr io.Writer) int {
	resp, err := sendForwardDaemonRequest(socketPath, daemon.CommandForwardRemove, params)
	if err != nil || !resp.OK {
		printForwardDaemonError(stderr, "remove background forward", socketPath, resp, err)
		return 1
	}
	var result daemon.ForwardRemoveResult
	if err := decodeDaemonResult(resp.Result, &result); err != nil {
		fmt.Fprintf(stderr, "adb-go forward: decode daemon remove response: %v\n", err)
		return 1
	}
	if jsonOutput {
		if err := writeJSON(stdout, result); err != nil {
			fmt.Fprintf(stderr, "adb-go forward: encode JSON: %v\n", err)
			return 1
		}
		return 0
	}
	out.Infof("Removed %d daemon-owned forward(s).\n", result.Removed)
	return 0
}

func runForwardDaemonRemoveAll(socketPath string, jsonOutput bool, out outputPolicy, stdout, stderr io.Writer) int {
	resp, err := sendForwardDaemonRequest(socketPath, daemon.CommandForwardRemoveAll, nil)
	if err != nil || !resp.OK {
		printForwardDaemonError(stderr, "remove all background forwards", socketPath, resp, err)
		return 1
	}
	var result daemon.ForwardRemoveAllResult
	if err := decodeDaemonResult(resp.Result, &result); err != nil {
		fmt.Fprintf(stderr, "adb-go forward: decode daemon remove-all response: %v\n", err)
		return 1
	}
	if jsonOutput {
		if err := writeJSON(stdout, result); err != nil {
			fmt.Fprintf(stderr, "adb-go forward: encode JSON: %v\n", err)
			return 1
		}
		return 0
	}
	out.Infof("Removed %d daemon-owned forward(s).\n", result.Removed)
	return 0
}

func sendForwardDaemonRequest(socketPath, command string, params any) (daemon.Response, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return clidaemon.Send(ctx, socketPath, command, params, sendDaemonRequest)
}

func decodeDaemonResult(result map[string]any, out any) error {
	return clidaemon.DecodeResult(result, out)
}

func printForwardDaemonError(stderr io.Writer, action, socketPath string, resp daemon.Response, err error) {
	if err != nil {
		fmt.Fprintf(stderr, "adb-go forward: cannot %s: adb-god is not running or socket is unavailable at %s: %v\n", action, socketPath, err)
		fmt.Fprintln(stderr, "Hint: start the daemon with `adb-go daemon service start` or inspect it with `adb-go daemon doctor`.")
		return
	}
	if resp.Error != nil {
		fmt.Fprintf(stderr, "adb-go forward: cannot %s: daemon error %s: %s\n", action, resp.Error.Code, resp.Error.Message)
		if resp.Error.Code == daemon.ErrorUnknownCommand {
			fmt.Fprintln(stderr, "Hint: adb-god is too old for persistent forwarding; upgrade adb-god or inspect it with `adb-go daemon doctor`.")
		}
		return
	}
	fmt.Fprintf(stderr, "adb-go forward: cannot %s: daemon returned an unsuccessful response\n", action)
}

func parseForwardLocalTCP(value string) (string, error) {
	port, err := parseForwardTCPPort("local", value)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("127.0.0.1:%d", port), nil
}

func parseForwardRemoteTCP(value string) (adb.ForwardTarget, error) {
	port, err := parseForwardTCPPort("remote", value)
	if err != nil {
		return adb.ForwardTarget{}, err
	}
	remote, err := adb.ForwardTCP(port)
	if err != nil {
		return adb.ForwardTarget{}, err
	}
	return remote, nil
}

func parseForwardTCPPort(kind, value string) (int, error) {
	if !strings.HasPrefix(value, "tcp:") {
		return 0, fmt.Errorf("unsupported %s forward target %q; only tcp:PORT is supported", kind, value)
	}
	portText := strings.TrimSpace(strings.TrimPrefix(value, "tcp:"))
	if portText == "" {
		return 0, fmt.Errorf("missing %s tcp port in %q", kind, value)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		return 0, fmt.Errorf("invalid %s tcp port in %q", kind, value)
	}
	if port < 0 || port > 65535 || (kind == "remote" && port == 0) {
		return 0, fmt.Errorf("%s tcp port %d out of range", kind, port)
	}
	return port, nil
}
