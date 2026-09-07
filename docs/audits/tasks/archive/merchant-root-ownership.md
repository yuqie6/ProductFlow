# 任务：业务根对象写入 merchant_id 并强制查询归属

状态：完成
类型：实现
认领者：sub-merchant/merchant-root-ownership
认领于：2026-09-07T12:31:30+08:00
完成于：2026-09-07T13:00:00+08:00
业务组：商家平台
父账本：merchant-platform.md
完成后可拆：B2 商品链隔离或按矩阵并行无写集冲突的切片；完整隔离门未过不开放第二商家

按 [Issue 协议](../README.md) 认领。承接 [身份骨架 B0](merchant-identity-skeleton.md) 与覆盖矩阵 **B1**。协调者审核：2026-09-07 CTO 通过——根表 merchant_id、ScopeMerchant、跨商 404 与隔离测成立；未宣称 MP-B。

## 问题来源

B0 已有 User/Merchant/Membership，业务根表仍无 `merchant_id`。矩阵要求 products、image_sessions、media_library_*、workflow_recipes、visual_systems、agent_sessions/tasks/conversations 写商家归属并回填到唯一开发商家。

## 做成什么样

1. Schema：上述根表增加 `merchant_id`（CreateTable/AddColumn + ExtraDDL），回填到 B0 唯一商家；查询/唯一键含商家。
2. 读写路径按当前会话商家过滤；裸 UUID 他商夹具拒绝（404/403 全站统一，B1 钉死一种）。
3. **仍不开放**第二互不信任商家注册；不恢复 admin 布尔回退。
4. 不实现完整 Agent 工具链商家字段（B7）或额度（MP-C）。

## 前置与并行

- 前置：B0 已归档。
- 运行资源：隔离测试 DB；migrate 回归。
- 排他写入：schema 业务根表、对应 store/HTTP、测试、父章程 B1 行、本文件。
- **并行**：若 `compete-facts-layer-gate` 或 `delivery-adoption-snapshot` 仍占用 `product`/`delivery`/共享 schema 文件，须等其关闭或由协调者切分写集后再认领。

## 只改这些文件

实际交付：

- `go/internal/platform/db/schema/models.go`：根表 `merchant_id` 列
- `go/internal/platform/db/schema/constraints.go`：回填、NOT NULL、FK、商家维唯一键、索引、BEFORE INSERT 填唯一商家触发器
- `go/internal/platform/db/schema/migrate_test.go`：迁移夹具含商家
- `go/internal/auth/merchant.go`、`testutil.go`：工作商家解析、`ScopeMerchant`、跨商统一 404、`ResolveMerchantID`
- `go/cmd/productflow-api/main.go`：挂载 `AttachWorkingMerchant`
- `go/internal/product/`、`library/`、`imagesession/`、`recipe/`、`agent/`：根读写过滤与写入归属
- 相关测试与 `docs/audits/merchant-platform.md`、本文件

未改：`delivery` 采用快照实现、发行脚本、看板 README。

## 不要碰

B2+ 全链路下载/SSE、开放第二商、Skill/grader。

## 合同

- MP-01..03 根归属；中间版本仍单商。
- 完成 ≠ MP-B。
- **跨商对外码钉死 404**（`auth.CrossMerchantDetail` =「资源不存在」），防枚举；全站根加载一致。

## 阻塞与交接

- 原因：无。
- 解除条件：无。
- 跟进者：协调者（审核关闭后可拆 B2 等）。
- 交接：**实现与证据已就绪，保持认领等待维护者审核**；不 commit、不 push、不改看板 README、不宣称 MP-B。

## 证据

- 跨商策略：统一 **404**。
- 迁移：AddColumn `merchant_id` → ExtraDDL 回填唯一商家 → SET NOT NULL → FK；唯一键改为含 `merchant_id`（folders/tags/upload_keys/collection_keys/assets source、products creation key、recipes official_key）；`productflow_fill_merchant_id` 触发器在写入缺省时填唯一商家（单商 B1）。
- 复用 CF-B0：未回退 `applyFactLayerGate`；未改 delivery 采用快照路径。
- 测试：
  - `bash scripts/with_dev_env.sh bash -lc 'go test -C go ./internal/auth/ ./internal/platform/db/schema/ ./internal/product/ ./internal/library/ ./internal/imagesession/ ./internal/recipe/ ./internal/agent/ ./internal/graph/ ./cmd/productflow-api/ -count=1 -p 1'` → 主路径 ok；`TestImageCheckpointTransactionBoundaries` / `TestConcurrentExpiredRecoverySkipLockedDoesNotDoubleTerminate` 偶发竞态，`-count=2` 复跑 ok
  - 正反测：`TestMerchantRootOwnershipProductIsolation`（本商 list/get；他商 UUID → 404）
- 自审：第二商仍不开放注册；admin 布尔未恢复；未改 delivery 采用快照 / 发行脚本；CF-B0 `applyFactLayerGate` 未回退；B7 Agent 工具链商家字段与下载/SSE 全链未宣称完成。

- 审核者：CTO（本会话）；结论：通过。可拆 B2 商品链；仍不开放第二商。
