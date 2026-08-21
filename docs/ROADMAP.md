# ProductFlow Roadmap

本文只记录尚未实现或尚未取得真实验证证据的方向。当前已交付能力见 `PRD.md`，当前代码结构见 `ARCHITECTURE.md`，V1 切换证据见 `rollout/workflow-v2-cutover.md`。在线 schema-v3 图已经写进那些当前实现文档；GraphProposal、配方从 live graph 提取保存和 Recipe ChangeSet 只从本文 §2 进入。

## 近期优先级

### 0. Agent 运行底座迁移到 Pi（main 已切换，验收证据待补）

- 当前 `main` 使用 Node.js 22 + Pi SDK ProductFlow Agent adapter。ProductFlow 的 FastAPI、Draft、确认、WorkflowRun、素材 owner 和 Web projection 合同继续由现有模块负责。
- 旧 Go Agent service 与 `agent-harness` 已保留在 `exp` 分支，继续验证 durable Turn、后台 Task、崩溃恢复、效果对账和调度；它不作为 `main` 的隐式运行时 fallback。
- 两条线共享 Tool/Context/Draft/事件合同和质量样本，隔离 runtime journal、session storage 和调度实现。
- 实施规则、当前交付边界、Skill 编写规则、动态 Context、Tool 边界和完成定义见 `docs/adr/0007-pi-agent-runtime-boundary.md` 与 `docs/specs/pi-agent-runtime-integration.md`。
- 当前未完成：真实 provider、真实 PostgreSQL/Redis、真实浏览器、SSE 断线恢复和后台 durable/reconciliation gate。
- 阶段 0 至 4 以交互式 Turn、只读能力、Question、Draft 和待确认 WorkflowRun 为主；后台 Task 和 durable recovery 只有在单独 gate 通过后才扩大默认能力。

### 1. Agent 创建质量

- 用真实商品和真实 provider 建立端到端回归样本。
- 评估 Agent 的追问数量、事实准确度、视觉体系一致性和单图提示词质量。
- 优化确认面板的信息密度、冲突处理和修改反馈。
- 验证 Turn 断线、重启、问题回答和 Draft 确认后 graph persist 恢复。

### 2. Schema-v3 未完成项

在线工作流权威已经是 `workflow_graphs`。当前实现见 `ARCHITECTURE.md`。画布修葺的北极星、质量上限、非目标和切片顺序见 `docs/specs/v3-canvas-restoration.md`。仍未交付：

- 对象命令与呈现身份（节点卡类型色/失败反馈、粘贴后选中、删除确认、视口处建点、最大化、busy 锁定、运行前 flush）。
- 服务端 Redo（inverse ChangeSet；不得把 Redo 映射成再调 Undo）。
- Inspector 按 Node Catalog `config_fields` 渲染；当前类型化表单是过渡适配。
- 进入分组：一层视觉组织、组内/全图分记视口。
- 从 live v3 graph 提取并保存配方，以及 Recipe ChangeSet 直接应用到另一张图。应用已保存配方仍只产生待确认 Draft。
- Agent 对 live graph 的 GraphProposal / ChangeSet。目标合同见 `docs/adr/0008-free-canvas-agent-graph-authority.md`。
- `docs/rollout/free-canvas-v3-interaction-parity.md` 中尚未完成的交互项；该表是检查清单，质量上限以修葺北极星 §4 为准。
- 真实 provider、PostgreSQL/Redis/worker、桌面与 390px 浏览器、console/network error、取消/retry 和无 retired runtime fallback 的完整 gate。

### 3. 图片生产质量

- 增加 provider 真实尺寸、格式和高级字段的合同测试。
- 建立商品形态保真、文字准确度和视觉统一性的评估样本。
- 改进交付图规格、裁切预览和批量下载。
- 完善生成失败、取消、重试和 provider note 的用户反馈。

### 4. 全局图库与工作流子图库

当前在线入口、组织操作和整理 Draft 见 `PRD.md`。仍待完成：

- current-schema 旧 `ImageGalleryEntry` 表的部署级回填、引用审计、观察窗和物理清理资格。
- 旧 `legacy_canvas_agent_20260518_0032` Gallery-only bridge 的真实部署演练、备份恢复证据和 approval。Agent archive 不在这条 bridge 范围内。
- 跨商品等更高范围的 Agent 写操作。
- v3 工作流子图库关联、图片节点绑定和 `reference` edge 的前端适配与浏览器验收。

### 5. 全局 Agent 与人工工作流协作

当前 Session、Task、Dock 和待确认 WorkflowRun 请求见 `PRD.md` 与 `ARCHITECTURE.md`。仍待完成：

- ProductFlow 业务级 Task 调度器。
- 跨进程 durable admission；`AGENT_MAX_CONCURRENT_TURNS` 只限制当前 Agent 进程。
- 统一的执行前 Fresh Observation harness 抽象。
- 更多有副作用操作和更完整的受影响对象跳转。

产品边界见 `specs/global-agent-human-workflow-design.md`；Pi runtime 规则见 `specs/pi-agent-runtime-integration.md`。

### 6. 配方

- 优化配方预览、版本说明和应用前差异确认。
- 保持配方完全由用户主动保存。

### 7. 开发体验

- 缩短本地启动、迁移、测试和真实 provider 验证路径。
- 保持配置样例、README、用户指南、CONTEXT、ADR、ARCHITECTURE 和 package AGENTS 与当前实现同步。
- 增加跨层合同测试，减少 DTO、route 和 provider 配置漂移。
- 保持运行时代码中无平行兼容模型。

## 中期方向

- 更丰富的商品事实冲突解决和结构化规格录入。
- 视觉体系的用户级保存、版本比较和跨商品复用。
- 图片质量评分、相似候选聚类和人工选择辅助。
- 更多图片 provider adapter 和可观测性。
- 工作流运行成本、时延和失败率统计。

## schema-v3 之后的工程运行时

以下条目在 schema-v3 治理基线完成前不得开工。它们不改变当前 FastAPI / Dramatiq / Alembic 运行单元，也不进入 `CONTEXT.md`、`PRD.md`、`ARCHITECTURE.md` 的当前事实段落。v3 仍是主线。

### Python 业务后端迁到 Go

- 产品合同：`docs/specs/go-backend-rewrite-prd.md`（Draft）
- 实现设计与切片：`docs/specs/go-backend-rewrite-design.md`（Draft）
- 只替换业务 API、worker 和 async dispatcher。Web 与 Node.js/Pi Agent service 保持现有合同。
- PostgreSQL 仍是商品、Draft、graph、Run、素材和 Agent 投影的权威。Redis/asynq 只负责可恢复投递。
- 默认栈：Gin、GORM、Viper、zap、go-redis、asynq。内部按功能竖切；GORM 不得 AutoMigrate，也不得替换 `FOR UPDATE` / advisory lock / `async_dispatches`。
- 开工前提：schema-v3 在线 graph 稳定、v2 leftover 删除、HTTP/SSE/session/queue 合同包已导出。
- `exp` 上的 Go Agent service 不是本项目的起点。

## SaaS 阶段

进入 SaaS 前需要单独设计：

- tenant、workspace 和成员权限。
- 配额、计费、成本归属和滥用控制。
- 对象存储、备份、恢复和数据保留。
- schema/API 兼容窗口和迁移承诺。
- 审计日志、合规、隐私和正式 SLO。

这些合同从 SaaS 基线开始建立，不反向约束当前 live demo。
