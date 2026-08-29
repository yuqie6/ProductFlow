package recipe

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/yuqie6/productflow/internal/platform/apperr"
)

type recipeRecord struct {
	ID               string
	Kind             string
	Origin           string
	OfficialKey      *string
	CurrentVersionID *string
	ArchivedAt       *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
	Current          *versionRecord
	Versions         []versionRecord
}

type versionRecord struct {
	ID                             string
	RecipeID                       string
	Version                        int
	SchemaVersion                  int
	CatalogVersion                 int
	CreationSource                 string
	Title                          string
	Description                    *string
	PayloadJSON                    []byte
	PayloadHash                    string
	GovernanceJSON                 []byte
	PreferredVisualSystemVersionID *string
	CreatedAt                      time.Time
}

type applicationRecord struct {
	ID                   string
	ProductID            string
	RecipeVersionID      string
	RecipeID             string
	RecipeVersion        int
	GraphID              string
	OperationGroupID     string
	Mode                 string
	IdempotencyKey       string
	RequestHash          string
	AddedNodeIDs         []string
	AddedEdgeIDs         []string
	PreviewGraphRevision *int
	PreviewDigest        *string
	UpdatedNodeIDs       []string
	RequiredBindings     []string
}

func scanRecipe(row pgx.Row) (recipeRecord, error) {
	var rec recipeRecord
	err := row.Scan(
		&rec.ID, &rec.Kind, &rec.Origin, &rec.OfficialKey, &rec.CurrentVersionID,
		&rec.ArchivedAt, &rec.CreatedAt, &rec.UpdatedAt,
	)
	return rec, err
}

func getProductTarget(ctx context.Context, tx pgx.Tx, productID string, forUpdate bool) (productTarget, error) {
	q := `SELECT id, current_fact_set_version_id FROM products WHERE id = $1`
	if forUpdate {
		q += ` FOR UPDATE`
	}
	var target productTarget
	err := tx.QueryRow(ctx, q, productID).Scan(&target.ID, &target.FactSetVersionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return productTarget{}, apperr.NotFound("商品不存在")
	}
	return target, err
}

func visualSystemVersionExists(ctx context.Context, tx pgx.Tx, id string) error {
	var found string
	err := tx.QueryRow(ctx, `SELECT id FROM visual_system_versions WHERE id = $1`, id).Scan(&found)
	if errors.Is(err, pgx.ErrNoRows) {
		return apperr.NotFound("视觉系统版本不存在")
	}
	return err
}

func listRecipes(ctx context.Context, tx pgx.Tx, includeArchived bool) ([]recipeRecord, error) {
	q := `
		SELECT id, kind, origin, official_key, current_version_id, archived_at, created_at, updated_at
		FROM workflow_recipes
		WHERE origin = 'user'
	`
	if !includeArchived {
		q += ` AND archived_at IS NULL`
	}
	q += ` ORDER BY updated_at DESC, id`
	rows, err := tx.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []recipeRecord{}
	ids := []string{}
	for rows.Next() {
		rec, err := scanRecipe(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
		if rec.CurrentVersionID != nil {
			ids = append(ids, *rec.CurrentVersionID)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	versions, err := loadVersionsByIDs(ctx, tx, ids)
	if err != nil {
		return nil, err
	}
	for i := range out {
		if out[i].CurrentVersionID == nil {
			continue
		}
		if ver, ok := versions[*out[i].CurrentVersionID]; ok {
			copied := ver
			out[i].Current = &copied
		}
	}
	return out, nil
}

func loadRecipe(ctx context.Context, tx pgx.Tx, recipeID string, forUpdate bool) (recipeRecord, error) {
	q := `
		SELECT id, kind, origin, official_key, current_version_id, archived_at, created_at, updated_at
		FROM workflow_recipes
		WHERE id = $1
	`
	if forUpdate {
		q += ` FOR UPDATE`
	}
	rec, err := scanRecipe(tx.QueryRow(ctx, q, recipeID))
	if errors.Is(err, pgx.ErrNoRows) {
		return recipeRecord{}, apperr.NotFound("工作流配方不存在")
	}
	if err != nil {
		return recipeRecord{}, err
	}
	if rec.Origin == originOfficial {
		return recipeRecord{}, apperr.NotFound("工作流配方不存在")
	}
	versions, err := loadRecipeVersions(ctx, tx, rec.ID)
	if err != nil {
		return recipeRecord{}, err
	}
	rec.Versions = versions
	if rec.CurrentVersionID != nil {
		for i := range versions {
			if versions[i].ID == *rec.CurrentVersionID {
				copied := versions[i]
				rec.Current = &copied
				break
			}
		}
	}
	return rec, nil
}

func loadRecipeVersions(ctx context.Context, tx pgx.Tx, recipeID string) ([]versionRecord, error) {
	rows, err := tx.Query(ctx, `
		SELECT id, recipe_id, version, schema_version, catalog_version, creation_source,
			title, description, payload_json, payload_hash, governance_json,
			preferred_visual_system_version_id, created_at
		FROM workflow_recipe_versions
		WHERE recipe_id = $1
		ORDER BY version
	`, recipeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []versionRecord{}
	for rows.Next() {
		ver, err := scanVersion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ver)
	}
	return out, rows.Err()
}

func loadVersionsByIDs(ctx context.Context, tx pgx.Tx, ids []string) (map[string]versionRecord, error) {
	out := map[string]versionRecord{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, `
		SELECT id, recipe_id, version, schema_version, catalog_version, creation_source,
			title, description, payload_json, payload_hash, governance_json,
			preferred_visual_system_version_id, created_at
		FROM workflow_recipe_versions
		WHERE id = ANY($1::text[])
	`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		ver, err := scanVersion(rows)
		if err != nil {
			return nil, err
		}
		out[ver.ID] = ver
	}
	return out, rows.Err()
}

func scanVersion(row pgx.Row) (versionRecord, error) {
	var ver versionRecord
	err := row.Scan(
		&ver.ID, &ver.RecipeID, &ver.Version, &ver.SchemaVersion, &ver.CatalogVersion, &ver.CreationSource,
		&ver.Title, &ver.Description, &ver.PayloadJSON, &ver.PayloadHash, &ver.GovernanceJSON,
		&ver.PreferredVisualSystemVersionID, &ver.CreatedAt,
	)
	return ver, err
}

func insertRecipe(ctx context.Context, tx pgx.Tx, rec recipeRecord) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO workflow_recipes (id, kind, origin, official_key, current_version_id, archived_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, NULL, NULL, NOW(), NOW())
	`, rec.ID, rec.Kind, rec.Origin, rec.OfficialKey)
	return err
}

func insertVersion(ctx context.Context, tx pgx.Tx, ver versionRecord) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO workflow_recipe_versions (
			id, recipe_id, version, schema_version, catalog_version, creation_source,
			title, description, payload_json, payload_hash, governance_json,
			preferred_visual_system_version_id, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb, $10, $11, $12, NOW())
	`, ver.ID, ver.RecipeID, ver.Version, ver.SchemaVersion, ver.CatalogVersion, ver.CreationSource,
		ver.Title, ver.Description, ver.PayloadJSON, ver.PayloadHash, nullableJSON(ver.GovernanceJSON),
		ver.PreferredVisualSystemVersionID)
	return err
}

func setCurrentVersion(ctx context.Context, tx pgx.Tx, recipeID, versionID string) error {
	_, err := tx.Exec(ctx, `
		UPDATE workflow_recipes SET current_version_id = $2, updated_at = NOW() WHERE id = $1
	`, recipeID, versionID)
	return err
}

func archiveRecipeRow(ctx context.Context, tx pgx.Tx, recipeID string) error {
	_, err := tx.Exec(ctx, `
		UPDATE workflow_recipes SET archived_at = NOW(), updated_at = NOW() WHERE id = $1
	`, recipeID)
	return err
}

func nullableJSON(raw []byte) any {
	if len(raw) == 0 {
		return nil
	}
	return raw
}

func applicationByKey(ctx context.Context, tx pgx.Tx, productID, key string) (*applicationRecord, error) {
	var rec applicationRecord
	var addedNodes, addedEdges, updatedNodes, bindings []byte
	err := tx.QueryRow(ctx, `
		SELECT a.id, a.product_id, a.recipe_version_id, v.recipe_id, v.version, a.graph_id, a.mode,
			a.idempotency_key, a.request_hash, a.added_node_ids_json, a.added_edge_ids_json,
			a.preview_graph_revision, a.preview_digest, a.updated_node_ids_json, a.required_bindings_json
		FROM workflow_recipe_applications a
		JOIN workflow_recipe_versions v ON v.id = a.recipe_version_id
		WHERE a.product_id = $1 AND a.idempotency_key = $2
	`, productID, key).Scan(
		&rec.ID, &rec.ProductID, &rec.RecipeVersionID, &rec.RecipeID, &rec.RecipeVersion, &rec.GraphID, &rec.Mode,
		&rec.IdempotencyKey, &rec.RequestHash, &addedNodes, &addedEdges,
		&rec.PreviewGraphRevision, &rec.PreviewDigest, &updatedNodes, &bindings,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	rec.AddedNodeIDs = decodeStringList(addedNodes)
	rec.AddedEdgeIDs = decodeStringList(addedEdges)
	rec.UpdatedNodeIDs = decodeStringList(updatedNodes)
	rec.RequiredBindings = decodeStringList(bindings)
	return &rec, nil
}

func insertApplication(ctx context.Context, tx pgx.Tx, rec applicationRecord) error {
	addedNodes, err := json.Marshal(nonNilStrings(rec.AddedNodeIDs))
	if err != nil {
		return err
	}
	addedEdges, err := json.Marshal(nonNilStrings(rec.AddedEdgeIDs))
	if err != nil {
		return err
	}
	updated, err := json.Marshal(nonNilStrings(rec.UpdatedNodeIDs))
	if err != nil {
		return err
	}
	bindings, err := json.Marshal(nonNilStrings(rec.RequiredBindings))
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO workflow_recipe_applications (
			id, product_id, recipe_version_id, graph_id, operation_group_id, mode, schema_version,
			idempotency_key, request_hash, added_node_ids_json, added_edge_ids_json,
			preview_graph_revision, preview_digest, updated_node_ids_json, required_bindings_json, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, 2, $7, $8, $9::jsonb, $10::jsonb, $11, $12, $13::jsonb, $14::jsonb, NOW())
	`, rec.ID, rec.ProductID, rec.RecipeVersionID, rec.GraphID, rec.OperationGroupID, rec.Mode,
		rec.IdempotencyKey, rec.RequestHash, addedNodes, addedEdges,
		rec.PreviewGraphRevision, rec.PreviewDigest, updated, bindings)
	return err
}

func decodeStringList(raw []byte) []string {
	if len(raw) == 0 || string(raw) == "null" {
		return []string{}
	}
	var out []string
	if err := json.Unmarshal(raw, &out); err != nil || out == nil {
		return []string{}
	}
	return out
}

func nonNilStrings(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}
