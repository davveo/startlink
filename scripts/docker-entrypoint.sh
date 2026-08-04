#!/bin/sh
set -e

APP="${APP:-api}"
CONFIG="${CONFIG:-/app/configs/config.yaml}"

case "$APP" in
  api|scheduler|pusher)
    exec "/app/${APP}" -config "${CONFIG}"
    ;;
  *)
    echo "unknown APP=${APP}, expect api|scheduler|pusher" >&2
    exit 1
    ;;
esac
