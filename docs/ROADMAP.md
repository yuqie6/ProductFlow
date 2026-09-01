# ProductFlow 路线图

尚未成为产品事实、或尚未被真实验证的方向。当前能力见 [`PRD.md`](PRD.md)，结构见 [`ARCHITECTURE.md`](ARCHITECTURE.md)，操作见 [`USER_GUIDE.md`](USER_GUIDE.md)。

## 近期

### 工作室增量

落地前不写进 CONTEXT / PRD / ARCHITECTURE。

| 项 | 用户可见 | 不做 |
|---|---|---|
| 镜头列表默认主区 | 工作台打开先看镜头行（图种、张数、状态、缩略图、运行此镜头），可切到同一张 schema-v3 画布 | 新 Shot 表、第二套执行器 |
| 生成套图文案 | 商家按钮跑现有整图 DAG（先内容节点，后各镜头生图），进度按镜头投影 | 另一套 run 模型 |
| 出图后局部修 | 对已有 `ProductImageAsset` 消除 / 换字 / 局部重绘；新资产保留谱系；失败不改节点当前结果 | 第七类节点、去水印货架、批量 200 张 |
| 结果保真核对清单 | 人在结果上核对外形、颜色、文字，不满意则局部修或重跑该镜头 | 自动质量评分当作正式闸门 |

内置 DeliverySpec 模板已在 ARCHITECTURE §7。配方库只列用户从 live graph 保存的配方。

### Agent 对话壳与对话路径

- 侧栏收起、手机对话 sheet、建议 chip。决策边界见 [`adr/0009-agent-canvas-sandbox.md`](adr/0009-agent-canvas-sandbox.md)。
- 对话创建的追问质量和视觉样本。
- 对话路径的独立浏览器回归。`just web-e2e-live-graph` 覆盖跳过 Agent 的直接创建与工作台连续动作。

### Agent 耐久

通过下列 gate 之前，不扩大默认能力，也不把 Pi session 文件当作 durable 证明。边界见 [`adr/0007-pi-agent-runtime-boundary.md`](adr/0007-pi-agent-runtime-boundary.md)。旧 Go Agent 留在 `exp`，不是 main 的隐式 fallback。

详细合同、逐项状态、证据和生产 Gate 统一维护在 [`audits/agent-production-readiness.md`](audits/agent-production-readiness.md)。该账本是本方向的唯一验收指标；本节只保留路线图入口，不以当前实现或会话摘要缩减账本范围。

- 真实 provider、PostgreSQL / Redis、浏览器。
- 后台 durable Task、跨进程 claim、全量 effect reconciliation。
- 独立的 Fresh Observation harness（副作用前后端重读已是规则）。

Session、Task、WorkflowRun 不得合并，见 `CONTEXT.md`。

### Agent 运行时所有权

Go 的 AgentTurn/journal/lease/effect 写权威与 Node.js/Pi adapter 职责正在按 [`audits/agent-runtime-ownership.md`](audits/agent-runtime-ownership.md) 分刀收口。该账本记录所有权目标、S0～S6 checkpoint 和每刀证据；生产可靠性状态仍只由 [`audits/agent-production-readiness.md`](audits/agent-production-readiness.md) 裁定。

### 运行时性能治理

跨 Graph、Agent、异步投递、连续生图、SSE、数据库查询和容量 admission 的当前基线、锁序、优化顺序与验收 Gate 维护在 [`audits/performance-governance.md`](audits/performance-governance.md)。当前工作树已收口部分锁序、各域 recovery 批次、dispatcher cadence、SSE fanout、queued recovery backlog 指标和列表摘要查询；Graph 长 advisory、stale-running 指标、列表分页、剩余 N+1 与 SaaS admission 仍待专项验证或新基线设计。

### 图片生产质量

- provider 真实尺寸、格式和高级字段的合同测试。
- 商品形态保真、文字准确度、视觉统一性的评估样本。
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
