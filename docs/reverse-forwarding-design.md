# Reverse forwarding design

This document records the M67 reverse-forwarding design. It promotes reverse
forwarding from the deferred roadmap into the active adb-go direction, but it is
a documentation-only milestone: no protocol, client, daemon, or CLI behavior is
implemented here.

## Background

ADB forwarding has two directions:

- **Forward** maps a host-side listener to a service on the device. A host app
  connects to `localhost:LOCAL`, then ADB opens a device service such as
  `tcp:REMOTE`.
- **Reverse** maps a device-side listener to a service on the host. A device app
  connects to `localhost:REMOTE` on the Android device, then `adbd` opens an ADB
  stream back to the host, and the host bridges that stream to `localhost:LOCAL`.

adb-go already supports foreground and daemon-owned forward TCP mappings. Reverse
forwarding is different because the long-lived listener is on the device, and
`adbd` may initiate new logical ADB streams toward the host for accepted device
connections.

## Reference behavior

Reference material for this milestone:

- AOSP `docs/dev/services.md` documents `reverse:<forward-command>` as a local
  device service. The `<forward-command>` is one of `list-forward`,
  `forward:<local>;<remote>`, `forward:norebind:<local>;<remote>`,
  `killforward:<local>`, or `killforward-all`. In reverse mode, `<local>` is the
  socket on the device and `<remote>` is the socket on the host. The output of
  `reverse:list-forward` matches `host:list-forward`, except the serial field is
  `host`.
- AOSP `docs/user/adb.1.md` describes the user-facing shape as
  `adb reverse --list | [--no-rebind] REMOTE LOCAL | --remove REMOTE |
  --remove-all`.

The first adb-go implementation should verify this behavior against a real
current Platform-Tools binary before coding CLI compatibility snapshots. The
protocol service strings below are stable enough to unblock design and unit-test
planning.

### Official command shape

The official CLI exposes:

```text
adb reverse --list
adb reverse [--no-rebind] REMOTE LOCAL
adb reverse --remove REMOTE
adb reverse --remove-all
```

For the endpoint family selected for adb-go's first slice:

```sh
adb reverse tcp:8081 tcp:8081
adb reverse --no-rebind tcp:8081 tcp:8081
adb reverse --list
adb reverse --remove tcp:8081
adb reverse --remove-all
```

Official docs also mention `tcp:0` for the remote/device side, meaning `adbd`
chooses an available device port. adb-go should not promise `tcp:0` until a real
reference run confirms how the selected port is returned and how consistently
supported `adbd` versions report it.

Official reverse endpoint families include device-side `tcp:PORT`,
`localabstract:NAME`, `localreserved:NAME`, and `localfilesystem:NAME`. adb-go's
first implementation should support **TCP to TCP only** and reject every other
family with a clear unsupported-endpoint error.

## Direct-device feasibility

adb-go connects directly to `adbd` over TCP or USB after the ADB `CNXN`
handshake. It does not send official ADB-server smart-socket requests to a host
ADB server. Reverse forwarding is still feasible in direct-device mode because
AOSP documents `reverse:<forward-command>` as a local service available after a
transport has been selected.

Registration and management should use normal adb-go service opens:

```text
reverse:forward:tcp:8081;tcp:8081
reverse:forward:norebind:tcp:8081;tcp:8081
reverse:list-forward
reverse:killforward:tcp:8081
reverse:killforward-all
```

In these service strings:

- the first endpoint is the **remote/device listener**, for example `tcp:8081`;
- the second endpoint is the **local/host target**, for example `tcp:8081`;
- `norebind` means setup must fail if the device already has a reverse mapping
  for that remote endpoint.

The likely setup flow is:

1. adb-go opens `reverse:forward:tcp:8081;tcp:8081` on the device.
2. `adbd` binds or records the device-side listener.
3. The setup stream returns success by closing normally or returns an error text
   before/with close. The implementation must capture real-device behavior and
   map it to actionable Go errors.
4. Later, when an app on the device connects to `127.0.0.1:8081`, `adbd` opens a
   new ADB stream toward the host.
5. adb-go accepts that peer-initiated stream, dials `127.0.0.1:8081` on the
   host, and bridges bytes both ways.

The key protocol gap is step 4. The current `protocol.Connection` demultiplexer
routes `OKAY`, `WRTE`, and `CLSE` packets for streams that adb-go initiated with
`OpenContext`. It does **not** yet handle peer-initiated `OPEN` packets. M68 must
add a safe accept/handler mechanism before any client reverse API can work.

## First implementation scope

The first coded slice should be process-scoped, direct-device reverse TCP:

- Device endpoint: `tcp:REMOTE_PORT` only.
- Host endpoint: `tcp:LOCAL_PORT` only, dialed on host loopback
  `127.0.0.1:LOCAL_PORT` by default.
- Lifetime: the registration belongs to the `Client`/reverse handle. Closing the
  handle, closing the client, canceling the context, or losing the ADB connection
  stops bridging and should attempt cleanup with `reverse:killforward:REMOTE`.
- Setup: registration must fail synchronously on unsupported endpoint syntax,
  invalid port ranges, target connection failure, or `adbd` registration errors.
- Per-connection host dial failures should close only the accepted reverse stream
  and record/report diagnostics where the API shape allows.

A typical data flow should look like:

```text
device app -> 127.0.0.1:8081
               adbd reverse listener
                 ADB OPEN from device to host
                   adb-go accepted reverse stream
                     host TCP dial 127.0.0.1:8081
                       host development server
                 io.Copy in both directions until either side closes
```

This supports common workflows such as an Android app reaching a development
server running on the host without requiring the official ADB server.

## Foreground, daemon, and compat decisions

### Custom `adb-go reverse`

The first custom CLI command should be foreground and explicit, mirroring the
original custom `adb-go forward` design:

```sh
adb-go reverse --addr 127.0.0.1:5555 tcp:8081 tcp:8081
# Reverse forwarding tcp:8081 -> 127.0.0.1:8081. Press Ctrl-C to stop.
```

The command should remain running so the user understands that adb-go owns the
host-side bridge for incoming device connections. On Ctrl-C, it should remove the
device reverse registration and close active bridges.

`--list`, `--remove`, and `--remove-all` are useful but should not be bolted onto
foreground-only reverse forwarding as if they were durable global state. They
belong either to direct-device management commands with very clear target
selection or to daemon-owned reverse forwarding. The plan keeps them in later
milestones so the lifecycle and diagnostics can match the existing forward
registry quality bar.

### Daemon-owned reverse forwarding

Daemon-owned reverse forwarding is the right long-lived model. `adb-god` can own
an in-memory reverse registry, keep the host bridge alive after the CLI exits,
list registrations, and remove them later.

Compared with daemon-owned forward mappings, reverse mappings have one extra
cleanup responsibility: the daemon must unregister the device-side listener when
a mapping is removed, when a target disconnects permanently, or during daemon
shutdown. If cleanup fails because the device is gone, the daemon should report a
stale cleanup error but still remove local daemon state.

### Compat `adb reverse`

Compat mode should eventually use adb-shaped syntax and output. It may be backed
by direct-device operations for simple one-shot/list/remove commands or by the
adb-go daemon for persistent behavior. The important CLI-facing rule is that
official selectors from the compat target-selection foundation (`-s`, `-d`,
`-e`, and `$ANDROID_SERIAL`) must select the device whose reverse table is
managed.

Unsupported endpoint families must be reported in adb-shaped errors, not exposed
as custom adb-go flags or target syntax.

## Error and cleanup semantics

Reverse forwarding should distinguish these failure classes:

- `unsupported_endpoint`: endpoint family is not in the first TCP-only slice.
- `invalid_port`: a TCP endpoint has a malformed or out-of-range port.
- `registration_failed`: `adbd` rejected the reverse service request, for
  example because the device is too old or the endpoint is already bound.
- `rebind_disallowed`: `--no-rebind`/`norebind` was requested and the remote
  endpoint already exists.
- `host_dial_failed`: a device connection arrived, but adb-go could not connect
  to the host TCP target.
- `cleanup_failed`: unregistering the reverse mapping failed during close or
  shutdown.
- `device_closed`: the ADB transport closed while a reverse mapping was active.

Cleanup should be best effort but visible. A reverse handle's `Close` should
attempt `reverse:killforward:<remote>` once. If active bridge goroutines are
running, close their ADB streams and host connections to unblock reads/writes. If
the device disconnected, returning or recording an `ErrDeviceClosed`-matching
error is more useful than hiding the failed cleanup.

## Security and supportability

- Bind/dial host loopback by default. Reverse forwarding lets device-side code
  reach host services, so widening host targets should be an explicit future
  design choice.
- Do not inspect or log forwarded payload bytes.
- Treat endpoints as caller-controlled input; validate syntax and supported
  families, but do not add command/path allowlists.
- Make lifetime clear in CLI usage: foreground mappings stop when the process
  exits; daemon-owned mappings stop when removed or when the daemon exits.
- Document that the initial reverse implementation is not a full Platform-Tools
  replacement because only TCP endpoints are supported.

## Suggested implementation slices

1. **Protocol peer streams**: teach `protocol.Connection` to accept peer
   `OPEN` packets, reply `OKAY`, expose accepted streams to registered handlers,
   and keep existing client-initiated stream behavior unchanged.
2. **Client reverse TCP API**: add typed reverse endpoint helpers, register
   `reverse:forward...`, bridge accepted reverse streams to host loopback TCP,
   and clean up with `reverse:killforward...`.
3. **Custom foreground CLI**: add `adb-go reverse REMOTE LOCAL` for TCP-to-TCP
   mappings with clear foreground lifetime and cleanup messaging.
4. **Daemon registry**: add daemon-owned reverse create/list/remove/remove-all
   with diagnostics and in-memory lifetime semantics.
5. **Compat command**: implement adb-shaped `adb reverse` behavior for supported
   TCP endpoints using official selectors and reference-output tests.

## Open verification items for coding milestones

Before M69 or M72 assert exact behavior, capture from current Platform-Tools and
real/fake devices where possible:

- exact stdout/stderr and exit code for successful `adb reverse tcp:R tcp:L`;
- exact output for `adb reverse --list`, especially empty lists and line format;
- exact errors for duplicate binds with and without `--no-rebind`;
- exact errors for unsupported endpoint families and old `adbd` versions;
- how `tcp:0` reports the selected remote/device port, if at all;
- whether setup failure text arrives as stream payload, close reason, or another
  protocol shape on supported devices.
