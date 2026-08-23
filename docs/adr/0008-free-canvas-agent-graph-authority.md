# ADR 0008: 自由画布、图权威与 Agent 协作合同

## 状态

Accepted。当前在线图是 schema-v3 `workflow_graphs`。Graph Command、live-graph GraphProposal 与配方 ChangeSet 是本 ADR 的已接受合同。

下文背景记录从 schema-v2 出发的理由，不是当前实现。尚未交付的后续：工作台浏览器证明见 [`docs/ROADMAP.md`](../ROADMAP.md)；Draft 拓扑收敛为 WorkflowIntent + ChangeSet、以及 v1 archive 重建，仍属后续决策。

## 背景

决策当时，schema-v2 将工作流计划物化为 `product_context`、`reference_image`、`prompt_generation` 和 `image_generation` 节点。画布可以展示和编辑部分关系，但提示词、图片计划和运行归属仍同时存在于 Prompt Artifact、节点配置 key 和 edge 中。为了维持三份关系一致，当时的实现要求特定 lineage edge 必须存在，并把提示词节点和图片节点绑定为隐藏的计划组。

这种模型可以表达经过确认的固定生产计划，但不能作为自由 DAG 编辑器的长期基础：

- 用户不能先建立不完整拓扑再补充配置。
- 节点创建和删除受隐藏 Artifact 合同约束。
- 删除画布 edge 不一定足以删除运行关系。
- 多个参考来源可能在 Artifact、Visual System 和画布 edge 之间重复。
- Agent 通过完整 WorkflowDraft 提出另一份拓扑，再 materialize 为正式 Workflow，形成双图边界。
- 用户、Agent 和配方没有统一的图修改合同、原子撤销和 revision 冲突语义。

ProductFlow 的工作流编辑器将演进为真正的自由画布。节点表示数据源或处理步骤，edge 表示唯一依赖关系，Artifact 表示一次执行产生的不可变结果。用户、Agent 和配方必须通过同一套图命令修改同一份正式工作流。

## 决策目标

目标画布满足以下合同：

1. 所有支持的节点类型始终可以创建。
2. 编辑中的不完整 DAG 是合法状态。
3. 所有 edge 都可以创建、查看、删除和撤销。
4. 只拒绝类型不兼容、输入基数冲突、越权和形成环的连接。
5. 必要输入只决定节点能否运行，不决定节点能否存在。
6. 节点有效上下文完全由当前 graph revision 的 incoming edges 推导。
7. Prompt Artifact、Visual System、页面选区和 Agent 会话不得隐式增加运行输入。
8. 用户、Agent 和配方使用同一个 Graph Command Service。
9. Agent 不拥有第二份正式拓扑，也不建立第二套执行器。

## 1. 权威对象

系统只有三个工作流权威对象：

| 对象 | 负责 | 不负责 |
|---|---|---|
| Node | 数据源、处理配置、配置状态和当前输出引用 | 保存其他节点的所属关系 |
| Edge | 节点间的数据类型、输入角色、顺序和依赖 | 保存 Artifact 内容 |
| Artifact | 某次运行产生的不可变提示词、图片或其他结果 | 保存当前拓扑或下游消费者 |

正式工作流由 PostgreSQL 中的 canonical graph 表示。页面选择、Agent transcript、Task、Draft 和本地 runtime 文件都不是图权威。

## 2. Schema-v3 节点目录

首个 schema-v3 版本支持六类用户可理解的节点：

| 节点 | 类型 | 用户语义 | 输入 | 输出 |
|---|---|---|---|---|
| 商品资料 | `product_source` | 提供商品事实快照 | 无 | `product_facts` |
| 图片素材 | `image_asset` | 持有一张已有图片 | 无 | `image_asset` |
| 创作要求 | `creative_brief` | 运行时生成目标、文案和限制 | 商品事实、参考图 | `creative_brief` |
| 视觉规范 | `visual_system` | 运行时生成风格和背景约束 | 商品事实、参考图 | `visual_system` |
| 提示词生成 | `prompt_generation` | 根据上游输入产生提示词 | 聚合输入 | `prompt` |
| 图片生成 | `image_generation` | 根据提示词和参考图产生图片 | 聚合输入 | `image_asset` |

`WorkflowFolder` 继续是画布组织对象，不是 DAG 节点，不拥有端口、运行状态或上下文语义。

图片生成结果不自动创建另一类结果节点。`image_generation` 节点通过当前输出引用和 `image_asset` 输出端口向下游提供最新结果。用户需要固定某个历史结果时，显式执行“固定为图片素材”，创建独立 `image_asset` 节点。

### 2.1 节点所有权

- `product_source` 保存商品或事实版本引用。
- `image_asset` 保存一个 canonical 图片资产引用及用户可编辑的用途标签。
- `creative_brief` 保存运行写出的创作目标、必需文案和禁止项。
- `visual_system` 保存运行写出的风格/背景 overlay，或明确固定的 Visual System version 引用。
- `prompt_generation` 保存提示词生成配置和当前 Prompt Artifact 引用。
- `image_generation` 保存画幅、质量、变化指令、交付规格和当前图片 Artifact 引用。

schema-v3 不使用 `prompt_plan_key`、`image_type_key` 或 `image_plan_key` 表达拓扑。Prompt Artifact 不保存下游 `images[]` 列表。图片节点是否使用某个提示词只由 `prompt` edge 决定。

### 2.2 图片资产、工作流子图库与图片节点

现有素材库的三层身份继续保留：

| 对象 | 语义 | 与 v3 图的关系 |
|---|---|---|
| `MediaLibraryAsset` | 商家全局素材库中的可组织资产 | 可以被多个商品和工作流复用 |
| `ProductImageAsset` | 某个商品范围内的稳定图片身份 | `image_asset` 节点绑定的 canonical 资产身份 |
| `WorkflowMediaLibraryAsset` | 全局素材加入某个工作流子图库的关联 | 决定素材在该工作流的选择器中可用，不表示已进入运行上下文 |

将素材加入工作流子图库、将素材绑定到图片节点、将图片节点连接到下游是三个独立动作：

1. 子图库关联提供素材选择范围，不复制媒体 bytes。
2. `image_asset` 节点持有一个明确的 `ProductImageAsset` 引用。
3. `reference` edge 表示该素材被下游节点实际使用。

已绑定但没有出边的图片节点是合法状态，画布和 Inspector 必须显示“未使用”。同一图片节点可以通过多条 `reference` edge 连接多个消费者。换绑素材只修改节点绑定，不自动创建、删除或重定向 edge；下游关系仍由用户可见的图命令管理。

工作台沿用现有全局素材库和工作流子图库的搜索、文件夹、标签、上传、预览、选择与拖入体验。v3 适配只替换选择后的写入合同：拖入空白区域创建并绑定真实 `image_asset` 节点；拖到兼容节点的聚合输入时创建或复用图片节点，并创建可见的 `reference` edge。复用节点必须由用户明确选择，系统不得仅凭同一资产、坐标或当前选区合并节点。

## 3. 聚合端口与 typed edge

每个处理节点在画布上最多显示一个左侧聚合输入端口。每个有输出的节点显示一个右侧输出端口。用户不面对一排内部 handle。

聚合端口可以接收多条不同数据类型的 edge。节点详情按语义分组展示实际输入，例如商品资料、创作要求、提示词、参考图片和视觉规范。

Edge 至少保存：

```ts
interface WorkflowEdgeV3 {
  id: string;
  workflowId: string;
  sourceNodeId: string;
  targetNodeId: string;
  dataType:
    | "product_facts"
    | "image_asset"
    | "creative_brief"
    | "visual_system"
    | "prompt";
  role:
    | "facts"
    | "reference"
    | "brief"
    | "visual_guidance"
    | "prompt";
  order: number;
}
```

`dataType` 由源节点输出决定，`role` 由源输出与目标接受合同决定，不能由客户端提交任意字符串。

### 3.1 首版连接矩阵

| 上游输出 | 创作要求 | 视觉规范 | 提示词生成 | 图片生成 |
|---|---:|---:|---:|---:|
| `product_facts` | 最多一条，可选 | 最多一条，可选 | 多条，可选 | 不支持 |
| `image_asset` | 多条参考图 | 多条参考图 | 多条参考图 | 多条参考图 |
| `creative_brief` | 不支持 | 不支持 | 多条，可选 | 不支持 |
| `visual_system` | 不支持 | 不支持 | 最多一条 | 最多一条 |
| `prompt` | 不支持 | 不支持 | 不支持 | 最多一条，运行必需 |

图片生成节点的 `image_asset` 输出可以进入提示词生成或另一个图片生成节点，角色均为 `reference`。一张图片素材可以扇出到多个消费者，每条使用关系都是独立 edge。

自由画布不等于无类型连接。连接校验只负责：

- 源输出与目标输入兼容。
- 输入基数没有冲突。
- source 和 target 属于同一 Workflow。
- 新 edge 不形成环。
- 当前 actor 有修改权限。

## 4. 编辑状态与运行状态

节点配置状态和执行状态分开保存或推导。

配置状态：

- `incomplete`：缺少必要配置或输入。
- `ready`：当前 graph revision 下可以运行。
- `stale`：上游、配置或依赖 Artifact 已变化，当前输出过期。

执行状态继续表达 `idle`、`queued`、`running`、`succeeded`、`failed` 和 `cancelled`。

合法编辑状态包括：

- 未绑定资产的图片素材节点。
- 没有消费者的参考图片节点。
- 没有下游图片的提示词节点。
- 没有提示词输入的图片生成节点。
- 删除必要 edge 后变为 `incomplete` 的处理节点。

系统不得通过“必要 lineage edge 不可删除”维持运行完整性。任何 edge 均可删除；删除后立即重新计算下游配置状态和 stale 范围。

## 5. 节点创建和画布交互

节点面板始终提供全部节点类型。创建节点不要求先选中其他节点，也不要求理解提示词组、Artifact 或 plan key。

点击或拖入后立即创建真实节点。新节点可以为空或不完整，但不能填入伪造的创作内容冒充有效配置。

系统可以提供显式快捷动作，例如“创建图片生成并连接当前提示词”或“作为参考连接到所选节点”。快捷动作必须：

1. 由用户明确触发。
2. 创建真实节点和 edge。
3. 立即在画布显示关系。
4. 作为一个 operation group 撤销。

系统不得根据坐标、最近节点、当前选区或创建顺序暗中建立关系。页面选区只帮助解释用户意图，不进入运行时上下文。

边默认使用低噪声语义样式；关系文本只在 hover、selection 或详情查看时显示。处理节点使用一个聚合输入端口，实际输入在节点摘要和详情面板中按类型列出。

### 5.1 工作台呈现与交互继承

v3 改造保留远端 v1 画布和本地 v2 工作台中已经成熟的呈现与操作方式，包括画布占比、节点信息层级、对象工具条、Inspector、运行记录、素材选择、框选、多选拖动、复制粘贴、删除、缩放、适应视图、快捷键和移动端抽屉。复用范围限于无业务语义的组件、交互 reducer 和视觉规则；v1/v2 DTO、查询、Draft materialization、plan key 和执行器不得随 UI 一起恢复。

工作台实现必须以成熟画布 shell 为呈现基线，以 v3 graph/revision、Graph Inspector、run contract 和素材绑定合同为数据源。此前被删除的 v3 scaffold、浏览器回归、截图和 focused tests 只保留历史背景，不构成当前实现或验收证据。新的浏览器与测试证据必须在重新实现的当前代码上产生。

## 6. 图上下文编译与执行

运行节点或工作流时，Graph Context Compiler 固定明确的 graph revision，并只读取目标节点的直接 incoming edges：

```text
target node
  -> incoming edges ordered by role/order
  -> source node current artifacts or source data
  -> typed runtime input
  -> input validation
  -> provider execution
```

提示词生成运行输入：

```ts
interface PromptRuntimeInput {
  productFacts: ProductFacts[];
  briefs: CreativeBrief[];
  referenceImages: ImageArtifact[];
  visualSystem: VisualSystemArtifact | null;
}
```

图片生成运行输入：

```ts
interface ImageRuntimeInput {
  prompt: PromptArtifact;
  referenceImages: ImageArtifact[];
  visualSystem: VisualSystemArtifact | null;
  generationSpec: GenerationSpec;
}
```

运行时不得读取坐标、最近节点、当前选择、Prompt Artifact 历史 reference 列表、Visual System reference 列表或 plan key 来补充依赖。历史引用只用于审计。需要重新启用时必须投影为 source node 和真实 edge。

支持三个执行范围：

- 运行此节点：运行目标及其需要更新的祖先。
- 运行到此节点：按拓扑执行所有必要祖先直到目标。
- 运行整个画布：按拓扑执行所有可运行处理节点。

运行前错误必须定位到具体节点、缺失输入或冲突 edge。编辑器允许保存不完整图，执行器只阻止受影响的执行范围。

每次运行固定 graph revision、节点配置版本、incoming edge 集合、上游 Artifact ID 和 provider effective config。运行期间图发生变化时，旧 snapshot 继续执行；旧 revision 结果不得静默覆盖新 revision 的 current output，应标记为历史结果或待用户采用。

## 7. Artifact 合同

Artifact 是不可变执行结果，至少记录：

- producer node ID。
- producer run ID。
- graph revision。
- artifact type 和 schema version。
- payload 或 canonical asset ID。
- 输入 Artifact ID 摘要。
- provider/model/effective config。
- created_at。

节点只保存当前采用的 Artifact 引用。重新运行产生新 Artifact。撤销图编辑不物理删除已经产生的 Artifact 或媒体 bytes。

## 8. 统一 Graph Command Service

用户画布操作、Agent 和 WorkflowRecipe 都通过同一个应用层服务修改 canonical graph：

```text
Web / Agent / Recipe
  -> WorkflowChangeSet
  -> Graph Command Service
  -> Node Catalog validation
  -> revision / type / cardinality / DAG validation
  -> atomic persistence
  -> canonical graph
```

Node Catalog 是节点能力的单一 owner，同时驱动画布连接、节点表单、Agent tool schema、ChangeSet 校验和运行前检查。

目录合同至少包含节点输出类型、接受输入、基数、运行必需性和可编辑配置字段。前端与 Agent service 不复制另一份节点兼容矩阵。

## 9. WorkflowChangeSet

多节点修改使用原子 ChangeSet：

```ts
interface WorkflowChangeSet {
  baseGraphRevision: number;
  summary: string;
  operations: GraphOperation[];
}
```

支持 `create_node`、`update_node_config`、`rename_node`、`delete_node`、`connect_nodes`、`disconnect_edge`、`move_nodes`、`create_group` 和 `move_nodes_to_group`。

同一 ChangeSet 新建节点使用 `client_ref`，后续 edge 可以引用该临时标识。服务端在一个事务中解析引用、验证完整 proposed graph、写入节点和 edge，并只增加一次 graph revision。任一步失败时整组操作不落库。

每次成功应用生成 operation group：

```text
operation_group_id
actor_type: user | agent | recipe
actor_id
base_revision
result_revision
operations
inverse_operations
created_at
```

Agent 批量修改只占一个撤销步骤。撤销仍经过 revision 和图规则校验；有外部副作用的 Artifact 保留为历史结果。

## 10. Agent 协作

Agent 是 canonical graph 的协作者，不是图 owner。Agent 每次工作先读取有界 `WorkflowGraphSnapshot`，其中包含 workflow ID、graph revision、节点摘要、edge、配置状态、当前 Artifact 摘要、页面显式选区和 Node Catalog。

页面选区可以帮助 Agent 解析“这张图”“第二个节点”等指代。任何实际依赖仍必须通过 ChangeSet 创建 edge。

Agent 工具收敛为：

- `get_workflow_graph`
- `get_node_detail`
- `get_node_catalog`
- `propose_workflow_changes`
- `apply_workflow_changes`
- `discard_workflow_proposal`
- `run_workflow_nodes`
- `get_workflow_run`
- `cancel_workflow_run`
- `list_media_assets`
- `focus_canvas_items`

Agent 不直接写节点 JSON，不提交 plan key，不拼 provider 请求，不维护隐藏 reference scope，也不绕过 Graph Command Service。

### 10.1 直接执行与确认

| 操作 | 默认行为 |
|---|---|
| 读取、定位、解释 | 直接执行 |
| 用户明确要求的单一可逆图编辑 | 直接执行，提供撤销 |
| 多节点重构、批量删除、预设覆盖 | 显示 ChangeSet 预览并确认 |
| 明确说“运行”的执行请求 | 复用正式运行链路执行 |
| 未明确授权的有费用运行 | 请求确认 |
| 删除已有结果或高影响配置 | 请求确认 |

ChangeSet 提案可以投影到画布提案层：新增节点和 edge 使用虚线，删除项降低透明度，修改项显示差异，并明确标记“尚未应用”。提案层不是正式 DAG，不能运行；确认后原子转为真实图，取消后完全移除。

### 10.2 revision 冲突

ChangeSet 必须携带 `base_graph_revision`。当前 revision 变化时：

- 改动对象完全不相交，可以重新验证后 rebase。
- 修改同一节点、edge、输入基数或删除关系时返回结构化冲突。
- Agent 重新读取当前图并生成新提案。
- 禁止 last-write-wins 和旧完整快照覆盖当前图。

### 10.3 Session、Task 和运行

AgentSession 保存长期交流，AgentTask 保存一个业务目标和任务执行状态。二者都不保存另一份正式图。

每个图编辑 Task 固定 base revision，独立等待确认、应用、取消和处理冲突。不同 Task 不得静默覆盖。Agent 请求运行时复用同一个 WorkflowRun application use case、Graph Context Compiler、队列、取消和重试合同。

## 11. WorkflowDraft 和 Recipe 收敛

WorkflowDraft 不再长期拥有一份与正式工作流平行的完整拓扑。目标对象拆分为：

- `WorkflowIntent`：用户目标、数量、用途、限制和确认答案。
- `WorkflowChangeSet`：对当前 canonical graph 的具体修改。
- canonical graph：已经应用的正式节点和 edge。

新商品流程由 Agent 根据 WorkflowIntent 对空图或基础商品源节点提出 ChangeSet；用户在画布查看提案并确认后原子应用。现有 Draft revision 的审阅、幂等和冲突价值保留，但不再通过完整 Draft materialization 产生第二份拓扑。

WorkflowRecipe 保存可复用结构和配置，应用时解析为 ChangeSet，经同一 Node Catalog、revision、DAG 和权限校验后写入，不拥有独立执行器。

## 12. Schema-v3 直接替换

schema-v2 从未部署到生产环境，没有需要保留的 v2 工作流、运行记录或 Artifact。实施采用直接替换，同时保留已经归档的生产 v1 历史及其重建入口：

1. 清空本地开发环境中的 v2 workflow 图数据。
2. 数据库模型和 API 直接以 schema-v3 为唯一合同。
3. 删除 `product_context`、`reference_image`、`prompt_plan_key`、`image_type_key`、`image_plan_key` 和 Prompt Artifact `images[]` 拓扑语义。
4. 删除 Draft materialization、v2 图命令、v2 执行器和隐藏 reference merge，不提供双读、回填或运行时 fallback。
5. 历史 Alembic revision 可以继续用于构建其他既有表；新的 schema revision 只负责建立最终 v3 结构，不转换 v2 工作流数据。
6. 保留 v1 `legacy_workflow_archives`、Canvas Agent archives、user template archives、hash、导出和 canonical 资产引用。
7. v1 重建入口改为 `legacy archive -> WorkflowIntent -> v3 WorkflowChangeSet`；重建结果必须经过用户确认和同一 Graph Command Service。
8. v1 中无法确定性转换的关系必须在提案中标记为待确认，不使用 v2 plan key、画布坐标或最近节点推断。
9. 开发数据库需要保留的商品、媒体资产和 v1 archives 不受影响；只重置 v2 workflow 图及其从属数据。

上线 gate 覆盖最终 schema、v1 archive 到 v3 提案的重建、Graph Context Compiler 输入追踪、真实 provider、真实 PostgreSQL/Redis、真实浏览器、Agent ChangeSet、并发冲突、撤销和运行历史。不存在 v2 图重建或 v2/v3 对账 gate。

## 13. 画布信息架构

画布是主要操作面。常驻控件只保留缩放、适应视图、整理、撤销、重做、运行和有界更多菜单。节点添加、详情、运行记录和图库通过侧栏工具进入。

不再提供：

- 顶部流程统计状态栏。
- 常驻 edge 文本。
- 多个难以识别的目标端口。
- “提示词组”作为用户必须理解的创建单位。
- 依靠复制节点实现基本创建能力。
- 不可删除的 lineage edge。
- 画布外 reference scope 表单。

节点详情必须提供“实际运行输入”视图，逐项显示来源节点、edge role、Artifact 和顺序。参考图节点必须显示素材是否绑定、是否被使用以及所有下游消费者。

## 14. 验收标准

### 14.1 图编辑

- 空画布可以直接创建所有节点类型。
- 图片生成节点可以在没有提示词时存在并显示缺失输入。
- 所有 edge 都可以删除和撤销。
- 一个聚合输入端口可以接收多条 typed edge。
- 一张图片素材可以连接多个消费者。
- 图片生成结果可以作为下游参考图。
- 页面重载后节点、edge、顺序和配置一致。
- 创建、连接、断开、删除和批量修改都有 operation group。

### 14.2 运行时

- provider 输入可以逐项追溯到固定 graph revision 的 incoming edge。
- 删除 edge 后对应运行输入消失。
- Prompt Artifact 和 Visual System 不隐式注入参考图。
- 运行时不读取坐标、选区、最近节点或 plan key 推断关系。
- 图变化期间的旧运行结果不会覆盖新 revision 输出。

### 14.3 Agent

- 用户和 Agent 创建的图使用同一数据模型和 Graph Command Service。
- Agent 的结构修改可以在画布预览或立即看到。
- Agent 批量修改可以一次撤销。
- Agent 与用户并发编辑不会静默覆盖。
- Agent 提案取消后不留下正式节点、edge 或运行时上下文。
- Session 或 Task 删除不删除 canonical graph。
- 画布不依赖某个 Agent Session 存活。
- Agent 请求运行复用正式 WorkflowRun，不建立第二套执行器。

### 14.4 产品理解

- 第一次使用者不需要理解 Artifact version、plan key、lineage 或 materialization。
- 用户可以从节点和 edge 判断每个输入为何存在。
- 用户可以从画布确认某张参考图是否生效以及影响哪些节点。
- 所有系统快捷操作最终形成可见、可编辑和可撤销的真实图关系。

## 后果

- schema-v3 是跨数据库、应用层、执行器、Agent service 和 Web 的直接替换，不能按纯前端改版实施。
- 允许不完整编辑态后，图持久化校验与运行前完整性校验必须拆开。
- Prompt Artifact、图片节点和 edge 的三重拓扑所有权将收敛到 typed edge。
- Agent 的完整 WorkflowDraft 拓扑将收敛为 WorkflowIntent 与 WorkflowChangeSet。
- 所有批量图操作需要 operation group、inverse operation 和 revision 冲突合同。
- 在线 v2 合同已经删除。当前实现写在 `CONTEXT.md`、`ARCHITECTURE.md`、PRD 和用户指南。

## 排除方案

- 继续在 schema-v2 上增加创建按钮，同时保留 plan-key 所属关系。
- 允许 Agent 直接写工作流表或节点 JSON。
- 让 Agent、配方和用户分别拥有不同图修改 API。
- 为保证工作流始终可运行而禁止删除必要 edge。
- 用坐标、最近节点或当前选择自动推断持久关系。
- 在 Prompt Artifact、Visual System 或 Session 中保存未投影的运行参考图。
- 用 last-write-wins 处理 Agent 与用户并发修改。
- 保留 v2/v3 双执行器作为长期 fallback。

## 相关决策

- ADR 0001：Agent Draft 权威边界。v3 实施后，工作流拓扑部分由本 ADR 的 WorkflowIntent/ChangeSet 合同取代。
- ADR 0003：GenerationSpec、DeliverySpec 和一层分组仍然有效。在线图权威以本 ADR 为准。
- ADR 0005：Agent 工作台 UI。本 ADR 进一步确定画布为主要工作面及 Agent 提案层边界。
- ADR 0007：Pi Agent runtime。Pi 继续负责模型 loop，ProductFlow Graph Command Service 继续拥有业务写入权威。
