package product

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/canonjson"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
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

// CreateFidelityInput 写入一条人工保真检查。IdempotencyKey 相同且内容不同返回 Conflict。
type CreateFidelityInput struct {
	ExpectedLatestVersion int    // 乐观锁；不匹配 Conflict
	IdempotencyKey        string // 相同键内容不同返回 Conflict
	ShapeFidelity         string // pass|fail|not_applicable
	ColorMaterialFidelity string // pass|fail|not_applicable
	LogoTextLegibility    string // pass|fail|not_applicable
	TextPolicyCompliance  string // pass|fail|not_applicable
	Notes                 *string
}

// FidelityCheck 是一条不可变人工保真检查的 HTTP 投影，对应 product_image_fidelity_checks。
// 四项结果闭集 pass|fail|not_applicable；CheckedBy 固定 administrator。同一 IdempotencyKey 内容不同返回 Conflict。
// 不要改已有行；新检查插入新 version。不要和 media.verification_status 搞混。
type FidelityCheck struct {
	ID                    string    `json:"id"`
	ProductID             string    `json:"product_id"`
	AssetID               string    `json:"asset_id"`
	Version               int       `json:"version"`                 // 不可变；新检查插入新 version
	ShapeFidelity         string    `json:"shape_fidelity"`          // pass|fail|not_applicable
	ColorMaterialFidelity string    `json:"color_material_fidelity"` // pass|fail|not_applicable
	LogoTextLegibility    string    `json:"logo_text_legibility"`    // pass|fail|not_applicable
	TextPolicyCompliance  string    `json:"text_policy_compliance"`  // pass|fail|not_applicable
	Notes                 *string   `json:"notes"`
	CheckedBy             string    `json:"checked_by"`      // 固定 administrator
	IdempotencyKey        string    `json:"idempotency_key"` // 同 key 内容不同返回 Conflict
	RequestHash           string    `json:"request_hash"`    // 内容哈希，用来检测同 key 改内容
	CreatedAt             time.Time `json:"created_at"`
}

// FidelityCheckList 是 GET .../fidelity-checks 200 体。LatestVersion 给 POST 的 expected 乐观锁。
// Items 新在前。limit 非法 400。不写库。
type FidelityCheckList struct {
	ProductID     string          `json:"product_id"`
	AssetID       string          `json:"asset_id"`
	LatestVersion int             `json:"latest_version"` // 给 POST 的 expected 乐观锁
	Items         []FidelityCheck `json:"items"`          // 新在前；空列表是 [] 不是 nil
}

func fidelityFromSchema(rec schema.ProductImageFidelityChecks) FidelityCheck {
	return FidelityCheck{
		ID:                    rec.ID,
		ProductID:             rec.ProductID,
		AssetID:               rec.AssetID,
		Version:               rec.Version,
		ShapeFidelity:         rec.ShapeFidelity,
		ColorMaterialFidelity: rec.ColorMaterialFidelity,
		LogoTextLegibility:    rec.LogoTextLegibility,
		TextPolicyCompliance:  rec.TextPolicyCompliance,
		Notes:                 rec.Notes,
		CheckedBy:             rec.CheckedBy,
		IdempotencyKey:        rec.IdempotencyKey,
		RequestHash:           rec.RequestHash,
		CreatedAt:             rec.CreatedAt,
	}
}

// ListFidelityChecks 按 version 降序列出保真检查。limit 须在 1–100。
func (s Service) ListFidelityChecks(ctx context.Context, productID, assetID string, limit int) (FidelityCheckList, error) {
	if limit < 1 || limit > fidelityCheckMaxLimit {
		return FidelityCheckList{}, apperr.Validationf("人工保真检查列表最多返回 %d 条", fidelityCheckMaxLimit)
	}
	out := FidelityCheckList{ProductID: productID, AssetID: assetID, Items: []FidelityCheck{}}
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		if err := requireProductAndAsset(ctx, pgxTx, productID, assetID, false); err != nil {
			return err
		}
		var rows []schema.ProductImageFidelityChecks
		err := pgxTx.WithContext(ctx).
			Where("product_id = ? AND asset_id = ?", productID, assetID).
			Order("version DESC, created_at DESC, id DESC").
			Limit(limit).
			Find(&rows).Error
		if err != nil {
			return err
		}
		for _, rec := range rows {
			out.Items = append(out.Items, fidelityFromSchema(rec))
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

// CreateFidelityCheck 在 expected_latest_version 匹配时追加新版本。资产须属于该商品。
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

// insertFidelityCheck 追加不可变保真检查版本。
// 同 idempotency key 且内容哈希一致：原样返回（幂等）。expected_latest_version 落后、或同 key 哈希不同：Conflict。
// 不覆盖旧行。资产必须属于该商品，调用方已锁商品。
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
	now := time.Now().UTC()
	rec := schema.ProductImageFidelityChecks{
		ID:                    clockid.New(),
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
		CreatedAt:             now,
	}
	if err := tx.WithContext(ctx).Create(&rec).Error; err != nil {
		return FidelityCheck{}, err
	}
	return fidelityFromSchema(rec), nil
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
	q := tx.WithContext(ctx).Select("id").Where("id = ? AND product_id = ?", assetID, productID)
	if forUpdate {
		q = q.Clauses(pfdb.ForUpdate())
	}
	var rec schema.ProductImageAssets
	err := q.Take(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return apperr.NotFound("商品图片不存在")
	}
	return err
}

func findFidelityByIdempotency(ctx context.Context, tx *gorm.DB, assetID, key string) (*FidelityCheck, error) {
	var rec schema.ProductImageFidelityChecks
	err := tx.WithContext(ctx).Where("asset_id = ? AND idempotency_key = ?", assetID, key).Take(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	item := fidelityFromSchema(rec)
	return &item, nil
}

func latestFidelityVersion(ctx context.Context, tx *gorm.DB, assetID string) (int, error) {
	var latest *int
	err := tx.WithContext(ctx).Model(&schema.ProductImageFidelityChecks{}).
		Where("asset_id = ?", assetID).
		Select("MAX(version)").
		Scan(&latest).Error
	if err != nil {
		return 0, err
	}
	if latest == nil {
		return 0, nil
	}
	return *latest, nil
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
