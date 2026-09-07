# 任务：交付与局部编辑商家隔离（B6）

状态：完成
类型：实现
认领者：sub-merchant/merchant-delivery-localedit
认领于：2026-09-07T13:56:00+08:00
完成于：2026-09-07T14:10:00+08:00
业务组：商家平台
父账本：merchant-platform.md
完成后可拆：B7 Agent 工具链；完整隔离门未过不开放第二商家

按 [Issue 协议](../README.md) 认领。承接 [图库绑定 B4](merchant-library-binding.md) 与覆盖矩阵 **B6**。协调者审核：2026-09-07 CTO 通过——delivery/localedit 全包复测；跨商 404；未宣称 MP-B。

## 问题来源

矩阵 G\*：delivery job/export/adopt 与 localedit task/source 仍可能跨商读写。

## 做成什么样

本商 job/export/adopt 与局部编辑成功；跨商 job/task/source 拒绝（404）。仍不开放第二商；不宣称 MP-B。

## 前置与并行

- 前置：B4 已归档。
- 排他写入：`go/internal/delivery/`、`go/internal/localedit/` 及相关测试、父章程 B6、本文件。勿与 CF-B2 同写事实预览冲突文件。
- 不改 Skill/grader、发行脚本。

## 只改这些文件

实际交付：

- `go/internal/delivery/service.go`：`requireProduct`；`loadJob`/`loadJobForUpdate` 校验商品商家；Submit/List 保留 `LoadAssetRow` 跨商文案
- `go/internal/delivery/export.go` / `adoption.go`：产品加载改走 `requireProduct`；导出保留跨商 job 404
- `go/internal/delivery/merchant_delivery_localedit_test.go`：B6 delivery 正反测
- `go/internal/delivery/export_archive_test.go`：缺失 job 接受 CrossMerchantDetail
- `go/internal/localedit/store.go`：`requireProduct`/`lockProduct` ScopeMerchant；`loadTask*` 先校验商家；`lockSource` 经 `LoadAssetRow`
- `go/internal/localedit/service.go`：List 商家过滤
- `go/internal/localedit/merchant_delivery_localedit_test.go`：B6 localedit 正反测
- `docs/audits/merchant-platform.md`：B6 批次结论（待审）
- 本文件

未改：Skill/grader、发行脚本、看板 README、CF-B2 facts 预览、release 脚本。

## 合同

- 矩阵 G\*；跨商 404；完成 ≠ MP-B。

## 阻塞与交接

- 原因：无。
- 解除条件：无。
- 跟进者：协调者（审核关闭后可拆 B7）。
- 交接：**实现与证据已就绪，保持认领等待维护者审核**；不 commit、不 push、不改看板 README、不宣称 MP-B。

## 证据

- 跨商策略：统一 **404**（`auth.CrossMerchantDetail` =「资源不存在」）。
- 实现要点：
  - G1 job get/retry：`loadJob*` → `requireProduct`（ScopeMerchant）
  - G1 create/list：`product.LoadAssetRow` 已商家过滤；不再改写 CrossMerchantDetail
  - G2 export / G adoption：`requireProduct`；跨商 job id → CrossMerchantDetail；CreateAdoption 继续复用 `product.Lock`
  - G4 localedit：`lockProduct`/`requireProduct`/`loadTask*`；跨商 source 经 `LoadAssetRow`
- 测试：
  - `bash scripts/with_dev_env.sh bash -lc 'go test -C go ./internal/delivery/ ./internal/localedit/ -count=1 -p 1'` → ok
  - 正反：`TestMerchantDeliveryIsolation`、`TestMerchantLocalEditIsolation`
- 自审：第二商仍不开放注册；未改 Skill/grader/发行/看板 README；未宣称 MP-B / B10；未 commit。

- 审核者：CTO（本会话）；结论：通过。可拆 B7 Agent 工具链；仍不开放第二商。
