# adb-go Implementation Plan

This plan is organized as small milestones. Each milestone should be implemented as one focused Conventional Commit.

## Progress

- Current milestone: M46 — Install adb-god as a systemd user service.
- Completed milestone range: Milestones 17–45 completed the initial CLI shell/push/pull work, CLI documentation, Linux USB transport design, transport abstraction, Linux USB discovery, Linux usbfs bulk transport, high-level USB connection API, CLI USB connection option, USB documentation, adb-go-specific target listing, explicit ADB authentication support, the client APK install helper, the CLI `install-apk` command, APK install documentation, logcat library/CLI/documentation support, property helpers, screencap support, reboot library/CLI/documentation support, foreground port forwarding support, and the minimal adb-god daemon foundation.
- Active focus: add a Linux systemd user-service installer for the existing minimal `adb-god` daemon without adding daemon-owned ADB persistence.
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

## M45 — adb-god daemon foundation

Goal: introduce the `adb-god` daemon process and a Unix domain socket control path for future persistent adb-go state, without implementing any persistent device/session functionality yet.

### M45.1 — Define adb-god daemon socket protocol and paths

Status: Implemented

Commit: `docs(daemon): design adb-god socket foundation`

Tasks:

- [x] Decide default Unix socket path behavior, including an override for tests and non-default installations.
- [x] Define the minimal request/response protocol needed only for daemon control, such as ping/status/shutdown.
- [x] Define daemon lifecycle semantics and explicit non-goals for the first daemon slice.
- [x] Document that the daemon does not yet own devices, transports, forwards, or other persistent adb-go functionality.

Tests:

- [x] No code tests required unless the design milestone includes small exploratory tests.
- [x] `go test ./...` passes

Done when:

- [x] The daemon foundation has a concrete, reviewable design that can be implemented without adding feature persistence.

### M45.2 — Add adb-god daemon server

Status: Implemented

Commit: `feat(daemon): add adb-god unix socket server`

Tasks:

- [x] Add a new `adb-god` command binary that starts a foreground daemon server.
- [x] Bind a Unix domain socket at the designed path, handling stale socket files safely.
- [x] Implement only the minimal control protocol from M45.1.
- [x] Handle graceful shutdown and cleanup of the socket file.
- [x] Keep all future persistence/device functionality out of this milestone.

Tests:

- [x] Unit or integration tests cover socket startup, ping/status behavior, shutdown, and socket cleanup.
- [x] Tests use isolated temporary socket paths.
- [x] `go test ./...` passes

Done when:

- [x] `adb-god` can run as a minimal Unix socket server and respond to daemon control requests.

### M45.3 — Add adb-go daemon control subcommand

Status: Implemented

Commit: `feat(cli): add daemon control command`

Tasks:

- [x] Add an `adb-go daemon` command group for controlling `adb-god`.
- [x] Implement minimal controls that map to the daemon protocol, such as `status`, `start`, and `stop` if supported by the M45.1 design.
- [x] Connect to the daemon over the configured Unix socket path.
- [x] Print clear errors when the daemon is not running or the socket is unavailable.
- [x] Avoid exposing any device/session persistence commands yet.

Tests:

- [x] CLI tests cover usage, socket path configuration, status success/failure, and stop/start behavior if implemented.
- [x] `go test ./...` passes

Done when:

- [x] CLI users can control the minimal `adb-god` daemon through `adb-go daemon ...` commands.

### M45.4 — Document daemon foundation

Status: Implemented

Commit: `docs: document adb-god daemon foundation`

Tasks:

- [x] Update README documentation to mention `adb-god` and the limited initial daemon scope.
- [x] Update `cmd/adb-go/README.md` with `adb-go daemon` usage.
- [x] Document Unix socket path configuration and lifecycle behavior.
- [x] Document explicitly that no persistent ADB functionality is implemented in the daemon yet.

Tests:

- [x] Documentation examples compile where applicable.
- [x] `go test ./...` passes

Done when:

- [x] Users can discover how to start/control the daemon foundation and understand that feature persistence is future work.

## M46 — Install adb-god as a systemd user service

Status: Implemented

Commit: `feat(cli): install daemon systemd user service`

Tasks:

- [x] Add `adb-go daemon install` for Linux systemd user-service installation.
- [x] Generate an `adb-god.service` user unit that starts `adb-god` with the selected control socket path.
- [x] Reload the user systemd manager and enable/start the unit by default.
- [x] Support explicit daemon binary and unit-directory overrides for non-default installations and tests.
- [x] Document the install command in README and CLI docs.

Tests:

- [x] CLI tests cover generated unit content and systemctl invocation using isolated test paths.
- [x] `go test ./...` passes

Done when:

- [x] Linux users can install and start the minimal adb-god daemon as a current-user systemd service with `adb-go daemon install`.

## Deferred milestones

These remain out of the active plan unless promoted into a concrete milestone using the template above.

### Cross-platform USB

Potential future commit series depends on platform research:

- `feat(usb): add macos usb transport`
- `feat(usb): add windows usb transport`

These may require different OS APIs from Linux usbfs. Preserve the pure-Go preference and avoid cgo/native dependencies unless a future decision changes this.
