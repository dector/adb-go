# adb-go Implementation Plan

This plan is organized as small milestones. Each milestone should be implemented as one focused Conventional Commit.

## Progress

- Current milestone: M70 — Add custom CLI reverse command.
- Completed milestone range: Milestones 17–69 completed the initial CLI shell/push/pull work, CLI documentation, Linux USB transport design, transport abstraction, Linux USB discovery, Linux usbfs bulk transport, high-level USB connection API, CLI USB connection option, USB documentation, adb-go-specific target listing, explicit ADB authentication support, the client APK install helper, the CLI `install-apk` command, APK install documentation, logcat library/CLI/documentation support, property helpers, screencap support, reboot library/CLI/documentation support, foreground and daemon-backed port forwarding foundations, CLI mode routing and adb-compatible skeleton work, compat daemon/device target-selection foundations, the reverse port forwarding design, protocol support for device-initiated streams, and the direct-device client reverse TCP API.
- Active focus: implement reverse port forwarding in small slices. Start with protocol support for device-initiated ADB streams, then add a direct-device TCP reverse API, a foreground custom CLI command, daemon-owned reverse lifecycle, and finally adb-compatible `adb reverse` behavior for supported endpoint families.
- Completed USB direction: Linux-only first, using the kernel usbfs interface under `/dev/bus/usb` behind build tags. This remains pure Go because it talks to device files and ioctls directly instead of linking native USB libraries.

## Milestone template

Use this shape when promoting new work into the active plan:

```markdown
## MNN.N — Short milestone title

Status: Not started

Commit: `type(scope): concise commit subject`

Tasks:

- [ ] First small, reviewable task
- [ ] Second small, reviewable task
- [ ] Documentation or API update, if relevant

Tests:

- [ ] Focused unit or integration tests for the new behavior
- [ ] `go test ./...` passes

Done when:

- [ ] One clear reviewable outcome describes why the milestone is complete
```

Milestone rules:

- Keep each milestone small enough to review as one merge request.
- Implement only the next incomplete milestone.
- Mark a milestone `Implemented` when it is ready for human review, even before commit.
- Use the listed Conventional Commit message, or update the milestone before committing if the scope changes.
- Use grouped numbering such as `M40.1`, `M40.2`, and `M41.1` for related slices of one feature area.
- Do not keep fully implemented milestone bodies in this file long term; summarize completed ranges in `Progress` instead.

## M62 — Extract custom CLI mode package

Status: Done

Commit: `refactor(cli): extract custom mode package`

Tasks:

- [x] Move the current `cmd/adb-go` CLI implementation into `cmd/adb-go/internal/custom`.
- [x] Expose a `custom.Run(args []string, stdout, stderr io.Writer) int` entrypoint for the moved implementation.
- [x] Keep the current adb-go custom UX, command names, flags, usage text, and behavior unchanged.
- [x] Leave `cmd/adb-go/main.go` as a thin compatibility wrapper around custom mode only; do not add mode detection yet.
- [x] Update package names/imports and test package references after the move.

Tests:

- [x] Existing CLI tests pass without intentional expectation changes.
- [x] `go test ./...` passes

Done when:

- [x] The current adb-go CLI behavior is preserved exactly while the code lives under `cmd/adb-go/internal/custom`.

## M63 — Add CLI mode router

Status: Done

Commit: `feat(cli): add custom and compat mode router`

Tasks:

- [x] Replace `cmd/adb-go/main.go` with a tiny router that only detects mode and dispatches to the selected implementation.
- [x] Select compat mode when `ADB_GO_MODE=compat` or the executable basename is exactly `adb` or `adb.exe`.
- [x] Select custom mode when `ADB_GO_MODE=custom` or when no compat signal is present.
- [x] Reject any other non-empty `ADB_GO_MODE` value with exit code `2`.
- [x] Do not support bootstrap flags such as `--compat` or `--custom`.
- [x] Add a temporary compat placeholder that returns a clear “compat mode is not implemented yet” error with exit code `1`.

Tests:

- [x] Router tests cover default custom mode, `ADB_GO_MODE=custom`, `ADB_GO_MODE=compat`, invalid `ADB_GO_MODE`, and `adb`/`adb.exe` basename detection.
- [x] Existing custom mode CLI tests continue to pass.
- [x] `go test ./...` passes

Done when:

- [x] The binary can route cleanly between custom mode and a compat placeholder without changing custom behavior.

## M64 — Add adb-compatible CLI skeleton

Status: Done

Commit: `feat(cli): add adb compat skeleton`

Tasks:

- [x] Add `cmd/adb-go/internal/compat` with an independent `compat.Run(args []string, stdout, stderr io.Writer) int` implementation.
- [x] Implement compat `help`, `--help`, no-args, unknown command, and `version` behavior using the current Android SDK Platform-Tools `adb` output shape as the reference.
- [x] Make compat `version` adb-shaped but explicit that the implementation is adb-go, for example with an `(adb-go compat)` marker or equivalent wording.
- [x] Keep custom-only commands and flags such as `targets`, `daemon`, `install-apk`, `--addr`, `--usb`, and `ADB_GO_ADDR` out of compat mode.
- [x] Parse official-style global options enough for host-only skeleton commands, including `-H` and `-P` before `version`.

Tests:

- [x] Compat tests cover `help`, `--help`, no args, `-h`, unknown commands, `version`, and global `-H`/`-P` with `version`.
- [x] Snapshot-style assertions cover important stdout/stderr placement and exit codes from the captured official adb reference.
- [x] `go test ./...` passes

Done when:

- [x] Compat mode has a real adb-shaped host-only skeleton while remaining independent from custom mode.

## M65 — Add compat daemon/server foundation

Status: Done

Commit: `feat(cli): add compat daemon foundation`

Tasks:

- [x] Add compat implementations for `start-server`, `kill-server`, and `devices` on top of adb-go daemon internals/protocol rather than the official adb server protocol.
- [x] Auto-start the adb-go daemon for compat commands that need server state.
- [x] Make `adb devices` show daemon-known TCP devices from prior compat `connect` work and locally discoverable USB devices when available.
- [x] Keep broad blind TCP/emulator scanning out of the initial compat `devices` behavior.
- [x] Preserve official adb CLI-facing output shape where practical while allowing different daemon internals.

Tests:

- [x] Compat tests cover server lifecycle commands and auto-start behavior.
- [x] Compat tests cover no-device `devices` output matching the official reference shape.
- [x] Daemon tests cover any new protocol/state needed by compat devices listing.
- [x] `go test ./...` passes

Done when:

- [x] Compat mode has adb-shaped server lifecycle and device-list foundations backed by adb-go daemon internals.

## M66 — Add compat target selection foundation

Status: Done

Commit: `feat(cli): add compat target selection`

Tasks:

- [x] Support official target selectors `-s SERIAL`, `-d`, and `-e` in compat mode.
- [x] Support `$ANDROID_SERIAL`, with `-s SERIAL` taking precedence.
- [x] Keep `ADB_GO_ADDR` ignored in compat mode.
- [x] Map selected compat transports onto daemon/device registry entries and adb-go TCP/USB connection options.
- [x] Defer transport IDs (`-t ID`) unless promoted into this milestone after additional reference capture.

Tests:

- [x] Compat parser tests cover selector precedence and no-command help/exit behavior for `-s`, `-d`, and `-e`.
- [x] Compat command tests cover selected-device resolution against fake daemon/device state.
- [x] `go test ./...` passes

Done when:

- [x] Compat commands can resolve devices using official adb selector mechanisms without exposing custom adb-go target flags.

## Reverse port forwarding

Reverse forwarding is now the active focus. These milestones should remain small,
reviewable slices and should be implemented in order.

### Background

Forward port forwarding is currently implemented in two paths:

- Foreground `adb-go forward` uses `client.ForwardLocalTCP` in `client/forward.go`: adb-go binds a host TCP listener, accepts host connections, opens a fresh device ADB service stream such as `tcp:8000` for each connection, and bridges bytes both ways until either side closes.
- Daemon-owned `adb-go forward --background` uses `internal/daemon/forward_registry.go`: `adb-god` stores an in-memory registration, owns the host listener, reconnects to the explicit TCP ADB target per accepted host connection, opens the configured device `tcp:PORT` service, tracks state/active connections/last errors, and exposes create/list/remove operations through the daemon protocol.

Reverse forwarding should mirror this in small slices while respecting that the
listener is device-side and that `adbd` may initiate streams back to the host.
The exact service strings and remote-initiated stream behavior should be verified
against current AOSP/platform-tools before implementation.

### M67 — Design reverse port forwarding

Status: Done

Commit: `docs(forward): design reverse port forwarding`

Tasks:

- [x] Capture official `adb reverse` behavior for `tcp:REMOTE tcp:LOCAL`, `--list`, `--remove`, `--remove-all`, and `--no-rebind`/norebind semantics where supported.
- [x] Document direct-device feasibility: reverse registration service strings, how `adbd` reports setup errors, and whether adb-go must support device-initiated `OPEN` streams to bridge back to host TCP.
- [x] Define first-slice endpoint support as device TCP to host TCP only, with loopback host TCP targets by default.
- [x] Decide foreground versus daemon-owned lifecycle for custom `adb-go reverse`, and how it maps to future compat `adb reverse` behavior.
- [x] Record security, cleanup, disconnect, and unsupported-endpoint behavior in a dedicated reverse-forwarding design doc or an update to the forwarding docs.

Tests:

- [x] Documentation review checks the proposed behavior against current Android SDK Platform-Tools reference output.
- [x] `go test ./...` passes

Done when:

- [x] The project has a reviewed reverse-forwarding design that can be implemented without guessing about ADB protocol behavior.

### M68 — Add protocol support for reverse streams

Status: Done

Commit: `feat(protocol): support device initiated streams`

Tasks:

- [x] Extend the protocol connection reader to handle peer-initiated `OPEN` packets instead of ignoring them.
- [x] Add an internal accept/handler mechanism that can route device-initiated reverse streams by service name to a host-side bridge.
- [x] Preserve existing client-initiated `OpenService` stream behavior and error semantics.
- [x] Ensure connection close, stream close, backpressure, and concurrent read/write behavior remain safe.

Tests:

- [x] Protocol tests cover receiving peer `OPEN`, replying `OKAY`, reading/writing payloads, and closing both accepted and initiated streams.
- [x] Existing protocol/client forwarding tests continue to pass.
- [x] `go test ./...` passes

Done when:

- [x] adb-go can safely accept and service streams opened by `adbd` without breaking existing client-initiated services.

### M69 — Add client reverse TCP API

Status: Done

Commit: `feat(client): add reverse tcp forwarding`

Tasks:

- [x] Add typed reverse endpoint helpers for device TCP and host TCP ports with range validation.
- [x] Add a `Client` reverse-forwarding API that registers the device-side listener using the verified `reverse:` ADB service, bridges accepted reverse streams to host TCP connections, and cleans up the registration on `Close`.
- [x] Support `tcp:0` remote-device port behavior if the reference implementation and `adbd` expose the selected port reliably; otherwise document it as unsupported in the first slice.
- [x] Return actionable setup errors for unsupported devices, address conflicts, registration failures, host dial failures, and cleanup failures.
- [x] Update root package re-exports and client README documentation.

Tests:

- [x] Fake ADB tests cover reverse registration service strings, host TCP bridging, cleanup, and setup failure mapping.
- [x] Unit tests cover endpoint parsing/validation and unsupported endpoint families.
- [x] `go test ./...` passes

Done when:

- [x] Library callers can create a process-scoped reverse TCP forward from a device TCP port to a host TCP port.

### M70 — Add custom CLI reverse command

Status: Done

Commit: `feat(cli): add reverse command`

Tasks:

- [x] Add `adb-go reverse [connection flags] tcp:REMOTE_PORT tcp:LOCAL_PORT` using the client reverse TCP API.
- [x] Make foreground lifetime and cleanup explicit in usage text and status output.
- [x] Add `--list`, `--remove`, and `--remove-all` only if the M67 design chooses daemon-backed or direct-device support for those operations in this slice; otherwise document them as deferred.
- [x] Keep unsupported endpoint families rejected with clear guidance.
- [x] Update README and forwarding documentation with examples and limitations.

Tests:

- [x] CLI tests cover success, argument validation, unsupported endpoint errors, connection errors, and cleanup/error reporting.
- [x] List/remove output tests are not applicable because M70 documents those operations as deferred rather than including them.
- [x] `go test ./...` passes

Done when:

- [x] Users can run a documented custom-mode reverse TCP forwarding workflow without relying on the official adb server.

### M71 — Add daemon-owned reverse forwarding

Status: Not started

Commit: `feat(daemon): add reverse forwarding registry`

Tasks:

- [ ] Extend daemon forwarding models or add reverse-specific models for device-side listener registrations.
- [ ] Add daemon protocol commands for reverse create/list/remove/remove-all, keeping forward and reverse diagnostics distinct enough for troubleshooting.
- [ ] Implement daemon-owned lifecycle, reconnection policy, cleanup-on-remove, active connection tracking, and last-error reporting for explicit TCP ADB targets.
- [ ] Wire custom CLI background/list/remove controls if the M67 design chooses daemon-owned reverse support.
- [ ] Document that reverse registrations are in-memory unless a later durable-state milestone is designed.

Tests:

- [ ] Daemon protocol and registry tests cover create/list/remove/remove-all, norebind, reconnect/degraded state, active connections, and shutdown cleanup.
- [ ] CLI tests cover daemon unavailable/too-old errors and successful daemon-owned reverse operations.
- [ ] `go test ./...` passes

Done when:

- [ ] `adb-god` can own in-memory reverse TCP forwarding registrations with lifecycle and diagnostics matching the forward registry quality bar.

### M72 — Add compat adb reverse support

Status: Not started

Commit: `feat(cli): add adb reverse compat`

Tasks:

- [ ] Implement compat-mode `adb reverse` syntax and output shape for supported TCP endpoints using current platform-tools behavior as reference.
- [ ] Route official target selectors from the compat target-selection foundation into reverse operations.
- [ ] Map compat list/remove/remove-all semantics to the chosen direct or daemon-backed adb-go reverse implementation.
- [ ] Keep unsupported endpoint families and unsupported devices reported in adb-shaped errors.

Tests:

- [ ] Compat tests cover `adb reverse tcp:REMOTE tcp:LOCAL`, `--list`, `--remove`, `--remove-all`, selector handling, unsupported endpoints, and error output.
- [ ] Reference-output snapshots cover important stdout/stderr and exit-code behavior.
- [ ] `go test ./...` passes

Done when:

- [ ] Compat mode has adb-shaped reverse TCP forwarding for the endpoint families adb-go actually supports.

### Cross-platform USB

Potential future commit series depends on platform research:

- `feat(usb): add macos usb transport`
- `feat(usb): add windows usb transport`

These may require different OS APIs from Linux usbfs. Preserve the pure-Go preference and avoid cgo/native dependencies unless a future decision changes this.
