# 任务：业务根对象写入 merchant_id 并强制查询归属

状态：开放
类型：实现
认领者：—
认领于：—
业务组：商家平台
父账本：merchant-platform.md
完成后可拆：B2 商品链隔离或按矩阵并行无写集冲突的切片；完整隔离门未过不开放第二商家

按 [Issue 协议](README.md) 认领。承接 [身份骨架 B0](archive/merchant-identity-skeleton.md) 与覆盖矩阵 **B1**。

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

认领后调查补齐；预期 schema、product/library/imagesession/recipe/agent 根查询、测试、父章程、本文件。

## 不要碰

B2+ 全链路下载/SSE、开放第二商、Skill/grader。

## 合同

- MP-01..03 根归属；中间版本仍单商。
- 完成 ≠ MP-B。

## 阻塞与交接

- 原因：无（执行前仍核并行写集）。
- 解除条件：无。
- 跟进者：待认领。
- 交接：仅发布。

## 证据

- 待补。
