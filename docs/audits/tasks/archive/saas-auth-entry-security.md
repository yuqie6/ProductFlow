# 任务：账号入口在邀请、跨站请求与多 API 实例下保持可靠鉴权

状态：完成
类型：实现
认领者：root/saas-auth-entry-security
认领于：2026-09-07T19:46:00+08:00
业务组：商家平台
父账本：merchant-platform.md
完成后可拆：saas-workspace-context

任务状态、认领及提交遵循 [Issue 协议](../README.md)。本任务仅经协调者确认所有权后执行；发布不表示已分配。

## 问题来源

设计基线 `0dff28fa`：`auth.Service.AcceptInvite` 对已有用户仅在 password 非空时校验密码，之后创建该用户会话；邀请不能隐式成为已有账号的登录凭据。`auth/ratelimit.go` 的 allow/fail 分离且只在进程内计数，直接采信 X-Forwarded-For。多实例和同时到达的请求需要统一准入。本任务不依赖第二商家开放。

## 做成什么样

- 新用户持有效邀请设置密码并加入；已有用户必须证明账号身份，未登录且空密码不能获得会话。已有 Operator 收到普通商家邀请也不得凭邀请取得 Operator 会话。
- 公开凭据入口具有跨实例、原子、有期限的限流；伪造转发头不改变未受信连接的限流身份。
- 浏览器状态修改验证可信来源；登录、bootstrap、接受邀请与已登录 mutation 都包含在保护范围；合法本地开发和正式同源部署有明确配置。
- 页面保留可重试的错误与 429 冷却状态，不向用户展示内部计数 key。

## 前置与并行

- 前置：现有 User/Merchant、邀请和 cookie 实现，认领时记录实际 HEAD 与相关文件 diff。
- 冻结输入：现有身份角色、内部服务鉴权与会话 cookie 名；不改变商家开通限制。
- 运行资源：任务专用 PostgreSQL 测试库和 Redis namespace/实例，两 API 测试实例；不得 FLUSHDB 共享 Redis 或改共享 provider。
- 不与其它 auth、HTTP middleware、config、LoginPage 或共享 API client 写者并行；协调者串行整合共享文档。
- 本轮分工：root/auth_implementation 独占本任务 auth/httpx/config、配置样例与 release 配置说明；主代理独占 go/cmd/productflow-api、Web、任务记录和活文档，并负责跨层审查与最终验收。双方仅按已确认 HTTP 合同交接，不交叉写文件。数据库测试按现有 testdb 派生专用库，Redis 测试使用独立实例；不停止共享服务。

## 只改这些文件

- `go/internal/auth/`、`go/internal/platform/httpx/`、`go/internal/platform/config/` 中本合同所需入口与测试。
- `go/cmd/productflow-api/` 接线；已有 go-redis 依赖直接使用所需的最小 `go/go.mod` / `go/go.sum` 调整。
- `web/src/pages/LoginPage.tsx`、`web/src/lib/api.ts`、`web/src/lib/types.ts`、`web/src/lib/i18n.ts` 及贴近入口的测试。
- `.env.example`、`contracts/http-routes.json`、`release/README.md` 中受影响配置/路由；需要新邀请页时由协调者扩入精确路径后实施。
- 主代理配置接线：`docker-compose.yml`、`release/docker-compose.yml`、`release/.env.example`，透传允许来源、可信代理和限流参数；否则容器 API 无法履行同源合同。
- 本文件；协调者整合父章程和受影响活文档。

## 不要碰

- Graph、额度结算、Pi runtime、现有评分材料、第二商家开放、公开注册、SSO、SMTP 和真实邮件发送。
- 不新建 JWT 系统，不将 User/Membership 或账本权威迁入 Redis。

## 现在代码在哪

`go/internal/auth/{service,http,crypto,ratelimit,principal}.go`、`go/internal/platform/httpx/{engine,session}.go`、`go/cmd/productflow-api/main.go`。`http_test.go`、`ops_test.go` 是现有回归入口；schema 在 `go/internal/platform/db/schema/identity.go`。现有 bcrypt、opaque token、会话库与 go-redis 可复用。

## 合同

1. 接受已有账号邀请：当前有效会话 user 必须与邀请账号相同，或提交正确现有密码。不同账号的有效会话返回 409 并要求显式退出，不能静默替换浏览器身份。新账号仍经邀请创建。受邀角色仅影响该商家 Membership；令牌不授予全局身份。错误不消耗邀请。
2. 密码继续使用现有 bcrypt；输入上限与实际库的 72-byte 限制一致，超限明确拒绝，不能静默截断。错误码与中文 detail 沿现有约定，不另加全局错误体系。
3. Redis 7 可用命令实现固定窗口原子尝试准入：按可信 IP 每 15 分钟最多 100 次、按 IP+规范化账号/邀请令牌摘要每 15 分钟最多 10 次。两预算同一原子决策，超限返回 429 + Retry-After；计数先于昂贵密码验证，成功也计本窗口。初始值是开发验收默认，不是已测抗滥用承诺；配置 env-only。key 只含命名空间、窗口和不可逆摘要，自动到期；不存邮箱或原始令牌。
4. Redis 超时/不可用：受限凭据交换返回 503，不退回各实例本地计数放行；已有有效会话的普通读取不依赖此 limiter。重建 Redis 只重置有限窗口，不撤销或授予身份。测试暂停时钟/构造窗口边界，不睡 15 分钟。
5. 显式配置可信代理 CIDR，默认不信任何代理头，使用解析后的 IP，去掉源端口；两条经过代理链的合法流量与直接伪造头分别验证。
6. 首版浏览器 Web/API 同源。允许来源由 env 的精确 scheme/host/port 列表提供；Origin 优先，缺失时检查 Referer origin，两者缺失/null/不匹配则拒绝 cookie/浏览器状态修改。不能用请求 Host 或未验证转发头生成信任名单；不使用通配 credential CORS。开发源在配置样例显式列出，生产 cookie 保持 HttpOnly、Secure 和 SameSite 约束。
7. 内部服务调用沿真实服务令牌授权，不因 URL 带 internal 就豁免。逐入口列明 JSON、multipart 上传和 cookie mutation 的覆盖，保留 webhook 等尚不存在接口为未实现；不能全局 Content-Type=JSON 拦坏已有上传。

## 怎么验收

- 确定性回归：已有账号/Operator 空密码邀请拒绝、正确密码/匹配会话成功、错误会话 409、新账号、过期/撤销/重复接受、错误不消耗 token、密码字节边界。
- 两 API 共享 Redis 并发 30 个同账号同 IP 尝试，允许量不超过 10；不同账号触发 IP 总上限；伪造代理头无绕过；TTL 正确；断 Redis 返回 503 且普通已登录读取可用。
- 同源登录/JSON/multipart 成功；外源、null、缺来源拒绝；Referer fallback、可信代理和内部服务正反用例。浏览器 390×844 / 1440×960 验登录错误与冷却，不调用真实 provider。
- 按包规则运行受影响 Go 测试与 `go/cmd/productflow-api` 路由合同，PG 不可跳过；前端 focused tests、lint/build 与浏览器入口；`just docs-check`。保存命令、版本和失败样本。

## 阻塞与交接

- 原因：无；需正常认领和资源隔离。
- 解除条件：无。
- 跟进者：开发协调者。
- 交接：身份入口实现及本合同回归已完成；未授予发信或外部开放权限。后续工作商家任务消费本次 cookie/来源合同。

## 证据

- 验证日期：2026-09-07；实现基线 `47379b8f` 加本任务 diff；设计观察基线 `0dff28fa`。
- `pnpm --dir web test:run`：103 文件、728 测试通过；`pnpm --dir web lint` 与 `just web-build`（含包体预算）通过。
- `pnpm --dir web exec playwright test e2e/login-security.spec.ts`：17 用例通过；390×844 / 1440×960、四语明暗，429 冷却、Enter 不重复提交、503 后可重试和 bootstrap。截图在 `web/test-results/login-security-*/login-cooldown.png`，已检查尺寸与溢出。本项使用真实浏览器、mock HTTP，不作为真实 provider 或完整 SaaS 验收。
- `bash scripts/with_dev_env.sh env REDIS_URL=redis://127.0.0.1:26379/0 go test -C go ./internal/auth ./internal/platform/httpx ./internal/platform/config ./cmd/productflow-api -count=1 -p 1`：通过；PG 使用包级专用 testdb，Redis 为任务专用进程。后补邀请与真实 Redis 停机测试的 focused `-v` 复跑通过，未跳过 PG/Redis。
- `docker compose --env-file .env.example config --quiet` 及使用测试镜像变量的 release Compose 校验通过。`just docs-check` 通过。
- 全后端命令 `bash scripts/with_dev_env.sh env REDIS_URL=redis://127.0.0.1:26379/0 go test -C go ./... -count=1 -p 1` 已执行：其余包通过，Agent 在复用库超时，quota 全局过期扫描受到历史夹具影响；运行途中新增 httpx 测试的 imports 导致该包编译输入失效。冻结代码后 auth/httpx/config/API 四包完整复跑通过。原全量命令结果为 FAIL，不记为一次性绿灯。
- Agent、quota 用当前代码编译测试二进制 `/tmp/pf_authgate_agent.test`、`/tmp/pf_authgate_quota.test` 后，在 dev 环境直接运行 `-test.count=1`（Agent 加 `-test.timeout=4m`），两包全部 PASS；实际库名为 `productflow_dev_gotest_pf_authgate_agent` / `productflow_dev_gotest_pf_authgate_quota`，与旧包级库隔离。`go test -o` 本身仍运行临时默认名称二进制，不能作为新库证明。全部后端包已有通过结果；旧测试库复跑稳定性仍是独立缺口，本任务未修改 Agent/额度实现或清空旧库。
- 主代理审查并修正：IP+subject 键、缺 limiter 的 503、显式邀请身份参数、真实内部令牌校验、来源配置透传、限流 IP 耗尽后不新增 subject key；补邀请失败不建 session/不消费 token、普通用户与 Operator 身份证明回归。
- 交付定位：随本任务提交。
- 审核者 / 结论：主代理审核子代理后端 diff，并自审 API 接线、前端和补充回归；合同回归通过，全后端失败包经隔离复跑通过。未使用独立审核者名义。
- Issue 结果 / 业务门槛结果 / 剩余缺口：本项完成；不重签 R1 或宣布 SaaS 可开放。浏览器使用 mock HTTP，现有开发 API 未重启，不声明新后端已在线运行。
