# adb-go/client

Package `client` provides the high-level ADB client API used by the root
`github.com/dector/adb-go` package. It handles TCP dialing, Linux USB dialing
and USB candidate listing, the initial ADB `CNXN`/`AUTH` handshake, service
opening, shell helpers, logcat streaming, single-file `sync:` push/pull helpers,
and a small APK install helper.

## Contents

- [Connect over TCP](#connect-over-tcp)
- [Connect over Linux USB](#connect-over-linux-usb)
- [Authenticate with an existing ADB key](#authenticate-with-an-existing-adb-key)
- [Shell commands](#shell-commands)
- [Logcat](#logcat)
- [File transfer](#file-transfer)
- [Install one APK](#install-one-apk)
- [Open a raw service](#open-a-raw-service)
- [Target discovery helpers](#target-discovery-helpers)
- [Linux USB permissions and troubleshooting](#linux-usb-permissions-and-troubleshooting)
- [Security and trust](#security-and-trust)
- [Testing and integration](#testing-and-integration)

Most users should import the root package:

```go
import adb "github.com/dector/adb-go"
```

Import `github.com/dector/adb-go/client` directly only when you specifically
want the package-level API without root re-exports.

## Connect over TCP

```go
ctx := context.Background()

c, err := adb.Connect(ctx, "127.0.0.1:5555")
if err != nil {
    return err
}
defer c.Close()
```

If the port is omitted, adb-go defaults to the standard ADB TCP port `5555`:

```go
c, err := adb.Connect(ctx, "127.0.0.1")
```

## Connect over Linux USB

Linux builds can connect through kernel usbfs device files under
`/dev/bus/usb`. If exactly one ADB-capable USB interface is visible, pass empty
options:

```go
c, err := adb.ConnectUSB(ctx, adb.USBOptions{})
if err != nil {
    return err
}
defer c.Close()
```

When multiple Android devices are connected, select one explicitly:

```go
c, err := adb.ConnectUSB(ctx, adb.USBOptions{
    DevicePath: "/dev/bus/usb/001/002",
})

c, err = adb.ConnectUSB(ctx, adb.USBOptions{
    BusNumber:    1,
    DeviceNumber: 2,
})

c, err = adb.ConnectUSB(ctx, adb.USBOptions{
    VendorID:  0x18d1,
    ProductID: 0x4ee7,
})
```

USB support is Linux-only initially. On other platforms, `ConnectUSB` returns an
error matching `adb.ErrUnsupported`. Serial-number selection is reserved for a
future USB string-descriptor implementation.

## Authenticate with an existing ADB key

If a device replies with `AUTH`, load an existing ADB RSA private key and pass it
to the connection call explicitly:

```go
credential, err := adb.LoadPrivateKey("/home/me/.android/adbkey")
if err != nil {
    return err
}

c, err := adb.ConnectTCPWithOptions(ctx, "127.0.0.1:5555", adb.ConnectOptions{
    AuthCredentials: []adb.AuthCredential{credential},
})
```

Linux USB uses the same credential through `USBOptions`:

```go
c, err := adb.ConnectUSB(ctx, adb.USBOptions{
    DevicePath:      "/dev/bus/usb/001/002",
    AuthCredentials: []adb.AuthCredential{credential},
})
```

Key loading is intentionally explicit. adb-go does not generate keys, persist
keys, search `~/.android`, or integrate with OS keychains in v0. See
[`../auth/README.md`](../auth/README.md) and
[`../docs/authentication.md`](../docs/authentication.md).

## Shell commands

`Shell` opens the ADB service string `shell:<command>`, reads until the device
closes the stream, and returns the complete output as bytes.

```go
out, err := c.Shell(ctx, "echo hello")
if err != nil {
    return err
}
fmt.Printf("%s", out)
```

Commands are passed as one shell command string, matching official `adb shell`
usage. adb-go does not add argv-style shell escaping helpers in v0.

Use `ShellStream` when output may be large or should be displayed as it arrives:

```go
err := c.ShellStream(ctx, "pm list packages", os.Stdout)
if err != nil {
    return err
}
```

## Logcat

`Logcat` is the high-level helper for Android's log output. It opens a
`shell:` stream and runs the device command `logcat`, copying output into the
provided `io.Writer` until the device closes the stream, the writer fails, or
the context is canceled.

```go
err := c.Logcat(ctx, os.Stdout, adb.LogcatOptions{})
if err != nil {
    return err
}
```

By default this follows the device log stream, like running `adb logcat`. For a
snapshot of the current log buffers that exits when the dump is complete, set
`Dump: true`; adb-go maps that option to `logcat -d`:

```go
err := c.Logcat(ctx, os.Stdout, adb.LogcatOptions{Dump: true})
if err != nil {
    return err
}
```

The helper intentionally supports only this small option set for now. It does
not try to model the full official `adb logcat` flag surface such as filter
specs, format selection, buffer selection, file rotation, clearing buffers, or
regex filtering. Callers that need device-specific or currently unsupported
logcat flags can still use `ShellStream` directly with an explicit shell command,
for example `c.ShellStream(ctx, "logcat -v time -s ActivityManager", w)`.

Log output is remote device data. Treat it with the same care as shell output:
it may be large, long-lived, and controlled by apps and services on the selected
device. Use context deadlines or cancellation when your application needs a
bounded logcat session.

## File transfer

`PushFile` implements ADB's `sync:` service for a single file. The default
remote mode is `0644`.

```go
err := c.PushFile(ctx, "./local.txt", "/data/local/tmp/local.txt")
```

`PullFile` transfers one remote file to a new local destination:

```go
err := c.PullFile(ctx, "/data/local/tmp/remote.txt", "./remote.txt")
```

`PullFile` fails if the local destination already exists. To overwrite
deliberately, use `PullFileWithOptions`:

```go
err := c.PullFileWithOptions(ctx,
    "/data/local/tmp/remote.txt",
    "./remote.txt",
    adb.PullOptions{Overwrite: true},
)
```

Directory-aware push/pull and custom mode/mtime options are reserved for future
APIs.

## Install one APK

`InstallAPK` installs exactly one local APK on the connected device. It is a
small adb-go helper, not an attempt to mirror every `adb install` flag.

```go
err := c.InstallAPK(ctx, "./app.apk")
if err != nil {
    return err
}
```

For the currently supported replace-existing-app behavior, use
`InstallAPKWithOptions` with `InstallOptions{Replace: true}`. This maps directly
to Android package manager's `pm install -r` option:

```go
err := c.InstallAPKWithOptions(ctx, "./app.apk", adb.InstallOptions{Replace: true})
if err != nil {
    return err
}
```

The helper is built from existing ADB primitives so its behavior stays explicit:

1. Push the local APK to a generated temporary path below `/data/local/tmp` using
   `sync:` file transfer.
2. Open `shell:` and run `pm install` against that temporary remote path. With
   `Replace: true`, adb-go runs `pm install -r`.
3. Ask the device to remove the temporary APK with `rm -f` in a best-effort
   cleanup step, even when package installation fails.

If the package manager does not report success, the returned error includes the
package-manager output from the device, for example an Android failure such as
`Failure [INSTALL_FAILED_ALREADY_EXISTS]`. Cleanup errors are intentionally not
reported because the primary operation is the install result.

Local APK paths are caller-controlled, and installing an APK changes the
connected device. Package-manager output also comes from the device, so CLI and
application code should display or log it with the same care as other remote
command output.

Unsupported in this helper: directory or split-APK installation, streaming
install sessions, ABI/user/install-location/grant flags, downgrade/test-package
flags, and broad official `adb install` flag compatibility. Those can be added
later as explicit adb-go API options when they map cleanly to supported behavior.

## Open a raw service

Advanced callers can open any supported device service directly:

```go
stream, err := c.OpenService(ctx, "shell:uname -a")
if err != nil {
    return err
}
defer stream.Close()

_, err = io.Copy(os.Stdout, stream)
```

## Target discovery helpers

`ScanTCPTargets` scans a small TCP port range for ADB endpoints. By default it
scans localhost odd emulator ADB ports `5555..5585`.

`ListUSBDevices` returns locally visible USB interfaces that match ADB's
vendor-specific class/subclass/protocol.

## Linux USB permissions and troubleshooting

The Linux USB backend opens device files such as `/dev/bus/usb/001/002` and
claims the ADB interface with usbfs ioctls. If your user cannot read and write
that device node, connection fails before the ADB handshake begins.

Common permission setup options are:

- Run as root temporarily for a quick hardware test.
- Install a udev rule that grants a development group access to your Android
  device vendor ID.
- Add your user to that group, then re-login, replug the device, or reload udev
  rules so the new permissions apply.

A typical udev rule shape is:

```text
SUBSYSTEM=="usb", ATTR{idVendor}=="18d1", MODE="0660", GROUP="plugdev", TAG+="uaccess"
```

Google devices often use vendor ID `18d1`; other manufacturers use different
IDs. If discovery finds no candidates, check that USB debugging is enabled, the
USB mode exposes an ADB interface, the device is visible under `/dev/bus/usb`,
and your udev rule matches the actual vendor ID. If the handshake fails with
`adb.ErrAuthRequired`, the transport worked but the device requires ADB RSA
authentication and no trusted explicit key completed the challenge.

## Security and trust

adb-go behaves like adb: callers control commands and paths. Shell commands, file paths, and APK installation requests may affect the
connected device. ADB private keys are sensitive: a
trusted key can authorize host access to a device. The library does not add
command or path allowlists/denylists, does not log by default, and requires
`context.Context` for blocking public operations so callers can set deadlines or
cancel work.

## Testing and integration

Run unit and example tests with:

```sh
go test ./...
```

Optional TCP integration tests are skipped by default. To run them against an
emulator or TCP-enabled device that is already authorized or otherwise accepts
unauthenticated ADB TCP connections, set `ADB_GO_INTEGRATION_ADDR`:

```sh
ADB_GO_INTEGRATION_ADDR=127.0.0.1:5555 go test ./...
```

There is also an optional instrumented integration test that starts the
project's containerized Linux `adbd` and exercises connect, shell, shell
streaming, raw service opening, single-file push, and single-file pull. The test
prefers Podman and falls back to Docker; set `ADB_GO_CONTAINER_RUNTIME` to choose
explicitly.

```sh
ADB_GO_CONTAINER_INTEGRATION=1 ADB_GO_CONTAINER_BUILD=1 go test -timeout 30m ./...
ADB_GO_CONTAINER_INTEGRATION=1 go test ./...
```

The default container image name is `adb-go-linux-adbd`; override it with
`ADB_GO_CONTAINER_IMAGE` when needed.

Linux USB integration testing is opt-in and requires Linux plus a connected
ADB-capable USB device that your user can open through `/dev/bus/usb`:

```sh
ADB_GO_USB_INTEGRATION=1 go test ./...
```
