# 任务：个人账户与既有商家成员管理

状态：阻塞
类型：实现
认领者：—
认领于：—
业务组：商家平台
父账本：merchant-platform.md
完成后可拆：saas-preferences-settings；saas-ops-console

任务状态、认领及提交遵循 [Issue 协议](README.md)。

## 问题来源

现有 User、Membership 和可撤销会话已接线，但没有完整账号、会话与团队管理 UI。旧邀请接口和令牌路径随公开注册切片退役，不保留兼容入口。`RevokeMembership` 当前按 actor/target 锁行，角色修改与最后 Owner 必须统一串行化决策，避免新增入口产生不同锁序。恢复 Membership 与恢复忘记密码的账号是不同操作。

## 做成什么样

- `/account` 可改显示名、改密码、查看和撤销自己的会话；邮箱首版只读。
- `/merchant/settings?section=members` 的 Owner 可分页列出现有成员、改角色、移除/恢复成员；最后 Owner 受保护。Editor/Viewer 不读取超出权限的成员资料。
- 现有成员管理继续保留。新增成员的直接已有用户关联另行设计，本任务不以人工转发、短期 token 或其它邀请机制替代公开注册。
- 账号密码恢复、修改邮箱与相关邮件流程尚未实现；未来恢复使用与注册用途分离的 SMTP 短期凭据，成功后撤销旧会话。本任务不得写成已交付能力或展示虚假的邮件成功提示。

## 前置与并行

- 前置：[saas-workspace-context](saas-workspace-context.md) 与身份入口任务已交付；采用已固定范围选择、CSRF 和限流。
- 冻结输入：角色枚举、cookie/会话、merchant 停用策略；不改变正在执行任务的归属。
- 资源：隔离 PG/Redis/browser，两个 Owner、Editor、Viewer、无成员 Operator 夹具；禁止真实发信或读取真实用户密码。
- auth/schema/共享 Web shell 排他；平台容量采证用独立固定 checkout 才可并行。

## 只改这些文件

- `go/internal/auth/` 的账户、密码、会话和 Membership 管理、`go/internal/platform/db/schema/identity.go`、migrate 与测试、API 路由合同。
- `web/src/pages/` 的 Account/MerchantSettings 及局部组件；`App.tsx`、`TopNav.tsx`、`web/src/lib/{api,types,i18n}.ts` 与测试、`web/e2e/` 用例。
- `.env.example` 如需显式 public origin；本文件及协调者整合的父章程/活文档。
- auth 审计表由本任务建唯一实现，后续运营任务消费，不能另造第二套。

## 不要碰

- 公开注册、登录页注册模式、SMTP、SSO、支付、商家 CreateMerchant 放开、宿主机应急 Operator 恢复工具。
- 新增成员的直接已有用户关联设计；它需要单独的权限、审计和并发合同。

## 现在代码在哪

`go/internal/auth/service.go` 的 `RevokeMembership`、`RestoreMembership`、`RevokeSession`；`schema/identity.go`；`web/src/pages/LoginPage.tsx`；现有 `auth/http_test.go` 与双商 fixture。旧邀请 reader/writer/API/model 由公开注册切片退役，不得复用或新增兼容读取。

## 合同

1. 新 API 属 `/api/auth` 的 account/password/sessions，以及 `/api/merchants/:merchant_id` 的 memberships 列表与角色修改、移除/恢复。执行者取得所有权后补充逐 route 方法、严格 DTO 和契约测试映射再实施；不擅自改变以下业务语义。
2. 显示名 1–160 字符；仅改本人。会话列表只含本人不透明会话 id、创建/过期时间与 current，不采集新增设备指纹；页长默认 20、最大 50，cursor 按 created_at/id。撤销他人 session 404，重复撤销本人 session 幂等。撤销当前 session 立即回登录。
3. 改密码要求现密码；一个事务更新密码并撤销全部旧会话，提交后清 cookie，重新登录。与并发 Login 的线性化点是 User 行锁：密码验证、会话创建不能穿过密码变更写入一张仍有效的旧凭据 session。
4. 账号密码恢复、修改邮箱和自助邮件发送当前未实现；未来恢复凭据使用独立用途的随机值，仅存 hash，短期、一次性并绑定 User，提交成功撤销该用户全部旧会话，不授予 Membership 或 Operator。实现前只保留明确的未实现状态，不创建 501 伪入口或“邮件已发送”提示；不得以 Operator 手工链接作为默认产品流程。
5. 公开注册使用独立 challenge 合同，由 SMTP 注册任务拥有；本任务不复制注册 DTO、challenge、SMTP 配置或创建 Merchant 的事务。
6. 旧邀请接口、短期令牌、表和 reader/writer 退役；不保留 URL、API、schema 或导出兼容入口。新增成员直接关联须另行设计，不以旧机制恢复。
7. 修改成员角色、移除/恢复、Owner 变更统一锁当前 Merchant 根行后重新读取 actor 权限及 target，再检查最后 Owner。所有参与 writer 同序；已有效成员的重复变更按现有冲突合同处理。
8. Owner 可授权其它 Owner；Owner 转移以先提升另一有效成员、再降级自己完成，两个步骤各自保持至少一位 Owner。停用商家禁止成员管理 mutation；Owner 可只读团队，恢复依运营流程。列表必须限定商家、服务端 cursor 分页。
9. 新增统一身份/运营审计事件：actor_user_id、目标 user/merchant、action、reason、时间、结果及必要的角色/status 前后值。成功 mutation 与审计同事务；审计不得存密码、验证码、token 或整份请求。本人改名无需强制 reason。失败请求走脱敏安全日志，不以失败业务审计写坏事务。

## 怎么验收

- 两 Owner 并发自降级/互移除不出现零 Owner；与成员变更、停用交错没有死锁和权限复活。验证数据库实际 Membership 与审计行。
- 改密码和并发登录、旧 session 失效、密码/API/log/URL 不泄漏秘密；未来恢复凭据的过期、重放、错误用途、错误用户和旧 session 撤销有回归，未实现前保持明确不可用。
- Owner 完整成员管理流程、Editor/Viewer 直接 API 和 URL 拒绝、个人页面无需商家、被移除用户仍可管理自己的账号。
- API/PG 和 migrate 回归、前端 focused tests、lint/build；真实浏览器 mock 操作覆盖 390×844 和 1440×960、四语明暗、长邮箱与冲突状态。`just docs-check`。
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
