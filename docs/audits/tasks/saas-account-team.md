# 任务：个人账户与商家团队可完成邀请、撤权和账号恢复

状态：阻塞
类型：实现
认领者：—
认领于：—
业务组：商家平台
父账本：merchant-platform.md
完成后可拆：saas-preferences-settings；saas-ops-console

任务状态、认领及提交遵循 [Issue 协议](README.md)。

## 问题来源

现有 User、Membership、邀请和可撤销会话已接线，但没有完整账号、会话与团队管理 UI。`RevokeMembership` 当前按 actor/target 锁行，角色修改与最后 Owner 必须统一串行化决策，避免新增入口产生不同锁序。恢复 Membership 与恢复忘记密码的账号是不同操作。

## 做成什么样

- `/account` 可改显示名、改密码、查看和撤销自己会话；邮箱首版只读。
- `/merchant/settings?section=members` 的 Owner 可列成员、邀请、撤销邀请、改角色、移除/恢复成员；最后 Owner 受保护。Editor/Viewer 不读取团队邮箱与邀请 token。
- `/invite` 能完成新/已有账号邀请；`/recover` 完成短期恢复凭据换密码。Operator 有受审计的恢复签发操作，首版由其人工交付链接，不集成发送服务。

## 前置与并行

- 前置：[saas-workspace-context](saas-workspace-context.md) 与身份入口任务已交付；采用已固定范围选择、CSRF 和限流。
- 冻结输入：角色枚举、cookie/会话、merchant 停用策略；不改变正在执行任务的归属。
- 资源：隔离 PG/Redis/browser，两个 Owner、Editor、Viewer、无成员 Operator 夹具；禁止真实发信或读取真实用户密码。
- auth/schema/共享 Web shell 排他；平台容量采证用独立固定 checkout 才可并行。

## 只改这些文件

- `go/internal/auth/`、`go/internal/platform/db/schema/identity.go`、migrate 与测试、API 路由合同。
- `web/src/pages/` 新 Account/Invite/Recovery/MerchantSettings 及其局部组件；`App.tsx`、`TopNav.tsx`、`web/src/lib/{api,types,i18n}.ts` 与测试、`web/e2e/` 用例。
- `.env.example` 如需显式 public origin；本文件及协调者整合的父章程/活文档。
- auth 审计和恢复表由本任务建唯一实现，后续运营任务消费，不能另造第二套。

## 不要碰

- 公开注册、修改邮箱、SSO、支付、商家 CreateMerchant 放开、SMTP、宿主机应急 Operator 恢复工具。

## 现在代码在哪

`go/internal/auth/service.go` 的 CreateInvite/AcceptInvite/RevokeMembership/RestoreMembership/RevokeSession；`schema/identity.go`；`web/src/pages/LoginPage.tsx`；现有 `auth/http_test.go` 与双商 fixture。现有邀请 token 只存 hash，必须复用。

## 合同

1. 新 API 属 `/api/auth` 的 account/password/sessions、`/api/merchants/:merchant_id` 的 memberships/invites 列表与角色修改、`/api/ops/users/:user_id` 的恢复签发。执行者取得所有权后补充逐 route 方法、严格 DTO 和契约测试映射再实施；不擅自改变以下业务语义。
2. 显示名 1–160 字符；仅改本人。会话列表只含本人不透明会话 id、创建/过期时间与 current，不采集新增设备指纹；页长默认 20、最大 50，cursor 按 created_at/id。撤销他人 session 404，重复撤销本人 session 幂等。撤销当前 session 立即回登录。
3. 改密码要求现密码；一个事务更新密码并撤销全部旧会话，提交后清 cookie，重新登录。与并发 Login 的线性化点是 User 行锁：密码验证、会话创建不能穿过密码变更写入一张仍有效的旧凭据 session。恢复后同样撤销全部旧会话，不自动登录。
4. 恢复凭据：32-byte 随机值，仅存 hash，30 分钟、一次性、绑定 user 与用途；签发新凭据撤销该用户未消费恢复凭据。Operator 最近 10 分钟有密码再认证才可签发，并填写原因；禁止通过此入口重置 Operator 账号。提交恢复不授予 Membership/Operator。登录页只提供人工联系路径，不出现未接线的“邮件已发送”。
5. 恢复/邀请链接用 URL fragment 承载 token，页面读入内存后 replaceState 移除；API 仅从 body 接收 token，不进入日志、query key、持久浏览器存储或分析系统。原始 token 仅创建成功一次返回；刷新页面不重新显示，丢失则撤销并重签。首版人工可信渠道发送，平台不承诺邮件送达。
6. 修改成员角色、移除/恢复、Owner 变更统一锁当前 Merchant 根行后重新读取 actor 权限及 target，再检查最后 Owner。邀请接受涉及 User 和商家时统一 User→Merchant→Membership/Invite 顺序，所有参与 writer 同序；schema 唯一约束处理并发同邮箱创建，失败不部分消费 token。已有效成员接受邀请为 409，不用旧邀请覆盖现角色。
7. Owner 可授权其它 Owner；Owner 转移以先提升另一有效成员、再降级自己完成，两个步骤各自保持至少一位 Owner。停用商家禁止成员管理 mutation；Owner 可只读团队，恢复依运营流程。列表必须限定商家、服务端 cursor 分页，过期邀请与被撤销成员可筛选。
8. 新增统一身份/运营审计事件：actor_user_id、目标 user/merchant、action、reason、时间、结果及必要的角色/status 前后值。成功 mutation 与审计同事务；审计不得存密码、token 或整份请求。本人改名无需强制 reason。失败请求走脱敏安全日志，不以失败业务审计写坏事务。

## 怎么验收

- 两 Owner 并发自降级/互移除不出现零 Owner；与邀请接受、停用交错没有死锁和权限复活。验证数据库实际 Membership 与审计行。
- 改密码/恢复和并发登录、token 重放/撤销/过期、错误用途、错误用户、Operator 恢复拒绝；旧 session 全部失效；密码/API/log/URL 不泄漏秘密。
- Owner 完整团队流程、Editor/Viewer 直接 API 和 URL 拒绝、个人页面无需商家、被移除用户仍可管理自己的账号。
- API/PG 和 migrate 回归、前端 focused tests、lint/build；真实浏览器 mock 操作覆盖 390×844 和 1440×960、四语明暗、长邮箱与冲突恢复。`just docs-check`。
- 不为此任务跑图片质量或真实模型门；本项完成不签 R1/R2/R5 总门。

## 阻塞与交接

- 原因：工作区上下文任务尚未交付。
- 解除条件：协调者核验前置与排他文件、登记 route/DTO 后开放；例行裁定无需用户重复批准。
- 跟进者：开发协调者。
- 交接：未开工，无资源占用。

## 证据

- 命令 / 日期 / 结果：待执行。
- 基线 commit / artifact：执行时固定。
- 交付定位：随本任务提交。
- 审核者 / 结论：待审核。
- Issue 结果 / 业务门槛结果 / 剩余缺口：未交付。
