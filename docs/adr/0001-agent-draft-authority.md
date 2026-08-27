# ADR 0001: Agent、Draft 与业务状态的权威边界

## 状态

Accepted。商品拓扑由 ADR 0008 / 0009 取代：创建写 live graph，Agent 用 ChangeSet，不再产出商品 WorkflowDraft。本 ADR 仍约束全局库整理 Draft、unknown 语义和业务权威在 PostgreSQL。

## 背景

Agent 需要通过多轮对话读取图片、补齐事实并提出可审阅的业务变更。模型输出、浏览器动画和 ProductFlow 业务写入具有不同的持久化与失败语义。把自然语言终答直接解析成正式状态，或让 Agent 绕过 application 逐条写入，都会在断线、重试和部分失败时留下不可审阅的业务对象。

## 决策

- ProductFlow PostgreSQL 持有商品、事实、资产、全局库整理 Draft、canonical graph 和业务任务状态。
- main 的 Node.js/Pi adapter session 文件保存交互式 Turn transcript；这些文件不是业务权威，也不能证明后台 durable Turn、效果对账或多实例恢复。
- 商品路径 Agent 通过 Graph Command 应用或提出 ChangeSet，不提交覆盖现图的 WorkflowDraft。
- 全局库整理仍使用版本化 `LibraryOrganizationDraft`。用户确认针对一个明确 revision；ProductFlow 在确认事务内重新观察事实并应用。
- Agent 业务工具通过有界、可幂等对账的 ProductFlow internal API 执行；Pi、Skill 和 adapter 不直接访问 PostgreSQL、Redis、storage 或 provider。
- Agent Turn 或工具副作用无法证明成功时保留 `unknown`，不猜测终态。
- ProductFlow 可以保存网页投影，但不能拼接另一份供模型继续调用的 transcript。
- reveal / 展示顺序不驱动业务对象逐个落库。

## 后果

- 浏览器断线或动画取消不会产生半份已确认业务状态。
- 相同幂等键只有在请求哈希相同时才能复用结果。
- 商品图画布不依赖 Draft 确认闸门；库整理仍需确认。

## 排除方案

- 解析 assistant prose 得到正式工作流或库整理结果。
- Agent 绕过 Graph Command / Draft 确认，循环调用底层 CRUD 完成物化。
- 浏览器绕过 FastAPI 直接信任内部 Agent service。
