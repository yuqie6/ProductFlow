package recipe

import (
	"time"

	"github.com/yuqie6/productflow/internal/platform/apperr"
)

// RecipeView 是配方及其当前版本的 HTTP 投影。
// Get 带 Versions 历史；List 的摘要不含 Versions。ArchivedAt 非 nil 表示已归档，默认列表不出现。
// 不要和全局图库、收藏画廊搞混——配方不存媒体 bytes 或商品身份。
type RecipeView struct {
	ID               string        `json:"id"`
	Kind             string        `json:"kind"`         // workflow_recipe | recipe_fragment
	Origin           string        `json:"origin"`       // user | official
	OfficialKey      *string       `json:"official_key"` // 用户配方为 nil
	CurrentVersionID string        `json:"current_version_id"`
	CurrentVersion   VersionView   `json:"current_version"` // Get/List 都带当前版本；List 不含 Versions 历史
	ArchivedAt       *time.Time    `json:"archived_at"`
	CreatedAt        time.Time     `json:"created_at"`
	UpdatedAt        time.Time     `json:"updated_at"`
	Versions         []VersionView `json:"versions,omitempty"` // List 摘要不含此项
}

// VersionView 是一版配方 payload；不含商品身份、绑定、生成结果或媒体 bytes。
type VersionView struct {
	ID                             string         `json:"id"`
	RecipeID                       string         `json:"recipe_id"`
	Version                        int            `json:"version"`         // 从 1 起
	SchemaVersion                  int            `json:"schema_version"`  // 当前为 3，与 live 图一致
	CatalogVersion                 int            `json:"catalog_version"` // 提取时的节点 Catalog 版本
	CreationSource                 string         `json:"creation_source"` // 当前为 user_extract
	Title                          string         `json:"title"`
	Description                    *string        `json:"description"`  // nil 表示未写说明
	Payload                        map[string]any `json:"payload"`      // 不含商品身份、绑定、生成结果或媒体 bytes
	PayloadHash                    string         `json:"payload_hash"` // payload 的 canonjson SHA256
	Governance                     any            `json:"governance"`   // 无治理元数据时为 null
	PreferredVisualSystemVersionID *string        `json:"preferred_visual_system_version_id"`
	CreatedAt                      time.Time      `json:"created_at"`
}

// ArchiveView 是 DELETE 归档的 HTTP 结果。
// Changed=false 表示本来就已归档，本次幂等成功，不是失败。
type ArchiveView struct {
	Changed bool       `json:"changed"` // false 表示本来就已归档，本次幂等成功
	Recipe  RecipeView `json:"recipe"`  // 归档后的配方投影
}

// PreviewView 是应用到目标商品前的 HTTP 预览，还不改 live 图。
// Mode 为 create（空画布）或 merge（fragment）。PreviewDigest 必须原样交给 Apply。
type PreviewView struct {
	Mode              string               `json:"mode"` // create | merge
	RecipeID          string               `json:"recipe_id"`
	RecipeVersion     int                  `json:"recipe_version"`      // 预览所用配方版本
	BaseGraphRevision int                  `json:"base_graph_revision"` // 目标 live 图当前 revision；空画布为 0
	PreviewDigest     string               `json:"preview_digest"`      // Apply 必须原样带回，否则 409
	Nodes             []PreviewNode        `json:"nodes"`               // 将新增的节点，Key 还不是 live id
	Edges             []PreviewEdge        `json:"edges"`               // 将新增的边，端点仍是 Key
	Groups            []PreviewGroup       `json:"groups"`              // 将新增的一层视觉分组
	UpdatedNodes      []PreviewUpdatedNode `json:"updated_nodes"`       // create 模式应为 []
	RequiredBindings  []string             `json:"required_bindings"`   // 如 product_identity；目标图必须满足
}

// ApplicationView 是配方 Apply 确认后的 HTTP 投影。
// Created=false 表示相同幂等键已应用过，回放上次结果。Graph 是目标商品 live 图投影。
type ApplicationView struct {
	Created           bool     `json:"created"` // false 表示相同幂等键已应用，回放上次结果
	RecipeID          string   `json:"recipe_id"`
	RecipeVersionID   string   `json:"recipe_version_id"`
	RecipeVersion     int      `json:"recipe_version"`      // 实际应用的配方版本
	Mode              string   `json:"mode"`                // create | merge
	Graph             any      `json:"graph"`               // 目标商品 live 图投影
	AddedNodeIDs      []string `json:"added_node_ids"`      // 本次写入的 live 节点
	AddedEdgeIDs      []string `json:"added_edge_ids"`      // 本次写入的 live 边
	UpdatedNodeIDs    []string `json:"updated_node_ids"`    // create 模式应为 []
	BaseGraphRevision *int     `json:"base_graph_revision"` // 预览时的图 revision
	PreviewDigest     *string  `json:"preview_digest"`      // 与 Preview 相同的 digest
	RequiredBindings  []string `json:"required_bindings"`   // 如 product_identity
}

// serializeVersion 校验 payload hash 后再投影。hash 对不上返回 409，不要把脏 JSON 给前端。
func serializeVersion(ver versionRecord) (VersionView, error) {
	payload, err := parsePayloadOrRaise(ver.PayloadJSON, ver.PayloadHash)
	if err != nil {
		return VersionView{}, err
	}
	var governance any
	if len(ver.GovernanceJSON) > 0 && string(ver.GovernanceJSON) != "null" {
		var parsed any
		if err := decodeStrict(ver.GovernanceJSON, &parsed); err != nil {
			return VersionView{}, apperr.Conflict("工作流配方治理元数据无效")
		}
		if _, err := parseGovernance(ver.GovernanceJSON); err != nil {
			return VersionView{}, err
		}
		governance = parsed
	}
	return VersionView{
		ID:                             ver.ID,
		RecipeID:                       ver.RecipeID,
		Version:                        ver.Version,
		SchemaVersion:                  ver.SchemaVersion,
		CatalogVersion:                 ver.CatalogVersion,
		CreationSource:                 ver.CreationSource,
		Title:                          ver.Title,
		Description:                    ver.Description,
		Payload:                        payloadDict(payload),
		PayloadHash:                    ver.PayloadHash,
		Governance:                     governance,
		PreferredVisualSystemVersionID: ver.PreferredVisualSystemVersionID,
		CreatedAt:                      ver.CreatedAt,
	}, nil
}

// serializeSummary 只投影当前版本，不带历史 Versions。缺 current 返回 409，列表不能出半残行。
func serializeSummary(rec recipeRecord) (RecipeView, error) {
	if rec.Current == nil || rec.CurrentVersionID == nil {
		return RecipeView{}, apperr.Conflict("工作流配方缺少 current version")
	}
	current, err := serializeVersion(*rec.Current)
	if err != nil {
		return RecipeView{}, err
	}
	return RecipeView{
		ID:               rec.ID,
		Kind:             rec.Kind,
		Origin:           rec.Origin,
		OfficialKey:      rec.OfficialKey,
		CurrentVersionID: *rec.CurrentVersionID,
		CurrentVersion:   current,
		ArchivedAt:       rec.ArchivedAt,
		CreatedAt:        rec.CreatedAt,
		UpdatedAt:        rec.UpdatedAt,
	}, nil
}

func serializeRecipe(rec recipeRecord) (RecipeView, error) {
	view, err := serializeSummary(rec)
	if err != nil {
		return RecipeView{}, err
	}
	versions := make([]VersionView, 0, len(rec.Versions))
	for _, ver := range rec.Versions {
		item, err := serializeVersion(ver)
		if err != nil {
			return RecipeView{}, err
		}
		versions = append(versions, item)
	}
	view.Versions = versions
	return view, nil
}

// serializePreview 把 nil 切片收成空数组。前端按数组渲染，不要输出 null。
func serializePreview(p Preview) PreviewView {
	nodes := p.Nodes
	if nodes == nil {
		nodes = []PreviewNode{}
	}
	edges := p.Edges
	if edges == nil {
		edges = []PreviewEdge{}
	}
	groups := p.Groups
	if groups == nil {
		groups = []PreviewGroup{}
	}
	updated := p.UpdatedNodes
	if updated == nil {
		updated = []PreviewUpdatedNode{}
	}
	bindings := p.RequiredBindings
	if bindings == nil {
		bindings = []string{}
	}
	return PreviewView{
		Mode:              p.Mode,
		RecipeID:          p.RecipeID,
		RecipeVersion:     p.RecipeVersion,
		BaseGraphRevision: p.BaseGraphRevision,
		PreviewDigest:     p.PreviewDigest,
		Nodes:             nodes,
		Edges:             edges,
		Groups:            groups,
		UpdatedNodes:      updated,
		RequiredBindings:  bindings,
	}
}

func serializeApplication(result ApplicationResult) ApplicationView {
	return ApplicationView{
		Created:           result.Created,
		RecipeID:          result.RecipeID,
		RecipeVersionID:   result.RecipeVersionID,
		RecipeVersion:     result.RecipeVersion,
		Mode:              result.Mode,
		Graph:             result.Graph,
		AddedNodeIDs:      nonNilStrings(result.AddedNodeIDs),
		AddedEdgeIDs:      nonNilStrings(result.AddedEdgeIDs),
		UpdatedNodeIDs:    nonNilStrings(result.UpdatedNodeIDs),
		BaseGraphRevision: result.BaseGraphRevision,
		PreviewDigest:     result.PreviewDigest,
		RequiredBindings:  nonNilStrings(result.RequiredBindings),
	}
}
