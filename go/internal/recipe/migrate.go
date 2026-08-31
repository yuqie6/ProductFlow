package recipe

import (
	"context"

	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// BackfillImagePromptNodeType upgrades persisted recipe payloads after the
// prompt_generation node type was retired. Recipe hashes cover the canonical
// payload, so the payload and hash must be changed atomically.
func BackfillImagePromptNodeType(ctx context.Context, db *gorm.DB) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rows []schema.WorkflowRecipeVersions
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where(`EXISTS (
				SELECT 1 FROM jsonb_array_elements(payload_json::jsonb->'nodes') AS node
				WHERE node->>'node_type' = ?
			)`, "prompt_generation").
			Find(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			payload, hash, changed, err := rewriteImagePromptPayload([]byte(row.PayloadJSON))
			if err != nil {
				return err
			}
			if !changed {
				continue
			}
			if err := tx.Model(&schema.WorkflowRecipeVersions{}).Where("id = ?", row.ID).Updates(map[string]any{
				"payload_json":    string(payload),
				"payload_hash":    hash,
				"catalog_version": graph.CatalogVersion,
			}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func rewriteImagePromptPayload(raw []byte) ([]byte, string, bool, error) {
	var wire payloadWire
	if err := decodeStrict(raw, &wire); err != nil {
		return nil, "", false, err
	}
	changed := false
	for i := range wire.Nodes {
		if wire.Nodes[i].NodeType == graph.NodeType("prompt_generation") {
			wire.Nodes[i].NodeType = graph.NodeImagePrompt
			changed = true
		}
	}
	if !changed {
		return raw, "", false, nil
	}
	payload := payloadFromWire(wire)
	if err := validatePayload(payload); err != nil {
		return nil, "", false, err
	}
	encoded, err := payloadJSON(payload)
	if err != nil {
		return nil, "", false, err
	}
	hash, err := payloadHash(payload)
	if err != nil {
		return nil, "", false, err
	}
	return encoded, hash, true, nil
}
