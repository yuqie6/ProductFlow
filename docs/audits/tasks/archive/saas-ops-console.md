# 任务：管理员可管理各商家商品与运营记录

状态：完成
类型：实现
认领者：account_frontend
认领于：2026-09-08T07:23:06+08:00
业务组：商家平台
父账本：merchant-platform.md
完成后可拆：无

任务状态、认领与提交遵循 [Issue 协议](../README.md)。

## 问题来源

用户明确平台管理员可管理所有商家的商品。旧任务限制只能读运营元数据、要求支持会话与未提供的再认证前置，不能完成实际管理职责；2026-09-08 撤销这些要求。

## 做成什么样

商家列表及显式目标商品读/事实编辑/删除 API 已由身份收敛交付。本任务补齐管理页面、必要媒体授权和操作审计，管理员可查商家、按商家查商品并查看/编辑/删除目标商品，商品仍归原商家；管理范围内的任务状态和消费记录可追溯。普通用户仍只能访问自有商家。复用现有商品服务与站点配置，不新增“全局商品”实体，不伪装商家成员。

## 前置与并行

[身份收敛](saas-identity-convergence.md) 已交付直接 users.merchant_id、session.merchant 和显式目标商家管理员接口；认领时消费该合同，不恢复成员模型。

[设计校准](saas-design-alignment.md) 已完成。本任务内核对管理员路由到用例、对象归属、审计与 UI 全链；无需团队/工作区选择或支持会话前置。隔离 PG/Redis/browser，用两个普通账号和一个管理员验证。

## 修改范围与所有权

实施前登记 auth、product、quota 的必要授权/查询/操作记录、schema 与前端 Ops 页面、共享 API/类型和测试排他范围；复用已有业务命令，不给 store 全局去掉商家过滤。不修改 Pi/Graph 执行器或支付政策。

## 合同

- 管理员入口直接验证 Operator，目标 merchant/product 在服务端加载并保持归属；没有 Membership 的管理员仍能管理。普通用户访问管理员 API 拒绝。
- 列表按名称、商家和状态查询并分页，详情展示所属商家；查看/编辑/删除走原业务不变量，日志记录真实 actor、目标、动作、时间及结果。
- 生成等新增消费不由商品读取/编辑权限隐式授予；费用归属未决定时保持不可用，不能扣错商家。调账沿现有幂等账本，操作原因和重试结果可回读。
- 当前支持会话草案删除后不恢复；管理员无需切换成商家身份。日常管理不引入统一密码再认证，具体敏感动作若需要新机制先明确需求。
- 额度单位不冒充法币；未知结果政策消费用户已裁定合同，不以历史 TTL 全额扣除为不可变前置。
- 普通用户的账户、下载与生产范围保持隔离；“全局素材库/Agent”仅同商跨商品。

## 怎么验收

A/B 商品创建、管理员跨商查看/编辑/删除与日志，普通用户交叉 ID 和管理员 API 拒绝；媒体、下载、关联任务不得绕过授权。操作失败/重试不改归属或重复副作用。桌面/手机真实浏览器与 PG 验证、受影响 Go/Web 门及 docs-check；不宣称付费运营或新的真实图片质量通过。

## 阻塞与交接

- 原因：身份收敛已交付，本次共享文件阻塞已解除；认领时仍需检查并行占用。
- 解除条件：不适用。
- 跟进者：主代理。
- 交接：前后端交付已由主代理审核；隔离运行已结束，共享开发栈未迁移或重启。

## 证据

2026-09-08，交付定位随本任务提交。主代理审阅 account_backend / account_frontend 的完整 diff，并自审 product/schema/接口/文档整合，不将自审称为独立审核。

- Go：隔离 PostgreSQL 下 auth、quota、product 全包、schema 全包和 API 路由合同通过；product race 及商品审计写入失败时事实/删除事务回滚通过。审计失败不删除媒体，确认删除后保留名称快照。日志位于 `.debug/saas-ops-20260908/accepted-*.log` 和 `product-schema.log`。
- Web：109 文件 766 测试、lint/token 检查、TypeScript、生产构建及包预算通过。Ops 浏览器 23 项覆盖四语言、桌面/手机、明暗主题及失败路径；身份边界和最终 Agent Dock 入口回归通过。最终入口变化重跑构建及相关 5 项浏览器测试，不重复宣称整套测试针对未变化源码之外的能力。证据 `.debug/saas-ops-console-20260908/`，源码清单 `source-sha256.txt`。
- 真实集成：独立普通 A/B 与无商家 Operator，真实 API/PG 验证目标商家查询、商品资料修改/409/禁止图采纳、授权媒体字节与交叉拒绝、普通账号 403、一次调账及同 key 重放、原账本操作人、删除快照。`live-4-browser` 从真实登录完成目录、资料编辑、图片加载、手机调账、账本/操作记录与确认删除，无页面异常或横向溢出。桌面/手机截图和完整日志位于 `.debug/saas-ops-20260908/live-4-browser/`。
- 原失败保留：live-1 使用直接 DB 账号夹具却按注册试用种子计算余额，live-2 明确关闭该夹具试用种子后完整 API 通过；live-3-browser 的商品卡片 accessible name 包含日期，修正验收定位器后 live-4-browser 通过，无产品代码绕过。
- 新表 `operator_product_actions` 的创建、索引和再次迁移保留历史记录通过。实际部署需迁移并重启 API；本轮未操作共享开发服务，不包含外部发布、付费生成或完整 SaaS 质量声明。
- 审核结论：本任务管理范围及正反验证满足合同，旧 Settings MerchantOpsPanel 已删除。真实任务查询保留四来源状态，产品筛选不混入无商品连续生图；未知操作不伪造成成功。docs-check 已通过。主代理创建的 pf_ops_0908 及四个派生测试库已确认无连接并删除；专用浏览器/API 进程已结束。

## 本轮排他分工

账户功能03482bc9已交付。account_frontend（Astra/low）负责Ops前端页面、必要导航/API/types/i18n与浏览器回归；主代理负责Go授权/查询/审计及schema、活文档和Git。先核现有已交付管理员端点与投影，报告实际缺口与页面方案；双方冻结wire后实施。不得改首页/媒体库/图执行/Pi或支付政策。Agent评测仅占agent-service/evals和Go eval_*观察测试，互不写入。隔离测试使用pf_ops_0908前缀、专用端口，不重启共享开发栈。主代理继续最终集成验收。

## 冻结接口（2026-09-08 本轮）

- 商家目录沿已有分页响应，新增 `q`（名称包含、最多100字符）、`status`（空/active/suspended）。新增 `GET /api/ops/merchants/:merchant_id` 返回现有 MerchantView。
- 商品读、事实更新、删除沿已有合同；Ops 事实更新拒绝 `update_node_ids`，不隐式执行图采纳。商家状态本轮仅展示，不新增停启交互。
- `GET /api/ops/merchants/:merchant_id/products/:product_id/image-assets` 沿 GalleryAssetPage 和现有筛选游标；媒体 URL 由服务端投影至 `/api/ops/merchants/:merchant_id/product-image-assets/:asset_id/download`（支持原有 variant）。封面同样使用 Ops URL。授权仍核目标商家及资产商品归属。
- `GET /api/ops/merchants/:merchant_id/tasks`：page/page_size，可选 product_id；返回 items/total/page/page_size。每项 id、kind（agent_task/workflow_run/image_session/local_edit）、product_id（可空）、title、status（各来源原状态）、created_at、started_at/finished_at（可空）、failure_reason（可空，公开摘要，不返回供应商原错误）。连续生图属于商家，无商品归属，商品筛选时不返回。只读，不提供重试/取消。
- `GET /api/ops/merchants/:merchant_id/quota/events`：page/page_size；返回 items/total/page/page_size。每项 id、merchant_id、hold_id、event_type、amount_units、available_after、reserved_after、reason、actor_user_id、created_at（可空字段显式 null）。直接读取原账本。开放现有调账 POST；相同操作失败重试复用幂等 key，成功刷新余额与账本。未知消费裁定不在本轮 UI 中开放。
- `GET /api/ops/merchants/:merchant_id/actions`：page/page_size，可选 product_id；返回 items/total/page/page_size。每项 id、actor_user_id、actor_name、merchant_id、product_id、product_name、action（product.facts.update/product.delete）、created_at、result（succeeded/rejected/unknown）、failure_reason（可空）。记录经授权并接受处理的商品修改尝试；success 与业务事务同时提交，明确业务拒绝记录 rejected，未能确认结果保留 unknown。删除后保留目标名称。调账使用独立真实额度账本，不重复记入此页签；不声称覆盖所有 HTTP 探测或历史操作。

主代理继续持有 product、schema、操作审计、任务查询、最终整合。account_backend 接管本任务 auth 商家查询与 quota 事件回读（含各自测试），不改 schema/product/Web 或账户恢复实现；account_frontend 持有上述 Web 范围。各方不覆盖其他作者修改，不提交或重启共享栈。后台查询确定性测试使用 pf_ops_reads_0908 隔离库，主代理使用 pf_ops_0908。

本轮调账边界补充：reason 必填且最多2000字符、幂等 key 最多200字符；已存在事件的数量/原因/actor 与重试入参不一致时409，不改变余额或追加事件。旧 Settings 中仅操作管理员自有商家的 MerchantOpsPanel 随新显式目标管理页面退役，删除其专用 Web 读写与翻译。
