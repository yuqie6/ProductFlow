# 业务后端按垂直切片迁到 Go

Python 业务 API、worker、dispatcher 一次换成 Go，内部按业务功能竖切。封印线是当时 live Python 的用户可观察行为与 HTTP / SSE / queue 合同，不是 Python 目录布局。Web 与 Pi Agent 默认零合同变更。工作台浏览器证明仍是 cutover 闸门，不再当作开始写 `go/` 的前提。Gin / GORM / asynq 在 cutover 前不写进 CONTEXT / PRD / ARCHITECTURE 的当前事实段落。

## 状态

Accepted。Cutover 已完成；Python 封印树已离开主线，见 0016。当前运行形状见 [`docs/ARCHITECTURE.md`](../ARCHITECTURE.md)。封印 HTTP / SSE / queue 合同在仓库根 `contracts/`。

## 考虑过的方案

- 先在 Python 里按包挪文件，再翻译成 Go。拒绝：同一条包边界会被碰两次。
- 等工作台 1440/1024/390 连续动作证明完成再开工。拒绝：证明仍约束 cutover 和产品完成度，不阻塞导出合同包与 `go/` host。
- 把 `exp` Go Agent 当业务后端起点。拒绝：那是 Agent runtime 实验，见 ADR 0007。

## 后果

- 搬家必须带走 Graph Command、`unknown`、Goal 与 GraphRun 分家、商品 WorkflowDraft 已删除。不得再立 `workflow_drafts` 或创建时的 onboarding Task。
- Python 里的泄漏缝（多条出生路径、Turn 嵌套 commit、双队列入口、残留词）按设计文档「带走 / 不搬」表处理：可观察行为封印，名字和内部事务形状可以在竖切时收口。
- 工作台规格的实施仍不插入这场搬家。
