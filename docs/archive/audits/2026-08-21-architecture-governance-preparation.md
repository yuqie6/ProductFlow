# 架构治理准备记录

## 记录范围

本记录用于说明 ProductFlow 回到可治理代码基线的执行结果、数据库状态、在线 owner 和后续验收门槛。v3 设计保留为目标态，未推送的 v3 实现已按用户决策删除。

记录日期：2026-08-21

## Git 基线

回退前工作区保持干净，`main` 相对 `origin/main` ahead 150 个提交。用户确认未推送代码不需要保留后，代码先回到 `e6cf9e66aaddc4ede13dded37588b41c0696b062`。当前活动分支为 `codex/development`，指向该治理基线；本地 `main` 已回到 `origin/main` 的 `649a8a52f26a39327dac734b5cd0bcb8c9b2f1b0`。

准备期间建立的两个临时保护引用已经删除：

| 引用 | 指向 | 用途 |
| --- | --- | --- |
| `archive/pre-architecture-governance-20260821` | 原 HEAD `38ae4c3f71a0523522fe07cf099753641bfa343b` | 已删除，不保留当前 v3 代码 |
| `architecture-governance-e6` | `e6cf9e66aaddc4ede13dded37588b41c0696b062` | 已删除，当前 `codex/development` 指向同一提交 |

`e6cf9e66` 本身只包含协作配置和 `AGENTS.md` 变更。它后面的代码提交为：

1. `70939a1a`：schema-v3 图、ChangeSet、提案、持久运行和 `0071` 到 `0074` 数据迁移。
2. `4fa746ac`：v3 工作台图编辑 scaffold 和前端图投影。
3. `38ae4c3f`：自由画布重建、素材库适配和验收边界文档。

从 `e6cf9e66` 到被删除 checkout 的差异为 71 个文件，约 `+10324/-1468`。当前代码树已移除这组代码差异，只保留 ADR、Roadmap 和 rollout 中的设计内容。

## 当前代码基线

治理阶段的当前代码基线为 `e6cf9e66`。基线阶段继续使用 e6 已有的 Agent、Session、Task、WorkflowDraft、schema-v2 Workflow 和 schema-v2 Run 业务链路。当前分支不保留 v3 graph 实现。

治理阶段不新增 V2 功能。V2 只承担当前基线的稳定性验证和现有商家工作流，v3 设计进入目标态管理。

## 当前与目标 owner

| 概念 | 治理阶段在线 owner | v3 目标 owner | 约束 |
| --- | --- | --- | --- |
| 商品创建 | Agent product workspace、Session、Task、Conversation | 保持 | Agent 负责收集事实和工作目标 |
| 初始方案 | `WorkflowDraft` | `WorkflowDraft` 转 v3 初始图变更 | 用户确认前不写入可执行图 |
| 在线工作流 | e6 的 schema-v2 `ProductWorkflow` | schema-v3 `ProductWorkflow` | 同一环境只能有一个在线图权威 |
| 图编辑 | V2 canvas mutation | Graph Command + ChangeSet + operation group | 节点、边、分组变更必须有持久命令记录 |
| 执行 | e6 的 V2 run | v3 `WorkflowRun` revision snapshot | Agent、页面和 worker 使用同一执行合同 |
| Agent 提案 | Agent conversation 和 WorkflowDraft | GraphProposal 转 ChangeSet | Agent 提案不能绕过用户确认直接改图 |
| 全局素材 | 现有 media library 业务路径 | `MediaLibraryAsset` | 全局素材和商品绑定分开管理 |
| 商品素材绑定 | 现有 `ProductImageAsset` 路径 | `ProductImageAsset` + workflow association | 绑定素材和连入 reference edge 是不同动作 |
| 运行时 | Node/Pi Agent | 保持 | Pi 不直接拥有业务数据库事务 |
| 业务持久化 | PostgreSQL、worker、业务 API | 保持 | runtime journal 不取代业务投影 |

## 回退前已确认的冲突

被删除 checkout 的在线链路存在 v2/v3 owner 分裂：

- `workflow_drafts/materialization.py` 仍按 `schema_version == 2` 查询，并创建 `schema_version=2` 的 `ProductWorkflow`。
- `20260820_0071` 已把工作流表约束切换到 schema-v3，并清理 pre-v3 工作流数据。
- Agent workbench bootstrap 仍使用 `AgentV2WorkbenchBootstrap` 和 `/api/v2/products/.../agent-workbench`。
- Agent workflow run request 仍导入和调用 `submit_v2_workflow_run`、`cancel_v2_workflow_run`、`retry_v2_workflow_run`。
- 前端产品工作台已经加载 v3 graph canvas，但 bootstrap、Agent conversation 和部分素材查询仍使用 V2 或旧 Gallery 合同。

这组冲突属于在线合同断裂。回到 e6 后，当前代码树重新使用 V2 单一基线；治理阶段不添加兼容 fallback，也不恢复被删除的混合实现。

## 数据库前置条件

当前代码迁移头为 `20260820_0070`。本地 `alembic current` 无法定位数据库中的 `20260518_0032`，因此本地数据库当前不能作为基线验证依据。`docker compose` 也因缺少 `AGENT_SERVICE_INTERNAL_TOKEN` 未能启动，当前没有运行中的本地服务。

被删除 checkout 中的 `20260820_0071` 会删除旧工作流数据、运行记录、节点、边、物化记录和素材关联，并且不支持 downgrade。该迁移已经离开当前代码树。进入基线运行验证前必须完成：

1. 盘点本地、共享环境和可能的部署数据库各自的 Alembic revision。
2. 确认是否有数据库应用过已经删除的 `20260820_0071` 及之后迁移。
3. 对已进入 v3 schema 的数据库选择备份恢复或独立新库，不运行已删除迁移的 downgrade。
4. 不复用 revision 不明的数据库做治理基线验收。

## 治理阶段验收状态

`codex/development@e6cf9e66` 已完成回退后的代码基线验证：

- 后端全量 pytest：`548 passed, 34 skipped`。
- 后端 Ruff：通过。
- Agent service：8 个测试文件、49 个测试通过，TypeScript build 通过。
- Web：57 个测试文件、286 个测试通过，lint 和 production build 通过。
- 文档合同检查：通过。
- v3 graph、run、API、前端工作台和 `0071-0074` 残留扫描：无结果。

仍需在可用的独立数据库和服务环境完成：

- PostgreSQL、Redis、worker、Node/Pi Agent 按 e6 配置启动。
- 浏览器完成商品创建、Agent 追问、WorkflowDraft 确认、V2 工作流物化、编辑、运行和结果保存。
- 真实 provider、SSE 重连、任务恢复和副作用对账门禁。

## v3 重启准入

治理阶段完成后，v3 以一条完整业务切片重新进入实现：

```text
商品事实与图片目标
    -> Agent conversation
    -> WorkflowDraft
    -> 用户确认
    -> v3 initial graph adapter
    -> Graph ChangeSet
    -> schema-v3 ProductWorkflow
    -> v3 WorkflowRun
    -> Artifact / ProductImageAsset / MediaLibrary
```

v3 进入在线代码前必须具备：

- Draft 确认到 v3 初始图的单一应用服务和幂等合同。
- 工作台 bootstrap、图查询、Agent 提案、人工编辑使用同一 graph projection。
- Agent run request、页面运行控制和 worker 使用同一 v3 run 合同。
- 全局素材、商品素材绑定、工作流素材关联和 reference edge 的边界测试。
- PostgreSQL、Redis、worker、真实 provider、SSE 重连和浏览器场景证据。

V2 退役应放在这些准入条件之后，并按 reader、writer、API、测试、迁移和文档逐项清理。保留 legacy archive 和数据迁移读取器，直到部署、备份和观察窗口完成。

## 明确不做

- 不恢复已经删除的 v2/v3 混合 checkout。
- 不保留两个在线 workflow executor。
- 不用 V2 fallback 解决 v3 数据库迁移造成的合同冲突。
- 不在数据库状态不明时执行 Alembic 降级或数据清理。
- 不为治理阶段增加 scheduler、repair job、cleanup scanner 或泛化一致性层。
- 不把 v3 scaffold、focused tests 或旧浏览器记录当作端到端交付证据。
