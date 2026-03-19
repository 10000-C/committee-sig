#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
BIN="go run ./cmd/committee-mvp"
LOG_DIR="$ROOT_DIR/.tmp/logs"
mkdir -p "$LOG_DIR"

"$ROOT_DIR/scripts/gen-node-configs.sh"

pids=()
for i in $(seq 1 8); do
  cfg="$ROOT_DIR/configs/nodes/node-$i.json"
  log_file="$LOG_DIR/node-$i.log"
  (
    cd "$ROOT_DIR"
    $BIN -config "$cfg" >> "$log_file" 2>&1
  ) &
  pid="$!"
  pids+=("$pid")
  echo "started node-$i pid=$pid log=$log_file"
done

cleanup() {
  for pid in "${pids[@]}"; do
    kill "$pid" >/dev/null 2>&1 || true
  done
}

trap cleanup EXIT INT TERM
wait
