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

STOP_TOKEN="${INTRADAY_AI_PREDICT_STOP_TOKEN:-END_SESSION}"
PROMPT_FILE="${INTRADAY_AI_PREDICT_PROMPT_FILE:-services/intraday-ai-predict/AI_README.md}"
PROMPT="${INTRADAY_AI_PREDICT_PROMPT:-}"
if [ -n "$PROMPT_FILE" ] && [ -f "$PROMPT_FILE" ]; then
  PROMPT="$(cat "$PROMPT_FILE")"
fi
if [ -z "$PROMPT" ]; then
  PROMPT="Execute os comandos: mkdir hello-world; ls."
fi
if ! echo "$PROMPT" | grep -Fq "$STOP_TOKEN"; then
  PROMPT="$PROMPT\n\nDepois finalize respondendo com ${STOP_TOKEN}."
fi

WORKSPACE="${INTRADAY_AI_PREDICT_WORKSPACE:-.data/intraday-ai-predict/workspace}"
OUT_FILE="${INTRADAY_AI_PREDICT_OUT_FILE:-.data/intraday-ai-predict/predictions.md}"
MODEL="${INTRADAY_AI_PREDICT_MODEL:-}"
MODE="${INTRADAY_AI_PREDICT_MODE:-full-auto}"
CODEX_ARGS="${INTRADAY_AI_PREDICT_CODEX_ARGS:-}"

MODEL_ARGS=""
if [ -n "$MODEL" ]; then
  MODEL_ARGS="--model $MODEL"
fi

MODE_ARGS=""
case "$MODE" in
  suggest|auto-edit|full-auto)
    MODE_ARGS="--$MODE"
    ;;
  *)
    MODE_ARGS=""
    ;;
esac

mkdir -p "$WORKSPACE"
cd "$WORKSPACE"

set +e
output=$(codex exec --ephemeral --skip-git-repo-check $MODE_ARGS $MODEL_ARGS $CODEX_ARGS "$PROMPT" 2>>/proc/1/fd/1)
status=$?
set -e

mkdir -p "$(dirname "$OUT_FILE")"
if [ $status -ne 0 ] || [ -z "$output" ]; then
  {
    echo "## $(date -Iseconds)"
    echo "ERROR: codex exec failed (status=$status)"
    echo
  } >> "$OUT_FILE"
  exit 1
fi

{
  echo "## $(date -Iseconds)"
  echo "$output"
  echo
} >> "$OUT_FILE"

echo "Wrote prediction at $(date -Iseconds)"
