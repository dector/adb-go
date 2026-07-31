# adb-go Implementation Plan

This plan is organized as small milestones. Each milestone should be implemented as one focused Conventional Commit.

## Progress

- Current milestone: no active incomplete output-control milestone; only deferred cross-platform USB milestones remain.
- Completed milestone range: Milestones 17–74 completed the initial CLI shell/push/pull work, CLI documentation, Linux USB transport design, transport abstraction, Linux USB discovery, Linux usbfs bulk transport, high-level USB connection API, CLI USB connection option, USB documentation, adb-go-specific target listing, explicit ADB authentication support, the client APK install helper, the CLI `install-apk` command, APK install documentation, logcat library/CLI/documentation support, property helpers, screencap support, reboot library/CLI/documentation support, foreground and daemon-backed port forwarding foundations, CLI mode routing and adb-compatible skeleton work, compat daemon/device target-selection foundations, the reverse port forwarding design, protocol support for device-initiated streams, the direct-device client reverse TCP API, the custom foreground reverse command, daemon-owned reverse forwarding, adb-compatible reverse TCP forwarding, M73 output/empty-state UX polish, and M74 global quiet/verbose output modes.
- Active focus: output control polish with global `--quiet` and `--verbose` modes is complete.
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

## Output verbosity polish

These milestones add global output controls for custom `adb-go` commands. Compatibility-mode output should remain adb-shaped unless a milestone explicitly says otherwise.

## M74.1 — Add quiet output mode

Status: Done

Commit: `feat(cli): add quiet output mode`

Tasks:

- [ ] Add a global `--quiet` flag, plus any shared CLI option plumbing needed for command handlers to inspect it.
- [ ] Route all non-error human-facing informational output through a shared output policy so quiet mode can suppress it consistently.
- [ ] Preserve command payload output that users explicitly request or scripts consume, such as shell command stdout, file data/status required for success, and machine-readable/listing output where silence would be ambiguous.
- [ ] Ensure errors, warnings, prompts, and diagnostic failures continue to write to stderr in quiet mode.
- [ ] Document quiet-mode semantics in command help and CLI docs.

Tests:

- [ ] CLI tests cover `--quiet` suppressing success/info messages while leaving stderr errors visible.
- [ ] CLI tests cover representative payload-producing commands that must still emit requested stdout in quiet mode.
- [ ] Existing adb-compatible output tests continue to pass.
- [ ] `go test ./...` passes

Done when:

- [ ] Users can run custom commands with `--quiet` and see only stderr diagnostics plus explicitly requested command payloads.

## M74.2 — Add verbose output mode

Status: Done

Commit: `feat(cli): add verbose output mode`

Tasks:

- [ ] Add a global `--verbose` flag and reject or clearly define combinations with `--quiet`.
- [ ] Introduce a shared verbose logging/output path for extra human-facing progress and decision details that defaults to stderr.
- [ ] Add useful verbose details for representative flows such as target selection, daemon connection/use, direct USB/TCP connection attempts, and file transfer/install progress boundaries.
- [ ] Keep default output unchanged and avoid leaking verbose lines into adb-compatible or machine-readable stdout.
- [ ] Document verbose-mode semantics in command help and CLI docs.

Tests:

- [ ] CLI tests cover verbose output appearing on stderr without changing normal stdout payloads.
- [ ] CLI tests cover the selected `--quiet`/`--verbose` interaction.
- [ ] Existing default-output and compatibility tests continue to pass.
- [ ] `go test ./...` passes

Done when:

- [ ] Users can opt into additional stderr diagnostics with `--verbose` while normal command output remains stable.

## Cross-platform USB

Potential future commit series depends on platform research:

- `feat(usb): add macos usb transport`
- `feat(usb): add windows usb transport`

These may require different OS APIs from Linux usbfs. Preserve the pure-Go preference and avoid cgo/native dependencies unless a future decision changes this.
