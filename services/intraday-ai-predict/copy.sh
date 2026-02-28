#!/bin/sh
set -eu

if [ "$#" -ne 2 ]; then
  echo "Usage: $0 '<start YYYY-MM-DD HH:MM:SS>' '<end YYYY-MM-DD HH:MM:SS>'" >&2
  exit 1
fi

START_RAW="$1"
END_RAW="$2"

start_epoch=$(date -d "$START_RAW" +%s 2>/dev/null || true)
end_epoch=$(date -d "$END_RAW" +%s 2>/dev/null || true)

if [ -z "$start_epoch" ] || [ -z "$end_epoch" ]; then
  echo "Invalid date format. Use: YYYY-MM-DD HH:MM:SS" >&2
  exit 1
fi

if [ "$start_epoch" -gt "$end_epoch" ]; then
  echo "Start date must be <= end date" >&2
  exit 1
fi

ROOT_DIR=$(cd "$(dirname "$0")/../.." && pwd)
SRC_MASSIVE="$ROOT_DIR/.data/massive-ticker-uploader"
SRC_CEDRO="$ROOT_DIR/.data/cedro-ticker-uploader"

START_SAFE=$(echo "$START_RAW" | tr ': ' '__')
END_SAFE=$(echo "$END_RAW" | tr ': ' '__')
SESSION_DIR="$ROOT_DIR/.data/intraday-ai-predict/.data/${START_SAFE}_${END_SAFE}"
DST_ROOT="$SESSION_DIR"
SESSION_ENV="$ROOT_DIR/.data/intraday-ai-predict/session.env"

copy_range() {
  src_root="$1"
  dst_root="$2"
  label="$3"

  if [ ! -d "$src_root" ]; then
    echo "Source not found: $src_root" >&2
    return 1
  fi

  mkdir -p "$dst_root"

  # Expected path: <src_root>/YYYY-MM-DD/<symbol>/HH_MM.csv
  find "$src_root" -type f | while IFS= read -r file; do
    rel="${file#$src_root/}"
    date_dir=$(echo "$rel" | cut -d/ -f1)
    time_file=$(basename "$file")

    case "$date_dir" in
      ????-??-??) : ;;
      *) continue ;;
    esac

    time_part=$(echo "$time_file" | sed -n 's/^\([0-9][0-9]\)\_\([0-9][0-9]\).*$/\1:\2/p')
    if [ -z "$time_part" ]; then
      continue
    fi

    ts=$(date -d "$date_dir $time_part:00" +%s 2>/dev/null || true)
    if [ -z "$ts" ]; then
      continue
    fi

    if [ "$ts" -lt "$start_epoch" ] || [ "$ts" -gt "$end_epoch" ]; then
      continue
    fi

    dest="$dst_root/$rel"
    mkdir -p "$(dirname "$dest")"
    cp -a "$file" "$dest"
  done

  echo "Copied $label into $dst_root"
}

copy_range "$SRC_MASSIVE" "$DST_ROOT/massive-ticker-uploader" "massive-ticker-uploader"
copy_range "$SRC_CEDRO" "$DST_ROOT/cedro-ticker-uploader" "cedro-ticker-uploader"

echo "Session data at $SESSION_DIR"

{
  echo "export INTRADAY_AI_PREDICT_SESSION_DIR=\"$SESSION_DIR\""
} > "$SESSION_ENV"

echo "Wrote $SESSION_ENV"
