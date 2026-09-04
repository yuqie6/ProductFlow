# 验收

每个总账本是一个**业务组**：管这个大方向的合同与历史证据，并按未完成阶段**发布** `tasks/` 里的 issue。Agent 是组员，不从章程开工。

并行认领看 [`tasks/README.md`](tasks/README.md)。**先认领再改代码。一人一份。**

## 业务组

| 组 | 章程 | 组状态 | 管什么 |
|---|---|---|---|
| 评测 | [`agent-eval-system.md`](agent-eval-system.md) | 开工 | L0–L6 任务集、grader、pass^k、分数是否采信 |
| 壳进化 | [`agent-self-harness.md`](agent-self-harness.md) | 开工 | 领域壳版本化与进化控制器；P1 之后才拆 P2 |
| 画布 | [`canvas-test-system.md`](canvas-test-system.md) | 开工 | schema-v3 文稿权威；生成不得盖用户已发布稿 |
| 生图测评 | [`image-quality-eval.md`](image-quality-eval.md) | 开工 | 淘宝套图过线池、工作台 vs 直调 vs 金标闸门 |
| 性能 | [`performance-governance.md`](performance-governance.md) | 开工 | 锁序、投递时延、列表/详情有界、admission 指标 |
| 生产可靠性 | [`agent-production-readiness.md`](agent-production-readiness.md) | 值班 | 实现切片已关闭。G-06 行为门槛由评测组的 `run_id` 裁定 |
| 运行时所有权 | [`agent-runtime-ownership.md`](agent-runtime-ownership.md) | 关闭 | S0–S6 已完成。不再发单 |

稳定事实写回 `CONTEXT.md`、`docs/ARCHITECTURE.md`、`docs/PRD.md` 或 `docs/USER_GUIDE.md`。未完成方向由 [`../ROADMAP.md`](../ROADMAP.md) 索引。历史叙事：[`../history/agent-runtime-timeline.md`](../history/agent-runtime-timeline.md)。

ROADMAP 里的工作室增量、对话壳、交付图规格等**没有**对应验收账本，不走本板，直到有人先立章程再发单。
