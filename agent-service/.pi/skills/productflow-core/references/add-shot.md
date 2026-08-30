# Add a generating shot

Use when the live graph is already expanded and the user wants one more photography or infographic image type.

This is the same shape as the canvas "添加场景" action. Propose it; do not apply a multi-node overlay.

## Operations

1. `create_group` with a new `client_ref` and the shot title. `member_refs` may be empty; later `create_node` rows set `group_ref`.
2. `create_node` `node_type=prompt_generation` in that group. Config uses `image_type_key` and `prompt.design_goal`.
3. One or more `create_node` `node_type=image_generation` in that group. Config uses the same `image_type_key` plus `generation_spec`.
4. `connect_nodes` from the prompt to each image node. `connect_nodes` has `client_ref`, `source_ref`, `target_ref`, and optional `order`. It has no `role` or `data_type`.
5. `connect_nodes` from existing `product_source`, `creative_brief`, and `visual_system` nodes onto the new prompt (and visual onto the image nodes when those shared nodes exist).

Use Graph Command `op` names from the propose tool schema. Do not invent `add_node` or `connect`.

Evidence types (certification, factory) are unbound `image_asset` placeholders, not this shot shape.

Do not propose a second complete topology that rebuilds source, brief, visual, and every existing shot.
