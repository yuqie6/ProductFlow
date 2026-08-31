# ADR 0014：画布文稿、产物与 cook 范围

画布节点按职责分为 `source`、`document`、`effect`。文稿节点把已发布内容放在 `config_json`；AI 生成结果先写入 `workflow_graph_artifacts`，并通过 `pending_candidate_artifact_id` 进入审阅。效果节点的最终输出才使用 `current_artifact_id`。生图读取上游 live `config.prompt`，不要求 prompt artifact。

文稿来源列为 `seed`、`generated`、`authored`、`collaborative`。AI 可自动采用未填写的 seed 文稿。已有正式内容的节点通过 `complete`、`rewrite`、`replace` 生成候选，用户在 Inspector 按业务 section 应用或放弃。

编排、端口与运行队列的后续决策见 [0015](0015-canvas-ports-run-queue.md)。

## 状态

Accepted；2026-08-31 修订为候选审阅合同。端口、镜头运行、一图一 run 与 `document_origin` 存储位置由 0015 修订。

## 背景

出生图几乎总是非空：brief 预填 look.rule，prompt 预填图种 `design_goal`。把「config 非空」当 freeze 会跳过首跑；把种子当已写又会让 LLM 整对象替换手填字段。生图 compile 还要求上游 artifact，手填构图无法直接出图。`to_node` 会把祖先全部入队且不 skip，整图会重写已写文稿。

## 决策

- `create_node`（含模板）为 `seed`。用户第一次填写可见字段后标为 `authored`；用户编辑 AI 文稿后标为 `collaborative`。来源只存在 `workflow_graph_nodes.document_origin`。
- seed 节点的普通运行可通过 ChangeSet 自动采用，origin=`generated`。运行中发生人工编辑时，生成结果进入候选，不覆盖 live config。
- 已有正式内容的文稿动作是 `complete`、`rewrite`、`replace`。三者只允许 `scope=node`、`force=true`。`complete` 填补空缺；`rewrite` 以当前文稿为上下文生成完整替代稿；`replace` 根据已确认资料重新生成完整文稿。三者都先生成候选。
- 候选 artifact 保存 `document_action`、`base_document_hash`、`input_digest`。应用前重新计算文稿哈希和输入摘要，任一变化都会把候选标为 `outdated` 并禁止应用。应用支持完整文稿或 Catalog 登记的业务 section。放弃只清候选指针。
- `scope=node` 只入队目标。`to_node` 入队目标以及仍需生成的祖先。`graph` 为具备必需输入的处理节点建立 node run，由执行器把无需重算的节点落成 `skipped`。`selection` 入队请求的处理节点集合（见 0015）。
- 生图 digest 哈希规范化后的 prompt 文稿、上游 visual overlay 内容、本节点 spec/variation/overlay。`delivery_spec` 仍忽略。digest 键由 Catalog `affects_digest` 声明。
- 生成 prompt 的出站 JSON 带完整 `briefs[]`；brief 生成收集图上 `image_type_key`；prompt 的 `text_policy` 取下游同镜头 image spec（第一条非 none）。brief → prompt 最多一条边。
- 镜头「运行此场景」对组内生图节点提交一次 `scope=selection`（修订原「每张 image 一次 `scope=node`」）。
- 参考边不是生图必填。缺边不写入卡片 `failureReason`。

## 后果

- 手填 prompt 可直接供下游生图，不需要先生成 artifact。
- AI 重写已有文稿不会直接覆盖人工内容。Inspector 展示当前值与候选值，并按目标、文案、构图、风格、限制等 section 应用。
- 节点卡片主状态表达资料、文稿或效果是否可用；排队、运行、失败等执行态只在执行期间覆盖主状态。运行历史保留上次执行结果。
- 改 brief 只让 prompt stale；改 visual overlay 让生图 digest 变脏并重绘，不重写 prompt。
- 同一次整图跑内，内容节点采用后 hydrate 文稿给下游 compile。
- 整图提交会保留具备必需输入的节点运行记录；没有任何具备必需输入的处理节点，提交返回「没有可运行的处理节点」。

## 排除方案

- 控制流边、把 `text_policy` 挪出 `generation_spec`、第二套执行器。原「多输入 handle」与「并发多 `WorkflowGraphRun`」排除由 0015 解除：现为按角色分端口，以及一活跃 run 加 FIFO 排队。

## 证据

- `go/internal/graph/document.go`、`document_candidate.go`、`select.go`、`compiler.go`、`prompt_assemble.go`、`execute_node.go`
- `web/src/pages/workbench/canvas/GraphNodeInspector.tsx`、`graphOperationalState.ts`、`catalogConfig.ts`
- `just go-test`、`pnpm --dir web test:run`
