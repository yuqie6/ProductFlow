package graph

import (
	"encoding/json"
	"testing"

	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/testdb"
)

func TestBackfillDocumentOriginUpgradesDivergedSeed(t *testing.T) {
	gdb := testdb.Gorm(t)
	tx := gdb.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer func() { _ = tx.Rollback() }()

	productID := clockid.New()
	graphID := clockid.New()
	seedPromptID := clockid.New()
	authoredPromptID := clockid.New()
	authoredBriefID := clockid.New()
	generatedID := clockid.New()
	if err := tx.Exec(`
		INSERT INTO products (id, name, created_at, updated_at, merchant_id)
		VALUES (?, 'origin回填', NOW(), NOW(), ?)
	`, productID, auth.MustDevMerchantID(t, tx)).Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.Exec(`
		INSERT INTO workflow_graphs (id, product_id, title, active, schema_version, revision, created_at, updated_at)
		VALUES (?, ?, 'origin回填', TRUE, 3, 1, NOW(), NOW())
	`, graphID, productID).Error; err != nil {
		t.Fatal(err)
	}
	heroGoal := imageTypePromptGoal("hero")
	seedPrompt, _ := json.Marshal(map[string]any{
		"image_type_key": "hero",
		"prompt":         map[string]any{"design_goal": heroGoal},
	})
	authoredPrompt, _ := json.Marshal(map[string]any{
		"image_type_key": "hero",
		"prompt": map[string]any{
			"design_goal": heroGoal,
			"composition": map[string]any{"layout": "左侧留白"},
		},
	})
	authoredBrief, _ := json.Marshal(map[string]any{"goal": "手填卖点"})
	generatedPrompt, _ := json.Marshal(map[string]any{
		"image_type_key": "hero",
		"prompt": map[string]any{
			"design_goal": heroGoal,
			"composition": map[string]any{"layout": "模型构图"},
		},
	})
	if err := tx.Exec(`
		INSERT INTO workflow_graph_nodes (
			id, graph_id, node_type, title, position_x, position_y, config_json, document_origin, created_at, updated_at
		) VALUES
			(?, ?, 'image_prompt', '种子提示词', 0, 0, ?::json, 'seed', NOW(), NOW()),
			(?, ?, 'image_prompt', '手填提示词', 0, 0, ?::json, 'seed', NOW(), NOW()),
			(?, ?, 'creative_brief', '手填要求', 0, 0, ?::json, 'seed', NOW(), NOW()),
			(?, ?, 'image_prompt', '已生成', 0, 0, ?::json, 'generated', NOW(), NOW())
	`,
		seedPromptID, graphID, string(seedPrompt),
		authoredPromptID, graphID, string(authoredPrompt),
		authoredBriefID, graphID, string(authoredBrief),
		generatedID, graphID, string(generatedPrompt),
	).Error; err != nil {
		t.Fatal(err)
	}
	if err := BackfillDocumentOrigin(tx); err != nil {
		t.Fatal(err)
	}

	assertOrigin := func(id, want string) {
		t.Helper()
		var got string
		if err := tx.Raw(`SELECT document_origin FROM workflow_graph_nodes WHERE id = ?`, id).Scan(&got).Error; err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("node %s origin %s want %s", id, got, want)
		}
	}
	assertOrigin(seedPromptID, OriginSeed)
	assertOrigin(authoredPromptID, OriginAuthored)
	assertOrigin(authoredBriefID, OriginAuthored)
	assertOrigin(generatedID, OriginGenerated)
}
