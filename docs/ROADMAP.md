# ProductFlow 路线图

尚未成为产品事实、或尚未被真实验证的方向。当前能力见 [`PRD.md`](PRD.md)，结构见 [`ARCHITECTURE.md`](ARCHITECTURE.md)，操作见 [`USER_GUIDE.md`](USER_GUIDE.md)。

## 近期

### 工作台证明

人离开 Agent 也必须能完整操作画布，交互按 [`specs/workbench.md`](specs/workbench.md)：命令靠近对象、失败写在对象上、结果立刻可再操作。这是不可妥协的质量条，不因 Agent 沙箱切片往后排。

工作台代码已经按 [`USER_GUIDE.md`](USER_GUIDE.md) 和该规格接线。还缺 1440 / 1024 / 390、真实 provider、PostgreSQL / Redis / worker 的连续动作证据：复制粘贴、拖线原因、运行失败写在对象上、配方预览确认、底抽屉不挡节点、无 console/network error。

证明之后删除 `specs/workbench.md`，把仍约束用户操作的句子留在 USER_GUIDE。

### Agent 画布沙箱

已落地：Turn run 身份、创建即现图、画布/全局会话归属、`sameRuntimeScope` 忽略 prompt / live-graph 刷新、商品路径不再写入 WorkflowDraft（intake 在 Product 上）、Goal 托管环接到显式 `AgentTask`（跑图结束不等于 Goal 完成）。当前合同见 CONTEXT / PRD / ARCHITECTURE。对话壳见 [`adr/0009-agent-canvas-sandbox.md`](adr/0009-agent-canvas-sandbox.md)。

Goal 托管环（跑图 → 看结果 → 改画布 → 再跑）已接到商品工作台对话侧栏。完成只能由用户点完成；图上可核对完成标准尚未自动化。Agent 对话壳（侧栏收起、手机对话 sheet、chip）不在本项。画布连续动作仍按上一节工作台证明验收。

对话创建的追问质量和视觉样本还没有。`just web-e2e-live-graph` 继续只覆盖跳过 Agent 的直接创建，直到对话路径有独立浏览器回归。

Pi 边界见 [`adr/0007-pi-agent-runtime-boundary.md`](adr/0007-pi-agent-runtime-boundary.md) 与 [`specs/pi-agent-runtime-integration.md`](specs/pi-agent-runtime-integration.md)。后台 durable Task 与多实例对账只有单独 gate 通过后才扩大默认能力；旧 Go Agent 留在 `exp`，不是 main 的隐式 fallback。

### 工作室增量

[`specs/studio-increments.md`](specs/studio-increments.md)：镜头列表默认主区、生成套图文案、创建页推荐套图、出图后局部修、结果保真核对清单。落地前不写进 CONTEXT / PRD / ARCHITECTURE。内置 DeliverySpec 模板已在 ARCHITECTURE §7。配方库不预置官方画布模板，只保留用户主动保存的配方。

### 图片生产质量

- provider 真实尺寸、格式和高级字段的合同测试。
- 商品形态保真、文字准确度、视觉统一性的评估样本。
- 交付图规格、裁切预览和批量下载。
- 生成失败、取消、重试和 provider note 的用户反馈。

### 主线清理

旧 V1 归档、Gallery 回填、WorkflowDraft 写入桩和兼容读取器按 [`adr/0010-mainline-no-compatibility.md`](adr/0010-mainline-no-compatibility.md) 删除。不补做部署级回填或 cutover 证据。

### Agent 耐久

真实 provider、真实库、SSE 断线恢复、后台 durable / reconciliation 的部署 gate 与能力声明。见 [`rollout/pi-agent-durability.md`](rollout/pi-agent-durability.md)。

Session、Task、WorkflowRun 不得合并，见 `CONTEXT.md`。业务级 Task 调度器、跨进程 durable admission、统一 Fresh Observation 仍未做。

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
