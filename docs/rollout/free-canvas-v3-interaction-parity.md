# 自由画布 v3 交互继承基线

本文记录自由画布 v3 重建期间必须保留的成熟交互。它是实现与浏览器验收清单，不改变
[`docs/adr/0008-free-canvas-agent-graph-authority.md`](../adr/0008-free-canvas-agent-graph-authority.md)
定义的图权威、边权威和 Agent 提案边界。质量上限、非目标和切片顺序见
[`docs/specs/v3-canvas-restoration.md`](../specs/v3-canvas-restoration.md)。
「已接线」不等于该规格 §4 的链路完成。

对照基线：

- `origin/main` 的 `web/src/pages/product-detail/WorkflowCanvas.tsx`、`ProductDetailPage.tsx`、
  `InspectorPanel.tsx`、`RunsPanel.tsx`、`ImagesPanel.tsx` 和 `TemplateGroupsPanel.tsx`。
- 当前工作树的 `web/src/pages/workbench/chrome/WorkflowCanvasChrome.tsx`、`shortcuts.ts`、
  `workflowCanvasInteraction.ts` 和 `web/src/pages/workbench/canvas/` 中已验证的交互组件。
- 目标 v3 的结构写入统一通过 `POST /api/v3/products/{product_id}/workflows/{workflow_id}/changesets`。
  画布本地状态只负责未提交的选择、视口、拖动预览和剪贴板。

质量上限与切片顺序见 [`docs/specs/v3-canvas-restoration.md`](../specs/v3-canvas-restoration.md)。本文是实现与浏览器验收清单；「已接线」不等于该规格 §4 的链路完成。

## 当前基线（2026-08-21）

- 工作台画布、构思直接创建和 Draft 确认物化已读/写 schema-v3 graph projection、ChangeSet 与 run。`workbench/chrome/` 的节点卡、端口、工具条、框选/多选、MiniMap、自动布局和快捷键已接到 v3 画布。
- Agent 工作台在已有 v3 graph 时保留对话侧栏，不再整页替换成无对话的 stub。无图时显示 onboarding，不再打开 V2 canvas。
- 工作流子图库挂在 `workflow_graphs` 上。绑定走 `update_node_config`，不自动连 `reference` 边。配方保存从 live v3 graph 提取；应用前预览新建或合并及将出现的节点/边，确认后一次写入 live graph。片段配方对 schema-v3 明确冲突。
- 服务端撤销（operation group inverse）与 Redo（`POST .../redo`，`history_kind`）已接线。详情按 Node Catalog `config_fields` 渲染。进入分组已接线（双击/按钮、面包屑、视口分记）。配方保存与 live apply 已接线。GraphProposal 幽灵预览、确认和取消已接线。交互表里部分状态列落后于代码。
- 无 live graph 时可 `POST /api/v3/products/{product_id}/workflows` 写入空图，随后用 Graph Command 添加六类节点；重复创建返回冲突。
- 浏览器 390px 与真实 provider 的完整交互表验收仍待补。切片 A 已把类型色、卡片失败、粘贴后选中、删除确认、视口建点、最大化、busy/flush 接到现有 chrome；切片 B 已接 Redo；窄屏检查器已接到底抽屉。浏览器证据仍待补，不得宣称切片 G 完成。

## 保留原则

1. 重建替换图合同和运行时，不降低用户已经掌握的直接操作能力。
2. 节点、边、分组和配置的持久变更必须形成真实 ChangeSet 与 operation group。
3. 选择、框选、视口和临时预览属于客户端交互状态，不伪装成工作流关系。
4. 主工具条只承载全局高频命令。节点命令、边命令和选区命令靠近操作对象出现。
5. 桌面端与移动端可以采用不同操作方式，但必须覆盖同一组核心任务。

## 交互继承矩阵

| 能力 | 成熟实现证据 | v3 归属 | 验收行为 | 状态 |
| --- | --- | --- | --- | --- |
| 单选、追加多选、框选 | 远端 `WorkflowCanvas.tsx`；本地 `workflowCanvasInteraction.ts` | React Flow 选择状态 | Shift/Ctrl/Command 可追加选择；空白拖动可框选 | 已接线；待浏览器验收 |
| 节点拖动与成组拖动 | 远端 `WorkflowCanvas.tsx` | `move_nodes` 单个 ChangeSet | 多选节点拖动后只产生一个 operation group | 已接线；待浏览器验收 |
| 连接目标反馈 | 本地 `WorkflowCanvasChrome.tsx` | Catalog 端口合同与 ChangeSet 校验 | 拖线时可连接目标为绿色，不合法目标为红色；失败原因可见 | 已接线；待浏览器验收 |
| 聚合输入端口 | ADR 0008 节点目录 | 单个 `input` handle，多条 typed edge | 多个参考素材连接同一输入，不生成无法解释的端口排 | 已接线；待浏览器验收 |
| 节点上下文工具条 | 远端 `WorkflowCanvas.tsx`；本地 `WorkflowCanvasChrome.tsx` | 节点/选区命令 | 运行、复制、聚焦、保存为配方、删除靠近选区出现 | 已接线；待浏览器验收 |
| 边上下文工具条 | 远端 `WorkflowCanvas.tsx` | `disconnect_edge` | 选中边可直接删除，并能看到边的语义名称 | 已接线；待浏览器验收 |
| 复制、粘贴、成组复制 | 远端 `ProductDetailPage.tsx`；本地 `shortcuts.ts` | 一个 create/connect ChangeSet | 保留选区内部边，生成新节点 ID，粘贴后选中新节点 | 已接线；待浏览器验收 |
| 删除选区 | 远端与本地快捷键实现 | 一个 delete/disconnect ChangeSet | 多节点及关联边原子删除；输入编辑时快捷键不误触 | 已接线；待浏览器验收 |
| 撤销、重做 | 远端 `workflowHistory.ts`；本地 operation group inverse | operation group 与逆 ChangeSet | Ctrl/Command+Z 提交 inverse ChangeSet；Ctrl/Command+Shift+Z 提交 redo ChangeSet | 已接线；待浏览器验收 |
| 自动布局 | 远端画布；本地 v2 `graph.ts` | `move_nodes` ChangeSet | 按 DAG 层级布局，结果可撤销，不改节点关系 | 已接线（撤销除外）；待浏览器验收 |
| 吸附、缩放、适应画布、聚焦选区 | 本地 `WorkflowCanvasChrome.tsx` | 纯客户端视口状态 | 吸附可切换；缩放记忆；可聚焦选区或全图 | 已接线；待浏览器验收 |
| MiniMap | 远端成熟画布 | 纯客户端视口状态 | 大图可定位和拖动，不遮挡检查器 | 已接线；待浏览器验收 |
| 添加全部节点类型 | ADR 0008 Node Catalog | `create_node` | 添加面板展示六类节点及用途，创建位置接近当前视口 | 已接线；待浏览器验收 |
| 节点配置与素材绑定 | 本地 Inspector；ADR 0008 Node Catalog | `update_node_config` | 未保存、保存中、失败、已保存状态明确；素材绑定不自动连边 | 已接线（绑定不连边）；详情按 `config_fields` 渲染；待浏览器验收 |
| 输入与消费者追踪 | ADR 0008 图查询合同 | 图查询结果 | 可查看每条输入的来源、role 和消费者；可跳转到关联节点 | 已接线；待浏览器验收 |
| 图片预览与下载 | 远端 `ImagesPanel.tsx`；本地图片浏览组件 | Artifact 与 ProductImageAsset | 节点输出可预览、下载并定位到生成节点 | 已接线 preview_asset_id；待浏览器验收 |
| 节点运行、目标运行、全图运行 | 远端运行交互；ADR 0008 run contract | v3 run API | 运行范围可见；执行只读取上游边；运行状态投影到节点 | 已接线节点/全图；待浏览器验收 |
| 运行历史、取消、重试 | 远端 `RunsPanel.tsx`；本地 v2 run panels | WorkflowRun/WorkflowNodeRun | 可查看 revision snapshot、context trace、失败原因、取消和 retry 来源 | 已接线取消/重试；context trace 待补 |
| 分组与进入分组 | 当前本地 v2 folder 交互 | v3 group ChangeSet | 选区可分组、移动、重命名、解散；分组不成为可执行节点 | 已接线分组/移动/解散/重命名/进入/视口分记；待浏览器验收 |
| 保存配方与应用配方 | 远端模板面板；本地 Recipe 面板 | Recipe ChangeSet | 全图、分组或选区可保存；应用前预览将创建的图变更 | 保存与 live apply 已接线；待浏览器验收 |
| Agent 图提案预览 | ADR 0008 提案合同 | GraphProposal -> ChangeSet | 提案以 ghost 节点/边预览；确认后一次应用并聚焦变更 | 已接线；待浏览器验收 |
| 移动端浏览、编辑、选择模式 | 远端 `ProductDetailPage.tsx`；本地 Chrome | 客户端交互模式 | 手势不会同时平移和移动节点；检查器用底部抽屉 | 已接线模式切换与底抽屉（画布仍露出一截）；待浏览器验收 |

## 明确不继承

- v2 的节点类型、wire schema、运行时读路径和 API。可复用的是交互行为与无业务语义的 UI 组件。
- 通过最近节点、坐标、当前选择或向导字段推断边。
- 将参考素材的“已绑定”展示成“已被下游使用”。
- 永久占据画布顶部的流程状态条。运行状态进入节点、运行侧栏和紧凑全局状态入口。
- 把所有命令继续堆入横跨画布的长工具条。低频命令进入对象工具条或侧栏。

## 实施顺序

1. 从远端 v1 和本地 v2 提取纯呈现组件、画布 shell、交互 reducer 与响应式规则，建立 v3 visual fixture；不引入旧 DTO、query 或 mutation。
2. 将 v3 graph/revision 投影接入成熟 shell，恢复选择、成组移动、对象工具条、快捷键、复制粘贴、删除、布局和稳定 Inspector。
3. 接入全局素材库与工作流子图库选择器，完成创建图片节点、绑定、换绑、未使用状态、多消费者和聚合多参考输入。
4. 接通节点、目标、全图运行、节点状态、运行历史、取消、retry、context trace 与 Artifact 图片预览。
5. 接通 v3 分组、Recipe ChangeSet 和 Agent ghost graph 提案，随后删除在线 v2/Draft topology 路径。
6. 用桌面、窄桌面和 390px 移动浏览器逐项执行本表，同时检查真实 ChangeSet、edge、revision snapshot、context trace、console 和 network error。

## 每个前端切片的停止条件

- 页面产生 React error、console error、无限 query/refetch 或 React Flow update loop。
- 关系只出现在表单、选区或本地状态，没有持久化为 edge。
- 拖入素材后无法在画布区分“已绑定未使用”和“已连接使用”。
- 为复用旧 UI 引入 v1/v2 API、Draft materialization、plan key 或 retired runtime fallback。
- 桌面和 390px 任一视口出现水平溢出、节点不可达、工具条遮挡或主要操作只能依靠 hover。
