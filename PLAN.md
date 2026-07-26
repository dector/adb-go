# adb-go Implementation Plan

This plan is organized as small milestones. Each milestone should be implemented as one focused Conventional Commit.

## Progress

- Current milestone: Milestone 36 — Add CLI install-apk command.
- Completed milestone range: Milestones 17–35 completed the initial CLI shell/push/pull work, CLI documentation, Linux USB transport design, transport abstraction, Linux USB discovery, Linux usbfs bulk transport, high-level USB connection API, CLI USB connection option, USB documentation, adb-go-specific target listing, explicit ADB authentication support, and the client APK install helper.
- Active focus: implement an adb-go-specific alternative to `adb install` in small slices: first a library helper, then a CLI command, then documentation.
- Completed USB direction: Linux-only first, using the kernel usbfs interface under `/dev/bus/usb` behind build tags. This remains pure Go because it talks to device files and ioctls directly instead of linking native USB libraries.

## Milestone template

Use this shape when promoting new work into the active plan:

```markdown
## Milestone N — Short milestone title

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
- Do not keep fully implemented milestone bodies in this file long term; summarize completed ranges in `Progress` instead.

## Milestone 35 — Add client APK install helper

Status: Implemented

Commit: `feat(client): add apk install helper`

Tasks:

- [x] Add a high-level client method for installing one local APK without exposing it as a full official-`adb install` clone.
- [x] Implement the install flow using existing primitives: push the APK to a generated path under `/data/local/tmp`, run `pm install` through `shell:`, then best-effort remove the temporary APK.
- [x] Add a small options type for intentionally supported install behavior, initially including replace-existing-app support only if it can map cleanly to `pm install -r`.
- [x] Keep caller-controlled local paths and package manager output explicit; do not add broad official adb flag compatibility in this milestone.
- [x] Re-export the stable install API from the root package if it is part of the high-level API.

Tests:

- [x] Client tests cover successful push/install/cleanup service flow using `internal/fakeadb`.
- [x] Client tests cover package-manager failure output returning a useful error.
- [x] Client tests cover cleanup being attempted after install failure.
- [x] `go test ./...` passes

Done when:

- [x] Library callers can install one APK on a connected device through adb-go's high-level client API, with clear limitations and no dependency on the official `adb` binary.

## Milestone 36 — Add CLI install-apk command

Status: Implemented

Commit: `feat(cli): add install-apk command`

Tasks:

- [x] Add an `adb-go install-apk` command as the adb-go-specific alternative to `adb install`.
- [x] Support the same TCP, Linux USB, and `--auth-key` connection flags as `shell`, `push`, and `pull`.
- [x] Accept exactly one local APK path, plus only the install options implemented by the client helper.
- [x] Print package-manager failure output clearly without implying compatibility with every official `adb install` flag.

Tests:

- [x] CLI tests cover usage, argument validation, connection flag propagation, success, and install failure messaging.
- [x] `go test ./...` passes

Done when:

- [x] CLI users can run `adb-go install-apk [connection flags] LOCAL_APK` to install one APK through supported adb-go transports.

## Milestone 37 — Document APK installation support and limitations

Status: Not started

Commit: `docs: document apk install command`

Tasks:

- [ ] Update README command examples and limitations to mention `install-apk` as an adb-go-specific alternative to `adb install`.
- [ ] Update `client/README.md` with the library install helper, temporary push behavior, cleanup behavior, and supported options.
- [ ] Update `cmd/adb-go/README.md` with CLI usage, examples for TCP/USB/auth, and a clear non-goal list for unsupported official `adb install` flags.
- [ ] Document security considerations: local APK path is caller-controlled, install effects happen on the connected device, and package-manager output comes from the device.

Tests:

- [ ] Documentation examples compile where applicable.
- [ ] `go test ./...` passes

Done when:

- [ ] Users can discover how to install one APK with adb-go and understand how it differs from the official `adb install` command.

## Deferred milestones

These remain out of the active plan unless promoted into a concrete milestone using the template above.

### Cross-platform USB

Potential future commit series depends on platform research:

- `feat(usb): add macos usb transport`
- `feat(usb): add windows usb transport`

These may require different OS APIs from Linux usbfs. Preserve the pure-Go preference and avoid cgo/native dependencies unless a future decision changes this.
