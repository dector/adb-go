# Linux adbd container image

This directory builds a small runtime image containing a standalone Linux `adbd` from <https://github.com/happyme531/standalone-linux-adbd>.

The image is intended for optional adb-go integration tests. It is not used by default `go test ./...`.

## Contents

- [Build](#build)
- [Run manually](#run-manually)
- [Run instrumented tests automatically](#run-instrumented-tests-automatically)
- [Security](#security)

## Build

Podman is the preferred local runtime:

```sh
podman build \
  -f docker/linux-adbd/Containerfile \
  --build-arg ADBD_REF=v36.0.1-linux.2 \
  -t adb-go-linux-adbd \
  docker/linux-adbd
```

Docker also works if Podman is unavailable:

```sh
docker build \
  -f docker/linux-adbd/Containerfile \
  --build-arg ADBD_REF=v36.0.1-linux.2 \
  -t adb-go-linux-adbd \
  docker/linux-adbd
```

The `Containerfile` uses a multi-stage build: source, toolchains, git, and build dependencies are kept in the build stage; the final image contains only the built `adbd`, a shell entrypoint, and small runtime packages.

## Run manually

```sh
podman run --rm -p 5555:5555 adb-go-linux-adbd
```

Then, from another terminal:

```sh
ADB_GO_INTEGRATION_ADDR=127.0.0.1:5555 go test ./...
```

## Run instrumented tests automatically

The root test suite can also start this image automatically as an optional
instrumented integration test. It publishes the server on a random localhost
port, waits for `adb.Connect` to succeed, and then verifies shell, shell
streaming, raw service opening, push, and pull against the real server. The test
prefers Podman and falls back to Docker; set `ADB_GO_CONTAINER_RUNTIME` to choose
explicitly.

```sh
ADB_GO_CONTAINER_INTEGRATION=1 ADB_GO_CONTAINER_BUILD=1 go test -timeout 30m ./...
```

Set `ADB_GO_CONTAINER_IMAGE` to use a custom image tag. Omit
`ADB_GO_CONTAINER_BUILD=1` to reuse an image that already exists locally.

## Security

This server is unauthenticated. Bind it only to trusted local interfaces. Do not expose port 5555 to public or untrusted networks.
