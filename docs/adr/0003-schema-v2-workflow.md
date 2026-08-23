# ADR 0003: Schema-v2 工作流、提示词和交付派生

## 状态

Accepted。在线图画权威已由 ADR 0008 / schema-v3 `workflow_graphs` 取代。本文仍约束 GenerationSpec、实测输出和 DeliverySpec 的分离，以及一层画布分组。

## 背景

V1 使用批量生图动作节点和下游结果槽位，运行血缘依赖 JSON 并行数组。它难以表达每张计划图片的独立提示词、参考绑定、重跑结果、供应商有效参数和交付规格。schema-v2 曾是治理期间的在线合同；schema-v2 从未上生产，在线 DAG 表已由 `20260821_0080` 删除。

## 决策

- 在线工作流保存在 schema-v3 `workflow_graphs`。`product_context` / `reference_image` / plan key 只留在 WorkflowDraft 与配方内部 payload，不进入 live `config_json`。
- 每个参考节点绑定一张 ProductImageAsset。
- 每个计划输出由一个可运行图片节点承载；一次节点运行只生成一张模型图片。
- 提示词、视觉体系、商品事实和生成记录保存不可变版本及引用快照。
- GenerationSpec、provider effective values 和 actual output 分开保存和展示。
- DeliverySpec 触发确定性 rendition job，不增加 DAG 节点，也不重新调用图片模型。
- 画布分组只组织一层局部画布；WorkflowRecipe 只保存用户主动选择的可复用结构，应用到 live graph 走同一套 Graph Command。

## 后果

- UI 不得把请求尺寸显示成实际尺寸。
- 修改交付格式或尺寸不会改变生成原图和运行历史。
- 文件夹聚合状态由成员节点推导，数据库不存在文件夹运行状态。
- 配方应用到另一商品前预览将出现的节点和连线，确认后一次写入 live schema-v3 graph；不再先写成待确认 Draft。

## 排除方案

- 保留 V1/V2 双执行器。
- 用一个批量节点隐藏多个结果身份。
- 为导出增加 DAG 节点。
- 把画布文件夹升级为嵌套、带端口的执行组件。

## 后续决策

ADR 0008 是当前在线图、typed edge、Graph Command 和 GraphProposal 合同。Draft 拓扑收敛为 WorkflowIntent + ChangeSet、以及 v1 archive 重建，仍待后续切片。
