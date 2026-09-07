# 任务：运营人员可查询商家并核对、裁定和调整内部额度

状态：阻塞
类型：实现
认领者：—
认领于：—
业务组：商家平台
父账本：merchant-platform.md
完成后可拆：受审计支持访问；邀请开通新商家；运营概览

任务状态、认领及提交遵循 [Issue 协议](README.md)。

## 问题来源

当前运营 UI 仅当前成员商家的启停面板，额度与 unknown 裁定已有 HTTP，支持会话仍 501。需要独立 Operator 入口、商家列表与消费明细，把已有服务组成可解释的操作流程；不能把额度页面做成第二本账或借运营列表直接读取全商商品。

## 做成什么样

- `/ops/merchants` 可按名称/ID/status 找到商家；`/ops/merchants/:merchantId` 有资料、状态、额度及消费明细，没有未经支持授权的商品图或对话内容。
- `/merchant/usage` 给 Owner 看本商余额/明细；Editor/Viewer 继续可看生成前单价和余额，不获得运营调账或完整账单入口。
- Operator 可带原因调账或裁定待核对预留；超时/重试保留同一操作键并可回读结果，不能重复扣增。
- 复用既有站点 `/settings`；明确支持会话尚未可用，不能挂可点击但返回 501 的业务操作。

## 前置与并行

- 前置：[saas-account-team](saas-account-team.md) 已交付身份审计、最近再认证和独立 Operator 范围；既有 MP-C 账本/price/unknown 任务交付记录有效。
- 冻结输入：quota 状态机、72h unknown 策略和现行单价目录；本任务不调整收费政策。
- 运行资源：隔离 PG、Redis、两个 mock 商家，固定时钟测试到期；不得使用共享 dispatcher 裁定真实账本。
- auth/quota、schema 与共享 Web 文件排他；可在设置任务之后串行执行，未固定 schema 时不得按无前置并行。

## 只改这些文件

- `go/internal/quota/` 的读取/投影和必要命令回读，`go/internal/auth/` 商家分页/审计接线；schema quota/identity 和 migrate 的必要索引及回归。
- `web/src/pages/` 新 OpsMerchants/OpsMerchant/MerchantUsage 页面及局部组件；`App.tsx`、TopNav、现有 MerchantOpsPanel、`api.ts`、`types.ts`、`i18n.ts` 和测试。
- API 路由合同、本任务 e2e；本文件及协调者整合的父章程和活文档。

## 不要碰

- CreateMerchant 单商限制、支持会话 501 的真实读权限、真实支付/订单/发票、价格版本政策、Graph/Pi 执行器、真实商家邀请。
- 不用解析任意 idempotency_key 字符串猜商品名称，不向 Operator 列表拼接受保护媒体。

## 现在代码在哪

`go/internal/quota/{http,service,catalog,unknown}.go`、`schema/quota.go`；`auth/{service,support,http}.go`；`web/src/pages/settings/MerchantOpsPanel.tsx`。读取当前 Reserve 调用方确认可用来源字段；现有账本只保证 operation key，缺友好业务关联时显示脱敏操作号，不能伪造来源。

## 合同

1. 目标读取：`GET /api/ops/merchants`、`GET /api/ops/merchants/:merchant_id`、商家/Op quota 下 `events` 与 `holds` 列表、按 hold id 详情。保留已有 quota/account/price/adjust/holds/resolve 入口。JSON page 统一 items、next_cursor；默认 20 最大 50，created_at/id 稳定倒序；status、UTC 时间范围白名单，禁止无界 all。
2. 商家详情返回名称/status、Owner 数量、quota 与版本摘要，不返回邮箱全集/原图/对话。支持授权未交付之前只能运营元数据读取。Owner 仅本商 ledger；普通成员 balance/price 权利保留；跨商资源继续 404。
3. 余额与流水分别读时返回 observed_at，不假称跨请求同一快照；某次操作后的余额以其 immutable event available_after/reserved_after 为准。amount_units、settled_units 和 currency 原义保留，内部单位不显示为法币；reserved 不计成 settled，unknown 单列。
4. 调账/裁定入口填写 reason，确认面显示目标商家、当前可见余额、变化值/实际裁定值、单位和后果。Operator 最近 10 分钟需密码再认证；成功/冲突后重新读取权威余额。客户端本次 action 只生成一个 key，丢响应重试沿用；payload 改变必须创建新 action。读回操作结果仅允许原权限范围。
5. 商家停用的已实现语义由执行者枚举后与父合同对照：禁止新消费，未开始的 provider 尝试不得开始；已经发出的调用完成或 unknown 按原事实结算，禁止假退款。若现有执行链缺该保证，交回另发完整因果切片，不在控制台任务顺手改所有 executor；该缺口阻止邀请试用。
6. 运营审计复用前置表；额度事件已经含 actor/reason 的事实保持唯一，身份审计可引用 event id，不能维护另一份金额余额。列表排序/filter 请求只读，不触发 EnsureAccount 或试用种子建账。
7. 页面用顶层列表、筛选条、行操作与详情区域；金额右对齐，时间有时区，窄屏行展开详情；不可用来源显示“来源未记录”，不能补编商品名。没有数据时为空，不默认填 mock 曲线。

## 怎么验收

- 无 Membership 的 Operator 可查运营元数据、商家 Owner 只能本商 ledger、Editor/Viewer 拒绝 ledger、普通用户直达 ops/API 拒绝；跨商 cookie/context 不改变目标授权。
- 分页同时间戳不重复/漏静态行；十万额度事件下页长有界、索引支持所选过滤；GET 不写账户，unknown 与 settled 统计口径正确。
- 调账重放、并发调账/预留、人工裁定与 TTL 竞争只有一个正确结果；丢响应重试同 key，不重复事件；拒绝超范围 actual_units、空 reason、过期再认证。
- 390×844、1440×960 浏览器完成查询→详情→调账/裁定→回读；四语明暗、长商家名、空/错/忙/409/503；核 DB 与 event，不能只核 toast。
- 受影响 Go/PG/migrate、前端 focused tests、lint/build、`just docs-check`。本任务不宣称 MP-D 全部完成、R5 通过或真实支付可用。

## 阻塞与交接

- 原因：账户审计与再认证前置未交付。
- 解除条件：协调者固定前置、quota 既有交付和共享文件窗口后开放。
- 跟进者：开发协调者。
- 交接：未开工，无资源占用。

## 证据

- 命令 / 日期 / 结果：待执行。
- 基线 commit / artifact：执行时固定。
- 交付定位：随本任务提交。
- 审核者 / 结论：待审核。
- Issue 结果 / 业务门槛结果 / 剩余缺口：未交付。
