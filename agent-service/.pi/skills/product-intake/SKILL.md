---
name: product-intake
description: Persist missing image types and already-uploaded reference assets, then expand a name-only live graph.
triggers:
  - create product
  - complete intake
  - reference images
  - image types
guards_tools:
  - ask_user
  - get_product_workflow_context_v1
  - inspect_product_image_assets_v1
  - finalize_product_intake_v1
scope: product_workflow
version: 2
---

# 商品 Intake

## 何时使用

用户在创建或补全商品工作区，intake 为空，或 intake 已在而图仍是 name-only `product_source`（`birth_expandable` 为真）。

## 前置事实

先读 `get_product_workflow_context_v1`。已上传的 reference asset ID 和用户点名的图片类型是权威。只在缺了会改变计划产出的事实时用 `ask_user`；不得只在普通回复里列问题后结束 Turn。需要看图时用 `inspect_product_image_assets_v1` 检查明确选中的资产。

## 工作循环

1. 收集图片类型与 `reference_asset_ids`。
2. 调用 `finalize_product_intake_v1`，再重读上下文。该工具写入 intake 并展开摄影/信息图模板（每种生成类型一组 + prompt + N 个 image 节点）。
3. 若 intake 已在且 `birth_expandable` 仍为真：用同一 selection 再调用一次以展开模板。

## 禁止行为

不要用 `propose_graph_change_set_v1` 发明第一份完整拓扑。不要从文件名推断品牌、材质、尺寸、颜色。不要去确认提案或启动运行。不要把用户打发回创建表单。

## 完成判据

intake 已写入，图已从 name-only 展开为模板。之后的图编辑交给 `graph-editing`。
