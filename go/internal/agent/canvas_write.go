package agent

import (
	"context"
	"errors"
	"time"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/product"
	"gorm.io/gorm"
)

// WriteProductCanvas 在商品出生事务里写入 product_workflow 会话与 conversation。Session 不存在返回 NotFound；已归档或不属于当前商品返回 Conflict。
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
	var rec schema.AgentSessions
	err = tx.WithContext(ctx).Select("product_id, status").Where("id = ?", sessionID).Take(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, "", apperr.NotFound("Agent Session 不存在")
	}
	return rec.ProductID, rec.Status, err
}

func insertProductSession(ctx context.Context, tx *gorm.DB, title, productID string) (string, error) {
	id := newID()
	if len([]rune(title)) > 160 {
		title = string([]rune(title)[:160])
	}
	now := time.Now().UTC()
	pid := productID
	rec := schema.AgentSessions{
		ID:         id,
		ProductID:  &pid,
		Title:      title,
		Summary:    ptr("暂无 Agent Task"),
		Status:     "active",
		CreatedAt:  now,
		UpdatedAt:  now,
		ActivityAt: now,
	}
	return id, tx.WithContext(ctx).Create(&rec).Error
}

// insertProductConversation 在商品出生事务写入 product_workflow conversation，并带上创建幂等键与 request hash。
// 不创建 AgentTask 或 Turn；产品 Goal 必须由用户稍后显式发起。
func insertProductConversation(ctx context.Context, tx *gorm.DB, sessionID, productID, key, requestHash string) (product.Conversation, error) {
	id := newID()
	now := time.Now().UTC()
	sid := sessionID
	pid := productID
	idempotencyKey := key
	hash := requestHash
	rec := schema.AgentConversations{
		ID:                     id,
		ScopeType:              "product_workflow",
		SessionID:              &sid,
		ProductID:              &pid,
		HarnessRunID:           id,
		Status:                 "collecting",
		CreationIdempotencyKey: &idempotencyKey,
		CreationRequestHash:    &hash,
		CreatedAt:              now,
		UpdatedAt:              now,
	}
	if err := tx.WithContext(ctx).Create(&rec).Error; err != nil {
		return product.Conversation{}, err
	}
	return product.Conversation{
		ID:           rec.ID,
		ScopeType:    rec.ScopeType,
		SessionID:    rec.SessionID,
		ProductID:    rec.ProductID,
		HarnessRunID: rec.HarnessRunID,
		Status:       rec.Status,
		CreatedAt:    rec.CreatedAt,
		UpdatedAt:    rec.UpdatedAt,
	}, nil
}
