# 验收账本

本目录保存仍在推进的跨层验收账本。账本记录目标合同、当前代码与实测证据、未完成缺口和验证命令，不等同于当前产品能力声明。

- [`agent-production-readiness.md`](agent-production-readiness.md)：ProductFlow Agent 单商家生产就绪的唯一验收指标。
- [`agent-runtime-ownership.md`](agent-runtime-ownership.md)：Agent 运行时职责归属与分刀重构账本；它不改变或缩小生产就绪账本的可靠性范围。
- [`performance-governance.md`](performance-governance.md)：Graph、Agent、异步投递、连续生图、SSE、查询和容量 admission 的性能治理账本。

状态更新必须引用当前代码、测试或真实环境结果。方向完成后，把稳定事实写回 `CONTEXT.md`、`docs/ARCHITECTURE.md`、`docs/PRD.md` 或 `docs/USER_GUIDE.md`，并从 `docs/ROADMAP.md` 移除已完成方向；账本保留为验收证据。
