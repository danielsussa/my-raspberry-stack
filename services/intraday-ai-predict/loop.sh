#!/bin/sh
set -eu

INTERVAL_SECONDS="${INTRADAY_AI_PREDICT_INTERVAL_SECONDS:-3600}"
CLEAR_ON_START="${INTRADAY_AI_PREDICT_CLEAR_ON_START:-0}"
DATA_ROOT="${INTRADAY_AI_PREDICT_DATA_ROOT:-/data/intraday-ai-predict}"
SESSION_DIR="${INTRADAY_AI_PREDICT_SESSION_DIR:-}"
WORKSPACE_DIR="${INTRADAY_AI_PREDICT_WORKSPACE:-/data/intraday-ai-predict/workspace}"

if [ "$CLEAR_ON_START" = "1" ] || [ "$CLEAR_ON_START" = "true" ]; then
  if [ -d "$DATA_ROOT" ]; then
    find "$DATA_ROOT" -mindepth 1 -maxdepth 1 \
      ! -name "AI_README.md" \
      ! -name ".data" \
      -exec rm -rf {} +
  fi
  if [ -d "$WORKSPACE_DIR" ]; then
    rm -rf "$WORKSPACE_DIR"/*
  fi
fi

echo "SESSION_DIR=$SESSION_DIR"
echo "WORKSPACE_DIR=$WORKSPACE_DIR"

if [ -z "$SESSION_DIR" ] || [ ! -d "$SESSION_DIR" ]; then
  echo "No session copied. INTRADAY_AI_PREDICT_SESSION_DIR is empty or not found."
  exit 1
fi

mkdir -p "$WORKSPACE_DIR/.data"
cp -a "$SESSION_DIR"/. "$WORKSPACE_DIR/.data/"
echo "Copied session from $SESSION_DIR into $WORKSPACE_DIR/.data"
ls -la "$WORKSPACE_DIR"

while true; do
  /app/run-intraday.sh
  status=$?
  if [ "$status" -eq 99 ]; then
    echo "Loop finished by stop token."
    exit 0
  fi
  sleep "$INTERVAL_SECONDS"
done
