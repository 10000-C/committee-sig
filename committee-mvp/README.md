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

## Geth-style CLI

Single binary with subcommands, similar to geth style.

Start node (default command):

```bash
go run ./cmd/committee-mvp --config configs/devnet.json
```

or explicit:

```bash
go run ./cmd/committee-mvp node --config configs/devnet.json
```

Submit a signing round to coordinator:

```bash
go run ./cmd/committee-mvp admin submit-sign-request \
	--control-addr 127.0.0.1:4401 \
	--session session-manual-1 \
	--message hello-world
```

Add static peer in config files:

```bash
go run ./cmd/committee-mvp admin add-peer --from node-1 --peer node-9 --addr 127.0.0.1:3409
go run ./cmd/committee-mvp admin add-peer --from all --peer node-9 --addr 127.0.0.1:3409
```

If needed, set control address at startup:

```bash
go run ./cmd/committee-mvp -config configs/devnet.json -control-addr 127.0.0.1:4401
```

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
