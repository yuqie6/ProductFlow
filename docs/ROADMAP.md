# ProductFlow 路线图

尚未成为产品事实、或尚未被真实验证的方向。当前能力见 [`PRD.md`](PRD.md)，结构见 [`ARCHITECTURE.md`](ARCHITECTURE.md)，操作见 [`USER_GUIDE.md`](USER_GUIDE.md)。

## 近期

### 工作台证明

工作台代码已经按 [`USER_GUIDE.md`](USER_GUIDE.md) 和 [`specs/workbench.md`](specs/workbench.md) 接线。还缺 1440 / 1024 / 390、真实 provider、PostgreSQL / Redis / worker 的连续动作证据：复制粘贴、拖线原因、运行失败写在对象上、配方预览确认、底抽屉不挡节点、无 console/network error。

证明之后删除 `specs/workbench.md`，把仍约束用户操作的句子留在 USER_GUIDE。

### Agent 创建质量

- `just web-e2e-live-graph` 只覆盖跳过 Agent 的直接创建。对话创建路径和质量样本还没有。
- 评估追问数量、事实准确度、视觉体系一致性和单图提示词质量。
- 确认面板的信息密度、冲突处理和修改反馈。
- Turn 断线、重启、问题回答和 Draft 确认后 graph persist 的恢复。

实施边界见 [`adr/0007-pi-agent-runtime-boundary.md`](adr/0007-pi-agent-runtime-boundary.md) 与 [`specs/pi-agent-runtime-integration.md`](specs/pi-agent-runtime-integration.md)。后台 durable Task 与多实例对账只有单独 gate 通过后才扩大默认能力；旧 Go Agent 留在 `exp`，不是 main 的隐式 fallback。

### 工作室增量

[`specs/productflow-studio-requirements.md`](specs/productflow-studio-requirements.md)：镜头列表默认主区、生成套图文案、创建页推荐套图、出图后局部修、平台交付预设、结果保真核对清单。落地前不写进 CONTEXT / PRD / ARCHITECTURE。配方库不预置官方画布模板，只保留用户主动保存的配方。

### 图片生产质量

- provider 真实尺寸、格式和高级字段的合同测试。
- 商品形态保真、文字准确度、视觉统一性的评估样本。
- 交付图规格、裁切预览和批量下载。
- 生成失败、取消、重试和 provider note 的用户反馈。

### 图库部署清理

当前在线入口见 PRD。还缺：

- current-schema 旧 `ImageGalleryEntry` 表的部署级回填、引用审计、观察窗和物理清理资格。
- 旧 `legacy_canvas_agent_20260518_0032` Gallery-only bridge 的部署演练、备份恢复证据和 approval。
- 跨商品等更高范围的 Agent 写操作。

证据与停止条件见 [`rollout/media-library-transition.md`](rollout/media-library-transition.md)。

### Agent 耐久

真实 provider、真实库、SSE 断线恢复、后台 durable / reconciliation 的部署 gate 与能力声明。见 [`rollout/pi-agent-durability.md`](rollout/pi-agent-durability.md)。

产品边界见 [`specs/global-agent-human-workflow-design.md`](specs/global-agent-human-workflow-design.md)。业务级 Task 调度器、跨进程 durable admission、统一 Fresh Observation 仍未做。

## 中期

- 更丰富的商品事实冲突解决和结构化规格录入。
- 视觉体系的用户级保存、版本比较和跨商品复用。
- 图片质量评分、相似候选聚类和人工选择辅助。
- 更多图片 provider adapter 和可观测性。
- 工作流运行成本、时延和失败率统计。

## 工作台证明之后的工程运行时

下列条目在工作台浏览器证明完成前不得开工，也不进入 CONTEXT / PRD / ARCHITECTURE。

### Python 业务后端迁到 Go

- 产品合同：[`specs/go-backend-rewrite-prd.md`](specs/go-backend-rewrite-prd.md)
- 实现设计：[`specs/go-backend-rewrite-design.md`](specs/go-backend-rewrite-design.md)
- 只替换业务 API、worker 和 async dispatcher。Web 与 Node.js/Pi Agent 保持现有合同。
- 开工前提：工作台证明完成、v2 leftover 删除、HTTP / SSE / session / queue 合同包已导出。
- `exp` 上的 Go Agent 不是本项目的起点。

## SaaS

进入 SaaS 前需要单独设计 tenant、配额计费、对象存储与保留、兼容窗口、审计与 SLO。这些合同从 SaaS 基线开始，不反向约束当前 live demo。
