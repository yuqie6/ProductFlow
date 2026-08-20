# Schema-v3 准入切片

## 1. 状态

- 文档状态：Draft
- 批准依据：待批准。目标合同已接受：`docs/adr/0008-free-canvas-agent-graph-authority.md`
- 交互继承：`docs/rollout/free-canvas-v3-interaction-parity.md`
- 当前运行事实：`CONTEXT.md`、`docs/ARCHITECTURE.md`、代码。本文不是当前实现。
- 阅读入口：只从 `docs/ROADMAP.md` §2 进入，不进入默认阅读。
- 代码基线：`codex/development` 的 schema-v2 工作台与 `application/` owner 收口之后。迁移头：`20260820_0070`

本文只覆盖 v3 重新进入在线代码的第一刀。批准前不写 v3 graph、run、API、前端数据源或新迁移。

人是工作台的主控。画布上每一种持久操作都必须能由用户单独完成，并且自洽、可撤销、体验完整。Agent 是加速手段，不是进画布或写出提示词/风格的闸门。

## 2. 切片范围

创建商品保留现在 `/products/new` 的构思表单（名称、图片类型与数量、1–6 张参考图）。确认「要哪些图」之后分两条入口，落到 **同一份** v3 graph：

```text
构思表单（保留）
  ├─ 直接创建
  │     -> 按类型/数量/参考图套预设模版
  │     -> 一个初始 Graph ChangeSet
  │     -> schema-v3 ProductWorkflow（提示词和风格可以不完整）
  │     -> 工作台；跑 prompt_generation / 相关节点才生成提示词和风格
  │
  └─ Agent 创建（现有对话路径，可选）
        -> WorkflowDraft revision
        -> 用户确认
        -> 同一套 Graph Command（Draft→初始图 adapter）
        -> 同一份 schema-v3 ProductWorkflow
```

两条入口之后：

```text
工作台 canvas 读同一 graph projection
  -> 用户直接编辑、连线、绑定素材、运行
  -> 或 Agent 辅助（不取代画布）
  -> v3 WorkflowRun（revision snapshot）
  -> Artifact / ProductImageAsset / MediaLibrary
```

本切片交付后，同一环境只有 schema-v3 作为在线图权威。在线 V2 的删除放在本切片完整 gate 之后，见 §10。

本切片不包含：

- `WorkflowIntent` 取代 WorkflowDraft 拓扑（ADR 0008 §11，后续收敛）
- GraphProposal 幽灵层与 Agent 对 **live graph** 的 `propose_workflow_changes`
- Recipe payload 直接解析为 ChangeSet（配方仍走 Draft → 本 adapter，见 §4.8）
- v1 archive → WorkflowIntent 重建（gate 后独立切片）
- 继续收 `application/` 根上的 durable/settings 平铺文件
- 恢复已删除的 `0071-0074`

## 3. 当前 V2 物化（对照基线）

确认与物化今天是两条命令：

- `workflow_drafts/confirmation.py`：确认 Draft revision，并收口商品 Agent conversation
- `workflow_drafts/materialization.py`：按幂等键写入 `schema_version=2` 的 `ProductWorkflow`、Prompt Artifact、Visual Exception、folder、node、edge、reveal events

Draft 拓扑字段在 `workflow_drafts/contracts.py` 的 `WorkflowDraftPayloadV1`。物化把 plan key 写进节点 `config_json`，并把 Draft edge 转成 `canonical_workflow_edge_handles` 的 V2 handle。

Agent 路径里，adapter 替换的是 **物化写出的图合同**，不是构思表单，也不是 Draft 作为「对话产物」的地位。直接创建路径 **不经过 Draft**，模版 ChangeSet 就是出生证明。

## 3.1 直接创建：预设模版

构思表单提交后立刻（一次事务）写入 Product、上传的 `ProductImageAsset`、active v3 graph。不创建 AgentConversation，不写 WorkflowDraft。

模版是写好的可见 ChangeSet，不是运行时按坐标猜边。第一刀默认结构：

| 构思 | 模版画出的节点 |
|---|---|
| 商品 | 一个 `product_source`（事实可空，节点合法） |
| 每张上传参考图 | 一个 `image_asset`，已绑定该 `ProductImageAsset` |
| 每个选中的图片类型 | 一个 `prompt_generation`（current Artifact 为空） |
| 该类型的每一张计划图 | 一个 `image_generation`（有 GenerationSpec 默认值；current Artifact 为空） |
| 工作流级 | 一个空的或预设的 `visual_system`、一个空的 `creative_brief`（均可 incomplete） |

默认边（必须出现在画布上，用户可删）：

- `product_source` → 每个 `prompt_generation`（`facts`）
- 每个 `prompt_generation` → 该类型下的每个 `image_generation`（`prompt`）
- `creative_brief` → 每个 `prompt_generation`（`brief`）
- `visual_system` → 每个 `prompt_generation` 和 `image_generation`（`visual_guidance`）
- 上传得到的 `image_asset`：**默认不连出边**，画布标未使用。参考图是用户声明的素材，连到哪些节点由用户在画布上拉，或由 Agent 之后提案。模版不把每张参考图暗接到每个提示词。

进入工作台时图就是正式图。提示词正文、视觉规范内容和商品事实都可以缺。用户在 Inspector 填，或 **运行** `prompt_generation`（以及后续若有的风格生成）来写出 Artifact，再运行 `image_generation`。不要求先跟 Agent 聊完才能跑。

预设 GenerationSpec（画幅等）可以来自类型默认值；这是节点配置，不是拓扑。

## 4. Draft → v3 初始图映射（仅 Agent 入口）

adapter 输入：已确认的 `WorkflowDraftRevision`（`WorkflowDraftPayloadV1`）+ 幂等键 + request hash。  
adapter 输出：一个 `WorkflowChangeSet`，`actor_type=user`（用户确认触发），`summary` 标明 Draft revision，一次事务写入 canonical graph，`graph revision = 1`。不新增 ADR 未列的 actor。同一 ChangeSet 内新建节点用 `client_ref` 供后续 edge 引用。

### 4.1 丢掉的拓扑语义

这些字段不再出现在节点 config、edge 或 Artifact 里当拓扑用：

- `prompt_plan_key`
- `image_type_key`（可作为 `image_generation` 的展示/用途标签，不得决定连线）
- `image_plan_key`
- Prompt Artifact `images[]` 对下游图片节点的所属关系
- V2 handle 名 `facts` / `asset` / `prompt` 作为客户端可提交字段

### 4.2 节点

| Draft 来源 | v3 节点 | 节点持有 | 不持有 |
|---|---|---|---|
| 一个 `product_context` 节点 + `facts` / fact set version | 一个 `product_source` | 商品或 `ProductFactSetVersion` 引用 | 下游名单 |
| `visual_system`（confirmed version 或本 revision 固化后的 version） | 一个 `visual_system` | Visual System version id | 谁在用它 |
| 每个 `prompt_plans[]` 的目标/限制/文案边界中适合「创作要求」的部分 | 零或一个 `creative_brief`（见下） | 目标、必需文案、禁止项 | 提示词正文 |
| 每个 `reference_bindings[]` | 一个 `image_asset` | `ProductImageAsset` id + 用户可见 label/role 文本 | 下游 edge |
| 每个 `prompt_generation` 节点 + 对应 `prompt_plans[].payload` | 一个 `prompt_generation` | 提示词生成配置；确认时还没有运行结果，current Artifact 为空 | `images[]`、plan key |
| 每个 `image_types[].images[]` / `image_generation` 节点 | 一个 `image_generation` | `GenerationSpec`、`DeliverySpec`、变化指令、用途标签 | 隐藏的 prompt 所属 |

`creative_brief`：若 Draft 没有独立 brief 对象，adapter 从 `prompt_plans` 的 `design_goal` / `creative_boundary` / `text` 合成 **一个** 工作流级 brief 节点，而不是每个 prompt 复制一份。Prompt 节点通过 `brief` edge 引用它。禁止为了「看起来完整」伪造第二份商品事实。

### 4.3 边

adapter 只根据 Draft `edges[]` 的 source/target 节点类型生成 typed edge，role 由 Node Catalog 决定，不接受 Draft 里的 handle 字符串作为权威。

| Draft / V2 关系 | v3 `dataType` | `role` |
|---|---|---|
| `product_context` → `prompt_generation` | `product_facts` | `facts` |
| `product_context` → `image_generation` | 不生成 | v3 图片节点不接收 facts；facts 只进 prompt |
| `reference_image` → `prompt_generation` 或 `image_generation` | `image_asset` | `reference` |
| `prompt_generation` → `image_generation` | `prompt` | `prompt` |
| `image_generation` → `image_generation` | `image_asset` | `reference` |
| V2 工作流级 Visual System（运行时隐式注入） | `visual_system` | `visual_guidance` |
| 从 prompt payload 抽出的工作流级创作要求 | `creative_brief` | `brief` |

V2 允许 `product_context` → `image_generation` 的 facts 边。v3 矩阵不允许。adapter 丢弃这条边，不把它改写成别的 role。图片节点的商品约束只通过 prompt 上游进入；若某张图在 Draft 里只靠 facts 边、没有 prompt 边，adapter 失败并返回结构化校验错误，不得猜一条 prompt。

V2 把 Visual System 当作整图运行时隐式输入。ADR 0008 禁止 compiler 再做这种注入，所以 adapter **必须写成画布上可见的节点和边**，作为用户确认的初始 ChangeSet 的一部分，而不是运行时补边。默认连接：

- `visual_system` → 每个 `prompt_generation` 和 `image_generation`（每个目标最多一条 `visual_guidance`）
- `creative_brief` → 每个 `prompt_generation`

这些边必须出现在物化后的 graph projection 里，用户可以立刻删。禁止 compiler 在没有边时仍读取 workflow.visual_system_version。

多条同类型 reference 边必须写入稳定 `order`（按 Draft 边顺序或 reference_bindings 顺序），供 compiler 按 role/order 读取。

`reference_bindings` 生成 `image_asset` 节点后：

- Draft 里有出边：生成对应 `reference` edge
- Draft 里没有出边：节点合法，标记未使用

换绑不在 adapter 里发生。folder 仍是组织对象，按 Draft `folders[]` / `folder_key` 写入，不进入 DAG。

### 4.5 Prompt 配置、证据图、fact 子集

确认时 `prompt_generation` 还没有 Artifact。节点 config 保存可编辑的提示词生成字段（shared_rules、composition、atmosphere 等）。**禁止**把 `payload.images[]` 留在 Artifact 或 config 里当下游名单。

`evidence_asset_ids`：Draft 已要求它与画布直接参考边一致（`workflow_drafts/contracts.py`）。adapter 只物化那些边；Artifact 不得再作为运行时 reference 来源。

`fact_keys`：V2 允许单条 prompt 引用事实子集。v3 第一刀只创建一个 `product_source`，facts 边送出完整 fact set。**丢掉 per-prompt fact_keys**，compiler 不得用节点 config 再过滤 incoming facts。若产品以后要子集，必须拆成多个可连边的事实节点，不能在 prompt JSON 里藏名单。

### 4.6 Visual Exception

V2 的 `visual_exceptions` 按 `workflow` / `image_type` / `image_plan` 覆盖视觉字段，运行时在 `v2_execution.py` 里按 plan key 匹配。v3 没有 plan key。

映射：

- `scope.type=workflow`：落到 `visual_system` 节点配置，或在确认时已合并进 Visual System version（二选一，实现时只留一个权威，禁止 version 与节点 config 双份覆盖）。
- `scope.type=image_type` 或 `image_plan`：落到对应 `image_generation` 节点的可编辑覆盖字段。compiler 读取 `visual_guidance` 边得到基础 Visual System，再叠加该节点 config。这是节点配置，不是第二条视觉边。

### 4.7 再次物化

V2 允许对同一商品再确认 Draft，停用旧图并写 `revision+1` 的新 `ProductWorkflow`。第一刀：**已有 active v3 graph 时拒绝再跑 adapter**。后续改图只走 Graph Command。这是相对 V2 的行为收口，必须在确认 UX 上显示，不能静默失败。

### 4.8 配方

`workflow_recipes/service.py` 的 `apply_workflow_recipe` 今天只创建 `WorkflowDraft`（fragment 还会记下 `schema_version=2` 的 base workflow）。第一刀 **不** 把配方直接变成 ChangeSet。

- 全图配方：仍生成 Draft → 用户确认 → 本 adapter。
- 片段配方：base 改为当前 active v3 graph revision，生成的 Draft 确认后走 Graph Command 合并；若第一刀来不及做片段合并，片段 apply 必须对 v3 商品返回明确未实现冲突，禁止再写 `schema_version=2` 的 base_workflow_id。
- 保存配方：可第二刀。第一刀画布「保存为配方」若仍读 V2 提取器，过 gate 前要改成从 v3 graph 提取，或暂时禁用，不能在 v3 图上写出 V2 recipe payload。

### 4.4 幂等与冲突

沿用物化级合同，不新造平行 ledger：

- 幂等键 + request hash 指向「确认的 draft revision → 初始图」
- 同一键同一 hash：返回已有 workflow
- 同一键不同 hash：conflict
- 商品已有 active schema-v3 workflow：conflict（本切片不允许静默再物化一份平行图）
- 开发库允许在切片上线前清空 V2 图数据；adapter 不读取、不转换 V2 `ProductWorkflow` 行

Reveal events 继续只控制呈现。断开动画不能留下半张图：ChangeSet 事务提交前浏览器只看到 Draft；提交后读 v3 projection。

## 5. 图命令与运行（本切片必须同时存在）

### 5.1 Graph Command Service

唯一写图入口。Web 画布、adapter、以后的 Agent/Recipe 都走它。

Owner：`application/product_workflow/` 下新模块（建议 `catalog.py`、`changesets.py`、`graph_commands.py`、`graph_queries.py`）。不要另起顶层 graph 包。

ChangeSet 操作本切片必须能表达当前 V2 画布已有的持久编辑，否则物化后的 folder/复制/删除会悬空：

- ADR 已列：`create_node`、`update_node_config`、`rename_node`、`delete_node`、`connect_nodes`、`disconnect_edge`、`move_nodes`、`create_group`、`move_nodes_to_group`
- 组合即可、不单开 opcode：复制/粘贴 = 一组 create+connect；删除选区 = delete+disconnect；自动布局 = `move_nodes`
- 「固定为图片素材」：从 `image_generation` 当前 Artifact 创建独立 `image_asset` 节点（`create_node` + 绑定）。把 `image_generation` 输出连到下游是另一条 `reference` 边，不自动创建素材节点

Folder 命令不能第二刀再补。adapter 会写入 folder，用户必须能重命名、改成员、平移、解散。这些走 `create_group` / `move_nodes_to_group` 及对称的 rename/dissolve（若 ADR 名单不够，实现时作为 group 的 `update_node_config` 同类命令补进 Catalog，仍经 Graph Command，不另开 V2 folder API）。

每次成功应用：

- `base_graph_revision` 必须匹配
- 只增加一次 graph revision
- 写入 operation group 与 inverse
- 不相交改动可 rebase 后再校验；相交则结构化冲突

校验分两层：持久化只拒绝类型不兼容、基数冲突、跨工作流、环、越权；运行前再检查必要输入。

### 5.2 Graph Context Compiler 与 Run

`application/product_workflow/execution.py` 在 **合并进主线的那次提交** 里只调度 v3。仓库可以在功能分支开发；一个正在服务的进程里禁止 V2/v3 两个 executor 并存，也禁止 flag 选边。

配置状态（`incomplete` / `ready` / `stale`）由 graph query 按当前 revision 推导，不另存一份权威表。执行状态仍在 WorkflowRun / NodeRun。

运行固定 graph revision、节点配置、incoming edge、上游 Artifact id、provider effective config。三个范围：此节点（含需要更新的祖先）、到此节点、整图。旧 snapshot 的成功结果不得覆盖新 revision 的 current Artifact；需要采用时用户显式选择，或走「固定为图片素材」。

`GenerationSpec` 留在 `image_generation` 配置；provider effective 与实测输出进 Artifact。`DeliverySpec` 仍是节点上的确定性派生，不增加 DAG 节点，也不走图片模型（ADR 0003 这条在 v3 继续成立）。

Agent 运行请求继续走「待确认请求 → 同一 run use case」，形状对齐今天的 `agent/workflow_run_requests.py`，内部改为 v3 run。Pi 不拥有业务事务。

### 5.3 HTTP

与 ADR 0008 §12 对齐：v3 进入 `codex/development` 时，模型和 API 以 schema-v3 为唯一在线图合同，删除 Draft→V2 物化和 V2 图命令。开发期间用 **独立分支 + 独立数据库**，不要在同一 FastAPI 进程用 flag 同时挂 V2 和 v3 物化。

`web/src/lib/api.ts` 在该分支改为 v3 图/运行方法。商品、Agent 对话、素材库、legacy archive 路由保留。

目标路径（名称可在实现时微调，语义固定）：

- `GET` 当前 graph projection（含 revision、节点配置状态、未使用标记、incoming/outgoing 摘要）
- `POST .../changesets`
- `GET/POST/cancel/retry` WorkflowRun，带 revision snapshot

交互继承表要求的 `POST /api/v3/products/{product_id}/workflows/{workflow_id}/changesets` 是画布写入合同。

## 6. 素材三个动作

现有身份保持：`MediaObject`、`MediaLibraryAsset`、`ProductImageAsset`、`WorkflowMediaLibraryAsset`。Owner 仍是 `media_objects.py`、`media_library/`、`product_images/`。

| 动作 | 现有代码 | v3 写入 |
|---|---|---|
| 加入工作流子图库 | `media_library/workflow.py` | 只写 `WorkflowMediaLibraryAsset`，不创建节点或边 |
| 绑定到图片节点 | 今天 `reference_image.bound_image_asset_id` | `update_node_config` 改 `image_asset` 节点的资产 id；不自动 connect/disconnect |
| 供下游使用 | 今天参考节点出边 | `connect_nodes`，`dataType=image_asset`，`role=reference` |

拖入空白：创建并绑定 `image_asset` 节点（一个 ChangeSet）。  
拖到聚合输入：创建或 **用户明确选择复用** 已有节点，再创建 `reference` edge。禁止凭同一资产、坐标或选区自动合并。

Inspector 必须区分：未绑定、已绑定未使用、已连接及消费者列表。

## 7. 工作台

| 目录 | 本切片 |
|---|---|
| `workbench/agent/` | 保留为可选辅助。直接创建进入工作台时可以没有 Conversation。Agent 路径仍确认 Draft revision。run request 走 v3 run。对话不能挡住画布运行、编辑、选图。 |
| `workbench/chrome/` | 保留。节点卡、快捷键、Explorer、Inspector 壳不换业务 DTO 源到 V2。 |
| `workbench/canvas/` | 换数据源。`graph.ts`、画布、AddNode、Inspector 内容、Runs、History、MediaLibraryPanel 写入改为 v3 projection / changeset / run。 |

`v2WorkflowHistory.ts` 的浏览器 undo 不能作为 v3 权威。撤销调用 inverse ChangeSet。

chrome 仍不得引用 agent/canvas。

## 8. Agent 第一刀

Agent 创建是第二条入口，不是唯一入口。

- 直接创建：无 Conversation、无 Draft；模版 ChangeSet 即图
- `propose_workflow_draft` 只出现在用户选择 Agent 创建之后，只写 Draft revision
- 用户确认后由 adapter 写成同一套 v3 graph
- `request_workflow_run` 仍创建待确认请求，确认后走同一 v3 run
- 本切片不增加 Agent 直接写节点 JSON 的 tool，也不做 live-graph GraphProposal

「用户和 Agent 创建的图使用同一数据模型」：两条入口的结构都落成同一份 v3 graph。直接创建路径必须能在没有 Agent 的情况下把提示词跑出来、把图跑完。

CONTEXT 里商品数量上限（类型 1–6 张、合计 30）约束的是创建期 Draft。自由画布是否继续限制 `image_generation` 个数：第一刀 **继续在 Graph Command 拒绝超过现有上限的新建图片节点**，避免无声取消产品上限。要放开必须改 CONTEXT/PRD，不在本切片偷偷放开。

## 9. 迁移

从 `20260820_0070` 开新 revision，不复用 `0071-0074`。新链建立 v3 图、edge typed 字段、graph revision、operation group、run snapshot 所需表或替换约束。

`ck_product_workflows_schema_version = 2` 必须随 v3 唯一权威一起改掉，不能先放宽成 `IN (2, 3)` 再靠应用层选边。

本地库若应用过已删除的 0071，只能备份恢复或换新库。

## 10. Gate 与 V2 退役

本切片 gate（全部在 **新代码** 上重做，不用旧 v3 scaffold）：

- 构思表单直接创建 → 预设模版图，无 Agent、无 Draft；上传参考图为未使用 `image_asset`
- 不跑 Agent 即可运行 `prompt_generation` 写出提示词，再运行 `image_generation`
- Agent 创建 → Draft 确认 → 初始图幂等、冲突、失败回滚；已有 active v3 时再次物化冲突可见
- 画布 create/connect/disconnect/delete/move、复制粘贴、folder 重命名/成员/解散各产生 ChangeSet 与 revision
- Visual System / brief 以画布边存在；compiler 在没边时不得读取 workflow 级隐式字段
- 绑定未连边显示未使用；换绑不改边
- 运行输入可追溯到 snapshot 的 incoming edges；visual exception 只作为目标节点 config 覆盖
- 全图配方经 Draft→adapter；片段配方要么合并进 v3 graph，要么明确拒绝，不得写回 schema-v2 base
- Agent run request 与页面 run 同一 use case
- 真实 PostgreSQL / Redis / worker / provider
- 桌面与 390px 浏览器；console/network 无 error
- 无 V2 fallback residue

通过后再删：在线 V2 route/schema/`v2_*.py`/canvas V2 hook/`api.ts` V2 图方法/对应测试。保留 `legacy_retirement/`、`legacy_archives*`、Gallery bridge、`/history`。

## 11. 验收对照

ADR 0008 §14.1–14.3 中，本切片必须覆盖：空画布可建六类节点、不完整节点可存在、edge 可删、聚合端口多 reference、绑定与使用分离、reload 后图一致、operation group、compiler 只读 incoming edges、单一 Graph Command、单一 WorkflowRun。

§14 里 GraphProposal 预览、Recipe **直接** ChangeSet、v1 archive 重建标为切片外。分组命令在第一刀必须能编辑 adapter 写出的 folder。

## 12. 文档张力（实现前保持可见）

这些不是笔误，是 ADR / 切片 / 交互表之间已经存在的分层，改 ADR 或扩大切片前不要假装已经统一：

1. ADR 0008 §11 终态是 WorkflowIntent 直接出 ChangeSet；第一刀仍用完整 WorkflowDraft 拓扑 + adapter。Draft 在第一刀仍然拥有一份确认前拓扑。
2. ADR 0008 §10 的 Agent live-graph tool 与 §14.3 提案预览不在第一刀。交互继承表把它们标待实现，顺序在第 5 步，晚于运行接通。
3. ADR 0008 §12 要求进入主线时直接替换、不双执行器。交互表和旧 ROADMAP 容易读成「仓库里长期挂两套 API」。以独立分支+独立库开发、合并时单权威为准。
4. 交互继承表「待实现」指 ChangeSet 版交互，不是当前 V2 画布没有框选。第一刀验收必须在新代码上重跑该表中除 GraphProposal/Recipe/v1 重建以外的行。
5. ADR 0003 的 DeliverySpec 不进 DAG、GenerationSpec 与实测分离，第一刀继续成立；不要为 v3 再加交付节点。
