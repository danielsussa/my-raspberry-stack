#!/usr/bin/env bash
set -euo pipefail

# Run from repo root regardless of where the script is invoked
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

# Ensure .env exists for docker compose
if [ ! -f .env ]; then
  if [ -f .env.example ]; then
    cp .env.example .env
  else
    echo "Missing .env and .env.example" >&2
    exit 1
  fi
fi

# Restart cleanly
docker compose down
exec docker compose up -d
