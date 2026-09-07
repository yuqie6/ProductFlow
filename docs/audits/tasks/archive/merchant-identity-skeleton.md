# 任务：实现 User/Merchant/Membership 身份骨架

状态：完成
类型：实现
认领者：sub-merchant/merchant-identity-skeleton
认领于：2026-09-07T12:03:00+08:00
完成于：2026-09-07T12:28:00+08:00
业务组：商家平台
父账本：merchant-platform.md
完成后可拆：B1 根对象 `merchant_id` 迁移；完整隔离门未过不开放第二商家

按 [Issue 协议](../README.md) 认领并确认所有权后调查与实现。承接 [覆盖矩阵](merchant-isolation-contract.md) 批次 **B0** 与父章程 MP-01/MP-02/MP-A。协调者审核：2026-09-07 CTO 通过——auth/schema 测试复跑 ok；admin 布尔已废除；第二商仍不开放。

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

实际交付清单：

- `go/internal/platform/db/schema/identity.go`（新）：`users`、`merchants`、`memberships`、`merchant_invites`、`auth_sessions`
- `go/internal/platform/db/schema/models.go`：`AllModels` 注册身份表
- `go/internal/platform/db/schema/constraints.go`：FK/UNIQUE/CHECK/索引 ExtraDDL
- `go/internal/auth/`：`crypto.go`、`service.go`、`principal.go`、`ratelimit.go`、`http.go`、`testutil.go`、`http_test.go`
- `go/internal/platform/httpx/admin.go`、`session.go`：RequireAdmin 改校验 User 会话；`SessionString`
- `go/cmd/productflow-api/main.go`、`routes_contract_test.go`：挂载 LoadPrincipal；跳过 `/api/auth/*` 未登录探针
- `contracts/http-routes.json`：bootstrap/邀请/商家路由
- 各包 harness 登录接线：`product`/`library`/`graph`/`recipe`/`imagesession`/`delivery`/`localedit`/`agent`/`settings`/`mediaarchive`/`imageeval`
- `web/src/pages/LoginPage.tsx`、`web/src/lib/api.ts`、`web/src/lib/types.ts`、`web/src/lib/i18n.ts`
- `docs/audits/merchant-platform.md` B0/MP-A；本文件

扩大到 product/library/graph 过滤属 B1，须交回协调者，不得擅自开写。

## 不要碰

业务根表 `merchant_id` 回填（B1）、Agent scope 商家字段全量（B7）、额度账本（MP-C）、Skill/grader、共享开发栈强制切多商。

## 现在代码在哪

`go/internal/auth/`（User 密码会话 + Membership）；schema `identity.go`；Web 登录页支持 bootstrap/密码。

## 合同

- MP-01、MP-02、MP-A：多角色、邀请/撤销/恢复、最后 Owner、会话失效可验证。
- 浏览器或 Agent 载荷中的商家 ID 不授予权限。
- 密码与会话使用 bcrypt + 可撤销 `auth_sessions`；登录节流（每 IP 每分钟 10 次失败）；cookie 仍为 HttpOnly/SameSite=Lax/`session` 名。
- 单商家开发路径：空实例 `POST /api/auth/bootstrap`（需 `ADMIN_ACCESS_KEY`）创建唯一 Operator+Owner+Merchant；之后仅邮箱密码登录。

## 怎么验收

- 聚焦 Go 测试：登录、邀请、角色、最后 Owner 并发、撤销会话、非成员 401/403。
- 必要 migrate 回归；Web 登录烟测（可 mock）。
- `just docs-check`；相关包测试。完成 ≠ MP-B/R1。

## 阻塞与交接

- 原因：无。
- 解除条件：无。
- 跟进者：协调者（审核关闭后可拆 B1）。
- 交接：**实现与证据已就绪，保持认领等待维护者审核与看板关闭**；不 push、不改看板 README、不宣称 MP-B。

## 证据

- 冻结 HEAD（认领）：`d6709c4aacb2e26bb30ab70a99d08b1dca05f487`。
- **迁移说明**：新表经 `schema.Apply` → CreateTable + ExtraDDL；**无 AutoMigrate**。表名：`users`、`merchants`、`memberships`、`merchant_invites`、`auth_sessions`。
- **与旧 admin 会话关系**：`is_authenticated` 布尔 cookie **已废除**，不作为正式回退；`RequireAdmin` 只认 `auth.Authenticated`（有效 `auth_sessions`）。`ADMIN_ACCESS_KEY` 仅用于空实例 bootstrap，不可再 `POST /api/auth/session` 登录。未开 `AdminAccessRequired` 时仍放行（本地无门禁），不写 admin 布尔 cookie。无调试开关。
- **测试命令与结果**：
  - `bash scripts/with_dev_env.sh bash -lc 'go test -C go ./internal/auth/ ./internal/platform/db/schema/ ./cmd/productflow-api/ -count=1 -p 1'` → ok
  - `... ./internal/product/ ./internal/library/ ./internal/agent/ ./internal/settings/ ./internal/graph/ ./internal/recipe/ ./internal/imagesession/ ./internal/delivery/ ./internal/localedit/` → ok（分批）
  - `just docs-check` → passed
- **自审**：最后 Owner 并发冲突；撤销会话后 runtime 401；非成员邀请 403；第二商家 Create 409；`admin_key` 登录体 400。未实现 B1 业务表 `merchant_id` 过滤（预期缺口）。
- 交付定位：待协调者审核后提交（本执行者不 commit）。

- 审核者：CTO（本会话）；结论：通过。可发布 B1 根归属（与 CF-B0/采用任务串行 schema 时排队）。
