# 任务：图库绑定商家隔离（B4）

状态：完成
类型：实现
认领者：sub-merchant/merchant-library-binding
认领于：2026-09-07T13:50:00+08:00
完成于：2026-09-07T13:56:00+08:00
业务组：商家平台
父账本：merchant-platform.md
完成后可拆：B6 或与 B5 并行（写集不冲突时）；完整隔离门未过不开放第二商家

按 [Issue 协议](../README.md) 认领。承接 [Graph/配方 B3](merchant-graph-recipe.md) 与 [会话生图 B5](merchant-image-session.md) 与覆盖矩阵 **B4**。协调者审核：2026-09-07 CTO 通过——library 全包复测；跨商绑定/sync 404 零写入；未宣称 MP-B。

## 问题来源

矩阵 E\*：from-product/session、workflow sync 等图库绑定仍可能跨商绑定半写入。

## 做成什么样

同商绑定成功；**跨商绑定**拒绝且无半写入（对外 404）。仍不开放第二商；不宣称 MP-B。

## 前置与并行

- 前置：B3 已归档；与 B5 的依赖按父章程——认领前读批次依赖行。
- 排他写入：library/product 绑定路径及相关测试、父章程 B4、本文件。勿与 CF-B2 同写事实预览冲突文件。

## 只改这些文件

实际交付：

- `go/internal/library/store.go`：`loadSessionAsset` JOIN `image_sessions` + `ScopeMerchant`；`requireWorkflow` 先 `GraphGuard`/`Lock` 校验商品商家；`reloadInOrder` 商家过滤
- `go/internal/library/workflow.go`：`SyncWorkflow` / `listWorkflowTx` 素材加载 `ScopeMerchant`；跨商/缺失统一 `NotFoundCrossMerchant`
- `go/internal/library/save.go`：`SaveFromSession` 合同注释对齐
- `go/internal/library/merchant_library_binding_test.go`：B4 正反测
- `docs/audits/merchant-platform.md`：B4 批次结论（待审）
- 本文件

未改：imagesession 核心、Skill/grader、delivery、发行脚本、看板 README、facts 影响预览实现（并行 CF-B2）。

## 合同

- 矩阵 E\*；跨商 404；完成 ≠ MP-B。

## 阻塞与交接

- 原因：无。
- 解除条件：无。
- 跟进者：协调者（审核关闭后可拆 B6）。
- 交接：**实现与证据已就绪，保持认领等待维护者审核**；不 commit、不 push、不改看板 README、不宣称 MP-B。

## 证据

- 跨商策略：统一 **404**（`auth.CrossMerchantDetail` =「资源不存在」）。
- 实现要点：
  - E3 from-product：继续 `product.LoadImageForUpdate` + `ScopeMerchant`（B2）
  - E3 from-session：`loadSessionAsset` JOIN session 后 `ScopeMerchant`；缺失/跨商同文案
  - E5 workflow list/sync/remove：`requireWorkflow` 先校验 product 商家；sync 素材 ID `ScopeMerchant`；跨商零半写入（事务回滚）
  - E4 get/download：既有 `loadAsset` 商家过滤；测试覆盖跨商 get/download
- 测试（验证时临时避开并行 CF-B2 未完成 `product/fact_impact*` 以免编译失败）：
  - `bash scripts/with_dev_env.sh bash -lc 'go test -C go ./internal/library/ -count=1 -p 1'` → ok
  - `bash scripts/with_dev_env.sh bash -lc 'go test -C go ./internal/product/ -count=1 -p 1 -run "TestMerchantProductChainIsolation|TestCoverAddImagesAndDelete|TestGalleryQuery"'` → ok
  - 正反：`TestMerchantLibraryBindingIsolation`（本商 from-product/session/sync/list/get；他商 from-product/session/list/sync、本商 sync 他商素材、get/download/collect → 404 `CrossMerchantDetail`；跨商零写入 library/workflow_links/product_assets）
- 自审：第二商仍不开放注册；未改 imagesession 核心 / Skill / grader / delivery / 发行脚本；未宣称 MP-B / B10；未 commit。
- 审核者：CTO（本会话）；结论：通过。可拆 B6 交付与局部编辑隔离；仍不开放第二商。
