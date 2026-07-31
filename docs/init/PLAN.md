# adb-go Implementation Plan

This plan is organized as small milestones. Each milestone should be implemented as one focused Conventional Commit.

## Progress

- Current milestone: none active; M73.3 is complete and only deferred cross-platform USB milestones remain.
- Completed milestone range: Milestones 17–72 completed the initial CLI shell/push/pull work, CLI documentation, Linux USB transport design, transport abstraction, Linux USB discovery, Linux usbfs bulk transport, high-level USB connection API, CLI USB connection option, USB documentation, adb-go-specific target listing, explicit ADB authentication support, the client APK install helper, the CLI `install-apk` command, APK install documentation, logcat library/CLI/documentation support, property helpers, screencap support, reboot library/CLI/documentation support, foreground and daemon-backed port forwarding foundations, CLI mode routing and adb-compatible skeleton work, compat daemon/device target-selection foundations, the reverse port forwarding design, protocol support for device-initiated streams, the direct-device client reverse TCP API, the custom foreground reverse command, daemon-owned reverse forwarding, adb-compatible reverse TCP forwarding, and M73.1–M73.2 empty-state UX polish.
- Active focus: output/UX polish from `polish2.md`, continuing with better empty states.
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

## Empty-state UX polish

These milestones cover `polish2.md` item 3: make empty command results consistently helpful, with `targets` as the style reference.

## M73.1 — Improve list-command empty states

Status: Done

Commit: `fix(cli): improve forwarding empty states`

Tasks:

- [x] Update custom `forward --list` empty output to clearly say there are no daemon-owned forwards and show the shortest creation hint.
- [x] Update custom `reverse --list` empty output to clearly say there are no daemon-owned reverse forwards and show the shortest creation hint.
- [x] Keep non-empty list output unchanged except where required for shared formatting helpers.
- [x] Ensure daemon unavailable / protocol errors still report as errors, not empty states.

Tests:

- [x] CLI tests cover empty `forward --list` and `reverse --list` output.
- [x] Existing forwarding list tests continue to pass.
- [x] `go test ./...` passes

Done when:

- [x] Forward and reverse list commands give actionable, friendly messages when the daemon has no owned registrations.

## M73.2 — Distinguish device-list empty states

Status: Done

Commit: `fix(cli): improve devices empty states`

Tasks:

- [x] Update custom `devices` output to distinguish a reachable daemon with no known devices from daemon discovery/listing failures.
- [x] Preserve script-friendly behavior for any existing machine-readable or adb-compatible device listing output.
- [x] Keep compat `adb devices` output adb-shaped; only change compat output if official adb-style behavior already permits the message.
- [x] Reuse the existing `targets` empty-state tone where it fits without making `devices` too verbose.

Tests:

- [x] CLI tests cover custom `devices` with daemon reachable and no devices.
- [x] CLI tests cover daemon unavailable/listing failure still returning an error.
- [x] Compat `devices` tests continue to pass.
- [x] `go test ./...` passes

Done when:

- [x] Users can tell the difference between “daemon running, no devices” and “could not ask the daemon for devices.”

## M73.3 — Improve missing property feedback

Status: Done

Commit: `fix(cli): report missing properties clearly`

Tasks:

- [x] Update custom `getprop NAME` behavior so a missing property reports an explicit “property not found” style message.
- [x] Preserve normal `getprop` listing behavior and successful `getprop NAME` output.
- [x] Avoid changing low-level property APIs unless needed; prefer CLI-level presentation if the transport response already exposes enough information.
- [x] Document any intentional exit-code behavior for missing properties in command help or tests.

Tests:

- [x] CLI tests cover missing `getprop NAME` output and exit code.
- [x] Existing getprop success/list tests continue to pass.
- [x] `go test ./...` passes

Done when:

- [x] Asking for a missing property gives a clear result instead of looking like blank or ambiguous command output.

## Cross-platform USB

Potential future commit series depends on platform research:

- `feat(usb): add macos usb transport`
- `feat(usb): add windows usb transport`

These may require different OS APIs from Linux usbfs. Preserve the pure-Go preference and avoid cgo/native dependencies unless a future decision changes this.
