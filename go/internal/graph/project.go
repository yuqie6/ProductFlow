package graph

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"gorm.io/gorm"
)

// Projection 是 live 图画布 HTTP 合同，含撤销栈与 pending 提案。
type Projection struct {
	ID                    string      `json:"id"`
	ProductID             string      `json:"product_id"`
	Title                 string      `json:"title"`
	SchemaVersion         int         `json:"schema_version"` // 在线图固定为 3
	Revision              int         `json:"revision"`       // 每次成功 ChangeSet 递增
	SourceDraftRevisionID *string     `json:"source_draft_revision_id"`
	LastOperationGroupID  *string     `json:"last_operation_group_id"`
	CanUndo               bool        `json:"can_undo"` // 栈顶不是 Undo 时为 true
	CanRedo               bool        `json:"can_redo"` // 仅当栈顶 HistoryKind=undo
	Nodes                 []NodeView  `json:"nodes"`    // 空列表是 [] 不是 nil
	Edges                 []EdgeView  `json:"edges"`    // 空列表是 [] 不是 nil
	Groups                []GroupView `json:"groups"`   // 空列表是 [] 不是 nil
	// PendingProposal 为 nil 表示没有 PENDING 提案。
	PendingProposal *ProposalView `json:"pending_proposal"`
}

// NodeView 是画布上一个节点的投影。BoundAssetID 是 ProductImageAsset id。
type NodeView struct {
	ID string `json:"id"`
	// NodeType 是 schema-v3 闭集，不是商品图种 image_type_key。
	NodeType       NodeType         `json:"node_type"`
	Title          string           `json:"title"`
	PositionX      int              `json:"position_x"`       // 画布像素坐标
	PositionY      int              `json:"position_y"`       // 画布像素坐标
	Config         map[string]any   `json:"config"`           // Catalog 登记的可见配置
	SourceProduct  *productSummary  `json:"source_product"`   // 仅 product_source
	ProductFactSet *factSetSnapshot `json:"product_fact_set"` // 仅 product_source
	BoundAssetID   *string          `json:"bound_asset_id"`
	GroupID        *string          `json:"group_id"`
	PreviewAssetID *string          `json:"preview_asset_id"`
	// ConfigStatus 为 incomplete|ready|stale；stale 表示产物 digest 已落后。
	ConfigStatus ConfigStatus `json:"config_status"`
	Unused       bool         `json:"unused"` // 仅 image_asset：未连出边时为 true
	// DocumentOrigin 是内容节点 seed|generated|authored|collaborative；非内容节点为 nil。
	DocumentOrigin *string `json:"document_origin"`
	// BindingStatus 仅 image_asset：bound|unbound；其他类型省略。
	BindingStatus              *string         `json:"binding_status,omitempty"`
	CurrentArtifactID          *string         `json:"current_artifact_id"`
	CurrentArtifactType        *string         `json:"current_artifact_type"`    // 如 image；无产物为 nil
	CurrentArtifactPayload     map[string]any  `json:"current_artifact_payload"` // 无产物为空 map
	PendingCandidateArtifactID *string         `json:"pending_candidate_artifact_id"`
	Incoming                   []EdgeSummary   `json:"incoming"` // 空列表是 [] 不是 nil
	Outgoing                   []EdgeSummary   `json:"outgoing"` // 空列表是 [] 不是 nil
	ImageInput                 *ImageInputView `json:"image_input,omitempty"`
}

type ImageInputView struct {
	Prompt                map[string]any `json:"prompt"`
	TextSettings          map[string]any `json:"text_settings"`
	InheritedPrompt       map[string]any `json:"inherited_prompt"`
	InheritedTextSettings map[string]any `json:"inherited_text_settings"`
}

// EdgeSummary 是 NodeView.incoming / outgoing 里的短边，给检查器画端口用，不是整图边列表。
// NodeID 是对端节点：入边为 source，出边为 target。Role 等于 React Flow handle id。
// 改字段会碰到画布 HTTP 投影；完整 source+target 请看 EdgeView。不要持久化本结构。
type EdgeSummary struct {
	ID       string       `json:"id"`
	NodeID   string       `json:"node_id"`
	DataType EdgeDataType `json:"data_type"`
	Role     EdgeRole     `json:"role"`  // 等于 React Flow handle id
	Order    int          `json:"order"` // 同一目标同一 role 下的次序
}

// EdgeView 是画布 HTTP 投影里一条完整边，对应 workflow_graph_edges。
// Role 由 Catalog 在 Connect 时写入，公开 ChangeSet 不得带 role。Order 是同一目标同一 role 下的次序。
// 不要和 EdgeSummary（缺一端）或 AppliedEdge（内存 Apply 快照、无 JSON tag）搞混。
type EdgeView struct {
	ID           string       `json:"id"`
	SourceNodeID string       `json:"source_node_id"`
	TargetNodeID string       `json:"target_node_id"`
	DataType     EdgeDataType `json:"data_type"`
	Role         EdgeRole     `json:"role"`  // 由 Catalog 在 Connect 时写入
	Order        int          `json:"order"` // 同一目标同一 role 下的次序
}

// GroupView 是画布 HTTP 投影的一层视觉分组，MemberIDs 由节点 GroupID 反推，不是单独成员表。
// 分组没有端口、运行或嵌套。对应 workflow_graph_groups。不要和商品图库 GalleryFolder 或 AppliedGroup 搞混。
type GroupView struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	MemberIDs []string `json:"member_ids"` // 由节点 GroupID 反推；空列表是 [] 不是 nil
}

// ProposalView 是 PENDING 提案相对当前 revision 的预览；Stale 表示 base 已落后。
type ProposalView struct {
	ID                string             `json:"id"`
	Summary           string             `json:"summary"`
	BaseGraphRevision int                `json:"base_graph_revision"` // 提案相对的 live revision
	Stale             bool               `json:"stale"`               // base 已落后，确认前须处理冲突
	AddedNodes        []ProposalNodeView `json:"added_nodes"`         // 空列表是 [] 不是 nil
	AddedEdges        []ProposalEdgeView `json:"added_edges"`         // 空列表是 [] 不是 nil
	DeletedNodeIDs    []string           `json:"deleted_node_ids"`    // 将删除的 live 节点 id
	DeletedEdgeIDs    []string           `json:"deleted_edge_ids"`    // 将删除的 live 边 id
	ChangedNodeIDs    []string           `json:"changed_node_ids"`    // 配置将改的已有节点 id
}

// ProposalNodeView 是 PENDING 提案预览里将新增的节点草稿，ID 是提案 ChangeSet 的 client_ref，尚未 assignPersistentIDs。
// 出现在 Projection.pending_proposal.added_nodes。确认前不写 workflow_graph_nodes。
// 不要当成已落库的 NodeView；本结构没有 BoundAssetID / ConfigStatus。
type ProposalNodeView struct {
	ID        string         `json:"id"`
	NodeType  NodeType       `json:"node_type"`
	Title     string         `json:"title"`
	PositionX int            `json:"position_x"` // 画布像素坐标
	PositionY int            `json:"position_y"` // 画布像素坐标
	GroupID   *string        `json:"group_id"`
	Config    map[string]any `json:"config"`
}

// ProposalEdgeView 是 PENDING 提案预览里将新增的边草稿；Role/DataType 是字符串，来自内存 Apply 推断结果。
// 确认前不写 workflow_graph_edges。不要和 EdgeView（已落库）或 ConnectNodesOp（公开 payload 不带 role）搞混。
type ProposalEdgeView struct {
	ID           string `json:"id"`
	SourceNodeID string `json:"source_node_id"`
	TargetNodeID string `json:"target_node_id"`
	Role         string `json:"role"`      // 内存 Apply 推断结果，尚未落库
	DataType     string `json:"data_type"` // 内存 Apply 推断结果，尚未落库
	Order        int    `json:"order"`
}

// Project 把 live 图行展开成画布 HTTP 投影，供 GET current/get 与改图成功后的 200 体。
// 读 workflow_graphs、节点/边/分组、workflow_operation_groups、artifacts 与 pending 提案；不写库。
// ctx 必须带 ProductGuard，否则绑图元数据会 Internal。缺图由调用方 loadGraph 先 NotFound。
// CanRedo 仅当栈顶 HistoryKind=undo。不要把返回值当成 AppliedGraph。
func Project(ctx context.Context, tx *gorm.DB, id Identity) (Projection, error) {
	row := graphRow{Identity: id}
	applied, err := loadAppliedGraph(ctx, tx, row)
	if err != nil {
		return Projection{}, err
	}
	last, err := lastOperationGroup(ctx, tx, row)
	if err != nil {
		return Projection{}, err
	}
	sources, previews, artifactDigests, pendingCandidates, err := loadGraphSources(ctx, tx, row, applied)
	if err != nil {
		return Projection{}, err
	}
	proposal, err := pendingProposalView(ctx, tx, row, applied)
	if err != nil {
		return Projection{}, err
	}
	var lastID *string
	canUndo, canRedo := false, false
	if last != nil {
		id := last.ID
		lastID = &id
		canUndo = last.HistoryKind != HistoryUndo
		canRedo = last.HistoryKind == HistoryUndo
	}
	return buildProjection(row, applied, lastID, canUndo, canRedo, previews, artifactDigests, pendingCandidates, sources, proposal), nil
}

// buildProjection 把 live 行+源+提案收成画布 HTTP 合同。未连出的 image_asset 标 unused，绑定仍不等于 reference 边。
// stale 走 configStatusWithStale（generated 文稿跳过 adopt 后自比）。不写库。
func buildProjection(
	row graphRow,
	applied AppliedGraph,
	lastID *string,
	canUndo, canRedo bool,
	previews map[string]string,
	artifactDigests map[string]string,
	pendingCandidates map[string]string,
	sources map[string]SourceRecord,
	proposal *ProposalView,
) Projection {
	outgoingIDs := map[string]struct{}{}
	for _, edge := range applied.Edges {
		outgoingIDs[edge.SourceNodeID] = struct{}{}
	}
	memberIDs := map[string][]string{}
	for _, group := range applied.Groups {
		memberIDs[group.ID] = []string{}
	}
	nodes := make([]NodeView, 0, len(applied.Nodes))
	for _, node := range applied.Nodes {
		if node.GroupID != nil {
			memberIDs[*node.GroupID] = append(memberIDs[*node.GroupID], node.ID)
		}
		unused := false
		if node.NodeType == NodeImageAsset {
			_, connected := outgoingIDs[node.ID]
			unused = !connected
		}
		var preview *string
		if id, ok := previews[node.ID]; ok {
			preview = &id
		}
		record := sources[node.ID]
		var sourceProduct *productSummary
		var factSet *factSetSnapshot
		if node.NodeType == NodeProductSource && record.ProductSource != nil {
			sourceProduct = record.ProductSource.SourceProduct
			factSet = record.ProductSource.FactSetVersion
		}
		incoming := []EdgeSummary{}
		outgoing := []EdgeSummary{}
		for _, edge := range applied.Edges {
			if edge.TargetNodeID == node.ID {
				incoming = append(incoming, EdgeSummary{edge.ID, edge.SourceNodeID, edge.DataType, edge.Role, edge.Order})
			}
			if edge.SourceNodeID == node.ID {
				outgoing = append(outgoing, EdgeSummary{edge.ID, edge.TargetNodeID, edge.DataType, edge.Role, edge.Order})
			}
		}
		status, err := applied.ConfigStatus(node.ID)
		if err != nil {
			status = ConfigIncomplete
		}
		status = configStatusWithStale(applied, node, artifactDigests[node.ID], sources, status)
		var binding *string
		var imageInput *ImageInputView
		if node.NodeType == NodeImageGeneration {
			if prompt, spec, _, err := resolveImageDocument(applied, node.ID, sources); err == nil {
				imageInput = &ImageInputView{Prompt: prompt, TextSettings: map[string]any{"policy": spec["text_policy"], "language": spec["text_language"]}}
				inherited := applied
				inherited.Nodes = append([]AppliedNode(nil), applied.Nodes...)
				for index, candidate := range inherited.Nodes {
					if candidate.ID == node.ID {
						candidate.Config = cloneMap(candidate.Config)
						delete(candidate.Config, "prompt_overrides")
						delete(candidate.Config, "text_override")
						inherited.Nodes[index] = candidate
						break
					}
				}
				base, baseSpec, _, _ := resolveImageDocument(inherited, node.ID, sources)
				imageInput.InheritedPrompt = base
				imageInput.InheritedTextSettings = map[string]any{"policy": baseSpec["text_policy"], "language": baseSpec["text_language"]}
			}
		}
		if node.NodeType == NodeImageAsset {
			if node.BoundAssetID != nil && *node.BoundAssetID != "" {
				bound := "bound"
				binding = &bound
			} else {
				unbound := "unbound"
				binding = &unbound
			}
		}
		nodes = append(nodes, NodeView{
			ID:                         node.ID,
			NodeType:                   node.NodeType,
			Title:                      node.Title,
			PositionX:                  node.PositionX,
			PositionY:                  node.PositionY,
			Config:                     nonemptyMap(node.Config),
			SourceProduct:              sourceProduct,
			ProductFactSet:             factSet,
			BoundAssetID:               node.BoundAssetID,
			GroupID:                    node.GroupID,
			PreviewAssetID:             preview,
			ConfigStatus:               status,
			Unused:                     unused,
			DocumentOrigin:             documentOriginPtr(node),
			BindingStatus:              binding,
			CurrentArtifactID:          record.CurrentArtifactID,
			CurrentArtifactType:        record.CurrentArtifactType,
			CurrentArtifactPayload:     record.CurrentArtifactPayload,
			PendingCandidateArtifactID: stringMapPtr(pendingCandidates, node.ID),
			Incoming:                   incoming,
			Outgoing:                   outgoing,
			ImageInput:                 imageInput,
		})
	}
	edges := make([]EdgeView, 0, len(applied.Edges))
	for _, edge := range applied.Edges {
		edges = append(edges, EdgeView{edge.ID, edge.SourceNodeID, edge.TargetNodeID, edge.DataType, edge.Role, edge.Order})
	}
	groups := make([]GroupView, 0, len(applied.Groups))
	for _, group := range applied.Groups {
		members := memberIDs[group.ID]
		if members == nil {
			members = []string{}
		}
		groups = append(groups, GroupView{group.ID, group.Title, members})
	}
	return Projection{
		ID:                   row.ID,
		ProductID:            row.ProductID,
		Title:                row.Title,
		SchemaVersion:        row.SchemaVersion,
		Revision:             applied.Revision,
		LastOperationGroupID: lastID,
		CanUndo:              canUndo,
		CanRedo:              canRedo,
		Nodes:                nodes,
		Edges:                edges,
		Groups:               groups,
		PendingProposal:      proposal,
	}
}

// configStatusWithStale 在 Catalog 已判 ready 时再比 input digest。generated 文稿跳过，避免 adopt 后自我 stale。
// authored 文稿仍比 digest。digest 算失败保持原 status，不要改成 incomplete。
func configStatusWithStale(applied AppliedGraph, node AppliedNode, artifactDigest string, sources map[string]SourceRecord, status ConfigStatus) ConfigStatus {
	if status != ConfigReady || !IsProcessingNode(node.NodeType) || artifactDigest == "" || sources == nil {
		return status
	}
	// 内容产物的 digest 哈希的是 adopt 之前的文稿生成请求。adopt 必然改节点自身 config，
	// 若拿请求哈希去跟 adopt 后的 config 比，每次成功 cook 都会被标 stale。
	// generated 文稿因此跳过这次比较；authored 文稿仍走 digest，当前文档偏离时才标 stale。
	if isContentNodeType(node.NodeType) && DocumentOrigin(node) == OriginGenerated {
		return status
	}
	digest, err := compileInputDigest(applied, node.ID, sources)
	if err != nil {
		return status
	}
	if digest != artifactDigest {
		return ConfigStale
	}
	return status
}

// loadGraphSources 组装编译/投影用的 SourceRecord：商品 facts、绑定图元数据、current artifact digest。
// 节点行已经由 loadAppliedGraph 展开；商品、fact、绑定资产和视觉版本均按类型批量读取。
// Guard 缺商品/fact 返回空源，不报 NotFound。同时返回 preview 标题、digest、pending candidate 映射。
func loadGraphSources(ctx context.Context, tx *gorm.DB, row graphRow, applied AppliedGraph) (map[string]SourceRecord, map[string]string, map[string]string, map[string]string, error) {
	artifactIDs := make([]string, 0)
	boundAssetIDs := make([]string, 0)
	visualVersionIDs := make([]string, 0)
	for _, node := range applied.Nodes {
		if node.CurrentArtifactID != nil && *node.CurrentArtifactID != "" {
			artifactIDs = append(artifactIDs, *node.CurrentArtifactID)
		}
		if node.NodeType == NodeImageAsset && node.BoundAssetID != nil && *node.BoundAssetID != "" {
			boundAssetIDs = append(boundAssetIDs, *node.BoundAssetID)
		}
		if node.NodeType == NodeVisualSystem {
			if versionID, ok := node.Config["visual_system_version_id"].(string); ok && strings.TrimSpace(versionID) != "" {
				visualVersionIDs = append(visualVersionIDs, strings.TrimSpace(versionID))
			}
		}
	}
	artifactByID := map[string]schema.WorkflowGraphArtifacts{}
	if len(artifactIDs) > 0 {
		var artifactRecs []schema.WorkflowGraphArtifacts
		if err := tx.WithContext(ctx).Where("id IN ?", uniqueStrings(artifactIDs)).Find(&artifactRecs).Error; err != nil {
			return nil, nil, nil, nil, err
		}
		for _, a := range artifactRecs {
			artifactByID[a.ID] = a
		}
	}
	boundAssetMetas := map[string]BoundAssetMetadata{}
	if len(boundAssetIDs) > 0 {
		guard, err := requireProductGuard(ctx)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		boundAssetMetas, err = guard.BoundAssetMetas(ctx, tx, row.ProductID, uniqueStrings(boundAssetIDs))
		if err != nil {
			return nil, nil, nil, nil, err
		}
	}
	visualPayloads, err := loadVisualSystemPayloads(ctx, tx, uniqueStrings(visualVersionIDs))
	if err != nil {
		return nil, nil, nil, nil, err
	}
	productSources, err := loadProductSourceSnapshots(ctx, tx, row.ProductID, applied.Nodes)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	type nodeArtifact struct {
		boundAssetID  *string
		artifactID    *string
		artifactType  *string
		payload       []byte
		digest        *string
		outputAssetID *string
	}
	artifacts := map[string]nodeArtifact{}
	for _, node := range applied.Nodes {
		rec := nodeArtifact{boundAssetID: node.BoundAssetID}
		if node.NodeType == NodeImageGeneration {
			rec.artifactID = node.CurrentArtifactID
		}
		if node.CurrentArtifactID != nil {
			if artifact, ok := artifactByID[*node.CurrentArtifactID]; ok {
				digest := artifact.InputDigest
				rec.digest = &digest
				if rec.artifactID != nil {
					rec.artifactType = &artifact.ArtifactType
					rec.payload = []byte(artifact.PayloadJSON)
					rec.outputAssetID = artifact.ProductImageAssetID
				}
			}
		}
		artifacts[node.ID] = rec
	}

	previews := map[string]string{}
	digests := map[string]string{}
	pendingCandidates := map[string]string{}
	sources := map[string]SourceRecord{}
	for _, node := range applied.Nodes {
		rec := artifacts[node.ID]
		if node.PendingCandidateArtifactID != nil {
			pendingCandidates[node.ID] = *node.PendingCandidateArtifactID
		}
		if rec.boundAssetID != nil && *rec.boundAssetID != "" {
			previews[node.ID] = *rec.boundAssetID
		} else if rec.outputAssetID != nil && *rec.outputAssetID != "" {
			previews[node.ID] = *rec.outputAssetID
		}
		if rec.digest != nil && *rec.digest != "" {
			digests[node.ID] = *rec.digest
		}
		record := SourceRecord{}
		if rec.artifactID != nil {
			record.CurrentArtifactID = rec.artifactID
			record.CurrentArtifactType = rec.artifactType
			record.CurrentOutputAssetID = rec.outputAssetID
			record.CurrentInputDigest = rec.digest
			if len(rec.payload) > 0 {
				payload := map[string]any{}
				if err := json.Unmarshal(rec.payload, &payload); err == nil {
					record.CurrentArtifactPayload = payload
				}
			}
		}
		switch node.NodeType {
		case NodeProductSource:
			snap := productSources[node.ID]
			record.ProductSource = &snap
			record.Facts = snap.Facts
		case NodeCreativeBrief:
			record.Brief = cloneMap(node.Config)
		case NodeVisualSystem:
			versionID, _ := node.Config["visual_system_version_id"].(string)
			if strings.TrimSpace(versionID) != "" {
				vid := strings.TrimSpace(versionID)
				record.VisualSystemVersionID = &vid
				record.VisualPayload = visualPayloads[vid]
			}
			if record.VisualPayload == nil {
				record.VisualPayload = visualOverlayFromConfig(node.Config)
			}
		case NodeImagePrompt:
			if prompt, ok := node.Config["prompt"].(map[string]any); ok {
				record.PromptDocument = cloneMap(prompt)
			}
		case NodeImageAsset:
			record.BoundAssetID = node.BoundAssetID
			label := node.Title
			record.BoundAssetLabel = &label
			if node.BoundAssetID != nil {
				if metadata, ok := boundAssetMetas[*node.BoundAssetID]; ok {
					if metadata.DisplayName != "" {
						record.BoundAssetLabel = &metadata.DisplayName
					}
					if metadata.MIMEType != "" {
						record.BoundAssetMIME = &metadata.MIMEType
					}
				}
			}
		}
		sources[node.ID] = record
	}
	return sources, previews, digests, pendingCandidates, nil
}

func stringMapPtr(values map[string]string, key string) *string {
	value := values[key]
	if value == "" {
		return nil
	}
	return &value
}

func loadVisualSystemPayloads(ctx context.Context, tx *gorm.DB, versionIDs []string) (map[string]map[string]any, error) {
	out := map[string]map[string]any{}
	if len(versionIDs) == 0 {
		return out, nil
	}
	var recs []schema.VisualSystemVersions
	if err := tx.WithContext(ctx).
		Select("id, payload_json").
		Where("id IN ?", versionIDs).
		Find(&recs).Error; err != nil {
		return nil, err
	}
	for _, rec := range recs {
		payload := map[string]any{}
		if err := json.Unmarshal([]byte(rec.PayloadJSON), &payload); err != nil {
			return nil, err
		}
		out[rec.ID] = payload
	}
	return out, nil
}
