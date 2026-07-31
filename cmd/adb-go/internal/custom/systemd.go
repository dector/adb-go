package custom

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

const daemonServiceUsage = `Usage:
  adb-go daemon [--socket PATH] service COMMAND

Manages adb-god's host service-manager integration. Currently implemented:

  install    Install and start adb-god as a systemd user service on Linux
  reinstall  Rewrite, reload, enable, and restart the systemd user service
  start      Start adb-god.service with systemd --user
  stop       Stop adb-god.service with systemd --user
  restart    Restart adb-god.service with systemd --user
  status     Report adb-god.service active/enabled state with systemd --user
  logs       Show recent adb-god.service logs with journalctl --user
  uninstall  Disable, stop, and remove the systemd user service

These host service commands are separate from daemon protocol commands like
ping/status/shutdown.
`

const daemonServiceInstallUsage = `Usage:
  adb-go daemon [--socket PATH] service install [--adb-god PATH] [--unit-dir DIR] [--systemctl PATH] [--no-enable]

Installs adb-god as a systemd user service on Linux. The command writes an
adb-god.service unit for the current user. Unless --no-enable is set, it then
reloads the user systemd manager and enables/starts the service with:

  systemctl --user daemon-reload
  systemctl --user enable --now adb-god.service
`

const daemonServiceReinstallUsage = `Usage:
  adb-go daemon [--socket PATH] service reinstall [--adb-god PATH] [--unit-dir DIR] [--systemctl PATH]

Rewrites the adb-god systemd user unit on Linux, reloads the user systemd
manager, enables the service, and restarts adb-god.service. Use reinstall after
changing the adb-god binary path, changing the daemon socket path, or upgrading
a locally built daemon binary whose service unit should be refreshed.
`

const daemonServiceLifecycleUsage = `Usage:
  adb-go daemon service COMMAND [--systemctl PATH]

Starts, stops, or restarts adb-god.service using systemd --user. COMMAND must be
start, stop, or restart.
`

const daemonServiceStatusUsage = `Usage:
  adb-go daemon service status [--systemctl PATH]

Reports adb-god.service state from the current user's systemd manager. This is
host service-manager state, not the live daemon socket protocol status.
`

const daemonServiceLogsUsage = `Usage:
  adb-go daemon service logs [--journalctl PATH] [--lines N] [--follow]

Shows adb-god.service logs from the current user's systemd journal. By default,
it prints the most recent 100 entries without opening a pager. Pass --lines N to
choose a different number of recent entries, or --follow to keep streaming new
entries after the initial output.
`

const daemonServiceUninstallUsage = `Usage:
  adb-go daemon service uninstall [--unit-dir DIR] [--systemctl PATH] [--keep-unit]

Disables and stops adb-god.service using systemd --user, removes the systemd
user unit file, and reloads the user systemd manager. Use --keep-unit to leave
the unit file in place after disabling/stopping the service.
`

func runDaemonService(args []string, socketPath string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("daemon service", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, daemonServiceUsage) }
	if err := fs.Parse(args); err != nil {
		return 2
	}
	command, commandArgs, ok := splitFlagSetCommand(fs)
	if !ok {
		fmt.Fprint(stderr, "adb-go daemon service: requires COMMAND\n\n")
		fs.Usage()
		return 2
	}

	switch command {
	case "install":
		return runDaemonServiceInstall(commandArgs, socketPath, stdout, stderr)
	case "reinstall":
		return runDaemonServiceReinstall(commandArgs, socketPath, stdout, stderr)
	case "start", "stop", "restart":
		return runDaemonServiceLifecycle(command, commandArgs, stdout, stderr)
	case "status":
		return runDaemonServiceStatus(commandArgs, stdout, stderr)
	case "logs":
		return runDaemonServiceLogs(commandArgs, stdout, stderr)
	case "uninstall":
		return runDaemonServiceUninstall(commandArgs, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "adb-go daemon service: unknown service command %q\n\n", command)
		fs.Usage()
		return 2
	}
}

func runDaemonServiceInstall(args []string, socketPath string, stdout, stderr io.Writer) int {
	if runtime.GOOS != "linux" {
		fmt.Fprintln(stderr, "adb-go daemon service install: systemd user services are supported on Linux only")
		return 1
	}
	fs := flag.NewFlagSet("daemon service install", flag.ContinueOnError)
	fs.SetOutput(stderr)
	adbGodPathFlag := fs.String("adb-god", "", "absolute path to the adb-god binary")
	unitDirFlag := fs.String("unit-dir", "", "systemd user unit directory")
	systemctlFlag := fs.String("systemctl", "systemctl", "systemctl binary path")
	noEnableFlag := fs.Bool("no-enable", false, "write the unit but do not run systemctl")
	fs.Usage = func() { fmt.Fprint(stderr, daemonServiceInstallUsage) }
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(stderr, "adb-go daemon service install: unexpected arguments %q\n\n", fs.Args())
		fs.Usage()
		return 2
	}

	adbGodPath, err := resolveADBGodPath(strings.TrimSpace(*adbGodPathFlag))
	if err != nil {
		fmt.Fprintf(stderr, "adb-go daemon service install: %v\n", err)
		return 1
	}
	unitDir, err := systemdUserUnitDir(strings.TrimSpace(*unitDirFlag))
	if err != nil {
		fmt.Fprintf(stderr, "adb-go daemon service install: %v\n", err)
		return 1
	}
	unitPath, err := writeDaemonServiceUnit(unitDir, adbGodPath, socketPath)
	if err != nil {
		fmt.Fprintf(stderr, "adb-go daemon service install: %v\n", err)
		return 1
	}

	if !*noEnableFlag {
		if code := runSystemctlUser(stderr, *systemctlFlag, "daemon-reload"); code != 0 {
			return code
		}
		if code := runSystemctlUser(stderr, *systemctlFlag, "enable", "--now", "adb-god.service"); code != 0 {
			return code
		}
	}

	fmt.Fprintf(stdout, "installed %s\n", unitPath)
	if *noEnableFlag {
		fmt.Fprintln(stdout, "systemctl enable/start skipped")
	} else {
		fmt.Fprintln(stdout, "adb-god.service enabled and started for the current user")
	}
	return 0
}

func runDaemonServiceReinstall(args []string, socketPath string, stdout, stderr io.Writer) int {
	if runtime.GOOS != "linux" {
		fmt.Fprintln(stderr, "adb-go daemon service reinstall: systemd user services are supported on Linux only")
		return 1
	}
	fs := flag.NewFlagSet("daemon service reinstall", flag.ContinueOnError)
	fs.SetOutput(stderr)
	adbGodPathFlag := fs.String("adb-god", "", "absolute path to the adb-god binary")
	unitDirFlag := fs.String("unit-dir", "", "systemd user unit directory")
	systemctlFlag := fs.String("systemctl", "systemctl", "systemctl binary path")
	fs.Usage = func() { fmt.Fprint(stderr, daemonServiceReinstallUsage) }
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(stderr, "adb-go daemon service reinstall: unexpected arguments %q\n\n", fs.Args())
		fs.Usage()
		return 2
	}

	adbGodPath, err := resolveADBGodPath(strings.TrimSpace(*adbGodPathFlag))
	if err != nil {
		fmt.Fprintf(stderr, "adb-go daemon service reinstall: %v\n", err)
		return 1
	}
	unitDir, err := systemdUserUnitDir(strings.TrimSpace(*unitDirFlag))
	if err != nil {
		fmt.Fprintf(stderr, "adb-go daemon service reinstall: %v\n", err)
		return 1
	}
	unitPath, err := writeDaemonServiceUnit(unitDir, adbGodPath, socketPath)
	if err != nil {
		fmt.Fprintf(stderr, "adb-go daemon service reinstall: %v\n", err)
		return 1
	}

	if code := runSystemctlUser(stderr, *systemctlFlag, "daemon-reload"); code != 0 {
		return code
	}
	if code := runSystemctlUser(stderr, *systemctlFlag, "enable", "adb-god.service"); code != 0 {
		return code
	}
	if code := runSystemctlUser(stderr, *systemctlFlag, "restart", "adb-god.service"); code != 0 {
		return code
	}

	fmt.Fprintf(stdout, "reinstalled %s\n", unitPath)
	fmt.Fprintln(stdout, "adb-god.service enabled and restarted for the current user")
	return 0
}

func runDaemonServiceLifecycle(command string, args []string, stdout, stderr io.Writer) int {
	if runtime.GOOS != "linux" {
		fmt.Fprintf(stderr, "adb-go daemon service %s: systemd user services are supported on Linux only\n", command)
		return 1
	}
	fs := flag.NewFlagSet("daemon service "+command, flag.ContinueOnError)
	fs.SetOutput(stderr)
	systemctlFlag := fs.String("systemctl", "systemctl", "systemctl binary path")
	fs.Usage = func() { fmt.Fprint(stderr, daemonServiceLifecycleUsage) }
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(stderr, "adb-go daemon service %s: unexpected arguments %q\n\n", command, fs.Args())
		fs.Usage()
		return 2
	}
	if code := runSystemctlUser(stderr, *systemctlFlag, command, "adb-god.service"); code != 0 {
		return code
	}
	fmt.Fprintf(stdout, "adb-god.service %s\n", serviceLifecyclePastTense(command))
	return 0
}

func serviceLifecyclePastTense(command string) string {
	switch command {
	case "start":
		return "started"
	case "stop":
		return "stopped"
	case "restart":
		return "restarted"
	default:
		return command
	}
}

func runDaemonServiceStatus(args []string, stdout, stderr io.Writer) int {
	if runtime.GOOS != "linux" {
		fmt.Fprintln(stderr, "adb-go daemon service status: systemd user services are supported on Linux only")
		return 1
	}
	fs := flag.NewFlagSet("daemon service status", flag.ContinueOnError)
	fs.SetOutput(stderr)
	systemctlFlag := fs.String("systemctl", "systemctl", "systemctl binary path")
	fs.Usage = func() { fmt.Fprint(stderr, daemonServiceStatusUsage) }
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(stderr, "adb-go daemon service status: unexpected arguments %q\n\n", fs.Args())
		fs.Usage()
		return 2
	}

	active, err := systemctlUserOutput(*systemctlFlag, "is-active", "adb-god.service")
	if err != nil {
		fmt.Fprintf(stderr, "adb-go daemon service status: %v\n", err)
		return 1
	}
	enabled, err := systemctlUserOutput(*systemctlFlag, "is-enabled", "adb-god.service")
	if err != nil {
		fmt.Fprintf(stderr, "adb-go daemon service status: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "active: %s\n", active)
	fmt.Fprintf(stdout, "enabled: %s\n", enabled)
	return 0
}

func runDaemonServiceLogs(args []string, stdout, stderr io.Writer) int {
	if runtime.GOOS != "linux" {
		fmt.Fprintln(stderr, "adb-go daemon service logs: systemd user service logs are supported on Linux only")
		return 1
	}
	fs := flag.NewFlagSet("daemon service logs", flag.ContinueOnError)
	fs.SetOutput(stderr)
	journalctlFlag := fs.String("journalctl", "journalctl", "journalctl binary path")
	linesFlag := fs.Int("lines", 100, "number of recent journal entries to print before following")
	followFlag := fs.Bool("follow", false, "keep streaming new journal entries")
	fs.Usage = func() { fmt.Fprint(stderr, daemonServiceLogsUsage) }
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(stderr, "adb-go daemon service logs: unexpected arguments %q\n\n", fs.Args())
		fs.Usage()
		return 2
	}
	if *linesFlag < 0 {
		fmt.Fprintln(stderr, "adb-go daemon service logs: --lines must be zero or greater")
		return 2
	}

	cmdArgs := []string{"--user", "-u", "adb-god.service", "-n", strconv.Itoa(*linesFlag), "--no-pager"}
	if *followFlag {
		cmdArgs = append(cmdArgs, "-f")
	}
	cmd := exec.Command(strings.TrimSpace(*journalctlFlag), cmdArgs...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(stderr, "adb-go daemon service logs: %s %s failed: %v\n", *journalctlFlag, strings.Join(cmdArgs, " "), err)
		return 1
	}
	return 0
}

func runDaemonServiceUninstall(args []string, stdout, stderr io.Writer) int {
	if runtime.GOOS != "linux" {
		fmt.Fprintln(stderr, "adb-go daemon service uninstall: systemd user services are supported on Linux only")
		return 1
	}
	fs := flag.NewFlagSet("daemon service uninstall", flag.ContinueOnError)
	fs.SetOutput(stderr)
	unitDirFlag := fs.String("unit-dir", "", "systemd user unit directory")
	systemctlFlag := fs.String("systemctl", "systemctl", "systemctl binary path")
	keepUnitFlag := fs.Bool("keep-unit", false, "disable and stop the service but do not remove the unit file")
	fs.Usage = func() { fmt.Fprint(stderr, daemonServiceUninstallUsage) }
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(stderr, "adb-go daemon service uninstall: unexpected arguments %q\n\n", fs.Args())
		fs.Usage()
		return 2
	}

	if code := runSystemctlUser(stderr, *systemctlFlag, "disable", "--now", "adb-god.service"); code != 0 {
		return code
	}
	unitDir, err := systemdUserUnitDir(strings.TrimSpace(*unitDirFlag))
	if err != nil {
		fmt.Fprintf(stderr, "adb-go daemon service uninstall: %v\n", err)
		return 1
	}
	unitPath := filepath.Join(unitDir, "adb-god.service")
	if !*keepUnitFlag {
		if err := os.Remove(unitPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			fmt.Fprintf(stderr, "adb-go daemon service uninstall: remove %s: %v\n", unitPath, err)
			return 1
		}
	}
	if code := runSystemctlUser(stderr, *systemctlFlag, "daemon-reload"); code != 0 {
		return code
	}
	if *keepUnitFlag {
		fmt.Fprintln(stdout, "adb-god.service disabled and stopped; unit file kept")
	} else {
		fmt.Fprintf(stdout, "adb-god.service disabled and stopped; removed %s\n", unitPath)
	}
	return 0
}

func writeDaemonServiceUnit(unitDir, adbGodPath, socketPath string) (string, error) {
	unitPath := filepath.Join(unitDir, "adb-god.service")
	unit := adbGodSystemdUnit(adbGodPath, socketPath)
	if err := os.MkdirAll(unitDir, 0o755); err != nil {
		return "", fmt.Errorf("create systemd user unit directory: %w", err)
	}
	if err := os.WriteFile(unitPath, []byte(unit), 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", unitPath, err)
	}
	return unitPath, nil
}

func resolveADBGodPath(configured string) (string, error) {
	if configured != "" {
		abs, err := filepath.Abs(configured)
		if err != nil {
			return "", fmt.Errorf("resolve adb-god path: %w", err)
		}
		return abs, nil
	}
	if found, err := exec.LookPath("adb-god"); err == nil {
		return filepath.Abs(found)
	}
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locate adb-god: adb-god is not on PATH and current executable path is unavailable")
	}
	candidate := filepath.Join(filepath.Dir(exe), "adb-god")
	if _, err := os.Stat(candidate); err == nil {
		return candidate, nil
	}
	return "", fmt.Errorf("locate adb-god: pass --adb-god PATH or install adb-god next to adb-go")
}

func systemdUserUnitDir(configured string) (string, error) {
	if configured != "" {
		return filepath.Abs(configured)
	}
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("locate systemd user unit directory: %w", err)
		}
		configHome = filepath.Join(home, ".config")
	}
	return filepath.Join(configHome, "systemd", "user"), nil
}

func adbGodSystemdUnit(adbGodPath, socketPath string) string {
	return strings.Join([]string{
		"[Unit]",
		"Description=adb-go daemon",
		"",
		"[Service]",
		"Type=simple",
		"ExecStart=" + systemdQuote(adbGodPath) + " -socket " + systemdQuote(socketPath),
		"Restart=on-failure",
		"RestartSec=2s",
		"",
		"[Install]",
		"WantedBy=default.target",
		"",
	}, "\n")
}

func systemdQuote(s string) string {
	return strconv.Quote(s)
}

func runSystemctlUser(stderr io.Writer, systemctl string, args ...string) int {
	cmdArgs := append([]string{"--user"}, args...)
	cmd := exec.Command(systemctl, cmdArgs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Fprintf(stderr, "adb-go daemon service install: %s %s failed: %v", systemctl, strings.Join(cmdArgs, " "), err)
		if len(out) > 0 {
			fmt.Fprintf(stderr, ": %s", strings.TrimSpace(string(out)))
		}
		fmt.Fprintln(stderr)
		return 1
	}
	return 0
}

func systemctlUserOutput(systemctl string, args ...string) (string, error) {
	cmdArgs := append([]string{"--user"}, args...)
	cmd := exec.Command(systemctl, cmdArgs...)
	out, err := cmd.CombinedOutput()
	status := strings.TrimSpace(string(out))
	if status != "" {
		return status, nil
	}
	if err != nil {
		return "", fmt.Errorf("%s %s failed: %w", systemctl, strings.Join(cmdArgs, " "), err)
	}
	return "", nil
}
