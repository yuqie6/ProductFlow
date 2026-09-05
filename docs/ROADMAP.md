# ProductFlow 路线图

尚未成为产品事实、或尚未被真实验证的方向。当前能力见 [`PRD.md`](PRD.md)，结构见 [`ARCHITECTURE.md`](ARCHITECTURE.md)，操作见 [`USER_GUIDE.md`](USER_GUIDE.md)。

跨层交付由 Agent 质量、Agent 自进化、图片质量、工作流体验、平台可靠性五组承接。组内任务按结果串行，组外消费已可用的固定输入，职责与次序在 [`audits/README.md`](audits/README.md)，issue 看板在 [`audits/tasks/README.md`](audits/tasks/README.md)。先认领再开工，一人一份。组文档保存合同和证据；用户独立交办任务也可进入同一看板，无需建组。

## 近期

### 工作室增量

落地前不写进 CONTEXT / PRD / ARCHITECTURE。

| 项 | 用户可见 | 不做 |
|---|---|---|
| 镜头列表默认主区 | 工作台打开先看镜头行（图种、张数、状态、缩略图、运行此镜头），可切到同一张 schema-v3 画布 | 新 Shot 表、第二套执行器 |
| 生成套图文案 | 商家按钮跑现有整图 DAG（先内容节点，后各镜头生图），进度按镜头投影 | 另一套 run 模型 |

内置 DeliverySpec 模板已在 ARCHITECTURE §7。配方库只列用户从 live graph 保存的配方。

### Agent 对话壳与对话路径

- 侧栏收起、手机对话 sheet、建议 chip。决策边界见 [`adr/0009-agent-canvas-sandbox.md`](adr/0009-agent-canvas-sandbox.md)。
- 对话创建的追问质量和视觉样本。
- 对话路径的独立浏览器回归。`just web-e2e-live-graph` 覆盖跳过 Agent 的直接创建与工作台连续动作。

### Agent 耐久

下一候选仍需 G-06 的可采信真实业务证据与 G-07 的干净固定 checkout 全量验证，口径见 [平台可靠性发布合同](audits/performance-governance.md#production-gates)。历史 S1–S6 实现切片已交付，旧 G-07 PASS 仅对 `fb658633` 有效，不代表后续候选通过；[原生产计划](history/agent-runtime-timeline.md#platform-production-gate-history)与[运行时所有权记录](history/agent-runtime-timeline.md#runtime-ownership-evidence)保留历史证据，不重新开工。

剩余：G-06 的行为门槛由 [评测组章程](audits/agent-eval-system.md) 根据已审核的 `run_id` 裁定，执行进度见 [Issue 看板](audits/tasks/README.md)。采证 issue 关闭不等于门槛通过。未过门前不扩大默认能力，也不把 Pi session 文件当作 durable 证明。background 模型调用仍按生产账本 D-03 不接。旧 Go Agent 留在 `exp`。Session、Task、WorkflowRun 不得合并，见 `CONTEXT.md`。

### Agent 评测体系

未完成执行任务与阻塞统一见 [Issue 看板](audits/tasks/README.md)，业务组后续发布条件见 [业务组索引](audits/README.md)。任务文件限定本次交付，仍需读取现场代码与测试。冻结决策与历史 `run_id` 在 [`audits/agent-eval-system.md`](audits/agent-eval-system.md)。未登记 `run_id` 前不把 pass^k 写成产品事实。

### Agent 自进化

自进化组在固定生产模型组合与业务版本上，针对有明确后置条件的商家任务交付自动发现、因果假设验证、指令/代码候选和有界多轮改进，最终一次用户审批；控制器尚未实现。复用现有 Pi、领域工具与公共评测，组件、证据及交付顺序由 [当前自进化章程](audits/agent-self-harness.md) 维护。已有 P1/P2/P2b 保留，旧 P3–P7 与 S0–S6 路线已替代。

开发输入仍有一次性启动阻塞，见 [开发基线](audits/tasks/eval-development-baseline.md)。有效固定输入交付后本组自行编排，不等待 [人工 Skill 修复](audits/tasks/eval-skills.md) 完成。用户不满意不直接证明 Agent 错误；本期从可测开发证据选题，不要求线上反馈系统先就绪。跨模型泛化、主观反馈归因、长期偏好与通用 harness 替换留在章程的 [未来问题](audits/agent-self-harness.md#未来问题)，不增加本期门槛；Steer、长期记忆和无人工发布仍不构成前置。

### 运行时性能治理

待验证方向：Graph/Agent 故障到用户可见收敛的时间预算、恢复积压与正常投递共存的尾延迟，以及实际部署中的连接与锁等待。连续生图活动 Status/SSE 在固定夹具下已由 [perf-imagesession-active-status](audits/tasks/archive/perf-imagesession-active-status.md) 交付。按 [平台可靠性章程](audits/performance-governance.md#下一步如何选择) 的风险与发布条件选题。候选尚未固定时不反复跑全量门；SaaS tenant admission 另属新产品基线。

### 画布文稿权威测试

C0–C6 已落地。C4 空闲改写/候选、运行中检查器打字、整图跑中途撤销、文稿 409 停止、检查器运行该节点/运行到这里与运行中取消已进 `just web-e2e-canvas-document`。C3 整图跑中途撤销具名钉死见 [canvas-graph-run-undo](audits/tasks/archive/canvas-graph-run-undo.md)。浏览器撤销与 409 见 [canvas-c4-remainder](audits/tasks/archive/canvas-c4-remainder.md)。检查器运行按钮证据见 [canvas-c4-run-controls](audits/tasks/archive/canvas-c4-run-controls.md)，不重复派版本语义任务。

### 职责收拢

检查器草稿版本语义已由 [canvas-inspector-midrun](audits/tasks/archive/canvas-inspector-midrun.md) 交付。[journal 调查](audits/tasks/archive/arch-journal-assessment.md) 已完成，结论为保留现状，不授权实现。[原架构候选记录](history/agent-runtime-timeline.md#architecture-assessment-history) 保留来源与历史证据，不再单独设组或发单。

### 图片生产质量

- provider 真实尺寸、格式和高级字段的合同测试。
- 内部淘宝套图对照与质量改进由 [图片质量组](audits/image-quality.md) 承接；当前任务 [`image-eval-pool`](audits/tasks/image-eval-pool.md) 保持原认领与合同，图片闸门与 Agent 分数分别验收。
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
