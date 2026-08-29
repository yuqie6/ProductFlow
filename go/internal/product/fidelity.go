package product

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	sqldb "database/sql"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/canonjson"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

const (
	fidelityCheckMaxLimit       = 100
	fidelityCheckActor          = "administrator"
	fidelityCheckNotesMaxLen    = 4000
	fidelityCheckIdempotencyMax = 120
	fidelityVersionConflict     = "商品图片人工保真检查版本已变化，请基于最新版本重试"
	fidelityIdempotencyConflict = "相同 idempotency key 不能写入不同的人工保真检查内容"
)

var fidelityOutcomes = map[string]struct{}{
	"pass":           {},
	"fail":           {},
	"not_applicable": {},
}

type CreateFidelityInput struct {
	ExpectedLatestVersion int
	IdempotencyKey        string
	ShapeFidelity         string
	ColorMaterialFidelity string
	LogoTextLegibility    string
	TextPolicyCompliance  string
	Notes                 *string
}

type FidelityCheck struct {
	ID                    string    `json:"id"`
	ProductID             string    `json:"product_id"`
	AssetID               string    `json:"asset_id"`
	Version               int       `json:"version"`
	ShapeFidelity         string    `json:"shape_fidelity"`
	ColorMaterialFidelity string    `json:"color_material_fidelity"`
	LogoTextLegibility    string    `json:"logo_text_legibility"`
	TextPolicyCompliance  string    `json:"text_policy_compliance"`
	Notes                 *string   `json:"notes"`
	CheckedBy             string    `json:"checked_by"`
	IdempotencyKey        string    `json:"idempotency_key"`
	RequestHash           string    `json:"request_hash"`
	CreatedAt             time.Time `json:"created_at"`
}

type FidelityCheckList struct {
	ProductID     string          `json:"product_id"`
	AssetID       string          `json:"asset_id"`
	LatestVersion int             `json:"latest_version"`
	Items         []FidelityCheck `json:"items"`
}

func (s Service) ListFidelityChecks(ctx context.Context, productID, assetID string, limit int) (FidelityCheckList, error) {
	if limit < 1 || limit > fidelityCheckMaxLimit {
		return FidelityCheckList{}, apperr.Validationf("人工保真检查列表最多返回 %d 条", fidelityCheckMaxLimit)
	}
	out := FidelityCheckList{ProductID: productID, AssetID: assetID, Items: []FidelityCheck{}}
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		if err := requireProductAndAsset(ctx, pgxTx, productID, assetID, false); err != nil {
			return err
		}
		rows, err := pfdb.Query(ctx, pgxTx, `
			SELECT `+fidelitySelect+`
			FROM product_image_fidelity_checks
			WHERE product_id = $1 AND asset_id = $2
			ORDER BY version DESC, created_at DESC, id DESC
			LIMIT $3
		`, productID, assetID, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			item, err := scanFidelity(rows)
			if err != nil {
				return err
			}
			out.Items = append(out.Items, item)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		latest, err := latestFidelityVersion(ctx, pgxTx, assetID)
		if err != nil {
			return err
		}
		out.LatestVersion = latest
		return nil
	})
	return out, err
}

func (s Service) CreateFidelityCheck(ctx context.Context, productID, assetID string, in CreateFidelityInput) (FidelityCheck, error) {
	if in.ExpectedLatestVersion < 0 {
		return FidelityCheck{}, apperr.Validation("expected_latest_version 不能小于 0")
	}
	key, err := normalizeFidelityIdempotencyKey(in.IdempotencyKey)
	if err != nil {
		return FidelityCheck{}, err
	}
	notes, err := normalizeFidelityNotes(in.Notes)
	if err != nil {
		return FidelityCheck{}, err
	}
	shape, err := normalizeFidelityOutcome(in.ShapeFidelity)
	if err != nil {
		return FidelityCheck{}, err
	}
	color, err := normalizeFidelityOutcome(in.ColorMaterialFidelity)
	if err != nil {
		return FidelityCheck{}, err
	}
	logo, err := normalizeFidelityOutcome(in.LogoTextLegibility)
	if err != nil {
		return FidelityCheck{}, err
	}
	text, err := normalizeFidelityOutcome(in.TextPolicyCompliance)
	if err != nil {
		return FidelityCheck{}, err
	}
	requestHash, err := fidelityRequestHash(productID, assetID, in.ExpectedLatestVersion, shape, color, logo, text, notes)
	if err != nil {
		return FidelityCheck{}, err
	}

	var out FidelityCheck
	err = tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		created, err := insertFidelityCheck(ctx, pgxTx, productID, assetID, in.ExpectedLatestVersion, key, requestHash, shape, color, logo, text, notes)
		if err != nil {
			return err
		}
		out = created
		return nil
	})
	if uniqueViolation(err) {
		return s.resolveFidelityUniqueConflict(ctx, assetID, key, requestHash)
	}
	return out, err
}

func insertFidelityCheck(
	ctx context.Context,
	tx *gorm.DB,
	productID, assetID string,
	expectedLatestVersion int,
	key, requestHash, shape, color, logo, text string,
	notes *string,
) (FidelityCheck, error) {
	if err := requireProductAndAsset(ctx, tx, productID, assetID, true); err != nil {
		return FidelityCheck{}, err
	}
	existing, err := findFidelityByIdempotency(ctx, tx, assetID, key)
	if err != nil {
		return FidelityCheck{}, err
	}
	if existing != nil {
		if existing.RequestHash != requestHash {
			return FidelityCheck{}, apperr.Conflict(fidelityIdempotencyConflict)
		}
		return *existing, nil
	}
	latest, err := latestFidelityVersion(ctx, tx, assetID)
	if err != nil {
		return FidelityCheck{}, err
	}
	if expectedLatestVersion != latest {
		return FidelityCheck{}, apperr.Conflict(fidelityVersionConflict)
	}
	id := clockid.New()
	row := FidelityCheck{
		ID:                    id,
		ProductID:             productID,
		AssetID:               assetID,
		Version:               latest + 1,
		ShapeFidelity:         shape,
		ColorMaterialFidelity: color,
		LogoTextLegibility:    logo,
		TextPolicyCompliance:  text,
		Notes:                 notes,
		CheckedBy:             fidelityCheckActor,
		IdempotencyKey:        key,
		RequestHash:           requestHash,
	}
	err = pfdb.QueryRow(ctx, tx, `
		INSERT INTO product_image_fidelity_checks (
			id, product_id, asset_id, version,
			shape_fidelity, color_material_fidelity, logo_text_legibility, text_policy_compliance,
			notes, checked_by, idempotency_key, request_hash, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, NOW())
		RETURNING created_at
	`, id, productID, assetID, row.Version, shape, color, logo, text, notes, fidelityCheckActor, key, requestHash).Scan(&row.CreatedAt)
	return row, err
}

func (s Service) resolveFidelityUniqueConflict(ctx context.Context, assetID, key, requestHash string) (FidelityCheck, error) {
	var out FidelityCheck
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		existing, err := findFidelityByIdempotency(ctx, pgxTx, assetID, key)
		if err != nil {
			return err
		}
		if existing != nil {
			if existing.RequestHash != requestHash {
				return apperr.Conflict(fidelityIdempotencyConflict)
			}
			out = *existing
			return nil
		}
		return apperr.Conflict(fidelityVersionConflict)
	})
	return out, err
}

func requireProductAndAsset(ctx context.Context, tx *gorm.DB, productID, assetID string, forUpdate bool) error {
	if forUpdate {
		if _, err := loadProductForUpdate(ctx, tx, productID); err != nil {
			return err
		}
	} else if _, err := loadProduct(ctx, tx, productID); err != nil {
		return err
	}
	sql := `SELECT id FROM product_image_assets WHERE id = $1 AND product_id = $2`
	if forUpdate {
		sql += ` FOR UPDATE`
	}
	var id string
	err := pfdb.QueryRow(ctx, tx, sql, assetID, productID).Scan(&id)
	if errors.Is(err, sqldb.ErrNoRows) {
		return apperr.NotFound("商品图片不存在")
	}
	return err
}

const fidelitySelect = `
	id, product_id, asset_id, version,
	shape_fidelity, color_material_fidelity, logo_text_legibility, text_policy_compliance,
	notes, checked_by, idempotency_key, request_hash, created_at`

type fidelityScanner interface {
	Scan(dest ...any) error
}

func scanFidelity(row fidelityScanner) (FidelityCheck, error) {
	var item FidelityCheck
	var notes sqldb.NullString
	err := row.Scan(
		&item.ID, &item.ProductID, &item.AssetID, &item.Version,
		&item.ShapeFidelity, &item.ColorMaterialFidelity, &item.LogoTextLegibility, &item.TextPolicyCompliance,
		&notes, &item.CheckedBy, &item.IdempotencyKey, &item.RequestHash, &item.CreatedAt,
	)
	if err != nil {
		return FidelityCheck{}, err
	}
	if notes.Valid {
		item.Notes = &notes.String
	}
	return item, nil
}

func findFidelityByIdempotency(ctx context.Context, tx *gorm.DB, assetID, key string) (*FidelityCheck, error) {
	item, err := scanFidelity(pfdb.QueryRow(ctx, tx, `
		SELECT `+fidelitySelect+`
		FROM product_image_fidelity_checks
		WHERE asset_id = $1 AND idempotency_key = $2
	`, assetID, key))
	if errors.Is(err, sqldb.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func latestFidelityVersion(ctx context.Context, tx *gorm.DB, assetID string) (int, error) {
	var latest sqldb.NullInt64
	err := pfdb.QueryRow(ctx, tx, `
		SELECT MAX(version) FROM product_image_fidelity_checks WHERE asset_id = $1
	`, assetID).Scan(&latest)
	if err != nil {
		return 0, err
	}
	if !latest.Valid {
		return 0, nil
	}
	return int(latest.Int64), nil
}

func normalizeFidelityIdempotencyKey(value string) (string, error) {
	if utf8.RuneCountInString(value) > fidelityCheckIdempotencyMax {
		return "", apperr.Validation("请求体无效")
	}
	normalized := strings.TrimSpace(value)
	if normalized == "" {
		return "", apperr.Validation("idempotency_key 不能为空")
	}
	return normalized, nil
}

func normalizeFidelityNotes(value *string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	if utf8.RuneCountInString(*value) > fidelityCheckNotesMaxLen {
		return nil, apperr.Validation("人工保真检查 notes 不能超过 4000 个字符")
	}
	normalized := strings.TrimSpace(*value)
	if utf8.RuneCountInString(normalized) > fidelityCheckNotesMaxLen {
		return nil, apperr.Validation("人工保真检查 notes 不能超过 4000 个字符")
	}
	if normalized == "" {
		return nil, nil
	}
	return &normalized, nil
}

func normalizeFidelityOutcome(value string) (string, error) {
	if _, ok := fidelityOutcomes[value]; !ok {
		return "", apperr.Validation("人工保真检查结论无效")
	}
	return value, nil
}

func fidelityRequestHash(productID, assetID string, expectedLatestVersion int, shape, color, logo, text string, notes *string) (string, error) {
	var notesVal any
	if notes != nil {
		notesVal = *notes
	}
	return canonjson.SHA256Hex(map[string]any{
		"asset_id":                assetID,
		"color_material_fidelity": color,
		"expected_latest_version": expectedLatestVersion,
		"logo_text_legibility":    logo,
		"notes":                   notesVal,
		"product_id":              productID,
		"shape_fidelity":          shape,
		"text_policy_compliance":  text,
	})
}
