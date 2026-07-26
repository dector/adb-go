# Integration testing

adb-go's regular test suite is self-contained:

```sh
go test ./...
```

Tests that talk to a real ADB daemon, emulator, device, USB transport, or
container runtime are opt-in. They are skipped unless you set the environment
variables described below.

## TCP emulator or device tests

Set `ADB_GO_INTEGRATION_ADDR` to run the root TCP integration test against an
already-running ADB TCP endpoint:

```sh
ADB_GO_INTEGRATION_ADDR=127.0.0.1:5555 go test ./...
```

The value is a host or `host:port` accepted by `adb.Connect`. When the port is
omitted, adb-go uses the standard ADB TCP port `5555`:

```sh
ADB_GO_INTEGRATION_ADDR=127.0.0.1 go test ./...
```

The current TCP integration test connects, opens a `shell:` service, runs a small
`echo` command, and verifies the output.

### Emulator TCP example

Android emulators commonly expose ADB over localhost on odd ports such as
`5555`, `5557`, or `5559`. If you already have an emulator running, you can ask
adb-go to probe the usual localhost range:

```sh
adb-go targets --scan
```

Then run tests against one reported selector:

```sh
ADB_GO_INTEGRATION_ADDR=127.0.0.1:5555 go test ./...
```

If you use the official Android tooling to manage the emulator, make sure the
emulator is fully booted and authorized before running the test. adb-go does not
start emulators and does not talk to the official host adb server for discovery.

### Real-device TCP prerequisites

Real Android devices usually do not listen on ADB TCP by default. Before using
`ADB_GO_INTEGRATION_ADDR`, ensure that:

- USB debugging is enabled on the device.
- The device is connected to a trusted network with the test host.
- ADB-over-TCP has been enabled for that device by your normal development
  workflow.
- The device accepts the connection without an interactive authorization prompt,
  or you run code/tests that explicitly pass a trusted key where supported.

Do not expose unauthenticated ADB TCP to public or untrusted networks. Treat an
ADB TCP port as device-control access.

## Containerized Linux adbd workflow

The repository includes an optional Linux `adbd` container image under
[`../docker/linux-adbd`](../docker/linux-adbd). This workflow is useful when you
want a repeatable daemon for protocol-level testing without relying on a physical
Android device.

Build the image once with Podman:

```sh
podman build \
  -f docker/linux-adbd/Containerfile \
  -t adb-go-linux-adbd \
  docker/linux-adbd
```

Run it manually and point the TCP integration test at the published port:

```sh
podman run --rm -p 5555:5555 adb-go-linux-adbd
ADB_GO_INTEGRATION_ADDR=127.0.0.1:5555 go test ./...
```

The root test suite can also start the container automatically. This
instrumented test publishes `adbd` on a random localhost port, waits for
`adb.Connect` to succeed, and then exercises connect, shell, shell streaming,
raw service opening, single-file push, and single-file pull:

```sh
ADB_GO_CONTAINER_INTEGRATION=1 ADB_GO_CONTAINER_BUILD=1 go test -timeout 30m ./...
```

Useful container variables:

- `ADB_GO_CONTAINER_INTEGRATION=1` enables the containerized integration test.
- `ADB_GO_CONTAINER_BUILD=1` builds the image before running the test.
- `ADB_GO_CONTAINER_IMAGE=NAME` selects a custom image tag; the default is
  `adb-go-linux-adbd`.
- `ADB_GO_CONTAINER_RUNTIME=podman` or `docker` chooses the runtime explicitly.
  If unset, tests prefer Podman and fall back to Docker.

See [`../docker/linux-adbd/README.md`](../docker/linux-adbd/README.md) for the
container image details and security notes.

## Linux USB integration tests

Linux USB tests are also opt-in:

```sh
ADB_GO_USB_INTEGRATION=1 go test ./...
```

They require Linux, an ADB-capable USB device, USB debugging enabled, and local
permissions to open the relevant `/dev/bus/usb/BBB/DDD` device node. If the USB
transport reaches the device but the ADB handshake requires authentication, pass
an explicit trusted key in the workflow being tested where the API or CLI
supports it.

## Troubleshooting

### Tests are skipped

A skip usually means the relevant opt-in environment variable was not set. This
is expected for normal `go test ./...` runs. Set one of these when you want the
corresponding integration path:

- `ADB_GO_INTEGRATION_ADDR` for an existing TCP daemon, emulator, or device.
- `ADB_GO_CONTAINER_INTEGRATION=1` for the containerized Linux `adbd` workflow.
- `ADB_GO_USB_INTEGRATION=1` for Linux USB transport tests.

### TCP connection is refused

`connection refused` means the host was reachable but nothing accepted the TCP
connection on that port. Check that the emulator/device/container is running,
that the selected port is correct, and that the daemon is bound to an interface
reachable from the test host. For local emulators, `adb-go targets --scan` can
help find the usual localhost ADB ports.

### TCP connection times out

A timeout usually points to routing, firewall, sleep/offline device state, or a
host/port typo. Try a shorter, direct test first, for example running
`adb-go shell --addr HOST:PORT echo ok` with the same address.

### Device requires authentication

`device requires authentication` means the ADB transport opened successfully, but
the device sent an `AUTH` challenge and adb-go did not complete it with a trusted
key. For CLI workflows, pass `--auth-key ~/.android/adbkey` if that key is
already trusted by the device. For library workflows, load the key with
`adb.LoadPrivateKey` and pass it through the connection options.

### Container runtime or image is missing

For containerized tests, install Podman or Docker and either set
`ADB_GO_CONTAINER_BUILD=1` so the test can build the image, or build/tag the
image yourself as `adb-go-linux-adbd` or the value of `ADB_GO_CONTAINER_IMAGE`.

### Linux USB permission errors

If discovery sees a USB device but opening it fails, check `/dev/bus/usb`
permissions. Common fixes are running a one-off root test, adding a udev rule for
the device vendor ID, replugging the device, and re-login so group or `uaccess`
changes apply.
