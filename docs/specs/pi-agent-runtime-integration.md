# Pi Agent Runtime 剩余 gate

- 文档状态：Approved
- 批准依据：[`docs/adr/0007-pi-agent-runtime-boundary.md`](../adr/0007-pi-agent-runtime-boundary.md)
- 已落地：main 的 Node.js 22 + Pi SDK ProductFlow adapter。当前实现见 `docs/ARCHITECTURE.md` 与 `agent-service/`。
- 实验实现：`exp` 保留 Go Agent service，不是 main 的隐式 fallback。

Skill / Context / Tool / Backend 的职责以 ADR 0007 为准。Session、Task、WorkflowRun 不得合并，见 `CONTEXT.md`。

## 相对当前产品多出来的东西

通过下列 gate 之前，不扩大默认能力，也不把 Pi session 文件当作 durable 证明：

- 真实 provider、PostgreSQL / Redis、浏览器。
- SSE 断线恢复的生产声明。
- 后台 durable Task、跨进程 claim、全量 effect reconciliation。
- 独立的 Fresh Observation harness（副作用前后端重读已是规则）。

证据与停止条件见 [`docs/rollout/pi-agent-durability.md`](../rollout/pi-agent-durability.md) 与 [`docs/ROADMAP.md`](../ROADMAP.md)。

## 明确排除

- 不把 Pi session file 当业务权威。
- 不把 Skill 当权限或事务保护。
- 不注册 Pi 默认 filesystem / process 工具。
- 不在 main 同时保留两套可切换 runtime。
