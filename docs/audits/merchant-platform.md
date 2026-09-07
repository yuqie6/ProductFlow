# 商家平台组

本组于 2026-09-07 按用户确认的可自托管多商家 SaaS 目标设立。负责同一部署内不同商家的身份、成员、数据权限、商业额度和运营控制。身份/隔离 B0–B10 和部分额度能力已经交付，完整账户与运营体验尚未交付；测试夹具不构成真实商家开放证据。产品决策、实体与角色见 [正式版总纲](../ROADMAP.md)，本文件拥有组内合同、交付顺序与验收结论。

## 责任与边界

| 本组拥有的结果 | 覆盖范围 | 交接 |
|---|---|---|
| 操作者身份可靠 | User、会话、公开注册、恢复、Membership、角色、撤销 | 前后端共同交付；不复用 lease owner 充当用户 |
| 所有商家资源隔离 | 商品、事实、Graph、素材、配方、Session、任务、下载、事件与缓存 | 一项隔离切片负责完整必要链路，不逐模块转单 |
| 受理后的任务归属稳定 | 入队、重试、取消、Agent 合同、内部回调与确认 | 复用平台可靠性的 lease/outbox/fencing，不建第二套执行器 |
| 商家权益与消费可解释 | 额度授权、价格版本、预留/结算/核对、运营调账 | 平台可靠性提供持久调用事实和执行约束；两组不得各造一本余额账 |
| 运营与商家权限分开 | 商家启停、成员恢复、密钥权限、支持会话与审计 | 部署、备份、发行与资源容量归平台可靠性 |

## 必须保持的合同

- MP-01：Merchant 是租户，User 通过有效 Membership 访问；Brand/Product/渠道都不能替代商家根边界。站点 Operator 与商家 Owner 权限不同。
- MP-02：资源在服务端验证所有权与操作权限，来自浏览器或 Agent 的商家 ID 不授予权限。共享管理员 cookie 不能回退为正式多商家访问方式。
- MP-03：根记录、子资源引用、媒体/缩略图/交付包、SSE、缓存、后台作业与 Go→Pi 工具范围保持同一商家。UUID 不作为授权依据。
- MP-04：任务受理时固定发起者、商家与操作授权快照，执行时检查相应商家运行策略；切换商家不改变原任务归属。成员撤销停止其读取、订阅和新动作，后续确认重新鉴权。
- MP-05：每个可消费入口有同一用量归属和幂等合同；预留、实际尝试、未知、释放、结算和人工调整可追踪。未知用量不是零消费，网络超时不自动授权重跑。
- MP-06：首版为共享 PostgreSQL、显式归属、bootstrap 后的公开邮箱注册和实例级 provider 凭据。商家 BYOK、跨商家分享和自动收款按独立就绪条件扩展；推荐和返利本阶段不实现，未来另行决定。
- MP-07：完整隔离门未过前不能对互不信任的商家开放。分批代码提交不构成第二商家可用的发布声明。

<a id="saas-platform-design"></a>

## 公开注册试用版 SaaS 平台设计

本节为目标合同，尚未整体实现；不回写历史 MP-A/MP-B 验收。公开注册切片已实现，并已有真实浏览器、SMTP/IMAP 收取及 Web 证据；整体 SaaS 和外部发布仍按单独授权与完整合同处理。页面与完整流程见 [ROADMAP §15](../ROADMAP.md#15-公开注册试用版-saas-设计交接)。具体 task 状态只在看板维护。历史邀请设计仅作档案，不是当前入口。

### 身份与范围不变量

| ID | 目标合同 | 当前基础与必要增量 |
|---|---|---|
| SP-01 | User 会话证明本人；公开注册先验证邮箱，再创建普通账号与其自有商家 | 复用 Users/AuthSessions；注册 challenge、密码和创建事务已实现，并有浏览器、SMTP/IMAP 收取与注册会话证据 |
| SP-02 | 工作商家是每标签页的明确选择，每请求服务端验证；无成员的 Operator 可管理站点 | 复用 header 和 merchant context；补 sessionStorage 选择、原生媒体/SSE query 与非商家路由分离 |
| SP-03 | 成员/角色 mutation 的授权、最后 Owner 检查和变更在同一商家事务中串行 | 扩展现有 Membership；所有相关 writer 统一锁序，不用各自 actor→target 锁造成互锁 |
| SP-04 | 个人、商家、实例配置分别拥有字段，不使用统一的任意 JSON 配置入口 | 个人偏好新增强类型存储；商家资料与现有 app_settings 分离 |
| SP-05 | Operator 读取运营元数据不等于取得商家业务身份；支持访问必须有用途、期限和审计 | support.go 当前 501，仅草案；运营列表不得临时塞一个 merchant context 绕过支持授权 |
| SP-06 | 商家消费仍由唯一 quota account/hold/event 解释，界面不能改变扣费事实 | 复用已交付 Reserve/Settle/Release/unknown、price；补分页读取和操作后回读 |
| SP-07 | 新消费受商家状态和当前权限约束，已发出的调用据事实结算；停用不隐式退款 | 沿现有 queue/worker merchant snapshot 逐入口验证；未完成的跨执行器行为单独交付 |
| SP-08 | 任何新商家开放必须满足完整试用合同；内部双商夹具和身份门通过不自动解除限制 | bootstrap 后公开注册创建普通 User、自有 Merchant、Owner Membership 和试用额度；SMTP 与注册事务已有回归证据，外部发布仍需单独授权 |

### 权限矩阵

本表约束新增页面与 API；既有余额/单价对成员可读的合同保留。普通 Operator 身份不自动拥有商家生产权，拥有 Membership 时仍按该商家角色判断。

| 操作 | Owner | Editor | Viewer | Operator（无 Membership） |
|---|---|---|---|---|
| 本人资料/偏好/密码/会话 | 本人 | 本人 | 本人 | 本人 |
| 本商商品、素材与生产 | 读写 | 读写 | 只读和允许下载 | 拒绝 |
| 商家名称/时区 | 读写 | 只读 | 只读 | 运营元数据只读 |
| 团队成员、角色、移除/恢复 | 读写 | 拒绝 | 拒绝 | 不隐式代管；账号恢复另走运营入口 |
| 余额、预估单价 | 只读 | 只读 | 只读 | 运营只读 |
| 商家完整消费流水 | 只读 | 拒绝 | 拒绝 | 运营只读 |
| 调账、unknown 裁定、商家启停 | 拒绝 | 拒绝 | 拒绝 | 再认证、原因与审计 |
| provider/密钥/实例配置 | 拒绝 | 拒绝 | 拒绝 | 既有受控权限 |
| 非 Operator 账号恢复签发 | 拒绝 | 拒绝 | 拒绝 | 再认证、原因与审计 |
| 商家内容支持读取 | 本商原权限 | 本商原权限 | 本商原权限 | 仅未来有效支持会话允许的只读范围 |

商家停用后，Owner 可读取商家资料/团队/额度以处理问题；普通生产面只读及已有下载按现有停用策略逐项冻结，不新增消费或成员变更。账号与运营入口始终能明确处理停用状态。授权拒绝遵循已有边界：未登录 401、已知范围操作无权 403、跨商资源 404、冲突 409；不为每页新建一套错误格式。

### 工作商家与会话

- 个人身份保持现有 HttpOnly cookie + PostgreSQL AuthSessions。工作商家选择存当前标签页 sessionStorage，key 绑定 user；它是选择值，永不授权。多商家且未选时显示选择页，不能默认展示任意第一商家数据。
- 普通 fetch 沿 `X-ProductFlow-Merchant-Id`；SSE、img、下载和跨页深链接沿 `merchant_id` query，后端统一校验。重复 query、多值或 header/query 冲突拒绝；个人/认证/运营路由不要求工作商家。选择值不是 secret，密码和短期 token 不得进入 query。
- 切换保留已受理作业归属；未保存草稿有明确保留/放弃决定。先取消旧读取、关闭旧订阅并使旧回调失效，再加载新商数据，防止 query、mutation、toast、导航和原生图片 URL 串商。两标签页独立，退出和账号变更清旧身份缓存。
- 撤权提交后的新授权检查使用当前权利；事件数据 flush 前重验，静默长连接最长 15 秒终止。已经发送的文件 bytes 不承诺收回，后续请求不得继续访问。活跃流与空闲流分别验收，不用短 GET 测试代替。

### 账号、团队与恢复

首版以 bootstrap 后的公开邮箱验证码注册为用户创建入口。注册前必须完成部署者初始化；用户请求验证码、在有效 challenge 内设置密码，成功后原子创建普通 User、该用户自己的 Merchant、Owner Membership 和试用额度。六位验证码有效 10 分钟，重发间隔 60 秒，每个 challenge 最多 5 次错误验证，重发使旧 challenge 失效；验证前使用统一凭据预算。已有邮箱和密码登录保留，不同用户会话不被静默替换。SMTP 或注册事务失败不能假报成功，同邮箱并发注册依唯一约束和事务结果收敛。

团队初始管理范围为分页成员列表、成员角色修改、移除/恢复。Owner 可增加 Owner；最后一位不能被移除或降级。统一 Merchant 根锁负责商家成员不变量；同时涉及 User 的操作使用 User→Merchant→Membership 顺序，补查现有所有相关 reader/writer。新增成员的直接已有用户关联另行设计，不以邀请、短期 token 或人工转发作为替代入口。恢复已撤销 Membership 不等于撤销账号停用。

本人可改显示名、密码并撤销会话，邮箱首版只读。改密码与 Login 统一 User 行串行化凭据版本观察，不能在撤销事务后用旧密码创建有效 session；成功后撤销全部旧会话、清 cookie、回登录，不自动签入。账号密码恢复、修改邮箱和相关邮件流程尚未实现；未来恢复使用与注册用途分离的 SMTP 短期凭据，成功后撤销全部旧会话，不授予 Membership 或 Operator。不能把该未来合同写成已交付能力。旧邀请接口、token、schema 和 reader/writer 退役，不保留兼容入口。

Operator 再认证有效期 10 分钟，绑定本人 session，由服务端记录；不是前端布尔值。未来恢复非 Operator 账号必须记录原因，不能签发另一个 Operator 的恢复凭据；该能力当前未实现，也不以 Operator 手工链接作为默认产品流程。唯一 Operator 无法登录属于部署者恢复流程，首版产品 API 不增设后门；需要对应部署任务时单独设计离线受控命令。

验证码只通过请求 body 交换，原始密码、验证码和 SMTP secret 不进入日志、URL、持久浏览器存储或普通导出。SMTP 未就绪时邮件交换与注册提交不可用，登录页注册模式仍可显示；真实 SMTP/IMAP 收取与注册流程已经验证，外部发布仍按部署授权处理。技术依据复核于 2026-09-07：[OWASP 账号恢复](https://cheatsheetseries.owasp.org/cheatsheets/Forgot_Password_Cheat_Sheet.html)、[会话管理](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html)。

### 配置字段与生效时机

| 所有者 | 首批字段/现有对象 | 读写与覆盖 | 生效边界 |
|---|---|---|---|
| User | display_name；新增 locale/theme 偏好 | 本人修改；locale/theme 复用现有枚举；服务端默认 zh-CN/system | 个人界面；登录后取本人偏好，不自动上传共享浏览器旧值 |
| Merchant | name、time_zone | Owner 修改；时区默认 UTC，用 IANA 名称；成员只读资料 | 日期显示/筛选；不改持久 UTC 或已确认的生成输入 |
| Brand/视觉方案 | 现有 Brand 与 visual_system | 沿已交付选择/继承合同 | 品牌风格不能携带旧商品身份，不重造配置层 |
| Product/图节点 | facts、语言、规格和已有覆盖 | 沿现有 Graph/DeliverySpec 合同 | 影响被用户确认的后续操作；实例/个人偏好不能静默改运行 snapshot |
| Instance | app_settings、provider、容量、注册 SMTP 字段 | 已登录 Operator；普通 User 与匿名请求拒绝；SMTP password 按 secret 处理，不回显/导出 | 按原配置读者生效；登录页注册模式在初始化后可见，SMTP 未就绪时只禁用邮件交换和注册提交，不复制为每商家一份 provider |

个人偏好与商家资料用强类型 PATCH、expected_revision 和有界字段；过期返回 409，前端保留草稿供比较。缺偏好行返回默认，不在 GET 落库；首次并发创建按 revision=0 和唯一约束收敛。新增“默认图种/尺寸/输出语言”等生产设置必须追到实际创建/运行读者后另行设计，不仅增加表单和 JSON。

注册 SMTP 字段闭集为 `smtp_host`、`smtp_port`、`smtp_security`（`starttls`/`tls`）、`smtp_username`、`smtp_password`、`smtp_from_address`、`smtp_from_name`。开发栈从 `.env.dev` 的 `SMTP_HOST`、`SMTP_PORT`、`SMTP_SECURITY`、`SMTP_USERNAME`、`SMTP_PASSWORD`、`SMTP_FROM_ADDRESS`、`SMTP_FROM_NAME` 读取启动默认；设置页保存的数据库值覆盖同名环境默认，恢复默认删除数据库覆盖并回到当前环境值。公开注册的邮件交换与提交由 `registration_available` 控制，只在 bootstrap 完成且 SMTP 可用时为 true；登录页注册模式在初始化完成后仍可显示，SMTP 未就绪时只禁用上述交换与提交。Operator 设置与注册读取共享同一数据库权威。

### 运营、消费与审计

运营控制台以商家列表→元数据详情→额度/异常操作为主线。首批只读 metadata、balance、hold/event；不得给 Operator 列表拼接原图、prompt、对话或材料下载。按商家/status/日期分页，默认 20、最大 50，cursor 用稳定 created_at/id；重要过滤必须有查询计划证据。

消费页面区分可用、预留、已结算与待核对；内部单位不显示为法币。来源应来自持久关联，没有友好来源时显示操作号和“来源未记录”，不能解析任意 key 猜商品名。详情保留价格版本、发生时间、状态、预留与结算单位；不向普通用户展示 provider 密钥或内部 prompt。

调账与 unknown 裁定复用现有 service、幂等键和 ledger。Operator 填原因并再认证，确认面显示目标与金额；丢响应沿同 key 重试/查询。政策继续沿现有 TTL 全额 Settle 和明示 actual_units，界面不偷偷把 unknown 变成免费 Release。人工裁定和扫描竞争必须唯一收敛；任何政策改动另行审查，不在 UI 任务顺手变化。

身份/运营审计使用一张追加事实表，记录 actor、目标 user/merchant、action、时间、结果、必要前后值和原因；成功 mutation 与审计同事务。额度事件本身是金额权威，审计可引用事件 id，不能复制可变余额。失败请求写脱敏安全日志，禁止密码/token/整份敏感请求进入记录；支持读取的审计写入失败必须拒绝数据返回。

### 后续完整结果的固定边界

以下未发布为首批可执行任务。开发协调者在前置已交付后补充精确 schema/API/资源范围并发布，未完成前 S2 仍有缺口。

| 结果 | 已定设计 | 必须先有的输入 |
|---|---|---|
| 受审计支持读取 | Operator 10 分钟再认证；指定 merchant、reason、只读资源范围；支持会话最长 30 分钟、可提前结束。所有读取先校验授权并记录审计；不伪造 Membership，不允许生成/修改/导出整商数据。未明确允许的媒体/对话拒绝 | 统一审计、运营页和逐资源读取矩阵；不是给所有业务路由统一跳过鉴权 |
| 后续商家开通 | 公开注册在 bootstrap 后验证邮箱，原子创建普通 User、自有 Merchant、Owner Membership 与试用额度；未验证或 SMTP 未就绪时不创建任何业务对象。名称不唯一，不能按名称去重；不得自动给 Operator 生产 Owner | 注册 challenge、SMTP 可用性、完整事务和额度原子建账；公开外部发布仍需单独授权，不通过手工邀请替代 |
| 商家停用完整执行语义 | 禁止新消费；尚未发出的 provider 尝试停止；已发出调用据事实落成功/unknown 并结算；保留交付与支持处理入口 | 所有消费执行器与授权快照枚举；测试停用与 admission 并发，不用只关前端按钮验收 |
| 商家首页/运营概览 | 有界投影与统一 metric 口径；只读数据，点击待办返回原业务对象；运营聚合不暴露商家内容 | 工作区选择、消费/任务明细、时区、已可用 API；原型与指标表由体验组拥有 |

首批实施包：[身份入口](tasks/archive/saas-auth-entry-security.md)、[工作商家](tasks/saas-workspace-context.md)、[账户团队](tasks/saas-account-team.md)、[偏好设置](tasks/saas-preferences-settings.md)、[运营额度](tasks/saas-ops-console.md)。独立 [容量基线](tasks/saas-multimerchant-capacity-baseline.md) 属平台可靠性。共享 auth/schema/Web 壳层默认串行；只有文件、输入和资源均独立才并行。详细步骤由开发者按真实调用链安排，不为每个 endpoint 单独建任务。

2026-09-07 身份入口合同历史快照（旧邀请流，保留原证据，不代表当前入口）：已有账号邀请要求身份证明，凭据入口共享 Redis 原子预算，浏览器写入校验来源，登录显示冷却。Web 728 测试及 17 浏览器用例通过；全部后端包已有通过结果，其中 Agent/额度在独立新测试库补跑通过，原复用库全量运行的失败保留于任务证据。SP-01 对应缺陷已修复；工作商家选择、账户团队及真实第二商家开放尚未交付，不重签 R1 或 S2。

## 组内序列

1. [merchant-isolation-contract](tasks/archive/merchant-isolation-contract.md)：逐入口、表/根资源、worker、媒体和 Agent 工具建立覆盖清单，固定所有权、角色矩阵和可分别验证的实施批次。**本任务已交付覆盖矩阵与批次（见下文）；不宣称隔离已实现。**
2. [身份骨架 B0](tasks/archive/merchant-identity-skeleton.md) 已交付。[根归属 B1](tasks/archive/merchant-root-ownership.md) **已交付**（根表 `merchant_id`、回填、查询过滤、跨商 404）。[商品链 B2](tasks/archive/merchant-product-chain.md) **已交付**（子链读写/下载隔离）。[Graph+配方 B3](tasks/archive/merchant-graph-recipe.md) **已交付**（changeset/run/SSE/recipe apply）。[会话生图 B5](tasks/archive/merchant-image-session.md) **已交付**。[图库绑定 B4](tasks/archive/merchant-library-binding.md) **已交付**（from-product/session、workflow sync）。[交付与局部编辑 B6](tasks/archive/merchant-delivery-localedit.md) **已交付**。[Agent 工具链 B7](tasks/archive/merchant-agent-tools.md) **已交付**。[队列与前端 B8](tasks/archive/merchant-queue-frontend.md) **已交付**。[运营面 B9](tasks/archive/merchant-ops-surface.md) **已交付**。[双商隔离门 B10](tasks/archive/merchant-isolation-gate.md) **自动化证据通过**；总纲 **R1 已通过**（见 [merchant-r1-close-ruling](tasks/archive/merchant-r1-close-ruling.md)）。**运营邀请第二商另决（历史基线，邀请流已退役）**；公开注册切片另行验收，未放开产品 `CreateMerchant`。
3. 在相同身份合同上交付商家额度与 Operator 运营控制，复用平台提供的固定消费事实；缺字段或原子预留入口时将其纳入完整因果切片。
4. 双商家正反用例、成员撤销和混合负载验收；把受影响身份/授权合同交给 Agent 质量独立固定题目，自进化消费已固定输入。

现阶段：B10/MP-B 与总纲 **R1 已裁定通过**（夹具双商口径）；冻结基线历史口径为 **在运营明确邀请前仍不得对互不信任的第二商家开放产品注册**（`CreateMerchant` 保持 409，旧邀请流已退役）。当前公开注册切片已由 SMTP、注册事务及浏览器/SMTP/IMAP 证据验证，外部发布仍需单独授权，不把历史 409 改写成已开放。[总纲 R2/R5 对照刷新](tasks/archive/roadmap-r2-r5-refresh.md) 已归档（两门仍未通过）。[新商家额度引导](tasks/archive/merchant-mp-c-bootstrap-quota.md) 已交付（`QUOTA_TRIAL_UNITS`；≠R5）。[入口单价展示 B0](tasks/archive/merchant-mp-c-price-display-b0.md) 已交付图会话 Generate 前读 `GET …/quota/price` 展示预估扣减（缺失/无效禁用；≠R5）。[工作台 Graph 确认单价](tasks/archive/merchant-mp-c-price-display-workbench.md) 已接 Agent 工作流执行确认面（`graph.image_generation`；≠R5）。[unknown 到期](tasks/archive/merchant-mp-c-unknown-expiry-b0.md) 已交付（Op `holds/resolve` + TTL 全额 Settle；≠R5）。

## 验收与现状

| 门 | 完成条件 | 当前 |
|---|---|---|
| MP-A 身份 | 多角色、邀请/撤销/恢复、最后 Owner 与并发变更、会话失效可验证 | **通过**（B0，见 [merchant-identity-skeleton](tasks/archive/merchant-identity-skeleton.md)；邀请/角色/撤销/最后 Owner 自动化） |
| MP-B 隔离 | A/B 商家合法操作成功，所有交叉读写、导出、事件、Agent 与后台路径拒绝；查询与引用一致性约束有测试 | **通过**（B10，2026-09-07，见 [merchant-isolation-gate](tasks/archive/merchant-isolation-gate.md)）；**未开放**第二互不信任商家产品上线；≠ MP-C/MP-D |
| MP-C 商业额度 | 并发争用、幂等、重试、取消、unknown 和调账不会重复结算；每项能解释费用来源 | **B0–B4 + localedit + source-note + 价格目录骨架**已交付（+[localedit](tasks/archive/merchant-mp-c-wire-localedit.md)+[source-note](tasks/archive/merchant-mp-c-wire-source-note.md)+[price-catalog-b0](tasks/archive/merchant-mp-c-price-catalog-b0.md)）。**新商家首次建账试用种子**（`QUOTA_TRIAL_UNITS`，默认 100；[bootstrap-quota](tasks/archive/merchant-mp-c-bootstrap-quota.md)）。**图会话 Generate 入口展示单价**（读价格目录；[price-display-b0](tasks/archive/merchant-mp-c-price-display-b0.md)）。**工作台 Agent 工作流确认展示 `graph.image_generation`**（[price-display-workbench](tasks/archive/merchant-mp-c-price-display-workbench.md)）。**unknown 到期运营策略**（`QUOTA_UNKNOWN_HOLD_TTL` 默认 72h 全额 Settle；Op `POST .../quota/holds/resolve`；禁止超时 Release；[unknown-expiry-b0](tasks/archive/merchant-mp-c-unknown-expiry-b0.md)）。**总纲 R5 未通过**（见 [merchant-r5-close-ruling](tasks/archive/merchant-r5-close-ruling.md)）：其它收费入口未全覆盖展示；≠真实支付 |
| MP-D 运营 | 运营密钥不进入商家上下文；停用、支持访问、数据导出有明确权限与审计 | 未实现（A8 合同草案仅） |

**总纲 R1：** **通过**（2026-09-07，见 [merchant-r1-close-ruling](tasks/archive/merchant-r1-close-ruling.md)）。证据口径：测试夹具双商 + 多角色 + 成员撤销 + HTTP/资源/队列/事件/Agent/后台交叉拒绝；**≠** 产品上线第二互不信任商；**≠** MP-C/MP-D。

冻结基线：`d6709c4aacb2e26bb30ab70a99d08b1dca05f487`（2026-09-07）。B0–B10 落地后：User 密码会话 + 根表 `merchant_id` 与查询过滤、跨商统一 404、双商自动化门与 R1 裁定已过；`app_settings` 仍为实例级；产品 `CreateMerchant` 仍拒第二开发商直至运营邀请。

---

## 源码枚举证据（矩阵输入）

以下命令在冻结 HEAD 上执行；矩阵入口必须能反查到这些清单，禁止用总纲条目冒充枚举。

| 类别 | 命令要点 | 计数 |
|---|---|---|
| HTTP 注册 | `go/cmd/productflow-api/register.go` 挂载含 `brand`（及既有 auth/settings/product/…/visualsystem/localedit/agent） | **业务路由 223**：相对冻结枚举 214 + Brand B0 的 4 条 + 价格目录 2 条 + Brand B1 的 2 条（`GET/PUT /api/v3/products/:id/brand-selection`）+ unknown resolve 1 条（`POST /api/ops/merchants/:id/quota/holds/resolve`）；其余分区仍以 register.go 为准 |
| Schema 模型 | `rg 'TableName\(\)' go/internal/platform/db/schema/*.go` | **71 表**（冻结枚举曾只计 `models.go`=57；现含 identity 5 + MP-C B0 `merchant_quota_*` 3 + Brand B0 `brands` 1 + 价格目录 `quota_price_*` 2） |
| Agent tool | `rg 'name: "' agent-service/src/tool-manifest.ts` | **25 工具名**（含 skill/ask/context_injection） |
| 异步 Actor | `go/internal/platform/queue/actors.go` | 信封 `run_async_dispatch`；Actor：`run_workflow_graph_run`、`run_image_session_generation_task`、`run_delivery_rendition_job`、`run_local_image_edit_task`、`run_agent_turn_sync` |
| 下载 | product/library/imagesession 的 `*/download`、商品 ZIP、delivery ZIP、internal `*/content`；变体经 `media.ServeVariant`（`?variant=`） | 见矩阵「下载/媒体」 |
| SSE | graph run events、image-session events、agent turn events（product/global）、agent control events；notify 通道 `productflow_{control,run,turn,image_session,dispatch}` | 6 类浏览器/SSE 入口 + 5 notify 通道 |
| 前端 query | `web/src` 中 `queryKey:`；订阅见 `EventSource`（agent conversation runtime、image-session、graph run） | 见矩阵「前端」列；商家切换时必须以 merchant 为 cache/SSE 边界前缀或整表清空 |

反查方法：新增路由/表/工具/Actor 时对照本节计数与下方矩阵分区；遗漏则阻断对应实施批次完成声明。

---

## 角色缩写（贯穿矩阵）

| 代号 | 含义 | 源 |
|---|---|---|
| Op | 站点 Operator：实例配置、商家启停、额度调账、支持会话 | 总纲 7.2 |
| Own | 商家 Owner：成员、商家资料、生产与导出 | 总纲 7.2 |
| Ed | 商家 Editor：本商家生产写路径 | 总纲 7.2 |
| Vw | 商家 Viewer：本商家只读与允许的下载 | 总纲 7.2 |
| Int | 内部服务令牌（Go↔Pi）；证明调用方服务身份，**不能**替换任务商家 | MP-02/04 |
| Sys | 实例系统进程（health/metrics/dispatcher）；无商家上下文 | MP-06 |

写路径默认 Own+Ed；只读含 Vw；成员管理仅 Own；provider/密钥/导入导出仅 Op。矩阵「角色」列写最小允许集；服务端仍按 Membership 实时校验。

---

## 持久对象所有权分类

### 根对象（须显式 `merchant_id` 或等价商家归属列 + 查询过滤）

| 表 | 当前 | 目标归属 | 说明 |
|---|---|---|---|
| *(新)* brands | 不存在 → **B0 已建**（[merchant-brand-entity-b0](tasks/archive/merchant-brand-entity-b0.md)） | 商家根 Brand | 总纲 §6.3；可选 `visual_system_id` 挂接本商家方案；商品经 `products.brand_id` 选定（[merchant-brand-product-select-b1](tasks/archive/merchant-brand-product-select-b1.md)）；继承合并读 Brand 挂接方案**当前（最新）版本** style/colors；≠跨商分享；≠完整多品牌 UI；≠R2 |
| *(新)* merchant_quota_accounts / holds / events | 不存在 → **B0 已建** | 商家商业额度账本 | MP-C B0；与 `agent_model_invocations.usage_source` 平台调用事实分离 |
| *(新)* quota_price_versions / quota_price_entries | 不存在 → **价格目录 B0 已建**（[merchant-mp-c-price-catalog-b0](tasks/archive/merchant-mp-c-price-catalog-b0.md)） | 平台价格版本目录（非商家根） | 种子默认 `pv-placeholder-v0`；Reserve 校验版本；≠法币/支付 |
| products | 无商家 | 商家根 | 所有商品链由此证明；可选 `brand_id` → 本商家 `brands`（ON DELETE SET NULL；跨商选定拒绝） |
| media_library_folders / media_library_tags / media_library_assets | 实例全局 | 商家根（商家共享图库） | 总纲「商家共享图库」；禁止跨商家 list |
| media_library_upload_keys / media_library_collection_keys | 无商家 | 随商家（或经 product/session 证明后冗余商家） | 幂等键须含商家，防跨商家碰撞 |
| image_sessions | 无商家 | 商家根 | 连续生图独立根；attach 到 product 时双方同商家 |
| workflow_recipes / workflow_recipe_versions | 全局列表 | 商家根 | 配方无商品身份但可复用；首版不跨商家共享 |
| visual_systems / visual_system_versions | 无商家 | 商家根（对接 Brand） | 总纲 Brand；versions/references 随父；Brand.`visual_system_id` 为品牌层风格来源（当前版本）；商品另可钉 `product_visual_selections` |
| agent_sessions / agent_tasks / agent_conversations | 有 product/global scope，无商家 | 商家根；global 会话仍属单一商家 | 「global」= 商家内全局 Agent，非跨商家 |
| library_organization_drafts | 随 conversation | 经 conversation→merchant | 可冗余 merchant_id 一致性约束 |

### 子对象（经父 FK 证明商家；可选冗余 `merchant_id` 须有一致性约束）

| 表族 | 父根 | 验证点 |
|---|---|---|
| product_fact_set_versions、product_image_assets、product_asset_folders | products | 用例加载 product 时校验 Membership；asset 直链下载按 asset→product→merchant |
| workflow_graphs 及 nodes/edges/groups/runs/events/artifacts/proposals/provider_effects/operation_groups/node_runs | product（经 graph.product_id） | URL product_id 与 graph 归属一致；run SSE 同校验 |
| workflow_recipe_applications | product + recipe | 两侧同商家 |
| workflow_media_library_assets | workflow + media_library_asset | 两侧同商家 |
| delivery_rendition_jobs | product_image_asset→product | job 直读/重试按 job→asset→merchant |
| local_image_edit_* | product | task 与 source/reference assets 同商家 |
| image_session_assets / rounds / generation_tasks / provider_effects | image_sessions | 下载按 asset→session→merchant |
| agent_turn_*、agent_tool_mutations、agent_workflow_run_requests、agent_page_context_snapshots、agent_model_invocations | conversation/task→merchant | internal tool 只能触达合同商家 |
| visual_system_version_references | visual_system_version | 随 Brand/商家 |
| async_dispatches | Actor aggregate | **不**另开租户；载荷/aggregate 解析后校验商家快照（见队列规则） |

### 实例级（保留无商家字段；仅 Op/Sys）

| 表/入口 | 原因 |
|---|---|
| app_settings、provider_profiles、provider_bindings | 首版实例级 provider（MP-06）；商家只选被允许能力 |
| media_objects | 内容寻址字节；授权在引用方（product/library/session asset）；首版不做跨商家去重 |
| /healthz、/healthz/ready、/metrics | 运维面；不得返回未脱敏商家业务内容 |

---

## 共享鉴权规则（所有业务入口套用）

1. **会话**：从服务端 session 取 `user_id`；请求中的 `merchant_id`（header/query/body）仅作「当前工作商家」声明，须存在有效 Membership，否则 403。
2. **根加载**：按 id 取根对象后比较 `root.merchant_id == auth.merchant_id`；找不到与跨商家统一对外表现为 404（防枚举）或契约固定的 403——实施批次内二选一并写测试，全站一致。
3. **子引用**：绑定/移动/attach/from-product/from-session/recipe apply/workflow media sync 时，所有被引用 id 解析后商家必须相同；跨商家绑定 400/403，且不产生半写入。
4. **角色**：写/消费类拒绝 Vw；成员与商家设置拒绝 Ed/Vw；settings/provider 拒绝全部商家角色。
5. **队列**：受理时写入 `merchant_id` + `actor_user_id` + 权限快照到业务行或 dispatch 旁路元数据；worker 只信任持久快照，不信任 asynq 信封外带商家；重试/Restage 保持原商家。
6. **SSE**：订阅建立时鉴权；游标只在已授权聚合内回放；成员撤销后拒绝续订与 after 重放。
7. **媒体**：`download`/`content`/`ServeVariant` 在解析资产身份后鉴权；路径不可猜不能替代授权。
8. **内部工具**：`requireInternal` 只证明 Pi；scope 以 conversation contract 的商家为准；工具参数中的 product/asset/run id 必须落入该商家。
9. **前端**：queryKey 与 EventSource 必须含 `merchantId`（或切换时 `removeQueries`+关闭旧源）；迟到响应丢弃。

---

## 覆盖矩阵

矩阵列：入口 | 角色 | 根所有权 | 子引用验证 | 队列/effect | 读写 owner（代码锚点） | 前端 query/订阅 | 测试计划。共享规则见上；同规则入口可归并，但清单必须完整。

**条目统计：归并矩阵 69 条**（A8+B9+C6+D6+E6+F6+G6+H7+I10+J5）。每条绑定源码锚点与测试计划；**展开覆盖**枚举面 222 业务路由 + 25 Pi 工具 + 5 Actor + 6 SSE/事件族 + 3 实例探活/指标入口 + 71 表的所有权分类。禁止只测商品列表过滤。

### A. 身份与实例面（8）

| ID | 入口 | 角色 | 根所有权 | 子引用 | 队列/effect | 读写 owner | 前端 | 测试计划 |
|---|---|---|---|---|---|---|---|---|
| A1 | `POST/GET/DELETE /api/auth/session` | 过渡：现 admin；目标 User 登录/退出 | User 会话 | — | — | `go/internal/auth/http.go` | `["session"]` | B0：密码会话；撤销后 401；禁止残留 admin 布尔回退 |
| A2 | User/Merchant/Membership CRUD（部分待建） | bootstrap/公开注册建商；Owner 管理既有成员；新增成员关联另行设计 | merchants、memberships | 最后 Owner 保护 | — | 新 `auth`/`membership` 包 | 商家切换器 query | 注册事务、并发撤 Owner、成员变更 |
| A3 | `GET/PATCH /api/settings*`、export/import、provider-* | 仅已登录 Operator | 实例 | — | — | `settings/http.go` | `["config"]` `["provider-config"]` `["runtime-config"]` | **B9**：商家角色、普通 User 与匿名请求拒绝；导出无商家密钥明文 |
| A4 | `GET /api/generation-queue` | 仅 Op；合同固定只读聚合（计数，无商家/任务明细） | 实例队列视图 | — | 读各 Actor 状态 | `settings/http.go` | api.getGenerationQueue | **B9**：商家角色 403；无他商任务细节 |
| A5 | `GET /api/v3/node-catalog`、`image-generation-options`、`delivery-presets` | 已认证成员只读 | 实例目录 | — | — | graph/delivery | `["graph-node-catalog"]` `["image-generation-options"]` `["delivery-presets"]` | 无商家数据；未登录 401 |
| A6 | `GET /healthz`、`/healthz/ready` | Sys | 实例 | — | — | `platform/httpx/health.go` | — | 无业务体 |
| A7 | `GET /metrics`（token） | Sys/Op | 实例 | — | — | `platform/metrics/http.go` | — | 无未脱敏商家内容 |
| A8 | 支持会话/审计（合同草案；完整 MP-D 另发） | Op 显式支持会话 | 目标商家 | 审计记录 | — | `auth/support.go`；`GET /api/ops/support-contract`；会话入口 501 占位 | — | **B9**：合同可测；无隐式全局商家 UI；≠完整 MP-D |

### B. 商品与事实 / 图库（9）

| ID | 入口 | 角色 | 根所有权 | 子引用 | 队列/effect | 读写 owner | 前端 | 测试计划 |
|---|---|---|---|---|---|---|---|---|
| B1 | `GET/POST /api/v2\|v3/products`、`GET/DELETE /api/v2/products/:id` | 读 Vw+；写 Ed+；删按策略 | products.merchant_id | — | — | `product/http.go`→`store.go` | `["products", …]` `["product", id]` | **因果样例**：list 仅本商；交叉 id→404/403 |
| B2 | `GET/PUT .../facts` | 读 Vw+；写 Ed+ | product | fact version 属 product | — | `product/facts.go` | `["product-facts", id]` | 跨商 product facts |
| B3 | 封面 PUT/DELETE、image-folders CRUD、move、list/get/patch/add assets | Ed+ / Vw 读 | product | folder/asset∈product | — | `product/gallery_*.go` | `["product-image-library*"]` galleryAssetsQueryKey | 跨商 folder/asset 绑定 |
| B4 | `GET .../product-image-assets/:id/download` + variant | Vw+ | asset→product→merchant | — | — | `product/http.go`+`media.ServeVariant` | download URL | 跨商 UUID 下载拒绝；thumbnail 同权 |
| B5 | `POST .../download-archive` | Vw+ 或 Ed+（合同固定） | product | 仅本商 assets | 同步 ZIP | `product/gallery_archive.go` | api download-archive | 打包不混入他商 |
| B6 | `DELETE .../product-image-assets/:id` | Ed+ 且 deletion 开关 | asset→product | — | — | product | — | 跨商删拒绝 |
| B7 | `POST /api/v3/products/from-recipe`、source-notes/generate | Ed+ | 新 product 本商；recipe 本商 | recipe∈merchant | source-notes：**已接** `quota.Reserve`（`product/quota_wire.go`；成功 Settle；未发出 Release；不明 MarkUnknown；≠R5 全过） | `product`+`recipe` | recipe create form | 跨商 recipe id；额度不足 409 |
| B8 | agent-product-workspaces options/drafts/create/get/intake | Ed+ | conversation→merchant；产出 product 本商 | 素材引用本商 | 可间接触发 Agent | `product/workspace.go` | `["agent-product-workspace*"]` | 跨商 conversation |
| B9 | **因果路径 B1**：`GET /api/v2/products` → `HTTP.list` → `store` 查询 products → JSON 列表 | 见上 | 查询必须 `WHERE merchant_id=?` | — | — | product | ProductListPage | 正：本商；反：种子他商不可见 |

### C. Graph / Run / SSE（6）

| ID | 入口 | 角色 | 根所有权 | 子引用 | 队列/effect | 读写 owner | 前端 | 测试计划 |
|---|---|---|---|---|---|---|---|---|
| C1 | workflows CRUD/current/get、changesets、undo/redo | Ed+ / Vw 读 | product→graph | changeset 节点属本 graph | — | `graph/http.go` | `["workflow-graph", productId]` | 跨商 workflow_id |
| C2 | proposals confirm/discard、document candidate * | Ed+ | graph | proposal∈graph | — | graph | workbench | 跨商 proposal |
| C3 | runs submit/preview/list/get/cancel/retry | Ed+ 消费；Vw 可读 list/get | graph | — | **ActorGraphRun** + notify run | `graph/service.go`+recovery | `["graph-runs", …]` | **因果样例**见 C9 |
| C4 | `GET .../runs/:run_id/events` SSE | Vw+ | run→graph→product→merchant | 游标∈run | notify ChannelRun | `graph/run_sse.go` | EventSource graph | 跨商 run SSE；撤销后断开 |
| C5 | Graph provider effects / artifacts / node_runs | 经 run | 子表随 graph | — | 执行中写 | graph/execute | — | 归属随 run 商家快照 |
| C6 | **因果路径**：`POST .../runs` → 建 WorkflowGraphRun + `async_dispatches`(ActorGraphRun) → dispatcher SENT → worker → events → SSE/GET run | Ed+ | 受理快照 merchant_id | — | outbox 不改商家 | graph+queue | workbench run UI | 正跑通；他商 cancel/retry/SSE 拒绝；Restage 保持商家。graph 包 20 路由均归 C1–C6 |

### D. 配方（6）

| ID | 入口 | 角色 | 根所有权 | 子引用 | 队列/effect | 读写 owner | 前端 | 测试计划 |
|---|---|---|---|---|---|---|---|---|
| D1 | `GET /workflow-recipes`、get、archive | 读 Vw+；archive Own/Ed | recipe.merchant_id | — | — | `recipe/http.go` | `["workflow-recipes"]` | list 仅本商 |
| D2 | create/append version on product workflow | Ed+ | recipe 本商；product 本商 | graph∈product | — | recipe | workbench | 跨商提取 |
| D3 | preview/apply on product | Ed+ | 双方同商 | 应用 Graph Command | 同步命令；非独立 Actor | recipe→graph | apply API | 跨商 recipe→product |
| D4 | creation-preview | Ed+ | recipe 本商 | — | — | recipe | `["recipe-creation-preview"]` | 跨商 |
| D5 | recipe applications 表 | — | 记录双方商家一致 | — | — | schema | — | 约束/测试 |
| D6 | **因果路径**：preview → apply → Graph Command → live graph | Ed+ | | | | `recipe/service.go` | | 跨商 apply 零写入 |

### E. 商家图库 Library（6）

| ID | 入口 | 角色 | 根所有权 | 子引用 | 队列/effect | 读写 owner | 前端 | 测试计划 |
|---|---|---|---|---|---|---|---|---|
| E1 | list/bootstrap、folders/tags CRUD、organize | 读 Vw+；写 Ed+ | library 根商家 | — | — | `library/http.go` | `["media-library-*"]` | list 隔离 |
| E2 | upload/collect | Ed+ | 本商 | — | — | library/save | MediaLibraryPage | 幂等键含商家 |
| E3 | from-session / from-product | Ed+ | 目标 library 本商 | session/product/asset 同商 | — | library/save | api | **跨商家绑定**必测 |
| E4 | get/download/archive/restore | Vw+ / Ed archive | asset→merchant | — | ServeVariant | library+media | download URL | 跨商下载 |
| E5 | workflow media-library list/sync/remove | Ed+ | workflow 与 library asset 同商 | product_id 查询参数须一致 | — | library | `["workflow-media-library"]` | 跨商 sync |
| E6 | **因果路径**：from-product → 校验 product 商家 → 写 media_library_assets → list 可见 | Ed+ | | | | | | 他商 product id 拒绝 |

### F. Image session（6）

| ID | 入口 | 角色 | 根所有权 | 子引用 | 队列/effect | 读写 owner | 前端 | 测试计划 |
|---|---|---|---|---|---|---|---|---|
| F1 | list/create/get/patch/delete、refs、history/status | 读 Vw+；写 Ed+ | image_sessions.merchant_id | — | — | `imagesession/http.go` | `["image-sessions"]` 等 | list 隔离 |
| F2 | generate/retry/cancel/reconcile | Ed+ | session | — | **ActorImageSession** | imagesession/execute+recovery | mutations | 重试归属 |
| F3 | `GET .../events` SSE | Vw+ | session | 游标 | ChannelImageSession | `imagesession/sse.go` | EventSource | 旧订阅/撤销 |
| F4 | `GET /image-session-assets/:id/download` | Vw+ | asset→session | — | variant | imagesession | | 跨商 |
| F5 | `attach-to-product` | Ed+ | session 与 product 同商 | asset∈session | 复用 MediaObject | imagesession.Attach | api | **跨商 attach** |
| F6 | **因果路径**：generate → dispatch → worker → rounds/assets → status/SSE | Ed+ | 快照商家 | | | | | 他商 retry/SSE 拒绝 |

### G. Delivery / Local edit（6）

| ID | 入口 | 角色 | 根所有权 | 子引用 | 队列/effect | 读写 owner | 前端 | 测试计划 |
|---|---|---|---|---|---|---|---|---|
| G1 | renditions create/list、job get/retry | Ed+；Vw 可读 | asset→product | — | **ActorDelivery** | `delivery/*` | workflowDraftApi | 跨商 job |
| G2 | `POST .../delivery-exports` ZIP | Vw+/Ed+ | product | 仅本商 assets | 同步 ZIP | delivery/export | deliveryExportApi | 跨商导出 |
| G3 | local-image-edits capability | 已认证 | 实例能力 | — | — | localedit | `["local-image-edit-capability"]` | |
| G4 | image-edits CRUD/submit/cancel/retry/adopt/revert | Ed+；Vw 读 | product | source/refs 同商 | **ActorLocalEdit** | `localedit/*` | `["local-image-edit-task", productId, …]` | 跨商 task；adopt 引用 |
| G5 | **因果路径 delivery**：create rendition → job+dispatch → worker → get | | 快照 | | | | | Restage 商家不变 |
| G6 | **因果路径 localedit**：submit → dispatch → attempt → adopt | | | assets 同商 | | | | 跨商 source asset |

### H. Agent 浏览器 API（7）

| ID | 入口 | 角色 | 根所有权 | 子引用 | 队列/effect | 读写 owner | 前端 | 测试计划 |
|---|---|---|---|---|---|---|---|---|
| H1 | agent-sessions / tasks CRUD 与 pause/cancel/complete/resume | Ed+ 写；Vw 受限读 | session/task.merchant_id | product scope 时 product 同商 | 可 Stage **ActorAgentTurnSync** | `agent/http.go` | `["agent-sessions"]` `["agent-tasks"]` | list 按商；跨商 task |
| H2 | product workbench ensure/get | Ed+/Vw | product+conversation 同商 | — | — | agent | agentWorkbenchQueryKey | |
| H3 | product/global conversation turns、questions、confirm/cancel run request、effect-reconciliation | Ed+ 确认；Vw 读 | conversation→merchant | request∈conversation | turn sync Actor | agent | useGlobalAgentConversation / product hooks | 确认前重鉴权；撤销后拒答 |
| H4 | turn SSE + events/page | Vw+ | turn→conversation→merchant | Last-Event-ID 游标 | ChannelTurn | agent/sse | conversation/runtime.ts EventSource | **旧订阅**；跨商 projection |
| H5 | `GET /agent-control/events` | 本商成员 | 控制面按商过滤 | — | ChannelControl | agent/control | GlobalAgentDock | 他商 session 事件不可见 |
| H6 | library-organization-draft get/confirm | Ed+ | conversation 商 | draft 只改本商 library | | agent | globalLibraryOrganizationDraftQueryKey | 跨商 draft |
| H7 | **因果路径**：submit turn → projection+dispatch → Pi claim → journal → SSE | | 合同含商家 | | | agent+agent-service | | 见自审「内部工具」 |

### I. Agent 内部 API + Pi tools（10）

| ID | 入口 | 角色 | 根所有权 | 子引用 | 队列/effect | 读写 owner | 前端 | 测试计划 |
|---|---|---|---|---|---|---|---|---|
| I1 | internal `requireInternal` 全部 `/api/internal/v1/...`（46） | Int | conversation contract.merchant | 工具参数 id∈商 | lease/fencing | `agent/http_internal.go` | Pi only | 伪造 token 401；**伪造 scope/商家**拒绝 |
| I2 | contract / runtime-context / product-context / global-workflow-context | Int | 返回必含商家 | — | — | agent | runtime-scope.ts | scope 无商家字段则本批次补齐 |
| I3 | graph apply/propose/discard/focus + reconcile | Int | 本商 graph | | tool_effect checkpoint | agent/tools_graph | | 跨商 node/run id |
| I4 | workflow-runs list/detail/inspect/cancel | Int | 本商 | | | | | |
| I5 | turn-executions claim/heartbeat/checkpoints/effects/events/release | Int | execution∈conversation 商 | | | agent/execution | | fencing 不逃逸商家 |
| I6 | workflow-run-requests prepare/create/reconcile（product+global） | Int | 本商 | | | | | |
| I7 | assets/media-library/products list/inspect/**content** | Int | 只读本商 | content 鉴权同下载 | | | | **content 跨商 UUID** |
| I8 | product-workspaces / product-intake + reconcile | Int | 创建归属合同商家 | | | | | |
| I9 | Pi tools（25）：`tool-manifest.ts` 全表 | Int→Go | `runtime-scope`+contract | 工具 args | tool-effect | `agent-service/src/tools.ts` | — | 每工具至少参数越权测或共享 harness |
| I10 | **因果路径**：tool `list_products_v1` → internal products → store 商家过滤 → tool result | | | | | | | 合同商家 A 不可见 B |

工具名清单（I9 展开）：`load_productflow_skill`、`ask_user`、`productflow_context_injection`、`get_product_workflow_context_v1`、`inspect_workflow_runs_v1`、`list_product_image_assets_v2`、`inspect_product_image_assets_v1`、`request_workflow_run_v1`、`request_global_workflow_run_v1`、`finalize_product_intake_v1`、`list_products_v1`、`inspect_products_v1`、`inspect_global_workflow_context_v1`、`inspect_global_workflow_runs_v1`、`list_global_media_library_assets_v1`、`inspect_global_media_library_assets_v1`、`create_product_workspace_v1`、`propose_global_draft`、`get_node_detail_v1`、`get_workflow_run_detail_v1`、`apply_graph_change_set_v1`、`propose_graph_change_set_v1`、`discard_workflow_proposal_v1`、`cancel_workflow_run_v1`、`focus_canvas_items_v1`。

### J. 队列 / 恢复 / 前端边界（5）

| ID | 入口 | 角色 | 根所有权 | 子引用 | 队列/effect | 读写 owner | 前端 | 测试计划 |
|---|---|---|---|---|---|---|---|---|
| J1 | async_dispatches + 五 Actor | Sys worker | aggregate 商家快照 | — | Stage/Restage/Sent 对账 | `platform/queue`+各 recovery.go | — | **重试归属**；禁止信封改商 |
| J2 | dispatcher / worker 进程 | Sys | — | — | ChannelDispatch | cmd/productflow-* | — | 多商混合队列不串权 |
| J3 | notify 五通道 | Sys→SSE | payload=聚合 id，接收前再鉴权 | — | | `platform/notify` | | 唤醒不授予数据 |
| J4 | 前端商家切换 | 用户 | — | — | 取消 EventSource；queryClient 按 merchant 隔离 | web App / docks | 所有 queryKey | 迟到响应不进新商 UI |
| J5 | 缓存与 React Query | — | — | — | — | 各 Page | 见枚举 queryKey 清单 | 无跨商复用 `["products"]` 等；B8 统一门禁 |

---

## 实施批次（User/Membership → 完整隔离）

**总约束（每批）：** 中间版本不得开放第二个互不信任商家的公开注册试用；不得保留「共享 admin cookie 可访问全部业务」作为正式回退；每批合并后仍可单商家开发使用。公开注册仍须经过 SMTP、账号事务和外部发布授权门。

| 批次 | 名称 | 精确范围 | 正测 | 反测 | 暴露限制 |
|---|---|---|---|---|---|
| **B0** | 身份骨架 | User、Merchant、Membership、邀请/密码会话；替换 `auth` 布尔登录为 User 会话；Operator 引导建**唯一**开发商家；schema 新表。**已交付**（2026-09-07，见任务证据；邀请分支已退役，仅作历史证据） | 登录、邀请、角色读 Membership | 无效会话；非成员；最后 Owner 移除 | 仅 1 个商家；业务表尚未强制 merchant 过滤时可暂限 Op 工具创建商 |
| **B1** | 根归属迁移 | products、image_sessions、media_library_*、workflow_recipes、visual_systems、agent_sessions/tasks/conversations 写 `merchant_id`；回填到 B0 商家；查询/唯一键含商家；**不**改 admin 回退。**已交付**（2026-09-07，跨商统一 404） | 本商 CRUD | 裸 UUID 他商（夹具） | 仍单商；迁移夹具可有第二商数据但 UI/API 不开放注册第二商 |
| **B2** | 商品链 | 矩阵 B\*：事实、图库、封面、直链下载/ZIP、workspace intake、from-recipe。**已交付**（2026-09-07，见 [merchant-product-chain](tasks/archive/merchant-product-chain.md)） | 本商读写下载 | 交叉 id、ZIP 混装 → 404 | 单商 |
| **B3** | Graph+配方 | 矩阵 C\*、D\*：changeset、run、SSE、recipe apply。**已交付**（2026-09-07，见 [merchant-graph-recipe](tasks/archive/merchant-graph-recipe.md)） | 本商 run/SSE/apply | 交叉 workflow/recipe/run；SSE 游标 → 404 | 单商 |
| **B4** | 图库绑定 | 矩阵 E\*：from-product/session、workflow sync。**已交付**（2026-09-07，见 [merchant-library-binding](tasks/archive/merchant-library-binding.md)；跨商统一 404） | 同商绑定 | **跨商绑定** | 单商 |
| **B5** | 会话生图 | 矩阵 F\*：generate、SSE、attach。**已交付**（2026-09-07，见 [merchant-image-session](tasks/archive/merchant-image-session.md)；跨商统一 404） | 本商 | 跨商 attach/download/retry | 单商 |
| **B6** | 交付与局部编辑 | 矩阵 G\*。**已交付**（2026-09-07，见 [merchant-delivery-localedit](tasks/archive/merchant-delivery-localedit.md)；跨商统一 404） | 本商 job/export/adopt | 跨商 job/task/source | 单商 |
| **B7** | Agent 全链 | 矩阵 H\*、I\*：浏览器+internal+Pi scope 商家字段、25 工具越权 harness。**已交付**（2026-09-07，见 [merchant-agent-tools](tasks/archive/merchant-agent-tools.md)；跨商统一 404） | 本商 turn/tool | 伪造 internal scope、跨商 content、确认在撤销后 | 单商；Agent 评测题不改 grader 边界 |
| **B8** | 队列与前端 | 矩阵 J\*：五 Actor 快照、Restage、recovery；前端 merchant 前缀与 SSE 取消。**已交付**（2026-09-07，见 [merchant-queue-frontend](tasks/archive/merchant-queue-frontend.md)） | 混合队列下本商 UI 正确 | 改信封商家无效；旧订阅；切换迟到 | 单商 |
| **B9** | 运营面最小集 | A3–A4、A8 支持会话合同草案；商家启停字段。**已交付**（2026-09-07，见 [merchant-ops-surface](tasks/archive/merchant-ops-surface.md)） | Op 停用后商家不可写 | 商家角色碰 settings | 仍不开放第二外部商 |
| **B10** | 双商隔离门 MP-B | 种子商家 A/B；矩阵每分区至少 1 正 1 反自动化；自审七场景全过 | A 合法全绿 | B 交叉全拒 | **自动化证据通过**（[merchant-isolation-gate](tasks/archive/merchant-isolation-gate.md)）；总纲 R1 另见 [merchant-r1-close-ruling](tasks/archive/merchant-r1-close-ruling.md)；**仍不**自动开放第二外部商产品注册 |

依赖：B0→B1→(B2∥B3∥B5 在 B1 后可并行，但写集不重叠时由协调者拆 issue)→B4 依赖 B1+B2+B5→B6 依赖 B2→B7 依赖 B1–B6 合同字段→B8 贯穿可与 B7 末并行→B9 可早于 B10→B10 最后。

额度（MP-C）与完整运营（MP-D）在隔离门之后另发，不阻塞本矩阵；但消费入口的商家快照字段在 B7/B8 预留，避免二次拆合同。

---

## 自审模拟 → 批次映射

| 场景 | 期望 | 落点 |
|---|---|---|
| 合法商家读写 | 本商 HTTP/工具/下载/SSE 成功 | B2–B7 正测；B10 汇总 |
| 跨商家替换 ID | 详情/下载/SSE/内部 content 拒绝 | 各域反测；统一 404/403 策略在 B1 钉死 |
| 跨商家绑定 | from-product、attach、recipe apply、workflow sync、edit refs 拒绝且无半写入 | B4/B5/B3/B6 |
| 旧订阅 | 切换或撤权后 EventSource 停止；游标重放拒 | B3/B5/B7/B8 |
| 成员撤销后确认 | 新读/写/答问/confirm 拒；已受理任务不改归属；取消需有权成员或 Op | B0+B7+B8；MP-04 |
| 内部工具伪造范围 | 坏 token、改 conversation、工具参数他商 id | B7 I1/I9 |
| 任务重试归属 | retry/Restage/recovery 仍原 merchant；消费不落到切换后商家 | B8 J1；MP-04 |

---

## 未裁定边界

**无。** 下列曾可能歧义项已按总纲 7.x + MP-01..07 在本文件裁定，实施不得再打开口子：

| 议题 | 裁定 |
|---|---|
| 全局图库 | 改为**商家共享图库**（每商隔离），不是实例共享 |
| Agent `scope_type=global` | 表示商家内非 product 会话，不是跨商 |
| 配方库 | 商家根；首版不共享 |
| media_objects | 实例存储；授权在引用身份；首版无跨商去重 |
| provider/settings | 实例 Op；商家不可写密钥 |
| 跨商 UUID 对外码 | **B1 钉死 404**（`auth.CrossMerchantDetail`），全站根加载一致，不按入口混用 403 |
| 第二商家开放 | 仅 B10/MP-B 通过之后 |

若实施中发现枚举未覆盖的新入口，先补矩阵再写代码，不在本任务外静默扩大范围。

---

## 验收缺口（R1 通过后的残余，不阻塞 R1）

- MP-C **B0–B4 + 价格目录骨架 + 图会话入口单价展示 + 工作台 Graph 确认单价 + unknown 到期策略**已交付；**总纲 R5 裁定未通过**（2026-09-07，[merchant-r5-close-ruling](tasks/archive/merchant-r5-close-ruling.md)）。已接：图会话 `Generate`、Graph `callImageProvider`、Agent `before_model_request`、商家/Op 余额 HTTP、**localedit `Executor.Execute`（Edit 前 Reserve）**、**source-note `POST /api/v2/product-source-notes/generate`（`product/quota_wire.go`）**、**价格版本表 + 种子默认 `pv-placeholder-v0` + Reserve 版本校验 + Op/商家只读价格 HTTP**、**新商家试用额度种子（`QUOTA_TRIAL_UNITS`，默认 100；bootstrap/create/activate → `EnsureAccount`；lazy `ensureAndLockAccount`；≠支付）**（[bootstrap-quota](tasks/archive/merchant-mp-c-bootstrap-quota.md)）、**文/图生图 Generate 前读 `GET /api/merchants/:id/quota/price` 展示 `image_session.generate` 预估扣减；缺失/无效禁用**（[price-display-b0](tasks/archive/merchant-mp-c-price-display-b0.md)）、**工作台 Agent 工作流执行确认读同 API 展示 `graph.image_generation`；缺失/无效禁用确认**（[price-display-workbench](tasks/archive/merchant-mp-c-price-display-workbench.md)）、**unknown 到期/Op 裁定（`QUOTA_UNKNOWN_HOLD_TTL` 默认 72h 全额 Settle；`POST /api/ops/merchants/:id/quota/holds/resolve`；dispatcher `quota_unknown`；禁止超时 Release；[unknown-expiry-b0](tasks/archive/merchant-mp-c-unknown-expiry-b0.md)）**。**阻塞缺口**：其它未枚举收费入口的入口展示（画布直接跑图、localedit、source-note、Agent model 等）。MP-D 仅 A8 合同草案（≠完整运营产品化）。**≠宣称 R5 通过**；**≠真实支付/法币**。
- **冻结基线尚未邀请第二互不信任商家**；产品 `CreateMerchant` 仍 409；该邀请口径仅作历史记录。当前公开注册由 SMTP 和注册事务保障，外部开放仍需单独授权。
- 额度公平调度、混合负载经营验收、真实支付仍属后续（≠本 R1/R5 条文通过条件中的已交付子集）。
- **Brand B1（商品选定 + 继承合并）**：`products.brand_id` 与 `ResolveInheritance` 品牌层 style/colors 合并见 [merchant-brand-product-select-b1](tasks/archive/merchant-brand-product-select-b1.md)；**≠宣称 R2/R3**；≠完整多品牌 UI。

本文件矩阵与批次可执行；**R1 / MP-A / MP-B 已过；MP-C B0–B4 + 价格目录骨架 + 图会话入口单价展示 + 工作台 Graph 确认单价 + unknown 到期策略已交付；总纲 R5 未通过；≠ 第二外部商产品上线；≠ 真实支付；≠ MP-D。**
