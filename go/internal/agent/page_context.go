package agent

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/canonjson"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"gorm.io/gorm"
)

const (
	pageContextRouteMax     = 512
	pageContextPageTypeMax  = 80
	pageContextIDMax        = 64
	pageContextMaxAssetIDs  = 100
	pageContextMaxFilters   = 20
	pageContextFilterKeyMax = 80
	pageContextFilterValMax = 255
)

var pageContextAllowedKeys = map[string]struct{}{
	"route": {}, "page_type": {}, "product_id": {}, "workflow_id": {},
	"selected_asset_ids": {}, "visible_asset_ids": {}, "filters": {},
	"workflow_revision": {}, "library_revision": {}, "captured_at": {},
}

type normalizedPageContext struct {
	Route            string
	PageType         string
	ProductID        *string
	WorkflowID       *string
	SelectedAssetIDs []string
	VisibleAssetIDs  []string
	Filters          map[string]string
	WorkflowRevision *int
	LibraryRevision  *int
	CapturedAt       time.Time
	Digest           string
	SelectedJSON     []byte
	VisibleJSON      []byte
	FiltersJSON      []byte
}

func normalizePageContext(value map[string]any) (normalizedPageContext, error) {
	if value == nil {
		return normalizedPageContext{}, apperr.Validation("页面上下文缺少 route 或 page_type")
	}
	for key := range value {
		if _, ok := pageContextAllowedKeys[key]; !ok {
			return normalizedPageContext{}, apperr.Validation("页面上下文包含未知字段")
		}
	}
	route, err := boundedPageString(value["route"], pageContextRouteMax, "页面 route")
	if err != nil {
		return normalizedPageContext{}, err
	}
	pageType, err := boundedPageString(value["page_type"], pageContextPageTypeMax, "页面类型")
	if err != nil {
		return normalizedPageContext{}, err
	}
	productID, err := optionalPageIdentifier(value["product_id"], "product_id")
	if err != nil {
		return normalizedPageContext{}, err
	}
	workflowID, err := optionalPageIdentifier(value["workflow_id"], "workflow_id")
	if err != nil {
		return normalizedPageContext{}, err
	}
	selected, err := boundedPageIdentifiers(value["selected_asset_ids"], "selected_asset_ids")
	if err != nil {
		return normalizedPageContext{}, err
	}
	visible, err := boundedPageIdentifiers(value["visible_asset_ids"], "visible_asset_ids")
	if err != nil {
		return normalizedPageContext{}, err
	}
	filters, err := normalizePageFilters(value["filters"])
	if err != nil {
		return normalizedPageContext{}, err
	}
	workflowRevision, err := optionalNonNegativeInt(value["workflow_revision"], "workflow_revision")
	if err != nil {
		return normalizedPageContext{}, err
	}
	libraryRevision, err := optionalNonNegativeInt(value["library_revision"], "library_revision")
	if err != nil {
		return normalizedPageContext{}, err
	}
	capturedAt, err := parsePageCapturedAt(value["captured_at"])
	if err != nil {
		return normalizedPageContext{}, err
	}
	out := normalizedPageContext{
		Route: route, PageType: pageType, ProductID: productID, WorkflowID: workflowID,
		SelectedAssetIDs: selected, VisibleAssetIDs: visible, Filters: filters,
		WorkflowRevision: workflowRevision, LibraryRevision: libraryRevision, CapturedAt: capturedAt,
	}
	selectedJSON, err := json.Marshal(selected)
	if err != nil {
		return normalizedPageContext{}, err
	}
	visibleJSON, err := json.Marshal(visible)
	if err != nil {
		return normalizedPageContext{}, err
	}
	filtersJSON, err := json.Marshal(filters)
	if err != nil {
		return normalizedPageContext{}, err
	}
	digest, err := pageContextDigest(out)
	if err != nil {
		return normalizedPageContext{}, err
	}
	out.SelectedJSON, out.VisibleJSON, out.FiltersJSON, out.Digest = selectedJSON, visibleJSON, filtersJSON, digest
	return out, nil
}

func pageContextDigest(value normalizedPageContext) (string, error) {
	return canonjson.SHA256Hex(map[string]any{
		"route":              value.Route,
		"page_type":          value.PageType,
		"product_id":         value.ProductID,
		"workflow_id":        value.WorkflowID,
		"selected_asset_ids": value.SelectedAssetIDs,
		"visible_asset_ids":  value.VisibleAssetIDs,
		"filters":            value.Filters,
		"workflow_revision":  value.WorkflowRevision,
		"library_revision":   value.LibraryRevision,
		"captured_at":        isoCapturedAt(value.CapturedAt),
	})
}

func insertPageContext(ctx context.Context, pgxTx *gorm.DB, taskID *string, turnID string, pageContext map[string]any) error {
	normalized, err := normalizePageContext(pageContext)
	if err != nil {
		return err
	}
	snapshotID := newID()
	now := time.Now().UTC()
	snap := schema.AgentPageContextSnapshots{
		ID:                   snapshotID,
		TaskID:               taskID,
		TurnID:               &turnID,
		Route:                normalized.Route,
		PageType:             normalized.PageType,
		ProductID:            normalized.ProductID,
		WorkflowID:           normalized.WorkflowID,
		SelectedAssetIdsJSON: string(normalized.SelectedJSON),
		VisibleAssetIdsJSON:  string(normalized.VisibleJSON),
		FiltersJSON:          string(normalized.FiltersJSON),
		WorkflowRevision:     normalized.WorkflowRevision,
		LibraryRevision:      normalized.LibraryRevision,
		Digest:               normalized.Digest,
		CapturedAt:           normalized.CapturedAt,
		CreatedAt:            now,
	}
	if err := pgxTx.WithContext(ctx).Create(&snap).Error; err != nil {
		return err
	}
	return pgxTx.WithContext(ctx).Model(&schema.AgentTurnProjections{}).Where("id = ?", turnID).Updates(map[string]any{
		"page_context_snapshot_id": snapshotID,
	}).Error
}

func loadPageContextPayload(ctx context.Context, pgxTx *gorm.DB, snapshotID string) (map[string]any, error) {
	var snap schema.AgentPageContextSnapshots
	if err := pgxTx.WithContext(ctx).Where("id = ?", snapshotID).Take(&snap).Error; err != nil {
		return nil, err
	}
	route, pageType, digest := snap.Route, snap.PageType, snap.Digest
	productID, workflowID := snap.ProductID, snap.WorkflowID
	selectedRaw, visibleRaw, filtersRaw := []byte(snap.SelectedAssetIdsJSON), []byte(snap.VisibleAssetIdsJSON), []byte(snap.FiltersJSON)
	workflowRevision, libraryRevision := snap.WorkflowRevision, snap.LibraryRevision
	capturedAt := snap.CapturedAt
	selected := []string{}
	if len(selectedRaw) > 0 && string(selectedRaw) != "null" {
		if err := json.Unmarshal(selectedRaw, &selected); err != nil {
			return nil, err
		}
	}
	visible := []string{}
	if len(visibleRaw) > 0 && string(visibleRaw) != "null" {
		if err := json.Unmarshal(visibleRaw, &visible); err != nil {
			return nil, err
		}
	}
	filters := map[string]string{}
	if len(filtersRaw) > 0 && string(filtersRaw) != "null" {
		if err := json.Unmarshal(filtersRaw, &filters); err != nil {
			return nil, err
		}
	}
	if selected == nil {
		selected = []string{}
	}
	if visible == nil {
		visible = []string{}
	}
	if filters == nil {
		filters = map[string]string{}
	}
	return map[string]any{
		"snapshot_id":        snapshotID,
		"route":              route,
		"page_type":          pageType,
		"product_id":         derefString(productID),
		"workflow_id":        derefString(workflowID),
		"selected_asset_ids": selected,
		"visible_asset_ids":  visible,
		"filters":            filters,
		"workflow_revision":  derefInt(workflowRevision),
		"library_revision":   derefInt(libraryRevision),
		"digest":             digest,
		"captured_at":        isoCapturedAt(capturedAt),
	}, nil
}

func derefString(v *string) any {
	if v == nil {
		return nil
	}
	return *v
}

func derefInt(v *int) any {
	if v == nil {
		return nil
	}
	return *v
}

func boundedPageString(value any, limit int, label string) (string, error) {
	text, ok := value.(string)
	if !ok || strings.TrimSpace(text) == "" {
		return "", apperr.Validation(label + "不能为空")
	}
	normalized := strings.TrimSpace(text)
	if utf8.RuneCountInString(normalized) > limit {
		return "", apperr.Validationf("%s不能超过 %d 个字符", label, limit)
	}
	return normalized, nil
}

func optionalPageIdentifier(value any, label string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	text, err := boundedPageString(value, pageContextIDMax, label)
	if err != nil {
		return nil, err
	}
	return &text, nil
}

func boundedPageIdentifiers(value any, label string) ([]string, error) {
	if value == nil {
		return []string{}, nil
	}
	raw, ok := value.([]any)
	if !ok {
		if typed, ok := value.([]string); ok {
			out := make([]string, 0, len(typed))
			seen := map[string]struct{}{}
			if len(typed) > pageContextMaxAssetIDs {
				return nil, apperr.Validation("页面 " + label + " 必须是有界 ID 列表")
			}
			for _, item := range typed {
				id, err := boundedPageString(item, pageContextIDMax, label)
				if err != nil {
					return nil, err
				}
				if _, dup := seen[id]; dup {
					return nil, apperr.Validation("页面 " + label + " 不能包含重复 ID")
				}
				seen[id] = struct{}{}
				out = append(out, id)
			}
			return out, nil
		}
		return nil, apperr.Validation("页面 " + label + " 必须是有界 ID 列表")
	}
	if len(raw) > pageContextMaxAssetIDs {
		return nil, apperr.Validation("页面 " + label + " 必须是有界 ID 列表")
	}
	out := make([]string, 0, len(raw))
	seen := map[string]struct{}{}
	for _, item := range raw {
		id, err := boundedPageString(item, pageContextIDMax, label)
		if err != nil {
			return nil, err
		}
		if _, dup := seen[id]; dup {
			return nil, apperr.Validation("页面 " + label + " 不能包含重复 ID")
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out, nil
}

func normalizePageFilters(value any) (map[string]string, error) {
	if value == nil {
		return map[string]string{}, nil
	}
	raw, ok := value.(map[string]any)
	if !ok {
		return nil, apperr.Validation("页面 filters 必须是有界对象")
	}
	if len(raw) > pageContextMaxFilters {
		return nil, apperr.Validation("页面 filters 必须是有界对象")
	}
	out := map[string]string{}
	for key, item := range raw {
		normalizedKey, err := boundedPageString(key, pageContextFilterKeyMax, "页面 filter key")
		if err != nil {
			return nil, err
		}
		text, ok := item.(string)
		if !ok {
			return nil, apperr.Validation("页面 filter value不能为空")
		}
		normalizedValue, err := boundedPageString(text, pageContextFilterValMax, "页面 filter value")
		if err != nil {
			return nil, err
		}
		out[normalizedKey] = normalizedValue
	}
	return out, nil
}

func optionalNonNegativeInt(value any, label string) (*int, error) {
	if value == nil {
		return nil, nil
	}
	switch typed := value.(type) {
	case int:
		if typed < 0 {
			return nil, apperr.Validation(label + " 必须是非负整数")
		}
		return &typed, nil
	case int64:
		if typed < 0 {
			return nil, apperr.Validation(label + " 必须是非负整数")
		}
		n := int(typed)
		return &n, nil
	case float64:
		if typed < 0 || typed != math.Trunc(typed) {
			return nil, apperr.Validation(label + " 必须是非负整数")
		}
		n := int(typed)
		return &n, nil
	case json.Number:
		n, err := typed.Int64()
		if err != nil || n < 0 {
			return nil, apperr.Validation(label + " 必须是非负整数")
		}
		out := int(n)
		return &out, nil
	default:
		return nil, apperr.Validation(label + " 必须是非负整数")
	}
}

func parsePageCapturedAt(value any) (time.Time, error) {
	switch typed := value.(type) {
	case time.Time:
		if typed.IsZero() {
			return time.Time{}, apperr.Validation("页面 captured_at 必须是时间")
		}
		return typed.UTC(), nil
	case string:
		text := strings.TrimSpace(typed)
		if text == "" {
			return time.Time{}, apperr.Validation("页面 captured_at 必须是时间")
		}
		parsed, err := time.Parse(time.RFC3339Nano, text)
		if err != nil {
			parsed, err = time.Parse(time.RFC3339, text)
		}
		if err != nil {
			return time.Time{}, apperr.Validation("页面 captured_at 无效")
		}
		return parsed.UTC(), nil
	default:
		return time.Time{}, apperr.Validation("页面 captured_at 必须是时间")
	}
}

func isoCapturedAt(t time.Time) string {
	t = t.UTC()
	if t.Nanosecond() == 0 {
		return t.Format("2006-01-02T15:04:05") + "+00:00"
	}
	return t.Format("2006-01-02T15:04:05.000000") + "+00:00"
}
