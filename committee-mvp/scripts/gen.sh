#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
OUT_DIR="$ROOT_DIR/configs/nodes"
mkdir -p "$OUT_DIR"

NODE_COUNT=8
THRESHOLD=5
COORDINATOR_ID="node-1"

usage() {
  echo "Usage: $0 [-n node_count] [-t threshold] [-c coordinator_id]"
  echo "  -n  number of nodes (default: 8)"
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

if [[ "$COORDINATOR_ID" != node-* ]]; then
  echo "coordinator_id must look like node-k" >&2
  exit 1
fi

for i in $(seq 1 "$NODE_COUNT"); do
  node="node-$i"
  port=$((3400 + i))
  control_port=$((4400 + i))

  {
    echo "{"
    echo "  \"node_id\": \"$node\"," 
    echo "  \"listen_addr\": \"127.0.0.1:$port\"," 
    echo "  \"static_nodes\": ["
    for j in $(seq 1 "$NODE_COUNT"); do
      peer="node-$j"
      comma="," 
      if [[ "$j" -eq "$NODE_COUNT" ]]; then
        comma=""
      fi
      echo "    \"$peer\"$comma"
    done
    echo "  ],"

    echo "  \"static_node_addrs\": {"
    for j in $(seq 1 "$NODE_COUNT"); do
      peer="node-$j"
      peer_addr="127.0.0.1:$((3400 + j))"
      comma="," 
      if [[ "$j" -eq "$NODE_COUNT" ]]; then
        comma=""
      fi
      echo "    \"$peer\": \"$peer_addr\"$comma"
    done
    echo "  },"

    echo "  \"control_addr\": \"127.0.0.1:$control_port\"," 
    echo "  \"committee_size\": $NODE_COUNT,"
    echo "  \"threshold\": $THRESHOLD,"
    echo "  \"coordinator_id\": \"$COORDINATOR_ID\"," 
    echo "  \"domain_separation\": \"committee-sig/mvp/v1\"," 
    echo "  \"message_version\": \"v1\""
    echo "}"
  } > "$OUT_DIR/$node.json"
done

echo "generated configs in $OUT_DIR (nodes=$NODE_COUNT threshold=$THRESHOLD coordinator=$COORDINATOR_ID)"
