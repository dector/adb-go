// Package protocol provides low-level ADB protocol primitives.
//
// It exposes raw ADB packet fields through Message, packet encoding and
// decoding through ReadMessage and WriteMessage, the CNXN/AUTH handshake through
// Connection, and logical ADB service streams through Stream. ADB streams are
// multiplexed over one transport connection: OPEN creates a stream, OKAY
// acknowledges stream state and writes, WRTE carries payload bytes, and CLSE
// closes the stream.
//
// This package is intended for protocol-aware callers, tests, and debugging
// tools. It may be less stable than the root and client packages during v0
// development; application code should prefer package adb or package client
// unless it needs raw packet control.
package protocol
