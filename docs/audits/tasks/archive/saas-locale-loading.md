# 任务：首屏只加载当前需要的语言资源

状态：完成
类型：实现
认领者：account_frontend
认领于：2026-09-08T16:32:25+08:00
业务组：商家平台
父账本：merchant-platform.md
完成后可拆：实际浏览器验证登录、账户与工作台的首次加载

遵循 [任务协议](../README.md)。

## 问题与结果

商家概览交付的入口包999970B，距现有1MB上限仅30B。root现场确认web/src/lib/i18n.ts约573298B，静态持有四种语言字典，PreferencesProvider使其进入所有首屏。目标减少首屏不需要的语言资源，保留四语言完整性、文案类型检查、账号偏好与匿名选择行为。不提高bundle预算，不删除文案或功能。

## 约束与验证

前端由Astra low执行。执行者认领后沿入口→PreferencesProvider→translate/字典及所有同步消费者调查，采用能保持单一翻译归属的最小分拆。切换语言须防乱序加载和跨账号迟到结果，保留原账号世代机制；不得让旧账号字典/偏好回填新账号。初次进入和切换中复用既有加载/失败模式，避免短暂显示翻译key或错语言。不得只手工切chunk规避入口统计而让首屏仍下载全部词典；必须记录实际请求与总传输。

修改限web的i18n资源、PreferencesProvider及必要入口/消费者/测试；不改Go或Agent协议/生产runtime。root持有文档/Git/最终验收。必须先记录当前构建与首屏资源证据，再修改；构建预算保持不变。包内静态检查、相关偏好/身份/翻译测试、四语言登录/账户/商家目录与慢请求/失败/快速切换/跨账号真实浏览器检查，记录前后首屏资源字节与请求。视觉布局不另行重设计；具体viewport按web/AGENTS.md相关规则。

## 当前并行情况

认领时有其他会话正在修改Web视觉样式、TopNav、SettingsPage、MediaLibraryPage及workbench/image-chat/product-create组件和设计规则；这些文件不属于本任务写入范围。account_frontend独占i18n资源、PreferencesProvider和独立语言加载测试；入口或其他消费者若需要改动，先向root协调具体文件，不覆盖其他会话的工作。容量任务使用e183b66b独立checkout，eval任务只写Node/Go eval观察；不得改两任务冻结资源。共享29282/29283不重启，浏览器/预览用独立端口。未认领前不要开始实现。

## 已协调的消费者范围

account_frontend同时持有web/src/pages/workbench/canvas/catalogConfig.ts及其测试（仅移除为key校验而加载整份中文的依赖）、web/src/lib/homeMessages.ts、nodeDetailMessages.ts、preferences.test.ts、web/vitest.config.ts和独立测试加载helper。GraphCanvas/documentCandidate测试仅语言资源预加载接线，不修改生产画布行为。已有Playwright共享fixture改动须给root具体路径再协调；新增本任务独立fixture/spec可直接执行。

已核无外部改动，授权web/e2e下account、preferences、ops、registration、merchant-dashboard、identity-session-boundary、home、smtp-settings、login-security这9个.spec.ts仅翻译导入接线，复用新增fixtures/i18n.ts测试预加载；业务断言保持原合同。

GraphResultsView.test.tsx和GraphShotFilmstrip.test.tsx已核无外部改动，授权仅补已确认匿名session的SSR fixture，不改组件或业务断言。

## 交付证据与root审核

实现将四语言按内容哈希JSON分开，确认会话后只取权威语言；保留匿名主题同步应用、语言最新意图、账号世代检查和失败重试。旧homeMessages/nodeDetailMessages已删除，2418个键和四语字符串由root逐项与冻结基线校验相同。root已核34个独占路径SHA无漂移，审阅加载器、Provider、键合同、消费者接线及手机明暗截图。merchant-dashboard.spec.ts只交付翻译导入，其外部视觉测试已由原会话提交。

证据位于`.debug/saas-locale-loading-20260908/`：delivery.json、delivery-files.json、task.patch、resource-comparison.json、controlled-input-validation.json与分轮浏览器日志。受控基线入口999948B/gzip274221B，候选538682B/gzip157788B；16次四语言登录/账户/目录/工作台首次访问均只请求当前语言JSON，实际压缩响应总量减少79199–94474B。预算未调整。114个单测文件781测试、lint、受控完整构建及预算通过；198个独立浏览器场景由原轮和明确修正的定向重跑覆盖，不宣称失败轮整轮通过。16张稳定首屏截图逐像素相同。

经root授权，账户加载断言限定个人资料region，重连fixture的offline/online分为两个独立浏览器事件任务；保留原失败trace和全部身份/草稿断言。浏览器真实加载构建资源，API为模拟；生产CDN与真实认证后端不在本任务验收内。最终整树just web-build通过（入口538704B/gzip157799B），包含外部已提交概览变化的29条merchant-dashboard浏览器测试全过；构建前后与浏览器结束Web输入零漂移。30483/30484/30485自有预览已停止，无监听。integration/result.json、build.log、browser.log和complete-input-drift.json保留最终证据。
