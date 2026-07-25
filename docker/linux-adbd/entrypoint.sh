#!/bin/sh
set -eu

export ADBD_PORT="${ADBD_PORT:-5555}"
export ADBD_SHELL="${ADBD_SHELL:-/bin/sh}"

exec /usr/local/bin/adbd
