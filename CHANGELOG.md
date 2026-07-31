# Changelog

## [Unreleased]

### Added

- Server-owned persistent TCP forwarding protocol, listener registry, ADB TCP bridging, and CLI controls through `adb-go forward --background`, `--list`, `--remove`, `--remove-id`, and `--remove-all`.
- Forwarding health diagnostics in server status/doctor output, including counts and degraded-state hints.
- Common Android property name constants for high-level property helpers.
- README overview diagrams, feature table, and reorganized installation guidance.

### Changed

- Moved detailed `adb-gos` server documentation out of the README and into dedicated docs.
- Expanded integration-test guidance and audited exported package examples.
- Improved milestone workflow documentation for continued implementation planning.
- Release artifacts now include version-tag releases, checksums, and a checksums alias.

### Fixed

- Improved common CLI error messages.
- Shortened server test socket paths and made server command tests more portable.

## [0.1.0] - 2026-07-26

### Added

- Pure-Go ADB packet encoding/decoding, connection handshake, authentication handshake support, and stream demultiplexing.
- High-level client API for explicit TCP connections, service opening, shell execution and streaming, single-file push/pull, APK installation, logcat, Android properties, screencap, reboot, and local TCP forwarding.
- Linux USB support using `/dev/bus/usb` directly, including ADB interface discovery, bulk transport, USB connection API, and CLI USB target option.
- Explicit-key ADB authentication helpers for loading and using existing RSA private keys.
- Experimental `adb-go` CLI with shell, push, pull, install-apk, logcat, getprop, screencap, reboot, forward, target listing, server controls, service management, diagnostics, logs, reinstall, and version commands.
- Initial `adb-gos` Unix-socket server foundation with ping, status, shutdown, diagnostics, and Linux systemd user-service lifecycle helpers.
- Fake ADB server tests, optional external integration tests, containerized Linux `adbd` integration tests, and cross-platform CI for Go tests.
- Release tooling for Git-derived CLI versions and Linux snapshot publishing.
- Project documentation covering architecture, authentication, Linux USB, integration testing, server foundation, forwarding design, package usage, and implementation planning.

### Changed

- Abstracted client transport dialing to support TCP and Linux USB transports behind the same high-level workflows.


[Unreleased]: https://github.com/dector/adb-go/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/dector/adb-go/releases/tag/v0.1.0
