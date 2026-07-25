# Linux USB transport design

This document designs the first USB transport for adb-go. It is intentionally
Linux-only for the first implementation slice, but the package boundaries should
not make Linux usbfs the only possible long-term solution. Future macOS,
Windows, or alternative Linux implementations should be able to plug into the
same transport seam without changing the ADB protocol layer.

## Goals

- Keep adb-go pure Go: no cgo, libusb, Android SDK, platform-tools, or official
  `adb` binary.
- Preserve the existing TCP code path and public TCP behavior.
- Add USB as another byte transport for the existing ADB protocol connection.
- Implement Linux first using the kernel usbfs device files under
  `/dev/bus/usb`.
- Make unsupported platforms fail clearly with `ErrUnsupported` instead of
  compile failures or silent no-ops.

## Non-goals for the first USB slice

- No ADB authentication implementation. If the device responds with `AUTH`, the
  high-level client should keep returning `ErrAuthRequired` just like TCP.
- No cross-platform USB implementation yet. Non-Linux builds should compile and
  return a clear unsupported error from USB entry points.
- No dependency on libusb or platform tools.
- No broad `adb devices` compatibility or automatic device management in the
  first version. Device selection can be explicit and minimal.

## Transport seam

The protocol layer already accepts an `io.ReadWriteCloser` through
`protocol.NewConnection`. USB should use the same shape as TCP:

```go
type Transport interface {
    io.ReadWriteCloser
}

type Dialer interface {
    Dial(ctx context.Context) (Transport, error)
}
```

The concrete package layout can stay internal at first, for example:

```text
client/
  transport.go          # shared handshake over any io.ReadWriteCloser
internal/transport/tcp/ # TCP dialer, no behavior change
internal/usb/           # platform-neutral USB selection types/errors
internal/usb/linux/     # linux usbfs implementation, //go:build linux
internal/usb/unsupported.go # non-Linux stubs, //go:build !linux
```

The important rule is that `client` should not know about usbfs ioctls,
endpoint descriptors, or Linux device-file details. It should only receive an
`io.ReadWriteCloser`, wrap it in `protocol.NewConnection`, and run the existing
`CNXN` handshake. This keeps the design open for a later macOS IOKit backend,
Windows WinUSB backend, or an alternative pure-Go Linux backend.

High-level flow:

```text
ConnectTCP/ConnectUSB
    -> transport dialer opens a raw byte stream
    -> protocol.NewConnection(rawStream)
    -> CNXN handshake
    -> Client{conn: protocol connection}
```

For unsupported platforms, the USB dialer should exist as a stub and return an
error wrapping `client.ErrUnsupported`, for example:

```text
adb connect usb: unsupported on darwin: adb operation unsupported
```

Callers can then use `errors.Is(err, adb.ErrUnsupported)`.

## Linux usbfs approach

Linux exposes USB devices as character device files under `/dev/bus/usb` when
usbfs/devtmpfs is mounted. A device usually appears as:

```text
/dev/bus/usb/<bus>/<device>
```

The Linux backend should:

1. Enumerate candidate paths under `/dev/bus/usb`.
2. Open each device file read/write only when needed.
3. Read raw USB descriptors from the device file.
4. Parse descriptors to find an ADB interface.
5. Claim that interface with usbfs ioctl calls.
6. Move bytes with bulk transfer ioctls.
7. Release the interface and close the file on `Close`.

ADB over USB is still the normal ADB packet protocol. USB only replaces the
underlying transport that carries ADB packets. After the USB bulk endpoints are
opened, the same `CNXN`, `OPEN`, `WRTE`, `OKAY`, and `CLSE` packets are used as
over TCP.

## Descriptor parsing needed for ADB

The discovery code must parse enough USB descriptor data to locate the ADB
interface and its endpoints. It does not need to implement a general-purpose USB
stack.

Required descriptor types:

- Device descriptor (`bDescriptorType = 0x01`) for vendor/product IDs and the
  active configuration reference.
- Configuration descriptor (`0x02`) for `wTotalLength` and interface groups.
- Interface descriptor (`0x04`) for class/subclass/protocol matching.
- Endpoint descriptor (`0x05`) for bulk IN and bulk OUT endpoint addresses.

ADB interface match:

```text
bInterfaceClass    = 0xff  vendor specific
bInterfaceSubClass = 0x42  ADB
bInterfaceProtocol = 0x01  ADB protocol
```

Endpoint requirements:

- One bulk IN endpoint: `bmAttributes & 0x03 == 0x02` and endpoint address has
  bit `0x80` set.
- One bulk OUT endpoint: `bmAttributes & 0x03 == 0x02` and endpoint address has
  bit `0x80` clear.

A discovered candidate should include at least:

```text
DevicePath          /dev/bus/usb/001/002
BusNumber           optional parsed bus number
DeviceNumber        optional parsed device number
VendorID/ProductID  from device descriptor
InterfaceNumber     bInterfaceNumber
BulkInEndpoint      bEndpointAddress, e.g. 0x81
BulkOutEndpoint     bEndpointAddress, e.g. 0x02
Serial              optional, later if string descriptors are read
```

Descriptor parsing should be fixture-testable without real hardware. The parser
should accept bytes and return structured candidates so tests can cover Android
and emulator-like descriptor examples.

## Required Linux usbfs ioctls

The implementation should use Linux usbfs ioctls from `linux/usbdevice_fs.h`.
The exact numeric constants should be defined in a Linux-only package and tested
where practical against the values from the kernel headers.

Expected minimum ioctl set:

- `USBDEVFS_CLAIMINTERFACE` — claim the selected ADB interface before bulk I/O.
- `USBDEVFS_RELEASEINTERFACE` — release the interface during close.
- `USBDEVFS_BULK` — perform blocking bulk IN and bulk OUT transfers.
- `USBDEVFS_RESET` — optional recovery helper, not required for the first
  working transport.
- `USBDEVFS_DISCONNECT_CLAIM` — optional later improvement for detaching a
  kernel driver and claiming an interface in one operation when appropriate.

The bulk ioctl uses a structure equivalent to:

```c
struct usbdevfs_bulktransfer {
    unsigned int ep;
    unsigned int len;
    unsigned int timeout;
    void *data;
};
```

Read maps to `USBDEVFS_BULK` with the bulk IN endpoint. Write maps to
`USBDEVFS_BULK` with the bulk OUT endpoint. The transport should handle partial
transfers according to normal `io.Reader`/`io.Writer` expectations.

Context cancellation for blocking USB operations should be implemented by
closing the device file. This mirrors existing stream cancellation behavior: the
close unblocks the pending ioctl, and the caller receives an error that can be
wrapped with the context error at the higher layer.

## Standard library syscall vs x/sys/unix

Decision for the first Linux implementation: prefer `golang.org/x/sys/unix`
when coding the ioctl layer, unless a prototype shows that the standard library
is sufficient without unsafe architecture traps.

Rationale:

- `x/sys/unix` is pure Go and does not violate the no-cgo/no-native-dependency
  requirement.
- It provides maintained syscall wrappers and Linux constants across
  architectures.
- The standard library `syscall` package is frozen and increasingly awkward for
  Linux-specific ioctl work.

The dependency should be isolated inside the Linux USB package. Public packages
and non-Linux builds should not depend on Linux-specific types.

If avoiding all non-standard-library dependencies becomes more important than
maintainability, only the Linux backend should need to change. The transport
seam and public API should remain unchanged.

## Permissions

Opening `/dev/bus/usb/*/*` generally requires sufficient permissions. Common
ways to provide access are:

- Run as root for testing.
- Install udev rules that grant a development group read/write access to Android
  devices.
- Add the user to the group selected by the udev rule, then re-login or reload
  permissions.

A typical udev rule shape is:

```text
SUBSYSTEM=="usb", ATTR{idVendor}=="18d1", MODE="0660", GROUP="plugdev", TAG+="uaccess"
```

The actual vendor ID depends on the device manufacturer. Google devices often
use `18d1`; other vendors use different IDs.

Permission errors should include the device path and remain compatible with
`errors.Is(err, fs.ErrPermission)` or the underlying `os` permission error where
possible. Example:

```text
adb usb open /dev/bus/usb/001/002: permission denied
```

## Platform behavior

Linux builds should compile the usbfs implementation behind `//go:build linux`.
Other platforms should compile stubs behind `//go:build !linux` that return an
unsupported error. The public behavior should be explicit:

- Linux: attempt discovery/open and return detailed discovery, permission,
  authentication, or transport errors.
- Non-Linux: immediately return `ErrUnsupported` from USB APIs.

This is intentionally conservative. It lets adb-go expose USB-shaped APIs early
without pretending to support platforms whose USB APIs need separate research.

## Testing plan

Default tests must not require USB hardware.

Unit tests:

- Descriptor parser fixture tests for ADB and non-ADB interfaces.
- Endpoint selection tests for missing IN/OUT endpoints.
- Option validation tests for explicit device selection and ambiguity.
- Error mapping tests for permission and unsupported-platform paths where
  possible.
- TCP regression tests to prove existing behavior is unchanged.

Optional integration tests:

```text
ADB_GO_USB_INTEGRATION=1 go test ./...
```

The optional test can require Linux and a connected, authorized or insecure ADB
USB device. It should skip by default and produce clear skip messages when no
suitable device or permissions are present.

## Review checklist before implementation

- TCP remains the default and unchanged.
- USB is an alternate transport into the same protocol connection.
- Linux-specific details stay behind build tags.
- Non-Linux USB calls return `ErrUnsupported` and still compile.
- Descriptor parsing is testable without hardware.
- The design allows a future backend that is not Linux usbfs.
