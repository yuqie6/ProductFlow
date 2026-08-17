# ProductFlow Roadmap

本文只记录尚未实现或尚未取得真实验证证据的方向。当前已交付能力见 `PRD.md`，当前代码结构见 `ARCHITECTURE.md`，V1 切换证据见 `rollout/workflow-v2-cutover.md`。

## 近期优先级

### 1. Agent 创建质量

- 用真实商品和真实 provider 建立端到端回归样本。
- 评估 Agent 的追问数量、事实准确度、视觉体系一致性和单图提示词质量。
- 优化确认面板的信息密度、冲突处理和修改反馈。
- 验证 Turn 断线、重启、问题回答和 materialization 恢复。

### 2. 工作台交互打磨

- 继续复用和完善现有节点卡片、详情面板、命令栏和侧栏。
- 优化大型 DAG 的自动布局、文件夹收纳、跨文件夹连线和定位。
- 完善节点添加、连线、批量选择、快捷键和移动端触控的真实浏览器验收。
- 控制工作台 bundle 体积和初次加载时间。

### 3. 图片生产质量

- 增加 provider 真实尺寸、格式和高级字段的合同测试。
- 建立商品形态保真、文字准确度和视觉统一性的评估样本。
- 改进交付图规格、裁切预览和批量下载。
- 完善生成失败、取消、重试和 provider note 的用户反馈。

### 4. 全局图库与工作流子图库

- `/media-library` 已提供全局素材列表、搜索、文件夹、标签、归档/恢复、批量组织和选择反馈。
- `WorkflowMediaLibraryAsset` 已把全局素材关联到工作流子图库；同一图片可被多个工作流使用，关联不复制媒体 bytes。
- ImageChat 的保存动作已切换到 canonical `/api/media-library/from-session`，`/gallery` 已重定向到 `/media-library`；旧表、旧 API 和历史 DTO 的迁移审计与 owner 退休仍待完成。
- Agent 图库整理已接入有界读取、可确认 Draft、revision 校验、幂等确认、原子应用和结果投影；剩余工作是旧 Gallery 对账/owner 退休，以及跨工作流同步等更高范围的 Agent 写操作。

### 5. 全局 Agent 与人工工作流协作

- 保留工作流画布的直接编辑、整图运行、单节点运行、取消、重试和运行记录入口；Agent 接入现有 `WorkflowRun`，不建立第二套执行器。
- 已实现 `AgentSession` 元数据、商品对话关联、列表/创建/改名/归档 API，以及工作台内按 Session 选择商品工作区的基础切换。
- 已实现独立 `AgentTask`、任务专属 harness run、本轮 `AgentTurn` 关联和页面上下文快照；Session 切换不取消后台任务，Task 目标不随路由变化。
- Global Agent Dock 已提供 Session/Task 列表、搜索、新建、归档、打开工作区、取消任务和全局素材整理 Draft 投影/确认；暂停/恢复、Task 摘要和统一调度器仍待实现。
- 按 Session 摘要、Task 目标、最近 Turn、当前页面上下文和执行前 Fresh Observation 分层组装上下文；完整 Agent journal 继续保留，模型工作上下文按 harness 规则压缩。
- 已交付商品工作区只读 WorkflowRun 监控工具；执行前 Fresh Observation、跨商品/跨工作流/全局图库范围和有副作用操作仍待实现。

落地策略和当前缺口见 `specs/global-agent-human-workflow-design.md`。

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
