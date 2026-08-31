package agent

import (
	"context"
	"errors"
	"time"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

// GetConversation 按商品作用域读取 Agent Conversation。
func (s Service) GetConversation(ctx context.Context, productID *string, conversationID string) (ConversationResponse, error) {
	var out ConversationResponse
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		row, err := loadConversation(ctx, pgxTx, productID, conversationID)
		if err != nil {
			return err
		}
		out = serializeConversation(row)
		return nil
	})
	return out, err
}

func conversationFromSchema(rec schema.AgentConversations) conversationRow {
	return conversationRow{
		ID: rec.ID, ScopeType: rec.ScopeType, SessionID: rec.SessionID, ProductID: rec.ProductID,
		HarnessRunID: rec.HarnessRunID, Status: rec.Status, CreatedAt: rec.CreatedAt, UpdatedAt: rec.UpdatedAt,
	}
}

// loadConversation 按商品或全局作用域读取 conversation。商品行缺失与 conversation 缺失返回不同 NotFound。
func loadConversation(ctx context.Context, pgxTx *gorm.DB, productID *string, conversationID string) (conversationRow, error) {
	var rec schema.AgentConversations
	var err error
	if productID == nil {
		err = pgxTx.WithContext(ctx).Where("id = ? AND scope_type = ?", conversationID, "global").Take(&rec).Error
	} else {
		err = pgxTx.WithContext(ctx).
			Where("id = ? AND scope_type = ? AND product_id = ?", conversationID, "product_workflow", *productID).
			Take(&rec).Error
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if productID != nil {
			var product schema.Products
			if scanErr := pgxTx.WithContext(ctx).Select("id").Where("id = ?", *productID).Take(&product).Error; errors.Is(scanErr, gorm.ErrRecordNotFound) {
				return conversationRow{}, apperr.NotFound("商品不存在")
			}
		}
		return conversationRow{}, apperr.NotFound("Agent conversation 不存在")
	}
	if err != nil {
		return conversationRow{}, err
	}
	return conversationFromSchema(rec), nil
}

func loadConversationByID(ctx context.Context, pgxTx *gorm.DB, conversationID string) (conversationRow, error) {
	var rec schema.AgentConversations
	err := pgxTx.WithContext(ctx).Where("id = ?", conversationID).Take(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return conversationRow{}, apperr.NotFound("Agent conversation 不存在")
	}
	if err != nil {
		return conversationRow{}, err
	}
	return conversationFromSchema(rec), nil
}

func lockConversation(ctx context.Context, pgxTx *gorm.DB, conversationID string) (conversationRow, error) {
	var rec schema.AgentConversations
	err := pgxTx.WithContext(ctx).Clauses(pfdb.ForUpdate()).Where("id = ?", conversationID).Take(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return conversationRow{}, apperr.NotFound("Agent conversation 不存在")
	}
	if err != nil {
		return conversationRow{}, err
	}
	return conversationFromSchema(rec), nil
}

// applyConversationStatus 把 Turn 终态投影到 agent_conversations。全局图库 draft 仍 awaiting_confirmation 时，Turn succeeded 不把 conversation 标 completed。
//
// applyTurnState 与本地 cancel 调用。写 conversation.status；状态变化时通知 Session。不改 Goal。
func applyConversationStatus(ctx context.Context, pgxTx *gorm.DB, conversationID, turnStatus string) error {
	status := "collecting"
	switch turnStatus {
	case "awaiting_confirmation":
		status = "awaiting_confirmation"
	case "succeeded":
		var result struct {
			ScopeType   string  `gorm:"column:scope_type"`
			DraftStatus *string `gorm:"column:draft_status"`
		}
		_ = pgxTx.WithContext(ctx).Model(&schema.AgentConversations{}).
			Select("agent_conversations.scope_type, library_organization_drafts.status AS draft_status").
			Joins("LEFT JOIN library_organization_drafts ON library_organization_drafts.conversation_id = agent_conversations.id").
			Where("agent_conversations.id = ?", conversationID).
			Take(&result).Error
		if result.ScopeType == "global" && result.DraftStatus != nil && *result.DraftStatus == "awaiting_confirmation" {
			status = "awaiting_confirmation"
		} else {
			status = "completed"
		}
	case "failed":
		status = "failed"
	case "canceled":
		status = "canceled"
	case "unknown":
		status = "unknown"
	}
	var previous schema.AgentConversations
	loadErr := pgxTx.WithContext(ctx).Select("session_id", "status").Where("id = ?", conversationID).Take(&previous).Error
	if err := pgxTx.WithContext(ctx).Model(&schema.AgentConversations{}).Where("id = ?", conversationID).Updates(map[string]any{
		"status":     status,
		"updated_at": time.Now().UTC(),
	}).Error; err != nil {
		return err
	}
	if loadErr == nil && previous.Status != status && previous.SessionID != nil {
		publishSessionChanged(pgxTx, *previous.SessionID)
	}
	return nil
}

func serializeConversation(row conversationRow) ConversationResponse {
	return ConversationResponse{
		ID: row.ID, ScopeType: row.ScopeType, SessionID: row.SessionID, ProductID: row.ProductID,
		HarnessRunID: row.HarnessRunID, Status: row.Status, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}
