# 任务：Graph 与配方商家隔离（B3）

状态：完成
类型：实现
认领者：sub-merchant/merchant-graph-recipe
认领于：2026-09-07T13:36:00+08:00
完成于：2026-09-07T13:45:00+08:00
业务组：商家平台
父账本：merchant-platform.md
完成后可拆：B4 图库绑定或 B5 会话生图（写集不冲突时并行）；完整隔离门未过不开放第二商家

按 [Issue 协议](../README.md) 认领。承接 [商品链 B2](merchant-product-chain.md) 与覆盖矩阵 **B3**。协调者审核：2026-09-07 CTO 通过——graph/recipe 隔离测复跑；跨商 changeset/run/SSE/apply 404；未宣称 MP-B。

## 问题来源

B2 已隔离商品子链下载/ZIP/workspace。Graph changeset/run/SSE 与 recipe apply 仍可能按裸 workflow/recipe/run id 跨商读写或重放游标。

## 做成什么样

矩阵 C\*、D\*：本商 run/SSE/apply 成功；交叉 workflow/recipe/run 与 SSE 游标拒绝（对外 **404**，与 B1/B2 一致）。仍不开放第二商；不宣称 MP-B。

## 前置与并行

- 前置：B2 已归档。
- 排他写入：`go/internal/graph/`、`go/internal/recipe/`（apply/SSE/run 路径）及相关测试、父章程 B3、本文件。CF-B1 已归档；勿与 `compete-facts-impact-preview` 同写冲突的 digest/预览文件——认领前核占用；本批优先商家过滤 HTTP/SSE/apply。
- 不改 Skill/grader、delivery 采用快照、发行脚本。

## 只改这些文件

实际交付：

- `go/internal/graph/guard.go`：`ProductGuard.Require`；`requireOwnedProduct`（有工作商家时校验，worker 无商家上下文跳过）
- `go/internal/graph/store.go`：`loadGraph` / `loadGraphForUpdate` / `loadActiveGraph` / `loadActiveGraphForUpdate` 入口 Require
- `go/internal/graph/runs.go`：`loadGraphRunStatus`（SSE）入口 Require
- `go/internal/graph/service.go`：`ListRuns` / `GetRun` / `GetRunStatus` / `GetRunForProduct` 挂 `guardCtx`
- `go/internal/product/collect.go`：`GraphGuard.Require` → `loadProduct`（`ScopeMerchant` + `NotFoundCrossMerchant`）
- `go/internal/recipe/service.go`：`Create` / `Append` 挂 ProductGuard + `getProductTarget`
- `go/internal/graph/command_test.go`、`sources_test.go`：测试守卫补 `Require`
- `go/internal/graph/merchant_graph_recipe_test.go`、`go/internal/recipe/merchant_graph_recipe_test.go`：B3 正反测
- `docs/audits/merchant-platform.md`：B3 批次结论
- 本文件

未改：Skill/grader、delivery 采用快照、发行脚本、看板 README、digest/`skipUnchanged`/text_trace。

## 合同

- 矩阵 C\*/D\*；跨商 404；完成 ≠ MP-B。

## 阻塞与交接

- 原因：无。
- 解除条件：无。
- 跟进者：协调者（审核关闭后可拆 B4/B5）。
- 交接：**实现与证据已就绪，保持认领等待维护者审核**；不 commit、不 push、不改看板 README、不宣称 MP-B。

## 证据

- 跨商策略：统一 **404**（`auth.CrossMerchantDetail` =「资源不存在」）；本商 URL 下他商 workflow/run id 仍 404（文案可为「商品工作流不存在」/「工作流运行不存在」）。
- 实现要点：
  - Graph 经 product→merchant：`Require` 在 load/SSE 路径强制；无商家上下文（worker/recovery）不拦主键加载
  - Recipe list/get/preview/apply 继续 `ScopeMerchant`；Create/Append 补 product 归属
  - SSE：`GetRunStatus` 跨商即拒，游标重放无法建立
- 测试：
  - `bash scripts/with_dev_env.sh bash -lc 'go test -C go ./internal/graph/ ./internal/recipe/ -count=1 -p 1'` → ok
  - 正反：`TestMerchantGraphIsolation`（本商 changeset/run/SSE；他商 product/workflow/run/SSE → 404）
  - 正反：`TestMerchantRecipeIsolation`（本商 list/save/preview/apply；交叉 recipe/product → 404；跨商 apply 零写入）
- 自审：第二商仍不开放注册；未改 delivery 采用快照 / Skill / grader / 发行脚本；未宣称 MP-B / B10；未 commit。
- 审核者：CTO（本会话）；结论：通过。可拆 B4 图库绑定 / B5 会话生图；仍不开放第二商。
