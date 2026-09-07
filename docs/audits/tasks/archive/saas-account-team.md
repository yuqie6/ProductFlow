# 任务：普通账号可管理本人资料、密码与会话

状态：完成
类型：实现
认领者：主代理-account-integration
认领于：2026-09-08T05:21:54+08:00
业务组：商家平台
父账本：merchant-platform.md
完成后可拆：无

任务状态、认领与提交遵循 [Issue 协议](../README.md)。文件 ID 保留引用，原团队范围已于 2026-09-08 取消。

## 问题来源

注册与密码会话已有实现，缺少本人账户管理入口。一个普通账号自有一个商家，不需要商家选择、团队管理或最后 Owner 转移页面。

## 做成什么样

`/account` 提供本人显示名、修改密码、会话查看和撤销，能明确退出。商家名称与个人偏好可按现有字段在该页面分区，不新增组织导航。密码恢复需独立用途的 SMTP 凭据，不用邀请或管理员手工转发代替；未实现时不显示假成功。

## 前置与并行

[身份收敛](saas-identity-convergence.md) 已交付直接 users.merchant_id、session.merchant 和显式目标商家管理员接口；认领时消费该合同，不恢复成员模型。

注册切片 cd78b456 已交付。[设计校准](saas-design-alignment.md) 已完成；认领时登记实际共享文件范围；不依赖工作区选择或团队功能。测试使用隔离 PG/browser，不动共享栈。

## 修改范围与所有权

认领后冻结 auth 账户/密码/会话用例、必要 schema、Account 页面、API/types/i18n 与回归；不修改 Graph/Pi 或收费政策。

## 合同

- 只修改本人；会话只列本人不透明 ID、创建/过期时间与 current，分页有界，不新增设备指纹。
- 改密码验证当前密码，同事务更新并撤销旧会话；与并发登录统一 User 锁序，不能留下旧凭据新会话。成功后清 cookie 回登录。
- 撤销当前会话立即退出；别人的 session 404，重复撤销本人 session 幂等。
- 不新增成员、角色修改、Owner 转移和管理员再认证机制。日常设置不再借旧二次解锁模拟管理员权限。
- 账号恢复尚未交付时显式说明缺口；实现其邮件流程前冻结用途、过期与防重放，复用已有 SMTP。

## 怎么验收

本人/他人正反访问，改密码与登录竞争，旧会话失效，正常/错误/空列表，桌面与手机可用。相关 Go/PG、Web focused tests、lint/build 和 docs-check；无付费模型或图片质量门。

## 阻塞与交接

- 原因：身份收敛已交付，本次共享文件阻塞已解除；认领时仍需检查并行占用。
- 解除条件：不适用。
- 跟进者：主代理。
- 交接：主代理完成验收，独立API/preview进程与live数据库已清理；共享开发栈未重启。

## 证据

- 命令 / 日期 / 结果：2026-09-08，auth全包race通过；新增退出失败重试focused race通过；settings/schema/API包通过。全部使用独立PG，schema含恢复表/索引迁移回归。
- 交付定位：随本任务提交。
- 审核者 / 结论：主代理审核子代理全部diff并接管收口。修复并发测试通道死锁、GORM测试绑定、同时间会话排序断言与交接重复更新；真实会话撤销失败不再返回假成功。
- Issue 结果 / 剩余缺口：账户后端与真实SMTP/API集成通过；页面由 [前端交付](saas-account-interface.md) 提供。个人偏好、管理员商品完整界面、整仓Go/发行门与共享开发栈更新未作为本次通过项。

## 本轮排他分工

account_backend 负责 Go 本人账户、密码、会话与 SMTP 密码恢复用例、路由、必要 schema 与回归。主代理持有活文档/Git/最终集成；前端由单独登记的 Astra 任务消费冻结接口。先报告当前实现与建议 wire 合同，再经主代理冻结后实施；不写 web、Go imagesession、Graph/Pi/计费。隔离 PostgreSQL，独占前缀 pf_account_0908；不重启共享服务。账户成熟度要求包含可实际收取的密码恢复邮件与一次性凭据，不只留缺口文案。语言主题与商家资料随后由现有偏好任务消费账户合同，不在此切片改 preferences。

## 冻结 wire 与恢复合同

- GET/PATCH `/api/account` 返回 `{user:{id,email,display_name,is_operator},merchant:{id,name,status}|null}`；PATCH 只接非空且不超过既有 160 字符限制的 display_name。
- POST `/api/account/password` 接 `{current_password,new_password}`，成功 `{ok:true}` 并撤销所有会话、清 cookie。登录与改密统一 User -> AuthSessions 锁序。
- GET `/api/account/sessions?after=&limit=` 默认20上限100，返回 `{items:[{id,created_at,expires_at,current}],next_cursor:string|null}`，只列本人有效会话；DELETE `/api/account/sessions/:id` 返回 `{ok:true}`，重复撤销本人幂等，他人/不存在404，撤销当前清cookie。
- POST `/api/auth/password-recovery/request` 接 `{email}`；有效格式的请求统一202，返回 `{challenge_id:string,expires_in_seconds:600,resend_after_seconds:60}`。已知、未知、disabled 和单次 SMTP 投递失败保持同一响应形状，失败/未知的 challenge_id 无可用恢复凭据。该响应仅表示请求已受理，前端不得显示“已发送成功”；显示条件式查收/重试说明。SMTP 未配置这种全站不可用可在查账号前统一503。投递失败按现有日志记录受控原因，不输出验证码/凭据；不增加管理员模拟解锁或向用户泄露账号是否存在。
- POST `/api/auth/password-recovery/confirm` 接 `{email,challenge_id,code,new_password}`，成功 `{ok:true}` 并清cookie；独立六位随机验证码、600秒过期、60秒重发、5次错误上限、只存hash、一次消费，错误尝试必须提交计数，不能随业务错误回滚。IP/邮箱限流对所有邮箱一致，不由存在性改变响应。
- User -> RecoveryChallenge -> AuthSessions 锁序；成功恢复同事务改密码、消费/失效恢复凭据、撤销全部用户会话。多个在途恢复码不得在一次成功恢复后再次覆盖密码。单次发送失败不得留下可使用的新码；复用现有SMTP传输与配置。
- account_backend 另获 contracts/http-routes.json 与必要路由合同测试写入权（代码合同，不属活文档）；前端独立任务消费以上冻结端点。错误 detail 的展示复用当前 API 错误投影；新增稳定错误码需要与主代理/前端同步，不随意变更成功 shape。
- 恢复确认公开错误统一：未知邮箱/不存在challenge、disabled、错码、过期、已消费和超尝试均为400 `invalid_recovery_code`，提示验证码无效或已过期并可重新申请；内部原因可分别测试。不能借request生成的真假challenge再通过confirm响应枚举账户。请求格式错误和与邮箱存在性无关的限流保持既有语义。

## 主审补充的同根约束

重复恢复请求的challenge ID行为不能暴露邮箱是否存在；仅同shape不足以防止“已知重复ID、未知随机ID”的两次请求枚举。生产恢复ID密钥复用SESSION_SECRET，测试显式提供，不加固定密钥fallback。重复请求对外声明600秒时，持久化有效期必须与该声明相符。SMTP失败清理使用有界独立取消上下文，且绑定本次发出的code_hash，不能由旧请求晚到失败消费后来重发的码。主动修改密码同样消费本用户既有未消费恢复码，避免改密前的凭据覆盖新密码。以上与原恢复防重放/会话撤销属于同一账户边界，由account_backend实现和回归，主代理复核。

执行交接：account_backend连续长时间无响应，主代理中断其轮次并收回写入权，保留其实现，接管最终修正、Go/PG回归和真实SMTP/浏览器集成。子代理不再写本范围。

## 最终验收证据

- 本地证据根 `.debug/saas-account-live-20260908/`：`go-auth-accepted.log` auth全包race（56.830s）；`go-logout-accepted.log`退出失败重试race（6.168s）；`go-tests.log`同时保留早期auth测试失败和settings/schema/API通过结果；`backend-source-sha256.json`与`binary-sha256.txt`固定代码/运行物。
- `attempt-2`真实SMTP注册与恢复邮件均由IMAP只读收取；普通账号直接归属商家、独立Operator merchant=null、跨用户撤销404、资料投影同步、恢复成功后的两旧会话401/旧密码401/重放400均通过。本轮浏览器脚本误等/home而非实际/products，保留失败，未当浏览器通过。
- `attempt-4-browser`使用独立普通账号夹具、真实Go API及生产Web构建，验证登录、资料保存、会话撤销、改密退出、新密码登录、公开恢复页；浏览器改密使独立已认证API会话失效。390/1440无溢出、无pageerror，截图与日志保存。该轮未重复发信，SMTP和浏览器证据按各自范围组合。
- 新表由productflow-migrate创建；部署需迁移和重启API。本次未动共享开发DB/29282/29283，未运行真实模型、未推送或发布。独立集成放大了测试限流额度，不据此宣称容量或限流门。此前脚本Origin缺失与夹具混用失败同样保留。

提交组织：后端与Astra页面为排他委派切片，消费同一冻结账户合同。实际SMTP/API/浏览器集成完成后，两切片与用户指南按同一账户功能提交，避免形成有页面无API或已归档文档缺实现的中间交付。公开回执/错误/ID行为一致，但SMTP调用同步完成，本次未提供恒时响应或异步邮件队列保证。
