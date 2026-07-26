# adb-go

`adb-go` is a pure-Go implementation of the Android Debug Bridge (ADB) protocol.
It is primarily a Go library for embedding ADB behavior in applications. The
v0 implementation supports explicit TCP connections and initial pure-Go Linux
USB connections to emulators and authorized devices. Existing ADB RSA private
keys can be supplied explicitly for devices that require authentication.

> **Status:** v0 is not a full replacement for the official `adb` binary yet.
> It currently implements a small subset: connect, generic service opening,
> shell execution/streaming, and single-file push/pull over TCP or Linux USB.

## Install

For library use, add the module to your Go project:

```sh
go get github.com/dector/adb-go
```

```go
import adb "github.com/dector/adb-go"
```

To install the experimental CLI, use `go install`:

```sh
go install github.com/dector/adb-go/cmd/adb-go@latest
```

From a local checkout, you can also run it without installing:

```sh
go run ./cmd/adb-go help
```

The module uses only the Go standard library for the current TCP and Linux USB
implementation. It does not require the Android SDK, platform-tools, the
official `adb` binary, cgo, libusb, or other native dependencies.

## Quick examples

### Connect

```go
ctx := context.Background()

client, err := adb.Connect(ctx, "127.0.0.1:5555")
if err != nil {
    return err
}
defer client.Close()
```

If the port is omitted, `adb-go` defaults to the standard ADB TCP port `5555`:

```go
client, err := adb.Connect(ctx, "127.0.0.1")
```

### Connect over Linux USB

Linux builds can connect through the kernel usbfs device files under
`/dev/bus/usb`. If exactly one ADB-capable USB interface is visible, pass empty
options:

```go
client, err := adb.ConnectUSB(ctx, adb.USBOptions{})
if err != nil {
    return err
}
defer client.Close()
```

When multiple Android devices are connected, select one explicitly. The most
direct selector is the usbfs path:

```go
client, err := adb.ConnectUSB(ctx, adb.USBOptions{
    DevicePath: "/dev/bus/usb/001/002",
})
```

You can also select by bus/device number or by vendor/product ID:

```go
client, err := adb.ConnectUSB(ctx, adb.USBOptions{
    BusNumber:    1,
    DeviceNumber: 2,
})

client, err = adb.ConnectUSB(ctx, adb.USBOptions{
    VendorID:  0x18d1,
    ProductID: 0x4ee7,
})
```

USB support is Linux-only initially. On other platforms, `ConnectUSB` returns an
error matching `adb.ErrUnsupported`. Serial-number selection is reserved for a
future USB string-descriptor implementation.

### Authenticate with an existing ADB key

If a device replies with `AUTH`, load an existing ADB RSA private key and pass it
to the connection call explicitly:

```go
credential, err := adb.LoadPrivateKey("/home/me/.android/adbkey")
if err != nil {
    return err
}
client, err := adb.ConnectTCPWithOptions(ctx, "127.0.0.1:5555", adb.ConnectOptions{
    AuthCredentials: []adb.AuthCredential{credential},
})
```

Linux USB uses the same credential through `USBOptions`:

```go
client, err := adb.ConnectUSB(ctx, adb.USBOptions{
    DevicePath:      "/dev/bus/usb/001/002",
    AuthCredentials: []adb.AuthCredential{credential},
})
```

Key loading is intentionally explicit. adb-go does not generate keys, persist
keys, search `~/.android`, or integrate with OS keychains in v0. See
[`docs/authentication.md`](docs/authentication.md) for the protocol flow and
supported key formats.

### Run a shell command

`Shell` opens the ADB service string `shell:<command>`, reads until the device
closes the stream, and returns the complete output as bytes.

```go
out, err := client.Shell(ctx, "echo hello")
if err != nil {
    return err
}
fmt.Printf("%s", out)
```

Commands are passed as one shell command string, matching the shape of official
`adb shell` usage. `adb-go` does not add argv-style shell escaping helpers in v0.
Callers are responsible for command contents.

### Stream shell output

Use `ShellStream` when output may be large or should be displayed as it arrives:

```go
err := client.ShellStream(ctx, "logcat -d", os.Stdout)
if err != nil {
    return err
}
```

### Push one file

```go
err := client.PushFile(ctx, "./local.txt", "/data/local/tmp/local.txt")
if err != nil {
    return err
}
```

`PushFile` implements ADB's `sync:` service for a single file. The default remote
mode is `0644`. Directory-aware push and custom mode/mtime options are reserved
for future APIs.

### Pull one file

```go
err := client.PullFile(ctx, "/data/local/tmp/remote.txt", "./remote.txt")
if err != nil {
    return err
}
```

`PullFile` fails if the local destination already exists. To overwrite
deliberately, use `PullFileWithOptions`:

```go
err := client.PullFileWithOptions(ctx,
    "/data/local/tmp/remote.txt",
    "./remote.txt",
    adb.PullOptions{Overwrite: true},
)
```

### Open a raw service

Advanced callers can open any supported device service directly:

```go
stream, err := client.OpenService(ctx, "shell:uname -a")
if err != nil {
    return err
}
defer stream.Close()

_, err = io.Copy(os.Stdout, stream)
```

## CLI

`adb-go` includes an experimental CLI named `adb-go`. The CLI is intentionally a
thin wrapper around the library's supported high-level operations, not a full
clone of the official `adb` command.

Every v0 CLI operation targets one explicit ADB endpoint. For TCP, pass
`--addr HOST[:PORT]`:

```sh
adb-go targets
adb-go shell --addr 127.0.0.1:5555 echo hello
adb-go push --addr 127.0.0.1:5555 ./local.txt /data/local/tmp/local.txt
adb-go pull --addr 127.0.0.1:5555 /data/local/tmp/remote.txt ./remote.txt
```

If the port is omitted, the library connection path defaults to the standard ADB
TCP port `5555`, so `--addr 127.0.0.1` means `127.0.0.1:5555`.

As a convenience for repeated TCP commands, you may set `ADB_GO_ADDR` instead of
passing `--addr` each time. An explicit `--addr` always takes precedence:

```sh
export ADB_GO_ADDR=127.0.0.1:5555
adb-go shell echo hello
adb-go push ./local.txt /data/local/tmp/local.txt
adb-go pull --overwrite /data/local/tmp/remote.txt ./remote.txt
```

`adb-go targets` is an adb-go-specific alternative to `adb devices`. It does
not query the official adb server and does not try to reproduce the official
state list. Instead, it prints connection selectors that adb-go itself can use:
`ADB_GO_ADDR` as an explicit TCP target, optionally scanned local emulator TCP
ports with `adb-go targets --scan`, plus locally discovered Linux USB ADB
interfaces when available.

`--scan` probes localhost emulator ADB ports `5555..5585`, odd ports only, with
a short timeout. This is useful for emulator serials such as `emulator-5554`,
whose ADB TCP endpoint is normally `127.0.0.1:5555`.

On Linux, pass USB selection flags instead of `--addr`:

```sh
adb-go shell --usb getprop ro.product.model
adb-go shell --usb-path /dev/bus/usb/001/002 getprop ro.product.model
adb-go push --usb-bus 1 --usb-device 2 ./local.txt /data/local/tmp/local.txt
adb-go pull --usb-vid 18d1 --usb-pid 4ee7 /data/local/tmp/remote.txt ./remote.txt
```

`--usb` requests USB discovery without narrowing selection. It succeeds only
when exactly one ADB USB device is visible. Use `--usb-path`,
`--usb-bus`/`--usb-device`, or `--usb-vid`/`--usb-pid` to choose a device when
more than one match exists. USB flags cannot be combined with `--addr`.
`--serial` is accepted as a reserved selector but currently returns an
unsupported error because USB serial string descriptors are not implemented yet.

For authenticated TCP or USB devices, provide an existing ADB private key with
`--auth-key`:

```sh
adb-go shell --auth-key ~/.android/adbkey --addr 127.0.0.1:5555 getprop ro.product.model
adb-go shell --auth-key ~/.android/adbkey --usb-path /dev/bus/usb/001/002 getprop ro.product.model
```

The CLI does not create or modify key files.

`adb-go shell` joins all remaining arguments with spaces and sends the result as
one shell command string, matching the library API and the common `adb shell`
shape. For example, this opens the ADB service string
`shell:pm list packages`:

```sh
adb-go shell --addr 127.0.0.1:5555 pm list packages
adb-go shell --usb-path /dev/bus/usb/001/002 pm list packages
```

`adb-go push` and `adb-go pull` transfer exactly one file. Pull refuses to
replace an existing local destination unless `--overwrite` is provided.

`adb-go targets` output is designed to be copied back into the other commands:

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

## Current limitations and differences from official adb

`adb-go` intentionally supports only a small v0 subset:

- Transport support is limited to explicit TCP endpoints and Linux USB via
  `/dev/bus/usb`; macOS and Windows USB are not implemented yet.
- ADB authentication requires explicit existing RSA key files. adb-go does not
  generate keys, discover default key locations, manage key authorization, or
  integrate with OS keychains yet. Without supplied credentials, `AUTH` still
  returns `adb.ErrAuthRequired`.
- No broad device discovery, server management, or official `adb devices`
  compatibility in v0. The `adb-go targets` command is an adb-go-specific
  listing of usable selectors, not a clone of the official adb server's device
  state list.
- TCP connects only to an explicit device address supplied by the caller or, for
  the CLI, by the `ADB_GO_ADDR` environment variable. USB requires either a
  single visible ADB USB device or explicit USB selection options/flags.
- Incomplete command coverage: library users can run shell commands, stream
  shell output, push one file, pull one file, and open generic services; CLI
  users currently have `shell`, `push`, and `pull`.
- The CLI is not a complete `adb` replacement. Broad command compatibility such
  as `devices`, `install`, `logcat` as a dedicated command, server management,
  wireless pairing, forwarding, and most official flags are not implemented.
- Push and pull are explicit single-file APIs. Directory-aware behavior is
  reserved for future `Push`/`Pull` style APIs.

## Architecture

The codebase is split into a small set of packages:

- Root package `github.com/dector/adb-go` re-exports the stable high-level API
  from `client` for normal users.
- Package `auth` loads explicit ADB RSA private keys and prepares public-key
  payloads for the ADB authentication exchange.
- Package `client` handles TCP dialing, Linux USB dialing and USB candidate
  listing, the initial ADB `CNXN`/`AUTH` handshake, service opening, shell
  helpers, and the single-file `sync:` push/pull helpers.
- Package `protocol` contains lower-level ADB packet primitives, connection
  handshake support, and stream demultiplexing. It is useful for tests,
  debugging, and advanced protocol work, but it may be less stable than the
  root/client API during v0 development.
- Package `internal/usb` contains the Linux usbfs discovery and bulk endpoint
  transport implementation behind Linux build tags, plus unsupported-platform
  stubs for other operating systems.
- Package `internal/fakeadb` is an in-process fake ADB server used by tests.

TCP and USB share the same ADB protocol layer. The selected transport only
provides a raw `io.ReadWriteCloser`; after that, `protocol.NewConnection` sends
and receives normal ADB packets over the byte stream.

At a high level, an ADB session works like this:

1. The client opens a byte transport: either a TCP socket to the device/emulator
   or a claimed Linux USB interface with bulk IN and bulk OUT endpoints.
2. The client and device exchange `CNXN` packets to establish protocol-level
   connectivity. If the device sends `AUTH TOKEN`, adb-go signs the token with a
   supplied RSA key, may offer the corresponding public key, and waits for the
   final device `CNXN`.
3. The client sends an `OPEN` packet containing a service string such as
   `shell:echo ok` or `sync:`.
4. The peer acknowledges the logical stream with `OKAY`.
5. Stream payloads flow in `WRTE` packets and each side eventually closes with
   `CLSE`.

For file transfer, `adb-go` opens the `sync:` service and then sends smaller
sync protocol records such as `RECV`, `SEND`, `DATA`, `DONE`, `OKAY`, and
`FAIL` inside the ADB stream.

## Linux USB permissions and troubleshooting

The Linux USB backend opens device files such as `/dev/bus/usb/001/002` and
claims the ADB interface with usbfs ioctls. If your user cannot read and write
that device node, connection fails before the ADB handshake begins.

Common permission setup options are:

- Run as root temporarily for a quick hardware test.
- Install a udev rule that grants a development group access to your Android
  device vendor ID.
- Add your user to that group, then re-login, replug the device, or reload udev
  rules so the new permissions apply.

A typical udev rule shape is:

```text
SUBSYSTEM=="usb", ATTR{idVendor}=="18d1", MODE="0660", GROUP="plugdev", TAG+="uaccess"
```

Google devices often use vendor ID `18d1`; other manufacturers use different
IDs. If discovery finds no candidates, check that USB debugging is enabled, the
USB mode exposes an ADB interface, the device is visible under `/dev/bus/usb`,
and your udev rule matches the actual vendor ID. If the handshake fails with
`adb.ErrAuthRequired`, the transport worked but the device requires ADB RSA
authentication and no trusted explicit key completed the challenge.

## Security and trust

`adb-go` behaves like `adb`: callers control commands and paths. Shell commands
and file paths may affect the connected device. ADB private keys are sensitive:
a trusted key can authorize host access to a device. The library does not add
command or path allowlists/denylists, does not log by default, and requires
`context.Context` for blocking public operations so callers can set deadlines or
cancel work.

## Testing

Run the normal unit and example test suite with:

```sh
go test ./...
```

Optional integration tests are skipped by default. To run them against an
emulator or TCP-enabled device that is already authorized or otherwise accepts
unauthenticated ADB TCP connections, set `ADB_GO_INTEGRATION_ADDR`:

```sh
ADB_GO_INTEGRATION_ADDR=127.0.0.1:5555 go test ./...
```

There is also an optional instrumented integration test that starts the
project's containerized Linux `adbd` and exercises the implemented high-level
workflows against a real daemon: connect, shell, shell streaming, raw service
opening, single-file push, and single-file pull. The test prefers Podman and
falls back to Docker; set `ADB_GO_CONTAINER_RUNTIME` to choose explicitly.

```sh
# Build the local adbd image once, then run the containerized test.
ADB_GO_CONTAINER_INTEGRATION=1 ADB_GO_CONTAINER_BUILD=1 go test -timeout 30m ./...

# Reuse an already-built image on later runs.
ADB_GO_CONTAINER_INTEGRATION=1 go test ./...
```

The default container image name is `adb-go-linux-adbd`; override it with
`ADB_GO_CONTAINER_IMAGE` when needed.

Linux USB integration testing is also opt-in and requires Linux plus a connected
ADB-capable USB device that your user can open through `/dev/bus/usb`:

```sh
ADB_GO_USB_INTEGRATION=1 go test ./...
```

The USB integration test discovers ADB USB interfaces, opens the first
candidate's bulk endpoints, and performs the ADB handshake. Devices that answer
with `AUTH` are reported as authentication-required unless explicit credentials
are added to the test in a future integration slice.

## Roadmap

Preferred post-v0 direction:

1. Continue growing the official CLI as a thin wrapper around supported library
   operations.
2. Continue improving ADB authentication ergonomics, including optional key
   generation/discovery if the project later chooses to manage keys.
3. Expand USB support beyond the initial Linux usbfs backend where feasible.
