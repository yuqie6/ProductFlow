package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

func (s Service) LaunchWorkspaceFromGlobal(ctx context.Context, globalConversationID, name, idempotencyKey string) (WorkspaceLaunchResponse, error) {
	conv, err := s.loadScopedConversation(ctx, globalConversationID)
	if err != nil {
		return WorkspaceLaunchResponse{}, err
	}
	if err := requireGlobalScope(conv); err != nil {
		return WorkspaceLaunchResponse{}, err
	}
	created, err := s.Product.CreateAgentDraft(ctx, name, idempotencyKey, nil)
	if err != nil {
		return WorkspaceLaunchResponse{}, err
	}
	if created.Conversation.SessionID == nil {
		return WorkspaceLaunchResponse{}, apperr.Conflict("Agent 商品工作区缺少 Session")
	}
	return WorkspaceLaunchResponse{
		SchemaVersion: 1, Created: created.Created, SessionID: *created.Conversation.SessionID,
		GlobalConversationID: globalConversationID, ProductConversationID: created.Conversation.ID,
		ProductID: created.Product.ID, ProductName: created.Product.Name, TaskID: created.TaskID,
		IntakeFinalized: created.IntakeFinalized,
		NavigationPath:  "/products/" + url.PathEscape(created.Product.ID) + "?agent_session_id=" + url.PathEscape(*created.Conversation.SessionID),
	}, nil
}

func (s Service) ReconcileWorkspaceFromGlobal(ctx context.Context, globalConversationID, name, idempotencyKey string) (ReconcileResponse, error) {
	if _, err := s.loadScopedConversation(ctx, globalConversationID); err != nil {
		return ReconcileResponse{}, err
	}
	key, err := normalizeIdempotency(idempotencyKey, "idempotency key")
	if err != nil {
		return ReconcileResponse{}, err
	}
	var rec schema.AgentConversations
	err = s.DB.WithContext(ctx).Select("id").Where("creation_idempotency_key = ?", key).Take(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ReconcileResponse{State: "not_applied", Detail: ptr("商品工作区尚未创建")}, nil
	}
	if err != nil {
		return ReconcileResponse{}, err
	}
	launch, err := s.LaunchWorkspaceFromGlobal(ctx, globalConversationID, name, key)
	if err != nil {
		var appErr apperr.Error
		if errors.As(err, &appErr) && appErr.Status == 409 {
			return ReconcileResponse{State: "conflict", Detail: ptr(appErr.Detail)}, nil
		}
		return ReconcileResponse{State: "unknown", Detail: ptr("商品工作区对账结果仍不明确")}, nil
	}
	raw, _ := json.Marshal(launch)
	return ReconcileResponse{State: "applied", Result: raw, Detail: ptr("商品工作区已创建")}, nil
}

func (s Service) FinalizeProductIntake(ctx context.Context, conversationID, idempotencyKey string, selection json.RawMessage, assetIDs []string) (map[string]any, error) {
	ids, err := normalizeAssetIDs(assetIDs)
	if err != nil {
		return nil, err
	}
	before := map[string]any{}
	target := map[string]any{"selection": json.RawMessage(selection), "reference_asset_ids": ids}
	replay, found, err := s.lookupMutation(ctx, conversationID, "finalize_product_intake_v1", idempotencyKey, "finalize_product_intake_v1", before, target)
	if err != nil {
		return nil, err
	}
	if found {
		return replay, nil
	}
	conv, err := s.loadScopedConversation(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	if err := requireProductWorkflow(conv); err != nil {
		return nil, err
	}
	intake, err := s.Product.ApplyIntake(ctx, *conv.ProductID, selection, ids)
	if err != nil {
		return nil, err
	}
	var intakeObj any
	_ = json.Unmarshal(intake.Intake, &intakeObj)
	result := map[string]any{
		"schema_version": 1, "accepted": true, "intake_finalized": true,
		"product_id": *conv.ProductID, "reference_asset_ids": ids, "intake": intakeObj,
		"graph_expanded": intake.GraphExpanded, "revision": intake.Revision,
		"node_count": intake.NodeCount, "group_count": intake.GroupCount,
	}
	if err := s.recordMutation(ctx, conversationID, "finalize_product_intake_v1", idempotencyKey, "finalize_product_intake_v1", before, target, result, nil); err != nil {
		return nil, err
	}
	return result, nil
}

func (s Service) ListGlobalProducts(ctx context.Context, conversationID, query, cursor string, limit int) (GlobalProductListResponse, error) {
	conv, err := s.loadScopedConversation(ctx, conversationID)
	if err != nil {
		return GlobalProductListResponse{}, err
	}
	if err := requireGlobalScope(conv); err != nil {
		return GlobalProductListResponse{}, err
	}
	if limit < 1 || limit > globalProductListMax {
		return GlobalProductListResponse{}, apperr.Validationf("商品分页 limit 必须在 1 到 %d 之间", globalProductListMax)
	}
	normalized := stringsTrim(query)
	if len([]rune(normalized)) > 255 {
		return GlobalProductListResponse{}, apperr.Validation("商品搜索词不能超过 255 个字符")
	}
	q := s.DB.WithContext(ctx).Model(&schema.Products{}).
		Select("id, name, category, updated_at").
		Where("? = '' OR name ILIKE '%' || ? || '%'", normalized, normalized)
	if cursor != "" {
		var decoded struct {
			UpdatedAt string `json:"updated_at"`
			ProductID string `json:"product_id"`
			Query     string `json:"query"`
		}
		if err := decodeCursor(cursor, &decoded, "商品分页 cursor 无效或与当前查询条件不匹配"); err != nil {
			return GlobalProductListResponse{}, err
		}
		if decoded.Query != normalized {
			return GlobalProductListResponse{}, apperr.Validation("商品分页 cursor 无效或与当前查询条件不匹配")
		}
		updated, err := time.Parse(time.RFC3339Nano, decoded.UpdatedAt)
		if err != nil {
			updated, err = time.Parse(time.RFC3339, decoded.UpdatedAt)
			if err != nil {
				return GlobalProductListResponse{}, apperr.Validation("商品分页 cursor 无效或与当前查询条件不匹配")
			}
		}
		q = q.Where("updated_at < ? OR (updated_at = ? AND id < ?)", updated, updated, decoded.ProductID)
	}
	var rows []schema.Products
	if err := q.Order("updated_at DESC, id DESC").Limit(limit + 1).Find(&rows).Error; err != nil {
		return GlobalProductListResponse{}, err
	}
	var products []GlobalProductResponse
	for _, row := range rows {
		products = append(products, GlobalProductResponse{
			ID: row.ID, Name: row.Name, Category: row.Category,
			UpdatedAt: row.UpdatedAt.UTC().Format(time.RFC3339Nano),
		})
	}
	hasMore := len(products) > limit
	if hasMore {
		products = products[:limit]
	}
	for i := range products {
		products[i].ActiveWorkflow = s.activeWorkflowSummary(ctx, products[i].ID)
	}
	out := GlobalProductListResponse{Items: products}
	if hasMore && len(products) > 0 {
		last := products[len(products)-1]
		encoded, err := encodeCursor(map[string]any{
			"updated_at": last.UpdatedAt, "product_id": last.ID, "query": normalized,
		})
		if err != nil {
			return GlobalProductListResponse{}, err
		}
		out.NextCursor = &encoded
	}
	return out, nil
}

func (s Service) InspectGlobalProducts(ctx context.Context, conversationID string, productIDs []string) ([]GlobalProductResponse, error) {
	conv, err := s.loadScopedConversation(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	if err := requireGlobalScope(conv); err != nil {
		return nil, err
	}
	if len(productIDs) < 1 || len(productIDs) > 20 {
		return nil, apperr.Validation("product_ids 必须包含 1 到 20 个商品")
	}
	out := make([]GlobalProductResponse, 0, len(productIDs))
	seen := map[string]struct{}{}
	for _, id := range productIDs {
		id = stringsTrim(id)
		if id == "" {
			return nil, apperr.Validation("product_ids 不能包含空值")
		}
		if _, ok := seen[id]; ok {
			return nil, apperr.Validation("product_ids 不能包含重复值")
		}
		seen[id] = struct{}{}
		detail, err := s.Product.Get(ctx, id)
		if err != nil {
			return nil, apperr.NotFound("部分商品不存在")
		}
		item := GlobalProductResponse{ID: detail.ID, Name: detail.Name, Category: detail.Category, UpdatedAt: detail.UpdatedAt.UTC().Format(time.RFC3339Nano)}
		item.ActiveWorkflow = s.activeWorkflowSummary(ctx, detail.ID)
		out = append(out, item)
	}
	return out, nil
}

func (s Service) activeWorkflowSummary(ctx context.Context, productID string) *map[string]any {
	live, err := s.Graph.TryCurrent(ctx, productID)
	if err != nil || live == nil {
		return nil
	}
	summary := map[string]any{
		"id": live.ID, "title": live.Title, "revision": live.Revision,
		"edit_version": live.Revision, "node_count": len(live.Nodes),
	}
	return &summary
}

func (s Service) GlobalWorkflowContext(ctx context.Context, conversationID, productID, responseFormat string) (map[string]any, error) {
	productID = stringsTrim(productID)
	if productID == "" {
		return nil, apperr.Validation("请求参数无效")
	}
	conv, err := s.loadScopedConversation(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	if err := requireGlobalScope(conv); err != nil {
		return nil, err
	}
	if _, err := s.Product.Get(ctx, productID); err != nil {
		return nil, err
	}
	var target schema.AgentConversations
	err = s.DB.WithContext(ctx).Select("id").
		Where("scope_type = ? AND product_id = ?", "product_workflow", productID).
		Order("updated_at DESC, id DESC").
		Take(&target).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, apperr.NotFound("商品没有 Agent 工作区")
	}
	if err != nil {
		return nil, err
	}
	targetID := target.ID
	payload, err := s.ProductContext(ctx, targetID, responseFormat)
	if err != nil {
		return nil, err
	}
	payload["target"] = map[string]any{
		"product_id": productID, "product_conversation_id": targetID,
	}
	return payload, nil
}

func (s Service) ValidateLibraryDraft(ctx context.Context, conversationID string, value json.RawMessage) error {
	conv, err := s.loadScopedConversation(ctx, conversationID)
	if err != nil {
		return err
	}
	if err := requireGlobalScope(conv); err != nil {
		return err
	}
	var body map[string]any
	if err := json.Unmarshal(value, &body); err != nil {
		return apperr.Validation("素材整理 Draft payload 无效")
	}
	if _, ok := body["operations"]; !ok {
		return apperr.Validation("素材整理 Draft payload 无效")
	}
	return nil
}

func (s Service) ValidateGlobalDraft(ctx context.Context, conversationID string, value json.RawMessage) error {
	conv, err := s.loadScopedConversation(ctx, conversationID)
	if err != nil {
		return err
	}
	if err := requireGlobalScope(conv); err != nil {
		return err
	}
	var body map[string]any
	if err := json.Unmarshal(value, &body); err != nil {
		return apperr.Validation("全局 Agent Draft 无效")
	}
	kind, _ := body["draft_kind"].(string)
	if kind != "" && kind != "library_organization" {
		return apperr.Validation("全局 Agent Draft 无效: 当前只接受素材整理")
	}
	payload := value
	if inner, ok := body["library_payload"]; ok {
		payload, _ = json.Marshal(inner)
	}
	return s.ValidateLibraryDraft(ctx, conversationID, payload)
}

func (s Service) ConfirmLibraryDraftHTTP(ctx context.Context, conversationID string, expectedVersion int, idempotencyKey string) (any, error) {
	key := strings.TrimSpace(idempotencyKey)
	if key == "" {
		return nil, apperr.Validation("idempotency key 不能为空")
	}
	var out any
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		conv, err := loadConversationByID(ctx, pgxTx, conversationID)
		if err != nil {
			return err
		}
		if err := requireGlobalScope(conv); err != nil {
			return err
		}
		draft, err := s.Library.ConfirmOrganizationDraftTx(ctx, pgxTx, conversationID, expectedVersion, key)
		if err != nil {
			return err
		}
		if err := completeOrganizationDraftTask(ctx, pgxTx, draft); err != nil {
			return err
		}
		out = draft
		return nil
	})
	return out, err
}

func (s Service) GetLibraryDraftHTTP(ctx context.Context, conversationID string) (any, error) {
	if _, err := s.loadScopedConversation(ctx, conversationID); err != nil {
		return nil, err
	}
	return s.Library.GetOrganizationDraft(ctx, conversationID)
}
