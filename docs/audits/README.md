# 内部业务组

这里维护仓库内的专项工作：业务组章程保存目标合同、验收结论和证据引用；[`tasks/`](tasks/README.md) 中每份文件是一张可认领 issue；关闭记录在 [`tasks/archive/`](tasks/archive/README.md)。Git 保存变更历史，审核结论与交付 commit 写在 issue 内。

组对问题方向和验收口径负责，代码修改范围由每张 issue 的真实因果决定。跨组成果通过具体 issue、条款和证据引用交接。主代理维护发布和集成，不按组建立永久代码领地。

## 业务组

| 组 | 章程 | 组状态 | 管什么 | 后续发布条件（未发布不构成开工许可） |
|---|---|---|---|---|
| 评测 | [agent-eval-system.md](agent-eval-system.md) | 开工 | L0–L6、grader、pass^k、分数采信 | user-sim 完成后核对是否仍需独立 sim-live；生产 mine 有 Turn 后发 production-tasks；L2/L5 有有效 run_id 后发 nightly；kappa 达标后发 judge 计入 pass |
| 壳进化 | [agent-self-harness.md](agent-self-harness.md) | 开工 | 领域壳版本化与进化控制器 | P1 完成后发 harness-attribution（P2）；P2b–P7 按阶段门逐步发布 |
| 画布 | [canvas-test-system.md](canvas-test-system.md) | 开工 | 文稿权威、用户已发布稿保护 | inspector-midrun 完成后发 canvas-c4-remainder；C0–C3、C5、C6 已收工 |
| 架构重构 | [architecture-refactoring.md](architecture-refactoring.md) | 开工 | 检查器草稿版本语义、journal 发布与确认职责 | 画布项复用 inspector-midrun，范围不足时调整原单；journal 调查获采信且收益成立后发实现单，允许保留现状 |
| 生图测评 | [image-quality-eval.md](image-quality-eval.md) | 开工 | 淘宝套图池、工作台/直调/金标闸门 | 池规模不足时按本轮采集证据发扩池；闸门未过须按失败原因裁定，不发纯刷分任务 |
| 性能 | [performance-governance.md](performance-governance.md) | 开工 | 锁序、投递时延、查询有界、admission | 当前详情、时延、指标三项验收后发 PERF-12 session plan；SaaS 分租户不在本组当前范围 |
| 生产可靠性 | [agent-production-readiness.md](agent-production-readiness.md) | 值班 | G-06 引用评测组可采信 run_id | 实现切片已关闭，当前不发实现 issue |
| 运行时所有权 | [agent-runtime-ownership.md](agent-runtime-ownership.md) | 关闭 | S0–S6 历史验收证据 | 不再发单 |

当前 issue 的状态、执行者和阻塞情况统一看 [Issue 看板](tasks/README.md)，本页不重复维护任务清单。

## 文档与外部 Issue

稳定事实写回 `CONTEXT.md`、`docs/ARCHITECTURE.md`、`docs/PRD.md` 或 `docs/USER_GUIDE.md`；未完成产品方向由 [`../ROADMAP.md`](../ROADMAP.md) 索引。历史叙事在 [`../history/agent-runtime-timeline.md`](../history/agent-runtime-timeline.md)。

GitHub Issues 承接产品需求、PRD 和对外问题；本地 issue 承接这些业务组的有界执行任务，规则见 [`../agents/issue-tracker.md`](../agents/issue-tracker.md)。关联时互记链接，任务关闭不自动关闭 GitHub 产品 issue，也不自动完成章程阶段门。普通小修、只读调查无需建单；新增专项方向由维护者确认章程后发布任务。
