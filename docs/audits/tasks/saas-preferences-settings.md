# 任务：个人偏好、商家资料和站点配置各有明确页面与所有权

状态：阻塞
类型：实现
认领者：—
认领于：—
业务组：商家平台
父账本：merchant-platform.md
完成后可拆：商家首页与运营概览详细实施任务

任务状态、认领及提交遵循 [Issue 协议](README.md)。

## 问题来源

现有 `/settings` 是实例 provider/运行配置，商家角色不得进入；`preferences.tsx` 是浏览器偏好。需要个人跨设备偏好和商家资料，但不能把实例 app_settings 复制成一份没有读者的商家 JSON，也不能让界面语言影响商品输出语言。

## 做成什么样

- `/account` 的偏好区持久化本人界面语言、主题；登录前可本机选择，登录后从本人设置恢复。
- `/merchant/settings?section=profile` 显示商家名称、显示时区与当前角色；Owner 编辑，Editor/Viewer 只读。团队区复用前置任务。
- 既有 `/settings` 继续是 Operator 实例配置，使用运营范围导航；账号菜单和商家导航不会混淆这三个位置。

## 前置与并行

- 前置：[saas-account-team](saas-account-team.md) 已交付账户/商家页面壳、更新冲突和审计机制。
- 冻结输入：现有 locale/theme 枚举、商品事实/DeliverySpec/Brand 继承规则、实例 provider 绑定；不改变已受理任务。
- 运行资源：隔离 PG、Web/browser；无需 worker 或真实 provider。
- 不与 auth/schema、preferences/i18n、App/TopNav/SettingsPage 写者并行。

## 只改这些文件

- `go/internal/auth/` 的账户/商家资料用例和 HTTP、schema identity/migrate/测试；不将新的业务所有权挂到实例 settings 包。
- `web/src/lib/preferences.tsx`、主题/locale 现有辅助文件、`api.ts`、`types.ts`、`i18n.ts`、前置账户/商家页面、`SettingsPage.tsx` / TopNav 的导航接线及测试。
- 路由合同、本任务 e2e、本文件；协调者整合活文档和父章程。

## 不要碰

- 新的生成默认参数、商家自带密钥、全站主题重设计、Logo 上传、营业资料/发票信息、支付、Brand 合并语义。
- 不在账户偏好里添加输出语言，不用空字符串表示清除所有配置。

## 现在代码在哪

`web/src/lib/preferences.tsx`、`theme.ts`、`i18n.ts`、`web/src/pages/SettingsPage.tsx`、`go/internal/settings/http.go`、`go/internal/platform/db/schema/identity.go`、`web/src/pages/workbench/chrome/workbenchUiState.ts`。认领时确认实际 theme helper 文件名，复用既有 enum 和 parser。

## 合同

1. 固定字段：User display_name（前置已有）；User preference locale、theme；Merchant name、time_zone。不建任意 key/value 设置 API。theme 复用 system/light/dark，locale 复用现有四语；time_zone 使用 Go/浏览器可识别 IANA 名称，默认 UTC。
2. 资料与偏好采用带非负整数 revision 的强类型 GET/PATCH，新增独立 UserPreferences 1:1 行和 Merchant revision/time_zone，缺 user preference 返回服务端默认而不在 GET 写库。PATCH 带 expected_revision，只更新请求提供的字段；过期 409 保留用户草稿并可重读比较。revision=0 表示尚无偏好行，并发首次创建只能一个成功。
3. 新用户偏好默认 zh-CN/system；登录前用本机设置，登录后以服务端本人偏好为准，不自动上传共享浏览器旧值。显式保存后同步服务端和当前界面；另一标签页收到失效通知后重新读取本人偏好，不广播密码/会话。退出清本人缓存，保留登录前本机默认。
4. 商家时区只用于商家列表、活动和消费日期显示/筛选，UTC 持久时间不变；更新不改历史账本日戳或在途任务。日期范围由该时区本地日界换算 UTC，夏令时按 IANA 规则。个人语言不改商品文案/出图语种。
5. Owner 更新商家 name/time_zone 必须有当前有效权利、商家 active；停用商家只读。改名不改 merchant_id，不让名称成为 URL 授权边界。
6. 实例 settings/provider 密钥仍只给 Operator；商家生产页消费现有非秘密能力投影。品牌/商品/节点覆盖链保留原有模型；本任务不创造未接执行器的新生产设置。

## 怎么验收

- 本人偏好跨会话恢复、不同 user 同浏览器不串、两并发 PATCH 一个 409、缺行 GET 不写库、无效 locale/theme/time_zone 拒绝。
- Owner/Editor/Viewer/Operator 角色矩阵；跨商 PATCH 不能修改；设置修改不改变已受理运行 snapshot、价格版本或商品输出语言。
- UTC/Asia-Shanghai/America-New_York 日期显示与夏令时筛选边界；长商家名、错误/保存中/409 草稿恢复；390×844/1440×960、四语明暗和 system theme。
- 受影响 Go/PG/migrate，前端 focused tests、lint/build、浏览器操作，`just docs-check`。仅保存截图不算持久化证据。

## 阻塞与交接

- 原因：账户/团队任务未交付，共用页面和身份合同尚未固定。
- 解除条件：协调者核验前置 commit 和文件占用后开放。
- 跟进者：开发协调者。
- 交接：未开工，无资源占用。

## 证据

- 命令 / 日期 / 结果：待执行。
- 基线 commit / artifact：执行时固定。
- 交付定位：随本任务提交。
- 审核者 / 结论：待审核。
- Issue 结果 / 业务门槛结果 / 剩余缺口：未交付。
