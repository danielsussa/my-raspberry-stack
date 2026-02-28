#!/bin/sh
set -eu

INTERVAL_SECONDS="${INTRADAY_AI_PREDICT_INTERVAL_SECONDS:-3600}"
CLEAR_ON_START="${INTRADAY_AI_PREDICT_CLEAR_ON_START:-0}"
DATA_ROOT="${INTRADAY_AI_PREDICT_DATA_ROOT:-/data/intraday-ai-predict}"

if [ "$CLEAR_ON_START" = "1" ] || [ "$CLEAR_ON_START" = "true" ]; then
  if [ -d "$DATA_ROOT" ]; then
    find "$DATA_ROOT" -mindepth 1 -maxdepth 1 ! -name "AI_README.md" -exec rm -rf {} +
  fi
fi

while true; do
  /app/run-intraday.sh
  status=$?
  if [ "$status" -eq 99 ]; then
    echo "Loop finished by stop token."
    exit 0
  fi
  sleep "$INTERVAL_SECONDS"
done
