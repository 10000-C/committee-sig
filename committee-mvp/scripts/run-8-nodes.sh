#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
BIN="go run ./cmd/committee-mvp"
LOG_DIR="$ROOT_DIR/.tmp/logs"
mkdir -p "$LOG_DIR"

"$ROOT_DIR/scripts/gen-node-configs.sh"

pids=()
session_id="session-$(date +%s)"
message="mvp-cross-process-sign"
for i in $(seq 1 8); do
  cfg="$ROOT_DIR/configs/nodes/node-$i.json"
  log_file="$LOG_DIR/node-$i.log"
  (
    cd "$ROOT_DIR"
    if [[ "$i" -eq 1 ]]; then
      $BIN -config "$cfg" -session "$session_id" -message "$message" >> "$log_file" 2>&1
    else
      $BIN -config "$cfg" >> "$log_file" 2>&1
    fi
  ) &
  pid="$!"
  pids+=("$pid")
  echo "started node-$i pid=$pid log=$log_file"
done

echo "coordinator auto request session=$session_id message=$message"

cleanup() {
  for pid in "${pids[@]}"; do
    kill "$pid" >/dev/null 2>&1 || true
  done
}

trap cleanup EXIT INT TERM
wait
