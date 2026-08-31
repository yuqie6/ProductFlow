---
name: graph-editing
description: Apply one reversible Graph Command or propose a multi-node overlay on a live schema-v3 graph.
triggers:
  - edit graph
  - add shot
  - connect nodes
  - rename node
  - propose overlay
owns_tools:
  - get_product_workflow_context_v1
  - get_node_detail_v1
  - apply_graph_change_set_v1
  - propose_graph_change_set_v1
  - discard_workflow_proposal_v1
  - focus_canvas_items_v1
scope: product_workflow
version: 3
---

# 图编辑

## 何时使用

用户要改当前商品的 live schema-v3 图：单点配置、连线、改名，或一次加入一组镜头。

## 前置事实

先读 `get_product_workflow_context_v1`。需要单个节点正文时用 `get_node_detail_v1`。操作名只来自 apply/propose 工具 schema：`create_node`、`update_node_config`、`rename_node`、`delete_node`、`connect_nodes`、`disconnect_edge`、`reorder_edges`、`move_nodes`、`create_group`、`move_nodes_to_group`、`rename_group`、`dissolve_group`。

## 工作循环

1. 一次可逆编辑（一个节点配置、一条边、一次改名）：`apply_graph_change_set_v1`，`operations` 恰好一条。用户给出精确节点标题和新值，且最新上下文中该标题只匹配一个节点时，必须用该节点 ID 和最新 revision 直接执行；只有零匹配或多匹配时才提问。
2. 多节点重建、加一组镜头、批量删除：`propose_graph_change_set_v1`。镜头配方见 `references/add-shot.md`。用户只说“加一组镜头”而未给数量时，使用一张生成图作为有界默认值并直接提交可审阅 overlay，不为数量重复提问。
3. 需要画布对准某些节点时用 `focus_canvas_items_v1`。
4. 用户放弃未应用提案时用 `discard_workflow_proposal_v1`。
5. 冲突则重读上下文，用最新 revision 再算，不要重放同一 payload。

## 禁止行为

不要发明 `add_node` 或 `connect`。不要在已展开的图上再提交一份完整拓扑。不要声称提案已经写入 live graph。不要在全局会话上改图。

## 完成判据

单次 apply 返回已应用，或提案作为未应用 overlay 等待画布确认。
