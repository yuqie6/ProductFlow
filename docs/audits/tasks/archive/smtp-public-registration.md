# 任务：SMTP 邮箱验证码公开注册并退役邀请

状态：完成
类型：实现
认领者：root/smtp-public-registration
认领于：2026-09-07T21:49:47+08:00
完成后可拆：无

任务协调遵循 [Issue 协议](../README.md)。用户直接要求 SMTP 接入、删除邀请与设置页二次解锁，本任务记录委派范围及交付证据。

## 问题来源

用户确认已有自建域名邮箱，要求公开邮箱验证码注册，SMTP 可配置；邀请机制当前无用，应删除，不实现推广或返利。

## 做成什么样

Operator 在现有设置页配置 SMTP；用户验证邮箱并设置密码后注册普通账号和自己的商家。保留部署者初始化和密码登录。删除邀请接口、服务、schema 权威与活跃文档依赖。

用户追加：删除管理员配置页二次解锁，以现有 Operator 身份作为配置访问权限。退役 settings lock-state/unlock API、cookie 标记、环境令牌、前端弹窗及其调用者；普通用户不得读写配置。

## 前置与并行

- 基线：`5aefc015`；无其它已认领任务占用本范围。
- 主代理负责集成、Web、API 主程序接线、合同清单和文档。
- root/registration_backend 独占 `go/internal/auth/` 与 `go/internal/platform/db/schema/`，实现验证码注册和邀请退役。
- root/smtp_backend 独占 `go/internal/settings/`，实现 SMTP 配置和传输；不修改 Web 或 auth。
- root/registration_docs 独占双语 ROADMAP/PRD/ARCHITECTURE/USER_GUIDE、商家平台章程及现有 SaaS 后续任务文档；主代理仍独占本任务、看板和归档索引。
- 追加范围分配：root/smtp_backend 独占 settings/config、相关 auth 测试、API main/route 测试及 httpx session 注释，删除二次解锁后端；root/registration_backend 独占 `web/e2e/`，迁移旧解锁调用并验证；主代理处理 Web 实现/合同/环境模板和集成，root/registration_docs 同步其文档范围及 performance-governance 的当前契约。
- 测试使用独立命名二进制派生的新 PG 测试库及本机 SMTP 测试服务器。用户随后提供临时邮箱并授权真实联调，SMTP 默认配置写入忽略的 `.env.dev`；仅向该测试邮箱发送注册验证码，不改远端配置，不调用真实模型。
- 原有图片质量任务与 `go/cmd/tmpcovergen/` 改动不属于本任务。

## 合同

- SMTP 字段复用设置闭集：smtp_host、smtp_port、smtp_security（starttls/tls）、smtp_username、smtp_password（secret）、smtp_from_address、smtp_from_name。凭据不回显、不进入公开 runtime 或普通导出。SMTP 未就绪时注册不可用，密码登录保留。
- settings.Store 实现 `RegistrationAvailable(context.Context) (bool,error)` 和 `SendVerificationCode(context.Context,string,string) error`；auth 通过相同方法的窄接口消费。
- POST `/api/auth/registration-code` 输入 email，成功返回 challenge_id、retry_after_seconds（60）。POST `/api/auth/register` 输入 email/challenge_id/code/password/display_name/merchant_name，成功沿现有 cookie 签入。GET session 增加 registration_available。
- 六位随机验证码 10 分钟有效，60 秒重发间隔，每个 challenge 最多 5 次错误验证，重发使旧 challenge 失效；验证前沿 Redis 凭据预算准入。SMTP 失败不假报成功，原始密码/验证码不入日志或 API。
- 邮箱规范化、验证码消费、User/Merchant/Owner/AuthSession/试用额度创建保持事务一致；并发与重放不能重复建账号/商家/额度。普通账号不能注册成 Operator。已登录请求不得静默替换身份；初始化完成前不开放注册。
- 邮箱验证后设置密码，保留现有 bcrypt 8 字符最小值与 72 字节上限。邀请 reader/writer/API/model 全部退役，无旧入口兼容；历史验收证据保留原结论。

## 怎么验收

验证码失效/错误次数/重发/发送失败/并发重放，SMTP 协议与 TLS/脱敏配置，真实 PG 注册事务回滚及迁移回归；API 路由合同、完整 Go 与 Web 门，实际浏览器注册与 SMTP 表单，docs-check。用户授权的临时邮箱用于真实发送、收件和注册验证。

## 阻塞与交接

- 原因：无。
- 解除条件：无。
- 跟进者：主代理。
- 交接：所有执行者完成后由主代理审查集成，子代理不提交或推送。

## 证据

- 基线：`5aefc015` 加本任务 diff。
- 后端针对性回归：`go test ./internal/settings ./internal/auth ./internal/platform/config ./internal/platform/db/schema ./internal/platform/httpx ./cmd/productflow-api -count=1 -p 1`，通过。使用 `.env.dev`、独立 Redis 26379 和按命名测试二进制派生的独立 PostgreSQL 库；没有以缺数据库跳过冒充通过。
- 覆盖：真实 PostgreSQL 注册事务与失败回滚、100 单位试用额度、错误次数落库、过期/重发/跨邮箱/并发重放、已有身份保护；TLS/STARTTLS、证书和降级拒绝、SMTP 失败、密码脱敏、环境默认/数据库覆盖/重置、邀请表退役迁移；管理员直接访问、普通用户与匿名拒绝、业务 runtime 可读及旧解锁路由删除。
- Web：`pnpm --dir web test:run` 103 文件、730 项通过；`pnpm --dir web lint`、`just web-build`（含 E2E 类型检查与 bundle budgets）通过。`playwright test e2e/registration.spec.ts e2e/login-security.spec.ts e2e/smtp-settings.spec.ts` 35 项通过，覆盖四种语言、390/1440 视口、明暗主题、注册入口可见性和无需二次解锁的 SMTP 表单。
- 真实浏览器：初次使用授权邮箱完成 SMTP TLS 发码、IMAP TLS 收码、注册进入 `/products`、普通账号/自有商家/Owner、试用额度 100、重放 410、密码登录 200。随后在独立空库和 API 29292 复验 `.env.dev` SMTP：初始化前注册拒绝；浏览器初始化后管理员直接进入邮件设置并保存；普通注册用户配置 GET/PATCH 403、业务 runtime 200，匿名配置 401，旧 unlock POST 404。测试结束已删除临时账号、独立数据库和测试 API/Redis，真实邮箱凭据仅保留在忽略的 `.env.dev`；本地 SMTP 数据库覆盖已清空。
- 浏览器截图：`web/test-results/smtp-registration-final/`；实际 SMTP 环境及管理员直接访问截图 `/tmp/productflow-smtp-admin-direct.png`、注册结果 `/tmp/productflow-smtp-env-registered.png`，主代理已查看。
- 全 Go 默认套件已执行，未通过：Graph 测试空库直接插入商品时报 `merchant_id required: no merchant exists`；其后 `TestWriteTxDoesNotCommit` 清理等待未释放事务，采集栈后终止该包以继续其它包。Product 的 `TestGenerateSourceNoteRejectsInsufficientQuota` 预期额度不足拒绝但返回 200。其余包通过。使用 `git archive 5aefc015 go` 的独立代码快照、新命名测试库复跑 `TestWriteTxProductSourceTemplate` 和完整 product 包，两处相同失败均在改动前复现。本交付不声明整仓 Go 门通过，不修改 Graph/Product 业务代码或放宽其断言。
- 文档和配置：`just docs-check`、`git diff --check`、开发与 release Compose `config --quiet` 通过。活文档同步公开注册、SMTP 环境默认和 Operator 直接配置；密码恢复、团队新增成员与完整多商家体验保留为未来能力。
- 审核者：主代理自审 Web/接线/合同并审查三个子代理完整 diff；子代理分别自审其交付，没有以自审冒充独立评估。
- 交付定位：随本任务提交。
- 结果：本次注册、SMTP 与管理员配置权限改动已交付并有针对性及真实浏览器证据；整仓 Go 门保留上述基线失败，未发布到外部站点。
