package graph

import (
	"encoding/json"
	"fmt"
	"strings"

	"gorm.io/gorm"
)

type originBackfillRow struct {
	ID             string
	NodeType       string
	ConfigJSON     string
	DocumentOrigin *string
}

// BackfillDocumentOrigin 把内容节点 origin 从「config 旧键或默认 seed」收敛为：
// 可见文稿与当前种子模板一致则为 seed，否则 authored。已是 generated/authored 的行不改。
func BackfillDocumentOrigin(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	var rows []originBackfillRow
	err := db.Raw(`
		SELECT id, node_type, config_json, document_origin
		FROM workflow_graph_nodes
		WHERE node_type IN ('creative_brief', 'visual_system', 'image_prompt')
	`).Scan(&rows).Error
	if err != nil {
		return fmt.Errorf("load document_origin rows: %w", err)
	}
	for _, row := range rows {
		current := ""
		if row.DocumentOrigin != nil {
			current = strings.TrimSpace(*row.DocumentOrigin)
		}
		if current == OriginGenerated || current == OriginAuthored {
			continue
		}
		config := map[string]any{}
		if strings.TrimSpace(row.ConfigJSON) != "" {
			if err := json.Unmarshal([]byte(row.ConfigJSON), &config); err != nil {
				config = map[string]any{}
			}
		}
		inferred := inferDocumentOriginFromConfig(NodeType(row.NodeType), config)
		if inferred == "" {
			inferred = OriginSeed
		}
		if current == inferred {
			continue
		}
		if err := db.Exec(
			`UPDATE workflow_graph_nodes SET document_origin = ? WHERE id = ?`,
			inferred,
			row.ID,
		).Error; err != nil {
			return fmt.Errorf("stamp document_origin %s: %w", row.ID, err)
		}
	}
	return nil
}
