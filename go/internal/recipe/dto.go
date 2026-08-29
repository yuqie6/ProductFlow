package recipe

import (
	"time"

	"github.com/yuqie6/productflow/internal/platform/apperr"
)

type RecipeView struct {
	ID               string        `json:"id"`
	Kind             string        `json:"kind"`
	Origin           string        `json:"origin"`
	OfficialKey      *string       `json:"official_key"`
	CurrentVersionID string        `json:"current_version_id"`
	CurrentVersion   VersionView   `json:"current_version"`
	ArchivedAt       *time.Time    `json:"archived_at"`
	CreatedAt        time.Time     `json:"created_at"`
	UpdatedAt        time.Time     `json:"updated_at"`
	Versions         []VersionView `json:"versions,omitempty"`
}

type VersionView struct {
	ID                             string         `json:"id"`
	RecipeID                       string         `json:"recipe_id"`
	Version                        int            `json:"version"`
	SchemaVersion                  int            `json:"schema_version"`
	CatalogVersion                 int            `json:"catalog_version"`
	CreationSource                 string         `json:"creation_source"`
	Title                          string         `json:"title"`
	Description                    *string        `json:"description"`
	Payload                        map[string]any `json:"payload"`
	PayloadHash                    string         `json:"payload_hash"`
	Governance                     any            `json:"governance"`
	PreferredVisualSystemVersionID *string        `json:"preferred_visual_system_version_id"`
	CreatedAt                      time.Time      `json:"created_at"`
}

type ArchiveView struct {
	Changed bool       `json:"changed"`
	Recipe  RecipeView `json:"recipe"`
}

type PreviewView struct {
	Mode              string               `json:"mode"`
	RecipeID          string               `json:"recipe_id"`
	RecipeVersion     int                  `json:"recipe_version"`
	BaseGraphRevision int                  `json:"base_graph_revision"`
	PreviewDigest     string               `json:"preview_digest"`
	Nodes             []PreviewNode        `json:"nodes"`
	Edges             []PreviewEdge        `json:"edges"`
	Groups            []PreviewGroup       `json:"groups"`
	UpdatedNodes      []PreviewUpdatedNode `json:"updated_nodes"`
	RequiredBindings  []string             `json:"required_bindings"`
}

type ApplicationView struct {
	Created           bool     `json:"created"`
	RecipeID          string   `json:"recipe_id"`
	RecipeVersionID   string   `json:"recipe_version_id"`
	RecipeVersion     int      `json:"recipe_version"`
	Mode              string   `json:"mode"`
	Graph             any      `json:"graph"`
	AddedNodeIDs      []string `json:"added_node_ids"`
	AddedEdgeIDs      []string `json:"added_edge_ids"`
	UpdatedNodeIDs    []string `json:"updated_node_ids"`
	BaseGraphRevision *int     `json:"base_graph_revision"`
	PreviewDigest     *string  `json:"preview_digest"`
	RequiredBindings  []string `json:"required_bindings"`
}

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
