# adb-go Implementation Plan

This plan is organized as small milestones. Each milestone should be implemented as one focused Conventional Commit.

## Progress

- Current milestone: M57 — Add daemon forwarding protocol model.
- Completed milestone range: Milestones 17–56 completed the initial CLI shell/push/pull work, CLI documentation, Linux USB transport design, transport abstraction, Linux USB discovery, Linux usbfs bulk transport, high-level USB connection API, CLI USB connection option, USB documentation, adb-go-specific target listing, explicit ADB authentication support, the client APK install helper, the CLI `install-apk` command, APK install documentation, logcat library/CLI/documentation support, property helpers, screencap support, reboot library/CLI/documentation support, foreground port forwarding support, the minimal adb-god daemon foundation, Linux systemd user-service install/lifecycle controls, Linux systemd user-service status reporting, daemon diagnostics, daemon service logs, daemon service reinstall, CLI version reporting, CLI error-message improvements, integration-test documentation, exported package documentation/examples, and the daemon-backed persistent forwarding design.
- Active focus: implement daemon-owned persistent TCP forwarding in small slices while preserving the existing foreground forwarding behavior and avoiding durable daemon-owned ADB state until explicitly designed.
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

## M57 — Add daemon forwarding protocol model

Status: Not started

Commit: `feat(daemon): add forwarding protocol model`

Tasks:

- [ ] Add daemon request/response types for `forward_create`, `forward_list`, `forward_remove`, and `forward_remove_all`.
- [ ] Define stable forwarding error codes such as `address_in_use`, `unsupported_endpoint`, `bad_target`, `rebind_disallowed`, and `forward_not_found`.
- [ ] Add validation for the first supported endpoint families without opening listeners or ADB device connections.
- [ ] Keep the protocol backward-compatible with existing `ping`, `status`, and `shutdown` commands.

Tests:

- [ ] Unit tests cover JSON request/response handling for forwarding commands.
- [ ] Unit tests cover validation and stable daemon error codes.
- [ ] Existing daemon protocol tests continue to pass.
- [ ] `go test ./...` passes

Done when:

- [ ] The daemon protocol can parse, validate, and report forwarding commands without yet owning real listeners.

## M58 — Add daemon forwarding registry and TCP listener ownership

Status: Not started

Commit: `feat(daemon): add forwarding listener registry`

Tasks:

- [ ] Add an in-memory daemon forwarding registry keyed by generated ID and local endpoint.
- [ ] Implement daemon-owned loopback TCP listener creation for supported local `tcp:PORT` endpoints.
- [ ] Implement list, remove-by-ID, remove-by-local, and remove-all behavior against daemon-owned listeners.
- [ ] Implement rebind and `norebind` semantics for daemon-owned forwards only.
- [ ] Ensure daemon shutdown closes forwarding listeners and active registry entries.

Tests:

- [ ] Daemon tests cover create/list/remove/remove-all with isolated local TCP ports.
- [ ] Daemon tests cover ephemeral `tcp:0`, address-in-use, and rebind-disallowed behavior.
- [ ] Daemon shutdown tests cover listener cleanup.
- [ ] `go test ./...` passes

Done when:

- [ ] `adb-god` can own and manage persistent local TCP listener registrations through the daemon protocol, without bridging ADB traffic yet.

## M59 — Bridge daemon forwards to TCP ADB targets

Status: Not started

Commit: `feat(daemon): bridge persistent forwards`

Tasks:

- [ ] For each accepted daemon-owned local TCP connection, connect to the explicit TCP ADB target from the forwarding registration.
- [ ] Open the configured remote `tcp:PORT` ADB service for each accepted host connection.
- [ ] Copy bytes in both directions and close both sides when either side finishes.
- [ ] Track active connection counts and last connection/setup errors for list diagnostics.
- [ ] Keep USB target persistence out of scope for this first bridge slice.

Tests:

- [ ] Tests use fake ADB and local TCP clients to verify bidirectional forwarding through the daemon.
- [ ] Tests cover failed target dial/service-open behavior without dropping the local listener.
- [ ] Tests cover remove/shutdown closing active bridged connections.
- [ ] `go test ./...` passes

Done when:

- [ ] A daemon-owned background forward can carry TCP traffic from a host client to a device `tcp:PORT` service through an explicit TCP ADB target.

## M60 — Add CLI persistent forwarding controls

Status: Not started

Commit: `feat(cli): add persistent forward controls`

Tasks:

- [ ] Add `adb-go forward --background` to create daemon-owned persistent forwards.
- [ ] Add `adb-go forward --list`, `--remove LOCAL`, `--remove-id ID`, and `--remove-all` wired to the daemon protocol.
- [ ] Keep existing foreground `adb-go forward LOCAL REMOTE` behavior unchanged when daemon flags are absent.
- [ ] Print clear setup/list/remove output, including actual bound local address for `tcp:0`.
- [ ] Return actionable daemon-unavailable or daemon-too-old hints, such as running `adb-go daemon doctor`.
- [ ] Document the new CLI behavior and the non-durable in-memory daemon lifecycle.

Tests:

- [ ] CLI tests cover argument parsing and daemon requests for background/list/remove/remove-all.
- [ ] CLI tests cover foreground forwarding still using the existing process-scoped path.
- [ ] CLI tests cover daemon-unavailable or unknown-command error messages.
- [ ] `go test ./...` passes

Done when:

- [ ] Users can create, inspect, and remove daemon-owned persistent TCP forwards from the CLI while existing foreground forwarding remains compatible.

## M61 — Surface persistent forwarding diagnostics

Status: Not started

Commit: `feat(cli): show daemon forwarding diagnostics`

Tasks:

- [ ] Add concise forwarding counts to daemon diagnostics without turning `daemon status` into a full forwarding table.
- [ ] Include forwarding state hints in `adb-go daemon doctor` when daemon-owned forwards are degraded.
- [ ] Ensure detailed mappings remain available through `adb-go forward --list`.
- [ ] Update daemon and CLI documentation with troubleshooting examples for degraded forwards, target disconnects, and lost forwards after daemon restart.

Tests:

- [ ] CLI/daemon tests cover forwarding counts in diagnostics.
- [ ] CLI tests cover doctor hints for degraded forwarding state.
- [ ] `go test ./...` passes

Done when:

- [ ] Support requests can distinguish daemon reachability problems from persistent-forward target/listener problems without exposing payload data.

## Deferred milestones

These remain out of the active plan unless promoted into a concrete milestone using the template above.

### Cross-platform USB

Potential future commit series depends on platform research:

- `feat(usb): add macos usb transport`
- `feat(usb): add windows usb transport`

These may require different OS APIs from Linux usbfs. Preserve the pure-Go preference and avoid cgo/native dependencies unless a future decision changes this.
