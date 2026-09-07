# 任务：身份模型收敛为普通账号自有一个商家与独立管理员权限

状态：完成
类型：实现
认领者：identity-convergence
认领于：2026-09-08T00:50:00+08:00
完成后可拆：无

任务协调遵循 [协议](../README.md)。

## 问题来源

用户明确本次也要彻底收敛身份模型，不只删除文档或占位。普通账号自有一个商家，管理员独立管理各商家的商品；多组织 Membership、Owner/Editor/Viewer 和商家切换不属于产品。

## 做成什么样

登录/注册、会话、业务授权和前端使用同一个账号自有商家合同；删除废弃成员/角色/切换读写者和接口，保留商品归属与跨账号隔离。管理员跨商权限明确验证，不伪造成员关系。不保留双模型兼容路径。

## 前置、实施与所有权

基线 6e8c271c；政策切片已独立提交 37ba9ebc 后才转入共享 Web 身份文件。主代理负责合同、消费者、显式目标商品 API、集成、审核、开发切换和提交；identity_convergence 执行 auth 与 Agent fixture，quota_policy 执行 schema、其他 fixture 和 Graph 异步归属修复，frontend_policy 执行 Web 身份读者及浏览器回归。

各切片单写者排他，完成后移交主代理；最终全 Go 与编译由 identity_convergence 使用独立 pf_identity_final 资源执行。全部新增生成测试使用 mock provider，无收费模型、SMTP 发信或生产部署。其他会话的首页改动与图片质量 smoke 记录保留。

## 合同与验收

一个普通账号自有一个商家；不同账号商品、素材、任务、额度隔离；已受理后台任务归属不变。管理员走独立权限管理目标商品。迁移保留现有普通账号唯一归属数据，歧义数据不得静默猜测或丢弃。退出、停用、会话撤销继续生效。删除 retired 字段后扫描所有读写者/路由/fixture/文档。受影响跨模块 Go/PG、schema migrate 回归、Web 静态和浏览器正反行为全部验证。

## 阻塞与交接

- 当前实现无阻塞。可选 Agent read-load 的旧查询数断言仍失败，详见下方证据；不据本任务宣称容量达标。
- 开发库已切换，原账号、商品、会话及其归属核对一致。仅 Go API、worker、dispatcher 更新为本任务程序，Vite/Pi 保持原进程。
- [本人账户管理](../saas-account-team.md) 与 [运营页面](../saas-ops-console.md) 消费本任务的直接归属和目标管理员接口，解除本次共享文件阻塞；这些完整页面与密码恢复不计入本任务完成范围。

## 审核与交付

- 用户 2026-09-08 明确将身份彻底收敛纳入本次。
- 交付定位：随本任务提交。
- 审核者 / 结论：主代理审核全部子代理 diff，并自审其消费者、管理 API、fixture、活文档和集成变更；身份实现通过验收。测试回归保留真实状态与隔离断言，未恢复默认首商家和角色兼容路径。
- Issue 结果：普通账号直接归属、独立 Operator、旧模型删除、迁移与前后端读者交付。完整运营 UI、操作审计、账户自助管理与商业容量继续按各自任务验收。

## 最终合同

- users.merchant_id 可空外键；普通用户必须有值且普通用户之间唯一。Operator 可保留唯一 bootstrap home merchant，也可为空；home 不是跨商管理权限。旧普通账号只在唯一有效归属且无历史跨商/共享冲突时映射，否则迁移整体失败保留数据。
- 会话 user 保留 is_operator，删除 memberships，新增 merchant:{id,name,status}|null。Principal.MerchantID 为 *string；MerchantView 使用 ID/Name/Status。
- Service.OwnMerchant(ctx,userID) 返回 *MerchantView；Service.RequireOwnMerchant(ctx,userID,merchantID) error 仅校验本人直接归属、用户/商家状态。HTTP.RequireOwnMerchant(param) 替代成员校验。工作商家只取账号直接归属，不读取切换 header；RequireWorkingMerchant 必须拒绝匿名或无归属。
- 管理员目标授权通过 HTTP.RequireOperatorMerchantTarget(param)，加载显式目标并设置该请求merchant scope；普通 ScopeMerchant 不改为全局。独立商品管理API只暴露读/编辑/删除，不挂付费生成。Operator商品管理无需拥有目标商家。
- 删除GET/POST /api/merchants 与成员撤销恢复路由；状态操作移到PATCH /api/ops/merchants/:merchant_id/status。注册/bootstrap不建Membership；旧角色与成员方法全部删。

## 集成证据

- 最终全 Go：独立 pf_identity_final 基名，`go test -json -count=1 -p 1 ./...` 退出 0；测试事件 1259 pass、23 skip、0 fail，包事件 36 pass、8 无测试包 skip、0 fail。日志 /tmp/pf-identity-runtime-final/go-test.jsonl。跳过为现有 opt-in/子进程辅助测试，未宣称收费 provider 或完整容量验证。
- schema：新库、重复、并发迁移、旧数据唯一映射、无 home 管理员、普通账号共享/歧义回滚与重试、空根归属拒绝均通过；迁移整体事务和 advisory lock 防止半迁移。
- Product/Auth：管理员显式目标商品读/事实编辑/删除、无 home 管理员、普通账号拒绝 ops、交叉 ID 404、密码/停用/会话撤销、匿名不受旧 admin_access_required 开关放行均通过。
- Graph：无请求商家 context 的 worker 图片成功后使用真实 Delivery Service 入队，以及 recovery 晋升 queued run，最终 dispatch 均保留正确商家；6 个 focused 测试通过、0 skip，包括缺失 run 和 lease/fencing。日志 /tmp/pf-identity-graph-scope-regression.jsonl。上述代码已包含在最终全 Go 中。
- 四包显式 fixture 补验：imagesession/library/platform-generation/metrics 全包及受影响 opt-in 数据查询/负载 gate 通过，无收费 provider；日志 /tmp/pf-identity-fixtures-gated.jsonl。
- Graph 与 Agent query-plan gate 通过。Agent read-load 恢复原 session 关联后，dock/product/turn_detail 通过；turns 内容与延迟通过，但总查询回调 600 与冻结断言 500 不符。逐条 SQL 追踪及 HEAD 对照确认 canvasFocusForTurns 与旧断言已并存；未修改预算或无关生产查询，该可选容量门仍为 FAIL。
- Web：执行者完整 `pnpm --dir web test:run` 为 104 files / 736 tests pass；`pnpm --dir web lint`、e2e TypeScript 检查、`just web-build` 含 bundle budget 均通过。身份完整门和消费浏览器结果保留在执行者会话输出，未另存原始日志；此前 policy 日志不冒充身份新门日志。
- 浏览器：身份 3/3 pass，覆盖登录/退出/换账号、直接自有商家、旧 SSE 迟到响应、普通账号拒绝设置、管理员状态目标路由、无 home 管理员仍可设置且不发空商家额度请求。受影响注册/SMTP设置/Agent创建/采用集合 37 pass、3 既有 live opt-in skip、0 fail；采用矩阵含 390/1440、四语言和明暗主题。使用 route mocks，非真实 SMTP/模型调用。
- 构建与开发切换：API/worker/dispatcher/migrate 四程序编译通过。开发库迁移前无活动作业、有效 Agent lease 或其他事务，普通账号归属无歧义；备份后执行 migrate 成功。迁移后 2 用户、2 商家、482 商品、5 auth sessions，归属与商品/会话 ID 摘要完全一致；memberships 表已删除。29282 healthz/session 正常，公开注册可用，29283 Vite 与 29284 Pi 健康接口保持 200。现场未另做真实账号登录或重复发信。
- `git diff --check` 与最终 `just docs-check` 通过；未提交 env、存储、构建或临时数据库。
