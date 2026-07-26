# adb-god daemon foundation

This document defines the first `adb-god` daemon slice. The purpose of this
slice is deliberately small: create a daemon process and a Unix domain socket
control channel that future milestones can build on. The daemon defined here is
only a controllable process. It does not yet own ADB devices, transports,
forwarding registrations, authentication state, sessions, or any other
persistent adb-go feature state.

## Goals

- Add one daemon process named `adb-god`.
- Give local adb-go tools a concrete Unix domain socket path for daemon control.
- Define a minimal, versioned request/response protocol for process control.
- Make startup, stale-socket handling, status checks, and shutdown behavior
  deterministic enough to implement and test in isolation.

## Non-goals

The initial daemon must not implement or imply any persistent ADB behavior.
Specifically, it does not:

- discover, claim, connect to, or remember devices;
- own TCP or USB transports;
- keep shell, sync, install, logcat, screencap, reboot, or forward sessions
  alive after a CLI command exits;
- replace the official adb server protocol;
- manage authentication keys or authorization prompts;
- expose a network listener; or
- provide multi-user access control beyond relying on a per-user local Unix
  socket path and filesystem permissions.

Those capabilities can be designed as later protocol extensions once the daemon
process and control path are stable.

## Socket path

The daemon control socket is a Unix domain socket. The same path selection logic
must be used by `adb-god` and by `adb-go daemon ...` clients.

Path selection order:

1. If `ADB_GO_DAEMON_SOCKET` is set, use that exact path.
   - This is the supported override for tests, development builds, non-default
     installations, and packagers.
   - The value must be an absolute path. Implementations should reject an empty
     or relative value with a clear error.
2. Otherwise, if `XDG_RUNTIME_DIR` is set to an absolute path, use
   `$XDG_RUNTIME_DIR/adb-go/adb-god.sock`.
3. Otherwise, use `$TMPDIR/adb-go-$UID/adb-god.sock`, where `$TMPDIR` means
   Go's `os.TempDir()` result and `$UID` is the current Unix user ID.

Before binding, `adb-god` creates the parent directory with mode `0700` when it
is missing. The socket file itself should be accessible only through that
per-user directory. This keeps the first slice simple while still avoiding a
world-writable shared control socket.

The override variable is intentionally environment-based rather than stored in a
config file. The v0 daemon remains stateless, and tests can run isolated daemon
instances by setting `ADB_GO_DAEMON_SOCKET` to a path inside a temporary
directory.

## Startup and stale socket handling

On startup, `adb-god` owns exactly one socket path.

If the socket path does not exist, the daemon binds it and starts serving.

If the path already exists:

1. Try to connect to it as an adb-go daemon socket.
2. If a daemon responds to the version 1 control protocol, startup fails with an
   "already running" error.
3. If connecting fails in a way that indicates there is no listener, treat the
   path as stale, remove it, and bind a new socket.
4. If the path exists but is not a Unix socket, do not remove it. Fail with a
   clear error so adb-go never deletes an unrelated user file.

On graceful shutdown, `adb-god` closes the listener and removes the socket file
it created. Cleanup should be best-effort, but tests should verify that the
normal shutdown path removes the socket.

The initial `adb-god` command runs in the foreground. Backgrounding,
supervision, launchd/systemd integration, and automatic daemon spawning are
future CLI/package concerns, not part of the foundation protocol.

## Control protocol v1

The control channel uses newline-delimited JSON over the Unix domain socket.
Each connection carries one request and one response in the first
implementation. Future implementations may allow multiple request/response pairs
on one connection, but clients should not depend on connection reuse.

All messages are UTF-8 JSON objects followed by `\n`.

### Request

```json
{"version":1,"id":"1","command":"ping"}
```

Fields:

- `version`: protocol version. The foundation version is `1`.
- `id`: optional client-chosen correlation string. When present, the daemon
  echoes it in the response.
- `command`: one of `ping`, `status`, or `shutdown`.

Unknown versions and commands return structured errors rather than closing the
connection silently.

### Response

```json
{"version":1,"id":"1","ok":true,"result":{"message":"pong"}}
```

Fields:

- `version`: daemon protocol version, currently `1`.
- `id`: copied from the request when supplied.
- `ok`: `true` for a successful command and `false` for a protocol or command
  error.
- `result`: command-specific JSON object for successful commands.
- `error`: error object for unsuccessful commands.

Error object fields:

```json
{"code":"unknown_command","message":"unknown daemon command \"devices\""}
```

Error `code` values should be stable enough for CLI decisions. The first slice
needs at least:

- `bad_request` for malformed JSON or missing required fields;
- `unsupported_version` for a protocol version the daemon does not understand;
- `unknown_command` for unsupported commands; and
- `internal_error` for unexpected daemon failures.

### Commands

#### `ping`

Liveness check. It does not inspect devices or daemon state beyond confirming
that the server can parse a request and write a response.

Request:

```json
{"version":1,"id":"p1","command":"ping"}
```

Successful response:

```json
{"version":1,"id":"p1","ok":true,"result":{"message":"pong"}}
```

#### `status`

Returns process metadata that is useful for humans and tests. It must not return
device lists, transport state, forwards, sessions, or authentication state in
this slice.

Successful response shape:

```json
{
  "version": 1,
  "id": "s1",
  "ok": true,
  "result": {
    "state": "running",
    "pid": 12345,
    "socketPath": "/run/user/1000/adb-go/adb-god.sock",
    "protocolVersion": 1,
    "uptimeMillis": 2500
  }
}
```

#### `shutdown`

Requests graceful daemon termination. A successful response means the daemon has
accepted the request and will stop serving new connections, close the listener,
and remove its socket file. The connection may close immediately after the
response is flushed.

Successful response:

```json
{"version":1,"id":"q1","ok":true,"result":{"message":"shutting_down"}}
```

## Compatibility and extension rules

- Keep protocol version `1` while adding optional request fields or optional
  response result fields that older clients can ignore.
- Add new daemon behavior as new commands rather than overloading `status`.
- Do not add persistent device/session state to the `status` response until a
  milestone explicitly designs daemon-owned ADB state.
- Preserve `ADB_GO_DAEMON_SOCKET` as the test and non-default installation
  override even if later CLIs add a `--socket` flag.
