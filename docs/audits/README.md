# 内部业务组

这里维护仓库内的专项工作，目标是让商家可靠、可控地得到可用商品素材图。**一份组文档对应一个组**，职责、合同、验收结论和证据引用都保存在该文档；本目录除索引外只保留下表 4 份组文档。[`tasks/`](tasks/README.md) 中每份文件是一张可认领 issue；关闭记录在 [`tasks/archive/`](tasks/archive/README.md)。

组对问题方向和验收口径负责，代码修改范围由每张 issue 的真实因果决定。跨组成果通过具体 issue、条款和证据引用交接。主代理维护发布和集成，不按组建立永久代码领地。

业务组只组织长期方向。用户直接提出、希望交给其他 Agent 的问题可以作为独立任务进入同一看板，不需要业务组或父章程；请求当前会话直接修复时无需建单。任务来源与交办方式见 [公共任务池](tasks/README.md#用户怎么交办)。

## 当前业务组

| 组 | 章程 | 组状态 | 管什么 | 后续发布条件（未发布不构成开工许可） |
|---|---|---|---|---|
| 工作流体验 | [canvas-test-system.md](canvas-test-system.md) | 开工 | 商家编辑、运行与结果使用链的业务正确性；当前交付聚焦文稿权威与检查器草稿保存 | C4/AR-01、C3 整图 undo 与浏览器整图撤销/文稿 409 均已归档；本组无未关闭 issue，不重复派版本语义任务 |
| Agent 能力 | [agent-self-harness.md](agent-self-harness.md) | 开工 | Agent 按商品事实和用户意图执行；Skill 修复、领域壳版本化与受控进化 | Skill 修复由评测复核；P1/P2/P2b 已交付，P3–P7 按既有阶段门发布，不因合组提前开工 |
| 评测 | [agent-eval-system.md](agent-eval-system.md) | 开工 | Agent 行为与商品图质量的独立测量；题库、样本池、grader、校准、分数采信 | user-sim 后核对 sim-live；生产 mine 有 Turn 后发 production-tasks；L2/L5 有有效 run_id 后核对 nightly；kappa 达标后发 judge 计入 pass；图片扩池按采集证据发布 |
| 平台可靠性 | [performance-governance.md](performance-governance.md) | 开工 | 执行不丢、不重复、不越权；lease、journal、恢复、队列、容量与查询成本 | PERF-06 积压续投已归档；PERF-12 仍缺目标规模 payload，本轮不发该后续单；journal 调查结论为保留现状，不发实现单；G-06 汇总评测证据，G-07 按候选基线复核 |

当前 issue 的状态、执行者和阻塞情况统一看 [Issue 看板](tasks/README.md)，本页不重复维护任务清单。

## 职责裁定

- 每张业务组 issue 只有一个交付组和一个父章程，独立任务无需归组。按失败合同归属，不按发现问题的组或代码目录归属。架构调整是交付手段，不单独设组或另建验收队列。
- 工作流体验负责“保存、执行、资产与交付行为是否符合商家操作”；Agent 能力负责“是否正确理解并使用这些能力”。同一 Graph Command 缺陷只交工作流体验修复，Agent 任务引用结果。
- 评测负责“测得是否可信、是否达到质量门槛”。Skill、生产提示词或生成链缺陷交 Agent 能力或工作流体验修复；测评代码和数据缺陷仍由评测修复。Agent 分数与图片四维评分独立报告，不合并 runner、schema 或通过条件。
- 平台可靠性负责执行基础与成本约束；Graph 文稿采用规则归工作流体验，采用过程的锁序、lease 和重复执行归平台可靠性。根因跨层时由维护者指定一张主 issue 的完整修改范围，其余组提供合同与验收证据，不设第二个 writer。
- 被测行为与验收题目不在同一实现任务里修改。题目确实过时时，评测根据产品合同单独修订并冻结新 task_hash，再比较同题基线与候选；不得将改题收益算作能力提升。主代理汇总验收，自执行时注明自审，不伪称独立审核。
- 评测裁定行为分数和图片闸门；平台可靠性维护生产 Gate 汇总；主代理决定交付与发布。任何单组、单项 live 或任务关闭都不能替代全部相关门槛。

## 当前投入顺序

1. 优先处理商家可感知的已知问题：运行中编辑保存与 Agent 系统性 Skill 失败。人工修复无需等待 Self-Harness 控制器建成。
2. 评测完善有效证据与校准，平台补容量观测。人工标签、生产 Turn、淘宝登录等外部输入由维护者协调；缺输入保持阻塞，不用开发库或模型标签冒充。
3. 壳工件 P1、归因 P2 与有界轨迹 P2b 已交付；争用人员或冻结资源时让位于前两项。P3–P7、后续重构和扩池只按已登记前置发布，不以持续发单作为产出。journal 有界调查已关闭，结论仍见平台可靠性章程。

组数不等于并发数。沿用看板的单 writer 和冻结窗口规则；共享 dev 的 mock 切换、真实生图、Agent live 与容量压测必须排期。本次没有认领或启动实现任务。

## 合并与历史

2026-09-05 按用户直接指派，从 `e0261711` 干净工作树核对 8 份账本、11 张未关闭 issue 及相关源码。原名册含 6 个开工组、1 个值班组、1 个已关闭组，现收敛为上述 4 个责任组：

- 画布与架构 AR-01 归工作流体验；壳进化与 `eval-skills` 归 Agent 能力；生图测评归评测；性能、生产可靠性与架构 AR-02 归平台可靠性。
- 生图测评文件删除，合同与证据并入评测组的 [图片质量验收](agent-eval-system.md#image-quality)；生产可靠性文件删除，合同与证据并入平台可靠性组的 [生产 Gate](performance-governance.md#production-gates)。数值门槛保持原值，不保留独立专题文档。
- 架构重构与运行时所有权组文件删除。仍有效的架构合同进入承接组与任务；[候选来源](../history/agent-runtime-timeline.md#architecture-assessment-history) 和 [已关闭所有权验收](../history/agent-runtime-timeline.md#runtime-ownership-evidence) 收入现有历史时间线，不新建档案文档。已关闭 S0–S6 不重新启动。
- 未关闭 issue 保留原 ID，迁移组与父章程；已归档 issue 保留当时归属、审核和 commit。组织调整不改变技术完成状态，不重写历史 run_id。

以下旧归属仅用于读取归档记录，不接受新任务：

| 历史组 | 原父账本 | 状态 |
|---|---|---|
| 性能 | [performance-governance.md](performance-governance.md) | 已并入平台可靠性，仅保留归档归属 |

## 文档与外部 Issue

稳定事实写回 `CONTEXT.md`、`docs/ARCHITECTURE.md`、`docs/PRD.md` 或 `docs/USER_GUIDE.md`；未完成产品方向由 [`../ROADMAP.md`](../ROADMAP.md) 索引。历史叙事在 [`../history/agent-runtime-timeline.md`](../history/agent-runtime-timeline.md)。

GitHub Issues 承接产品需求、PRD 和对外问题；本地 issue 承接业务组任务和用户独立交办的执行任务，规则见 [`../agents/issue-tracker.md`](../agents/issue-tracker.md)。关联时互记链接，任务关闭不自动关闭 GitHub 产品 issue，也不自动完成章程阶段门。只有需要长期组织的新专项方向才建立章程，独立问题不以归组为执行前提。
