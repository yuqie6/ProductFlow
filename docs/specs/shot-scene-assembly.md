# 镜头 / 场景装配

## 1. 状态

- 文档状态：Approved
- 批准依据：仓库目标「完成 Shot/scene assembly plan」。
- 目标合同：`docs/adr/0008-free-canvas-agent-graph-authority.md`
- 相关：`docs/specs/schema-v3-admission-slice.md` 的默认落图边由本文覆盖。

## 2. 用户对象

Shot 不是新表、不是第七种节点。用户单元是一个一层 Group，成员为：

- 1 个 `prompt_generation`
- N 个 `image_generation`（N = 该镜头张数，共用提示词）

共享的 `product_source`、`visual_system`、`creative_brief` 和身份 `image_asset` 留在组外，靠跨组边进入镜头。Group 仍无端口、无运行状态。「运行此场景」对组内第一张生图使用已有 `to_node`，其余使用 `node`。

`image_type_key` 只作分类标签和图库目录，不表达拓扑。

## 3. 生产家族

| 家族 | keys | 落图 |
|---|---|---|
| 摄影 | `hero` `scene` `detail` `sku` `packaging` | 一组：1 prompt + N 生图 |
| 信息图 | `selling_point` `dimensions` `specifications` `after_sales` `precautions` `faq` `shipping` `brand_story` | 同上；未指定文案策略时默认 `required` |
| 证据 | `certification` `factory` | 不建 prompt/生图。1 个未绑定 `image_asset`，`role=evidence` |

创建上传的参考图：`image_asset.config.role = product_identity`。接到视觉、创作、每个会生图镜头的 prompt 与 image。不接到证据占位节点。

## 4. 运行输入

Compiler 只读取目标节点的 incoming edges。断开参考或 facts 边后，该输入从 compiled runtime 消失。禁止全图扫素材，禁止 `edge_id=""` 的合成参考。

Draft 适配器只映射 Draft `edges[]`。会生图的 `image_generation` 缺少 reference 边时结构化失败，不再按笛卡尔积补边。

## 5. 画布

添加面板提供「添加场景」：一个 ChangeSet 创建组 + prompt + 1 张生图，接到已有商品资料 / 视觉 / 创作要求；仅当用户选中身份参考时连上那些素材。六类原语仍可单独添加。

## 6. 代码锚点

- 模版：`backend/src/productflow_backend/application/product_workflow/graph_template.py`
- Draft 适配器：`.../draft_graph_adapter.py`
- Compiler：`.../graph_compiler.py`
- 执行角色：`.../graph_execution.py`
- 家族：`.../agent/product_intake.py`、`web/src/lib/imageTypeFamilies.ts`
- 画布 ChangeSet：`web/src/pages/workbench/canvas/shotChangeSet.ts`
