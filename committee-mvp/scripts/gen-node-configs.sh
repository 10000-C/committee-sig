#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
OUT_DIR="$ROOT_DIR/configs/nodes"
mkdir -p "$OUT_DIR"

static_nodes_json=""
for i in $(seq 1 8); do
  node="node-$i"
  if [[ -n "$static_nodes_json" ]]; then
    static_nodes_json+="\n    ,\"$node\""
  else
    static_nodes_json+="\n    \"$node\""
  fi
done

for i in $(seq 1 8); do
  node="node-$i"
  port=$((3400 + i))
  cat > "$OUT_DIR/$node.json" <<EOF
{
  "node_id": "$node",
  "listen_addr": "127.0.0.1:$port",
  "static_nodes": [$static_nodes_json
  ],
  "committee_size": 8,
  "threshold": 5,
  "coordinator_id": "node-1",
  "domain_separation": "committee-sig/mvp/v1",
  "message_version": "v1"
}
EOF
done

echo "generated configs in $OUT_DIR"
