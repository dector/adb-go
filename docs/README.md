# adb-go implementation notes

This directory contains cross-cutting documentation for adb-go internals and
transport behavior.

## Contents

- [Documents](#documents)
- [Architecture](#architecture)
- [Protocol flow](#protocol-flow)
- [Security and trust](#security-and-trust)
- [Current implementation boundaries](#current-implementation-boundaries)

## Documents

- [`authentication.md`](authentication.md) - ADB `AUTH` flow, supported key
  formats, and explicit credential handling.
- [`server-foundation.md`](server-foundation.md) - `adb-gos` Unix socket path,
  control protocol, lifecycle semantics, and initial non-goals.
- [`forwarding-design.md`](forwarding-design.md) - direct-device forwarding
  design, official `adb forward` differences, and proposed library/CLI shape.
- [`integration-testing.md`](integration-testing.md) - optional TCP, containerized
  Linux `adbd`, and Linux USB integration test workflows and troubleshooting.
- [`linux-usb-transport.md`](linux-usb-transport.md) - Linux usbfs discovery,
  endpoint selection, permissions, and transport details.
- [`persistent-forwarding-design.md`](persistent-forwarding-design.md) - future
  server-owned forwarding representation, server protocol additions, CLI shape,
  and lifecycle semantics.

## Architecture

The codebase is split into a small set of packages:

- Root package `github.com/dector/adb-go` re-exports the stable high-level API
  from `client` for normal users.
- Package `auth` loads explicit ADB RSA private keys and prepares public-key
  payloads for the ADB authentication exchange.
- Package `client` handles TCP dialing, Linux USB dialing and USB candidate
  listing, the initial ADB `CNXN`/`AUTH` handshake, service opening, shell
  helpers, and single-file `sync:` push/pull helpers.
- Package `protocol` contains lower-level ADB packet primitives, connection
  handshake support, and stream demultiplexing. It is useful for tests,
  debugging, and advanced protocol work, but it may be less stable than the
  root/client API during v0 development.
- Future command package `cmd/adb-gos` will contain the minimal server process
  described in [`server-foundation.md`](server-foundation.md). The first server
  slice is only a local control process and does not own devices, transports,
  forwards, sessions, or authentication state.
- Package `internal/usb` contains the Linux usbfs discovery and bulk endpoint
  transport implementation behind Linux build tags, plus unsupported-platform
  stubs for other operating systems.
- Package `internal/fakeadb` is an in-process fake ADB server used by tests.

TCP and USB share the same ADB protocol layer. The selected transport only
provides a raw `io.ReadWriteCloser`; after that, `protocol.NewConnection` sends
and receives normal ADB packets over the byte stream.

## Protocol flow

At a high level, an ADB session works like this:

1. The client opens a byte transport: either a TCP socket to the device/emulator
   or a claimed Linux USB interface with bulk IN and bulk OUT endpoints.
2. The client and device exchange `CNXN` packets to establish protocol-level
   connectivity.
3. If the device sends `AUTH TOKEN`, adb-go signs the token with a supplied RSA
   key, may offer the corresponding public key, and waits for the final device
   `CNXN`.
4. The client sends an `OPEN` packet containing a service string such as
   `shell:echo ok` or `sync:`.
5. The peer acknowledges the logical stream with `OKAY`.
6. Stream payloads flow in `WRTE` packets and each side eventually closes with
   `CLSE`.

For file transfer, adb-go opens the `sync:` service and then sends smaller sync
protocol records such as `RECV`, `SEND`, `DATA`, `DONE`, `OKAY`, and `FAIL`
inside the ADB stream.

## Security and trust

adb-go behaves like adb: callers control commands and paths. Shell commands and
file paths may affect the connected device.

ADB private keys are sensitive. A trusted key can authorize host access to a
device. The library does not add command or path allowlists/denylists, does not
log by default, and requires `context.Context` for blocking public operations so
callers can set deadlines or cancel work.

## Current implementation boundaries

- The project does not use or manage the official adb server.
- TCP connections target an explicit device/emulator endpoint.
- USB support is implemented through Linux usbfs and is not yet available on
  macOS or Windows.
- Authentication uses explicitly supplied credentials only.
- Push and pull are single-file APIs; directory-aware behavior is reserved for
  future APIs.
- Forwarding supports foreground process-scoped local listeners and in-memory
  server-owned TCP listeners. Server-owned forwards are not durable across
  `adb-gos` restarts and are described in
  [`persistent-forwarding-design.md`](persistent-forwarding-design.md).
- The `adb-gos` server defines a Unix socket control channel for ping, status,
  shutdown, diagnostics, service management, and in-memory TCP forwarding. It
  does not yet persist ADB device/session/authentication state.
