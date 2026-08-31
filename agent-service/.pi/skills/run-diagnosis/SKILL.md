---
name: run-diagnosis
description: Explain why one WorkflowGraphRun failed and what to do next using bounded run detail.
triggers:
  - run failed
  - why did generation fail
  - inspect run
  - node error
owns_tools:
  - get_product_workflow_context_v1
  - inspect_workflow_runs_v1
  - inspect_global_workflow_runs_v1
  - get_workflow_run_detail_v1
  - get_node_detail_v1
  - request_workflow_run_v1
  - request_global_workflow_run_v1
scope: any
version: 1
---

# 运行诊断

## 何时使用

用户要解释某次工作流运行为什么失败、停在哪、下一步该修图还是重跑。

## 前置事实

列表摘要不够时必须用目标 `run_id` 读取 `get_workflow_run_detail_v1`。节点配置以 `get_node_detail_v1` 或上下文里的 catalog 为准。

## 工作循环

1. 商品会话：`inspect_workflow_runs_v1` 找到目标 run，再 `get_workflow_run_detail_v1`。
2. 全局会话：`inspect_global_workflow_runs_v1` 选定目标 run，再用 `run_id` 读详情。
3. 根据节点 `status` 与 `failure_reason` 说明失败点。详情里已有失败节点和原因时不必再调 `get_node_detail_v1`。需要改图时加载 `graph-editing`；用户明确要重跑时加载 `workflow-run-request`。
4. 不要把 unknown 说成 failed。

## 禁止行为

不要靠 20 条列表摘要臆造节点级原因。不要声称运行已完成，除非详情里的 status 就是终态。不要替用户确认跑图。

## 完成判据

给出有界的失败节点、原因和下一步（修图、重跑请求、或提问）。
