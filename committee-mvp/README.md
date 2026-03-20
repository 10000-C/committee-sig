# committee-mvp

委员会门限签名 MVP（交互式 CLI 用法）

本文档以交互式 CLI 为主流程：
- 节点先启动并保持运行
- 运维/测试人员通过 `admin console` 在运行中交互发起签名、切换节点、查询公钥

## 1) 启动委员会节点

先生成配置，再启动 N 个节点：

```bash
cd /root/committee-sig/committee-mvp
./scripts/gen.sh -n 5 -t 3 -c node-1
./scripts/run.sh -n 5 -t 3 -c node-1
```

说明：
- `-n`：节点数量
- `-t`：门限
- `-c`：协调者节点 ID
- 节点日志输出在 `.tmp/logs` 目录

## 2) 打开交互式 CLI（推荐）

连接协调者控制端口：

```bash
cd /root/committee-sig/committee-mvp
go run ./cmd/committee-mvp admin console --control-addr 127.0.0.1:4401
```

进入后使用以下交互命令：
- `submit`：交互输入 `session/message` 发起新签名请求
- `pubkey`：查询固定委员会公钥（hex，可用于链上验签）
- `result [session]`：查看聚合签名明文（hex）；不带 session 时返回最新一条
- `target <host:port>`：切换控制目标节点
- `autosign on|off`：开启/关闭目标节点自动签名
- `sign`：手动触发目标节点签名某个会话
- `help`：查看命令帮助
- `exit`：退出控制台

## 3) 典型交互流程

1. 在协调者控制台执行 `submit`，输入新的 `message`
2. 可随时执行 `pubkey` 获取委员会固定公钥
3. 如需人工签名流程：
   - 先 `target 127.0.0.1:4402`
   - 再 `autosign off`
   - 收到请求后执行 `sign`

## 4) 单节点手动启动（可选）

如果你不使用批量脚本，也可以单独启动某个节点：

```bash
cd /root/committee-sig/committee-mvp
go run ./cmd/committee-mvp node --config configs/nodes/node-1.json
```

## 5) 测试

```bash
cd /root/committee-sig/committee-mvp
go test ./...
```
