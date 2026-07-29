# adb-go Specification

## Summary

`adb-go` is a pure-Go implementation of the Android Debug Bridge (ADB) protocol. It is primarily a Go library for embedding ADB behavior in applications. The long-term goal is to support an official CLI built on the library that can eventually replace the official `adb` binary for supported workflows.

The module must not require the Android SDK, platform-tools, the official `adb` binary, cgo, libusb, or other native dependencies for the v0 TCP implementation.

## Module and packages

- Module path: `github.com/dector/adb-go`
- Root package name: `adb`
- License: MIT
- Minimum Go version: latest stable Go at project start
- Dependencies: standard library only for v0 unless there is a strong reason otherwise

Initial package layout:

```text
.
├── adb.go                 # root package; re-exports high-level API
├── client/                # high-level client API
├── protocol/              # low-level ADB protocol API
└── internal/fakeadb/      # in-process fake ADB daemon/server for tests
```

## Scope for v0

v0 is a practical MVP for TCP ADB:

- TCP connections only
- No USB support yet
- No ADB authentication implementation yet
- Targets emulators and authorized/insecure test devices
- Supports explicit device address connection only
- No device discovery/listing in v0
- Supports shell execution
- Supports shell streaming
- Supports single-file push and pull
- Supports generic service opening for advanced users

README must clearly state that v0 is not a full `adb` replacement yet and must list current limitations and differences from official `adb`, including TCP-only operation, no auth, no USB, and incomplete command coverage.

## Transport

v0 uses TCP only.

- `Connect` accepts either `host` or `host:port`
- If the port is omitted, default to ADB TCP port `5555`
- TCP dialing uses standard `net.Dialer` with `context.Context`
- Avoid platform-specific networking behavior
- TCP mode should support Linux, macOS, and Windows

USB is deferred until after the pure-Go TCP protocol implementation is working. The design should keep transport seams open for future USB work.

## Authentication

ADB authentication is deferred for v0.

- Add TODOs and design seams for future RSA authentication
- Do not persist or manage keys/config in v0
- The client is stateless except for user-requested push/pull filesystem paths
- If a device replies with `AUTH`:
  - low-level protocol callers can observe/expose AUTH packets
  - high-level client returns a clear sentinel error such as `ErrAuthRequired`

## High-level API

The root `adb` package re-exports the high-level API implemented in `client`.

Initial high-level operations:

```go
Connect(ctx context.Context, addr string) (*Client, error)
ConnectTCP(ctx context.Context, addr string) (*Client, error) // optional explicit alias
(*Client).Close() error
(*Client).Shell(ctx context.Context, cmd string) ([]byte, error)
(*Client).ShellStream(ctx context.Context, cmd string, stdout io.Writer) error
(*Client).PushFile(ctx context.Context, localPath, remotePath string) error
(*Client).PullFile(ctx context.Context, remotePath, localPath string) error
(*Client).PullFileWithOptions(ctx context.Context, remotePath, localPath string, opts PullOptions) error
(*Client).OpenService(ctx context.Context, service string) (io.ReadWriteCloser, error)
```

API decisions:

- Shell commands are a single command string like official `adb`, e.g. `Shell(ctx, "pm list packages")`
- Do not add argv-style shell helpers in v0
- `PushFile`/`PullFile` are explicit single-file methods
- Reserve `Push`/`Pull` names for possible future directory-aware behavior
- `PushFile` uses default remote mode `0644`
- Custom push mode/mtime can be added later through advanced options
- `PullFile` must return an error if the destination exists
- Overwrite requires explicit options, e.g. `PullFileWithOptions(..., PullOptions{Overwrite: true})`
- Every blocking public operation accepts `context.Context`
- Cancellation should close the stream/connection when needed to unblock reads/writes

## Low-level protocol API

The `protocol` package exposes both packet primitives and stream abstractions.

Packet-level API:

- Export ADB command constants such as `CommandCNXN`, `CommandOPEN`, etc.
- Expose raw message fields for protocol fidelity:
  - `Command`
  - `Arg0`
  - `Arg1`
  - `Payload`
- Provide helper methods or higher-level stream APIs for semantic meanings such as local/remote stream IDs
- Provide message encode/decode helpers such as `ReadMessage` and `WriteMessage`
- Checksum validation is strict by default and returns errors for invalid packets
- Optional debug/compat behavior may be added later if needed

Connection/stream model:

- High-level client performs TCP dial and ADB `CNXN` handshake before returning
- Low-level protocol exposes manual raw connection and handshake steps for testing/debugging
- Implement ADB stream demultiplexing if not too difficult:
  - one reader goroutine reads packets from the TCP connection
  - packets are routed to streams by local/remote IDs
  - writes are serialized through a shared writer lock
- High-level concurrent operations can remain experimental in v0
- Low-level streams should implement `io.Reader`, `io.Writer`, and `io.Closer` where practical
- Support generic service strings through `OpenService(ctx, service string)`

The `protocol` package docs should state that it is lower-level and less stable than the root/client API during v0. This should be documentation only, not runtime warnings or awkward API markers.

## Error handling

Public errors should include sentinel errors where useful, wrapped with context using `%w`.

Initial sentinel errors may include:

- `ErrAuthRequired`
- `ErrUnsupported`
- `ErrDeviceClosed`
- destination-exists error for pull without overwrite

Callers should be able to use `errors.Is`.

## Security and trust

The library behaves like `adb`: callers are responsible for commands and paths.

Rules:

- Do not add command/path allowlists or denylists in v0
- Require `context.Context` for blocking operations
- Avoid shell interpolation helpers that imply safety
- Document that shell commands and file paths are caller-controlled and may affect the connected device
- Do not log by default
- Optional protocol tracing/debug hooks may be added later

## Testing

Testing strategy:

- Unit tests for packet encoding/decoding and checksums
- Unit tests for stream lifecycle and demux behavior
- In-process fake ADB daemon/server under `internal/fakeadb` for reliable tests
- Optional integration tests against a real emulator/device

Integration tests:

- Gated by env var `ADB_GO_INTEGRATION_ADDR`
- Example: `ADB_GO_INTEGRATION_ADDR=127.0.0.1:5555`
- Skipped when unset

CI is deferred until the initial implementation compiles locally. Later CI should run `go test ./...` on Linux, macOS, and Windows.

## Documentation

README should include:

- Project purpose
- Installation/import instructions
- Quick examples for connect, shell, push, pull
- Current limitations and differences from official `adb`
- Architecture section describing root `adb`, `client`, `protocol`, and future `auth`/USB/CLI roadmap
- Warning that v0 is not a full `adb` replacement yet

Add compile-tested Go examples where possible, such as `ExampleConnect` and `ExampleClient_Shell`, to protect against API drift.

## Future roadmap

Preferred post-v0 priority order:

1. Official CLI thin wrapper around the library
2. ADB authentication
3. USB support

First CLI should be a thin wrapper around supported library operations:

- `connect`
- `shell`
- `push`
- `pull`

It should not try to mimic every `adb` command/flag from day one. It can gradually converge toward `adb` compatibility as command coverage grows.
