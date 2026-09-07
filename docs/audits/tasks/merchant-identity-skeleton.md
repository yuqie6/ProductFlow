# 任务：实现 User/Merchant/Membership 身份骨架

状态：认领
类型：实现
认领者：sub-merchant/merchant-identity-skeleton
认领于：2026-09-07T12:03:00+08:00
业务组：商家平台
父账本：merchant-platform.md
完成后可拆：B1 根对象 `merchant_id` 迁移；完整隔离门未过不开放第二商家

按 [Issue 协议](README.md) 认领并确认所有权后调查与实现。承接 [覆盖矩阵](archive/merchant-isolation-contract.md) 批次 **B0** 与父章程 MP-01/MP-02/MP-A。

## 问题来源

当前 `go/internal/auth` 仅为共享管理员密钥与布尔会话，无法承载多商家。覆盖矩阵已冻结：须先交付 User、Merchant、Membership、邀请与密码会话，再谈业务表归属。

## 做成什么样

1. Schema：`users`、`merchants`、`memberships`、邀请/会话所需表（调查后列精确表名）；经 `productflow-migrate` CreateTable/AddColumn，**不用** AutoMigrate。
2. 用 User 密码会话替换业务 API 的共享 admin 布尔登录作为正式入口；Operator 可引导建立**唯一**开发商家；邀请成员、角色读 Membership、会话撤销后 401。
3. 最后 Owner 保护与并发移除有测试；无效会话、非成员拒绝。
4. **中间版本仍仅 1 个可运营商家**；不得提供开放注册第二互不信任商家的入口；不得保留「共享 admin cookie 访问全部业务」作为正式回退（迁移窗口若暂留调试开关须在证据写明且默认关闭）。
5. 业务表尚未强制 `merchant_id` 过滤时可暂限 Op 工具创建商（与矩阵 B0 一致）；商品/媒体隔离留给 B1+。

## 前置与并行

- 前置：覆盖矩阵已归档；MP-01..07；总纲 §7.1–7.2。
- 冻结输入：认领时 HEAD；矩阵对身份实体的裁定；不实现 B1 业务根归属。
- 运行资源：隔离测试 DB；不改共享 provider；不开放第二商生产数据。
- 排他写入：auth/membership 新包或 `go/internal/auth/` 改造、`go/internal/platform/db/schema/`、migrate、相关 HTTP 注册与测试、Web 登录/会话最小接线、本文件与父章程 B0/MP-A 结论。与 `release-compose-proxy-overlay`（nginx/compose）及 `delivery-workbench-projection`（workbench UI）路径不重叠。

## 只改这些文件

调查后在本文件补齐精确清单；预期包括：
- `go/internal/platform/db/schema/`、migrate 相关
- `go/internal/auth/` 及新建 membership/merchant 包（若拆分）
- `go/cmd/productflow-api/register.go` 最小挂载
- 相关 `*_test.go`；Web 登录页/会话 API 客户端最小替换
- `docs/audits/merchant-platform.md` B0/MP-A 行；本文件
扩大到 product/library/graph 过滤属 B1，须交回协调者，不得擅自开写。

## 不要碰

业务根表 `merchant_id` 回填（B1）、Agent scope 商家字段全量（B7）、额度账本（MP-C）、Skill/grader、共享开发栈强制切多商。

## 现在代码在哪

`go/internal/auth/http.go`、`go/internal/platform/httpx/admin.go`；schema `models.go` 无 users/merchants；Web 登录依赖 admin session。

## 合同

- MP-01、MP-02、MP-A：多角色、邀请/撤销/恢复、最后 Owner、会话失效可验证。
- 浏览器或 Agent 载荷中的商家 ID 不授予权限。
- 密码与会话使用成熟库做法；补登录节流、CSRF/安全 cookie 约束（与总纲 7.2 一致，范围以本切片可验证为限）。
- 单商家开发路径不断裂：现有单管理员工作流在唯一开发商家下可继续（业务隔离完整性由后续批次保证）。

## 怎么验收

- 聚焦 Go 测试：登录、邀请、角色、最后 Owner 并发、撤销会话、非成员 401/403。
- 必要 migrate 回归；Web 登录烟测（可 mock）。
- `just docs-check`；相关包测试。完成 ≠ MP-B/R1。

## 阻塞与交接

- 原因：无。
- 解除条件：无。
- 跟进者：sub-merchant/merchant-identity-skeleton。
- 交接：已认领；未改代码。

## 证据

- 待补：HEAD、实际文件、测试命令、自审、与旧 admin 会话迁移说明。
- 交付定位：随本任务提交（协调者审核后）。
