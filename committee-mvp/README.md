
# committee-mvp

委员会签名 MVP（支持动态节点数量）

## 核心接口

- `internal/p2p/network.go`：网络传输抽象（TCP 静态节点实现）
- `internal/committee/api.go`：委员会协议对外 API（`Start/Stop/SubmitSignRequest`）

## 跨进程运行（N 节点）

提供两个辅助脚本：

- `scripts/gen-node-configs.sh` — 生成每个节点的 JSON 配置，支持参数：
	- `-n` 节点数量（默认 8）
	- `-t` 门限（threshold，默认 5）
	- `-c` 协调者 id（默认 node-1）

示例：生成 5 个节点，门限为 3：

	```bash
	./scripts/gen-node-configs.sh -n 5 -t 3 -c node-1
	```

- `scripts/run-8-nodes.sh` — 启动器，接受 `-n/-t/-c` 参数以启动 N 个进程，日志写入 `.tmp/logs`：

```bash
	./scripts/run-8-nodes.sh -n 5 -t 3 -c node-1
```

运行流程概述：节点互相建立 P2P 连接后会启动 DKG（每个节点作为 dealer 广播 share），当所有节点收齐分片并完成 DKG 后会生成固定的委员会公钥（在委员会存续期内保持不变）。协调者在 DKG 完成后可发起签名请求；各节点返回门限签名份额，协调者使用拉格朗日系数在 x=0 处聚合份额，最广播聚合签名（`AGG_RESULT`）。

## Geth 风格 CLI（单一二进制）

默认启动节点命令示例：

```bash
	go run ./cmd/committee-mvp --config configs/nodes/node-1.json
```

或显式指定子命令：

```bash
	go run ./cmd/committee-mvp node --config configs/nodes/node-1.json
```

管理（admin）子命令（通过节点的 control TCP 端口调用）：

- `submit-sign-request`：让协调者发起一轮签名

```bash
	go run ./cmd/committee-mvp admin submit-sign-request \
		--control-addr 127.0.0.1:4401 \
		--session session-1 \
		--message "hello"
```

- `set-auto-sign`：开启/关闭本节点的自动签名（便于人工审批场景）

```bash
	go run ./cmd/committee-mvp admin set-auto-sign --control-addr 127.0.0.1:4402 --enabled=false
	```

- `sign-session`：对已排队的会话手动触发签名

```bash
	go run ./cmd/committee-mvp admin sign-session --control-addr 127.0.0.1:4402 --session session-1
	```

- `get-committee-pubkey`：导出固定委员会公钥（hex），可用于链上校验

```bash
	go run ./cmd/committee-mvp admin get-committee-pubkey --control-addr 127.0.0.1:4401
```

- `add-peer`：在配置文件中添加静态节点条目（修改 `configs/nodes/*.json`）

```bash
	go run ./cmd/committee-mvp admin add-peer --from node-1 --peer node-9 --addr 127.0.0.1:3409
	go run ./cmd/committee-mvp admin add-peer --from all --peer node-9 --addr 127.0.0.1:3409
```

如果需要在启动时覆盖 control 地址：

```bash
	go run ./cmd/committee-mvp -config configs/nodes/node-2.json -control-addr 127.0.0.1:4402
```

## 添加静态节点（等价于 addPeer.sh）

脚本会在所有或指定节点的配置文件中添加 `static_nodes` 和 `static_node_addrs` 条目。

## 运行示例

```bash
	go run ./cmd/committee-mvp -config configs/nodes/node-1.json
```

## 测试

	```bash
	go test ./...
	```
