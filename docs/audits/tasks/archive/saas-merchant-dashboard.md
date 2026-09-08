# 任务：商家从真实工作概览进入商品与待处理任务

状态：完成
类型：实现
认领者：account_backend
认领于：2026-09-08T09:00:34+08:00
完成后可拆：无

遵循 [Issue 协议](../README.md)。

## 问题来源

用户要求成熟个人账户、专业仪表盘与后台。商品入口现可显示自有商品、当前已采用交付及四来源工作，按明确状态与滚动时间查找记录并继续已有业务。沿真实持久数据实现，不增加销售/收入占位、团队身份或平行首页。完整目标仍见 [路线图](../../../ROADMAP.md#14-当前完整目标的验收边界)。

account_backend 独占 Go product 概览与共享工作读者，account_frontend（Astra low）独占 Web 商品入口、ImageChat 接线及测试，root 冻结合同、审阅全部交付、维护文档并执行真实整合。身份、Ops 与偏好依赖已分别交付6d431274、b251f20b、931c8d60。没有更改 Agent 执行机制或生成请求。

## 验收合同

GET `/api/v2/products/overview` 使用普通商品组的自有商家授权，不是 Ops 路由。原商品列表响应保持。查询 `days=7|30`（默认30）、`kind=all|agent_task|workflow_run|image_session|local_edit`（默认all）、`state=all|active|waiting|unknown|failed`（默认all）、`page`1–100000、`page_size`1–100（默认10）。非法值400。

响应：`as_of`、`recent_window:{days,from,to}`、`products:{total,current_adopted}`、`work:{by_source,records:{items,total,page,page_size}}`。by_source 固定四种来源，每项 `{active,waiting,unknown,recent_failed}`。所有数字为真实商家范围，不累计跨来源“生产任务总量”。商品与按来源统计不受记录列表kind/state分页影响；该区别在界面体现。

active 为 queued/running，waiting 为 waiting_user/awaiting_confirmation，unknown 单列，均不受日期限制。recent_failed 为 failed 且活动时间在窗口内；活动时间明确取已记录的 finished_at，缺失时取 started_at/created_at，响应仍保留各原始 nullable 时间，不伪造完成时间。图记录起点沿 started_at；Agent取消时间可沿现有COALESCE(finished_at,canceled_at)。窗口边界 `[from,to]`，UTC滚动7或30×24小时，不宣称自然日趋势或用户时区报表。

默认记录集合包含当前 queued/running/waiting_user/awaiting_confirmation/paused/draft/unknown（不限日期）以及窗口内其它终态记录。state筛选在此集合上收窄；failed只对应窗口内失败，unknown不并入failed。分页按created_at降序、kind升序、id降序固定，所有参数可回读。日期不会隐藏老的待继续工作；历史失败不宣称仍未修复。

每条复用 `{id,kind,product_id,title,status,created_at,started_at,finished_at,failure_reason}`，补 `product_name:string|null` 和 `session_id:string|null`（Agent或连续生图会话）；id本身已是任务/运行ID，不重复提供同值别名。已采用商品沿当前指针连接同商品的版本，只计商品不计旧版本，且不宣称全部图片质检合格。错误只返回公开摘要，不泄漏供应商原始载荷。

Go实现已将operator_records内四来源查询提取为共用内部读者，旧Ops响应/权限/分页不变；没有新增schema或业务状态。多语与状态文案在商品列表懒加载消息中维护，不扩大首屏。前端用现有商品/Agent入口；连续生图显式会话入口保证服务端归属校验，失效或跨商ID不自动创建/切到其它会话。运行资源仍按本任务独立前缀和专用端口；共享栈不重启。

## 页面与身份边界

保留/products原目录，在其上方组合商品概览和有界工作记录；记录筛选/分页用独立URL参数，不改变目录q/page/sort。四来源分别展示，图运行和局部编辑链接明确称打开商品，不虚构运行定位参数。

account_frontend 的排他范围包括 ImageChatPage 及其现有路由状态模型、必要 App 身份key。新增 `/image-chat?image_session_id=<id>`；显式参数优先直接读取详情，不依赖列表第一页。有参数时禁止空列表自动创建与找不到列表项自动选首条；失效/跨商/空值明确显示错误与返回会话列表，不产生写入。用户主动选会话、URL后退及切换复用选择清理边界。原模块内会话草稿Map需按账号/商家与现有世代分域，页面随身份重挂；不新增身份store，不修改生成命令。此范围没有其它在写的Web任务，backend图片业务和共享运行资源保持原归属。

验证包含第二页会话深链接、未知/跨商/空ID无POST副作用、URL切换迟到响应不覆盖、换账号不沿用旧草稿/图片引用。局部导航接线不冒充图片生成质量提升。新增业务代码与消息留在懒加载页面，入口预算未提高。


## 验收与限制

后端执行者自审后交付5个Go文件。root审阅查询、授权、原Ops投影及全量任务diff，发现显式kind/state空值被当默认值，执行者补400回归。root使用独立 `pf_dashboard_root_0908_gotest_product` 跑完整product包通过（4.477s），路由合同通过；双商家与空集合、当前采用指针、原时间、7/30窗口与边界、分页、四来源状态、失败脱敏均有PostgreSQL回归。早期执行者使用默认派生 `productflow_dev_gotest_product`，最终接受的是root独立库结果。

前端113文件774测试、lint与设计token、构建预算通过。27条新页面浏览器覆盖四语言、390/1440明暗矩阵、1024、键盘触摸、空态错误恢复、分页与真实目标参数；注册10条与原账户/Ops/身份6条通过。最后缓存/面板保持修正后7条身份与显式会话回归通过，包含无匿名中间态换账号、迟到改名与新账号正常保存。入口999970B/gzip274232B，原1MB预算未提高。

root在29982/29983与 `pf_dashboard_0908_live` 独立栈验证真实登录、双商家商品隔离、45天前等待与unknown不被7天筛掉、近期/40天失败边界、非法参数400、无商家Operator403、真实Agent目标链接、实际连续生图跳转、失效/空目标不写入；桌面与390手机截图已检查，无横向溢出与pageerror。尝试1脚本误替换bootstrap邮箱变量，尝试2图夹具scope误填all；修正夹具后尝试3 API通过，尝试4最终API+浏览器通过，失败材料保留。没有执行图片生成或模型调用。

最终源码指纹与root日志位于 `.debug/saas-dashboard-20260908/`（accepted-product.log、routes.log、final-go.sha256、live-4-browser）；Web22文件指纹、各验证日志与多语截图位于 `.debug/saas-merchant-dashboard-20260908/`。root API/preview与自身数据库已清理，执行者29383预览已停止。共享开发栈未重启或迁移；页面在该栈生效需要运行更新后的服务。当前记录是查询时状态，不宣称多条SQL为同一数据库快照，不构成完整SaaS或容量、生成质量验收。

验收后并行会话继续修改graph显式Guard参数及其product调用者，这些未提交改动不属于本单；本单五个Go文件和22个Web文件与验收指纹一致。局部证据不替代该并行重构的整树验收。
