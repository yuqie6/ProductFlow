# Engineering Knowledge Migration Audit

基线：`9d1f4fa`，2026-08-16。本文记录项目级工作流工具移除时，原 21 份工程 spec 的处置证据。它用于检查迁移完整性，不承担长期规范职责；长期规则以 `AGENTS.md`、`backend/AGENTS.md`、`web/AGENTS.md`、`CONTEXT.md`、ADR 和当前架构文档为准。

## 判定规则

- **吸收**：合同仍与当前代码一致，迁入明确的长期所有者。
- **纠正后吸收**：原则有效，但旧路径、所有权或语义已漂移，以当前调用链重写。
- **淘汰**：只服务旧工作流工具、已删除运行时或无当前实现证据，不迁入长期文档。
- 每项必须包含代码或测试锚点；只有文档互相引用不算实现证据。

## 后端

| 原 spec | 处置 | 长期归宿 | 当前代码/测试证据 |
|---|---|---|---|
| `backend/index` | 吸收 | `backend/AGENTS.md` 的阅读路径和验证 | `backend/pyproject.toml`, `backend/tests/` |
| `backend/directory-structure` | 纠正后吸收 | `ARCHITECTURE.md`, `backend/AGENTS.md` | `presentation/api.py`, `application/`, `domain/`, `infrastructure/`, `workers.py` |
| `backend/database-guidelines` | 纠正后吸收 | `backend/AGENTS.md`, ADR 0001-0004 | `db/models.py`, `workflow_drafts/materialization.py`, `gallery_mutations.py`, migration tests |
| `backend/error-handling` | 吸收 | `backend/AGENTS.md` | `domain/errors.py`, `presentation/errors.py`, `test_error_handling.py` |
| `backend/logging-guidelines` | 压缩吸收 | `backend/AGENTS.md`, `ARCHITECTURE.md` | `infrastructure/logging.py`, `presentation/api.py`, `workers.py`, `test_logging_behavior.py` |
| `backend/quality-guidelines` | 吸收 | 根和后端 `AGENTS.md` | `justfile`, backend/Pi test suites |
| `backend/product-gallery-explorer` | 纠正后吸收 | `CONTEXT.md`, `ARCHITECTURE.md`, `backend/AGENTS.md` | `gallery_assets.py`, `gallery_mutations.py`, `gallery_archives.py`, `test_product_gallery_explorer.py` |
| `backend/product-workflow-dag` | 吸收 | `CONTEXT.md`, ADR 0003, `ARCHITECTURE.md`, `backend/AGENTS.md` | `domain/workflow_rules.py`, `product_workflow/v2_*.py`, workflow tests |
| `backend/workflow-agent-service` | 吸收 | ADR 0001, `ARCHITECTURE.md`, `backend/AGENTS.md` | `agent_*` application modules, `agent-service/`, `test_workflow_agent_service.py` |

### 后端纠正项

- Session 生命周期与事务所有权已拆开。FastAPI dependency/worker 负责创建和关闭；当前 public application command 经常负责一次业务 commit/rollback，`stage_*` 等内部 helper 只 flush。旧 spec 的“创建 Session 者必然提交”与实际代码不符。
- 商品图片库所有权由 `gallery_assets.py`、`gallery_mutations.py`、`gallery_archives.py` 和 `media_assets.py` 承担；旧顶层 Gallery 的迁移读取由 `legacy_retirement/media_library.py` 承担。
- 日志 spec 中针对旧任务系统的模板段落不迁移；保留 request/worker context、敏感信息边界和 durable state 优先原则。
- “禁止指标系统”不是长期产品合同，只保留“相关改动不得顺带引入另一套观测框架”的范围约束。

## 前端

| 原 spec | 处置 | 长期归宿 | 当前代码/测试证据 |
|---|---|---|---|
| `frontend/index` | 吸收 | `web/AGENTS.md` | `package.json`, Vitest/ESLint/build |
| `frontend/directory-structure` | 纠正后吸收 | `ARCHITECTURE.md`, `web/AGENTS.md` | `App.tsx`, `pages/`, `components/`, `lib/` |
| `frontend/component-guidelines` | 压缩吸收 | `web/AGENTS.md` | workbench shell, node card, inspector, dialog and explorer components |
| `frontend/hook-guidelines` | 吸收 | `web/AGENTS.md` | `useAgentConversation.ts`, `useAgentTurnEvents.ts`, `useV2NodeDraftAutosave.ts`, `useProductImageExplorer.ts` |
| `frontend/state-management` | 吸收 | `web/AGENTS.md` | TanStack Query owners, canvas/local preference parsers, reducer tests |
| `frontend/type-safety` | 吸收 | `web/AGENTS.md` | `lib/types.ts`, `lib/api.ts`, `generationSpec.ts`, API/parser tests |
| `frontend/quality-guidelines` | 吸收 | `web/AGENTS.md` | frontend gates and browser acceptance requirements |
| `frontend/product-image-explorer` | 吸收 | `CONTEXT.md`, `ARCHITECTURE.md`, `web/AGENTS.md` | `pages/product-detail/image-explorer/`, explorer/API tests |
| `frontend/product-workbench-dag` | 纠正后吸收 | ADR 0001/0003, `ARCHITECTURE.md`, `web/AGENTS.md` | `pages/agent-workbench/`, `pages/product-workflow-v2/`, current component tests |

### 前端纠正项

- 当前路由以 `App.tsx` 为准，包含 `/history` 和 `/history/:archiveKind/:archiveId`；旧文档遗漏的历史入口已补入架构和产品文档。
- 工作台所有权按当前三个 feature 目录重建，不继承已删除页面和大组件的名称。
- 精确 query key、localStorage key 和像素断点属于实现细节；长期规范保留 identity、解析、窄失效和真实容器宽度原则，具体值由代码和测试负责。
- V1 字样可能是 wire schema 或 archive 值，不等同于在线 workflow schema；前端不得因此恢复旧编辑器或转换器。

## 共享思考指南

| 原 spec | 处置 | 长期归宿 | 当前证据 |
|---|---|---|---|
| `guides/code-reuse-thinking-guide` | 压缩吸收 | 根 `AGENTS.md` | 当前 helper/component ownership 与 residual scan 工作法 |
| `guides/cross-layer-thinking-guide` | 吸收 | 根 `AGENTS.md` | DTO/API/DB/frontend compatibility tests and migration read paths |
| `guides/index` | 淘汰 | 无 | 仅提供旧 spec 路由和工作流说明 |

## 未迁移内容

- 旧工作流工具的 task/session/journal、阶段命令、spec 索引和模板写作流程。
- 已删除 V1 editor/executor/default DAG/template catalog 的实现说明。
- 只描述旧文件名、旧页面组合或旧 query key 且没有当前代码对应物的内容。
- 与代码无关的固定汇报模板、重复 checklist 和示例性“wrong/correct”脚手架。

## 完成条件

- 21 份原 spec 均有处置记录。
- 长期规则只落在职责明确的当前文档中。
- `ARCHITECTURE.md` 的代码路径全部存在，路由与 `App.tsx` 一致。
- package `AGENTS.md` 覆盖事务、错误、日志、durability、DTO/state/query、画布、图库和验证边界。
- 文档链接检查、残留扫描和相关测试通过。
