# adb-go/protocol

Package `protocol` provides lower-level ADB protocol primitives.

It contains packet encoding/decoding, checksum and command helpers, the initial
`CNXN`/`AUTH` handshake, logical stream opening, and stream demultiplexing.
This package is useful for tests, debugging, custom transports, and advanced
protocol work.

## Contents

- [Core pieces](#core-pieces)
- [Implementation notes](#implementation-notes)
- [Stability](#stability)

For normal application code, prefer the root package or `client` package:

- [`../README.md`](../README.md)
- [`../client/README.md`](../client/README.md)

## Core pieces

- `Message`, `ReadMessage`, and `WriteMessage` implement ADB packet framing.
- `Connection` wraps an `io.ReadWriter` transport and performs handshake and
  stream management.
- `HandshakeOptions` accepts explicit authentication credentials for `AUTH`
  responses.
- `Stream` implements logical ADB streams opened with service strings such as
  `shell:echo ok` or `sync:`.

## Implementation notes

See [`../docs/README.md`](../docs/README.md) for the cross-package architecture
and protocol flow.

## Stability

`protocol` may be less stable than the root and client APIs during v0
development. Prefer `client` unless you need direct protocol access.
