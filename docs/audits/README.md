# 验收账本

本目录保存仍在推进的跨层验收账本。账本记录目标合同、当前代码与实测证据、未完成缺口和验证命令，不等同于当前产品能力声明。

- [`agent-production-readiness.md`](agent-production-readiness.md)：ProductFlow Agent 单商家生产就绪的唯一验收指标。
- [`agent-eval-system.md`](agent-eval-system.md)：Agent L0-L6 评测体系、任务合同、统计口径与生产样本回流的逐项验收账本；它提供 G-06 的行为质量证据，不替代生产就绪指标。
- [`agent-self-harness.md`](agent-self-harness.md)：ProductFlow 领域壳自进化（Self-Harness）的冻结决策、阶段门、两档生产出口与运维合同；它使用评测当 verifier，不替代评测分数或生产就绪指标。
- [`agent-runtime-ownership.md`](agent-runtime-ownership.md)：Agent 运行时职责归属与分刀重构账本；它不改变或缩小生产就绪账本的可靠性范围。
- [`performance-governance.md`](performance-governance.md)：Graph、Agent、异步投递、连续生图、SSE、查询和容量 admission 的性能治理账本。
- [`canvas-test-system.md`](canvas-test-system.md)：schema-v3 画布文稿权威测试体系（cook / 候选 / 有界搜索 / 运行中插入写 / mock 浏览器 / Agent 交错）；不替代 Agent 评测或生产就绪账本。
- [`image-quality-eval.md`](image-quality-eval.md)：内部淘宝完整套图对照、一句直调对照与视觉评委闸门。像素不进 git。

相关历史叙事（不是验收指标）：[`../history/agent-runtime-timeline.md`](../history/agent-runtime-timeline.md)。

状态更新必须引用当前代码、测试或真实环境结果。方向完成后，把稳定事实写回 `CONTEXT.md`、`docs/ARCHITECTURE.md`、`docs/PRD.md` 或 `docs/USER_GUIDE.md`，并从 `docs/ROADMAP.md` 移除已完成方向；账本保留为验收证据。
