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
  - [`targets`](#targets)
  - [`shell`](#shell)
  - [`push` and `pull`](#push-and-pull)
- [Limitations](#limitations)
- [Testing](#testing)

## Install

```sh
go install github.com/dector/adb-go/cmd/adb-go@latest
```

From a local checkout, run it without installing:

```sh
go run ./cmd/adb-go help
```

## Target selection

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

As a convenience for repeated TCP commands, set `ADB_GO_ADDR` instead of passing
`--addr` each time. An explicit `--addr` always takes precedence:

```sh
export ADB_GO_ADDR=127.0.0.1:5555
adb-go shell echo hello
adb-go push ./local.txt /data/local/tmp/local.txt
adb-go pull --overwrite /data/local/tmp/remote.txt ./remote.txt
```

## USB targets

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

## Authentication

For authenticated TCP or USB devices, provide an existing ADB private key with
`--auth-key`:

```sh
adb-go shell --auth-key ~/.android/adbkey --addr 127.0.0.1:5555 getprop ro.product.model
adb-go shell --auth-key ~/.android/adbkey --usb-path /dev/bus/usb/001/002 getprop ro.product.model
```

The CLI does not create or modify key files.

## Commands

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

## Limitations

The CLI is not a complete `adb` replacement. Broad command compatibility such as
`devices`, `install`, `logcat` as a dedicated command, server management,
wireless pairing, forwarding, and most official flags are not implemented.

## Testing

From the repository root:

```sh
go test ./cmd/adb-go ./...
```

Optional end-to-end workflows are covered by the repository integration tests;
see [`../../client/README.md`](../../client/README.md#testing-and-integration).
