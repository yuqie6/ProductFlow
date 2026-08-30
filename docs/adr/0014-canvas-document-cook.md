# ADR 0014：画布文稿、产物与 cook 范围

处理节点把**已发布文稿/参数**放在 `config_json`，把一次 cook 的**不可变产物**放在 `workflow_graph_artifacts`。生图读上游 live `config.prompt`，不再用 prompt artifact 当门槛。内容节点用节点列 `document_origin`（`seed` | `generated` | `authored`）区分模板种子与已写文稿；fill cook 只补 seed，不覆盖 authored/generated。

编排、端口与运行队列的后续决策见 [0015](0015-canvas-ports-run-queue.md)。

## 状态

Accepted；端口、镜头运行、一图一 run 与 `document_origin` 存储位置由 0015 修订。

## 背景

出生图几乎总是非空：brief 预填 look.rule，prompt 预填图种 `design_goal`。把「config 非空」当 freeze 会跳过首跑；把种子当已写又会让 LLM 整对象替换手填字段。生图 compile 还要求上游 artifact，手填构图无法直接出图。`to_node` 会把祖先全部入队且不 skip，整图会重写已写文稿。

## 决策

- `create_node`（含模板）为 `seed`。检查器可见字段保存不回传 origin 键，Apply 把列标 `authored`。生成采用走 ChangeSet `update_node_config`，origin=`generated`。origin 存在 `workflow_graph_nodes.document_origin`，不进 `config_json`。跑图中途改了该节点可见文稿时，adopt 不覆盖 live config；生成 artifact 仍写入 lineage，`current_artifact_id` 指向该 artifact，节点投影为 `stale`，node run output 标记 `adopted=false, stale=true`。
- `scope=node` 只入队目标。`to_node` 入队目标以及 fill 仍会干活的祖先。`graph` 为具备必需输入的处理节点建立 node run，由执行器把无需重算的节点落成 `skipped`。`selection` 入队请求的处理节点集合（见 0015）。`skipUnchanged` 适用于任意非 force scope。`force` + `regenerate_mode=refine|replace` 只作用于请求的目标内容节点，且不可与 `scope=graph` 组合。
- 生图 digest 哈希规范化后的 prompt 文稿、上游 visual overlay 内容、本节点 spec/variation/overlay。`delivery_spec` 仍忽略。digest 键由 Catalog `affects_digest` 声明。
- 生成 prompt 的出站 JSON 带完整 `briefs[]`；brief 生成收集图上 `image_type_key`；prompt 的 `text_policy` 取下游同镜头 image spec（第一条非 none）。brief → prompt 最多一条边。
- 镜头「运行此场景」对组内生图节点提交一次 `scope=selection`（修订原「每张 image 一次 `scope=node`」）。
- 参考边不是生图必填。缺边不写入卡片 `failureReason`。

## 后果

- 手填 prompt 后「运行该节点」生图不再报「尚未生成」，也不改 prompt 字段。
- 改 brief 只让 prompt stale；改 visual overlay 让生图 digest 变脏并重绘，不重写 prompt。
- 同一次整图跑内，内容节点采用后 hydrate 文稿给下游 compile。
- 整图提交会保留具备必需输入的节点运行记录；没有任何具备必需输入的处理节点，提交返回「没有可运行的处理节点」。

## 排除方案

- 控制流边、把 `text_policy` 挪出 `generation_spec`、第二套执行器。原「多输入 handle」与「并发多 `WorkflowGraphRun`」排除由 0015 解除：现为按角色分端口，以及一活跃 run 加 FIFO 排队。

## 证据

- `go/internal/graph/document.go`、`select.go`、`compiler.go`、`prompt_assemble.go`、`execute_node.go`、`listing_prompt.go`
- `web/src/pages/workbench/canvas/shotChangeSet.ts`、`GraphNodeInspector.tsx`、`catalogConfig.ts`
- `just go-test`、`pnpm --dir web test:run`
