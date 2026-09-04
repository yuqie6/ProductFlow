# 验收

未完成工作在 [`tasks/`](tasks/)。**选一份打开，只读那一份，只改它列出的文件，证据写回那一份。** 不要从本页下面的总账本开工。

总账本是合同与历史证据，不是任务说明书。

## 可开工（互不改对方文件）

| 指导 | 做什么 |
|---|---|
| [`tasks/eval-skills.md`](tasks/eval-skills.md) | 改 Skill 与既有评测题，提高 L1 可复现 pass，让变异能杀 |
| [`tasks/eval-user-sim.md`](tasks/eval-user-sim.md) | 模拟用户改走独立模型生成话语 |
| [`tasks/eval-labels.md`](tasks/eval-labels.md) | 填 50 条人工标签并算出 kappa |
| [`tasks/eval-go-loader.md`](tasks/eval-go-loader.md) | Go 评测 loader 拒绝未知字段 |
| [`tasks/eval-live-layers.md`](tasks/eval-live-layers.md) | 跑 L2/L5/生产 mine（不改生产代码） |
| [`tasks/harness-artifact.md`](tasks/harness-artifact.md) | 做出可哈希的 Agent 领域壳工件 |
| [`tasks/canvas-inspector-midrun.md`](tasks/canvas-inspector-midrun.md) | mock 浏览器门覆盖运行中检查器打字 |
| [`tasks/image-eval-pool.md`](tasks/image-eval-pool.md) | 扩淘宝过线池并登记 live 闸门 |
| [`tasks/perf-imagesession-detail.md`](tasks/perf-imagesession-detail.md) | 连续生图详情读取有界 |
| [`tasks/perf-dispatcher-latency.md`](tasks/perf-dispatcher-latency.md) | 测 PENDING→SENT 负载时延 |
| [`tasks/perf-capacity-metrics.md`](tasks/perf-capacity-metrics.md) | 生图 admission 的 wait/running/denied 指标 |

一次最多派三个实现代理，且他们选的指导「只改这些文件」不能相交。

## 总账本（证据，不开工）

| 文件 | 用途 |
|---|---|
| [`agent-production-readiness.md`](agent-production-readiness.md) | 生产可靠性合同。实现切片已关闭。 |
| [`agent-runtime-ownership.md`](agent-runtime-ownership.md) | 运行时职责迁移证据。已关闭。 |
| [`agent-eval-system.md`](agent-eval-system.md) | 评测冻结决策与历史 `run_id`。 |
| [`agent-self-harness.md`](agent-self-harness.md) | 壳进化全程合同。当前开工用 `tasks/harness-artifact.md`。 |
| [`canvas-test-system.md`](canvas-test-system.md) | 画布文稿权威合同。当前开工用 `tasks/canvas-inspector-midrun.md`。 |
| [`image-quality-eval.md`](image-quality-eval.md) | 生图测评合同。当前开工用 `tasks/image-eval-pool.md`。 |
| [`performance-governance.md`](performance-governance.md) | 性能基线与锁序。当前开工用上面三份 perf 指导。 |

稳定事实写回 `CONTEXT.md`、`docs/ARCHITECTURE.md`、`docs/PRD.md` 或 `docs/USER_GUIDE.md`。未完成方向由 [`../ROADMAP.md`](../ROADMAP.md) 索引。历史叙事：[`../history/agent-runtime-timeline.md`](../history/agent-runtime-timeline.md)。
