# adb-go Implementation Plan

This plan is organized as small milestones. Each milestone should be implemented as one focused Conventional Commit.

## Progress

- Current milestone: Milestone 20 — CLI connect ergonomics
- Completed milestones have been removed from this file to keep the active plan focused.
- Next milestone after CLI connect ergonomics is accepted: CLI documentation

## Milestone 17 — CLI shell command

Status: Implemented

Commit: `feat(cmd): add shell command`

Tasks:

- [x] Add a `shell` subcommand to `cmd/adb-go`
- [x] Accept an explicit device address flag, e.g. `--addr 127.0.0.1:5555`
- [x] Treat remaining arguments as one shell command string
- [x] Connect using the existing high-level `adb.Connect` API
- [x] Stream command output to stdout using `Client.ShellStream`
- [x] Write clear errors to stderr and return a non-zero exit code on failure
- [x] Keep behavior TCP-only and explicit-address-only for v0

Tests:

- [x] CLI dispatch recognizes `shell`
- [x] Missing `--addr` returns a clear usage error
- [x] Missing shell command returns a clear usage error
- [x] Shell command arguments are joined into the intended single command string
- [x] Shell output is streamed to stdout against a fake server or test hook

Done when:

- [x] `go test ./...` passes
- [x] A user can run a command shaped like `adb-go shell --addr 127.0.0.1:5555 echo hello`

## Milestone 18 — CLI push command

Status: Implemented

Commit: `feat(cmd): add push command`

Tasks:

- [x] Add a `push` subcommand to `cmd/adb-go`
- [x] Accept an explicit device address flag, e.g. `--addr 127.0.0.1:5555`
- [x] Accept exactly two positional arguments: local path and remote path
- [x] Connect using the existing high-level `adb.Connect` API
- [x] Transfer the file using `Client.PushFile`
- [x] Write clear errors to stderr and return a non-zero exit code on failure

Tests:

- [x] CLI dispatch recognizes `push`
- [x] Missing `--addr` returns a clear usage error
- [x] Wrong argument count returns a clear usage error
- [x] Push calls the high-level file transfer path against a fake server or test hook

Done when:

- [x] `go test ./...` passes
- [x] A user can run a command shaped like `adb-go push --addr 127.0.0.1:5555 ./local.txt /data/local/tmp/local.txt`

## Milestone 19 — CLI pull command

Status: Implemented

Commit: `feat(cmd): add pull command`

Tasks:

- [x] Add a `pull` subcommand to `cmd/adb-go`
- [x] Accept an explicit device address flag, e.g. `--addr 127.0.0.1:5555`
- [x] Accept exactly two positional arguments: remote path and local path
- [x] Add an explicit `--overwrite` flag for replacing an existing local destination
- [x] Connect using the existing high-level `adb.Connect` API
- [x] Transfer the file using `Client.PullFile` or `Client.PullFileWithOptions`
- [x] Write clear errors to stderr and return a non-zero exit code on failure

Tests:

- [x] CLI dispatch recognizes `pull`
- [x] Missing `--addr` returns a clear usage error
- [x] Wrong argument count returns a clear usage error
- [x] `--overwrite` selects `PullFileWithOptions` with `PullOptions{Overwrite: true}`
- [x] Pull calls the high-level file transfer path against a fake server or test hook

Done when:

- [x] `go test ./...` passes
- [x] A user can run a command shaped like `adb-go pull --addr 127.0.0.1:5555 /data/local/tmp/remote.txt ./remote.txt`

## Milestone 20 — CLI connect ergonomics

Status: Implemented

Commit: `feat(cmd): add shared cli connection options`

Tasks:

- [x] Refactor shared CLI address parsing used by shell, push, and pull
- [x] Keep `--addr` as the explicit primary connection option
- [x] Optionally support `ADB_GO_ADDR` as a convenience fallback
- [x] Ensure command-specific usage remains clear after refactoring
- [x] Keep the CLI independent from device discovery/listing in v0

Tests:

- [x] Shared address parsing accepts `--addr`
- [x] Shared address parsing rejects missing addresses clearly
- [x] If implemented, `ADB_GO_ADDR` is used only when `--addr` is absent
- [x] Command usage output remains command-specific and helpful

Done when:

- [x] `go test ./...` passes
- [x] Shell, push, and pull use one shared path for CLI connection configuration

## Milestone 21 — CLI documentation

Status: Implemented

Commit: `docs: document adb-go cli`

Tasks:

- [x] Add README documentation for installing or running the CLI
- [x] Add examples for `shell`, `push`, and `pull`
- [x] Document the required explicit TCP address
- [x] Document that the CLI is not a complete `adb` replacement
- [x] Document skipped features such as USB, auth, discovery, and broad command compatibility

Tests:

- [x] `go test ./...` passes
- [x] README examples match the implemented CLI command shapes

Done when:

- [x] A new user can understand the current CLI capabilities and limitations from README

## Deferred milestones

These are intentionally out of v0 initial scope.

### Authentication

Potential commit series:

- `feat(auth): add adb rsa key loading`
- `feat(auth): add adb authentication handshake`
- `docs: document adb authentication setup`

### USB transport

Potential commit series depends on feasibility research. Keep pure-Go preference and avoid cgo/native dependencies unless a future decision changes this.
