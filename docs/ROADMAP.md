# ProductFlow 路线图

尚未成为产品事实、或尚未被真实验证的方向。当前能力见 [`PRD.md`](PRD.md)，结构见 [`ARCHITECTURE.md`](ARCHITECTURE.md)，操作见 [`USER_GUIDE.md`](USER_GUIDE.md)。

跨层验收未完成工作在 [`audits/tasks/README.md`](audits/tasks/README.md) 看板：先认领再开工，一人一份。总账本只留合同与证据。

## 近期

### 工作室增量

落地前不写进 CONTEXT / PRD / ARCHITECTURE。

| 项 | 用户可见 | 不做 |
|---|---|---|
| 镜头列表默认主区 | 工作台打开先看镜头行（图种、张数、状态、缩略图、运行此镜头），可切到同一张 schema-v3 画布 | 新 Shot 表、第二套执行器 |
| 生成套图文案 | 商家按钮跑现有整图 DAG（先内容节点，后各镜头生图），进度按镜头投影 | 另一套 run 模型 |
| 出图后局部修 | 对已有 `ProductImageAsset` 消除 / 换字 / 局部重绘；新资产保留谱系；失败不改节点当前结果 | 第七类节点、去水印货架、批量 200 张 |

内置 DeliverySpec 模板已在 ARCHITECTURE §7。配方库只列用户从 live graph 保存的配方。

### Agent 对话壳与对话路径

- 侧栏收起、手机对话 sheet、建议 chip。决策边界见 [`adr/0009-agent-canvas-sandbox.md`](adr/0009-agent-canvas-sandbox.md)。
- 对话创建的追问质量和视觉样本。
- 对话路径的独立浏览器回归。`just web-e2e-live-graph` 覆盖跳过 Agent 的直接创建与工作台连续动作。

### Agent 耐久

lease、journal、effect 对账、SSE gap 与容量门的证据在 [`audits/agent-production-readiness.md`](audits/agent-production-readiness.md)。S1–S6 与 G-01–G-05、G-07 已关闭。运行时所有权 S0–S6 已关闭，证据在 [`audits/agent-runtime-ownership.md`](audits/agent-runtime-ownership.md)。

剩余：G-06 的行为门槛由 [`audits/tasks/eval-skills.md`](audits/tasks/eval-skills.md) 与 [`audits/tasks/eval-live-layers.md`](audits/tasks/eval-live-layers.md) 登记的 `run_id` 裁定。未过门前不扩大默认能力，也不把 Pi session 文件当作 durable 证明。background 模型调用仍按生产账本 D-03 不接。旧 Go Agent 留在 `exp`。Session、Task、WorkflowRun 不得合并，见 `CONTEXT.md`。

### Agent 评测体系

未完成指导（各读一份）：[`audits/tasks/eval-skills.md`](audits/tasks/eval-skills.md)、[`audits/tasks/eval-user-sim.md`](audits/tasks/eval-user-sim.md)、[`audits/tasks/eval-labels.md`](audits/tasks/eval-labels.md)、[`audits/tasks/eval-go-loader.md`](audits/tasks/eval-go-loader.md)、[`audits/tasks/eval-live-layers.md`](audits/tasks/eval-live-layers.md)。冻结决策与历史 `run_id` 在 [`audits/agent-eval-system.md`](audits/agent-eval-system.md)。未登记 `run_id` 前不把 pass^k 写成产品事实。

### Agent Self-Harness

当前开工：[`audits/tasks/harness-artifact.md`](audits/tasks/harness-artifact.md)。全程合同在 [`audits/agent-self-harness.md`](audits/agent-self-harness.md)。进化不得改评测 grader 刷分。

### 运行时性能治理

未完成指导：[`audits/tasks/perf-imagesession-detail.md`](audits/tasks/perf-imagesession-detail.md)、[`audits/tasks/perf-dispatcher-latency.md`](audits/tasks/perf-dispatcher-latency.md)、[`audits/tasks/perf-capacity-metrics.md`](audits/tasks/perf-capacity-metrics.md)。基线与锁序在 [`audits/performance-governance.md`](audits/performance-governance.md)。SaaS tenant admission 仍是新基线。

### 画布文稿权威测试

C0–C3、C5、C6 已落地。剩余：[`audits/tasks/canvas-inspector-midrun.md`](audits/tasks/canvas-inspector-midrun.md)。

### 图片生产质量

- provider 真实尺寸、格式和高级字段的合同测试。
- 内部淘宝套图对照：[`audits/tasks/image-eval-pool.md`](audits/tasks/image-eval-pool.md)。
- 交付图规格、裁切预览和批量下载。
- 生成失败、取消、重试和 provider note 的用户反馈。

## 中期

- 更丰富的商品事实冲突解决和结构化规格录入。
- 视觉体系的用户级保存、版本比较和跨商品复用。
- 图片质量评分、相似候选聚类和人工选择辅助。
- 更多图片 provider adapter 和可观测性。
- 工作流运行成本、时延和失败率统计。

## SaaS

进入 SaaS 前需要单独设计 tenant、配额计费、对象存储与保留、兼容窗口、审计与 SLO。这些合同从 SaaS 基线开始，不反向约束当前 live demo。
