package product

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	sqldb "database/sql"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

// UpdateFactsInput 对应 PUT /v3/products/{id}/facts。
// Fields 记录 JSON 里实际出现的键，用来区分「未传」和「显式清空」。
type UpdateFactsInput struct {
	ExpectedFactSetVersionID *string
	ExpectedFactVersion      *int
	ExpectedVersionProvided  bool
	Name                     *string
	Category                 *string
	Price                    *string
	SourceNote               *string
	Facts                    *[]map[string]any
	Fields                   map[string]bool
}

// GetFacts 返回当前选中的 fact 版本；v2 无图出生尚未写 fact 时 id 为 null。
func (s Service) GetFacts(ctx context.Context, productID string) (FactsResponse, error) {
	var out FactsResponse
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		product, err := loadProduct(ctx, pgxTx, productID)
		if err != nil {
			return err
		}
		out, err = factsResponse(ctx, pgxTx, product)
		return err
	})
	return out, err
}

// UpdateFacts 先锁商品再校验 expected version，然后写入新的不可变 fact 版本。
func (s Service) UpdateFacts(ctx context.Context, productID string, in UpdateFactsInput) (FactsResponse, error) {
	var out FactsResponse
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		product, err := loadProductForUpdate(ctx, pgxTx, productID)
		if err != nil {
			return err
		}
		current, err := loadCurrentFactSet(ctx, pgxTx, product)
		if err != nil {
			return err
		}
		if err := checkExpectedFactVersion(in, product, current); err != nil {
			return err
		}
		if in.Fields["name"] {
			name, err := normalizeName(deref(in.Name))
			if err != nil {
				return err
			}
			product.Name = name
		}
		if in.Fields["category"] {
			category, err := optionalText(deref(in.Category), "类目", 120)
			if err != nil {
				return err
			}
			product.Category = category
		}
		if in.Fields["price"] {
			price, err := normalizePrice(deref(in.Price))
			if err != nil {
				return err
			}
			product.Price = price
		}
		if in.Fields["source_note"] {
			note, err := optionalText(deref(in.SourceNote), "备注", 4000)
			if err != nil {
				return err
			}
			product.SourceNote = note
		}
		_, err = pfdb.Exec(ctx, pgxTx, `
			UPDATE products SET name = $1, category = $2, price = $3, source_note = $4, updated_at = NOW()
			WHERE id = $5
		`, product.Name, product.Category, product.Price, product.SourceNote, product.ID)
		if err != nil {
			return err
		}
		factPayload := currentFactMaps(current)
		if in.Facts != nil {
			factPayload, err = normalizeFactMaps(*in.Facts)
			if err != nil {
				return err
			}
		}
		if _, _, err := insertFactSet(ctx, pgxTx, product.ID, factPayload); err != nil {
			return err
		}
		product, err = loadProduct(ctx, pgxTx, product.ID)
		if err != nil {
			return err
		}
		out, err = factsResponse(ctx, pgxTx, product)
		return err
	})
	return out, err
}

func checkExpectedFactVersion(in UpdateFactsInput, product Product, current *FactSet) error {
	if current != nil && !in.ExpectedVersionProvided {
		return apperr.Conflict("保存商品资料必须携带当前 fact version")
	}
	if !in.ExpectedVersionProvided {
		return nil
	}
	if in.ExpectedFactSetVersionID != nil && (product.FactSetVersionID == nil || *product.FactSetVersionID != *in.ExpectedFactSetVersionID) {
		return apperr.Conflict("商品资料 fact version 已变化，请刷新后重试")
	}
	if in.ExpectedFactVersion != nil && (current == nil || current.Version != *in.ExpectedFactVersion) {
		return apperr.Conflict("商品资料 fact version 已变化，请刷新后重试")
	}
	if in.ExpectedFactSetVersionID == nil && in.ExpectedFactVersion == nil && current != nil {
		return apperr.Conflict("商品资料 fact version 已变化，请刷新后重试")
	}
	return nil
}

func factsResponse(ctx context.Context, tx *gorm.DB, product Product) (FactsResponse, error) {
	current, err := loadCurrentFactSet(ctx, tx, product)
	if err != nil {
		return FactsResponse{}, err
	}
	facts := []Fact{}
	if current != nil {
		facts = current.Facts
	}
	out := FactsResponse{
		Product: serializeDetail(product),
		Facts:   facts,
	}
	if current != nil {
		id := current.ID
		version := current.Version
		out.CurrentFactSetVersionID = &id
		out.CurrentFactVersion = &version
		out.FactSet = current
	}
	return out, nil
}

func loadCurrentFactSet(ctx context.Context, tx *gorm.DB, product Product) (*FactSet, error) {
	if product.FactSetVersionID == nil {
		return nil, nil
	}
	var set FactSet
	var payload []byte
	err := pfdb.QueryRow(ctx, tx, `
		SELECT id, product_id, version, payload_json, created_at
		FROM product_fact_set_versions WHERE id = $1
	`, *product.FactSetVersionID).Scan(&set.ID, &set.ProductID, &set.Version, &payload, &set.CreatedAt)
	if errors.Is(err, sqldb.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Facts []map[string]any `json:"facts"`
	}
	if err := json.Unmarshal(payload, &parsed); err != nil {
		return nil, err
	}
	set.Facts = factsFromMaps(parsed.Facts)
	return &set, nil
}

func factsFromMaps(items []map[string]any) []Fact {
	out := make([]Fact, 0, len(items))
	for _, item := range items {
		normalized, err := normalizeFactPayload(item)
		if err != nil {
			out = append(out, Fact{
				Key:              stringOr(item["key"], "unknown"),
				Value:            item["value"],
				SourceType:       stringOr(item["source_type"], "user"),
				Status:           stringOr(item["status"], "confirmed"),
				EvidenceAssetIDs: anyList(item["evidence_asset_ids"]),
				Conflicts:        anyList(item["conflicts"]),
			})
			continue
		}
		out = append(out, factFromMap(normalized))
	}
	return out
}

func factFromMap(item map[string]any) Fact {
	return Fact{
		Key:                  stringOr(item["key"], "unknown"),
		Value:                item["value"],
		SourceType:           stringOr(item["source_type"], "user"),
		Status:               stringOr(item["status"], "confirmed"),
		RequiresConfirmation: boolOr(item["requires_confirmation"]),
		EvidenceAssetIDs:     anyList(item["evidence_asset_ids"]),
		Conflicts:            anyList(item["conflicts"]),
	}
}

func currentFactMaps(current *FactSet) []map[string]any {
	if current == nil {
		return []map[string]any{}
	}
	out := make([]map[string]any, 0, len(current.Facts))
	for _, fact := range current.Facts {
		out = append(out, map[string]any{
			"key":                   fact.Key,
			"value":                 fact.Value,
			"source_type":           fact.SourceType,
			"status":                fact.Status,
			"requires_confirmation": fact.RequiresConfirmation,
			"evidence_asset_ids":    fact.EvidenceAssetIDs,
			"conflicts":             fact.Conflicts,
		})
	}
	return out
}

func normalizeFactMaps(items []map[string]any) ([]map[string]any, error) {
	seen := map[string]struct{}{}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		normalized, err := normalizeFactPayload(item)
		if err != nil {
			return nil, err
		}
		folded := strings.ToLower(stringOr(normalized["key"], ""))
		if _, ok := seen[folded]; ok {
			return nil, apperr.Validation("商品事实 key 不能重复")
		}
		seen[folded] = struct{}{}
		out = append(out, normalized)
	}
	return out, nil
}

func normalizeFactPayload(payload map[string]any) (map[string]any, error) {
	key := strings.TrimSpace(stringOr(payload["key"], ""))
	if key == "" || utf8.RuneCountInString(key) > 120 {
		return nil, apperr.Validation("商品事实 key 不能为空且不能超过 120 个字符")
	}
	return map[string]any{
		"key":                   key,
		"value":                 payload["value"],
		"source_type":           stringOr(payload["source_type"], "user"),
		"status":                stringOr(payload["status"], "confirmed"),
		"requires_confirmation": boolOr(payload["requires_confirmation"]),
		"evidence_asset_ids":    anyList(payload["evidence_asset_ids"]),
		"conflicts":             anyList(payload["conflicts"]),
	}, nil
}

func deref(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func stringOr(v any, fallback string) string {
	s, _ := v.(string)
	if s == "" {
		return fallback
	}
	return s
}

func boolOr(v any) bool {
	b, _ := v.(bool)
	return b
}

func anyList(v any) []any {
	switch typed := v.(type) {
	case []any:
		if typed == nil {
			return []any{}
		}
		return typed
	case nil:
		return []any{}
	default:
		return []any{}
	}
}
