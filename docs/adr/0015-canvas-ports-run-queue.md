# ADR 0015：画布端口、选择运行与运行队列

schema-v3 画布按角色分输入端口，运行 scope 增加 `selection`，活跃 run 之外的提交进入 FIFO 队列，跳过写成 `skipped`。不做控制流边、循环/条件节点或第二套执行器。

## 状态

Accepted

## 背景

ADR 0014 把单输入 handle、镜头逐张 `scope=node` 和一图一活跃 run 写成条款。这些选择让用户无法从画布结构读出缺什么输入，镜头进度不可整体观测，有活跃 run 时新提交只能 409。digest 排除清单和 `document_origin` 启发式也在运行时漂移。本 ADR 记录 0014 中被修订的编排/交互边界；文稿 vs 产物、`delivery_spec` 不进 digest、fill 不覆盖 authored/generated 仍以 0014 为准。

## 决策

- 处理节点左侧按 Catalog `accepts` 渲染具名输入端口，handle id 等于持久化边 `role`。连接时只校验类型、基数和防环；就绪校验留在运行时。brief → prompt 扇入上限为 1。边可重连，同角色扇入用 `reorder_edges` 排序。
- `document_origin` 是内容节点列（`seed|generated|authored`），不再用文稿启发式。Catalog 字段声明 `affects_digest` 与 `required`。`create_node` 默认 config 由后端 Catalog 填充。
- 节点 run 状态为 `queued|running|succeeded|failed|unknown|skipped|cancelled`。digest 未变或 frozen 写 `skipped`。取消写 `cancelled`。`idle` 不再写入。
- 运行 scope：`node`、`to_node`、`selection`（显式节点集合）、`graph`。`force` 仅在 `node|to_node|selection` 且有目标时合法。镜头「运行此场景」与「重试失败节点」各提交一次 `selection`。
- 已有 `running` run 时新请求写入图级 `queued` run，FIFO 出队时再快照。同类请求（同 scope、同目标、同 force/mode）合并。`POST .../runs/preview` 返回 `planned_action`。`GET .../runs/:id/events` 推送节点状态；浏览器保留轮询兜底。内容 adopt 用系统 Actor；live 可见文稿相对运行快照有差异或 revision 冲突时不覆盖 config，生成 artifact 仍保留并成为 `current_artifact_id`，节点投影为 `stale`。
- `GET .../runs/:id/events` 读取与状态变更同事务写入的 `workflow_graph_run_events`。事件按 run 使用单调 `sequence`，SSE `id`、`after` 查询参数和 `Last-Event-ID` 都是该游标；断线重连会补发游标之后的事件，直到收到终态事件。`GET .../runs/:id` 仍是完整投影的对账入口，SSE 不可用时浏览器回退到轮询。
- Agent `request_workflow_run_v1` 可带 `scope=node|to_node|selection`，仍须用户确认。
- 不做：控制流边、循环或条件节点、第二套执行器、连接时校验就绪。

## 后果

- 缺必连输入（目前仅 prompt → image）在端口和卡片上可见，Play 禁用。
- 全图会为具备必需输入的处理节点建立 node run；无需重算的节点显示为已复用/已冻结的 `skipped`，不再伪装成功。缺少必连输入的节点不会入队，整图提交仍返回校验错误。
- 排队期间用户可继续改图；出队跑的是当时 live 图。

## 证据

- `go/internal/graph/catalog.go`、`select.go`、`runs.go`、`document.go`、`run_sse.go`
- `web/src/pages/workbench/canvas/GraphWorkflowCanvas.tsx`、`shotChangeSet.ts`、`GraphCanvasPanel.tsx`
- `just go-test`、`pnpm --dir web test:run`
