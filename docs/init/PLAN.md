# adb-go Implementation Plan

This plan is organized as small milestones. Each milestone should be implemented as one focused Conventional Commit.

## Progress

- Current milestone: M62 — Extract custom CLI mode package.
- Completed milestone range: Milestones 17–61 completed the initial CLI shell/push/pull work, CLI documentation, Linux USB transport design, transport abstraction, Linux USB discovery, Linux usbfs bulk transport, high-level USB connection API, CLI USB connection option, USB documentation, adb-go-specific target listing, explicit ADB authentication support, the client APK install helper, the CLI `install-apk` command, APK install documentation, logcat library/CLI/documentation support, property helpers, screencap support, reboot library/CLI/documentation support, foreground port forwarding support, the minimal adb-god daemon foundation, Linux systemd user-service install/lifecycle controls, Linux systemd user-service status reporting, daemon diagnostics, daemon service logs, daemon service reinstall, CLI version reporting, CLI error-message improvements, integration-test documentation, exported package documentation/examples, the daemon-backed persistent forwarding design, the daemon forwarding protocol model, daemon-owned forwarding listener registration, TCP target bridging for persistent forwards, CLI persistent forwarding controls, and persistent forwarding diagnostics.
- Active focus: split the CLI into clearly separated custom and adb-compatibility modes. Custom mode preserves the current adb-go UX. Compat mode will target current Android SDK Platform-Tools `adb` CLI behavior closely enough that a future `adb` symlink can use adb-go as a drop-in replacement for supported workflows.
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

Status: Not started

Commit: `refactor(cli): extract custom mode package`

Tasks:

- [ ] Move the current `cmd/adb-go` CLI implementation into `cmd/adb-go/internal/custom`.
- [ ] Expose a `custom.Run(args []string, stdout, stderr io.Writer) int` entrypoint for the moved implementation.
- [ ] Keep the current adb-go custom UX, command names, flags, usage text, and behavior unchanged.
- [ ] Leave `cmd/adb-go/main.go` as a thin compatibility wrapper around custom mode only; do not add mode detection yet.
- [ ] Update package names/imports and test package references after the move.

Tests:

- [ ] Existing CLI tests pass without intentional expectation changes.
- [ ] `go test ./...` passes

Done when:

- [ ] The current adb-go CLI behavior is preserved exactly while the code lives under `cmd/adb-go/internal/custom`.

## M63 — Add CLI mode router

Status: Not started

Commit: `feat(cli): add custom and compat mode router`

Tasks:

- [ ] Replace `cmd/adb-go/main.go` with a tiny router that only detects mode and dispatches to the selected implementation.
- [ ] Select compat mode when `ADB_GO_MODE=compat` or the executable basename is exactly `adb` or `adb.exe`.
- [ ] Select custom mode when `ADB_GO_MODE=custom` or when no compat signal is present.
- [ ] Reject any other non-empty `ADB_GO_MODE` value with exit code `2`.
- [ ] Do not support bootstrap flags such as `--compat` or `--custom`.
- [ ] Add a temporary compat placeholder that returns a clear “compat mode is not implemented yet” error with exit code `1`.

Tests:

- [ ] Router tests cover default custom mode, `ADB_GO_MODE=custom`, `ADB_GO_MODE=compat`, invalid `ADB_GO_MODE`, and `adb`/`adb.exe` basename detection.
- [ ] Existing custom mode CLI tests continue to pass.
- [ ] `go test ./...` passes

Done when:

- [ ] The binary can route cleanly between custom mode and a compat placeholder without changing custom behavior.

## M64 — Add adb-compatible CLI skeleton

Status: Not started

Commit: `feat(cli): add adb compat skeleton`

Tasks:

- [ ] Add `cmd/adb-go/internal/compat` with an independent `compat.Run(args []string, stdout, stderr io.Writer) int` implementation.
- [ ] Implement compat `help`, `--help`, no-args, unknown command, and `version` behavior using the current Android SDK Platform-Tools `adb` output shape as the reference.
- [ ] Make compat `version` adb-shaped but explicit that the implementation is adb-go, for example with an `(adb-go compat)` marker or equivalent wording.
- [ ] Keep custom-only commands and flags such as `targets`, `daemon`, `install-apk`, `--addr`, `--usb`, and `ADB_GO_ADDR` out of compat mode.
- [ ] Parse official-style global options enough for host-only skeleton commands, including `-H` and `-P` before `version`.

Tests:

- [ ] Compat tests cover `help`, `--help`, no args, `-h`, unknown commands, `version`, and global `-H`/`-P` with `version`.
- [ ] Snapshot-style assertions cover important stdout/stderr placement and exit codes from the captured official adb reference.
- [ ] `go test ./...` passes

Done when:

- [ ] Compat mode has a real adb-shaped host-only skeleton while remaining independent from custom mode.

## M65 — Add compat daemon/server foundation

Status: Not started

Commit: `feat(cli): add compat daemon foundation`

Tasks:

- [ ] Add compat implementations for `start-server`, `kill-server`, and `devices` on top of adb-go daemon internals/protocol rather than the official adb server protocol.
- [ ] Auto-start the adb-go daemon for compat commands that need server state.
- [ ] Make `adb devices` show daemon-known TCP devices from prior compat `connect` work and locally discoverable USB devices when available.
- [ ] Keep broad blind TCP/emulator scanning out of the initial compat `devices` behavior.
- [ ] Preserve official adb CLI-facing output shape where practical while allowing different daemon internals.

Tests:

- [ ] Compat tests cover server lifecycle commands and auto-start behavior.
- [ ] Compat tests cover no-device `devices` output matching the official reference shape.
- [ ] Daemon tests cover any new protocol/state needed by compat devices listing.
- [ ] `go test ./...` passes

Done when:

- [ ] Compat mode has adb-shaped server lifecycle and device-list foundations backed by adb-go daemon internals.

## M66 — Add compat target selection foundation

Status: Not started

Commit: `feat(cli): add compat target selection`

Tasks:

- [ ] Support official target selectors `-s SERIAL`, `-d`, and `-e` in compat mode.
- [ ] Support `$ANDROID_SERIAL`, with `-s SERIAL` taking precedence.
- [ ] Keep `ADB_GO_ADDR` ignored in compat mode.
- [ ] Map selected compat transports onto daemon/device registry entries and adb-go TCP/USB connection options.
- [ ] Defer transport IDs (`-t ID`) unless promoted into this milestone after additional reference capture.

Tests:

- [ ] Compat parser tests cover selector precedence and no-command help/exit behavior for `-s`, `-d`, and `-e`.
- [ ] Compat command tests cover selected-device resolution against fake daemon/device state.
- [ ] `go test ./...` passes

Done when:

- [ ] Compat commands can resolve devices using official adb selector mechanisms without exposing custom adb-go target flags.

## Deferred milestones

These remain out of the active plan unless promoted into a concrete milestone using the template above.

### Cross-platform USB

Potential future commit series depends on platform research:

- `feat(usb): add macos usb transport`
- `feat(usb): add windows usb transport`

These may require different OS APIs from Linux usbfs. Preserve the pure-Go preference and avoid cgo/native dependencies unless a future decision changes this.
