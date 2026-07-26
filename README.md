# adb-go

`adb-go` is a pure-Go implementation of the Android Debug Bridge (ADB)
protocol. It is primarily a Go library for embedding ADB behavior in
applications, with an experimental CLI wrapper for supported workflows.

> **Status:** v0 is not a full replacement for the official `adb` binary yet.
> It currently implements explicit TCP connections, initial Linux USB support,
> explicit-key authentication, service opening, shell execution/streaming,
> Android property lookup, logcat streaming/dump, screenshot capture, single-file
> push/pull, and one-APK installation.

## Contents

- [Install](#install)
- [High-level client API](#high-level-client-api)
- [Linux USB support](#linux-usb-support)
- [Authentication helpers](#authentication-helpers)
- [Experimental CLI](#experimental-cli)
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
pushing or pulling one file, and installing one APK.

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

adb-go shell --addr 127.0.0.1:5555 echo hello
adb-go push --addr 127.0.0.1:5555 ./local.txt /data/local/tmp/local.txt
adb-go pull --addr 127.0.0.1:5555 /data/local/tmp/remote.txt ./remote.txt
adb-go install-apk --addr 127.0.0.1:5555 ./app.apk
adb-go getprop --addr 127.0.0.1:5555 ro.product.model
adb-go getprop --addr 127.0.0.1:5555
adb-go logcat --addr 127.0.0.1:5555
adb-go logcat --dump --addr 127.0.0.1:5555
adb-go screencap --addr 127.0.0.1:5555 ./screen.png
```

CLI docs: [`cmd/adb-go/README.md`](cmd/adb-go/README.md).

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
- Incomplete command coverage: shell, shell streaming, generic service opening,
  Android property lookup, logcat streaming/dump, screenshot capture,
  single-file push/pull, and one-APK installation are the main supported
  workflows.
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

More details are in the package READMEs and [`docs/README.md`](docs/README.md).

## Testing

```sh
go test ./...
```

Optional integration tests are skipped by default. See
[`client/README.md`](client/README.md#testing-and-integration) and
[`cmd/adb-go/README.md`](cmd/adb-go/README.md#testing).
