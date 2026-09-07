# 任务：商品链商家隔离（B2）

状态：完成
类型：实现
认领者：sub-merchant/merchant-product-chain
认领于：2026-09-07T13:15:00+08:00
完成于：2026-09-07T13:22:00+08:00
业务组：商家平台
父账本：merchant-platform.md
完成后可拆：B3 Graph/配方或 B4 图库绑定（写集不冲突时由协调者并行）；完整隔离门未过不开放第二商家

按 [Issue 协议](../README.md) 认领。承接 [根归属 B1](merchant-root-ownership.md) 与覆盖矩阵 **B2**。协调者审核：2026-09-07 CTO 通过——product/auth 复测 ok；跨商直链/workspace/from-recipe 404；未宣称 MP-B。

## 问题来源

B1 已写根表 `merchant_id` 并过滤根查询。商品子链（事实、图库绑定、封面、直链下载/ZIP、workspace intake、from-recipe）仍可能按裸 UUID 跨商读到或混装导出。

## 做成什么样

矩阵 B\* 路径：本商读写/下载成功；跨商 product/fact/folder/asset id、ZIP 混装拒绝（对外码继续 **404**，与 B1 一致）。仍不开放第二商注册；不宣称 MP-B。

## 前置与并行

- 前置：B1 已归档。
- 排他写入：`go/internal/product/` 商品链（facts/gallery/cover/download/ZIP/intake/from-recipe）及相关测试、父章程 B2、本文件。勿与 CF-B1 同时改同一产物元数据文件——认领前核占用。
- 不改 Skill/grader；不改 delivery 采用快照合同；不改发行脚本。

## 只改这些文件

实际交付：

- `go/internal/product/store.go`：`loadAsset` JOIN products + `ScopeMerchant`；conversation 按 `merchant_id` 过滤；跨商/缺失统一 `NotFoundCrossMerchant`
- `go/internal/product/collect.go`：`LoadImageForUpdate` 同样 JOIN + 商家过滤
- `go/internal/product/recipe_create.go`：配方创建幂等回放按商家过滤 `creation_idempotency_key`
- `go/internal/product/app.go`：下载/GetAsset 注释与合同对齐
- `go/internal/product/merchant_product_chain_test.go`：B2 正反测
- `docs/audits/merchant-platform.md`：B2 批次结论
- 本文件

未改：共享根表 schema 迁移、delivery、web、发行脚本、看板 README。

## 合同

- 矩阵 B\*；跨商 404；完成 ≠ MP-B / ≠ B10。

## 阻塞与交接

- 原因：无。
- 解除条件：无。
- 跟进者：协调者（审核关闭后可拆 B3/B4）。
- 交接：**实现与证据已就绪，保持认领等待维护者审核**；不 commit、不 push、不改看板 README、不宣称 MP-B。

## 证据

- 跨商策略：统一 **404**（`auth.CrossMerchantDetail` =「资源不存在」）；ZIP 混装/本商路径下他商 folder id 对外仍 404（文案可为「商品图片不存在」/「商品图片文件夹不存在」，不泄枚举）。
- Schema：未改共享根表迁移；子表仍经 product/conversation→merchant 证明。
- 实现要点：
  - 直链 `GET .../product-image-assets/:id/download` 与删除经 `loadAsset`→products.merchant_id
  - facts/gallery/cover/ZIP 继续经 `loadProduct` 商家过滤；ZIP 仅打包 `product_id` 内资产
  - workspace get/intake 经 conversation.merchant_id；from-recipe 幂等键与 recipe 包 ScopeMerchant
- 测试：
  - `bash scripts/with_dev_env.sh bash -lc 'go test -C go ./internal/product/ ./internal/auth/ -count=1 -p 1'` → ok
  - `bash scripts/with_dev_env.sh bash -lc 'go test -C go ./internal/product/ -count=2 -p 1 -run "TestMerchantProductChainIsolation|TestMerchantRootOwnershipProductIsolation|TestCoverAddImagesAndDelete|TestRecipeCreationPreviewAndAtomicConfirmation|TestGalleryQuery"'` → ok
  - 正反：`TestMerchantProductChainIsolation`（本商 facts/gallery/cover/download/ZIP/workspace；他商 product/asset/conversation/recipe → 404；ZIP 混装 404）
  - 顺带：`./internal/library/` 相关 FromProduct/Collect/Merchant 冒烟 ok（`LoadImageForUpdate` 签名行为兼容）
- 自审：第二商仍不开放注册；未改 delivery 采用快照 / Skill / grader / 发行脚本；未宣称 MP-B / B10；未 commit。
- 审核者：待协调者。
