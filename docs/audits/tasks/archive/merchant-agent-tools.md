# 任务：Agent 工具链商家隔离（B7）

状态：完成
类型：实现
认领者：sub-merchant/merchant-agent-tools
认领于：2026-09-07T14:10:00+08:00
完成于：2026-09-07T14:35:00+08:00
业务组：商家平台
父账本：merchant-platform.md
完成后可拆：B8 队列与前端；完整隔离门未过不开放第二商家

按 [Issue 协议](../README.md) 认领。承接 [交付/局部编辑 B6](merchant-delivery-localedit.md) 与覆盖矩阵 **B7**。协调者审核：2026-09-07 CTO 通过——`TestMerchantAgentToolsIsolation` 与 agent-service contracts 复测；跨商 404；未宣称 MP-B / B10。

## 问题来源

矩阵 H\*、I\*：浏览器+internal+Pi scope 商家字段与工具越权仍可能跨商读写或伪造 scope。

## 做成什么样

本商 turn/tool 成功；伪造 internal scope、跨商 content、确认在撤销后拒绝（404/统一策略）。25 工具越权 harness 覆盖关键路径。仍不开放第二商；不宣称 MP-B。不改 grader 边界。

## 前置与并行

- 前置：B6 已归档。
- 排他写入：`go/internal/agent/` 及相关测试、父章程 B7、本文件。勿改 Skill/grader 评测题边界。
- 可与 CF-B3 并行若写集不交。

## 只改这些文件

实际交付：

- `go/internal/agent/merchant_scope.go`：conversation/task 合同商家绑定；浏览器 AbortIfNoMerchant；内部伪造商家头拒绝
- `go/internal/agent/conversations.go`：`loadConversation*` ScopeMerchant；跨商 404
- `go/internal/agent/tools_graph.go`：`loadScopedConversation` 绑定商家；Propose 补 ProductGuard
- `go/internal/agent/tools_assets.go` / `tools_workspace.go` / `workflow_requests.go`：工具路径带商家 ctx；`ListGlobalProducts` ScopeMerchant；确认要求 Membership
- `go/internal/agent/contract.go` / `dto.go`：Contract/RuntimeContext 含 `merchant_id`
- `go/internal/agent/http.go` / `http_internal.go`：浏览器 AbortIfNoMerchant；internal conversation/task 商家中间件
- `go/internal/agent/merchant_agent_tools_test.go`：B7 正反测 + 25 工具越权 harness
- `agent-service/src/contracts.ts` / `runtime-scope.ts` 及 Scope 夹具：Pi scope 含 `merchant_id`
- `docs/audits/merchant-platform.md`：B7 批次结论
- 本文件

未改：Skill/grader 评测题、看板 README、CF-B3 图位/catalog 夹具。

## 合同

- 矩阵 H\*/I\*；跨商 404；完成 ≠ MP-B / ≠ B10。

## 阻塞与交接

- 原因：无。
- 解除条件：无。
- 跟进者：无。
- 交接：已归档；交付随本任务提交。

## 证据

- 跨商策略：统一 **404**（`auth.CrossMerchantDetail`）；伪造商家声明头在无成员时 403/401；撤销 Membership 后确认 403。
- 实现要点：
  - H\*：浏览器 Agent 路由 `abortIfNoMerchant`；conversation 根加载 ScopeMerchant
  - I1/I2：internal conversation/task 中间件以合同行 `merchant_id` 为权威；Contract/RuntimeContext/Pi Scope 含商家字段
  - I7/I9/I10：工具路径绑定商家后走 product/library ScopeMerchant；`list_products_v1` 过滤；content/inspect 跨商拒
  - 确认撤销：`ConfirmWorkflowRunRequest` / `ConfirmLibraryDraftHTTP` + HTTP 门禁
  - B3 `LoadGraph` 需 ProductGuard：`requireRunnableWorkflow` / Propose 工具挂 guard
- 测试（协调者复测 2026-09-07）：
  - `bash scripts/with_dev_env.sh bash -lc 'go test -C go ./internal/agent/ -count=1 -p 1 -run TestMerchantAgentToolsIsolation'` → ok
  - `pnpm --dir agent-service exec vitest run src/contracts.test.ts` → 3 passed
  - 交付方另记：同包核心确认/恢复测 ok；`TestEvalObservationFixtures` catalog 漂移非本批、未改 grader
- 自审：第二商仍不开放注册；未改 Skill/grader；未宣称 MP-B / B10
- 交付定位：随本任务提交（用 `git log --follow -- docs/audits/tasks/archive/merchant-agent-tools.md` 查询）

- 审核者：CTO（本会话）；结论：通过。可拆 B8 队列与前端。
