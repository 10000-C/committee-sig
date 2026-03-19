## Plan: 委员会聚合签名与静态P2P项目搭建

目标是在当前仓库内新增一个独立 Go 子项目，先交付可运行的端到端 MVP：静态 8 节点网络 + 单 Dealer Shamir(5/8) 密钥分发 + BN254(BLS风格) 聚合签名与验证闭环。方案优先复用仓库已有 BN254 与 P2P 机制，避免自研底层曲线与网络栈。

**Steps**
1. 阶段一：项目骨架与依赖基线
1.1 在 cmd 下创建独立可执行入口与内部模块分层（crypto、share、p2p、committee、config、wire、testkit）。
1.2 依赖策略：优先复用现有 BN254 实现，若需完整 BLS API 则引入 gnark-crypto 的 bn254/bn254fr 子包并固定版本。
1.3 定义统一配置模型：委员会成员、静态节点列表、阈值 t=5、域分离标签、消息格式版本。
1.4 产出最小 main 启动路径：加载静态配置、初始化网络、启动 RPC/命令通道。

2. 阶段二：Shamir 密钥分发（Dealer 模式）
2.1 定义有限域：以 BN254 标量域 Fr 作为 Shamir 运算域，明确 mod 运算与序列化规范。
2.2 实现 Split(secret, t, n) 与 Recover(shares, t) 核心库，支持份额校验与重复索引防护。
2.3 设计分发协议消息：Dealer->Member 的 share 包、会话 ID、epoch、nonce、防重放字段。
2.4 节点侧实现份额接收与本地安全存储（MVP 可内存+可选本地加密文件）。
2.5 验证：随机 secret 在 5 份可恢复、4 份不可恢复，异常输入可检测。

3. 阶段三：BN254 聚合签名闭环（depends on 2）
3.1 明确签名体制：采用 BN254 上 BLS 风格签名流程（消息哈希到曲线、单签、聚合、聚合校验）。
3.2 定义委员会公钥模型：成员公钥列表、聚合公钥、签名者位图 bitmap。
3.3 实现签名与聚合接口：SignShare、AggregateSignatures、VerifyAggregate。
3.4 把阈值语义接入：仅当签名份额达到 5 个及以上才触发聚合验证。
3.5 兼容性验证：不同签名顺序聚合结果一致；重复签名者会被拒绝；错误 bitmap 必须失败。

4. 阶段四：静态 P2P 网络与协议（parallel with 2/3 in接口定义层）
4.1 网络模式固定为 StaticNodes + NoDiscovery，节点列表写死在配置文件。
4.2 实现最小协议：HELLO、DEAL_SHARE、SIGN_REQUEST、SIGN_RESPONSE、AGG_RESULT。
4.3 连接管理：启动时主动拨号全部静态节点，维持心跳与重连退避。
4.4 委员会流程编排：收到业务消息后广播签名请求，收集到 >=5 份签名后聚合并广播结果。
4.5 故障路径：离线节点、超时节点、重复消息、无效签名都要有状态机处理。

5. 阶段五：端到端演示与工程化收敛（depends on 2/3/4）
5.1 提供一键本地脚本：启动 8 节点、加载静态拓扑、触发一轮分发与签名。
5.2 输出可观测日志：会话 ID、份额分发完成率、签名收集进度、聚合验证结果。
5.3 补齐测试矩阵：单测（Shamir/签名）、集成测试（8 节点网络）、回归测试（异常输入）。
5.4 提供最小运维文档：配置项说明、启动方式、常见故障定位。

**Relevant files**
- /root/redactable-goethereum/go.mod — 依赖版本锁定与新增密码学库。
- /root/redactable-goethereum/crypto/bn256/cloudflare/bn256.go — BN254 底层点运算与 pairing 复用参考。
- /root/redactable-goethereum/crypto/bn256/google/bn256.go — 与 cloudflare 实现对照验证参考。
- /root/redactable-goethereum/p2p/server.go — 静态节点、连接维护、Server 配置复用模式。
- /root/redactable-goethereum/node/node.go — 服务注册与协议启动链路参考。
- /root/redactable-goethereum/cmd/geth/config.go — P2P 配置映射与静态节点注入参考。
- /root/redactable-goethereum/run.sh — 多节点本地启动脚本模式参考。
- /root/redactable-goethereum/init.sh — 多节点初始化流程参考。

**Verification**
1. 密码学单测：Shamir 在 Fr 域上做 1000 轮 property test（随机 secret、随机丢份额），验证 t=5 可恢复且 t-1 不可恢复。
2. 签名单测：单签验签、聚合验签、重复 signer 拒绝、错误消息拒绝、错误 bitmap 拒绝。
3. 网络集成：本地 8 节点静态拓扑，随机下线 0-3 节点时，系统仍可完成聚合签名；下线 4 节点时流程失败并返回阈值不足错误。
4. 互操作检查：同一消息在不同节点触发流程，最终聚合签名与签名者集合一致。
5. 性能基线：记录单轮签名聚合延迟、消息大小、节点 CPU 峰值，形成 MVP 基线报告。

**Decisions**
- 包含范围：独立子项目、静态节点列表、Dealer 模式 Shamir、n=8 t=5、BN254 聚合签名闭环、本地可运行演示。
- 明确不包含：生产级 DKG、动态成员变更、链上预编译改造、跨机房部署、HSM 集成。
- 安全假设：MVP 阶段 Dealer 可信；网络可被窃听时需依赖传输层加密与消息签名。

**Further Considerations**
1. 若后续要去中心化密钥生成，建议在阶段二之后新增 DKG 阶段，替换单 Dealer 分发路径。
2. 若要接入链上验证，可复用仓库现有 BLS 迁移文档思路，增加 aggregateSignature + signerBitmap 的交易字段。
3. 若希望更快出 Demo，可先做内存网络模拟，再切到真实 TCP 静态节点。