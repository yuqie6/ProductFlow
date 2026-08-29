package agent

import (
	"context"
	"errors"

	sqldb "database/sql"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

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

func loadConversation(ctx context.Context, pgxTx *gorm.DB, productID *string, conversationID string) (conversationRow, error) {
	var row conversationRow
	var err error
	if productID == nil {
		err = pfdb.QueryRow(ctx, pgxTx, `
			SELECT id, scope_type, session_id, product_id, harness_run_id, status, created_at, updated_at
			FROM agent_conversations WHERE id = $1 AND scope_type = 'global'
		`, conversationID).Scan(&row.ID, &row.ScopeType, &row.SessionID, &row.ProductID, &row.HarnessRunID, &row.Status, &row.CreatedAt, &row.UpdatedAt)
	} else {
		err = pfdb.QueryRow(ctx, pgxTx, `
			SELECT id, scope_type, session_id, product_id, harness_run_id, status, created_at, updated_at
			FROM agent_conversations
			WHERE id = $1 AND scope_type = 'product_workflow' AND product_id = $2
		`, conversationID, *productID).Scan(&row.ID, &row.ScopeType, &row.SessionID, &row.ProductID, &row.HarnessRunID, &row.Status, &row.CreatedAt, &row.UpdatedAt)
	}
	if errors.Is(err, sqldb.ErrNoRows) {
		if productID != nil {
			var exists int
			if scanErr := pfdb.QueryRow(ctx, pgxTx, `SELECT 1 FROM products WHERE id = $1`, *productID).Scan(&exists); errors.Is(scanErr, sqldb.ErrNoRows) {
				return conversationRow{}, apperr.NotFound("商品不存在")
			}
		}
		return conversationRow{}, apperr.NotFound("Agent conversation 不存在")
	}
	return row, err
}

func loadConversationByID(ctx context.Context, pgxTx *gorm.DB, conversationID string) (conversationRow, error) {
	var row conversationRow
	err := pfdb.QueryRow(ctx, pgxTx, `
		SELECT id, scope_type, session_id, product_id, harness_run_id, status, created_at, updated_at
		FROM agent_conversations WHERE id = $1
	`, conversationID).Scan(&row.ID, &row.ScopeType, &row.SessionID, &row.ProductID, &row.HarnessRunID, &row.Status, &row.CreatedAt, &row.UpdatedAt)
	if errors.Is(err, sqldb.ErrNoRows) {
		return conversationRow{}, apperr.NotFound("Agent conversation 不存在")
	}
	return row, err
}

func lockConversation(ctx context.Context, pgxTx *gorm.DB, conversationID string) (conversationRow, error) {
	var row conversationRow
	err := pfdb.QueryRow(ctx, pgxTx, `
		SELECT id, scope_type, session_id, product_id, harness_run_id, status, created_at, updated_at
		FROM agent_conversations WHERE id = $1 FOR UPDATE
	`, conversationID).Scan(&row.ID, &row.ScopeType, &row.SessionID, &row.ProductID, &row.HarnessRunID, &row.Status, &row.CreatedAt, &row.UpdatedAt)
	if errors.Is(err, sqldb.ErrNoRows) {
		return conversationRow{}, apperr.NotFound("Agent conversation 不存在")
	}
	return row, err
}

func applyConversationStatus(ctx context.Context, pgxTx *gorm.DB, conversationID, turnStatus string) error {
	status := "collecting"
	switch turnStatus {
	case "awaiting_confirmation":
		status = "awaiting_confirmation"
	case "succeeded":
		var scope string
		var draftStatus *string
		_ = pfdb.QueryRow(ctx, pgxTx, `
			SELECT c.scope_type, d.status
			FROM agent_conversations c
			LEFT JOIN library_organization_drafts d ON d.conversation_id = c.id
			WHERE c.id = $1
		`, conversationID).Scan(&scope, &draftStatus)
		if scope == "global" && draftStatus != nil && *draftStatus == "awaiting_confirmation" {
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
	_, err := pfdb.Exec(ctx, pgxTx, `
		UPDATE agent_conversations SET status = $2, updated_at = NOW() WHERE id = $1
	`, conversationID, status)
	return err
}

func serializeConversation(row conversationRow) ConversationResponse {
	return ConversationResponse{
		ID: row.ID, ScopeType: row.ScopeType, SessionID: row.SessionID, ProductID: row.ProductID,
		HarnessRunID: row.HarnessRunID, Status: row.Status, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}
