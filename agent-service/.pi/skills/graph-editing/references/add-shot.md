# 添加一组生成镜头

已展开的 live graph 上再加一个摄影或信息图类型时使用。形状与画布「添加场景」相同。用 `propose_graph_change_set_v1` 提出，不要 apply 多节点 overlay。

## 操作

1. `create_group`，新 `client_ref` 与镜头标题。`member_refs` 可空；后续 `create_node` 用 `group_ref`。
2. `create_node` `node_type=image_prompt` 放进该组。配置含 `image_type_key` 和 `prompt.design_goal`。
3. 一个或多个 `create_node` `node_type=image_generation` 放进该组。配置用同一 `image_type_key` 和 `generation_spec`。
4. `connect_nodes` 从 prompt 接到每个 image 节点。`connect_nodes` 有 `client_ref`、`source_ref`、`target_ref`，可选 `order`。没有 `role` 或 `data_type`。
5. `connect_nodes` 把已有 `product_source`、`creative_brief`、`visual_system` 接到新 prompt（有 visual 时也接到 image 节点）。

证据类（certification、factory）是未绑定的 `image_asset` 占位，不是这组镜头。

不要提出第二份完整拓扑去重建 source、brief、visual 和全部已有镜头。
