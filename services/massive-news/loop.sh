#!/bin/sh
set -eu

INTERVAL_SECONDS="${MASSIVE_NEWS_INTERVAL_SECONDS:-3600}"

while true; do
  /app/run-massive-news.sh || true
  sleep "$INTERVAL_SECONDS"
done
