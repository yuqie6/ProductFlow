package graph

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"gorm.io/gorm"
)

// ArtifactDeliveryQualification 从 image artifact payload 解析采用硬闸判据（IQ-CF-08）。
// 缺字段时 Has* 为 false，调用方按「无元数据」策略处理，不得把缺省当合格。
type ArtifactDeliveryQualification struct {
	HasTextTrace    bool
	TextQualified   bool
	HasProduceRoute bool
	Route           ProduceRouteRecord
}

// ParseArtifactDeliveryQualification 只读解析 payload 中的 text_trace / produce_route。
func ParseArtifactDeliveryQualification(payload map[string]any) ArtifactDeliveryQualification {
	var out ArtifactDeliveryQualification
	if payload == nil {
		return out
	}
	if raw, ok := payload["text_trace"].(map[string]any); ok && raw != nil {
		out.HasTextTrace = true
		out.TextQualified = boolFromAny(raw["text_qualified"])
	}
	if raw, ok := payload["produce_route"].(map[string]any); ok && raw != nil {
		out.HasProduceRoute = true
		out.Route = ProduceRouteRecord{
			SchemaVersion:       intFromAny(raw["schema_version"]),
			Route:               stringFromAny(raw["route"]),
			ImageTypeKey:        stringFromAny(raw["image_type_key"]),
			AppearanceMayChange: boolFromAny(raw["appearance_may_change"]),
			RouteQualified:      boolFromAny(raw["route_qualified"]),
			UnresolvedItems:     stringListFromAny(raw["unresolved_items"]),
			ForbiddenPhrases:    stringListFromAny(raw["forbidden_phrases"]),
		}
	}
	return out
}

// TextAllowsDeliveryPass 文字追溯合格才允许进入已采用交付合格集。
func TextAllowsDeliveryPass(q ArtifactDeliveryQualification) bool {
	return q.HasTextTrace && q.TextQualified
}

// ArtifactAllowsQualifiedAdoption 文字与路线均显式合格（含 RouteAllowsDeliveryPass）。
func ArtifactAllowsQualifiedAdoption(q ArtifactDeliveryQualification) bool {
	return TextAllowsDeliveryPass(q) && q.HasProduceRoute && RouteAllowsDeliveryPass(q.Route)
}

// LoadImageArtifactPayloadForAdoption 按源图（优先节点）读取最近一条 image artifact payload。
// 找不到返回 nil, false, nil。
func LoadImageArtifactPayloadForAdoption(
	ctx context.Context,
	tx *gorm.DB,
	productID, sourceAssetID string,
	sourceNodeID *string,
) (map[string]any, bool, error) {
	assetID := strings.TrimSpace(sourceAssetID)
	if assetID == "" {
		return nil, false, nil
	}
	tryLoad := func(nodeID string) (schema.WorkflowGraphArtifacts, error) {
		q := tx.WithContext(ctx).Model(&schema.WorkflowGraphArtifacts{}).
			Select("workflow_graph_artifacts.payload_json").
			Joins("JOIN workflow_graphs g ON g.id = workflow_graph_artifacts.graph_id").
			Where("workflow_graph_artifacts.artifact_type = ? AND workflow_graph_artifacts.product_image_asset_id = ? AND g.product_id = ?",
				"image", assetID, productID).
			Order("workflow_graph_artifacts.created_at DESC, workflow_graph_artifacts.id DESC")
		if nodeID != "" {
			q = q.Where("workflow_graph_artifacts.node_id = ?", nodeID)
		}
		var art schema.WorkflowGraphArtifacts
		err := q.Take(&art).Error
		return art, err
	}

	if sourceNodeID != nil {
		nodeID := strings.TrimSpace(*sourceNodeID)
		if nodeID != "" {
			art, err := tryLoad(nodeID)
			if err == nil {
				return decodeArtifactPayloadJSON(art.PayloadJSON)
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, false, err
			}
		}
	}
	art, err := tryLoad("")
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return decodeArtifactPayloadJSON(art.PayloadJSON)
}

func decodeArtifactPayloadJSON(raw string) (map[string]any, bool, error) {
	if strings.TrimSpace(raw) == "" {
		return map[string]any{}, true, nil
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil, false, err
	}
	if payload == nil {
		payload = map[string]any{}
	}
	return payload, true, nil
}

func boolFromAny(v any) bool {
	switch typed := v.(type) {
	case bool:
		return typed
	default:
		return false
	}
}

func intFromAny(v any) int {
	switch typed := v.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return 0
	}
}

func stringFromAny(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}
