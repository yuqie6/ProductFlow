package product

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

// UpdateFactsInput 对应 PUT /v3/products/{id}/facts。
// Fields 记录 JSON 里实际出现的键，用来区分「未传」和「显式清空」。
type UpdateFactsInput struct {
	ExpectedFactSetVersionID *string
	ExpectedFactVersion      *int // 与 ExpectedFactSetVersionID 二选一做乐观锁
	ExpectedVersionProvided  bool // 区分「未传 expected」和「传了但值为 0」
	Name                     *string
	Category                 *string           // 仅当 Fields["category"] 时写入；可显式清空
	Price                    *string           // 仅当 Fields["price"] 时写入
	SourceNote               *string           // 仅当 Fields["source_note"] 时写入
	Facts                    *[]map[string]any // nil 表示不改 facts 数组
	Fields                   map[string]bool   // JSON 里实际出现的键
}

// GetFacts 返回当前选中的 fact 版本；v2 无图出生尚未写 fact 时 id 为 null。
// 找不到商品返回 NotFound。
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
// 缺商品返回 NotFound；expected version 不匹配返回 Conflict；字段非法返回 Validation。
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
		err = pgxTx.WithContext(ctx).Model(&schema.Products{}).Where("id = ?", product.ID).Updates(map[string]any{
			"name":        product.Name,
			"category":    product.Category,
			"price":       product.Price,
			"source_note": product.SourceNote,
			"updated_at":  time.Now().UTC(),
		}).Error
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

// factsResponse 无 current_fact_set_version_id 或版本行缺失时 id 为 null，不报 NotFound。
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

// loadCurrentFactSet 指向的版本行不存在时返回 nil，避免把悬挂指针当错误。
func loadCurrentFactSet(ctx context.Context, tx *gorm.DB, product Product) (*FactSet, error) {
	if product.FactSetVersionID == nil {
		return nil, nil
	}
	var rec schema.ProductFactSetVersions
	err := tx.WithContext(ctx).Where("id = ?", *product.FactSetVersionID).Take(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	set := FactSet{
		ID:        rec.ID,
		ProductID: rec.ProductID,
		Version:   rec.Version,
		CreatedAt: rec.CreatedAt,
	}
	payload := []byte(rec.PayloadJSON)
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
				Layer:            stringOr(item["layer"], "performance"),
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
		Layer:                stringOr(item["layer"], "performance"),
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
			"layer":                 fact.Layer,
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

// normalizeFactPayload 拒绝可选字段显式 null；source_type/status/layer 必须是闭集枚举。
// 分层闸：未确认推断不得升为 confirmed；营销口吻不得写入 performance 层。
func normalizeFactPayload(payload map[string]any) (map[string]any, error) {
	for _, key := range []string{"source_type", "status", "layer", "requires_confirmation", "evidence_asset_ids", "conflicts"} {
		if v, ok := payload[key]; ok && v == nil {
			return nil, apperr.Validation("请求体无效")
		}
	}
	key := strings.TrimSpace(stringOr(payload["key"], ""))
	if key == "" || utf8.RuneCountInString(key) > 120 {
		return nil, apperr.Validation("商品事实 key 不能为空且不能超过 120 个字符")
	}
	sourceType := "user"
	if v, ok := payload["source_type"]; ok && v != nil {
		s, ok := v.(string)
		if !ok {
			return nil, apperr.Validation("请求体无效")
		}
		s = strings.TrimSpace(s)
		if _, allowed := factSourceTypes[s]; !allowed {
			return nil, apperr.Validation("请求体无效")
		}
		sourceType = s
	}
	status := defaultFactStatus(sourceType)
	if v, ok := payload["status"]; ok && v != nil {
		s, ok := v.(string)
		if !ok {
			return nil, apperr.Validation("请求体无效")
		}
		s = strings.TrimSpace(s)
		if _, allowed := factStatuses[s]; !allowed {
			return nil, apperr.Validation("请求体无效")
		}
		status = s
	}
	layer := defaultFactLayer(key)
	if v, ok := payload["layer"]; ok && v != nil {
		s, ok := v.(string)
		if !ok {
			return nil, apperr.Validation("请求体无效")
		}
		s = strings.TrimSpace(s)
		if _, allowed := factLayers[s]; !allowed {
			return nil, apperr.Validation("请求体无效")
		}
		layer = s
	}
	requiresConfirmation := boolOr(payload["requires_confirmation"])
	if v, ok := payload["requires_confirmation"]; ok && v != nil {
		if _, ok := v.(bool); !ok {
			return nil, apperr.Validation("请求体无效")
		}
	}
	evidence := anyList(payload["evidence_asset_ids"])
	for _, item := range evidence {
		if _, ok := item.(string); !ok {
			return nil, apperr.Validation("请求体无效")
		}
	}
	conflicts := anyList(payload["conflicts"])
	for _, item := range conflicts {
		if _, ok := item.(map[string]any); !ok {
			return nil, apperr.Validation("请求体无效")
		}
	}
	requiresConfirmation, err := applyFactLayerGate(sourceType, status, layer, requiresConfirmation, conflicts, payload["value"])
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"key":                   key,
		"value":                 payload["value"],
		"source_type":           sourceType,
		"status":                status,
		"layer":                 layer,
		"requires_confirmation": requiresConfirmation,
		"evidence_asset_ids":    evidence,
		"conflicts":             conflicts,
	}, nil
}

// applyFactLayerGate 落实 IQ-CF-01 / CF-B0：确认门、冲突可见、营销与性能分栏。
func applyFactLayerGate(
	sourceType, status, layer string,
	requiresConfirmation bool,
	conflicts []any,
	value any,
) (bool, error) {
	if sourceType == "agent_inference" || sourceType == "image_observation" {
		if status == "confirmed" {
			return false, apperr.Validation("未确认的推断或图观事实不能升为已确认性能事实")
		}
		requiresConfirmation = true
	}
	if status == "confirmed" && requiresConfirmation {
		return false, apperr.Validation("已确认事实不能同时要求确认")
	}
	if status == "conflicted" {
		requiresConfirmation = true
		if len(conflicts) == 0 {
			return false, apperr.Validation("冲突事实必须附带可裁定的冲突说明")
		}
	}
	if layer == "performance" && looksLikeMarketingCopy(value) {
		return false, apperr.Validation("营销口吻不能写入性能事实")
	}
	return requiresConfirmation, nil
}

func defaultFactStatus(sourceType string) string {
	if sourceType == "agent_inference" || sourceType == "image_observation" {
		return "observed"
	}
	return "confirmed"
}

func defaultFactLayer(key string) string {
	folded := strings.ToLower(strings.TrimSpace(key))
	if _, ok := marketingFactKeys[folded]; ok {
		return "marketing"
	}
	return "performance"
}

func looksLikeMarketingCopy(value any) bool {
	text := strings.ToLower(strings.TrimSpace(factValueText(value)))
	if text == "" {
		return false
	}
	for _, marker := range marketingToneMarkers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func factValueText(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case float64, float32, int, int64, int32, bool:
		return fmt.Sprint(typed)
	default:
		raw, err := json.Marshal(typed)
		if err != nil {
			return ""
		}
		return string(raw)
	}
}

var factSourceTypes = map[string]struct{}{
	"user": {}, "image_observation": {}, "agent_inference": {},
}

var factStatuses = map[string]struct{}{
	"observed": {}, "user_declared": {}, "confirmed": {}, "conflicted": {},
}

var factLayers = map[string]struct{}{
	"performance": {}, "marketing": {},
}

var marketingFactKeys = map[string]struct{}{
	"selling_point": {}, "slogan": {}, "tagline": {}, "marketing_copy": {},
	"卖点": {}, "口号": {}, "营销文案": {},
}

var marketingToneMarkers = []string{
	"明星同款", "网红同款", "网红推荐", "爆款必入", "必入爆款", "种草神器",
	"celebrity same", "influencer pick", "viral must-have",
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
