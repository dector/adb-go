# adb-go Implementation Plan

This plan is organized as small milestones. Each milestone should be implemented as one focused Conventional Commit.

## Progress

- Current milestone: Milestone 14 — Containerized adbd instrumented integration tests
- Completed:
  - Milestone 1 — Project specification
  - Milestone 2 — Initialize Go module and package skeleton
- Implemented / review pending:
  - Milestone 3 — Add protocol message encoding
  - Milestone 4 — Add low-level connection handshake
  - Milestone 5 — Add fake ADB server foundation
  - Milestone 6 — Add stream demultiplexing
  - Milestone 7 — Add high-level client connect/open service
  - Milestone 8 — Add shell support
  - Milestone 9 — Add sync protocol single-file pull
  - Milestone 10 — Add sync protocol single-file push
  - Milestone 11 — Add public docs and examples
  - Milestone 12 — Add optional integration tests
  - Milestone 13 — Local hardening before CI
  - Milestone 14 — Containerized adbd instrumented integration tests
- Next milestone after containerized adbd instrumented integration tests are accepted: CI

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

Status: Implemented

Commit: `feat(protocol): add adb stream demultiplexing`

Tasks:

- [x] Implement stream type with local/remote IDs
- [x] Start one reader goroutine per protocol connection
- [x] Route packets to streams by ID
- [x] Serialize writes with a mutex
- [x] Implement `Open(service string)` at low level
- [x] Make streams implement `io.Reader`, `io.Writer`, and `io.Closer` where practical
- [x] Handle `OKAY`, `WRTE`, and `CLSE`
- [x] Define close/error behavior for remote close and connection close

Tests:

- [x] Open service sends `OPEN`
- [x] `OKAY` establishes stream
- [x] `WRTE` data is readable from correct stream
- [x] Multiple streams receive correct data
- [x] `CLSE` closes stream

Done when:

- [x] Low-level protocol can open services and exchange stream data against fake server

## Milestone 7 — Add high-level client connect/open service

Status: Implemented

Commit: `feat(client): add tcp connect and service opening`

Tasks:

- [x] Implement `client.Connect` / `client.ConnectTCP`
- [x] Default omitted port to `5555`
- [x] Use `net.Dialer` with context
- [x] Perform full ADB handshake before returning
- [x] Implement `Client.Close`
- [x] Implement `Client.OpenService(ctx, service string)`
- [x] Re-export high-level API from root package `adb`

Tests:

- [x] Address normalization with and without port
- [x] Connect succeeds against fake server
- [x] Auth response maps to high-level `ErrAuthRequired`
- [x] `OpenService` works against fake service

Done when:

- [x] Users can connect to a fake ADB server and open a generic service through high-level API

## Milestone 8 — Add shell support

Status: Implemented

Commit: `feat(client): add shell support`

Tasks:

- [x] Implement `Client.Shell(ctx, cmd string) ([]byte, error)`
- [x] Implement `Client.ShellStream(ctx, cmd string, stdout io.Writer) error`
- [x] Use service string `shell:<cmd>`
- [x] Keep command input as a single string
- [x] Support context cancellation by closing stream/connection as needed

Tests:

- [x] Shell captures output against fake server
- [x] Shell streaming writes to provided writer
- [x] Shell uses exact single command string
- [x] Context cancellation unblocks operation

Done when:

- [x] Basic shell command execution works through the high-level client

## Milestone 9 — Add sync protocol single-file pull

Status: Implemented

Commit: `feat(client): add single-file pull support`

Tasks:

- [x] Implement enough ADB `sync:` protocol for single-file pull
- [x] Add `PullFile(ctx, remotePath, localPath)`
- [x] Add `PullFileWithOptions(ctx, remotePath, localPath, PullOptions)`
- [x] `PullFile` returns an error if local destination exists
- [x] `PullOptions{Overwrite:true}` allows overwrite

Tests:

- [x] Pull writes file contents from fake server
- [x] Existing destination without overwrite returns error
- [x] Existing destination with overwrite succeeds
- [x] Remote errors are surfaced clearly

Done when:

- [x] Single-file pull works against fake server

## Milestone 10 — Add sync protocol single-file push

Status: Implemented

Commit: `feat(client): add single-file push support`

Tasks:

- [x] Implement enough ADB `sync:` protocol for single-file push
- [x] Add `PushFile(ctx, localPath, remotePath)`
- [x] Use default remote mode `0644`
- [x] Add TODO/design seam for future mode/mtime options

Tests:

- [x] Push sends file contents to fake server
- [x] Missing local file returns useful error
- [x] Default mode is `0644`
- [x] Remote errors are surfaced clearly

Done when:

- [x] Single-file push works against fake server

## Milestone 11 — Add public docs and examples

Status: Implemented

Commit: `docs: add usage documentation and examples`

Tasks:

- [x] Write README
- [x] Include limitation/difference list vs official `adb`
- [x] Include architecture section
- [x] Include quick examples:
  - [x] connect
  - [x] shell
  - [x] shell streaming
  - [x] push file
  - [x] pull file
- [x] Add compile-tested Go examples where possible
- [x] Document `protocol` package as lower-level and less stable during v0

Tests:

- [x] `go test ./...` passes including examples

Done when:

- [x] A new user can understand current capabilities and limitations from README

## Milestone 12 — Add optional integration tests

Status: Implemented

Commit: `test: add optional adb integration tests`

Tasks:

- [x] Add integration tests skipped unless `ADB_GO_INTEGRATION_ADDR` is set
- [x] Test connect and shell against real emulator/device when available
- [x] Add docs for running integration tests

Done when:

- [x] `go test ./...` skips integration tests by default
- [x] `ADB_GO_INTEGRATION_ADDR=127.0.0.1:5555 go test ./...` runs integration tests

## Milestone 13 — Local hardening before CI

Status: Implemented

Commit: `test: harden protocol and client behavior`

Tasks:

- [x] Add edge case tests discovered during implementation
- [x] Improve error wrapping and sentinel errors
- [x] Ensure no unexpected logging
- [x] Ensure no external dependencies
- [x] Run formatting and tests locally

Done when:

- [x] `go test ./...` passes cleanly
- [x] Public errors support `errors.Is` where intended

## Milestone 14 — Containerized adbd instrumented integration tests

Status: Implemented

Commit: `test: add containerized adbd integration tests`

Tasks:

- [x] Add optional tests gated by `ADB_GO_CONTAINER_INTEGRATION`
- [x] Use a `Containerfile` for the local Linux `adbd` image
- [x] Prefer Podman, with Docker fallback, for local container execution
- [x] Start the local Linux `adbd` container image on a random localhost port
- [x] Exercise implemented high-level functionality against a real daemon:
  - [x] connect
  - [x] shell
  - [x] shell streaming
  - [x] generic service opening
  - [x] single-file push
  - [x] single-file pull
- [x] Document how to build/reuse the container image for instrumented tests

Done when:

- [x] `go test ./...` skips container integration tests by default
- [x] `ADB_GO_CONTAINER_INTEGRATION=1 ADB_GO_CONTAINER_BUILD=1 go test -timeout 30m ./...` runs them against containerized `adbd`

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
