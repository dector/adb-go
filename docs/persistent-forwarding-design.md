# Daemon-backed persistent forwarding design

This document records the M56 design for future daemon-owned forwarding. It is a
documentation-only milestone: the existing `adb-go forward` command remains a
foreground, process-scoped local TCP forward, and `adb-god` still does not own
ADB devices, transports, or forwards until later implementation milestones add
that behavior.

## Background

Today adb-go forwarding is intentionally direct and foreground-only. The CLI or
library process binds the host listener, connects directly to one selected ADB
target, and opens a fresh device `tcp:PORT` service for each accepted host
connection. When that process exits, the listener and active bridges disappear.

Persistent forwarding needs a different owner. The only adb-go process designed
to outlive a CLI invocation is `adb-god`, so background forwards should be
represented as daemon state and controlled through explicit daemon protocol
commands. This keeps adb-go honest about lifetime: a forward is persistent only
when the daemon owns the listener and the target connection policy.

## Goals

- Add a concrete design for daemon-owned forwards that can keep listening after
  `adb-go forward --background ...` exits.
- Preserve the current foreground `adb-go forward LOCAL REMOTE` behavior.
- Define how forwards are represented, listed, removed, and diagnosed.
- Define daemon protocol extensions without implementing them yet.
- Keep target selection explicit; do not introduce broad adb-server-compatible
  discovery or global transport ownership in this design.

## Non-goals

- No implementation in this milestone.
- No compatibility promise for the official ADB server smart-socket protocol.
- No reverse forwarding.
- No host Unix sockets, JDWP, vsock, Android local socket namespaces, or raw
  advanced service targets in the first persistent-forwarding slice.
- No daemon-managed authentication key discovery or persistence. Any future
  authenticated persistent forward must use explicit credentials or a separately
  designed auth store.

## Forward representation

The daemon should store a small in-memory table of forwarding registrations. A
registration describes desired listener state, not just one active connection.

Suggested logical fields:

```json
{
  "id": "fwd_01J...",
  "state": "listening",
  "local": {"network":"tcp", "address":"127.0.0.1:9000"},
  "remote": {"service":"tcp:9000"},
  "target": {"transport":"tcp", "address":"127.0.0.1:5555"},
  "norebind": false,
  "createdAt": "2026-07-26T12:00:00Z",
  "activeConnections": 0,
  "lastError": ""
}
```

Definitions:

- `id` is an adb-go-generated stable identifier for later removal. It should be
  opaque to callers.
- `local` is the host-side listener. The first implementation should support
  TCP only and should bind loopback by default for `tcp:PORT` CLI syntax.
- `remote` is the ADB service opened on the device for each accepted host
  connection. The first implementation should support only `tcp:PORT`.
- `target` is the explicit ADB target selection needed to reconnect or open
  streams. Initially this can be TCP-only; USB persistence should be designed as
  a separate slice because device paths and permissions can change across
  reconnects.
- `state` should distinguish at least `listening`, `degraded`, and `stopped` in
  daemon responses. Removed forwards do not need to stay in the table.
- `lastError` is diagnostic text for the most recent listener, target, or bridge
  setup failure. It is informational and not a stable API field.

The table should be in-memory for the first daemon-backed implementation.
Systemd can keep `adb-god` running, but restarting the daemon intentionally drops
all registered forwards unless a later milestone designs durable state.

## CLI command shape

The existing foreground command remains unchanged:

```sh
adb-go forward --addr 127.0.0.1:5555 tcp:9000 tcp:9000
```

Persistent forwarding should be opt-in with an explicit background flag:

```sh
adb-go forward --background --addr 127.0.0.1:5555 tcp:9000 tcp:9000
adb-go forward --background --norebind --addr 127.0.0.1:5555 tcp:9000 tcp:9000
```

Behavior:

- Without `--background`, the command continues to run in the foreground exactly
  as it does today.
- With `--background`, the CLI sends a daemon request and exits after the daemon
  has bound the listener or returned a setup error.
- `--norebind` rejects creation when another daemon-owned forward already uses
  the same local endpoint. Without `--norebind`, adb-go may replace an existing
  daemon-owned forward for that local endpoint, but it must never steal an
  unrelated listener owned by another process.
- If the local port is `tcp:0`, the daemon binds an ephemeral port and returns
  the actual address so the CLI can print it.

Listing and removal should be daemon-backed commands, not foreground-client
commands:

```sh
adb-go forward --list
adb-go forward --remove tcp:9000
adb-go forward --remove-id fwd_01J...
adb-go forward --remove-all
```

Listing output should be human-readable by default and include enough data for
support requests:

```text
ID          LOCAL             REMOTE    TARGET              STATE      ACTIVE
fwd_01JABC 127.0.0.1:9000    tcp:9000  tcp:127.0.0.1:5555  listening  2
```

Removal semantics:

- `--remove LOCAL` removes the daemon-owned registration for the local endpoint.
- `--remove-id ID` removes exactly one registration by daemon ID and is safest
  for scripts.
- `--remove-all` closes all daemon-owned forwarding listeners.
- Removing a forward closes its listener and all active bridged connections.
- Removing a missing forward should return a clear not-found error.

The command should make daemon dependence visible. If `adb-god` is unreachable,
background/list/remove operations should fail with a hint to run
`adb-go daemon doctor` or start/install the daemon service.

## Daemon protocol additions

The daemon control protocol can remain newline-delimited JSON with
`version: 1` if these commands are added as optional request/response shapes.
Older daemons will return `unknown_command`, which the CLI can turn into a clear
"daemon is too old" message.

Suggested commands:

- `forward_create`
- `forward_list`
- `forward_remove`
- `forward_remove_all`

### `forward_create`

Request:

```json
{
  "version": 1,
  "id": "c1",
  "command": "forward_create",
  "params": {
    "local": {"network":"tcp", "address":"127.0.0.1:9000"},
    "remote": {"service":"tcp:9000"},
    "target": {"transport":"tcp", "address":"127.0.0.1:5555"},
    "norebind": false
  }
}
```

Successful response:

```json
{
  "version": 1,
  "id": "c1",
  "ok": true,
  "result": {
    "forward": {
      "id": "fwd_01JABC",
      "state": "listening",
      "local": {"network":"tcp", "address":"127.0.0.1:9000"},
      "remote": {"service":"tcp:9000"},
      "target": {"transport":"tcp", "address":"127.0.0.1:5555"},
      "activeConnections": 0
    }
  }
}
```

Creation should bind the local listener before returning success. A bind failure
or target-selection validation failure returns `ok:false` and a stable error
code such as `address_in_use`, `unsupported_endpoint`, `bad_target`, or
`rebind_disallowed`.

### `forward_list`

Request:

```json
{"version":1,"id":"l1","command":"forward_list"}
```

Successful response includes all current daemon-owned forwards:

```json
{
  "version": 1,
  "id": "l1",
  "ok": true,
  "result": {"forwards": []}
}
```

### `forward_remove`

Request by ID:

```json
{"version":1,"id":"r1","command":"forward_remove","params":{"id":"fwd_01JABC"}}
```

Request by local endpoint:

```json
{
  "version": 1,
  "id": "r2",
  "command": "forward_remove",
  "params": {"local":{"network":"tcp", "address":"127.0.0.1:9000"}}
}
```

The daemon should require exactly one selector. Success means the listener has
been closed and active bridges have been asked to close.

### `forward_remove_all`

Request:

```json
{"version":1,"id":"ra1","command":"forward_remove_all"}
```

The response should include a count of removed forwards.

## Runtime flow

For each daemon-owned registration:

1. The daemon binds the local TCP listener.
2. For each accepted host connection, the daemon dials or reuses the selected
   ADB target according to the implementation slice.
3. The daemon opens the configured remote service, such as `tcp:9000`.
4. The daemon copies bytes in both directions until one side closes or errors.
5. Connection-level failures close only that host connection. Listener-level
   failures move the registration to `degraded` or remove it, depending on the
   failure.

The first implementation should prefer simple correctness over pooling. Opening
one ADB connection or stream per accepted host connection is easier to reason
about and avoids daemon-wide device lifecycle ownership. Later milestones can
add connection reuse if tests prove it is safe.

## Lifecycle semantics

### Daemon shutdown and restart

On graceful shutdown, `adb-god` closes every forwarding listener and active
bridge before exiting. Because the initial table is in-memory, a daemon restart
starts with no forwards. This must be documented in CLI help and `--list` output
should not imply durable state.

### Systemd restart

A systemd user-service restart has the same effect as daemon restart: all
forwards are dropped. Users who need forwards recreated after login or restart
should use their own service units or scripts until adb-go explicitly designs
persistent on-disk configuration.

### Target disconnects

A failed per-connection target dial or ADB stream open should reject that host
connection and update `lastError`, but the listener may remain active so later
connections can succeed after the device returns. If repeated failures occur,
the daemon may report `state: degraded` while keeping the listener bound.

### Port conflicts

If another process owns the requested local address, creation fails with
`address_in_use`. If another daemon-owned forward owns it:

- `norebind: true` fails with `rebind_disallowed`.
- `norebind: false` may replace the old daemon-owned registration after closing
  its listener and active bridges.

The daemon must not remove or alter listeners it does not own.

## Security and supportability

- Bind loopback for shorthand CLI endpoints. Users must opt in explicitly to
  wider bind addresses if a later CLI syntax supports them.
- Treat remote services and target addresses as caller-controlled input; do not
  add allowlists beyond the endpoint families supported by the specific
  milestone.
- Do not log forwarded payload bytes.
- Include forwarding counts in future daemon diagnostics, but keep detailed
  mappings in `forward --list` so `daemon status` remains concise.
- Keep errors actionable and stable enough for CLI handling: unsupported
  endpoint, daemon too old, daemon unavailable, address in use, rebind
  disallowed, forward not found, and target connection failed.

## Suggested implementation slices

1. Add daemon protocol request/response types and tests for forwarding commands
   without opening listeners.
2. Implement TCP listener ownership in `adb-god` with create/list/remove/remove
   all over loopback TCP endpoints.
3. Bridge accepted connections to direct TCP ADB targets and `tcp:PORT` remote
   services.
4. Add CLI `forward --background`, `--list`, `--remove`, `--remove-id`, and
   `--remove-all` wired to the daemon protocol.
5. Revisit USB target persistence, broader endpoint families, and optional
   durable forward restoration as separate designs.
