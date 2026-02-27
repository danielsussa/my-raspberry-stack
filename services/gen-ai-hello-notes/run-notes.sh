#!/bin/sh
set -eu

if [ -z "${CODEX_API_KEY:-}" ]; then
  if [ -n "${OPENAI_API_KEY:-}" ]; then
    export CODEX_API_KEY="$OPENAI_API_KEY"
  else
    echo "CODEX_API_KEY (or OPENAI_API_KEY) is required"
    exit 1
  fi
fi

PROMPT="${PROMPT:-Hey bot, tell me something new today.}"
PROMPT="$PROMPT (Today: $(date -I))"
OUT_FILE="${OUT_FILE:-/data/my-notes.md}"
WORKSPACE="${WORKSPACE:-/workspace}"
MODEL="${CODEX_MODEL:-}"

MODEL_ARGS=""
if [ -n "$MODEL" ]; then
  MODEL_ARGS="--model $MODEL"
fi

set +e
cd "$WORKSPACE"
note=$(codex exec --ephemeral $MODEL_ARGS "$PROMPT" 2>>/proc/1/fd/1)
status=$?
set -e

if [ $status -ne 0 ] || [ -z "$note" ]; then
  {
    echo "## $(date -Iseconds)"
    echo "ERROR: codex exec failed (status=$status)"
    echo
  } >> "$OUT_FILE"
  exit 1
fi

{
  echo "## $(date -Iseconds)"
  echo "$note"
  echo
} >> "$OUT_FILE"

echo "Wrote note at $(date -Iseconds)"
