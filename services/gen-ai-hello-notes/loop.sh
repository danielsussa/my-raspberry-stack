#!/bin/sh
set -eu

INTERVAL_SECONDS="${INTERVAL_SECONDS:-3600}"

while true; do
  /app/run-notes.sh || true
  sleep "$INTERVAL_SECONDS"
done
