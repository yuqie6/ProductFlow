# Agent 画布 Goal 托管环

- 文档状态：Draft
- 目标合同：[`docs/adr/0009-agent-canvas-sandbox.md`](../adr/0009-agent-canvas-sandbox.md) §5
- 已接受图权威：[`docs/adr/0008-free-canvas-agent-graph-authority.md`](../adr/0008-free-canvas-agent-graph-authority.md)
- 第 1～4 刀已落地。Goal 托管环接到显式 `AgentTask`：商品路径 Turn / `WorkflowGraphRun` 结束保持 `waiting_user` + `goal_loop`，完成由 `POST /api/v2/agent-tasks/{id}/complete`。当前事实写在 `CONTEXT.md`、`docs/PRD.md`、`docs/ARCHITECTURE.md`。

## 相对当前产品多出来的东西

用户在已能跑的图上明确立目标。`AgentTask` 文本 = 目标 + 完成标准。循环使用已有工具：`request_workflow_run` → 等结果 → inspect → `apply` / `propose` → 再请求跑图。

状态投影到现有 `AgentTaskStatus`：进行中 / 暂停 / 用户完成 / 清除。续跑 opt-in。完成由用户点完成；图上可核对完成标准尚未自动化。默认仍要画布确认跑图。main 不承诺进程外自己跑。

一问一答不要求 Task。开聊不是 Goal。

## 验收

- 用户必须显式开始 Goal；创建商品对话不会插入 onboarding Task。
- Goal 运行中关闭对话仍可操作整张画布。
- `complete` 只能由用户或图上可核对的完成标准触发。
- Goal 状态不接管 `WorkflowGraphRun`。

## 明确排除

- 不把 `@narumitw/pi-goal` 等社区插件装进 `agent-service`。
- 不合并 Session / Task / WorkflowGraphRun。
- Agent 对话壳（侧栏收起、手机 sheet、chip）不在本文。画布手感以 [`workbench.md`](workbench.md) 为准。
