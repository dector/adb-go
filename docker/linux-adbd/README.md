# Linux adbd Docker image

This directory builds a small runtime image containing a standalone Linux `adbd` from <https://github.com/happyme531/standalone-linux-adbd>.

The image is intended for optional adb-go integration tests. It is not used by default `go test ./...`.

## Build

```sh
docker build \
  --build-arg ADBD_REF=v36.0.1-linux.2 \
  -t adb-go-linux-adbd \
  docker/linux-adbd
```

The Dockerfile uses a multi-stage build: source, toolchains, git, and build dependencies are kept in the build stage; the final image contains only the built `adbd`, a shell entrypoint, and small runtime packages.

## Run manually

```sh
docker run --rm -p 5555:5555 adb-go-linux-adbd
```

Then, from another terminal:

```sh
ADB_GO_INTEGRATION_ADDR=127.0.0.1:5555 go test ./integration
```

## Security

This daemon is unauthenticated. Bind it only to trusted local interfaces. Do not expose port 5555 to public or untrusted networks.
