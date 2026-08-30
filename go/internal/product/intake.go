package product

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

const (
	intakeMinImagesPerType  = 1
	intakeMaxImagesPerType  = 6
	intakeMaxTotalImages    = 30
	selectionInvalidDetail  = "图片类型选择不符合 AgentProductSelectionV1"
	directTypesArrayDetail  = "图片类型必须是 JSON 数组"
	directTypesFormatDetail = "图片类型格式无效"
)

var aspectRatioPattern = regexp.MustCompile(`^[1-9][0-9]{0,2}:[1-9][0-9]{0,2}$`)

var lookupDeliveryPresetSpec func(key string) (map[string]any, error)

// BindDeliveryPresetSpec 由 delivery 包注入 GetPreset+SpecAsMap，避免 product→delivery 循环依赖。
func BindDeliveryPresetSpec(fn func(key string) (map[string]any, error)) {
	lookupDeliveryPresetSpec = fn
}

type ImageTypeSelection struct {
	Key      string `json:"key"`
	Quantity int    `json:"quantity"`
	Order    int    `json:"order"`
}

type Selection struct {
	SchemaVersion     int                  `json:"schema_version"`
	ImageTypes        []ImageTypeSelection `json:"image_types"`
	DeliveryPresetKey *string              `json:"delivery_preset_key"`
}

func catalogKeySet() map[string]struct{} {
	out := map[string]struct{}{}
	for _, item := range graph.ImageTypeCatalogJSON() {
		key, _ := item["key"].(string)
		if key != "" {
			out[key] = struct{}{}
		}
	}
	return out
}

func deliveryPresetSpec(key string) (map[string]any, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, nil
	}
	if lookupDeliveryPresetSpec == nil {
		return nil, apperr.Validation("未知 DeliverySpec 预设: " + key)
	}
	spec, err := lookupDeliveryPresetSpec(key)
	if err != nil || spec == nil {
		return nil, apperr.Validation("未知 DeliverySpec 预设: " + key)
	}
	return spec, nil
}

func selectionDeliverySpec(selection Selection) (map[string]any, error) {
	if selection.DeliveryPresetKey == nil {
		return nil, nil
	}
	return deliveryPresetSpec(*selection.DeliveryPresetKey)
}

func parseSelection(raw string) (Selection, error) {
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	var selection Selection
	if err := dec.Decode(&selection); err != nil {
		return Selection{}, apperr.Validation(selectionInvalidDetail)
	}
	if dec.More() {
		return Selection{}, apperr.Validation(selectionInvalidDetail)
	}
	if selection.SchemaVersion != 1 {
		return Selection{}, apperr.Validation(selectionInvalidDetail)
	}
	catalog := catalogKeySet()
	if len(selection.ImageTypes) == 0 || len(selection.ImageTypes) > len(catalog) {
		return Selection{}, apperr.Validation(selectionInvalidDetail)
	}
	seen := map[string]struct{}{}
	total := 0
	for i, item := range selection.ImageTypes {
		if item.Order != i {
			return Selection{}, apperr.Validation(selectionInvalidDetail)
		}
		key := strings.TrimSpace(item.Key)
		if _, ok := catalog[key]; !ok {
			return Selection{}, apperr.Validation(selectionInvalidDetail)
		}
		if item.Quantity < intakeMinImagesPerType || item.Quantity > intakeMaxImagesPerType {
			return Selection{}, apperr.Validation(selectionInvalidDetail)
		}
		if _, ok := seen[key]; ok {
			return Selection{}, apperr.Validation(selectionInvalidDetail)
		}
		seen[key] = struct{}{}
		selection.ImageTypes[i].Key = key
		total += item.Quantity
	}
	if total > intakeMaxTotalImages {
		return Selection{}, apperr.Validation(selectionInvalidDetail)
	}
	if selection.DeliveryPresetKey != nil {
		key := strings.TrimSpace(*selection.DeliveryPresetKey)
		if key == "" || len(key) > 80 {
			return Selection{}, apperr.Validation(selectionInvalidDetail)
		}
		if _, err := deliveryPresetSpec(key); err != nil {
			return Selection{}, apperr.Validation(selectionInvalidDetail)
		}
		selection.DeliveryPresetKey = &key
	}
	return selection, nil
}

func parseDirectImageTypes(raw string) ([]graph.DirectCreateImageType, error) {
	dec := json.NewDecoder(strings.NewReader(raw))
	var items []json.RawMessage
	if err := dec.Decode(&items); err != nil {
		return nil, apperr.Validation(directTypesArrayDetail)
	}
	if dec.More() {
		return nil, apperr.Validation(directTypesArrayDetail)
	}
	catalog := catalogKeySet()
	out := make([]graph.DirectCreateImageType, 0, len(items))
	unknown := make([]string, 0)
	total := 0
	for i, rawItem := range items {
		itemDec := json.NewDecoder(bytes.NewReader(rawItem))
		itemDec.DisallowUnknownFields()
		var item struct {
			Key         string  `json:"key"`
			Quantity    int     `json:"quantity"`
			AspectRatio *string `json:"aspect_ratio"`
		}
		if err := itemDec.Decode(&item); err != nil || itemDec.More() {
			return nil, apperr.Validation(directTypesFormatDetail)
		}
		key := strings.TrimSpace(item.Key)
		if key == "" {
			return nil, apperr.Validation(directTypesFormatDetail)
		}
		if item.Quantity < intakeMinImagesPerType || item.Quantity > intakeMaxImagesPerType {
			return nil, apperr.Validation(directTypesFormatDetail)
		}
		if item.AspectRatio != nil && !aspectRatioPattern.MatchString(*item.AspectRatio) {
			return nil, apperr.Validation(directTypesFormatDetail)
		}
		if _, ok := catalog[key]; !ok {
			unknown = append(unknown, key)
		}
		aspect := ""
		if item.AspectRatio != nil {
			aspect = *item.AspectRatio
		}
		if graph.ImageTypeFamily(key) != "evidence" {
			total += item.Quantity
		}
		out = append(out, graph.DirectCreateImageType{
			Key:         key,
			Quantity:    item.Quantity,
			Order:       i,
			AspectRatio: aspect,
		})
	}
	if len(unknown) > 0 {
		return nil, apperr.Validation("不支持的图片类型: " + strings.Join(unknown, ", "))
	}
	if total > intakeMaxTotalImages {
		return nil, apperr.Validation("图片生成总数不能超过 30")
	}
	return out, nil
}

func parseGenerationSpec(raw string) (map[string]any, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil, apperr.Validation("出图设定必须是 JSON 对象")
	}
	return payload, nil
}

func normalizeIdempotencyKey(value string) (string, error) {
	normalized := strings.TrimSpace(value)
	if normalized == "" {
		return "", apperr.Validation("Idempotency-Key 不能为空")
	}
	if len([]byte(normalized)) > 200 {
		return "", apperr.Validation("Idempotency-Key 不能超过 200 bytes")
	}
	return normalized, nil
}

func draftRequestHash(name string, sessionID *string) string {
	payload := map[string]any{
		"request_kind": "agent_product_draft_workspace_v1",
		"product_name": name,
	}
	if sessionID != nil {
		payload["agent_session_id"] = *sessionID
	}
	raw, _ := canonicalJSON(payload)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func workspaceRequestHash(name string, selection Selection, uploads []Upload, sessionID *string) string {
	images := make([]map[string]any, 0, len(uploads))
	for i, upload := range uploads {
		sum := sha256.Sum256(upload.Content)
		images = append(images, map[string]any{
			"order":     i,
			"filename":  upload.Filename,
			"mime_type": upload.MIMEType,
			"sha256":    hex.EncodeToString(sum[:]),
		})
	}
	selectionPayload := map[string]any{
		"schema_version": selection.SchemaVersion,
		"image_types":    selection.ImageTypes,
	}
	if selection.DeliveryPresetKey != nil {
		selectionPayload["delivery_preset_key"] = *selection.DeliveryPresetKey
	}
	payload := map[string]any{
		"product_name": name,
		"selection":    selectionPayload,
		"images":       images,
	}
	if sessionID != nil {
		payload["agent_session_id"] = *sessionID
	}
	raw, _ := canonicalJSON(payload)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func intakeRequestHash(selection Selection, uploads []Upload) string {
	images := make([]map[string]any, 0, len(uploads))
	for i, upload := range uploads {
		sum := sha256.Sum256(upload.Content)
		images = append(images, map[string]any{
			"order":     i,
			"filename":  upload.Filename,
			"mime_type": upload.MIMEType,
			"sha256":    hex.EncodeToString(sum[:]),
		})
	}
	selectionPayload := map[string]any{
		"schema_version": selection.SchemaVersion,
		"image_types":    selection.ImageTypes,
	}
	if selection.DeliveryPresetKey != nil {
		selectionPayload["delivery_preset_key"] = *selection.DeliveryPresetKey
	}
	payload := map[string]any{
		"request_kind": "agent_product_intake_finalization_v1",
		"selection":    selectionPayload,
		"images":       images,
	}
	raw, _ := canonicalJSON(payload)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func intakePayload(selection Selection, assetIDs []string) ([]byte, error) {
	imageTypes := make([]map[string]any, 0, len(selection.ImageTypes))
	for _, item := range selection.ImageTypes {
		imageTypes = append(imageTypes, map[string]any{
			"key":      item.Key,
			"quantity": item.Quantity,
			"order":    item.Order,
		})
	}
	payload := map[string]any{
		"schema_version":      1,
		"image_types":         imageTypes,
		"reference_asset_ids": assetIDs,
	}
	if selection.DeliveryPresetKey != nil {
		payload["delivery_preset_key"] = *selection.DeliveryPresetKey
		spec, err := deliveryPresetSpec(*selection.DeliveryPresetKey)
		if err != nil {
			return nil, err
		}
		if spec != nil {
			payload["delivery_spec"] = spec
		}
	}
	return canonicalJSON(payload)
}

func selectionToImageTypes(selection Selection) []graph.DirectCreateImageType {
	out := make([]graph.DirectCreateImageType, 0, len(selection.ImageTypes))
	for _, item := range selection.ImageTypes {
		out = append(out, graph.DirectCreateImageType{
			Key:      item.Key,
			Quantity: item.Quantity,
			Order:    item.Order,
		})
	}
	return out
}

// ApplyIntake 把图片类型选择与参考图写入商品 intake。
func (s Service) ApplyIntake(ctx context.Context, productID string, selectionJSON []byte, assetIDs []string) (json.RawMessage, error) {
	selection, err := parseSelection(string(selectionJSON))
	if err != nil {
		return nil, err
	}
	if len(assetIDs) == 0 {
		return nil, apperr.Validation("至少选择一张参考图")
	}
	if len(assetIDs) > 6 {
		return nil, apperr.Validation("参考图最多上传 6 张")
	}
	payload, err := intakePayload(selection, assetIDs)
	if err != nil {
		return nil, err
	}
	err = tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		if _, err := loadProduct(ctx, pgxTx, productID); err != nil {
			return err
		}
		return setIntake(ctx, pgxTx, productID, payload)
	})
	if err != nil {
		return nil, err
	}
	return payload, nil
}
