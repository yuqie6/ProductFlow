# ADR 0001: Agent、WorkflowDraft 与业务状态的权威边界

## 状态

Accepted

## 背景

Agent 需要通过多轮对话读取图片、补齐商品事实并提出工作流方案。模型输出、浏览器动画和 ProductFlow 业务写入具有不同的持久化与失败语义。把自然语言终答直接解析成 DAG，或让 Agent 逐节点写入正式工作流，都会在断线、重试和部分失败时留下不可审阅的业务状态。

## 决策

- ProductFlow PostgreSQL 持有商品、事实、资产、WorkflowDraft、最终 DAG 和业务任务状态。
- Go Agent service journal 持有 durable Turn、transcript、Question、工具步骤、token delta 和事件游标。
- Agent 只产出版本化的结构化 WorkflowDraft artifact。
- 用户确认针对一个明确的 Draft revision。
- ProductFlow 在一个事务内校验并物化完整 DAG，同时写入 reveal events。
- reveal events 只决定展示顺序，不驱动业务对象逐个落库。
- Agent 业务工具通过有界、作用域明确、可幂等对账的 ProductFlow internal API 执行。

## 后果

- 浏览器断线或动画取消不会产生半张工作流。
- 相同幂等键只有在请求哈希相同时才能复用结果。
- Agent Turn 或工具副作用无法证明成功时保留 `unknown`，不猜测终态。
- ProductFlow 可以保存网页投影，但不能拼接另一份供模型继续调用的 transcript。

## 排除方案

- 解析 assistant prose 得到正式工作流。
- Agent 循环调用 create-node/create-edge 完成物化。
- 浏览器绕过 FastAPI 直接信任内部 Agent service。
