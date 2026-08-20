# ProductFlow Roadmap

本文只记录尚未实现或尚未取得真实验证证据的方向。当前已交付能力见 `PRD.md`，当前代码结构见 `ARCHITECTURE.md`，V1 切换证据见 `rollout/workflow-v2-cutover.md`。

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
- 验证 Turn 断线、重启、问题回答和 materialization 恢复。

### 2. Schema-v3 自由画布与 Agent 图协作

- 目标合同见 `docs/adr/0008-free-canvas-agent-graph-authority.md`。当前治理 checkout 位于 `e6cf9e66` 的 schema-v2 基线，代码树中没有 schema-v3 graph、run、API、前端工作台或迁移实现。
- 完成治理基线验证：确认 Product、WorkflowDraft、ProductWorkflow、WorkflowRun、Agent Session/Task/Conversation、MediaLibraryAsset 和 ProductImageAsset 的唯一在线 owner，并让 e6 全量测试、真实服务和浏览器主链路形成可重复证据。
- 从 confirmed WorkflowDraft 到 v3 初始图建立单一应用服务，再实现 Node Catalog、typed edge、Graph Context Compiler、ChangeSet、graph revision、operation group、GraphProposal 和 revision snapshot run。
- 工作台使用成熟 v1/v2 画布 shell、节点呈现、Inspector、运行侧栏和素材选择体验，数据源统一为 v3 graph/revision；不引入旧 DTO、query、mutation、plan key 或隐藏 reference merge。
- 完成 `MediaLibraryAsset -> ProductImageAsset -> WorkflowMediaLibraryAsset -> image_asset node -> reference edge` 的端到端适配，覆盖拖入空白处、绑定、换绑、未使用提示、一个素材节点连接多个下游和多个素材进入单一聚合端口。
- Agent 提案、人工编辑和配方应用统一进入 Graph Command Service；Agent run request、页面运行控制和 worker 统一进入 v3 WorkflowRun。
- v3 主链路通过完整 gate 后，再删除在线 v2 route、schema、application、page、hook、API、type 和对应测试。每个删除切片都要确认 v1 archive、Gallery bridge 和历史画布读路径不受影响。
- 当前迁移头为 `20260820_0070`。v3 实现重新开始时从当前 schema 设计新的迁移链，不复用已经从代码树删除的 `0071-0074` 作为当前实现或验收证据。
- 最终 gate 包括真实 provider、PostgreSQL/Redis/worker、Agent ChangeSet、并发冲突、桌面与 390px 浏览器、console/network error、edge 变化后的上下文重编译、运行历史、取消、retry 和无 retired runtime fallback residue scan。

### 3. 图片生产质量

- 增加 provider 真实尺寸、格式和高级字段的合同测试。
- 建立商品形态保真、文字准确度和视觉统一性的评估样本。
- 改进交付图规格、裁切预览和批量下载。
- 完善生成失败、取消、重试和 provider note 的用户反馈。

### 4. 全局图库与工作流子图库

- `/media-library` 已提供全局素材列表、搜索、文件夹、标签、归档/恢复、批量组织和选择反馈。
- `WorkflowMediaLibraryAsset` 已把全局素材关联到工作流子图库；同一图片可被多个工作流使用，关联不复制媒体 bytes。
- v3 继续使用这套素材库身份和组织能力。工作流子图库关联、图片节点绑定和 `reference` edge 分别表达“可选择”“节点持有”“运行实际使用”；前端适配与浏览器验收仍未完成。
- ImageChat 的保存动作已切换到 canonical `/api/media-library/from-session`，`/gallery` 已重定向到 `/media-library`；旧 `/api/gallery`、历史 DTO 和在线 runtime owner 已移除，current schema 的旧表部署级回填、引用审计、观察窗和物理清理资格仍待完成。旧 `legacy_canvas_agent_20260518_0032` 数据库已有 Gallery-only migration bridge：manifest、目标素材导入、source/hash 对账和独立 source retirement command 已实现；真实部署演练、备份恢复证据和 approval 仍待完成。Agent archive 不在这条 bridge 范围内。
- Agent 图库整理已接入有界读取、可确认 Draft、revision 校验、幂等确认、原子应用和结果投影；全局素材关联到明确工作流也已通过同一 Draft 机制落地，使用工作流 revision 和当前关联状态校验，不复制媒体 bytes。剩余工作是旧 Gallery 对账/owner 退休，以及跨商品等更高范围的 Agent 写操作。

### 5. 全局 Agent 与人工工作流协作

- 保留工作流画布的直接编辑、整图运行、单节点运行、取消、重试和运行记录入口；Agent 接入现有 `WorkflowRun`，不建立第二套执行器。
- 已实现 `AgentSession` 元数据、商品对话关联、列表/创建/改名/归档 API，以及工作台内按 Session 选择商品工作区的基础切换；全局 Agent 可以在当前 Session 下创建商品 onboarding 工作区，并在同一事务中创建等待人工输入的 onboarding `AgentTask`。创建页继续负责参考图和图片需求，Intake 成功后收口该 Task。
- 已实现独立 `AgentTask`、任务专属 run（兼容字段仍叫 harness run）、本轮 `AgentTurn` 关联和页面上下文快照；Session 切换不取消已落库业务目标，Task 目标不随路由变化。
- Global Agent Dock 已提供 Session/Task 列表、搜索、新建、归档、打开工作区、取消任务、暂停/恢复、Task 摘要和全局素材整理 Draft 投影/确认；业务级统一调度器仍待实现。Agent service 已通过 `AGENT_MAX_CONCURRENT_TURNS` 为当前进程的模型调用设置上限；跨进程 durable admission 仍未在 main 承诺。
- 按 Session 摘要、Task 目标、最近 Turn、当前页面上下文和执行前 Fresh Observation 分层组装上下文；Pi 负责当前 session 的消息和压缩，业务事实仍由 ProductFlow backend 重新观察。
- 已交付商品工作区和全局 Agent 的 WorkflowRun 监控工具；全局 Agent 可以针对明确商品、明确工作流和 revision 创建待确认执行请求，确认后复用现有 WorkflowRun 链路。统一的执行前 Fresh Observation harness 抽象、更多有副作用操作和更完整的受影响对象跳转仍待实现。全局素材到明确工作流的关联 Draft 已交付。

Global Agent、Session、Task 和人工工作流的产品落地策略见 `specs/global-agent-human-workflow-design.md`；Pi runtime 迁移策略见 `specs/pi-agent-runtime-integration.md`。

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

## SaaS 阶段

进入 SaaS 前需要单独设计：

- tenant、workspace 和成员权限。
- 配额、计费、成本归属和滥用控制。
- 对象存储、备份、恢复和数据保留。
- schema/API 兼容窗口和迁移承诺。
- 审计日志、合规、隐私和正式 SLO。

这些合同从 SaaS 基线开始建立，不反向约束当前 live demo。
