package product

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
)

// insertProduct 写入商品行；封面与 fact 由调用方随后设置。
func insertProduct(ctx context.Context, tx pgx.Tx, name string, category, price, sourceNote *string) (Product, error) {
	id := clockid.New()
	row := Product{ID: id, Name: name, Category: category, Price: price, SourceNote: sourceNote}
	err := tx.QueryRow(ctx, `
		INSERT INTO products (id, name, category, price, source_note, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, NOW(), NOW())
		RETURNING created_at, updated_at
	`, id, name, category, price, sourceNote).Scan(&row.CreatedAt, &row.UpdatedAt)
	return row, err
}

func assignCover(ctx context.Context, tx pgx.Tx, productID, assetID string) error {
	_, err := tx.Exec(ctx, `
		UPDATE products SET cover_image_asset_id = $1, updated_at = NOW() WHERE id = $2
	`, assetID, productID)
	return err
}

func setIntake(ctx context.Context, tx pgx.Tx, productID string, payload []byte) error {
	_, err := tx.Exec(ctx, `
		UPDATE products SET intake_schema_version = 1, intake_json = $1, updated_at = NOW() WHERE id = $2
	`, payload, productID)
	return err
}

func setCurrentFactSet(ctx context.Context, tx pgx.Tx, productID, factSetID string) error {
	_, err := tx.Exec(ctx, `
		UPDATE products SET current_fact_set_version_id = $1, updated_at = NOW() WHERE id = $2
	`, factSetID, productID)
	return err
}

func insertAsset(ctx context.Context, tx pgx.Tx, productID, mediaID, filename string) (ImageAsset, error) {
	id := clockid.New()
	display := strings.TrimSpace(filename)
	if display == "" {
		display = "image"
	}
	if len([]rune(display)) > 255 {
		display = string([]rune(display)[:255])
	}
	original := display
	asset := ImageAsset{
		ID:                 id,
		ProductID:          productID,
		MediaObjectID:      mediaID,
		OriginType:         "upload",
		DisplayName:        display,
		OriginalFilename:   original,
		VerificationStatus: "verified",
	}
	err := tx.QueryRow(ctx, `
		INSERT INTO product_image_assets (
			id, product_id, media_object_id, origin_type, display_name, original_filename, created_at, updated_at
		) VALUES ($1, $2, $3, 'upload', $4, $5, NOW(), NOW())
		RETURNING created_at, updated_at
	`, id, productID, mediaID, display, original).Scan(&asset.CreatedAt, &asset.UpdatedAt)
	return asset, err
}

func loadProduct(ctx context.Context, tx pgx.Tx, id string) (Product, error) {
	return scanProduct(ctx, tx, id, false)
}

func loadProductForUpdate(ctx context.Context, tx pgx.Tx, id string) (Product, error) {
	return scanProduct(ctx, tx, id, true)
}

func scanProduct(ctx context.Context, tx pgx.Tx, id string, forUpdate bool) (Product, error) {
	sql := `
		SELECT id, name, category, price::text, source_note, cover_image_asset_id,
		       intake_schema_version, intake_json, current_fact_set_version_id, created_at, updated_at
		FROM products WHERE id = $1`
	if forUpdate {
		sql += ` FOR UPDATE`
	}
	var row Product
	var intake []byte
	err := tx.QueryRow(ctx, sql, id).Scan(
		&row.ID, &row.Name, &row.Category, &row.Price, &row.SourceNote, &row.CoverImageAssetID,
		&row.IntakeVersion, &intake, &row.FactSetVersionID, &row.CreatedAt, &row.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Product{}, apperr.NotFound("商品不存在")
	}
	if err != nil {
		return Product{}, err
	}
	if len(intake) > 0 {
		row.IntakeJSON = json.RawMessage(intake)
	}
	return row, nil
}

func loadAssetsByIDs(ctx context.Context, tx pgx.Tx, productID string, ids []string) ([]ImageAsset, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := tx.Query(ctx, `
		SELECT a.id, a.product_id, a.media_object_id, a.origin_type, a.display_name, a.original_filename,
		       a.image_type_key, a.user_folder_id, a.parent_asset_id, a.source_image_session_asset_id,
		       a.source_library_asset_id, m.mime_type, m.byte_size, m.width, m.height, m.verification_status,
		       m.storage_path, a.created_at, a.updated_at
		FROM product_image_assets a
		JOIN media_objects m ON m.id = a.media_object_id
		WHERE a.product_id = $1 AND a.id = ANY($2)
		ORDER BY a.created_at ASC, a.id ASC
	`, productID, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAssets(rows)
}

func loadAsset(ctx context.Context, q interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}, assetID string) (ImageAsset, error) {
	var asset ImageAsset
	err := q.QueryRow(ctx, `
		SELECT a.id, a.product_id, a.media_object_id, a.origin_type, a.display_name, a.original_filename,
		       a.image_type_key, a.user_folder_id, a.parent_asset_id, a.source_image_session_asset_id,
		       a.source_library_asset_id, m.mime_type, m.byte_size, m.width, m.height, m.verification_status,
		       m.storage_path, a.created_at, a.updated_at
		FROM product_image_assets a
		JOIN media_objects m ON m.id = a.media_object_id
		WHERE a.id = $1
	`, assetID).Scan(
		&asset.ID, &asset.ProductID, &asset.MediaObjectID, &asset.OriginType, &asset.DisplayName, &asset.OriginalFilename,
		&asset.ImageTypeKey, &asset.UserFolderID, &asset.ParentAssetID, &asset.SourceImageSessionAsset,
		&asset.SourceLibraryAsset, &asset.MIMEType, &asset.ByteSize, &asset.Width, &asset.Height, &asset.VerificationStatus,
		&asset.StoragePath, &asset.CreatedAt, &asset.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ImageAsset{}, apperr.NotFound("商品图片不存在")
	}
	return asset, err
}

func listProducts(ctx context.Context, tx pgx.Tx, page, pageSize int, q, sort string) ([]Summary, int, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	order := "p.updated_at DESC, p.id DESC"
	switch sort {
	case "created_desc":
		order = "p.created_at DESC, p.id DESC"
	case "name_asc":
		order = "LOWER(p.name) ASC, p.name ASC, p.id ASC"
	}
	where := ""
	args := []any{}
	if strings.TrimSpace(q) != "" {
		where = "WHERE p.name ILIKE $1"
		args = append(args, "%"+escapeLike(strings.TrimSpace(q))+"%")
	}
	var total int
	countSQL := `SELECT COUNT(*) FROM products p ` + where
	if err := tx.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	offset := (page - 1) * pageSize
	query := `
		SELECT p.id, p.name, p.category, p.price::text, p.cover_image_asset_id, a.original_filename,
		       p.created_at, p.updated_at
		FROM products p
		LEFT JOIN product_image_assets a ON a.id = p.cover_image_asset_id
		` + where + ` ORDER BY ` + order + ` OFFSET $` + strconv.Itoa(len(args)+1) + ` LIMIT $` + strconv.Itoa(len(args)+2)
	args = append(args, offset, pageSize)
	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []Summary{}
	for rows.Next() {
		var item Summary
		if err := rows.Scan(&item.ID, &item.Name, &item.Category, &item.Price, &item.CoverImageAssetID, &item.CoverImageFilename, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, 0, err
		}
		if item.CoverImageAssetID != nil {
			download, preview, thumb := assetURLs(*item.CoverImageAssetID)
			item.CoverImageDownloadURL = &download
			item.CoverImagePreviewURL = &preview
			item.CoverImageThumbnailURL = &thumb
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func insertFactSet(ctx context.Context, tx pgx.Tx, productID string, facts []map[string]any) (string, int, error) {
	payload := map[string]any{
		"schema_version":    1,
		"source_product_id": productID,
		"facts":             facts,
	}
	raw, err := canonicalJSON(payload)
	if err != nil {
		return "", 0, err
	}
	sum := sha256Hex(raw)
	var version int
	err = tx.QueryRow(ctx, `
		SELECT COALESCE(MAX(version), 0) FROM product_fact_set_versions WHERE product_id = $1
	`, productID).Scan(&version)
	if err != nil {
		return "", 0, err
	}
	version++
	id := clockid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO product_fact_set_versions (id, product_id, version, payload_json, payload_hash, created_at)
		VALUES ($1, $2, $3, $4, $5, NOW())
	`, id, productID, version, raw, sum)
	if err != nil {
		return "", 0, err
	}
	if err := setCurrentFactSet(ctx, tx, productID, id); err != nil {
		return "", 0, err
	}
	return id, version, nil
}

func loadConversationByKey(ctx context.Context, tx pgx.Tx, key string) (Conversation, error) {
	var row Conversation
	err := tx.QueryRow(ctx, `
		SELECT id, scope_type, session_id, product_id, harness_run_id, status, created_at, updated_at
		FROM agent_conversations WHERE creation_idempotency_key = $1
	`, key).Scan(&row.ID, &row.ScopeType, &row.SessionID, &row.ProductID, &row.HarnessRunID, &row.Status, &row.CreatedAt, &row.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Conversation{}, err
	}
	return row, err
}

func conversationRequestHash(ctx context.Context, tx pgx.Tx, id string) (string, error) {
	var hash *string
	err := tx.QueryRow(ctx, `SELECT creation_request_hash FROM agent_conversations WHERE id = $1`, id).Scan(&hash)
	if err != nil {
		return "", err
	}
	if hash == nil {
		return "", nil
	}
	return *hash, nil
}

func insertSession(ctx context.Context, tx pgx.Tx, title, productID string) (string, time.Time, time.Time, error) {
	id := clockid.New()
	if len([]rune(title)) > 160 {
		title = string([]rune(title)[:160])
	}
	var created, updated time.Time
	err := tx.QueryRow(ctx, `
		INSERT INTO agent_sessions (id, product_id, title, summary, status, created_at, updated_at)
		VALUES ($1, $2, $3, '暂无 Agent Task', 'active', NOW(), NOW())
		RETURNING created_at, updated_at
	`, id, productID, title).Scan(&created, &updated)
	return id, created, updated, err
}

func loadSessionProduct(ctx context.Context, tx pgx.Tx, sessionID string) (productID *string, status string, err error) {
	err = tx.QueryRow(ctx, `SELECT product_id, status FROM agent_sessions WHERE id = $1`, sessionID).Scan(&productID, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", apperr.NotFound("Agent Session 不存在")
	}
	return productID, status, err
}

func insertConversation(ctx context.Context, tx pgx.Tx, sessionID, productID, key, requestHash string) (Conversation, error) {
	id := clockid.New()
	row := Conversation{
		ID:           id,
		ScopeType:    "product_workflow",
		SessionID:    &sessionID,
		ProductID:    &productID,
		HarnessRunID: id,
		Status:       "collecting",
	}
	err := tx.QueryRow(ctx, `
		INSERT INTO agent_conversations (
			id, scope_type, session_id, product_id, harness_run_id, status,
			creation_idempotency_key, creation_request_hash, created_at, updated_at
		) VALUES ($1, 'product_workflow', $2, $3, $1, 'collecting', $4, $5, NOW(), NOW())
		RETURNING created_at, updated_at
	`, id, sessionID, productID, key, requestHash).Scan(&row.CreatedAt, &row.UpdatedAt)
	return row, err
}

func scanAssets(rows pgx.Rows) ([]ImageAsset, error) {
	out := []ImageAsset{}
	for rows.Next() {
		var asset ImageAsset
		if err := rows.Scan(
			&asset.ID, &asset.ProductID, &asset.MediaObjectID, &asset.OriginType, &asset.DisplayName, &asset.OriginalFilename,
			&asset.ImageTypeKey, &asset.UserFolderID, &asset.ParentAssetID, &asset.SourceImageSessionAsset,
			&asset.SourceLibraryAsset, &asset.MIMEType, &asset.ByteSize, &asset.Width, &asset.Height, &asset.VerificationStatus,
			&asset.StoragePath, &asset.CreatedAt, &asset.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, asset)
	}
	return out, rows.Err()
}

func escapeLike(q string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(q)
}

func sha256Hex(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
