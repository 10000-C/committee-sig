#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 3 || $# -gt 4 ]]; then
  echo "Usage: $0 <from-node|all> <peer-node> <peer-addr> [config-dir]"
  echo "Example: $0 node-1 node-9 127.0.0.1:3409"
  echo "Example: $0 all node-9 127.0.0.1:3409 configs/nodes"
  exit 1
fi

FROM_NODE="$1"
PEER_NODE="$2"
PEER_ADDR="$3"
ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
CONFIG_DIR="${4:-$ROOT_DIR/configs/nodes}"

if ! command -v jq >/dev/null 2>&1; then
  echo "jq is required. install with: apt install -y jq"
  exit 1
fi

if [[ ! -d "$CONFIG_DIR" ]]; then
  echo "config dir not found: $CONFIG_DIR"
  exit 1
fi

update_file() {
  local file="$1"
  local tmp
  tmp="$(mktemp)"
  jq --arg peer "$PEER_NODE" --arg addr "$PEER_ADDR" '
    .static_nodes = ((.static_nodes // []) + [$peer] | unique) |
    .static_node_addrs = (.static_node_addrs // {}) |
    .static_node_addrs[$peer] = $addr
  ' "$file" > "$tmp"
  mv "$tmp" "$file"
  echo "updated: $file"
}

if [[ "$FROM_NODE" == "all" ]]; then
  while IFS= read -r file; do
    update_file "$file"
  done < <(find "$CONFIG_DIR" -maxdepth 1 -type f -name 'node-*.json' | sort)
  exit 0
fi

TARGET_FILE="$CONFIG_DIR/$FROM_NODE.json"
if [[ ! -f "$TARGET_FILE" ]]; then
  echo "node config not found: $TARGET_FILE"
  exit 1
fi

update_file "$TARGET_FILE"
