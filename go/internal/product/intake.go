package product

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

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

func parseSelection(raw string) (Selection, error) {
	var selection Selection
	if err := json.Unmarshal([]byte(raw), &selection); err != nil {
		return Selection{}, apperr.Validation("图片类型选择不符合 AgentProductSelectionV1")
	}
	if selection.SchemaVersion != 1 {
		return Selection{}, apperr.Validation("图片类型选择不符合 AgentProductSelectionV1")
	}
	if len(selection.ImageTypes) == 0 {
		return Selection{}, apperr.Validation("图片类型选择不符合 AgentProductSelectionV1")
	}
	seen := map[string]struct{}{}
	for i, item := range selection.ImageTypes {
		if item.Order != i {
			return Selection{}, apperr.Validation("图片类型选择不符合 AgentProductSelectionV1")
		}
		if _, ok := seen[item.Key]; ok {
			return Selection{}, apperr.Validation("图片类型选择不符合 AgentProductSelectionV1")
		}
		seen[item.Key] = struct{}{}
	}
	return selection, nil
}

func parseDirectImageTypes(raw string) ([]graph.DirectCreateImageType, error) {
	var payload []map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil, apperr.Validation("图片类型必须是 JSON 数组")
	}
	out := make([]graph.DirectCreateImageType, 0, len(payload))
	for i, item := range payload {
		key, _ := item["key"].(string)
		if strings.TrimSpace(key) == "" {
			return nil, apperr.Validation("图片类型格式无效")
		}
		quantity := 1
		switch typed := item["quantity"].(type) {
		case float64:
			quantity = int(typed)
		case json.Number:
			n, _ := typed.Int64()
			quantity = int(n)
		}
		aspect, _ := item["aspect_ratio"].(string)
		out = append(out, graph.DirectCreateImageType{
			Key:         key,
			Quantity:    quantity,
			Order:       i,
			AspectRatio: aspect,
		})
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
