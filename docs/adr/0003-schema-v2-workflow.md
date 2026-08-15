# ADR 0003: Schema-v2 工作流、提示词和交付派生

## 状态

Accepted

## 背景

V1 使用批量生图动作节点和下游结果槽位，运行血缘依赖 JSON 并行数组。它难以表达每张计划图片的独立提示词、参考绑定、重跑结果、供应商有效参数和交付规格。

## 决策

- 在线 ProductWorkflow 和 WorkflowNode schema 固定为 2。
- 节点类型限于 product context、reference image、prompt generation 和 image generation。
- 每个参考节点绑定一张 ProductImageAsset。
- 每个计划输出由一个可运行图片节点承载；一次节点运行只生成一张模型图片。
- 提示词、视觉体系、商品事实和生成记录保存不可变版本及引用快照。
- GenerationSpec、provider effective values 和 actual output 分开保存和展示。
- DeliverySpec 触发确定性 rendition job，不增加 DAG 节点，也不重新调用图片模型。
- WorkflowFolder 只组织一层局部画布；WorkflowRecipe 只保存用户主动选择的可复用结构。

## 后果

- UI 不得把请求尺寸显示成实际尺寸。
- 修改交付格式或尺寸不会改变生成原图和运行历史。
- 文件夹聚合状态由成员节点推导，数据库不存在文件夹运行状态。
- 配方应用到另一商品时先生成 Draft，重新绑定商品事实、素材和提示词后才能物化。

## 排除方案

- 保留 V1/V2 双执行器。
- 用一个批量节点隐藏多个结果身份。
- 为导出增加 DAG 节点。
- 把画布文件夹升级为嵌套、带端口的执行组件。
