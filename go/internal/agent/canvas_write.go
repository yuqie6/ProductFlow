package agent

import (
	"context"
	"errors"

	sqldb "database/sql"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/product"
	"gorm.io/gorm"
)

// WriteProductCanvas 在商品出生事务里写入 product_workflow 会话与 conversation。
func WriteProductCanvas(ctx context.Context, tx *gorm.DB, productID, title, key, hash string, agentSessionID *string) (string, product.Conversation, error) {
	sessionID := ""
	if agentSessionID != nil && *agentSessionID != "" {
		boundProductID, status, err := loadSessionProduct(ctx, tx, *agentSessionID)
		if err != nil {
			return "", product.Conversation{}, err
		}
		if status != "active" {
			return "", product.Conversation{}, apperr.Conflict("已归档的 Agent Session 不能创建商品工作区")
		}
		if boundProductID == nil {
			id, err := insertProductSession(ctx, tx, title, productID)
			if err != nil {
				return "", product.Conversation{}, err
			}
			sessionID = id
		} else if *boundProductID != productID {
			return "", product.Conversation{}, apperr.Conflict("Agent Session 不属于当前商品")
		} else {
			sessionID = *agentSessionID
		}
	} else {
		id, err := insertProductSession(ctx, tx, title, productID)
		if err != nil {
			return "", product.Conversation{}, err
		}
		sessionID = id
	}
	conv, err := insertProductConversation(ctx, tx, sessionID, productID, key, hash)
	return sessionID, conv, err
}

func loadSessionProduct(ctx context.Context, tx *gorm.DB, sessionID string) (productID *string, status string, err error) {
	err = pfdb.QueryRow(ctx, tx, `SELECT product_id, status FROM agent_sessions WHERE id = $1`, sessionID).Scan(&productID, &status)
	if errors.Is(err, sqldb.ErrNoRows) {
		return nil, "", apperr.NotFound("Agent Session 不存在")
	}
	return productID, status, err
}

func insertProductSession(ctx context.Context, tx *gorm.DB, title, productID string) (string, error) {
	id := newID()
	if len([]rune(title)) > 160 {
		title = string([]rune(title)[:160])
	}
	_, err := pfdb.Exec(ctx, tx, `
		INSERT INTO agent_sessions (id, product_id, title, summary, status, created_at, updated_at)
		VALUES ($1, $2, $3, '暂无 Agent Task', 'active', NOW(), NOW())
	`, id, productID, title)
	return id, err
}

func insertProductConversation(ctx context.Context, tx *gorm.DB, sessionID, productID, key, requestHash string) (product.Conversation, error) {
	id := newID()
	row := product.Conversation{
		ID:           id,
		ScopeType:    "product_workflow",
		SessionID:    &sessionID,
		ProductID:    &productID,
		HarnessRunID: id,
		Status:       "collecting",
	}
	err := pfdb.QueryRow(ctx, tx, `
		INSERT INTO agent_conversations (
			id, scope_type, session_id, product_id, harness_run_id, status,
			creation_idempotency_key, creation_request_hash, created_at, updated_at
		) VALUES ($1, 'product_workflow', $2, $3, $1, 'collecting', $4, $5, NOW(), NOW())
		RETURNING created_at, updated_at
	`, id, sessionID, productID, key, requestHash).Scan(&row.CreatedAt, &row.UpdatedAt)
	return row, err
}
