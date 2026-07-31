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

	"github.com/dector/adb-go/cmd/adb-go/internal/cliserver"
	"github.com/dector/adb-go/internal/server"
)

const serverUsage = `Usage:
  adb-go server [--socket PATH] COMMAND

Controls the local adb-gos server over its Unix domain socket. This command
only exposes server process controls; adb-gos does not persist devices,
transports, forwards, sessions, or authentication state yet.

Commands:
  doctor   Print read-only server socket and service diagnostics
  ping     Check whether adb-gos responds to the control protocol
  status   Print basic adb-gos process metadata
  stop     Request graceful adb-gos shutdown
  service  Manage the adb-gos system service integration

Socket path selection uses --socket when provided, otherwise ADB_GO_SERVER_SOCKET,
then XDG_RUNTIME_DIR, then a per-user temporary directory. The service install
command writes ~/.config/systemd/user/adb-gos.service by default and runs
systemctl --user daemon-reload followed by systemctl --user enable --now.
`

func runServer(args []string, stdout, stderr io.Writer) int {
	return runServerWithJSON(args, false, stdout, stderr)
}

func runServerWithJSON(args []string, jsonOutput bool, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("server", flag.ContinueOnError)
	fs.SetOutput(stderr)
	socketPathFlag := fs.String("socket", "", "absolute Unix domain socket path")
	jsonFlag := fs.Bool("json", jsonOutput, "print machine-readable JSON for supported server commands")
	fs.Usage = func() { fmt.Fprint(stderr, serverUsage) }
	if err := fs.Parse(args); err != nil {
		return 2
	}
	command, commandArgs, ok := splitFlagSetCommand(fs)
	if !ok {
		fmt.Fprint(stderr, "adb-go server: requires COMMAND\n\n")
		fs.Usage()
		return 2
	}

	socketPath := strings.TrimSpace(*socketPathFlag)
	if socketPath == "" {
		var err error
		socketPath, err = server.DefaultSocketPath()
		if err != nil {
			fmt.Fprintf(stderr, "adb-go server: %v\n", err)
			return 1
		}
	}

	jsonOutput = *jsonFlag
	if command == "service" {
		return runServerService(commandArgs, socketPath, stdout, stderr)
	}
	if command == "doctor" {
		return runServerDoctorWithJSON(commandArgs, socketPath, jsonOutput, stdout, stderr)
	}
	switch command {
	case server.CommandPing, server.CommandStatus, "stop":
		if len(commandArgs) != 0 {
			fmt.Fprintf(stderr, "adb-go server %s: unexpected arguments %q\n\n", command, commandArgs)
			fs.Usage()
			return 2
		}
	default:
		fmt.Fprintf(stderr, "adb-go server: unknown server command %q\n\n", command)
		fs.Usage()
		return 2
	}

	protocolCommand := command
	if command == "stop" {
		protocolCommand = server.CommandShutdown
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	resp, err := cliserver.Send(ctx, socketPath, protocolCommand, nil, sendServerRequest)
	if err != nil {
		fmt.Fprintf(stderr, "adb-go server %s: server is not running or socket is unavailable at %s: %v\n", command, socketPath, err)
		return 1
	}
	if !resp.OK {
		if resp.Error != nil {
			fmt.Fprintf(stderr, "adb-go server %s: server error %s: %s\n", command, resp.Error.Code, resp.Error.Message)
		} else {
			fmt.Fprintf(stderr, "adb-go server %s: server returned an unsuccessful response\n", command)
		}
		return 1
	}

	switch command {
	case server.CommandPing:
		if jsonOutput {
			if err := writeJSON(stdout, map[string]string{"result": "pong"}); err != nil {
				fmt.Fprintf(stderr, "adb-go server ping: encode JSON: %v\n", err)
				return 1
			}
			return 0
		}
		fmt.Fprintln(stdout, "pong")
	case server.CommandStatus:
		if jsonOutput {
			if err := writeJSON(stdout, resp.Result); err != nil {
				fmt.Fprintf(stderr, "adb-go server status: encode JSON: %v\n", err)
				return 1
			}
			return 0
		}
		printServerStatus(stdout, resp.Result)
	case "stop":
		if jsonOutput {
			if err := writeJSON(stdout, map[string]string{"state": "shutting_down"}); err != nil {
				fmt.Fprintf(stderr, "adb-go server stop: encode JSON: %v\n", err)
				return 1
			}
			return 0
		}
		fmt.Fprintln(stdout, "adb-gos shutting down")
	}
	return 0
}

func printServerStatus(stdout io.Writer, result map[string]any) {
	fmt.Fprintf(stdout, "state: %v\n", result["state"])
	fmt.Fprintf(stdout, "pid: %v\n", result["pid"])
	fmt.Fprintf(stdout, "socketPath: %v\n", result["socketPath"])
	fmt.Fprintf(stdout, "protocolVersion: %v\n", result["protocolVersion"])
	fmt.Fprintf(stdout, "uptimeMillis: %v\n", result["uptimeMillis"])
	if diagnostics, ok := serverForwardDiagnostics(result); ok {
		fmt.Fprintf(stdout, "forwardTotal: %d\n", diagnostics.Total)
		fmt.Fprintf(stdout, "forwardListening: %d\n", diagnostics.Listening)
		fmt.Fprintf(stdout, "forwardDegraded: %d\n", diagnostics.Degraded)
		fmt.Fprintf(stdout, "forwardActiveConnections: %d\n", diagnostics.ActiveConnections)
	}
	if diagnostics, ok := serverReverseDiagnostics(result); ok {
		fmt.Fprintf(stdout, "reverseTotal: %d\n", diagnostics.Total)
		fmt.Fprintf(stdout, "reverseListening: %d\n", diagnostics.Listening)
		fmt.Fprintf(stdout, "reverseDegraded: %d\n", diagnostics.Degraded)
		fmt.Fprintf(stdout, "reverseActiveConnections: %d\n", diagnostics.ActiveConnections)
	}
}

func serverForwardDiagnostics(result map[string]any) (server.ForwardDiagnostics, bool) {
	raw, ok := result["forwardDiagnostics"]
	if !ok || raw == nil {
		return server.ForwardDiagnostics{}, false
	}
	body, err := json.Marshal(raw)
	if err != nil {
		return server.ForwardDiagnostics{}, false
	}
	var diagnostics server.ForwardDiagnostics
	if err := json.Unmarshal(body, &diagnostics); err != nil {
		return server.ForwardDiagnostics{}, false
	}
	return diagnostics, true
}

func serverReverseDiagnostics(result map[string]any) (server.ReverseDiagnostics, bool) {
	raw, ok := result["reverseDiagnostics"]
	if !ok || raw == nil {
		return server.ReverseDiagnostics{}, false
	}
	body, err := json.Marshal(raw)
	if err != nil {
		return server.ReverseDiagnostics{}, false
	}
	var diagnostics server.ReverseDiagnostics
	if err := json.Unmarshal(body, &diagnostics); err != nil {
		return server.ReverseDiagnostics{}, false
	}
	return diagnostics, true
}

const serverDoctorUsage = `Usage:
  adb-go server [--socket PATH] doctor [--systemctl PATH]

Prints read-only diagnostics for the local adb-gos server. The command reports
which socket path adb-go resolved, whether that path exists, whether a
compatible server responds to the socket protocol, and, on Linux when systemctl
is available, adb-gos.service active/enabled state.
`

func runServerDoctor(args []string, socketPath string, stdout, stderr io.Writer) int {
	return runServerDoctorWithJSON(args, socketPath, false, stdout, stderr)
}

func runServerDoctorWithJSON(args []string, socketPath string, jsonOutput bool, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("server doctor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	systemctlFlag := fs.String("systemctl", "systemctl", "systemctl binary path")
	jsonFlag := fs.Bool("json", jsonOutput, "print machine-readable JSON")
	fs.Usage = func() { fmt.Fprint(stderr, serverDoctorUsage) }
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(stderr, "adb-go server doctor: unexpected arguments %q\n\n", fs.Args())
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
			hints = append(hints, "The socket path exists but is not a Unix socket; inspect it before starting adb-gos with this path.")
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
		hints = append(hints, "No server socket exists at this path; start adb-gos with a matching -socket path, or run adb-go server service install then adb-go server service start on supported platforms.")
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
	resp, err := cliserver.Send(ctx, socketPath, server.CommandStatus, nil, sendServerRequest)
	if err != nil {
		diagnostics["serverProtocol"] = "not_responding"
		diagnostics["serverProtocolError"] = err.Error()
		if !jsonOutput {
			fmt.Fprintf(stdout, "serverProtocol: not responding (%v)\n", err)
		}
		hints = append(hints, "No compatible adb-gos server answered the control protocol; verify the server process and socket path match.")
	} else if !resp.OK {
		diagnostics["serverProtocol"] = "error"
		if !jsonOutput {
			fmt.Fprintln(stdout, "serverProtocol: error")
		}
		if resp.Error != nil {
			diagnostics["serverError"] = resp.Error
			if !jsonOutput {
				fmt.Fprintf(stdout, "serverError: %s: %s\n", resp.Error.Code, resp.Error.Message)
			}
		}
		hints = append(hints, "The server socket answered but returned an error; check that adb-go and adb-gos are compatible builds.")
	} else {
		diagnostics["serverProtocol"] = "responding"
		diagnostics["serverState"] = resp.Result["state"]
		diagnostics["serverProtocolVersion"] = resp.Result["protocolVersion"]
		if !jsonOutput {
			fmt.Fprintln(stdout, "serverProtocol: responding")
			fmt.Fprintf(stdout, "serverState: %v\n", resp.Result["state"])
			fmt.Fprintf(stdout, "serverProtocolVersion: %v\n", resp.Result["protocolVersion"])
		}
		if forwardDiagnostics, ok := serverForwardDiagnostics(resp.Result); ok {
			diagnostics["forwardDiagnostics"] = forwardDiagnostics
			if !jsonOutput {
				fmt.Fprintf(stdout, "forwardTotal: %d\n", forwardDiagnostics.Total)
				fmt.Fprintf(stdout, "forwardListening: %d\n", forwardDiagnostics.Listening)
				fmt.Fprintf(stdout, "forwardDegraded: %d\n", forwardDiagnostics.Degraded)
				fmt.Fprintf(stdout, "forwardActiveConnections: %d\n", forwardDiagnostics.ActiveConnections)
			}
			if forwardDiagnostics.Degraded > 0 {
				hints = append(hints, "One or more server-owned forwards are degraded; run adb-go forward --list to see the mapping IDs, targets, and last setup errors.")
			}
		}
		if reverseDiagnostics, ok := serverReverseDiagnostics(resp.Result); ok {
			diagnostics["reverseDiagnostics"] = reverseDiagnostics
			if !jsonOutput {
				fmt.Fprintf(stdout, "reverseTotal: %d\n", reverseDiagnostics.Total)
				fmt.Fprintf(stdout, "reverseListening: %d\n", reverseDiagnostics.Listening)
				fmt.Fprintf(stdout, "reverseDegraded: %d\n", reverseDiagnostics.Degraded)
				fmt.Fprintf(stdout, "reverseActiveConnections: %d\n", reverseDiagnostics.ActiveConnections)
			}
			if reverseDiagnostics.Degraded > 0 {
				hints = append(hints, "One or more server-owned reverses are degraded; run adb-go reverse --list to see the mapping IDs, targets, and last setup errors.")
			}
		}
	}

	if runtime.GOOS == "linux" {
		active, enabled, ok, hint := serverDoctorSystemdStatus(strings.TrimSpace(*systemctlFlag))
		if ok {
			diagnostics["systemdActive"] = active
			diagnostics["systemdEnabled"] = enabled
			if !jsonOutput {
				fmt.Fprintf(stdout, "systemdActive: %s\n", active)
				fmt.Fprintf(stdout, "systemdEnabled: %s\n", enabled)
			}
			if active != "active" {
				hints = append(hints, "adb-gos.service is not active; run adb-go server service start to start the installed user service.")
			}
			if enabled != "enabled" {
				hints = append(hints, "adb-gos.service is not enabled; run adb-go server service install or systemctl --user enable adb-gos.service to start it automatically.")
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
			fmt.Fprintf(stderr, "adb-go server doctor: encode JSON: %v\n", err)
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

func serverDoctorSystemdStatus(systemctl string) (active, enabled string, ok bool, hint string) {
	if systemctl == "" {
		systemctl = "systemctl"
	}
	if !systemctlAvailable(systemctl) {
		return "", "", false, "systemctl is not available on PATH; install systemd tools or pass --systemctl PATH to inspect adb-gos.service."
	}
	active, activeErr := systemctlUserOutput(systemctl, "is-active", "adb-gos.service")
	enabled, enabledErr := systemctlUserOutput(systemctl, "is-enabled", "adb-gos.service")
	if active == "" && activeErr != nil {
		return "", "", false, fmt.Sprintf("systemctl could not read adb-gos.service active state: %v", activeErr)
	}
	if enabled == "" && enabledErr != nil {
		return "", "", false, fmt.Sprintf("systemctl could not read adb-gos.service enabled state: %v", enabledErr)
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
