// Package graph 实现 schema-v3 Graph Command：ChangeSet 应用、空图画布、撤销重做、Catalog、提案与 GraphRun。
//
// 职责：在线图只存在 workflow_graphs，schema 只能是 v3。Node Catalog 拥有连线规则与可编辑 config key；
// ChangeSet 的 config 不能引入未注册或退役 plan key。空画布合法，可编辑、撤销、配方预览。
//
// 调用时机：Web/Agent HTTP 改图走 [Service]；已有事务的跨包写入走 [WriteTx]；
// asynq worker 执行走 [Executor.ExecuteRun]。
// 提交运行只写 PENDING dispatch，不在 HTTP 请求里打 broker。
//
// 副作用：改图写 workflow_graphs / 节点 / 边 / 分组 / 历史；跑图写 workflow_graph_runs、
// workflow_graph_node_runs、workflow_graph_provider_effects、run events，并 Stage asynq。
// 生成图通过 [GeneratedImageWriter] 写成 ProductImageAsset；交付通过 [DeliveryQueuer] 排队。
//
// 错误：同节点丢失更新、拓扑 revision 不匹配、已有 running、空 inverse 等返回 Conflict；缺图/缺 run 返回 NotFound。
// 进程锁或 GraphRun execution lease 未拿到返回 queue.ErrBusy。无法证明的 provider 结果标 unknown
// （IsRetryable=false，不自动当失败重试）；已证明失败标 failed（可 RetryRun）。unknown 不可经 RetryRun 重试。
//
// 约束：本包不得 import product、recipe 或 delivery。跨切片走 [ProductGuard]、
// [GeneratedImageWriter]、[DeliveryQueuer]。不要把 V1 runtime 当回退，不要双写旧 schema。
package graph

const (
	// SchemaVersion 是唯一在线工作流 schema，值为 3，存在 workflow_graphs。
	SchemaVersion = 3
	// CatalogVersion 是 Node Catalog 合同版本；Web 与 Agent 共用 [CatalogJSON]。
	CatalogVersion = 9
	// DefaultGraphTitle 是空图画布与出生图未指定标题时的默认名。
	DefaultGraphTitle  = "商品创意工作流"
	maxImagePerType    = 6
	minImagePerType    = 1
	maxTotalImages     = 30
	maxReferenceAssets = 6
	maxRefLen          = 80
	maxTitleLen        = 255
	maxSummaryLen      = 500
)

// NodeType 是 schema-v3 节点类型闭集，持久化在 workflow_graph_nodes.node_type。
// 取值 product_source | image_asset | creative_brief | visual_system | image_prompt | image_generation。
// 给 Catalog、ChangeSet 与编译器看，不是商品图种 image_type_key。新增类型必须改 Catalog 与 Graph Command schema。
type NodeType string

const (
	// NodeProductSource 是商品资料节点，输出 product_facts。
	NodeProductSource NodeType = "product_source"
	// NodeImageAsset 绑定恰好一个 ProductImageAsset；绑定不是 reference 边。
	NodeImageAsset NodeType = "image_asset"
	// NodeCreativeBrief 是创作要求文稿节点，config 持有已发布文档。
	NodeCreativeBrief NodeType = "creative_brief"
	// NodeVisualSystem 是视觉规范文稿节点，config 持有 visual_overlay。
	NodeVisualSystem NodeType = "visual_system"
	// NodeImagePrompt 是提示词文稿节点，config.prompt 是 live 文档。
	NodeImagePrompt NodeType = "image_prompt"
	// NodeImageGeneration 消费 live prompt 文档生成图片；产物是 run lineage，不是编译门闩。
	NodeImageGeneration NodeType = "image_generation"
)

// EdgeDataType 是边携带的数据类型，由源节点输出类型决定，写入 workflow_graph_edges.data_type。
// 公开 ConnectNodesOp 不得带此字段，Apply 按 Catalog 推断。不要和 EdgeRole（目标端口）或图片 MIME 搞混。
type EdgeDataType string

const (
	// DataProductFacts 是 product_source 输出的事实流。
	DataProductFacts EdgeDataType = "product_facts"
	// DataImageAsset 是 image_asset 输出的商品图引用。
	DataImageAsset EdgeDataType = "image_asset"
	// DataCreativeBrief 是 creative_brief 输出的创作要求。
	DataCreativeBrief EdgeDataType = "creative_brief"
	// DataVisualSystem 是 visual_system 输出的视觉规范。
	DataVisualSystem EdgeDataType = "visual_system"
	// DataPrompt 是 image_prompt 输出的提示词文档。
	DataPrompt EdgeDataType = "prompt"
)

// EdgeRole 是处理节点命名输入端口；React Flow handle id 等于持久化边 role。
type EdgeRole string

const (
	// RoleFacts 接入 product_facts。
	RoleFacts EdgeRole = "facts"
	// RoleReference 接入参考图；与 image_asset 绑定不是同一件事。
	RoleReference EdgeRole = "reference"
	// RoleBrief 接入创作要求。
	RoleBrief EdgeRole = "brief"
	// RoleVisualGuidance 接入视觉规范。
	RoleVisualGuidance EdgeRole = "visual_guidance"
	// RolePrompt 接入提示词文档。
	RolePrompt EdgeRole = "prompt"
)

// ConfigStatus 是节点配置相对 Catalog 必填项与产物 digest 的就绪状态。
type ConfigStatus string

const (
	// ConfigIncomplete 表示必填 config 或运行所需入边缺失。
	ConfigIncomplete ConfigStatus = "incomplete"
	// ConfigReady 表示可运行；不等于「必须重新 cook」。
	ConfigReady ConfigStatus = "ready"
	// ConfigStale 表示已有产物但输入 digest 已变。
	ConfigStale ConfigStatus = "stale"
)

// ActorType 记录谁写入这条 ChangeSet，落在 workflow_operation_groups.actor_type。
// user=画布直接编辑；agent=立即写入或确认提案；recipe=配方应用；system=生成文稿 adopt 等回写。
// HTTP applyChangeSet 固定 ActorUser。公开请求不要自己填 system。
type ActorType string

const (
	// ActorUser 是人在画布上的直接编辑。
	ActorUser ActorType = "user"
	// ActorAgent 是 Agent 立即写入或确认提案。
	ActorAgent ActorType = "agent"
	// ActorRecipe 是配方应用到 live 图。
	ActorRecipe ActorType = "recipe"
	// ActorSystem 是系统侧回写（例如生成文稿 adopt）。
	ActorSystem ActorType = "system"
)

// HistoryKind 区分 workflow_operation_groups 的编辑账本方向：edit 可被 Undo；undo 的 inverse 给 Redo；redo 行为同 edit 可再撤销。
// 画布 can_undo/can_redo 只看栈顶这一行。不要把 HistoryRedo 当成「再执行一次 Undo」。
type HistoryKind string

const (
	// HistoryEdit 是正向编辑，可被 Undo 消费。
	HistoryEdit HistoryKind = "edit"
	// HistoryUndo 是撤销写入；下一次 Redo 消费它的 inverse。
	HistoryUndo HistoryKind = "undo"
	// HistoryRedo 是重做写入，行为与 HistoryEdit 一样进入可撤销栈。
	HistoryRedo HistoryKind = "redo"
)

// AppliedNode 是内存中的节点快照，供 Apply、Project 与 compiler 共用。
type AppliedNode struct {
	ID        string
	NodeType  NodeType // schema-v3 闭集，不是商品图种
	Title     string
	PositionX int            // 画布像素坐标
	PositionY int            // 画布像素坐标
	Config    map[string]any // Catalog 登记的可见配置
	// CurrentArtifactID / PendingCandidateArtifactID 只由 live-store 读取时填充，供 Project 复用节点行投影。
	// 它们不是 Apply 的输入，也不直接参与图快照序列化。
	CurrentArtifactID          *string `json:"-"`
	PendingCandidateArtifactID *string `json:"-"`
	// BoundAssetID 是 image_asset 绑定的 ProductImageAsset id；nil 表示未绑定。绑定不是 reference 边。
	BoundAssetID *string
	GroupID      *string
	// DocumentOrigin 是内容节点文稿来源：seed | generated | authored | collaborative。非内容节点为空。
	DocumentOrigin string
}

// AppliedEdge 是一条已应用的连线；DataType 与 Role 由 Catalog 在 Connect 时填入。
type AppliedEdge struct {
	ID           string
	SourceNodeID string
	TargetNodeID string
	DataType     EdgeDataType // 由 Catalog 在 Connect 时填入
	Role         EdgeRole     // 由 Catalog 在 Connect 时填入，公开 ChangeSet 不得带 role
	Order        int          // 同一目标同一 role 下的次序
}

// AppliedGroup 是 Apply 之后内存里的一层视觉分组，对应将写入的 workflow_graph_groups。
// 没有端口、运行、取消或嵌套。成员在 AppliedNode.GroupID，不在本结构。不要和 GroupView（HTTP）或图库文件夹搞混。
type AppliedGroup struct {
	ID    string
	Title string
}

// AppliedGraph 是一次 Apply 之后的完整图；Revision 随每次成功 ChangeSet 递增。
type AppliedGraph struct {
	Revision int            // 每次成功 ChangeSet 递增
	Nodes    []AppliedNode  // 空列表是 [] 不是 nil
	Edges    []AppliedEdge  // 空列表是 [] 不是 nil
	Groups   []AppliedGroup // 空列表是 [] 不是 nil
}

// EmptyGraph 是 revision 0 的空图画布。空图画布合法，可运行编辑、撤销和配方预览；CreateEmpty 持久化它而不是用 no-op ChangeSet 出生。
var EmptyGraph = AppliedGraph{Revision: 0, Nodes: []AppliedNode{}, Edges: []AppliedEdge{}, Groups: []AppliedGroup{}}

// Operation 是 Graph Command 封闭 op 表中的一条；JSON op 名见 [GraphCommandOpNames]。
type Operation interface {
	graphOp()
}

// CreateNodeOp 创建节点。公开 ChangeSet 不得携带 DocumentOrigin；该字段只给生成回写与历史还原。
type CreateNodeOp struct {
	ClientRef    string // Apply 里的临时 id，持久化时 assignPersistentIDs
	NodeType     NodeType
	Title        string
	PositionX    int            // 画布像素坐标
	PositionY    int            // 画布像素坐标
	Config       map[string]any // Catalog 登记的可见配置
	BoundAssetID *string
	GroupRef     *string // 已有 group id 或同 ChangeSet 的 client_ref；nil 表示不入组
	// DocumentOrigin 是服务端元数据，用于生成内容回写与历史还原。公开 ChangeSet 不得提供它。
	DocumentOrigin *string
}

func (CreateNodeOp) graphOp() {}

// UpdateNodeConfigOp 合并节点 config。未登记或退役 plan key 会被 Catalog 拒绝。
type UpdateNodeConfigOp struct {
	NodeRef      string         // 已有 id 或同 ChangeSet 的 client_ref
	Config       map[string]any // 与现有 config 合并；未登记 key 会被 Catalog 拒绝
	BoundAssetID *string
	// BoundAssetIDSet 为 true 时才写入 BoundAssetID，从而区分「不改绑定」和「清空绑定」。
	BoundAssetIDSet bool
	// DocumentOrigin 是服务端元数据，用于生成内容回写与历史还原。公开 ChangeSet 不得提供它。
	DocumentOrigin *string
}

func (UpdateNodeConfigOp) graphOp() {}

// RenameNodeOp 对应 JSON op rename_node：只改 workflow_graph_nodes.title，不影响 input digest，也不改绑定或 config。
// NodeRef 可以是已有 id 或同 ChangeSet 的 client_ref。JSON 解析空标题 Validation；Apply 只 TrimSpace。不要用 UpdateNodeConfigOp 改标题。
type RenameNodeOp struct {
	NodeRef string // 已有 id 或同 ChangeSet 的 client_ref
	Title   string
}

func (RenameNodeOp) graphOp() {}

// DeleteNodeOp 对应 JSON op delete_node：删节点及其所有关联边（入边+出边）；分组行仍在，只是该成员消失。
// 不删 ProductImageAsset。公开 ChangeSet 与 Agent tool 共用。禁区：不要只删节点行而留下边——Apply 必须先清 incident edges。
type DeleteNodeOp struct {
	NodeRef string // 已有 id 或同 ChangeSet 的 client_ref
}

func (DeleteNodeOp) graphOp() {}

// ConnectNodesOp 按 Catalog accepts 推断 DataType 与 Role；公开 payload 不带 role。
type ConnectNodesOp struct {
	ClientRef string // 新边的临时 id
	SourceRef string // 源节点 id 或 client_ref
	TargetRef string // 目标节点 id 或 client_ref
	Order     int    // 同一目标同一 role 下的次序
}

func (ConnectNodesOp) graphOp() {}

// DisconnectEdgeOp 删除一条边。compiler 只读取入边，断开即从运行输入消失。
type DisconnectEdgeOp struct {
	EdgeRef string // 已有边 id 或同 ChangeSet 的 client_ref
}

func (DisconnectEdgeOp) graphOp() {}

// ReorderEdgesOp 调整同一目标节点同一 role 下多条边的 Order。
type ReorderEdgesOp struct {
	NodeRef  string   // 目标节点 id 或 client_ref
	Role     EdgeRole // 只重排该端口下的边
	EdgeRefs []string // 新次序，必须覆盖该 role 的全部边
}

func (ReorderEdgesOp) graphOp() {}

// NodeMove 是 MoveNodesOp 里一次画布位移（像素坐标），不改变拓扑、绑定或 digest。
// Ref 是节点 id 或 client_ref。不要把坐标写进 config JSON。
type NodeMove struct {
	Ref string // 节点 id 或 client_ref
	X   int    // 画布像素
	Y   int    // 画布像素
}

// MoveNodesOp 对应 JSON op move_nodes：批量更新 workflow_graph_nodes.position_x/y。
// 不碰边、分组或 digest。HTTP 画布拖拽走此 op。找不到 Ref 返回 Validation。
type MoveNodesOp struct {
	Nodes []NodeMove // 批量位移；找不到 Ref 返回 Validation
}

func (MoveNodesOp) graphOp() {}

// CreateGroupOp 对应 JSON op create_group：创建一层视觉分组并把 MemberRefs 的 GroupID 指过来。
// ClientRef 在 Apply 里当临时 id，持久化时 assignPersistentIDs。禁止嵌套分组。不要当成创建图库文件夹。
type CreateGroupOp struct {
	ClientRef  string // Apply 里的临时 id，持久化时 assignPersistentIDs
	Title      string
	MemberRefs []string // 节点 id 或 client_ref；禁止嵌套分组
}

func (CreateGroupOp) graphOp() {}

// MoveNodesToGroupOp 把节点移入分组；GroupRef 为 nil 表示移出分组。
type MoveNodesToGroupOp struct {
	// GroupRef 为 nil 表示移出分组。
	GroupRef *string
	NodeRefs []string // 节点 id 或 client_ref
}

func (MoveNodesToGroupOp) graphOp() {}

// RenameGroupOp 对应 JSON op rename_group：只改 workflow_graph_groups.title，不移动成员、不影响 digest。
// 找不到分组返回 Validation。不要和 RenameNodeOp 搞混。
type RenameGroupOp struct {
	GroupRef string // 已有 group id 或同 ChangeSet 的 client_ref
	Title    string
}

func (RenameGroupOp) graphOp() {}

// DissolveGroupOp 对应 JSON op dissolve_group：删除分组行，并把成员 AppliedNode.GroupID 置 nil，节点留在画布上。
// 不删节点或边。不要实现成删除组内节点。
type DissolveGroupOp struct {
	GroupRef string // 已有 group id 或同 ChangeSet 的 client_ref
}

func (DissolveGroupOp) graphOp() {}

// GraphCommandOpNames 是 schema-v3 ChangeSet 的封闭 op 表。Agent tool JSON Schema 必须与此对齐。
var GraphCommandOpNames = generatedGraphCommandOpNames

// ChangeSet 是一次可逆图命令。Apply 成功后 [Invert] 生成 inverse，供 Undo/Redo。
type ChangeSet struct {
	BaseGraphRevision int         // 客户端观察到的 live revision。拓扑/改名/移动必须精确匹配。仅含 update_node_config 且其后历史只改了其它节点时，mutate 会 rebase 到当前 revision。
	Summary           string      // 历史摘要，给撤销栈展示
	ActorType         ActorType   // user|agent|recipe|system；HTTP 固定 ActorUser
	Operations        []Operation // 封闭 op 表；空列表是 [] 不是 nil
}

// CommandResult 是 WriteTx 写入后的图身份与已应用快照。
type CommandResult struct {
	GraphID          string
	ProductID        string
	Title            string
	Active           bool         // WriteTx 写入后的 active 位
	SchemaVersion    int          // 在线图固定为 3
	Revision         int          // 应用后的 live revision
	Applied          AppliedGraph // 写入后的内存快照，不是 HTTP Projection
	OperationGroupID string
	HistoryKind      HistoryKind // edit|undo|redo
}

// DirectCreateImageType 给 BuildDirectCreateTemplate / 直连创建与 Agent intake 展开：一种图种及其数量。
// Key 是图种闭集（如 hero）；Quantity 生成类为 1–6；evidence 类展开成未绑定 image_asset 占位。
// 不是 DB 行，来自创建表单/intake JSON。不要和 NodeType 搞混；ImageTypeKey 是生成后才写到商品图上的。
type DirectCreateImageType struct {
	Key         string // 图种闭集（如 hero），不是 NodeType
	Quantity    int    // 生成类为 1–6；evidence 类展开成未绑定 image_asset
	Order       int    // 套图展开次序
	Title       string
	AspectRatio string // 写入 generation_spec；缺省走图种默认
}

// DirectCreateInput 驱动 [BuildDirectCreateTemplate]：参考图绑定为 image_asset，生成类图种展开成 group + prompt + N 张图。
type DirectCreateInput struct {
	ImageTypes        []DirectCreateImageType // 按 Order 展开套图
	ReferenceAssetIDs []string                // ProductImageAsset id，绑定为 image_asset
	ProductTitle      string
	SourceProductID   *string
	FactSetVersionID  *string
	SourceNote        *string        // 商品说明，不是 CreativeBrief
	GenerationSpec    map[string]any // 复制到 image_generation
	TextSettings      map[string]any // 复制到 image_prompt；不属于模型生成参数
	DeliverySpec      map[string]any // 复制到 image_generation；不进 image digest
}
