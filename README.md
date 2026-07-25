# adb-go

`adb-go` is a pure-Go implementation of the Android Debug Bridge (ADB) protocol.
It is primarily a Go library for embedding ADB behavior in applications. The
v0 implementation focuses on explicit TCP connections to emulators or already
authorized/insecure devices.

> **Status:** v0 is not a full replacement for the official `adb` binary yet.
> It currently implements a small TCP-only subset: connect, generic service
> opening, shell execution/streaming, and single-file push/pull.

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

## CLI

`adb-go` includes an experimental CLI named `adb-go`. The CLI is intentionally a
thin wrapper around the library's supported high-level operations, not a full
clone of the official `adb` command.

Every v0 CLI operation targets one explicit TCP ADB endpoint. Pass it with
`--addr HOST[:PORT]`:

```sh
adb-go shell --addr 127.0.0.1:5555 echo hello
adb-go push --addr 127.0.0.1:5555 ./local.txt /data/local/tmp/local.txt
adb-go pull --addr 127.0.0.1:5555 /data/local/tmp/remote.txt ./remote.txt
```

If the port is omitted, the library connection path defaults to the standard ADB
TCP port `5555`, so `--addr 127.0.0.1` means `127.0.0.1:5555`.

As a convenience for repeated commands, you may set `ADB_GO_ADDR` instead of
passing `--addr` each time. An explicit `--addr` always takes precedence:

```sh
export ADB_GO_ADDR=127.0.0.1:5555
adb-go shell echo hello
adb-go push ./local.txt /data/local/tmp/local.txt
adb-go pull --overwrite /data/local/tmp/remote.txt ./remote.txt
```

`adb-go shell` joins all remaining arguments with spaces and sends the result as
one shell command string, matching the library API and the common `adb shell`
shape. For example, this opens the ADB service string
`shell:pm list packages`:

```sh
adb-go shell --addr 127.0.0.1:5555 pm list packages
```

`adb-go push` and `adb-go pull` transfer exactly one file. Pull refuses to
replace an existing local destination unless `--overwrite` is provided.

## Current limitations and differences from official adb

`adb-go` intentionally supports only a small v0 subset:

- TCP connections only.
- No USB transport support yet.
- No ADB authentication implementation yet. If a peer replies with `AUTH`, the
  high-level client returns `adb.ErrAuthRequired`.
- No device discovery or device listing in v0.
- Connects only to an explicit device address supplied by the caller or, for the
  CLI, by the `ADB_GO_ADDR` environment variable.
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
`ADB_GO_CONTAINER_IMAGE` when needed. Because v0 does not implement ADB
authentication, devices that answer with `AUTH` will fail with
`adb.ErrAuthRequired` until authentication support is added.

## Roadmap

Preferred post-v0 direction:

1. Continue growing the official CLI as a thin wrapper around supported library
   operations.
2. Implement ADB authentication.
3. Add USB transport support while preserving pure-Go preferences where
   feasible.
