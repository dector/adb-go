# adb-go CLI

`adb-go` includes an experimental CLI named `adb-go`. The CLI is intentionally a
thin wrapper around the library's supported high-level operations, not a full
clone of the official `adb` command.

## Contents

- [Install](#install)
- [Target selection](#target-selection)
- [USB targets](#usb-targets)
- [Authentication](#authentication)
- [Commands](#commands)
  - [`version`](#version)
  - [`targets`](#targets)
  - [`shell`](#shell)
  - [`push` and `pull`](#push-and-pull)
  - [`install-apk`](#install-apk)
  - [`getprop`](#getprop)
  - [`logcat`](#logcat)
  - [`screencap`](#screencap)
  - [`reboot`](#reboot)
  - [`forward`](#forward)
  - [`daemon`](#daemon)
- [Troubleshooting common errors](#troubleshooting-common-errors)
- [Limitations](#limitations)
- [Testing](#testing)

## Install

```sh
go install github.com/dector/adb-go/cmd/adb-go@latest
```

The optional daemon control commands talk to the separate foreground `adb-god`
binary. Install it when you want to try the daemon foundation:

```sh
go install github.com/dector/adb-go/cmd/adb-god@latest
```

From a local checkout, run the CLI without installing:

```sh
go run ./cmd/adb-go help
go run ./cmd/adb-go version
```

## Target selection

Every v0 CLI operation targets one explicit ADB endpoint. For TCP, pass
`--addr HOST[:PORT]`:

```sh
adb-go targets
adb-go shell --addr 127.0.0.1:5555 echo hello
adb-go push --addr 127.0.0.1:5555 ./local.txt /data/local/tmp/local.txt
adb-go pull --addr 127.0.0.1:5555 /data/local/tmp/remote.txt ./remote.txt
adb-go install-apk --addr 127.0.0.1:5555 ./app.apk
adb-go getprop --addr 127.0.0.1:5555 ro.product.model
adb-go logcat --addr 127.0.0.1:5555
adb-go screencap --addr 127.0.0.1:5555 ./screen.png
adb-go reboot --addr 127.0.0.1:5555
adb-go forward --addr 127.0.0.1:5555 tcp:9000 tcp:9000
```

If the port is omitted, the library connection path defaults to the standard ADB
TCP port `5555`, so `--addr 127.0.0.1` means `127.0.0.1:5555`.

As a convenience for repeated TCP commands, set `ADB_GO_ADDR` instead of passing
`--addr` each time. An explicit `--addr` always takes precedence:

```sh
export ADB_GO_ADDR=127.0.0.1:5555
adb-go shell echo hello
adb-go push ./local.txt /data/local/tmp/local.txt
adb-go pull --overwrite /data/local/tmp/remote.txt ./remote.txt
adb-go install-apk --replace ./app.apk
adb-go getprop ro.product.model
adb-go getprop
adb-go logcat --dump
adb-go screencap ./screen.png
adb-go reboot recovery
adb-go forward tcp:9000 tcp:9000
```

## USB targets

On Linux, pass USB selection flags instead of `--addr`:

```sh
adb-go shell --usb getprop ro.product.model
adb-go shell --usb-path /dev/bus/usb/001/002 getprop ro.product.model
adb-go push --usb-bus 1 --usb-device 2 ./local.txt /data/local/tmp/local.txt
adb-go pull --usb-vid 18d1 --usb-pid 4ee7 /data/local/tmp/remote.txt ./remote.txt
adb-go install-apk --usb-path /dev/bus/usb/001/002 ./app.apk
adb-go getprop --usb-path /dev/bus/usb/001/002 ro.product.model
adb-go logcat --usb-path /dev/bus/usb/001/002
adb-go screencap --usb-path /dev/bus/usb/001/002 ./screen.png
adb-go reboot --usb-path /dev/bus/usb/001/002 bootloader
adb-go forward --usb-path /dev/bus/usb/001/002 tcp:9000 tcp:9000
```

`--usb` requests USB discovery without narrowing selection. It succeeds only
when exactly one ADB USB device is visible. Use `--usb-path`,
`--usb-bus`/`--usb-device`, or `--usb-vid`/`--usb-pid` to choose a device when
more than one match exists. USB flags cannot be combined with `--addr`.
`--serial` is accepted as a reserved selector but currently returns an
unsupported error because USB serial string descriptors are not implemented yet.

## Authentication

For authenticated TCP or USB devices, provide an existing ADB private key with
`--auth-key`:

```sh
adb-go shell --auth-key ~/.android/adbkey --addr 127.0.0.1:5555 getprop ro.product.model
adb-go shell --auth-key ~/.android/adbkey --usb-path /dev/bus/usb/001/002 getprop ro.product.model
adb-go install-apk --auth-key ~/.android/adbkey --addr 127.0.0.1:5555 ./app.apk
adb-go getprop --auth-key ~/.android/adbkey --addr 127.0.0.1:5555 ro.product.model
adb-go logcat --auth-key ~/.android/adbkey --addr 127.0.0.1:5555 --dump
adb-go screencap --auth-key ~/.android/adbkey --addr 127.0.0.1:5555 ./screen.png
adb-go reboot --auth-key ~/.android/adbkey --addr 127.0.0.1:5555 recovery
adb-go forward --auth-key ~/.android/adbkey --addr 127.0.0.1:5555 tcp:9000 tcp:9000
```

The CLI does not create or modify key files.

## Commands

Device workflow commands such as `shell`, `push`, `pull`, `install-apk`,
`getprop`, `logcat`, `screencap`, `reboot`, and `forward` connect directly to an
explicit TCP or Linux USB target. The `daemon` command is different: it talks to
the local `adb-god` control socket and does not select or operate on an Android
device. The `version` command is host-only and performs no ADB or daemon I/O.

### `version`

`adb-go version` prints concise build and runtime information for support
requests:

```text
adb-go: dev
go: go1.25.0
os: linux
arch: amd64
```

Development builds use `dev` as the adb-go version fallback. Release builds can
inject the project's Git-derived version with Go's standard linker variable
support:

```sh
version=$(./tools/git-version.sh)
go build -ldflags "-X main.version=${version}" ./cmd/adb-go
```

The helper looks for the latest semantic `vMAJOR.MINOR.PATCH` Git tag. It
prints the tag unchanged when `HEAD` is exactly on that tag and the worktree is
clean. When there are clean commits after the tag, it bumps the patch component
and appends the zero-padded commit distance, such as `v0.1.1-001` for one commit
after `v0.1.0`. Dirty worktrees append `-snapshot` to the calculated version,
such as `v0.1.1-001-snapshot`.

### `targets`

`adb-go targets` is an adb-go-specific alternative to `adb devices`. It does not
query the official adb server and does not try to reproduce the official state
list. Instead, it prints connection selectors that adb-go itself can use:
`ADB_GO_ADDR` as an explicit TCP target, optionally scanned local emulator TCP
ports with `adb-go targets --scan`, plus locally discovered Linux USB ADB
interfaces when available.

`--scan` probes localhost emulator ADB ports `5555..5585`, odd ports only, with
a short timeout. This is useful for emulator serials such as `emulator-5554`,
whose ADB TCP endpoint is normally `127.0.0.1:5555`.

Example output:

```text
TRANSPORT  SELECTOR                         DETAILS
tcp        --addr 127.0.0.1:5555            from ADB_GO_ADDR
tcp        --addr 127.0.0.1:5557            scanned localhost emulator port
usb        --usb-path /dev/bus/usb/001/002  bus=001 device=002 vid:pid=18d1:4ee7 interface=3 endpoints=in:0x81,out:0x02
```

The USB row means adb-go found a USB interface whose descriptors match ADB's
vendor-specific class/subclass/protocol. It is still only a candidate: opening
it may require `/dev/bus/usb` permissions, and the ADB handshake may still fail
with `adb.ErrAuthRequired` unless you provide a trusted key with `--auth-key`.

### `shell`

`adb-go shell` joins all remaining arguments with spaces and sends the result as
one shell command string, matching the library API and common `adb shell` usage.
For example, this opens the ADB service string `shell:pm list packages`:

```sh
adb-go shell --addr 127.0.0.1:5555 pm list packages
adb-go shell --usb-path /dev/bus/usb/001/002 pm list packages
```

### `push` and `pull`

`adb-go push` and `adb-go pull` transfer exactly one file:

```sh
adb-go push --addr 127.0.0.1:5555 ./local.txt /data/local/tmp/local.txt
adb-go pull --addr 127.0.0.1:5555 /data/local/tmp/remote.txt ./remote.txt
```

`pull` refuses to replace an existing local destination unless `--overwrite` is
provided.

### `install-apk`

`adb-go install-apk` installs exactly one local APK on the selected device:

```sh
adb-go install-apk --addr 127.0.0.1:5555 ./app.apk
adb-go install-apk --usb-path /dev/bus/usb/001/002 ./app.apk
adb-go install-apk --auth-key ~/.android/adbkey --addr 127.0.0.1:5555 ./app.apk
```

Pass `--replace` to allow replacing an already-installed app. This maps to
Android package manager's `pm install -r` behavior:

```sh
adb-go install-apk --replace --addr 127.0.0.1:5555 ./app.apk
```

The command is an adb-go-specific alternative to `adb install`, not a flag-for-
flag clone. Internally it pushes the local APK to a generated path below
`/data/local/tmp`, runs `pm install` through `shell:`, then asks the device to
remove the temporary APK. If package installation fails, stderr includes the
package-manager output returned by the device, such as
`Failure [INSTALL_FAILED_ALREADY_EXISTS]`.

Non-goals for the current command include official `adb install` flag
compatibility, split APKs, directory installs, install sessions, streamed
installs, user/ABI/install-location controls, granting permissions at install
time, downgrade/test-package flags, and package discovery. Use only the options
shown by `adb-go install-apk -h`; unknown official adb flags are not accepted.

Security note: the local APK path is caller-controlled, installation changes the
selected device, and package-manager output is device-provided remote command
output.

### `getprop`

`adb-go getprop` reads Android system properties from the selected device. With
one property argument, it prints only that property's value:

```sh
adb-go getprop --addr 127.0.0.1:5555 ro.product.model
adb-go getprop --usb-path /dev/bus/usb/001/002 ro.build.version.sdk
adb-go getprop --auth-key ~/.android/adbkey --addr 127.0.0.1:5555 ro.product.name
```

With no property argument, it prints all properties in stable name order using
Android `getprop`'s standard readable format:

```sh
adb-go getprop --addr 127.0.0.1:5555
```

Example output:

```text
[ro.build.version.sdk]: [35]
[ro.product.model]: [Pixel Fixture]
```

The command accepts the same target-selection flags as other device operations:
TCP with `--addr` or `ADB_GO_ADDR`, Linux USB with `--usb`/`--usb-path`/USB ID
selectors, and explicit authentication with `--auth-key`. It accepts at most one
property name; use `adb-go shell ...` if you need unsupported device-side
`getprop` behavior.

Property names and values come from the connected device. Treat them as remote
data that can differ across devices, Android releases, vendor builds, and test
fixtures.

### `logcat`

`adb-go logcat` streams Android log output from the selected device to stdout:

```sh
adb-go logcat --addr 127.0.0.1:5555
adb-go logcat --usb-path /dev/bus/usb/001/002
adb-go logcat --auth-key ~/.android/adbkey --addr 127.0.0.1:5555
```

By default it follows the live log stream until the device closes it or the
process is interrupted. Pass `--dump` to request logcat's dump-and-exit mode,
which maps to the device command `logcat -d`:

```sh
adb-go logcat --dump --addr 127.0.0.1:5555
adb-go logcat --dump --auth-key ~/.android/adbkey --usb-path /dev/bus/usb/001/002
```

The command accepts the same target-selection flags as other device operations:
TCP with `--addr` or `ADB_GO_ADDR`, Linux USB with `--usb`/`--usb-path`/USB ID
selectors, and explicit authentication with `--auth-key`.

Current logcat support is deliberately narrow. adb-go does not aim to be
flag-compatible with every official `adb logcat` option yet; filter specs,
format flags, buffer selection, log clearing, file output, and other official
logcat controls are not accepted by `adb-go logcat`. For unsupported behavior,
use `adb-go shell ...` or the library's `ShellStream` with an explicit logcat
command.

Logcat output comes from the connected device and can be long-running or large.
Redirect it, pipe it, or interrupt the command according to your shell's normal
stdout/process behavior.

### `screencap`

`adb-go screencap` captures one PNG screenshot from the selected device and saves
it as a local file:

```sh
adb-go screencap --addr 127.0.0.1:5555 ./screen.png
adb-go screencap --usb-path /dev/bus/usb/001/002 ./screen.png
adb-go screencap --auth-key ~/.android/adbkey --addr 127.0.0.1:5555 ./screen.png
```

If `LOCAL_PNG` is omitted, adb-go writes to a timestamped file in the current
directory named like `screen-yyyymmdd-hhmmssmmm.png`, for example
`screen-20260102-030405123.png`, and prints the chosen path to stdout:

```sh
adb-go screencap --addr 127.0.0.1:5555
```

By default the command refuses to replace an existing local path. Pass
`--overwrite` only when replacement is intentional:

```sh
adb-go screencap --overwrite --addr 127.0.0.1:5555 ./screen.png
```

Internally the command uses the library's `Screencap` helper, which runs the
Android device command `screencap -p` through an ADB `shell:` stream and writes
the resulting PNG bytes locally. adb-go includes a narrow compatibility cleanup
for Android shell paths that CRLF-mangle PNG output, but it does not otherwise
interpret or validate the screenshot contents.

Screenshots come from the connected device and may contain sensitive on-screen
information. Treat the output file as remote device data captured at the moment
the command runs.

### `reboot`

`adb-go reboot` requests an immediate reboot of the selected device:

```sh
adb-go reboot --addr 127.0.0.1:5555
adb-go reboot --usb-path /dev/bus/usb/001/002
adb-go reboot --auth-key ~/.android/adbkey --addr 127.0.0.1:5555
```

With no mode argument, adb-go requests a normal Android reboot. The command also
accepts the supported explicit modes `bootloader` and `recovery`:

```sh
adb-go reboot --addr 127.0.0.1:5555 bootloader
adb-go reboot --auth-key ~/.android/adbkey --usb-path /dev/bus/usb/001/002 recovery
```

The command accepts the same target-selection flags as other device operations:
TCP with `--addr` or `ADB_GO_ADDR`, Linux USB with `--usb`/`--usb-path`/USB ID
selectors, and explicit authentication with `--auth-key`. Unsupported modes are
rejected before adb-go connects to the device; use `normal`, `bootloader`, or
`recovery` only.

Reboot is intentionally disruptive. It affects the selected device immediately,
can interrupt apps and tests running on that device, and commonly closes the ADB
connection while Android or the bootloader restarts. A successful command means
that the ADB daemon accepted the reboot request; it does not wait for the device
to come back online.

### `forward`

`adb-go forward` starts a foreground local TCP forwarding session from the host
to a TCP port reachable from the selected device:

```sh
adb-go forward --addr 127.0.0.1:5555 tcp:9000 tcp:9000
adb-go forward --usb-path /dev/bus/usb/001/002 tcp:9000 tcp:9000
adb-go forward --auth-key ~/.android/adbkey --addr 127.0.0.1:5555 tcp:9000 tcp:9000
```

The first endpoint is the local host listener and the second endpoint is the
remote device target. The initial command supports `tcp:PORT` for both sides
only. Local `tcp:PORT` binds to `127.0.0.1:PORT`; use `tcp:0` to ask the
operating system for an available local port. After setup, adb-go prints the
actual bound local address, which is especially useful with `tcp:0`:

```text
Forwarding 127.0.0.1:49321 -> tcp:9000. Press Ctrl+C to stop.
```

The command then remains running. While it is running, each local client
connection is accepted by adb-go, adb-go opens a fresh device `tcp:PORT` ADB
service stream, and bytes are copied in both directions. Press Ctrl-C or stop
the process to close the local listener and any active bridged connections.

This lifecycle is the main difference from official `adb forward`. Official adb
registers mappings in the background host ADB server, so the `adb forward`
command can exit while the server keeps listening and can later answer
`--list`, `--remove`, and `--remove-all`. adb-go is direct-device and
process-scoped in v0: it does not use the official server, does not create a
persistent mapping table, and removes the forward when the command exits.

Unsupported forwarding forms currently include host Unix sockets, Android local
socket namespaces such as `localabstract:`, JDWP, vsock, reverse forwarding, raw
advanced service targets, persistent mappings, `--list`, `--remove`, and
`--remove-all`. Future daemon-backed persistent forwarding is designed in
[`../../docs/persistent-forwarding-design.md`](../../docs/persistent-forwarding-design.md),
but it is not implemented yet.

### `daemon`

`adb-go daemon` controls the local `adb-god` process over adb-go's Unix domain
socket control protocol:

```sh
adb-go daemon doctor
adb-go daemon ping
adb-go daemon status
adb-go daemon stop
adb-go daemon service install
adb-go daemon service reinstall
adb-go daemon service start
adb-go daemon service stop
adb-go daemon service restart
adb-go daemon service status
adb-go daemon service logs
adb-go daemon service uninstall
```

`doctor` is a read-only diagnostics command. It prints the socket path resolved
by the same rules as the other daemon commands, whether that path exists,
whether a compatible daemon responds to the daemon socket protocol, and Linux
systemd user-service active/enabled state when `systemctl` is available:

```text
socketPath: /run/user/1000/adb-go/adb-god.sock
socketExists: true
socketType: unix
daemonProtocol: responding
daemonState: running
daemonProtocolVersion: 1
systemdActive: active
systemdEnabled: enabled
hints: none
```

When something looks wrong, `doctor` keeps diagnosing instead of starting,
stopping, installing, or uninstalling anything. For example, a missing socket or
inactive systemd service produces actionable hints such as starting the service
or checking that `adb-go` and `adb-god` agree on the socket path. Pass
`--systemctl PATH` after `doctor` to test or use a non-default systemctl binary:

```sh
adb-go daemon doctor --systemctl /usr/bin/systemctl
```

`ping` is a liveness check. It sends a protocol `ping` request and prints
`pong` when a compatible daemon responds.

`status` prints basic process metadata only:

```text
state: running
pid: 12345
socketPath: /run/user/1000/adb-go/adb-god.sock
protocolVersion: 1
uptimeMillis: 2500
```

These fields describe the daemon process and protocol endpoint. They are not a
device list and do not include transport state, sessions, forwarding mappings,
authentication state, or any persistent ADB feature state.

`stop` sends the protocol `shutdown` request. A successful response means the
daemon accepted graceful shutdown; the daemon then stops accepting new control
connections, closes its listener, and removes its socket file on the normal
shutdown path.

By default, `adb-go daemon ...` and `adb-god` use the same socket path selection
rules:

1. `ADB_GO_DAEMON_SOCKET`, when set to an absolute path.
2. `$XDG_RUNTIME_DIR/adb-go/adb-god.sock`, when `XDG_RUNTIME_DIR` is absolute.
3. `$TMPDIR/adb-go-$UID/adb-god.sock`, using Go's `os.TempDir()` and the current
   Unix user ID.

For tests, development, or non-default installations, pass an explicit absolute
socket path to the CLI:

```sh
adb-go daemon --socket /tmp/adb-go-demo/adb-god.sock status
```

Start the daemon itself separately with the matching path:

```sh
adb-god --socket /tmp/adb-go-demo/adb-god.sock
```

On Linux systems that use systemd user services, `service install` writes
`~/.config/systemd/user/adb-god.service`, runs `systemctl --user daemon-reload`,
and enables/starts the service with `systemctl --user enable --now
adb-god.service`:

```sh
adb-go daemon service install
```

Use `--adb-god PATH` when `adb-god` is not on `PATH`, and use `--socket PATH` to
bake a non-default socket path into the unit:

```sh
adb-go daemon --socket /tmp/adb-go-demo/adb-god.sock service install --adb-god /usr/local/bin/adb-god
```

The service group also wraps unit refreshes and common systemd user lifecycle
operations:

```sh
adb-go daemon service reinstall
adb-go daemon service start
adb-go daemon service stop
adb-go daemon service restart
adb-go daemon service status
adb-go daemon service logs
adb-go daemon service uninstall
```

Use `service reinstall` when the installed unit should be rewritten, for example
after installing `adb-god` at a different path, choosing a different daemon
socket path with top-level `--socket`, or replacing a local development build.
It uses the same `--adb-god PATH`, `--unit-dir DIR`, `--socket PATH`, and
`--systemctl PATH` overrides as the install/lifecycle commands where relevant,
then runs `systemctl --user daemon-reload`, `systemctl --user enable
adb-god.service`, and `systemctl --user restart adb-god.service`.

`service status` runs `systemctl --user is-active adb-god.service` and
`systemctl --user is-enabled adb-god.service`, then prints concise fields:

```text
active: active
enabled: enabled
```

Inactive or disabled services are reported the same way, for example
`active: inactive` or `enabled: disabled`. This command reports systemd's host
service-manager view. It is intentionally separate from `adb-go daemon status`,
which talks to a running daemon through the adb-go daemon socket protocol.

`service logs` reads recent host-service output through the current user's
systemd journal:

```sh
adb-go daemon service logs
adb-go daemon service logs --lines 25
adb-go daemon service logs --follow
adb-go daemon service logs --journalctl /usr/bin/journalctl
```

The default invocation is intentionally small and predictable:
`journalctl --user -u adb-god.service -n 100 --no-pager`. `--lines N` changes
the `-n` value, `--follow` adds journalctl's follow mode, and `--journalctl PATH`
is available for tests or installations where the binary is not found as plain
`journalctl`.

`service uninstall` runs `systemctl --user disable --now adb-god.service`,
removes the user unit file, then runs `systemctl --user daemon-reload`. These
service commands manage the host systemd unit. They are different from
`adb-go daemon stop`, which sends a graceful shutdown request to the currently
running daemon over the daemon socket protocol.

The initial daemon is deliberately minimal. It is not the official adb server,
it does not keep ADB devices,
USB/TCP transports, foreground forwards, shell sessions, install state, logcat
streams, screenshots, reboots, or authentication keys alive after a CLI command
exits. Those are future design areas built on this process and socket
foundation.

## Troubleshooting common errors

The CLI keeps the original lower-level error text, but common failures include a
short hint before it:

- `device requires authentication`: pass `--auth-key PATH` for an existing ADB
  private key trusted by the device.
- `connection refused`: check that the emulator/device is running ADB TCP at the
  selected address. For local emulators, try `adb-go targets --scan`.
- `operation timed out`: check target reachability, device online state, and USB
  permissions.
- `operation unsupported`: the selected transport, selector, or feature is not
  implemented in this adb-go build; USB is Linux-only initially and USB serial
  selection is still reserved.
- `destination already exists`: `pull` and `screencap` refuse to overwrite local
  files by default; pass `--overwrite` only when replacement is intentional.
- `local file or directory not found`: check local source paths for `push` and
  `install-apk`, or the parent directory for local destinations.

## Limitations

The CLI is not a complete `adb` replacement. Broad command compatibility such as
`devices`, official `adb install` compatibility, full official `adb logcat`
flag compatibility, server management, wireless pairing, adb-server-backed
persistent forwarding, and most official flags are not implemented. Use
`getprop` for adb-go's limited property inspection workflow, `screencap` for
one-shot PNG screenshot capture, `reboot` for explicit disruptive reboot
requests, `forward` for foreground local-TCP-to-device-TCP forwarding,
`daemon` for minimal local `adb-god` process control, and `install-apk` for
adb-go's limited one-APK installation workflow.

## Testing

From the repository root:

```sh
go test ./cmd/adb-go ./...
```

Optional end-to-end workflows are covered by the repository integration tests;
see [`../../docs/integration-testing.md`](../../docs/integration-testing.md) for
TCP emulator/device examples, the containerized Linux `adbd` workflow, Linux USB
requirements, and troubleshooting.
