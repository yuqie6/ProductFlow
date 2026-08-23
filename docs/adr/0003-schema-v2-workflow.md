# ADR 0003: Schema-v2 工作流、提示词和交付派生

## 状态

Accepted。在线图权威已被 [`0008`](0008-free-canvas-agent-graph-authority.md) 取代。下文「决策」记录当时选择 schema-v2 的理由，不再描述当前实现。

仍然有效、且不随 0008 作废的约束：

- GenerationSpec、provider effective values 和实测输出分开保存。
- DeliverySpec 触发确定性 rendition，不增加 DAG 节点，也不重新调用图片模型。
- 画布分组只组织一层局部流程，不是可执行节点。
- 每个参考绑定一张 ProductImageAsset；每个计划输出由一个可运行图片节点承载。

## 背景

V1 使用批量生图动作节点和下游结果槽位，运行血缘依赖 JSON 并行数组。它难以表达每张计划图片的独立提示词、参考绑定、重跑结果、供应商有效参数和交付规格。

## 决策（当时）

- 在线 ProductWorkflow 和 WorkflowNode schema 固定为 2。
- 节点类型限于 product context、reference image、prompt generation 和 image generation。
- 每个参考节点绑定一张 ProductImageAsset。
- 每个计划输出由一个可运行图片节点承载；一次节点运行只生成一张模型图片。
- 提示词、视觉体系、商品事实和生成记录保存不可变版本及引用快照。
- GenerationSpec、provider effective values 和 actual output 分开保存和展示。
- DeliverySpec 触发确定性 rendition job，不增加 DAG 节点，也不重新调用图片模型。
- WorkflowFolder 只组织一层局部画布；WorkflowRecipe 只保存用户主动选择的可复用结构。

schema-v2 从未上生产。在线 DAG 表已由 `20260821_0080` 删除。当前在线图是 schema-v3 `workflow_graphs`，见 0008。

## 后果

- UI 不得把请求尺寸显示成实际尺寸。
- 修改交付格式或尺寸不会改变生成原图和运行历史。
- 文件夹聚合状态由成员节点推导，数据库不存在文件夹运行状态。

## 排除方案

- 保留 V1/V2 双执行器。
- 用一个批量节点隐藏多个结果身份。
- 为导出增加 DAG 节点。
- 把画布文件夹升级为嵌套、带端口的执行组件。

## 后续决策

[`0008`](0008-free-canvas-agent-graph-authority.md) 是当前在线图、typed edge、Graph Command 和 GraphProposal 合同。配方应用到 live graph 走同一套 Graph Command，不再先写成待确认 Draft。
