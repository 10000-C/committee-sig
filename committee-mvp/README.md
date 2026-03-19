# committee-mvp

MVP for static 8-node committee signing workflow.

## Core interfaces

- `internal/p2p/network.go`: transport interface used by committee service
- `internal/committee/api.go`: committee protocol API (`Start/Stop/SubmitSignRequest`)

## Cross-process run (8 nodes)

```bash
./scripts/run-8-nodes.sh
```

The coordinator (`node-1`) auto-sends one `SIGN_REQUEST`; peers return `SIGN_RESPONSE` with signer index + share signature; coordinator aggregates by bitmap and broadcasts `AGG_RESULT`.

## Add static peer (like addPeer.sh)

```bash
./scripts/addPeer.sh node-1 node-9 127.0.0.1:3409
./scripts/addPeer.sh all node-9 127.0.0.1:3409
```

This updates node config files by adding the new peer to both `static_nodes` and `static_node_addrs`.

## Run

```bash
go run ./cmd/committee-mvp -config configs/devnet.json
```

## Test

```bash
go test ./...
```
