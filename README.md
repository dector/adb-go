# adb-go

`adb-go` is a pure-Go implementation of the Android Debug Bridge (ADB) protocol.
It is primarily a Go library for embedding ADB behavior in applications. The
v0 implementation focuses on explicit TCP connections to emulators or already
authorized/insecure devices.

> **Status:** v0 is not a full replacement for the official `adb` binary yet.
> It currently implements a small TCP-only subset: connect, generic service
> opening, shell execution/streaming, and single-file push/pull.

## Install

```sh
go get github.com/dector/adb-go
```

```go
import adb "github.com/dector/adb-go"
```

The module uses only the Go standard library for the current TCP implementation.
It does not require the Android SDK, platform-tools, the official `adb` binary,
cgo, libusb, or other native dependencies.

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

## Current limitations and differences from official adb

`adb-go` intentionally supports only a small v0 subset:

- TCP connections only.
- No USB transport support yet.
- No ADB authentication implementation yet. If a peer replies with `AUTH`, the
  high-level client returns `adb.ErrAuthRequired`.
- No device discovery or device listing in v0.
- Connects only to an explicit device address supplied by the caller.
- Incomplete command coverage: shell, shell streaming, single-file push,
  single-file pull, and generic service opening are the supported workflows.
- No official CLI yet; the project is currently library-first.
- Push and pull are explicit single-file APIs. Directory-aware behavior is
  reserved for future `Push`/`Pull` style APIs.

## Architecture

The codebase is split into a small set of packages:

- Root package `github.com/dector/adb-go` re-exports the stable high-level API
  from `client` for normal users.
- Package `client` handles TCP dialing, the initial ADB `CNXN` handshake,
  service opening, shell helpers, and the single-file `sync:` push/pull helpers.
- Package `protocol` contains lower-level ADB packet primitives, connection
  handshake support, and stream demultiplexing. It is useful for tests,
  debugging, and advanced protocol work, but it may be less stable than the
  root/client API during v0 development.
- Package `internal/fakeadb` is an in-process fake ADB server used by tests.

At a high level, an ADB TCP session works like this:

1. The client opens a TCP connection to the device or emulator.
2. The client and device exchange `CNXN` packets to establish protocol-level
   connectivity.
3. The client sends an `OPEN` packet containing a service string such as
   `shell:echo ok` or `sync:`.
4. The peer acknowledges the logical stream with `OKAY`.
5. Stream payloads flow in `WRTE` packets and each side eventually closes with
   `CLSE`.

For file transfer, `adb-go` opens the `sync:` service and then sends smaller
sync protocol records such as `RECV`, `SEND`, `DATA`, `DONE`, `OKAY`, and
`FAIL` inside the ADB stream.

## Security and trust

`adb-go` behaves like `adb`: callers control commands and paths. Shell commands
and file paths may affect the connected device. The library does not add command
or path allowlists/denylists, does not log by default, and requires
`context.Context` for blocking public operations so callers can set deadlines or
cancel work.

## Testing

Run the normal unit and example test suite with:

```sh
go test ./...
```

Optional integration tests are skipped by default. To run them, start an emulator
or TCP-enabled device that is already authorized or otherwise accepts unauthenticated
ADB TCP connections, then set `ADB_GO_INTEGRATION_ADDR`:

```sh
ADB_GO_INTEGRATION_ADDR=127.0.0.1:5555 go test ./...
```

The integration test connects with `adb.Connect` and runs a small shell command.
Because v0 does not implement ADB authentication, devices that answer with
`AUTH` will fail with `adb.ErrAuthRequired` until authentication support is added.

## Roadmap

Preferred post-v0 direction:

1. Add an official CLI as a thin wrapper around the supported library operations.
2. Implement ADB authentication.
3. Add USB transport support while preserving pure-Go preferences where
   feasible.
