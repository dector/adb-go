// Package client provides the high-level ADB client API.
//
// The package owns transport selection, the initial ADB CNXN handshake, AUTH
// error mapping, and convenient operations built on ADB service streams. Use
// ConnectTCP or ConnectUSB to create a Client, then call methods such as Shell,
// PushFile, PullFile, InstallAPK, Logcat, GetProp, Screencap, Reboot, or
// ForwardLocalTCP.
//
// All blocking public operations accept context.Context. When cancellation must
// unblock an in-flight ADB read or write, adb-go may close the underlying stream
// or connection; callers should treat canceled clients as disposable unless a
// method explicitly documents narrower behavior.
package client
