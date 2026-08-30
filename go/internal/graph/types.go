// Package graph 实现 schema-v3 Graph Command：ChangeSet 应用、空图画布、撤销重做、Catalog 与 live 图 HTTP。
package graph

const (
	SchemaVersion      = 3
	CatalogVersion     = 7
	DefaultGraphTitle  = "商品创意工作流"
	maxImagePerType    = 6
	minImagePerType    = 1
	maxTotalImages     = 30
	maxReferenceAssets = 6
	maxRefLen          = 80
	maxTitleLen        = 255
	maxSummaryLen      = 500
)

type NodeType string

const (
	NodeProductSource    NodeType = "product_source"
	NodeImageAsset       NodeType = "image_asset"
	NodeCreativeBrief    NodeType = "creative_brief"
	NodeVisualSystem     NodeType = "visual_system"
	NodePromptGeneration NodeType = "prompt_generation"
	NodeImageGeneration  NodeType = "image_generation"
)

type EdgeDataType string

const (
	DataProductFacts  EdgeDataType = "product_facts"
	DataImageAsset    EdgeDataType = "image_asset"
	DataCreativeBrief EdgeDataType = "creative_brief"
	DataVisualSystem  EdgeDataType = "visual_system"
	DataPrompt        EdgeDataType = "prompt"
)

type EdgeRole string

const (
	RoleFacts          EdgeRole = "facts"
	RoleReference      EdgeRole = "reference"
	RoleBrief          EdgeRole = "brief"
	RoleVisualGuidance EdgeRole = "visual_guidance"
	RolePrompt         EdgeRole = "prompt"
)

type ConfigStatus string

const (
	ConfigIncomplete ConfigStatus = "incomplete"
	ConfigReady      ConfigStatus = "ready"
	ConfigStale      ConfigStatus = "stale"
)

type ActorType string

const (
	ActorUser   ActorType = "user"
	ActorAgent  ActorType = "agent"
	ActorRecipe ActorType = "recipe"
	ActorSystem ActorType = "system"
)

type HistoryKind string

const (
	HistoryEdit HistoryKind = "edit"
	HistoryUndo HistoryKind = "undo"
	HistoryRedo HistoryKind = "redo"
)

type AppliedNode struct {
	ID             string
	NodeType       NodeType
	Title          string
	PositionX      int
	PositionY      int
	Config         map[string]any
	BoundAssetID   *string
	GroupID        *string
	DocumentOrigin string
}

type AppliedEdge struct {
	ID           string
	SourceNodeID string
	TargetNodeID string
	DataType     EdgeDataType
	Role         EdgeRole
	Order        int
}

type AppliedGroup struct {
	ID    string
	Title string
}

type AppliedGraph struct {
	Revision int
	Nodes    []AppliedNode
	Edges    []AppliedEdge
	Groups   []AppliedGroup
}

var EmptyGraph = AppliedGraph{Revision: 0, Nodes: []AppliedNode{}, Edges: []AppliedEdge{}, Groups: []AppliedGroup{}}

type Operation interface {
	graphOp()
}

type CreateNodeOp struct {
	ClientRef    string
	NodeType     NodeType
	Title        string
	PositionX    int
	PositionY    int
	Config       map[string]any
	BoundAssetID *string
	GroupRef     *string
	// DocumentOrigin is server-side metadata used by generated-content writeback
	// and history restore. Public ChangeSets must not provide it.
	DocumentOrigin *string
}

func (CreateNodeOp) graphOp() {}

type UpdateNodeConfigOp struct {
	NodeRef         string
	Config          map[string]any
	BoundAssetID    *string
	BoundAssetIDSet bool
	// DocumentOrigin is server-side metadata used by generated-content writeback
	// and history restore. Public ChangeSets must not provide it.
	DocumentOrigin *string
}

func (UpdateNodeConfigOp) graphOp() {}

type RenameNodeOp struct {
	NodeRef string
	Title   string
}

func (RenameNodeOp) graphOp() {}

type DeleteNodeOp struct {
	NodeRef string
}

func (DeleteNodeOp) graphOp() {}

type ConnectNodesOp struct {
	ClientRef string
	SourceRef string
	TargetRef string
	Order     int
}

func (ConnectNodesOp) graphOp() {}

type DisconnectEdgeOp struct {
	EdgeRef string
}

func (DisconnectEdgeOp) graphOp() {}

type ReorderEdgesOp struct {
	NodeRef  string
	Role     EdgeRole
	EdgeRefs []string
}

func (ReorderEdgesOp) graphOp() {}

type NodeMove struct {
	Ref string
	X   int
	Y   int
}

type MoveNodesOp struct {
	Nodes []NodeMove
}

func (MoveNodesOp) graphOp() {}

type CreateGroupOp struct {
	ClientRef  string
	Title      string
	MemberRefs []string
}

func (CreateGroupOp) graphOp() {}

type MoveNodesToGroupOp struct {
	GroupRef *string
	NodeRefs []string
}

func (MoveNodesToGroupOp) graphOp() {}

type RenameGroupOp struct {
	GroupRef string
	Title    string
}

func (RenameGroupOp) graphOp() {}

type DissolveGroupOp struct {
	GroupRef string
}

func (DissolveGroupOp) graphOp() {}

// GraphCommandOpNames 是 schema-v3 ChangeSet 的封闭 op 表。Agent tool JSON Schema 必须与此对齐。
var GraphCommandOpNames = generatedGraphCommandOpNames

type ChangeSet struct {
	BaseGraphRevision int
	Summary           string
	ActorType         ActorType
	Operations        []Operation
}

type CommandResult struct {
	GraphID          string
	ProductID        string
	Title            string
	Active           bool
	SchemaVersion    int
	Revision         int
	Applied          AppliedGraph
	OperationGroupID string
	HistoryKind      HistoryKind
}

type DirectCreateImageType struct {
	Key         string
	Quantity    int
	Order       int
	Title       string
	AspectRatio string
}

type DirectCreateInput struct {
	ImageTypes        []DirectCreateImageType
	ReferenceAssetIDs []string
	ProductTitle      string
	SourceProductID   *string
	FactSetVersionID  *string
	SourceNote        *string
	GenerationSpec    map[string]any
	DeliverySpec      map[string]any
}
