# 任务：会话生图商家隔离（B5）

状态：完成
类型：实现
认领者：sub-merchant/merchant-image-session
认领于：2026-09-07T13:45:00+08:00
完成于：2026-09-07T13:50:00+08:00
业务组：商家平台
父账本：merchant-platform.md
完成后可拆：与 B4 并行收束后进 B6；完整隔离门未过不开放第二商家

按 [Issue 协议](../README.md) 认领。承接 [Graph/配方 B3](merchant-graph-recipe.md) 与覆盖矩阵 **B5**。协调者审核：2026-09-07 CTO 通过——imagesession 隔离测复跑；跨商 download/attach/retry 404；未宣称 MP-B。

## 问题来源

矩阵 F\*：image session generate、SSE、attach 仍可能跨商 attach/download/retry。

## 做成什么样

本商 generate/SSE/attach 成功；跨商 attach/download/retry 拒绝（404）。仍不开放第二商；不宣称 MP-B。

## 前置与并行

- 前置：B3 已归档。
- 排他写入：`go/internal/imagesession/` 及相关测试、父章程 B5、本文件。可与 `merchant-library-binding` 并行若写集不交。

## 只改这些文件

实际交付：

- `go/internal/imagesession/service.go`：`AssetDownload` 经 session `ScopeMerchant`；`Attach` 商品加载 `ScopeMerchant`；`Reconcile` 入口 `loadSession`
- `go/internal/imagesession/merchant_image_session_test.go`：B5 正反测
- `docs/audits/merchant-platform.md`：B5 批次结论（待审）
- 本文件

未改：library 绑定、Skill/grader、delivery、发行脚本、看板 README、graph skipUnchanged/text_trace、facts 影响预览。

## 合同

- 矩阵 F\*；跨商 404；完成 ≠ MP-B。

## 阻塞与交接

- 原因：无。
- 解除条件：无。
- 跟进者：协调者（审核关闭后可与 B4 收束进 B6）。
- 交接：**实现与证据已就绪，保持认领等待维护者审核**；不 commit、不 push、不改看板 README、不宣称 MP-B。

## 证据

- 跨商策略：统一 **404**（`auth.CrossMerchantDetail` =「资源不存在」）。
- 实现要点：
  - 会话根继续 `loadSession` + `ScopeMerchant`（generate/retry/cancel/SSE/status 已覆盖）
  - F4 download：asset→session join 后 `ScopeMerchant`，缺失与跨商同文案
  - F5 attach：session 与 product 均 `ScopeMerchant`；跨商 attach 零写入
  - F2 reconcile：入口补 `loadSession`（与 retry 一致）
- 测试：
  - `bash scripts/with_dev_env.sh bash -lc 'go test -C go ./internal/imagesession/ -count=1 -p 1'` → ok
  - 正反：`TestMerchantImageSessionIsolation`（本商 generate/SSE/download/attach；他商 generate/SSE/retry/download/attach 及交叉 product → 404 `CrossMerchantDetail`；跨商 attach 零写入）
- 自审：第二商仍不开放注册；未改 library 绑定 / Skill / grader / delivery / 发行脚本；未宣称 MP-B / B10；未 commit。

- 审核者：CTO（本会话）；结论：通过。可认领 B4 图库绑定；仍不开放第二商。
