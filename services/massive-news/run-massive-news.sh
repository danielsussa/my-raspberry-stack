#!/bin/sh
set -eu

if [ -z "${MASSIVE_API_KEY:-}" ]; then
  echo "MASSIVE_API_KEY is required"
  exit 1
fi

node /app/massive-news.js
