# syntax=docker/dockerfile:1

FROM debian:stable AS build

ARG ADBD_REPO=https://github.com/happyme531/standalone-linux-adbd.git
ARG ADBD_REF=v36.0.1-linux.2

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    git \
    build-essential \
    cmake \
    ninja-build \
    perl \
    python3 \
    golang \
    pkg-config \
    file \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /src
RUN git clone --filter=blob:none "${ADBD_REPO}" standalone-linux-adbd
WORKDIR /src/standalone-linux-adbd
RUN git fetch --depth 1 origin "${ADBD_REF}" \
    && git checkout --detach FETCH_HEAD
RUN git submodule update --init --recursive --depth 1 || true

RUN ANDROID_TOOLS_ADBD_USB=OFF scripts/build-linux-adbd.sh build/linux-adbd
RUN mkdir -p /out \
    && adbd_path="$(find build -type f -name adbd -perm /111 -print -quit)" \
    && test -n "${adbd_path}" \
    && file "${adbd_path}" \
    && cp "${adbd_path}" /out/adbd

FROM debian:stable-slim AS runtime

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    procps \
    && rm -rf /var/lib/apt/lists/*

COPY --from=build /out/adbd /usr/local/bin/adbd
COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /usr/local/bin/adbd /entrypoint.sh

EXPOSE 5555
ENTRYPOINT ["/entrypoint.sh"]
