---
name: workflow-run-request
description: Create a pending request to run an existing ProductFlow workflow without starting it.
triggers:
  - run workflow
  - retry workflow
  - generate images
  - run existing graph
owns_tools:
  - ask_user
  - get_product_workflow_context_v1
  - inspect_workflow_runs_v1
  - inspect_global_workflow_context_v1
  - request_workflow_run_v1
  - request_global_workflow_run_v1
scope: any
version: 1
---

# 工作流运行请求

## 何时使用

用户要求运行或重试一张已经存在的工作流。

## 前置事实

读当前工作流与 revision。商品会话用 `request_workflow_run_v1`。全局会话先 `inspect_global_workflow_context_v1`，再 `request_global_workflow_run_v1`，并给出 `product_id` 与 `workflow_id`。只在用户明确要重试时带 `source_run_id`。可选 `scope` 为 `graph`（默认）、`node`、`to_node`、`selection`；后两者分别需要 `node_id` 或 `node_ids`。

## 工作循环

1. 读取当前 revision 与可运行节点。
2. 提交对应的 request 工具。
3. 告诉用户必须在 ProductFlow UI 确认后才会真正开跑。

## 禁止行为

不要声称运行已经开始。不要自己 cancel、retry 或 materialize。运行失败的解读交给 `run-diagnosis`。

## 完成判据

已有一条 pending 的运行请求，等待用户确认。
