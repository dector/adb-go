# adb-go forwarding design

This document records the M44.1 foreground forwarding design. It explains what
official `adb forward` does, why adb-go initially implemented forwarding as a
foreground direct-device bridge, and the small direct-device forwarding shape
used by the current library and CLI. Future daemon-backed persistent forwarding
is designed separately in [`persistent-forwarding-design.md`](persistent-forwarding-design.md).

## Official adb behavior

Official `adb` is a client/server tool. The command-line client connects to the
host ADB server's smart-socket interface, selects a target transport, and asks
the server to manage forwarding state.

The AOSP ADB service documentation describes forwarding as host services of the
form:

```text
<host-prefix>:forward:<local>;<remote>
<host-prefix>:forward:norebind:<local>;<remote>
<host-prefix>:killforward:<local>
<host-prefix>:killforward-all
<host-prefix>:list-forward
```

`<host-prefix>` selects the target device through the server, for example
`host-serial:<serial>`, `host-usb`, `host-local`, or `host`. The server then
owns the host-side listener and bookkeeping. That split is why normal commands
such as `adb forward tcp:8000 tcp:8000` return immediately: the long-lived ADB
server keeps listening after the short-lived CLI process exits.

Official local endpoint forms include host TCP listeners and host Unix sockets.
Official remote endpoint forms include device TCP sockets, Android local socket
namespaces, JDWP process endpoints, vsock endpoints, and other local ADB
services exposed by `adbd`.

A typical official forwarding flow is:

1. The user runs `adb -s SERIAL forward tcp:9000 tcp:9000`.
2. The CLI sends a smart-socket request such as
   `host-serial:SERIAL:forward:tcp:9000;tcp:9000` to the host ADB server.
3. The server binds a listener on the host, records the mapping, and replies to
   the CLI.
4. The CLI exits.
5. Later, each host connection accepted by the server is bridged to a new ADB
   stream opened on the selected device with a service such as `tcp:9000`.
6. `adb forward --list`, `--remove`, and `--remove-all` inspect or mutate the
   server's forwarding table.

## adb-go architecture constraint

adb-go intentionally does not use or implement the official host ADB server for
v0 workflows. A `client.Client` is a direct connection to one selected ADB
device over TCP or Linux USB. After the `CNXN`/`AUTH` handshake, adb-go opens
local device services such as `shell:...`, `sync:`, `reboot:`, or `tcp:<port>`
by sending normal ADB `OPEN` packets to `adbd`.

Because there is no separate adb-go daemon process, adb-go has nowhere to store
global forwarding state after the caller exits. It also cannot send
`host-prefix:forward:...` requests to `adbd`; those are ADB server smart-socket
services, not direct device services. Therefore adb-go should not claim full
server-compatible `adb forward` behavior in the first implementation.

## Decision: foreground direct-device forwarding

adb-go can support useful forwarding by owning the host listener in the calling
process and opening one device service stream for each accepted local
connection. This matches adb-go's direct-device architecture and keeps lifetime
control explicit.

The initial implementation should support:

- Local endpoint: a TCP listener on the host.
- Remote endpoint: a device TCP service, encoded as `tcp:<port>` and opened with
  `Client.OpenService` for each accepted local connection.
- Lifetime: foreground/in-process only. Closing the returned forward handle, the
  caller's context, the client, or the CLI process stops the listener and active
  bridges.
- Binding conflicts: rely on normal host listener behavior. If the local TCP
  address is already in use, setup fails. There is no adb-server-style rebind
  table to mutate.

A forwarding session should look like this:

```text
host app -> 127.0.0.1:9000
             adb-go TCP listener
               accepted net.Conn
                 ADB OPEN "tcp:9000"
                   adbd connects to device localhost:9000
                 io.Copy in both directions until either side closes
```

This supports the common "debug a service listening on the device" workflow
without pretending that adb-go has a background server.

## Proposed library API

Keep the public API intentionally small and typed enough to avoid ambiguous raw
ADB service strings in the common path:

```go
// ForwardTarget describes the device-side endpoint opened for each accepted
// host connection.
type ForwardTarget struct { /* unexported service string */ }

func ForwardTCP(port int) (ForwardTarget, error)

// Forward is an active local forwarding session.
type Forward struct { /* unexported fields */ }

func (f *Forward) LocalAddr() net.Addr
func (f *Forward) Close() error
func (f *Forward) Wait() error

func (c *Client) ForwardLocalTCP(ctx context.Context, localAddr string, remote ForwardTarget) (*Forward, error)
```

Design notes for M44.2:

- `localAddr` should be a normal Go TCP listen address such as
  `"127.0.0.1:9000"` or `"127.0.0.1:0"`. Binding to loopback by default is safer
  for helper functions and CLI parsing; callers who want a wider bind can pass
  an explicit address such as `"0.0.0.0:9000"` if the API allows it.
- `ForwardTCP(port)` should validate the port range and produce the ADB service
  string `tcp:<port>`.
- The accept loop should open a fresh ADB stream for every accepted connection.
- The bridge should copy bytes in both directions and close both sides when one
  direction finishes or errors.
- `Close` should stop accepting new local connections and close active
  connections/streams. `Wait` should let callers observe the forwarding loop
  ending.
- Errors from individual bridged connections may be difficult to report without
  making the API noisy. The first implementation can make setup errors
  synchronous and reserve optional per-connection error hooks for later.

Potential later extensions, explicitly out of scope for the first code slice:

- Device local socket namespaces: `localabstract:`, `localreserved:`, and
  `localfilesystem:`.
- JDWP, vsock, `dev:`, `dev-raw:`, or raw advanced service targets.
- Host Unix socket listeners.
- Daemon-backed `--list`, `--remove`, `--remove-all`, or persistent mappings
  after process exit. The persistent variant has a separate design in
  [`persistent-forwarding-design.md`](persistent-forwarding-design.md).
- Reverse forwarding.

## Proposed CLI behavior

The CLI should make foreground lifetime obvious:

```text
adb-go forward [connection flags] LOCAL REMOTE
```

For the initial implementation:

- `LOCAL` supports `tcp:PORT` only. It binds to `127.0.0.1:PORT`; `tcp:0` asks
  the OS for an available port and the CLI should print the selected address.
- `REMOTE` supports `tcp:PORT` only.
- The command stays in the foreground until interrupted or until an error stops
  the listener.
- Ctrl-C or process termination stops the local listener and active bridges.
- The command should document that it is not a persistent adb-server
  registration. There is no `--list`, `--remove`, or `--remove-all` until/unless
  adb-go grows a daemon or explicit local state model.

Example:

```sh
adb-go forward --addr 127.0.0.1:5555 tcp:9000 tcp:9000
# Forwarding 127.0.0.1:9000 -> tcp:9000. Press Ctrl-C to stop.
```

## Consequences

This design gives adb-go a useful forwarding workflow while preserving the
project's current direct-device model. It avoids silently depending on the
official adb server and avoids over-promising compatibility with server-managed
`adb forward`. The tradeoff is that adb-go forwarding is process-scoped: when
the library handle or CLI exits, the forward is gone.
