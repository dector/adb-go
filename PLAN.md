# adb-go Implementation Plan

This plan is organized as small milestones. Each milestone should be implemented as one focused Conventional Commit.

## Progress

- Current milestone: Milestone 5 — Add fake ADB server foundation
- Completed:
  - Milestone 1 — Project specification
  - Milestone 2 — Initialize Go module and package skeleton
- Implemented / review pending:
  - Milestone 3 — Add protocol message encoding
  - Milestone 4 — Add low-level connection handshake
  - Milestone 5 — Add fake ADB server foundation
- Next milestone after fake ADB server foundation is accepted: Milestone 6 — Add stream demultiplexing

## Milestone 1 — Project specification

Status: Complete

Commit: `docs: add adb-go specification and implementation plan`

Tasks:

- [x] Add `SPEC.md`
- [x] Add `PLAN.md`
- [x] Keep this milestone documentation-only

Done when:

- [x] The project goals, v0 scope, package layout, API shape, limitations, and roadmap are documented
- [x] `PLAN.md` is committed

## Milestone 2 — Initialize Go module and package skeleton

Status: Complete

Commit: `chore: initialize go module and package skeleton`

Tasks:

- [x] Create `go.mod` with module path `github.com/dector/adb-go`
- [x] Use latest stable Go version
- [x] Create root package `adb`
- [x] Create packages:
  - `client`
  - `protocol`
  - `internal/fakeadb`
- [x] Add package docs where useful
- [x] Add placeholder exported errors if needed

Done when:

- [x] `go test ./...` passes
- [x] No external dependencies are introduced

## Milestone 3 — Add protocol message encoding

Status: Implemented

Commit: `feat(protocol): add adb message encoding`

Tasks:

- [x] Define ADB command type/constants, e.g. `CommandCNXN`, `CommandOPEN`, `CommandOKAY`, `CommandWRTE`, `CommandCLSE`, `CommandAUTH`
- [x] Define `Message` with raw fields:
  - `Command`
  - `Arg0`
  - `Arg1`
  - `Payload`
- [x] Implement checksum calculation
- [x] Implement command magic validation
- [x] Implement `ReadMessage` and `WriteMessage`
- [x] Add strict checksum validation

Tests:

- [x] Encode/decode round trip
- [x] Invalid checksum fails
- [x] Invalid command magic fails
- [x] Empty payload works

Done when:

- [x] Protocol message tests pass

## Milestone 4 — Add low-level connection handshake

Status: Implemented

Commit: `feat(protocol): add adb connection handshake`

Tasks:

- [x] Add low-level connection type around `net.Conn`/`io.ReadWriter`
- [x] Implement manual `Handshake` / `CNXN` exchange
- [x] Detect `AUTH` during handshake
- [x] Return/export `ErrAuthRequired` or equivalent sentinel
- [x] Preserve ability for low-level callers to observe raw `AUTH` messages
- [x] Add TODO comments for future RSA auth

Tests:

- [x] Handshake succeeds against fake peer
- [x] `AUTH` response returns `ErrAuthRequired`
- [x] Bad/unknown handshake response returns useful error

Done when:

- [x] Low-level handshake can connect to fake ADB peer

## Milestone 5 — Add fake ADB server foundation

Status: Implemented

Commit: `test: add fake adb server foundation`

Tasks:

- [x] Implement `internal/fakeadb` TCP server helper for tests
- [x] Support minimal `CNXN` handshake
- [x] Allow registering service handlers by service string
- [x] Provide test helpers for start/stop/address

Tests:

- [x] Fake server accepts connection and handshakes
- [x] Fake server shuts down cleanly

Done when:

- [x] Future client/protocol tests can use fake server without real devices

## Milestone 6 — Add stream demultiplexing

Commit: `feat(protocol): add adb stream demultiplexing`

Tasks:

- Implement stream type with local/remote IDs
- Start one reader goroutine per protocol connection
- Route packets to streams by ID
- Serialize writes with a mutex
- Implement `Open(service string)` at low level
- Make streams implement `io.Reader`, `io.Writer`, `io.Closer` where practical
- Handle `OKAY`, `WRTE`, and `CLSE`
- Define close/error behavior for remote close and connection close

Tests:

- Open service sends `OPEN`
- `OKAY` establishes stream
- `WRTE` data is readable from correct stream
- Multiple streams receive correct data
- `CLSE` closes stream

Done when:

- Low-level protocol can open services and exchange stream data against fake server

## Milestone 7 — Add high-level client connect/open service

Commit: `feat(client): add tcp connect and service opening`

Tasks:

- Implement `client.Connect` / `client.ConnectTCP`
- Default omitted port to `5555`
- Use `net.Dialer` with context
- Perform full ADB handshake before returning
- Implement `Client.Close`
- Implement `Client.OpenService(ctx, service string)`
- Re-export high-level API from root package `adb`

Tests:

- Address normalization with and without port
- Connect succeeds against fake server
- Auth response maps to high-level `ErrAuthRequired`
- `OpenService` works against fake service

Done when:

- Users can connect to a fake ADB server and open a generic service through high-level API

## Milestone 8 — Add shell support

Commit: `feat(client): add shell support`

Tasks:

- Implement `Client.Shell(ctx, cmd string) ([]byte, error)`
- Implement `Client.ShellStream(ctx, cmd string, stdout io.Writer) error`
- Use service string `shell:<cmd>`
- Keep command input as a single string
- Support context cancellation by closing stream/connection as needed

Tests:

- Shell captures output against fake server
- Shell streaming writes to provided writer
- Shell uses exact single command string
- Context cancellation unblocks operation

Done when:

- Basic shell command execution works through the high-level client

## Milestone 9 — Add sync protocol single-file pull

Commit: `feat(client): add single-file pull support`

Tasks:

- Implement enough ADB `sync:` protocol for single-file pull
- Add `PullFile(ctx, remotePath, localPath)`
- Add `PullFileWithOptions(ctx, remotePath, localPath, PullOptions)`
- `PullFile` returns an error if local destination exists
- `PullOptions{Overwrite:true}` allows overwrite

Tests:

- Pull writes file contents from fake server
- Existing destination without overwrite returns error
- Existing destination with overwrite succeeds
- Remote errors are surfaced clearly

Done when:

- Single-file pull works against fake server

## Milestone 10 — Add sync protocol single-file push

Commit: `feat(client): add single-file push support`

Tasks:

- Implement enough ADB `sync:` protocol for single-file push
- Add `PushFile(ctx, localPath, remotePath)`
- Use default remote mode `0644`
- Add TODO/design seam for future mode/mtime options

Tests:

- Push sends file contents to fake server
- Missing local file returns useful error
- Default mode is `0644`
- Remote errors are surfaced clearly

Done when:

- Single-file push works against fake server

## Milestone 11 — Add public docs and examples

Commit: `docs: add usage documentation and examples`

Tasks:

- Write README
- Include limitation/difference list vs official `adb`
- Include architecture section
- Include quick examples:
  - connect
  - shell
  - shell streaming
  - push file
  - pull file
- Add compile-tested Go examples where possible
- Document `protocol` package as lower-level and less stable during v0

Tests:

- `go test ./...` passes including examples

Done when:

- A new user can understand current capabilities and limitations from README

## Milestone 12 — Add optional integration tests

Commit: `test: add optional adb integration tests`

Tasks:

- Add integration tests skipped unless `ADB_GO_INTEGRATION_ADDR` is set
- Test connect and shell against real emulator/device when available
- Add docs for running integration tests

Done when:

- `go test ./...` skips integration tests by default
- `ADB_GO_INTEGRATION_ADDR=127.0.0.1:5555 go test ./...` runs integration tests

## Milestone 13 — Local hardening before CI

Commit: `test: harden protocol and client behavior`

Tasks:

- Add edge case tests discovered during implementation
- Improve error wrapping and sentinel errors
- Ensure no unexpected logging
- Ensure no external dependencies
- Run formatting and tests locally

Done when:

- `go test ./...` passes cleanly
- Public errors support `errors.Is` where intended

## Deferred milestones

These are intentionally out of v0 initial scope.

### Official CLI

Potential commit series:

- `feat(cmd): add adb-go cli skeleton`
- `feat(cmd): add shell command`
- `feat(cmd): add push and pull commands`

Initial CLI should be a thin wrapper around supported library operations only.

### Authentication

Potential commit series:

- `feat(auth): add adb rsa key loading`
- `feat(auth): add adb authentication handshake`
- `docs: document adb authentication setup`

### USB transport

Potential commit series depends on feasibility research. Keep pure-Go preference and avoid cgo/native dependencies unless a future decision changes this.

### CI

Add CI after the implementation compiles locally.

Potential commit:

- `ci: run go tests on linux macos and windows`
