# 任务：同一账号在不同标签页明确选择商家并安全切换工作区

状态：取消
类型：实现
认领者：—
认领于：—
业务组：商家平台
父账本：merchant-platform.md
完成后可拆：saas-account-team；saas-preferences-settings

任务状态、认领及提交遵循 [Issue 协议](../README.md)。

## 取消裁定

2026-09-08 用户明确一个普通账号自有一个商家，取消多商家切换产品路线。本轮未提交 Web 增量已经清除，执行代理停止，无该任务新增运行资源。以下原合同仅作历史记录，不再指导开发；账号隔离和管理员权限由 [设计校准](saas-design-alignment.md) 整合。取消不等于原验收通过。

## 问题来源

`web/src/lib/merchantBoundary.ts` 当前取第一个 Membership；Go 接受 `X-ProductFlow-Merchant-Id`，原生 EventSource、img 和下载不能附此 header。只有清缓存并不能保证非首商家的图片、SSE 与下载请求进入同一商家上下文。商家选择是应用壳层与整条 HTTP 链合同。

## 做成什么样

- 登录后可在已有有效成员关系中选商家；选择在当前标签页刷新后保留，两标签页可各处于不同商家。
- 顶栏持续显示当前商家，个人菜单独立；Operator 可进入运营范围且无需拥有商家 Membership。
- fetch、SSE、图片、缩略图、原图、下载及深链接保持同一明确商家；A 页迟到结果不能进入 B 页。
- 无商家、商家停用、权限撤销均有可退出和选择其它商家的页面，不无限跳转或默默切到首商家。

## 前置与并行

- 前置：[saas-auth-entry-security](saas-auth-entry-security.md) 已交付并固定 cookie/来源合同。
- 冻结输入：MP-01 至 MP-04、已有 worker merchant 快照；两商家由测试夹具创建。产品 CreateMerchant 的历史开发限制由公开注册任务单独替换，本任务不宣称注册入口开放。
- 运行资源：隔离 PG、Redis、API/Web/worker、mock provider；不用共享开发栈做退出/停用实验。
- 全链客户端和商家 middleware 排他；涉及各 SSE reader 时协调者核对实际文件后锁定，不能与其它工作台任务并行写共享文件。

## 只改这些文件

- `go/internal/auth/{merchant,principal,http,service}.go` 及相关测试；`go/internal/platform/httpx/` 与 API 注册的最小必要调整。
- `web/src/App.tsx`、`web/src/components/TopNav.tsx`、`web/src/lib/{api,types,merchantBoundary,i18n}.ts` 与新增工作区选择组件及测试。
- 读者清单确认后，协调者将确需修改的图片 URL、SSE、download helper、各页 query/订阅生命周期文件补入排他范围；此阶段允许有界枚举，未登记前不跨文件修补。
- `web/e2e/` 本任务用例、`contracts/http-routes.json`；本文件与协调者整合的活文档。

## 不要碰

- 新建商家、支付、Graph 执行器、Pi 调度、provider 配置；不把 merchant 选择存为跨标签页共享的 cookie 当前值。
- 不用裸 UUID 或前端过滤替代服务端所有权，不恢复旧共享 admin 会话。

## 现在代码在哪

`auth.AttachWorkingMerchant`、`auth.RequireWorkingMerchant`、`web/src/lib/merchantBoundary.ts`、`App.tsx`、`api.ts`；`web/src/pages/workbench/agent/conversation/runtime.ts` 已有关闭共享订阅入口。媒体和事件注册枚举见父章程矩阵 B/E/F/H/J；不得只覆盖 Agent SSE。

## 合同

1. 选择存于 sessionStorage，key 含当前 user ID；仅作为用户选择值，每次 server 请求仍验证 Membership。读入时严格解析并与当前 session memberships 比较。单有效商家可自动选，多有效商家且无有效选择必须显示选择页；显式失效选择显示失效状态，不回落到另一个商家。
2. JSON/multipart fetch 使用既有 header；原生 SSE、媒体与下载使用 URL query `merchant_id`。服务端只允许单一值；header 与 query 同时存在且不一致为 400。query 不是凭据。认证/个人/运营 API 不依赖工作商家，运营目标用路径表达。
3. 多商用户访问商家根业务缺选择时拒绝并要求选择，不能用首 Membership 猜测；单商开发回落只限原明确开发合同。携带已失效商家选择为 403，跨商对象继续沿已有 404。错误体沿现有 detail 格式。
4. 深链接带 merchant_id 时先校验并建立该标签页上下文；无选择的深链接保留目标等待选择，有权限但选错商家的对象不搜索其它商家猜归属。退出时清所有当前 user 的选择和业务缓存。
5. 切换先拦截未保存编辑并让用户保留/放弃，确认后取消旧读请求、关闭全部旧业务订阅、递增既有 generation、清旧业务缓存，再挂载新商页。已受理 mutation 不因切换取消业务执行；旧完成回调不得 toast/导航/更新新商页面。所有 query key 和 URL builder 的身份传递选一种一致策略并列清，不保留局部旧路径。
6. 撤权提交后的新授权检查拒绝访问；每次事件数据 flush 前验证当前权利，空闲 SSE 最迟 15 秒终止。有限 HTTP/文件响应已经授权并发送的 bytes 不承诺撤回；未来新请求必须拒绝。只有刷新或身份变化事件触发授权重读，避免页面轮询风暴。

## 怎么验收

- A/B 都有唯一商品和图片，两标签页同账号分别 list/编辑/看图/订阅/下载成功；交叉 id、冲突 header/query 和失效选择拒绝。
- mock 网络延迟：A 的 query、mutation 完成及 SSE 在切到 B 后到达，B 不出现 A 内容、toast 或跳转；A 已受理任务保持 A 的商家/消费归属。
- 成员撤销、停用、无成员 Operator、重新登录不同 user、深链接和无 sessionStorage 条件；活跃和空闲 SSE 分别验撤权。
- 390×844、1280×800、1440×960，现有四语和明暗模式；商家长名、选择列表为空/失败/分页、键盘与未保存对话框。复用仓库 UI token，不新造配色。
- 受影响 Go 读者/写者及 migrate（若涉及 schema）、前端 focused tests 与 package 静态门、实际浏览器 mock 链；`just docs-check`。保存请求路径表与截图，不只截顶栏。

## 阻塞与交接

- 原因：无；身份入口已交付于 `5aefc015`，共享写入范围已释放。
- 解除条件：已满足；开发协调者于 2026-09-07 核对前置提交和工作区后恢复开放。
- 跟进者：开发协调者。
- 交接：未开工，无资源占用；依赖通过后可自主解阻，不需用户重复审批。

## 证据

- 命令 / 日期 / 结果：待执行。
- 基线 commit / artifact：执行时固定。
- 交付定位：随本任务提交。
- 审核者 / 结论：待审核。
- Issue 结果 / 业务门槛结果 / 剩余缺口：未交付；不开放真实第二商家。
