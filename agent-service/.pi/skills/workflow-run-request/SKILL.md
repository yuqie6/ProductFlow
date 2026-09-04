---
name: workflow-run-request
description: Create a pending request to run an existing ProductFlow workflow without starting it.
triggers:
  - run workflow
  - retry workflow
  - generate images
  - run existing graph
  - cancel workflow run
owns_tools:
  - ask_user
  - get_product_workflow_context_v1
  - inspect_workflow_runs_v1
  - inspect_global_workflow_context_v1
  - cancel_workflow_run_v1
  - request_workflow_run_v1
  - request_global_workflow_run_v1
scope: any
version: 2
---

# 工作流运行请求

## 何时使用

用户要求运行、重试或取消一张已经存在的工作流运行。

## 前置事实

读当前工作流与 revision。商品会话用 `request_workflow_run_v1`。全局会话先 `inspect_global_workflow_context_v1`，再 `request_global_workflow_run_v1`，并给出 `product_id` 与 `workflow_id`。只在用户明确要重试时带 `source_run_id`。可选 `scope` 为 `graph`（默认）、`node`、`to_node`、`selection`；后两者分别需要 `node_id` 或 `node_ids`。取消请求先用运行列表确定唯一 `run_id`；目标不明确时提问。

## 工作循环

0. 用户明确要求取消时，读取运行列表；目标唯一且仍可取消才调用 `cancel_workflow_run_v1`，然后停止。
1. 读取当前 revision 与可运行节点。
2. 提交对应的 request 工具。
3. 告诉用户必须在 ProductFlow UI 确认后才会真正开跑。

## 禁止行为

不要声称运行已经开始。不要擅自 cancel、retry 或 materialize；取消必须来自用户明确请求。运行失败的解读交给 `run-diagnosis`。

## 完成判据

已有一条 pending 的运行请求等待用户确认，或用户点名的运行已返回取消结果。
