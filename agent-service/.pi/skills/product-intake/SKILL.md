---
name: product-intake
description: Create a product workspace from global scope, or persist intake and expand a name-only product graph.
triggers:
  - create product
  - find product
  - complete intake
  - reference images
  - image types
owns_tools:
  - ask_user
  - list_products_v1
  - inspect_products_v1
  - create_product_workspace_v1
  - get_product_workflow_context_v1
  - list_product_image_assets_v2
  - inspect_product_image_assets_v1
  - finalize_product_intake_v1
scope: any
version: 4
---

# 商品 Intake

## 何时使用

用户要从全局会话创建或查找商品工作区，或在商品会话补全 intake；也用于 intake 已存在而图仍是 name-only `product_source`（`birth_expandable` 为真）的情况。

## 前置事实

全局会话先用 `list_products_v1` 搜索。需要消歧时用 `inspect_products_v1` 检查候选；只有用户明确要求新建且名称确定时才调用 `create_product_workspace_v1`。创建工具只建立商品和画布会话，不写 intake、不上传图片、不启动运行。

商品会话按下面的 intake 流程执行。

先读 `get_product_workflow_context_v1`。已上传的 reference asset ID 和用户点名的图片类型是权威；需要确认可用资产时读 `list_product_image_assets_v2`，需要看图时用 `inspect_product_image_assets_v1` 检查明确选中的资产。

若上下文仍缺图片类型、每类数量或 reference selection，必须在完成必要读取后的同一 Turn 调用 `ask_user`。不要输出“我先读取”“稍后补齐”一类计划后结束 Turn，也不要用普通回复代替结构化问题。

## 工作循环

0. 全局会话只处理商品查找或工作区创建。已有同名候选时先消歧，不重复创建；创建成功后让界面进入返回的商品工作区，本 Turn 不继续写 intake。
1. 收集图片类型与 `reference_asset_ids`。
   将用户的图种名称映射到当前上下文 `image_type_catalog`，`selection.image_types[].key` 使用目录项的原始 key，数量沿用用户已确认的选择。不要把中文名称自行翻译成新的枚举；目录无法支持的图种须说明并澄清。
2. 用户没点名图种时，用 `ask_user` 提问。默认选项为推荐套图：封面主图 2、核心卖点图 4、规格参数图 1、SKU 1、场景 1、细节 1。不要在用户未确认时直接 finalize 这一套。
3. 用户只点封面/主图和细节时，追问是否补上卖点图、规格图和选款图；选项为「补齐推荐套图」和「只要我说的这几种」。未确认前不要 finalize 纯摄影套图。
4. 调用 `finalize_product_intake_v1`，再重读上下文。该工具写入 intake 并展开摄影/信息图模板（每种生成类型一组 + prompt + N 个 image 节点）。
5. 若 intake 已在且 `birth_expandable` 仍为真：用同一 selection 再调用一次以展开模板。

## 禁止行为

不要用 `propose_graph_change_set_v1` 发明第一份完整拓扑。不要从文件名推断品牌、材质、尺寸、颜色。不要去确认提案或启动运行。不要把用户打发回创建表单。

## 完成判据

全局创建返回真实商品工作区，或商品会话中的 intake 已写入且图已从 name-only 展开为模板。之后的图编辑交给 `graph-editing`。
