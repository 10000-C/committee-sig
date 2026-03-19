#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
OUT_DIR="$ROOT_DIR/configs/nodes"
mkdir -p "$OUT_DIR"

for i in $(seq 1 8); do
  node="node-$i"
  port=$((3400 + i))
  {
    echo "{"
    echo "  \"node_id\": \"$node\"," 
    echo "  \"listen_addr\": \"127.0.0.1:$port\"," 
    echo "  \"static_nodes\": ["
    for j in $(seq 1 8); do
      peer="node-$j"
      comma="," 
      if [[ "$j" -eq 8 ]]; then
        comma=""
      fi
      echo "    \"$peer\"$comma"
    done
    echo "  ],"
    echo "  \"static_node_addrs\": {"
    for j in $(seq 1 8); do
      peer="node-$j"
      peer_addr="127.0.0.1:$((3400 + j))"
      comma="," 
      if [[ "$j" -eq 8 ]]; then
        comma=""
      fi
      echo "    \"$peer\": \"$peer_addr\"$comma"
    done
    echo "  },"
    cat <<EOF
  "committee_size": 8,
  "threshold": 5,
  "coordinator_id": "node-1",
  "domain_separation": "committee-sig/mvp/v1",
  "message_version": "v1"
}
EOF
  } > "$OUT_DIR/$node.json"
done

echo "generated configs in $OUT_DIR"
