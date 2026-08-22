# Schema-v3 工作台修葺北极星

## 1. 状态

- 文档状态：Draft
- 批准意图：2026-08-21 会话要求把修葺大局写成后续切片的默认阅读，避免把 v3 合同修回 V1/V2，也避免把「已经接线」当成产品完成。
- 目标合同：`docs/adr/0008-free-canvas-agent-graph-authority.md`
- 交互清单：`docs/rollout/free-canvas-v3-interaction-parity.md`（检查表，不是质量上限）
- 审计证据：2026-08-21 对照 `origin/main` V1、`e6cf9e66` V2、当前 `workbench/` V3
- 未完成项入口：`docs/ROADMAP.md` §2；本文是该节画布部分的实施北极星

在线图权威已经是 schema-v3。本文管的是：**人在工作台上把图画完、跑完、复用完时，完成度必须达到 V1 对象操作 + V2 工作台打磨，语义必须是 v3。**

## 2. 一句话目标

工作台是主生产面。用户不经过 Agent 也能建齐六类节点、连上 typed edge、绑定素材、跑出提示词和图片、撤销/重做、分组整理、把选区存成配方、在另一商品上预览后应用。Agent 只加速同一条 Graph Command，不另开拓扑，也不挡住画布。

「按钮在」不算完成。完成定义是：**一条连续动作能做完，结果立刻可再操作，失败写在操作对象上，持久化走 ChangeSet，桌面与 390px 都通。**

## 3. 已锁定的方向（后续切片不得改）

1. **图合同只许 v3。** 写入只走 `WorkflowChangeSet` + Node Catalog。禁止恢复 plan key、Prompt Artifact `images[]` 拓扑、不可删除血缘边、V2 mutation、`edit_version` 权威、浏览器 undo 权威。
2. **壳只许复用。** 呈现基线是 `workbench/chrome/` 与成熟 V1/V2 交互组件。禁止为修手感新写一套画布壳或新节点卡。禁止为复用 UI 把 V1/V2 API 带回来。
3. **人是主控。** 画布上每一种持久操作都必须能由用户单独完成。Agent 是协作者。对话、Draft、GraphProposal 都不能成为进画布、改配置或运行的闸门。
4. **绑定 ≠ 连边 ≠ 子图库。** 加入子图库、绑定 `image_asset`、`reference` edge 是三个动作。换绑不改边。未使用必须可见。
5. **不完整 DAG 合法。** 缺提示词的生图节点可以存在。必要输入只决定能否运行。任何 edge 可删；删后重算配置状态。
6. **分组是一层视觉组织。** 可进入局部视图、记住视口；没有端口、运行、取消、重试、嵌套。
7. **配方来自用户主动保存。** 保存必须从 live v3 graph 提取。禁止写出 V2 recipe payload。应用必须经同一 Graph Command；第一刀可以先落到待确认 Draft，但必须能预览将产生的图变更。
8. **检查器的目的地是 Catalog。** `GET /api/v3/node-catalog` 的 `config_fields` 是表单、校验、Agent tool schema 的同一份文档。当前 `GraphNodeInspector` 类型化编辑器是过渡适配，禁止再把它加深成长期模型。
9. **完成证据是浏览器。** 单元测试证明命令形状。切片完成还要桌面、窄桌面、390px、亮/暗色、reduced-motion、console/network 无 error、真实 ChangeSet。
10. **Go 后端重写不得插入本程序。** 见 `docs/README.md`：v3 治理基线完成前不开工。

## 4. 质量上限（比交互表更严）

交互表记录「有没有接到」。本节目录记录「接到什么程度才算合格」。后续 PR 用链路验收，不用功能点打勾。

### 4.1 对象命令

| 对象 | 必须靠近对象出现的命令 | 不合格形态 |
|---|---|---|
| 节点 | 运行（处理节点）、到此节点、复制、聚焦、删除；`image_asset` 另加绑定；配方保存可用后另加「存为配方」 | 只靠全局胶囊；到此节点藏在检查器深处；聚焦只在 Controls |
| 边 | 删除；能读到 `role` | 无工具条；失败才弹 toast |
| 选区 | 复制、分组、删除、存配方（交付后） | 多选后只能用快捷键，没有可见命令 |
| 分组 | 重命名、解散、进入、返回 | 只能看虚线框 |

删除节点/选区必须确认。删除边可以不确认，但要能撤销。

### 4.2 连续动作（必须整条存在）

1. **复制 → 粘贴 → 新节点被选中 → 立刻可拖/可连/可删。** 保留选区内部边。要有结果计数或等价可见反馈。
2. **添加节点 → 落在当前视口中心附近（吸附网格）→ 被选中 → 打开详情。** 禁止写死 `(120, 120)`。
3. **改检查器 → 保存徽章 → 切节点或运行前 flush。** 脏提示词不得被运行吃掉。画布 ChangeSet 进行中，添加面板和检查器锁定。
4. **运行 → 卡片进入 queued/running（含 glow 或等价运动，遵守 reduced-motion）→ 失败原因写在卡片上并可重试/不可重试 → 检查器取消 → 运行侧栏重试。** 失败不得只存在于侧栏。
5. **拖线 → 合法翠绿 / 非法红虚线 → 连上 → 检查器输入列表出现该边并可跳转。** 端口随缩放保持可点。
6. **拖素材到空白 → 已绑定未使用的 `image_asset`。拖到聚合口 → 创建或用户确认复用后再连 `reference`。** 禁止凭坐标/同资产自动合并。
7. **选 ≥2 节点 → 分组 → 重命名 → 进入 → 组内平移/布局/视口独立记忆 → 返回全图视口恢复。**
8. **选区或分组或全图 → 保存配方 → 另一商品应用 → 预览将创建的节点/边 → 确认后一次写入。** 片段合并走 Graph Command，禁止静默写 V2 base。
9. **最大化 → TopNav 收起 → 画布占满主区 → 还原。** MiniMap 不遮检查器。
10. **Ctrl/Cmd+Z 撤销最近 operation group；Redo 重做被撤销且其后没有新编辑的组。** Redo 不得映射成再调一次 undo。

### 4.3 呈现身份

- 六类节点必须能在缩略图尺寸下被扫出来：类型色、图标、状态芯片分开。禁止六类共用一块石板灰。
- 选中语言回到高对比环（V1/V2 的靛蓝/主色环），主选与加选可区分。
- 悬浮有位移或等价反馈；拖动中关闭。
- `WorkflowNodePresentationCard` 已有的失败/最近运行/glow 槽位必须喂数据。禁止另写一张卡。
- 未使用素材占用状态位时，仍要能看到 idle 以外的运行状态。

### 4.4 视口与移动端

- 视口按 `workflowId` 持久化；宽布局切换超过既有 width-ratio 守卫时丢弃。
- 390px：浏览 / 编辑 / 选择三模式有图标；检查器用底抽屉或等价不挡节点的容器；主操作不是 hover-only；无水平溢出。
- 桌面和 390px 覆盖同一组核心任务，操作方式可以不同。

### 4.5 接口

画布结构与运行的写路径只许 `/api/v3`。允许暂时留在 v2 的：商品图库、交付 rendition、Agent 对话/工作台引导、全局素材库。不允许长期留在 v2 的：配方保存、配方应用到 live graph、图撤销/重做。

必须补齐：`POST .../redo`（对最近一次 **undo** operation group 提交 inverse ChangeSet，`history_kind=redo`）、从 live graph 提取配方的 v3 写路径。`GET /api/v3/node-catalog` 必须继续是连线预校验的唯一矩阵；前端不得再复制一份兼容表。

## 5. 明确不做（看起来像修葺、实际是走偏）

- 把 V2 多输入 handle 排请回来。聚合单端口是 v3 信息架构。
- 把顶部流程统计条请回来。运行状态进节点、运行侧栏、紧凑全局入口。
- 为「一直可运行」禁止删除必要边。
- 用坐标、最近节点、当前选区推断持久边。
- 新做一套卡片/工具条「更符合 v3 扁平审美」，替换已经验证的 chrome。
- 把类型化 Inspector 再堆三个月字段，推迟 Catalog。
- 用 GraphProposal 顶替用户直接编辑。
- 配方保存继续 410，只在 UI 藏按钮。
- 在 v3 画布上为 V1 archive 重建开平行编辑器。
- 为本程序引入 Go 后端或双执行器。

## 6. 切片顺序（可调工期，不可调依赖）

后一切片可以提前做只读勘察，但不得在前一刀的停止条件未满足时把后一刀的半成品合进主路径。

| 顺序 | 切片 | 用户可见结果 | 停止条件 |
|---|---|---|---|
| A | 对象命令与呈现身份 | 卡能扫、工具条够用、粘贴选中克隆、删除有确认、建点跟视口、最大化可用、busy 锁定侧栏、运行前 flush | 桌面+390px；卡片失败/类型色有截图或 DOM 证据；无新 v2 写入 |
| B | 历史权威 | Redo 快捷键与按钮；新编辑清空 redo | 后端测试覆盖 undo→redo→再编辑；禁止 redo=undo；浏览器证据归切片 G |
| C | Catalog 检查器 | 详情表单按 `config_fields` 渲染；保存仍 `update_node_config` | 前端不再为新字段加一份私有 schema；Agent tool 读同一目录 |
| D | 进入分组 | 双击进入、面包屑返回、组内/全图视口分记 | 分组仍非 DAG 节点；跨组边在全图可见 |
| E | 配方 ChangeSet | 从 live graph 保存全图/分组/选区；应用有预览 | 410 消失；无 V2 recipe payload；片段对 v3 要么合并要么明确冲突 |
| F | GraphProposal | Agent 改图在画布上 ghost 预览，确认一次应用 | 提案层不能运行；取消不留节点/边 |
| G | 工作台 gate | 真实 provider + PostgreSQL/Redis/worker + 1440/1024/390 + 无 console/network error | 交互表除切片外项全部重跑；更新 USER_GUIDE / HelpPage |

切片 A 不改 Inspector 字段模型。切片 C 之前，Inspector 只修 flush、busy、输入/消费者跳转，不加新的长期表单结构。

## 7. 每个画布 PR 的完成定义

1. 指出本 PR 服务切片 A–G 中的哪一条，以及覆盖了 §4 哪几条链路。
2. 持久操作产生一个 ChangeSet / operation group，或明确属于纯客户端（选择、视口、拖动预览、剪贴板）。
3. 不新增 V1/V2 图写入、plan key、血缘禁删、浏览器 undo 权威。
4. 不新增平行组件；缺口优先喂给现有 chrome 槽位。
5. 有 focused 测试：命令形状、选区、快捷键或 Catalog 投影。
6. 触及可见画布时，写明桌面与 390px 的验证结果或明确「未做浏览器、不得宣称完成」。

## 8. 文档同步

- 行为落地后更新 `docs/USER_GUIDE.md` 与 `web/src/pages/HelpPage.tsx` 同一提交。
- `PRD.md` 已写「完整工作流、文件夹或多选节点保存为配方」。保存已从 live v3 graph 提取；应用到 live graph 的 ChangeSet 仍未交付，ARCHITECTURE 只写当前 Draft+预览事实。
- 交互表状态列落后于代码时改交互表，不要为了表格绿灯降低 §4。

## 9. 代码锚点

- 呈现：`web/src/pages/workbench/chrome/WorkflowNodeCard.tsx`、`WorkflowCanvasChrome.tsx`
- 画布命令：`web/src/pages/workbench/canvas/GraphCanvasPanel.tsx`、`GraphWorkflowCanvas.tsx`、`graphLayout.ts`
- 检查器/运行：`GraphNodeInspector.tsx`、`GraphRunsPanel.tsx`
- 图命令：`backend/.../product_workflow/graph_commands.py`、`graph_contracts.py`
- 目录：`backend/.../domain/graph_catalog.py`
- 配方：`backend/.../workflow_recipes/service.py`、`extract.py`
