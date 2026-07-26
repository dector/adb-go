# adb-go Implementation Plan

This plan is organized as small milestones. Each milestone should be implemented as one focused Conventional Commit.

## Progress

- Current milestone: M44.1 — Research direct-device forwarding design.
- Completed milestone range: Milestones 17–43 completed the initial CLI shell/push/pull work, CLI documentation, Linux USB transport design, transport abstraction, Linux USB discovery, Linux usbfs bulk transport, high-level USB connection API, CLI USB connection option, USB documentation, adb-go-specific target listing, explicit ADB authentication support, the client APK install helper, the CLI `install-apk` command, APK install documentation, logcat library/CLI/documentation support, property helpers, screencap support, and reboot library/CLI/documentation support.
- Active focus: add missing non-USB ADB workflows in small library/CLI/docs slices. Start with shell-backed features that fit the current direct-device architecture, then investigate forwarding separately because official `adb forward` semantics usually involve host-side listener behavior.
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

## M40 — Logcat support

Goal: expose Android log output through adb-go without trying to clone every official `adb logcat` flag from day one.

### M40.1 — Add client logcat streaming helper

Status: Implemented

Commit: `feat(client): add logcat streaming helper`

Tasks:

- [x] Add a high-level `Client.Logcat(ctx, stdout, opts)` style API that streams device logs to an `io.Writer`.
- [x] Add a small `LogcatOptions` type with intentionally supported behavior, initially including dump-and-exit behavior if it maps cleanly to `logcat -d`.
- [x] Implement the helper using existing `shell:` streaming primitives and clear command construction.
- [x] Re-export stable logcat API from the root package if it is part of the high-level API.

Tests:

- [x] Client tests cover streaming logcat output through `internal/fakeadb`.
- [x] Client tests cover option-to-command behavior, including dump mode if implemented.
- [x] `go test ./...` passes

Done when:

- [x] Library callers can stream logcat output from a connected device through adb-go's high-level client API.

### M40.2 — Add CLI logcat command

Status: Implemented

Commit: `feat(cli): add logcat command`

Tasks:

- [x] Add an `adb-go logcat` command.
- [x] Support the same TCP, Linux USB, and `--auth-key` connection flags as `shell`, `push`, `pull`, and `install-apk`.
- [x] Stream log output to stdout.
- [x] Support only the logcat options implemented by the client helper, such as `--dump` if present.

Tests:

- [x] CLI tests cover usage, argument validation, connection flag propagation, success, and streaming/failure behavior.
- [x] `go test ./...` passes

Done when:

- [x] CLI users can run `adb-go logcat [connection flags]` to stream or dump device logs through supported adb-go transports.

### M40.3 — Document logcat support and limitations

Status: Implemented

Commit: `docs: document logcat support`

Tasks:

- [x] Update README examples and limitations to mention logcat support.
- [x] Update `client/README.md` with the library logcat helper and supported options.
- [x] Update `cmd/adb-go/README.md` with CLI usage and examples for TCP/USB/auth.
- [x] Document that adb-go does not yet aim for full official `adb logcat` flag compatibility.

Tests:

- [x] Documentation examples compile where applicable.
- [x] `go test ./...` passes

Done when:

- [x] Users can discover how to read logs with adb-go and understand the supported subset.

## M41 — Device property helpers

Goal: make common `getprop` workflows available as small high-level helpers and a CLI command.

### M41.1 — Add client device property helpers

Status: Implemented

Commit: `feat(client): add device property helpers`

Tasks:

- [x] Add a high-level helper for reading one property, for example `Client.GetProp(ctx, name string) (string, error)`.
- [x] Add a helper for reading all properties, for example `Client.Properties(ctx) (map[string]string, error)`.
- [x] Parse standard `getprop` output carefully and return useful errors for malformed lines where appropriate.
- [x] Re-export stable property APIs from the root package if they are part of the high-level API.

Tests:

- [x] Client tests cover one-property lookup through `internal/fakeadb`.
- [x] Client tests cover parsing all-properties output.
- [x] Client tests cover empty and malformed output behavior.
- [x] `go test ./...` passes

Done when:

- [x] Library callers can read Android system properties without manually building and parsing `getprop` shell commands.

### M41.2 — Add CLI getprop command

Status: Implemented

Commit: `feat(cli): add getprop command`

Tasks:

- [x] Add an `adb-go getprop` command.
- [x] Support the same TCP, Linux USB, and `--auth-key` connection flags as other device commands.
- [x] With one property argument, print that property value.
- [x] With no property argument, print all properties in a stable, readable format.

Tests:

- [x] CLI tests cover usage, argument validation, connection flag propagation, one-property output, all-property output, and errors.
- [x] `go test ./...` passes

Done when:

- [x] CLI users can run `adb-go getprop [connection flags] [PROPERTY]` for common Android property inspection.

### M41.3 — Document property helpers

Status: Implemented

Commit: `docs: document getprop support`

Tasks:

- [x] Update README examples and limitations to mention `getprop` support.
- [x] Update `client/README.md` with library property helper examples.
- [x] Update `cmd/adb-go/README.md` with CLI usage and examples for TCP/USB/auth.
- [x] Document that property values come from the connected device and should be treated as remote data.

Tests:

- [x] Documentation examples compile where applicable.
- [x] `go test ./...` passes

Done when:

- [x] Users can discover how to inspect device properties with adb-go.

## M42 — Screencap support

Goal: add a simple screenshot workflow using existing shell streaming support.

### M42.1 — Add client screencap helper

Status: Implemented

Commit: `feat(client): add screencap helper`

Tasks:

- [x] Add a high-level helper for capturing PNG screenshot bytes, for example `Client.Screencap(ctx) ([]byte, error)`.
- [x] Add a helper for writing a screenshot directly to a local file if it keeps the API small and clear.
- [x] Implement using `shell:screencap -p` or another device-supported service/command.
- [x] Handle or document Android shell newline behavior if it affects PNG output.
- [x] Re-export stable screencap APIs from the root package if they are part of the high-level API.

Tests:

- [x] Client tests cover screenshot byte capture through `internal/fakeadb`.
- [x] Client tests cover local file output behavior if implemented.
- [x] `go test ./...` passes

Done when:

- [x] Library callers can capture one screenshot from a connected device without manually invoking shell commands.

### M42.2 — Add CLI screencap command

Status: Implemented

Commit: `feat(cli): add screencap command`

Tasks:

- [x] Add an `adb-go screencap` command.
- [x] Support the same TCP, Linux USB, and `--auth-key` connection flags as other device commands.
- [x] Accept exactly one local output path, or explicitly decide and document stdout behavior.
- [x] Avoid overwriting existing local files unless an explicit overwrite option is added.

Tests:

- [x] CLI tests cover usage, argument validation, connection flag propagation, success, destination-exists behavior, and errors.
- [x] `go test ./...` passes

Done when:

- [x] CLI users can run `adb-go screencap [connection flags] LOCAL_PNG` to save a screenshot.

### M42.3 — Document screencap support

Status: Implemented

Commit: `docs: document screencap support`

Tasks:

- [x] Update README examples and limitations to mention screencap support.
- [x] Update `client/README.md` with screenshot helper examples and output behavior.
- [x] Update `cmd/adb-go/README.md` with CLI usage and examples for TCP/USB/auth.
- [x] Document overwrite behavior and any known Android shell output caveats.

Tests:

- [x] Documentation examples compile where applicable.
- [x] `go test ./...` passes

Done when:

- [x] Users can discover how to capture screenshots with adb-go.

## M43 — Reboot support

Goal: expose a small, explicit reboot workflow while making the disruptive nature of the operation clear.

### M43.1 — Add client reboot helper

Status: Implemented

Commit: `feat(client): add reboot helper`

Tasks:

- [x] Add a high-level reboot helper, for example `Client.Reboot(ctx, mode RebootMode) error`.
- [x] Define intentionally supported reboot modes, initially normal reboot plus optional bootloader/recovery if they map cleanly.
- [x] Choose the implementation path deliberately, such as ADB `reboot:` service or a shell command, and document the choice in code comments.
- [x] Re-export stable reboot APIs from the root package if they are part of the high-level API.

Tests:

- [x] Client tests cover service/command flow through `internal/fakeadb`.
- [x] Client tests cover unsupported mode validation.
- [x] `go test ./...` passes

Done when:

- [x] Library callers can request a supported device reboot mode with explicit API behavior.

### M43.2 — Add CLI reboot command

Status: Implemented

Commit: `feat(cli): add reboot command`

Tasks:

- [x] Add an `adb-go reboot` command.
- [x] Support the same TCP, Linux USB, and `--auth-key` connection flags as other device commands.
- [x] Accept no mode for normal reboot and a small explicit set of supported modes such as `bootloader` and `recovery` if implemented by the client.
- [x] Print clear errors for unsupported modes.

Tests:

- [x] CLI tests cover usage, argument validation, connection flag propagation, supported modes, unsupported modes, and errors.
- [x] `go test ./...` passes

Done when:

- [x] CLI users can run `adb-go reboot [connection flags] [MODE]` for supported reboot modes.

### M43.3 — Document reboot support and safety

Status: Implemented

Commit: `docs: document reboot support`

Tasks:

- [x] Update README examples and limitations to mention reboot support.
- [x] Update `client/README.md` with reboot helper examples.
- [x] Update `cmd/adb-go/README.md` with CLI usage and examples for TCP/USB/auth.
- [x] Document that reboot is disruptive, may close the ADB connection, and affects the selected device immediately.

Tests:

- [x] Documentation examples compile where applicable.
- [x] `go test ./...` passes

Done when:

- [x] Users can discover reboot support and understand its safety implications.

## M44 — Port forwarding support

Goal: investigate and then implement adb-go forwarding in a way that matches the direct-device architecture instead of blindly copying adb-server behavior.

### M44.1 — Research direct-device forwarding design

Status: Implemented

Commit: `docs: design adb-go forwarding support`

Tasks:

- [x] Research official ADB forwarding behavior and identify which pieces are host-side listener management versus device stream opening.
- [x] Decide whether adb-go can support a direct-device forwarding API without running an adb-server-compatible daemon.
- [x] Design a small library API for local TCP to remote ADB service forwarding if feasible.
- [x] Document limitations and non-goals, including differences from official `adb forward`.

Design:

- Official `adb forward` is adb-server-managed state. The CLI sends smart-socket host-service requests such as `<host-prefix>:forward:<local>;<remote>` to the host ADB server. The server owns local listeners, preserves mappings after the CLI exits, and serves `--list`, `--remove`, and `--remove-all` from its forwarding table.
- adb-go should not claim that behavior while it remains a direct-device library/CLI with no adb-server-compatible daemon. Direct connections can open device services such as `tcp:<port>` but cannot register persistent host-side forwarding state inside `adbd`.
- The follow-up implementation should provide foreground, process-scoped forwarding: bind a local TCP listener in the caller process, accept local connections, open a fresh remote device service stream for each connection, and bridge bytes in both directions until closed.
- The first code slice should support local TCP to remote device TCP only. Proposed library shape: a typed `ForwardTarget`, `ForwardTCP(port)`, an active `Forward` handle with `LocalAddr`, `Close`, and `Wait`, and `Client.ForwardLocalTCP(ctx, localAddr, remote)`.
- The first CLI slice should support `adb-go forward [connection flags] tcp:PORT tcp:PORT`, run in the foreground, print the actual local address for `tcp:0`, and document that stopping the process removes the forward.
- Out of scope for the initial implementation: persistent server-like mappings, `--list`, `--remove`, `--remove-all`, reverse forwarding, host Unix sockets, device local socket namespaces, JDWP, vsock, and raw advanced remote service targets.
- Full rationale and consequences are documented in [`docs/forwarding-design.md`](docs/forwarding-design.md).

Tests:

- [x] No code tests required unless the design milestone includes small exploratory tests.
- [x] `go test ./...` passes

Done when:

- [x] The plan contains a concrete, reviewable design for forwarding that can be implemented in small follow-up milestones.

### M44.2 — Add client port forwarding helpers

Status: Implemented

Commit: `feat(client): add port forwarding helpers`

Tasks:

- [x] Implement the forwarding design from M44.1 as a small high-level client API.
- [x] Support local TCP listener forwarding to an explicitly supported remote target form.
- [x] Provide a clear close/shutdown mechanism for active forwards.
- [x] Avoid claiming full official `adb forward` compatibility unless the design proves it.

Tests:

- [x] Client tests cover local listener lifecycle and stream bridging through `internal/fakeadb` or focused in-memory tests.
- [x] Client tests cover close/shutdown behavior.
- [x] `go test ./...` passes

Done when:

- [x] Library callers can forward local TCP connections to a supported remote device endpoint through adb-go.

### M44.3 — Add CLI forward command

Status: Not started

Commit: `feat(cli): add forward command`

Tasks:

- [ ] Add an `adb-go forward` command based on the client forwarding API.
- [ ] Support the same TCP, Linux USB, and `--auth-key` connection flags as other device commands.
- [ ] Support only the remote/local target forms implemented by the library.
- [ ] Make foreground lifetime and shutdown behavior explicit.

Tests:

- [ ] CLI tests cover usage, argument validation, connection flag propagation, successful forward setup, and errors.
- [ ] `go test ./...` passes

Done when:

- [ ] CLI users can start a supported adb-go forwarding session with clear lifecycle behavior.

### M44.4 — Document forwarding support and limitations

Status: Not started

Commit: `docs: document forwarding support`

Tasks:

- [ ] Update README examples and limitations to mention forwarding support.
- [ ] Update `client/README.md` with forwarding helper examples and lifecycle behavior.
- [ ] Update `cmd/adb-go/README.md` with CLI usage and examples for TCP/USB/auth.
- [ ] Clearly document differences from official `adb forward`, especially if adb-go runs foreground forwarding rather than registering state in an adb server.

Tests:

- [ ] Documentation examples compile where applicable.
- [ ] `go test ./...` passes

Done when:

- [ ] Users can discover forwarding support and understand how adb-go's behavior differs from official adb-server-backed forwarding.

## Deferred milestones

These remain out of the active plan unless promoted into a concrete milestone using the template above.

### Cross-platform USB

Potential future commit series depends on platform research:

- `feat(usb): add macos usb transport`
- `feat(usb): add windows usb transport`

These may require different OS APIs from Linux usbfs. Preserve the pure-Go preference and avoid cgo/native dependencies unless a future decision changes this.
