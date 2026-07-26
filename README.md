# adb-go

`adb-go` is a pure-Go implementation of the Android Debug Bridge (ADB)
protocol. It is primarily a Go library for embedding ADB behavior in
applications, with an experimental CLI wrapper for supported workflows.

> **Status:** v0 is not a full replacement for the official `adb` binary yet.
> It currently implements explicit TCP connections, initial Linux USB support,
> explicit-key authentication, service opening, shell execution/streaming,
> Android property lookup, logcat streaming/dump, screenshot capture, reboot,
> foreground local TCP forwarding, single-file push/pull, and one-APK
> installation.

## Contents

- [Install](#install)
- [High-level client API](#high-level-client-api)
- [Linux USB support](#linux-usb-support)
- [Authentication helpers](#authentication-helpers)
- [Experimental CLI](#experimental-cli)
- [adb-god daemon foundation](#adb-god-daemon-foundation)
- [Low-level protocol package](#low-level-protocol-package)
- [Implementation notes](#implementation-notes)
- [Current limitations](#current-limitations)
- [Testing](#testing)

## Install

```sh
go get github.com/dector/adb-go
```

```go
import adb "github.com/dector/adb-go"
```

The current TCP and Linux USB implementation uses only the Go standard library:
no Android SDK, platform-tools, official `adb` binary, cgo, libusb, or native
dependencies are required.

## High-level client API

Use the root package for the stable high-level API. It re-exports the `client`
package for connecting to a device, opening services, running shell commands,
reading Android system properties, streaming Android logs, capturing screenshots,
rebooting into supported modes, forwarding local TCP connections to device TCP
ports, pushing or pulling one file, and installing one APK.

```go
ctx := context.Background()

c, err := adb.Connect(ctx, "127.0.0.1:5555")
if err != nil {
    return err
}
defer c.Close()

out, err := c.Shell(ctx, "echo hello")
if err != nil {
    return err
}
fmt.Printf("%s", out)
```

Read Android system properties with helpers that wrap and parse `getprop`:

```go
model, err := c.GetProp(ctx, "ro.product.model")
if err != nil {
    return err
}
fmt.Println(model)

props, err := c.Properties(ctx)
if err != nil {
    return err
}
fmt.Println(props["ro.build.version.sdk"])
```

Stream Android log output with the small logcat helper:

```go
err := c.Logcat(ctx, os.Stdout, adb.LogcatOptions{})
if err != nil {
    return err
}
```

Use dump-and-exit mode when you want the current log buffer instead of a
long-running stream:

```go
err := c.Logcat(ctx, os.Stdout, adb.LogcatOptions{Dump: true})
if err != nil {
    return err
}
```

Capture one PNG screenshot with the screencap helper:

```go
png, err := c.Screencap(ctx)
if err != nil {
    return err
}
err = os.WriteFile("screen.png", png, 0o666)
```

Or let adb-go write to a new local destination that must not already exist:

```go
err := c.ScreencapFile(ctx, "screen.png")
if err != nil {
    return err
}
```

Request a supported reboot mode explicitly:

```go
err := c.Reboot(ctx, adb.RebootNormal)
if err != nil {
    return err
}
```

`Reboot` is disruptive: a successful request affects the selected device
immediately and may close the ADB connection as the device restarts.

Forward local TCP connections to a TCP endpoint on the selected device:

```go
remote, err := adb.ForwardTCP(8080)
if err != nil {
    return err
}

forward, err := c.ForwardLocalTCP(ctx, "127.0.0.1:0", remote)
if err != nil {
    return err
}
defer forward.Close()

fmt.Println("listening on", forward.LocalAddr())
err = forward.Wait()
```

This is process-scoped foreground forwarding. It is useful for bridging local
clients to a service listening on the device, but it is not an adb-server-backed
persistent `adb forward` registration.

Install one local APK with the adb-go-specific install helper:

```go
err := c.InstallAPKWithOptions(ctx, "./app.apk", adb.InstallOptions{Replace: true})
if err != nil {
    return err
}
```

The install helper intentionally is not a full clone of `adb install`; it pushes
the APK to `/data/local/tmp`, runs Android's package manager, and removes the
temporary file on a best-effort basis. More examples: [`client/README.md`](client/README.md).

## Linux USB support

On Linux, adb-go can connect directly through `/dev/bus/usb` without libusb.
Select the only visible ADB-capable interface, or provide a USB selector when
multiple devices are connected.

```go
c, err := adb.ConnectUSB(ctx, adb.USBOptions{
    DevicePath: "/dev/bus/usb/001/002",
})
```

Details and troubleshooting: [`client/README.md`](client/README.md#connect-over-linux-usb),
[`docs/linux-usb-transport.md`](docs/linux-usb-transport.md).

## Authentication helpers

Authenticated devices can use an explicitly supplied existing ADB RSA private
key. adb-go does not generate, discover, or persist keys in v0.

```go
credential, err := adb.LoadPrivateKey("/home/me/.android/adbkey")
if err != nil {
    return err
}

c, err := adb.ConnectTCPWithOptions(ctx, "127.0.0.1:5555", adb.ConnectOptions{
    AuthCredentials: []adb.AuthCredential{credential},
})
```

Package docs: [`auth/README.md`](auth/README.md). Protocol details:
[`docs/authentication.md`](docs/authentication.md).

## Experimental CLI

The `adb-go` CLI is a thin wrapper around supported library workflows. It is not
a full clone of the official `adb` command.

```sh
go install github.com/dector/adb-go/cmd/adb-go@latest

adb-go version
adb-go shell --addr 127.0.0.1:5555 echo hello
adb-go push --addr 127.0.0.1:5555 ./local.txt /data/local/tmp/local.txt
adb-go pull --addr 127.0.0.1:5555 /data/local/tmp/remote.txt ./remote.txt
adb-go install-apk --addr 127.0.0.1:5555 ./app.apk
adb-go getprop --addr 127.0.0.1:5555 ro.product.model
adb-go getprop --addr 127.0.0.1:5555
adb-go logcat --addr 127.0.0.1:5555
adb-go logcat --dump --addr 127.0.0.1:5555
adb-go screencap --addr 127.0.0.1:5555 ./screen.png
adb-go reboot --addr 127.0.0.1:5555
adb-go reboot --addr 127.0.0.1:5555 recovery
adb-go forward --addr 127.0.0.1:5555 tcp:9000 tcp:9000
```

`adb-go version` prints the adb-go build version, Go runtime version, target OS,
and target architecture. Development builds report `dev`; release builds can
inject a Git-derived value with Go's standard linker flags. The helper
`./tools/git-version.sh` prints the latest semantic `vMAJOR.MINOR.PATCH` tag
as-is when `HEAD` is exactly on that tag and the worktree is clean. For clean
commits after the tag, it bumps the patch component and appends the zero-padded
commit distance, for example `v0.1.1-001`. Dirty worktrees append `-snapshot` to
that calculated version, for example `v0.1.1-001-snapshot`:

```sh
version=$(./tools/git-version.sh)
go build -ldflags "-X main.version=${version}" ./cmd/adb-go
```

CLI docs: [`cmd/adb-go/README.md`](cmd/adb-go/README.md).

## adb-god daemon foundation

`adb-god` is the first adb-go daemon process. In this initial slice it is only a
local control daemon for future persistence work. It listens on a Unix domain
socket and supports `ping`, `status`, and graceful `shutdown`; it does not own
devices, transports, forwards, shell sessions, authentication state, or other
ADB workflow state yet.

```sh
go install github.com/dector/adb-go/cmd/adb-god@latest
adb-god

adb-go daemon doctor
adb-go daemon status
```

On Linux with systemd user services, the CLI can install and manage `adb-god` as
a per-user service:

```sh
adb-go daemon service install
adb-go daemon service status
adb-go daemon service logs
```

Daemon design, socket selection, control protocol, troubleshooting, and service
management details: [`docs/daemon-foundation.md`](docs/daemon-foundation.md).

## Low-level protocol package

Advanced callers can use `protocol` for direct ADB packet, handshake, and stream
access. Most applications should use the high-level client API instead.

```go
conn := protocol.NewConnection(rw)
_, err := conn.Handshake(ctx)
```

Package docs: [`protocol/README.md`](protocol/README.md).

## Implementation notes

Architecture, package responsibilities, protocol flow, security model, and other
cross-cutting implementation notes live in [`docs/README.md`](docs/README.md).

## Current limitations

`adb-go` intentionally supports only a small v0 subset:

- Transport support is limited to explicit TCP endpoints and Linux USB via
  `/dev/bus/usb`; macOS and Windows USB are not implemented yet.
- ADB authentication requires explicit existing RSA key files.
- No broad device discovery, server management, or official `adb devices`
  compatibility in v0. The CLI has an adb-go-specific `targets` command.
- The initial `adb-god` daemon is only a local process-control foundation. It
  does not persist or own devices, transports, forwards, sessions, or
  authentication state yet.
- Incomplete command coverage: shell, shell streaming, generic service opening,
  Android property lookup, logcat streaming/dump, screenshot capture, reboot,
  foreground local TCP forwarding, single-file push/pull, and one-APK
  installation are the main supported workflows.
- APK installation is exposed as `install-apk`, an adb-go-specific helper rather
  than official `adb install` compatibility. It currently supports one local APK
  and the replace-existing-app option only.
- Property support intentionally covers common `getprop` reads through adb-go's
  `GetProp`/`Properties` helpers and CLI `getprop` command. Property names and
  values come from the connected device and should be treated as remote data.
- Logcat support intentionally covers only adb-go's `Logcat` helper and CLI
  `logcat` command with optional dump mode. It is not a full implementation of
  every official `adb logcat` filter, format, buffer, or output flag.
- Screencap support captures one PNG image through Android's `screencap -p`
  shell command. The CLI writes to a local PNG file and refuses to overwrite an
  existing path unless `--overwrite` is passed.
- Reboot support is intentionally explicit and disruptive. It currently supports
  normal, bootloader, and recovery modes through adb-go's `Reboot` helper and
  CLI `reboot` command; a successful request may close the ADB connection while
  the selected device restarts.
- Forwarding support is foreground and process-scoped. adb-go binds its own
  local TCP listener and opens a fresh device `tcp:PORT` service for each
  accepted connection; it does not register persistent mappings in an adb server
  and does not support `adb forward --list`, `--remove`, `--remove-all`, JDWP,
  Unix sockets, reverse forwarding, or other endpoint families yet.

More details are in the package READMEs and [`docs/README.md`](docs/README.md).

## Testing

```sh
go test ./...
```

Optional integration tests are skipped by default. See
[`client/README.md`](client/README.md#testing-and-integration) and
[`cmd/adb-go/README.md`](cmd/adb-go/README.md#testing).
