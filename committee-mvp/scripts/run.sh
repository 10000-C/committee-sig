#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
BIN="go run ./cmd/committee-mvp"
LOG_DIR="$ROOT_DIR/.tmp/logs"
mkdir -p "$LOG_DIR"

NODE_COUNT=8
THRESHOLD=5
COORDINATOR_ID="node-1"

usage() {
  echo "Usage: $0 [-n node_count] [-t threshold] [-c coordinator_id]"
  echo "  -n  number of nodes to launch (default: 8)"
  echo "  -t  threshold (default: 5)"
  echo "  -c  coordinator node id (default: node-1)"
}

while getopts ":n:t:c:h" opt; do
  case "$opt" in
    n)
      NODE_COUNT="$OPTARG"
      ;;
    t)
      THRESHOLD="$OPTARG"
      ;;
    c)
      COORDINATOR_ID="$OPTARG"
      ;;
    h)
      usage
      exit 0
      ;;
    \?)
      echo "invalid option: -$OPTARG" >&2
      usage
      exit 1
      ;;
  esac
done

if ! [[ "$NODE_COUNT" =~ ^[0-9]+$ ]] || [[ "$NODE_COUNT" -lt 1 ]]; then
  echo "node_count must be a positive integer" >&2
  exit 1
fi
if ! [[ "$THRESHOLD" =~ ^[0-9]+$ ]] || [[ "$THRESHOLD" -lt 1 ]] || [[ "$THRESHOLD" -gt "$NODE_COUNT" ]]; then
  echo "threshold must be a positive integer and <= node_count" >&2
  exit 1
fi

"$ROOT_DIR/scripts/gen.sh" -n "$NODE_COUNT" -t "$THRESHOLD" -c "$COORDINATOR_ID"

pids=()
session_id="session-$(date +%s)"
message="mvp-cross-process-sign"
for i in $(seq 1 "$NODE_COUNT"); do
  cfg="$ROOT_DIR/configs/nodes/node-$i.json"
  log_file="$LOG_DIR/node-$i.log"
  (
    cd "$ROOT_DIR"
    if [[ "node-$i" == "$COORDINATOR_ID" ]]; then
      $BIN -config "$cfg" -session "$session_id" -message "$message" >> "$log_file" 2>&1
    else
      $BIN -config "$cfg" >> "$log_file" 2>&1
    fi
  ) &
  pid="$!"
  pids+=("$pid")
  echo "started node-$i pid=$pid log=$log_file"
done

echo "coordinator=$COORDINATOR_ID auto request session=$session_id message=$message"

cleanup() {
  for pid in "${pids[@]}"; do
    kill "$pid" >/dev/null 2>&1 || true
  done
}

trap cleanup EXIT INT TERM
wait
