#!/bin/sh
set -eu

if [ -z "${OPENAI_API_KEY:-}" ]; then
  echo "OPENAI_API_KEY is required"
  exit 1
fi

MODEL="${OPENAI_MODEL:-gpt-4.1-mini}"
PROMPT="${PROMPT:-Hey bot, tell me something new today.}"
PROMPT="$PROMPT (Today: $(date -I))"
USE_WEB_SEARCH="${USE_WEB_SEARCH:-false}"

payload=$(jq -n --arg model "$MODEL" --arg input "$PROMPT" '{model: $model, input: $input, store: false}')
if [ "$USE_WEB_SEARCH" = "true" ]; then
  payload=$(echo "$payload" | jq '.tools=[{"type":"web_search"}]')
fi

response=$(curl -sS https://api.openai.com/v1/responses \
  -H "Authorization: Bearer $OPENAI_API_KEY" \
  -H "Content-Type: application/json" \
  -d "$payload")

text=$(echo "$response" | jq -r '[.output[]? | select(.type=="message") | .content[]? | select(.type=="output_text") | .text] | join("\n")')

if [ -z "$text" ] || [ "$text" = "null" ]; then
  {
    echo "## $(date -Iseconds)"
    echo "ERROR: empty response"
    echo "$response" | jq -c '.'
    echo
  } >> /app/my-notes.md
  exit 1
fi

{
  echo "## $(date -Iseconds)"
  echo "$text"
  echo
} >> /app/my-notes.md

echo "Wrote note at $(date -Iseconds)"
