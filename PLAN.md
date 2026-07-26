# adb-go Implementation Plan

This plan is organized as small milestones. Each milestone should be implemented as one focused Conventional Commit.

## Progress

- Current milestone: M53 — Improve CLI error messages.
- Completed milestone range: Milestones 17–52 completed the initial CLI shell/push/pull work, CLI documentation, Linux USB transport design, transport abstraction, Linux USB discovery, Linux usbfs bulk transport, high-level USB connection API, CLI USB connection option, USB documentation, adb-go-specific target listing, explicit ADB authentication support, the client APK install helper, the CLI `install-apk` command, APK install documentation, logcat library/CLI/documentation support, property helpers, screencap support, reboot library/CLI/documentation support, foreground port forwarding support, the minimal adb-god daemon foundation, Linux systemd user-service install/lifecycle controls, Linux systemd user-service status reporting, daemon diagnostics, daemon service logs, daemon service reinstall, and CLI version reporting.
- Active focus: improve observability and supportability around the existing minimal `adb-god` daemon and CLI without adding daemon-owned ADB persistence.
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

## M49 — Add daemon diagnostics command

Status: Implemented

Commit: `feat(cli): add daemon doctor command`

Tasks:

- [x] Add `adb-go daemon doctor` as a read-only diagnostics command.
- [x] Report the resolved daemon socket path and whether a daemon responds to the socket protocol.
- [x] On Linux, report systemd user-service active/enabled state when `systemctl` is available.
- [x] Print actionable hints for common states such as missing socket, daemon not responding, service inactive, or service disabled.
- [x] Keep the command diagnostic-only: do not start, stop, install, uninstall, or mutate daemon/service state.
- [x] Document the daemon doctor command in README and CLI docs.

Tests:

- [x] CLI tests cover successful daemon socket checks using an isolated test daemon.
- [x] CLI tests cover missing socket and non-running daemon diagnostics.
- [x] CLI tests cover Linux systemd status diagnostics using an isolated fake `systemctl` path.
- [x] `go test ./...` passes

Done when:

- [x] Users can run one command to understand whether `adb-god` is reachable, what socket path is being used, and what systemd reports for the user service.

## M50 — Add daemon service logs command

Status: Implemented

Commit: `feat(cli): add daemon service logs command`

Tasks:

- [x] Add `adb-go daemon service logs` for Linux systemd user-service log viewing.
- [x] Invoke `journalctl --user -u adb-god.service` with a small, predictable default output shape.
- [x] Support a `--journalctl PATH` override for tests and non-default installations.
- [x] Consider minimal flags such as `--follow` and `--lines N` without trying to mirror all `journalctl` options.
- [x] Document the service logs command in README and CLI docs.

Tests:

- [x] CLI tests cover `journalctl` invocation using an isolated fake binary path.
- [x] CLI tests cover flags selected for the milestone, such as `--follow` or `--lines` if added.
- [x] `go test ./...` passes

Done when:

- [x] Linux users can inspect recent `adb-god.service` logs from the adb-go CLI without remembering the exact `journalctl` command.

## M51 — Add daemon service reinstall command

Status: Implemented

Commit: `feat(cli): add daemon service reinstall command`

Tasks:

- [x] Add `adb-go daemon service reinstall` for rewriting the systemd user unit and restarting it.
- [x] Reuse the install path resolution, socket selection, unit rendering, and systemctl override behavior.
- [x] Run the systemd operations needed to reload units and restart/enable the service after rewriting the unit.
- [x] Preserve `install`, `uninstall`, and lifecycle command semantics.
- [x] Document when to use reinstall, such as after changing the daemon binary path or socket path.

Tests:

- [x] CLI tests cover unit rewriting and systemctl invocation order using isolated test paths.
- [x] CLI tests cover daemon binary, unit-directory, socket, and systemctl overrides.
- [x] `go test ./...` passes

Done when:

- [x] Users can update the installed `adb-god.service` unit from the CLI without manually uninstalling and reinstalling.

## M52 — Add CLI version reporting

Status: Implemented

Commit: `feat(cli): add version command`

Tasks:

- [x] Add `adb-go version`.
- [x] Print the adb-go version value, Go runtime version, target OS, and target architecture.
- [x] Provide build-time version injection through standard Go linker variables while keeping a useful development fallback.
- [x] Document the version command in README and CLI docs.

Tests:

- [x] CLI tests cover the default development version output shape.
- [x] Unit tests cover version formatting if implemented outside the command dispatcher.
- [x] `go test ./...` passes

Done when:

- [x] Users can collect concise adb-go build/runtime information for support requests and bug reports.

## M53 — Improve CLI error messages

Status: Implemented

Commit: `fix(cli): improve common error messages`

Tasks:

- [x] Audit current CLI error output for common failures: connection refused, timeout, auth required, unsupported USB platform, destination exists, and missing local files.
- [x] Add small formatting helpers where they make errors clearer without hiding wrapped sentinel errors in library code.
- [x] Keep errors concise and actionable, with examples where useful.
- [x] Avoid changing public library error semantics unless a specific bug is found and scoped.
- [x] Document any user-visible behavior changes in CLI docs if needed.

Tests:

- [x] CLI tests cover at least the most important improved error messages.
- [x] Existing library tests continue to validate sentinel error behavior.
- [x] `go test ./...` passes

Done when:

- [x] Common CLI failures point users toward the likely fix without changing the underlying adb-go library contract.

## M54 — Expand integration test documentation

Status: Implemented

Commit: `docs: expand integration test guidance`

Tasks:

- [x] Document `ADB_GO_INTEGRATION_ADDR` usage with emulator TCP examples.
- [x] Document expected prerequisites and limitations for real-device TCP tests.
- [x] Document the existing Linux adbd container workflow if it is stable enough for contributors.
- [x] Add troubleshooting notes for skipped tests, refused TCP connections, and auth-required devices.

Tests:

- [x] Documentation examples are command-line examples only; no new code tests required unless examples are compile-tested.
- [x] `go test ./...` passes

Done when:

- [x] Contributors can discover and run the optional integration tests without reading test source first.

## M55 — Audit exported package documentation and examples

Status: Not started

Commit: `docs: audit package examples`

Tasks:

- [ ] Review exported API docs in the root, `client`, and `protocol` packages.
- [ ] Add or update examples for newer features such as logcat, screencap, reboot, APK install, and foreground forwarding where compile-tested examples are practical.
- [ ] Ensure `protocol` documentation continues to communicate that it is lower-level and less stable than root/client APIs during v0.
- [ ] Avoid changing runtime behavior unless a documentation example exposes an API bug that is explicitly fixed in this milestone.

Tests:

- [ ] Compile-tested examples pass.
- [ ] `go test ./...` passes

Done when:

- [ ] Public API documentation reflects the current feature set and protects basic examples against API drift.

## M56 — Design daemon-backed persistent forwarding

Status: Not started

Commit: `docs(daemon): design persistent forwarding`

Tasks:

- [ ] Design how future daemon-owned foreground/background forwards should be represented, listed, and removed.
- [ ] Define CLI command shapes for persistent forwards, including possible `forward --background`, `forward --list`, `forward --remove`, and `forward --remove-all` behavior.
- [ ] Define daemon protocol additions needed to manage forwarding state.
- [ ] Specify lifecycle semantics for daemon shutdown, systemd restart, target disconnects, and port conflicts.
- [ ] Keep this milestone documentation-only; do not implement daemon-owned forwarding yet.

Tests:

- [ ] No code tests required for the design milestone.
- [ ] `go test ./...` passes

Done when:

- [ ] Persistent forwarding has a concrete, reviewable design that can be sliced into future implementation milestones.

## Deferred milestones

These remain out of the active plan unless promoted into a concrete milestone using the template above.

### Cross-platform USB

Potential future commit series depends on platform research:

- `feat(usb): add macos usb transport`
- `feat(usb): add windows usb transport`

These may require different OS APIs from Linux usbfs. Preserve the pure-Go preference and avoid cgo/native dependencies unless a future decision changes this.
