package custom

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/dector/adb-go/cmd/adb-go/internal/clidaemon"
	"github.com/dector/adb-go/internal/daemon"
)

const daemonUsage = `Usage:
  adb-go daemon [--socket PATH] COMMAND

Controls the local adb-god daemon over its Unix domain socket. This command
only exposes daemon process controls; adb-god does not persist devices,
transports, forwards, sessions, or authentication state yet.

Commands:
  doctor   Print read-only daemon socket and service diagnostics
  ping     Check whether adb-god responds to the control protocol
  status   Print basic adb-god process metadata
  stop     Request graceful adb-god shutdown
  service  Manage the adb-god system service integration

Socket path selection uses --socket when provided, otherwise ADB_GO_DAEMON_SOCKET,
then XDG_RUNTIME_DIR, then a per-user temporary directory. The service install
command writes ~/.config/systemd/user/adb-god.service by default and runs
systemctl --user daemon-reload followed by systemctl --user enable --now.
`

func runDaemon(args []string, stdout, stderr io.Writer) int {
	return runDaemonWithJSON(args, false, stdout, stderr)
}

func runDaemonWithJSON(args []string, jsonOutput bool, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("daemon", flag.ContinueOnError)
	fs.SetOutput(stderr)
	socketPathFlag := fs.String("socket", "", "absolute Unix domain socket path")
	jsonFlag := fs.Bool("json", jsonOutput, "print machine-readable JSON for supported daemon commands")
	fs.Usage = func() { fmt.Fprint(stderr, daemonUsage) }
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() == 0 {
		fmt.Fprint(stderr, "adb-go daemon: requires COMMAND\n\n")
		fs.Usage()
		return 2
	}

	socketPath := strings.TrimSpace(*socketPathFlag)
	if socketPath == "" {
		var err error
		socketPath, err = daemon.DefaultSocketPath()
		if err != nil {
			fmt.Fprintf(stderr, "adb-go daemon: %v\n", err)
			return 1
		}
	}

	jsonOutput = *jsonFlag
	command := fs.Arg(0)
	commandArgs := fs.Args()[1:]
	if command == "service" {
		return runDaemonService(commandArgs, socketPath, stdout, stderr)
	}
	if command == "doctor" {
		return runDaemonDoctorWithJSON(commandArgs, socketPath, jsonOutput, stdout, stderr)
	}
	switch command {
	case daemon.CommandPing, daemon.CommandStatus, "stop":
		if len(commandArgs) != 0 {
			fmt.Fprintf(stderr, "adb-go daemon %s: unexpected arguments %q\n\n", command, commandArgs)
			fs.Usage()
			return 2
		}
	default:
		fmt.Fprintf(stderr, "adb-go daemon: unknown daemon command %q\n\n", command)
		fs.Usage()
		return 2
	}

	protocolCommand := command
	if command == "stop" {
		protocolCommand = daemon.CommandShutdown
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	resp, err := clidaemon.Send(ctx, socketPath, protocolCommand, nil, sendDaemonRequest)
	if err != nil {
		fmt.Fprintf(stderr, "adb-go daemon %s: daemon is not running or socket is unavailable at %s: %v\n", command, socketPath, err)
		return 1
	}
	if !resp.OK {
		if resp.Error != nil {
			fmt.Fprintf(stderr, "adb-go daemon %s: daemon error %s: %s\n", command, resp.Error.Code, resp.Error.Message)
		} else {
			fmt.Fprintf(stderr, "adb-go daemon %s: daemon returned an unsuccessful response\n", command)
		}
		return 1
	}

	switch command {
	case daemon.CommandPing:
		if jsonOutput {
			if err := writeJSON(stdout, map[string]string{"result": "pong"}); err != nil {
				fmt.Fprintf(stderr, "adb-go daemon ping: encode JSON: %v\n", err)
				return 1
			}
			return 0
		}
		fmt.Fprintln(stdout, "pong")
	case daemon.CommandStatus:
		if jsonOutput {
			if err := writeJSON(stdout, resp.Result); err != nil {
				fmt.Fprintf(stderr, "adb-go daemon status: encode JSON: %v\n", err)
				return 1
			}
			return 0
		}
		printDaemonStatus(stdout, resp.Result)
	case "stop":
		if jsonOutput {
			if err := writeJSON(stdout, map[string]string{"state": "shutting_down"}); err != nil {
				fmt.Fprintf(stderr, "adb-go daemon stop: encode JSON: %v\n", err)
				return 1
			}
			return 0
		}
		fmt.Fprintln(stdout, "adb-god shutting down")
	}
	return 0
}

func printDaemonStatus(stdout io.Writer, result map[string]any) {
	fmt.Fprintf(stdout, "state: %v\n", result["state"])
	fmt.Fprintf(stdout, "pid: %v\n", result["pid"])
	fmt.Fprintf(stdout, "socketPath: %v\n", result["socketPath"])
	fmt.Fprintf(stdout, "protocolVersion: %v\n", result["protocolVersion"])
	fmt.Fprintf(stdout, "uptimeMillis: %v\n", result["uptimeMillis"])
	if diagnostics, ok := daemonForwardDiagnostics(result); ok {
		fmt.Fprintf(stdout, "forwardTotal: %d\n", diagnostics.Total)
		fmt.Fprintf(stdout, "forwardListening: %d\n", diagnostics.Listening)
		fmt.Fprintf(stdout, "forwardDegraded: %d\n", diagnostics.Degraded)
		fmt.Fprintf(stdout, "forwardActiveConnections: %d\n", diagnostics.ActiveConnections)
	}
	if diagnostics, ok := daemonReverseDiagnostics(result); ok {
		fmt.Fprintf(stdout, "reverseTotal: %d\n", diagnostics.Total)
		fmt.Fprintf(stdout, "reverseListening: %d\n", diagnostics.Listening)
		fmt.Fprintf(stdout, "reverseDegraded: %d\n", diagnostics.Degraded)
		fmt.Fprintf(stdout, "reverseActiveConnections: %d\n", diagnostics.ActiveConnections)
	}
}

func daemonForwardDiagnostics(result map[string]any) (daemon.ForwardDiagnostics, bool) {
	raw, ok := result["forwardDiagnostics"]
	if !ok || raw == nil {
		return daemon.ForwardDiagnostics{}, false
	}
	body, err := json.Marshal(raw)
	if err != nil {
		return daemon.ForwardDiagnostics{}, false
	}
	var diagnostics daemon.ForwardDiagnostics
	if err := json.Unmarshal(body, &diagnostics); err != nil {
		return daemon.ForwardDiagnostics{}, false
	}
	return diagnostics, true
}

func daemonReverseDiagnostics(result map[string]any) (daemon.ReverseDiagnostics, bool) {
	raw, ok := result["reverseDiagnostics"]
	if !ok || raw == nil {
		return daemon.ReverseDiagnostics{}, false
	}
	body, err := json.Marshal(raw)
	if err != nil {
		return daemon.ReverseDiagnostics{}, false
	}
	var diagnostics daemon.ReverseDiagnostics
	if err := json.Unmarshal(body, &diagnostics); err != nil {
		return daemon.ReverseDiagnostics{}, false
	}
	return diagnostics, true
}

const daemonDoctorUsage = `Usage:
  adb-go daemon [--socket PATH] doctor [--systemctl PATH]

Prints read-only diagnostics for the local adb-god daemon. The command reports
which socket path adb-go resolved, whether that path exists, whether a
compatible daemon responds to the socket protocol, and, on Linux when systemctl
is available, adb-god.service active/enabled state.
`

func runDaemonDoctor(args []string, socketPath string, stdout, stderr io.Writer) int {
	return runDaemonDoctorWithJSON(args, socketPath, false, stdout, stderr)
}

func runDaemonDoctorWithJSON(args []string, socketPath string, jsonOutput bool, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("daemon doctor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	systemctlFlag := fs.String("systemctl", "systemctl", "systemctl binary path")
	jsonFlag := fs.Bool("json", jsonOutput, "print machine-readable JSON")
	fs.Usage = func() { fmt.Fprint(stderr, daemonDoctorUsage) }
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(stderr, "adb-go daemon doctor: unexpected arguments %q\n\n", fs.Args())
		fs.Usage()
		return 2
	}

	jsonOutput = *jsonFlag
	hints := []string{}
	diagnostics := map[string]any{"socketPath": socketPath}
	if !jsonOutput {
		fmt.Fprintf(stdout, "socketPath: %s\n", socketPath)
	}
	if info, err := os.Lstat(socketPath); err == nil {
		diagnostics["socketExists"] = true
		if !jsonOutput {
			fmt.Fprintln(stdout, "socketExists: true")
		}
		if info.Mode()&os.ModeSocket == 0 {
			diagnostics["socketType"] = info.Mode().Type().String()
			if !jsonOutput {
				fmt.Fprintf(stdout, "socketType: %s\n", info.Mode().Type())
			}
			hints = append(hints, "The socket path exists but is not a Unix socket; inspect it before starting adb-god with this path.")
		} else {
			diagnostics["socketType"] = "unix"
			if !jsonOutput {
				fmt.Fprintln(stdout, "socketType: unix")
			}
		}
	} else if errors.Is(err, os.ErrNotExist) {
		diagnostics["socketExists"] = false
		if !jsonOutput {
			fmt.Fprintln(stdout, "socketExists: false")
		}
		hints = append(hints, "No daemon socket exists at this path; start adb-god with a matching -socket path, or run adb-go daemon service install then adb-go daemon service start on supported platforms.")
	} else {
		diagnostics["socketExists"] = "unknown"
		diagnostics["socketError"] = err.Error()
		if !jsonOutput {
			fmt.Fprintf(stdout, "socketExists: unknown (%v)\n", err)
		}
		hints = append(hints, "adb-go could not inspect the socket path; check parent directory permissions.")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	resp, err := clidaemon.Send(ctx, socketPath, daemon.CommandStatus, nil, sendDaemonRequest)
	if err != nil {
		diagnostics["daemonProtocol"] = "not_responding"
		diagnostics["daemonProtocolError"] = err.Error()
		if !jsonOutput {
			fmt.Fprintf(stdout, "daemonProtocol: not responding (%v)\n", err)
		}
		hints = append(hints, "No compatible adb-god daemon answered the control protocol; verify the daemon process and socket path match.")
	} else if !resp.OK {
		diagnostics["daemonProtocol"] = "error"
		if !jsonOutput {
			fmt.Fprintln(stdout, "daemonProtocol: error")
		}
		if resp.Error != nil {
			diagnostics["daemonError"] = resp.Error
			if !jsonOutput {
				fmt.Fprintf(stdout, "daemonError: %s: %s\n", resp.Error.Code, resp.Error.Message)
			}
		}
		hints = append(hints, "The daemon socket answered but returned an error; check that adb-go and adb-god are compatible builds.")
	} else {
		diagnostics["daemonProtocol"] = "responding"
		diagnostics["daemonState"] = resp.Result["state"]
		diagnostics["daemonProtocolVersion"] = resp.Result["protocolVersion"]
		if !jsonOutput {
			fmt.Fprintln(stdout, "daemonProtocol: responding")
			fmt.Fprintf(stdout, "daemonState: %v\n", resp.Result["state"])
			fmt.Fprintf(stdout, "daemonProtocolVersion: %v\n", resp.Result["protocolVersion"])
		}
		if forwardDiagnostics, ok := daemonForwardDiagnostics(resp.Result); ok {
			diagnostics["forwardDiagnostics"] = forwardDiagnostics
			if !jsonOutput {
				fmt.Fprintf(stdout, "forwardTotal: %d\n", forwardDiagnostics.Total)
				fmt.Fprintf(stdout, "forwardListening: %d\n", forwardDiagnostics.Listening)
				fmt.Fprintf(stdout, "forwardDegraded: %d\n", forwardDiagnostics.Degraded)
				fmt.Fprintf(stdout, "forwardActiveConnections: %d\n", forwardDiagnostics.ActiveConnections)
			}
			if forwardDiagnostics.Degraded > 0 {
				hints = append(hints, "One or more daemon-owned forwards are degraded; run adb-go forward --list to see the mapping IDs, targets, and last setup errors.")
			}
		}
		if reverseDiagnostics, ok := daemonReverseDiagnostics(resp.Result); ok {
			diagnostics["reverseDiagnostics"] = reverseDiagnostics
			if !jsonOutput {
				fmt.Fprintf(stdout, "reverseTotal: %d\n", reverseDiagnostics.Total)
				fmt.Fprintf(stdout, "reverseListening: %d\n", reverseDiagnostics.Listening)
				fmt.Fprintf(stdout, "reverseDegraded: %d\n", reverseDiagnostics.Degraded)
				fmt.Fprintf(stdout, "reverseActiveConnections: %d\n", reverseDiagnostics.ActiveConnections)
			}
			if reverseDiagnostics.Degraded > 0 {
				hints = append(hints, "One or more daemon-owned reverses are degraded; run adb-go reverse --list to see the mapping IDs, targets, and last setup errors.")
			}
		}
	}

	if runtime.GOOS == "linux" {
		active, enabled, ok, hint := daemonDoctorSystemdStatus(strings.TrimSpace(*systemctlFlag))
		if ok {
			diagnostics["systemdActive"] = active
			diagnostics["systemdEnabled"] = enabled
			if !jsonOutput {
				fmt.Fprintf(stdout, "systemdActive: %s\n", active)
				fmt.Fprintf(stdout, "systemdEnabled: %s\n", enabled)
			}
			if active != "active" {
				hints = append(hints, "adb-god.service is not active; run adb-go daemon service start to start the installed user service.")
			}
			if enabled != "enabled" {
				hints = append(hints, "adb-god.service is not enabled; run adb-go daemon service install or systemctl --user enable adb-god.service to start it automatically.")
			}
		} else {
			diagnostics["systemdUserService"] = "unavailable"
			if !jsonOutput {
				fmt.Fprintln(stdout, "systemdUserService: unavailable")
			}
			if hint != "" {
				hints = append(hints, hint)
			}
		}
	} else {
		diagnostics["systemdUserService"] = "unsupported"
		if !jsonOutput {
			fmt.Fprintln(stdout, "systemdUserService: unsupported on this platform")
		}
	}

	diagnostics["hints"] = hints
	if jsonOutput {
		if err := writeJSON(stdout, diagnostics); err != nil {
			fmt.Fprintf(stderr, "adb-go daemon doctor: encode JSON: %v\n", err)
			return 1
		}
		return 0
	}
	if len(hints) == 0 {
		fmt.Fprintln(stdout, "hints: none")
	} else {
		fmt.Fprintln(stdout, "hints:")
		for _, hint := range hints {
			fmt.Fprintf(stdout, "  - %s\n", hint)
		}
	}
	return 0
}

func daemonDoctorSystemdStatus(systemctl string) (active, enabled string, ok bool, hint string) {
	if systemctl == "" {
		systemctl = "systemctl"
	}
	if !systemctlAvailable(systemctl) {
		return "", "", false, "systemctl is not available on PATH; install systemd tools or pass --systemctl PATH to inspect adb-god.service."
	}
	active, activeErr := systemctlUserOutput(systemctl, "is-active", "adb-god.service")
	enabled, enabledErr := systemctlUserOutput(systemctl, "is-enabled", "adb-god.service")
	if active == "" && activeErr != nil {
		return "", "", false, fmt.Sprintf("systemctl could not read adb-god.service active state: %v", activeErr)
	}
	if enabled == "" && enabledErr != nil {
		return "", "", false, fmt.Sprintf("systemctl could not read adb-god.service enabled state: %v", enabledErr)
	}
	return active, enabled, true, ""
}

func systemctlAvailable(systemctl string) bool {
	if strings.ContainsRune(systemctl, os.PathSeparator) {
		info, err := os.Stat(systemctl)
		return err == nil && !info.IsDir()
	}
	_, err := exec.LookPath(systemctl)
	return err == nil
}
