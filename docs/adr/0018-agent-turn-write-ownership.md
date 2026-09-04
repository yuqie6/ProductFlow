# ADR 0018：Agent Turn 写入所有权

## 状态

Accepted。后继并收紧 ADR 0007 的运行时职责；ADR 0017 的 PostgreSQL journal 与浏览器协议继续有效。

## 背景

当前主线由 Go 业务后端和 Node.js/Pi agent-service 共同完成一个 Agent Turn。PostgreSQL `agent_turn_events` 已是浏览器与历史回放的 journal 权威，Go 也拥有 execution lease、fencing、业务 mutation ledger 和 Graph Command。agent-service 仍同时参与本地 WAL、批量发布、启动恢复、Turn 快照与工具效果解释，造成同一状态可能存在两个作者：

- `main.ts` 将 Store publisher 注入 Pi runtime，再由 runtime 发布 durable event，写路径所有权形成回环；
- Go lease recovery 与 Node startup recovery 都可能尝试收敛丢失执行；
- Go worker 可读取 Node Turn 文件快照更新活投影；
- TypeScript 与 Go 都解释工具 `recovery_policy`。

这些路径即使产生相同结果，也会让 lease 竞争、ACK 失败、进程崩溃和副作用对账缺少唯一裁决者。

## 决策

### Go 拥有 AgentTurn 写权威

Go 是以下状态的唯一业务作者：

- `AgentTurnProjection` 状态、终态和 `terminal_reason_code`；
- PostgreSQL `agent_turn_events` 的连续序列、持久化 ACK 与浏览器投影；
- execution lease、attempt、owner、phase 和 fencing；
- `agent_tool_mutations` 中的副作用证明及恢复策略解释；
- Question answer、approval、WorkflowRun request、global Draft 和 Graph Command 的业务事务。

丢失的 in-flight execution 只由 Go lease 过期扫描写终态。Go 从 PostgreSQL journal fold Turn 摘要，不把 Node 文件快照当作活状态作者。

### Node 只运行 Pi loop 和适配边界

agent-service 拥有：

- Pi session 的创建、恢复、drain 和上下文压缩；
- Skill catalog 与受控 ProductFlow Tool 注册；
- Pi runtime event 到有界 journal chunk/checkpoint 的翻译与合帧；
- 对 Go Tool HTTP API 的调用；
- 作为 PostgreSQL handoff 前置缓冲的本地有界 WAL。

本地 WAL 和 Pi session 不是业务事实源。Node 只能按当前 lease/fencing 把连续事件批量交给 Go，并在精确 receipt 后推进 ACK；失败时原序重试或诚实停止，不能跳过 sequence。Node 重启只 drain/confirm 已能证明的前缀，不替 Go 判断丢失执行终态。

### 保持既有产品和可靠性合同

- 浏览器继续只连接 Go，并只读取已经提交到 PostgreSQL 的 journal；断开浏览器不取消 Turn。
- 保留 20ms、64 events、768KiB 的批量上限及结构性 flush barrier，不退回每 token 单独等待 PostgreSQL。
- `ask_user` 的答案写入原 Turn，并恢复同一 Pi session；不创建 continuation Turn。
- 工具 intent schema 与 manifest `recovery_policy` 保持 wire 兼容，但恢复策略只由 Go mutation ledger/reconciler 解释。
- `unknown`、`terminal_reason_code`、approval、Goal 和画布编辑语义不变。

迁移按 [`../history/agent-runtime-timeline.md#runtime-ownership-evidence`](../history/agent-runtime-timeline.md#runtime-ownership-evidence) 的 S1-S6 和 checkpoint 协议执行。该账本不替代生产就绪账本。

## 后果

- 每个持久状态和副作用证明只有一个裁决者，lease/fencing 能覆盖所有业务写路径。
- agent-service 可以围绕 Pi SDK 生命周期拆分，不需要复制 ProductFlow 的恢复状态机。
- Go 承担 journal 接收、投影、终态和 effect reconciliation 的完整一致性责任。
- 本地 WAL 仍需要容量、ACK、尾部损坏和关停 flush 测试；职责变窄不代表可以删除耐久性门。

## 排除方案

- 浏览器直连 agent-service 或读取本地 event file。
- 把 Node Turn snapshot 作为 PostgreSQL 活投影的回退作者。
- Go 与 TypeScript 分别维护 `recovery_policy` 状态机。
- 用 Pi session persistence 证明业务副作用或跨进程模型请求可续跑。
- 在 agent-service 中实现商品、图片、确认事务或 Graph Command。

## 参考

- [`0007-pi-agent-runtime-boundary.md`](0007-pi-agent-runtime-boundary.md)
- [`0017-agent-full-journal-ui-protocol.md`](0017-agent-full-journal-ui-protocol.md)
- [`../history/agent-runtime-timeline.md#runtime-ownership-evidence`](../history/agent-runtime-timeline.md#runtime-ownership-evidence)
- [`../audits/performance-governance.md#production-gates`](../audits/performance-governance.md#production-gates)
