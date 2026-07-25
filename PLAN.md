# adb-go Implementation Plan

This plan is organized as small milestones. Each milestone should be implemented as one focused Conventional Commit.

## Progress

- Current milestone: Milestone 29 — adb-go targets listing
- Milestones 17–28 completed the initial CLI shell/push/pull work, CLI documentation, Linux USB transport design, transport abstraction, Linux USB discovery, Linux usbfs bulk transport, high-level USB connection API, CLI USB connection option, and USB documentation.
- Active focus: provide an adb-go-specific alternative to `adb devices` that lists connection selectors supported by this library without depending on the official adb server.
- Preferred implementation direction: Linux-only first, using the kernel usbfs interface under `/dev/bus/usb` behind build tags. This is still pure Go because it talks to device files and ioctls directly instead of linking native USB libraries.

## Milestone 22 — USB feasibility and Linux transport design

Status: Implemented

Commit: `docs: design linux usb transport`

Tasks:

- [x] Document the Linux-only USB approach before coding
- [x] Identify the required Linux usbfs ioctls and descriptor parsing needed for ADB
- [x] Decide whether to use only the standard library syscall surface or add a pure-Go helper dependency such as `golang.org/x/sys/unix`
- [x] Define the internal transport seam so TCP and USB can both provide an `io.ReadWriteCloser` to `protocol.NewConnection`
- [x] Document Linux permission requirements, such as udev rules or running with sufficient access to `/dev/bus/usb/*/*`
- [x] Keep non-Linux builds compiling with USB APIs returning a clear unsupported error

Tests:

- [x] `go test ./...` passes
- [x] Design notes include enough detail to review before implementation

Done when:

- [x] The project has an agreed Linux USB design that preserves pure-Go builds and keeps TCP behavior unchanged

## Milestone 23 — Transport abstraction

Status: Implemented

Commit: `refactor(client): abstract adb transport dialing`

Tasks:

- [x] Introduce a small internal transport/dial abstraction for connection setup
- [x] Move TCP dialing behind the shared transport path without changing public TCP APIs
- [x] Preserve `adb.Connect` and `adb.ConnectTCP` behavior exactly
- [x] Ensure future USB dialing can return an `io.ReadWriteCloser` that is passed into `protocol.NewConnection`
- [x] Keep authentication behavior unchanged: `AUTH` maps to high-level `ErrAuthRequired`

Tests:

- [x] Existing TCP client tests still pass
- [x] Add tests proving `ConnectTCP` uses the shared handshake path
- [x] `go test ./...` passes

Done when:

- [x] TCP is implemented through a transport seam and no USB code is required yet

## Milestone 24 — Linux USB device discovery primitives

Status: Implemented

Commit: `feat(usb): discover linux adb interfaces`

Tasks:

- [x] Add an internal Linux-only USB package behind `//go:build linux`
- [x] Enumerate candidate device nodes under `/dev/bus/usb`
- [x] Read and parse USB device/configuration/interface/endpoint descriptors in pure Go
- [x] Match ADB interfaces by class `0xff`, subclass `0x42`, protocol `0x01`
- [x] Select bulk IN and bulk OUT endpoints for the matched ADB interface
- [x] Return structured candidates that include bus/device path, interface number, and endpoint addresses
- [x] Add non-Linux stubs that return `ErrUnsupported` or an internal unsupported error

Tests:

- [x] Descriptor parser unit tests cover representative ADB descriptor bytes
- [x] Discovery logic can be tested against fixture descriptor data without real USB hardware
- [x] Non-Linux stubs compile where applicable
- [x] `go test ./...` passes

Done when:

- [x] adb-go can identify ADB-capable USB interfaces from Linux descriptor data without opening a live ADB session

## Milestone 25 — Linux usbfs bulk transport

Status: Implemented

Commit: `feat(usb): add linux bulk transport`

Tasks:

- [x] Open selected `/dev/bus/usb/*/*` device files
- [x] Claim the selected ADB interface through usbfs ioctl calls
- [x] Implement bulk IN reads and bulk OUT writes as an `io.ReadWriteCloser`
- [x] Release the interface and close the file on `Close`
- [x] Respect context cancellation by closing the device file to unblock pending USB operations
- [x] Return clear permission and unsupported-platform errors
- [x] Keep implementation pure Go and Linux-only behind build tags

Tests:

- [x] Unit tests cover error mapping and close semantics where possible without hardware
- [x] Add optional Linux USB integration test gated by an environment variable, for example `ADB_GO_USB_INTEGRATION=1`
- [x] Existing TCP tests continue to pass
- [x] `go test ./...` passes without USB hardware

Done when:

- [x] The Linux USB transport can move raw bytes over ADB bulk endpoints and can be tested optionally against real hardware

## Milestone 26 — High-level USB connect API

Status: Implemented

Commit: `feat(client): connect over usb on linux`

Tasks:

- [x] Add a high-level `ConnectUSB(ctx context.Context, opts USBOptions) (*Client, error)` API
- [x] Add root package re-exports for USB options and `ConnectUSB`
- [x] Define `USBOptions` for explicit device selection, such as serial, vendor/product ID, or bus/device path
- [x] Require explicit selection when multiple ADB USB devices are present
- [x] Perform the normal ADB `CNXN` handshake over the USB transport
- [x] Preserve `ErrAuthRequired` behavior for authenticated devices
- [x] Return `ErrUnsupported` on non-Linux platforms for the initial implementation

Tests:

- [x] Unit tests cover option validation and multi-device ambiguity
- [x] Optional real-device integration test is gated and skipped by default
- [x] `go test ./...` passes

Done when:

- [x] Library users can connect to one explicitly selected Linux USB ADB device and then use existing shell/push/pull/service APIs

## Milestone 27 — CLI USB connect option

Status: Implemented

Commit: `feat(cmd): add usb connection option`

Tasks:

- [x] Add CLI flags for USB selection, such as `--usb`, `--serial`, or `--usb-path`
- [x] Keep TCP `--addr` behavior unchanged
- [x] Reject ambiguous combinations such as both TCP address and USB selection in one command
- [x] Use the shared CLI connection path for shell, push, and pull
- [x] Keep missing or ambiguous USB device errors clear and actionable
- [x] Document that USB CLI support is Linux-only initially

Tests:

- [x] CLI parsing tests cover TCP, USB, missing selection, and conflicting flags
- [x] Existing shell/push/pull TCP tests continue to pass
- [x] Optional USB integration test remains gated by environment
- [x] `go test ./...` passes

Done when:

- [x] CLI users on Linux can run shell/push/pull through a selected USB ADB device, while TCP workflows continue unchanged

## Milestone 28 — USB documentation

Status: Implemented

Commit: `docs: document linux usb support`

Tasks:

- [x] Update README with Linux USB status and examples
- [x] Document permission setup and troubleshooting for `/dev/bus/usb`
- [x] Document USB limitations: Linux-only first, no auth until auth milestone, no broad device management compatibility
- [x] Document optional USB integration test environment variables
- [x] Update architecture notes to describe TCP and USB transports sharing the same ADB protocol layer

Tests:

- [x] `go test ./...` passes
- [x] README examples match implemented API and CLI flags

Done when:

- [x] Users can understand how to try Linux USB support, why it may fail due to permissions or auth, and what remains unsupported

## Milestone 29 — adb-go targets listing

Status: Implemented

Commit: `feat(cmd): list adb-go connection targets`

Tasks:

- [x] Add a public USB candidate listing API for locally visible ADB USB interfaces
- [x] Add a CLI command that acts as an adb-go-specific alternative to `adb devices`
- [x] Show copyable connection selectors such as `--addr` from `ADB_GO_ADDR` and `--usb-path` for Linux USB candidates
- [x] Avoid pretending to be a full official adb server device-state listing
- [x] Keep unsupported USB platforms graceful and actionable
- [x] Document how the command differs from `adb devices`

Tests:

- [x] Unit tests cover USB candidate API mapping and unsupported behavior
- [x] CLI tests cover target listing, unsupported USB, and USB discovery errors
- [x] `go test ./...` passes

Done when:

- [x] Users have a supported way to discover adb-go connection selectors without requiring the official `adb devices` command or adb server

## Deferred milestones

These remain out of the immediate Linux USB transport slice unless promoted into the active plan.

### Authentication

Potential commit series:

- `feat(auth): add adb rsa key loading`
- `feat(auth): add adb authentication handshake`
- `docs: document adb authentication setup`

### Cross-platform USB

Potential future commit series depends on platform research:

- `feat(usb): add macos usb transport`
- `feat(usb): add windows usb transport`

These may require different OS APIs from Linux usbfs. Preserve the pure-Go preference and avoid cgo/native dependencies unless a future decision changes this.
