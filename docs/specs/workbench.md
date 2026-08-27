# 工作台

- 文档状态：Draft
- 批准意图：工作台是成品生产面，不是从 V1/V2 修葺回来的工程清单。
- 图合同：[`docs/adr/0008-free-canvas-agent-graph-authority.md`](../adr/0008-free-canvas-agent-graph-authority.md)；当前形状见 [`docs/ARCHITECTURE.md`](../ARCHITECTURE.md) §6
- 人离开 Agent 仍能操作整张图：[`docs/adr/0009-agent-canvas-sandbox.md`](../adr/0009-agent-canvas-sandbox.md) §1
- 当前操作说明：[`docs/USER_GUIDE.md`](../USER_GUIDE.md)
- 未在浏览器证明之前：[`docs/ROADMAP.md`](../ROADMAP.md)

工作台让人不经过 Agent 也能建齐六类节点、连上 typed edge、绑定素材、跑出提示词和图片、撤销/重做、分组、把选区存成配方、在另一商品上预览后应用。Agent 加速同一条 Graph Command，不另开拓扑，也不挡住画布。

按钮在不算完成。完成是：一条连续动作能做完，结果立刻可再操作，失败写在操作对象上，持久化走 ChangeSet，桌面与 390px 都通。

代码已经按这份规格接线。1440 / 1024 / 390 与真实 provider 的连续动作证据还没拿到。未证明之前不得宣称工作台完成。证明之后，把仍有用的质量条写进 USER_GUIDE，删除本文。

## 1. 锁定方向

1. 图合同只许 v3。写入只走 `WorkflowChangeSet` + Node Catalog。
2. 壳只许复用 `workbench/chrome/`。不为手感新写画布壳或节点卡，不为复用 UI 带回 V1/V2 API。
3. 人是主控。对话、Draft、GraphProposal、Turn 状态都不是进画布、改配置或运行的闸门。关闭对话后面板就是画布；Agent 刚写入的节点人可以立刻改、立刻撤销。画布手感不得因为 Agent 工作而降级。
4. 绑定 ≠ 连边 ≠ 子图库。换绑不改边。未使用必须可见。
5. 不完整 DAG 合法。必要输入只决定能否运行。任何 edge 可删。
6. 分组是一层视觉组织。可进入、可分记视口；没有端口、运行、取消、重试、嵌套。
7. 配方只由用户从 live graph 保存。应用走同一套 Graph Command。
8. 检查器目的地是 Catalog `config_fields`。禁止把 `GraphNodeInspector` 加深成长期私有模型。
9. 常规 chrome 是结果语言。schema、revision、digest、asset id 不出现在第一屏。
10. Go 后端重写不插入本规格的实施。

## 2. 对象命令

命令靠近对象。主工具条只放全局高频动作。

| 对象 | 必须靠近对象出现 | 不合格 |
|---|---|---|
| 节点 | 运行（处理节点）、到此节点、复制、聚焦、删除；`image_asset` 另加绑定；可另加存为配方 | 只靠全局胶囊；到此节点藏在检查器深处 |
| 边 | 删除；能读到 role | 无工具条；失败才弹 toast |
| 选区 | 复制、分组、删除、存配方 | 多选后只能用快捷键 |
| 分组 | 重命名、解散、进入、返回 | 只能看虚线框 |

删除节点或选区必须确认。删除边可以不确认，但要能撤销。

## 3. 侧栏职责

唯一 inspector 所有者是 `ProductWorkbenchInspector`。

| 工具 | 必须完成 | 不合格 |
|---|---|---|
| 添加 | 建六类节点；选区可复制、分组、解散；忙时锁定 | 只罗列类型 |
| 详情 | 改当前节点；处理节点可运行；失败写在打开的检查器上并可重试；输入/消费者可跳转；空选是下一步 | 失败只在卡片或运行页；空选只剩版本号 |
| 运行 | 看这次跑的结果、取消、重试、跳到节点 | 第一屏铺 compiled_context、digest、内部 id |
| 图库 | 选一张商品图绑到 `image_asset`；未使用可见 | 架构说明当 hint |
| 配方 | 预览将出现的节点/边后应用；预览失败不能确认 | 成功条画出 Draft id |
| Agent | 对话、附图、回答、确认 Agent 发起的运行 | 挡住添加/详情/运行 |

## 4. 连续动作

1. 复制 → 粘贴 → 新节点被选中 → 立刻可拖、可连、可删。保留选区内部边。
2. 添加节点 → 落在当前视口中心附近 → 被选中 → 打开详情。
3. 改检查器 → 保存徽章 → 切工具或运行前 flush。脏提示词不得被运行吃掉。ChangeSet 进行中，添加、详情、绑定、配方锁定。
4. 运行 → 卡片进入 queued/running → 失败写在卡片和打开的详情上 → 可重试或不可重试清楚 → 检查器可取消 → 运行页可重试。
5. 拖线 → 合法绿 / 非法红 → 连不上写出原因 → 详情输入列表出现该边并可跳转。
6. 拖素材到空白 → 已绑定未使用的 `image_asset`。拖到聚合口 → 创建或确认复用后再连 `reference`。
7. 选 ≥2 节点 → 分组 → 重命名 → 进入 → 组内/全图视口分记 → 返回全图视口恢复。
8. 选区或分组或全图 → 保存配方 → 另一商品预览节点/边 → 确认后一次写入 live graph。
9. 最大化 → TopNav 收起 → 画布占满主区 → 还原。MiniMap 不遮检查器。
10. Ctrl/Cmd+Z 撤销最近 operation group；Shift 组合重做。Redo 不得映射成再调一次 undo。
11. 空选详情 → 添加节点 / 打开图库 / 运行整张图。
12. 绑定素材 → 详情显示已绑；未连边则显示未使用。换绑不改边。

## 5. 呈现

- 六类节点在缩略图尺寸下能被扫出来：类型色、图标、状态芯片分开。
- 主选与加选可区分。
- 失败、最近运行、glow 喂给现有 `WorkflowNodePresentationCard`，不另写一张卡。
- 390px：浏览 / 编辑 / 选择三模式有图标；检查器是底抽屉，画布仍露出节点；主操作不是 hover-only；无水平溢出。
- 视口按 workflow 持久化。

## 6. 不做

- 把 V2 多输入 handle 排请回来。
- 把顶部流程统计条请回来。
- 为「一直可运行」禁止删除必要边。
- 用坐标、最近节点或当前选区推断持久边。
- 新做一套卡片或检查器替换已经验证的 chrome。
- 用 GraphProposal 顶替用户直接编辑。
- 在工作台上为 V1 archive 开平行编辑器。
- 为本规格引入 Go 后端或双执行器。

## 7. 代码锚点

- 壳：`web/src/pages/workbench/agent/AgentWorkbenchShell.tsx`、`chrome/ProductWorkbenchInspector.tsx`
- 画布：`web/src/pages/workbench/canvas/GraphWorkflowCanvas.tsx`、`GraphCanvasPanel.tsx`
- 详情/运行/图库/配方：`GraphNodeInspector.tsx`、`GraphRunsPanel.tsx`、`GraphLibraryPanel.tsx`、`RecipeLibraryPanel.tsx`
- 目录：`backend/src/productflow_backend/domain/graph_catalog.py`
- 写入：`product_workflow/graph_commands.py`、`workflow_recipes/live_apply.py`
