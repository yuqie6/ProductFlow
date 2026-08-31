package agent

import (
	"context"
	"errors"
	"time"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/canonjson"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

// GetWorkbench 读取商品工作台；没有工作区时返回 Conflict。
func (s Service) GetWorkbench(ctx context.Context, productID string, sessionID, taskID *string) (WorkbenchResponse, error) {
	return s.loadWorkbench(ctx, productID, sessionID, taskID)
}

// EnsureWorkbench 按幂等键确保商品拥有 Agent 工作区；已存在且未 forceNew 则复用。
func (s Service) EnsureWorkbench(ctx context.Context, productID, idempotencyKey string, sessionID *string, forceNew bool) (WorkbenchResponse, error) {
	key, err := normalizeIdempotency(idempotencyKey, "Idempotency-Key")
	if err != nil {
		return WorkbenchResponse{}, err
	}
	err = tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		var existing schema.AgentConversations
		_ = pgxTx.Where("product_id = ?", productID).Order("created_at DESC, id DESC").Take(&existing).Error
		if existing.ID != "" && !forceNew {
			if sessionID == nil {
				return nil
			}
			if existing.SessionID != nil && *existing.SessionID == *sessionID {
				return nil
			}
			var match schema.AgentConversations
			_ = pgxTx.Where("product_id = ? AND session_id = ?", productID, *sessionID).Take(&match).Error
			if match.ID != "" {
				return nil
			}
		}
		var product schema.Products
		if err := pgxTx.Select("id").Where("id = ?", productID).Take(&product).Error; err != nil {
			return apperr.NotFound("商品不存在")
		}
		var graph schema.WorkflowGraphs
		if err := pgxTx.Select("id").Where("product_id = ? AND active = TRUE", productID).Take(&graph).Error; err != nil {
			return apperr.Conflict("当前商品还没有可执行的工作流")
		}
		payload := map[string]any{"request_kind": "ensure_agent_workbench_v1", "product_id": productID}
		if sessionID != nil {
			payload["agent_session_id"] = *sessionID
		}
		hash, err := canonjson.SHA256Hex(payload)
		if err != nil {
			return err
		}
		var byKey schema.AgentConversations
		err = pgxTx.Where("creation_idempotency_key = ?", key).Take(&byKey).Error
		if err == nil {
			stored := ""
			if byKey.CreationRequestHash != nil {
				stored = *byKey.CreationRequestHash
			}
			if stored != hash {
				return apperr.Conflict("相同 Idempotency-Key 不能创建不同的 Agent 商品")
			}
			return nil
		}
		sessID := ""
		if sessionID != nil && *sessionID != "" {
			var session schema.AgentSessions
			err := pgxTx.Select("product_id, status").Where("id = ?", *sessionID).Take(&session).Error
			if err != nil {
				return apperr.NotFound("Agent Session 不存在")
			}
			if session.Status != "active" {
				return apperr.Conflict("已归档的 Agent Session 不能创建商品工作区")
			}
			if session.ProductID == nil {
				newSID, err := createProductBoundSession(ctx, pgxTx, productID)
				if err != nil {
					return err
				}
				sessID = newSID
			} else if *session.ProductID != productID {
				return apperr.Conflict("Agent Session 不属于当前商品")
			} else {
				sessID = *sessionID
			}
		} else {
			newSID, err := createProductBoundSession(ctx, pgxTx, productID)
			if err != nil {
				return err
			}
			sessID = newSID
		}
		if _, err := insertProductConversation(ctx, pgxTx, sessID, productID, key, hash); err != nil {
			if uniqueViolation(err) {
				return nil
			}
			return err
		}
		return nil
	})
	if err != nil {
		return WorkbenchResponse{}, err
	}
	return s.loadWorkbench(ctx, productID, sessionID, nil)
}

func createProductBoundSession(ctx context.Context, pgxTx *gorm.DB, productID string) (string, error) {
	var product schema.Products
	_ = pgxTx.WithContext(ctx).Select("name").Where("id = ?", productID).Take(&product).Error
	id := newID()
	now := time.Now().UTC()
	pid := productID
	rec := schema.AgentSessions{
		ID:        id,
		ProductID: &pid,
		Title:     product.Name,
		Summary:   ptr("暂无 Agent Task"),
		Status:    "active",
		CreatedAt: now,
		UpdatedAt: now,
	}
	return id, pgxTx.WithContext(ctx).Create(&rec).Error
}

// loadWorkbench 读取商品工作台：Product、product_workflow conversation、live 图。没有工作区返回 Conflict，不隐式创建。
//
// GetWorkbench / EnsureWorkbench 在确认存在后调用。session/task 必须属于该商品。只读 graph.TryCurrent，不写 Goal。
func (s Service) loadWorkbench(ctx context.Context, productID string, sessionID, taskID *string) (WorkbenchResponse, error) {
	productDetail, err := s.Product.Get(ctx, productID)
	if err != nil {
		return WorkbenchResponse{}, err
	}
	var conv conversationRow
	err = tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		q := pgxTx.Where("product_id = ?", productID)
		if sessionID != nil {
			if _, err := loadSession(ctx, pgxTx, *sessionID); err != nil {
				return err
			}
			q = q.Where("session_id = ?", *sessionID)
		}
		if taskID != nil {
			task, err := loadTask(ctx, pgxTx, *taskID)
			if err != nil {
				return err
			}
			if task.ProductID == nil || *task.ProductID != productID || task.ConversationID == nil {
				return apperr.Conflict("当前 Agent Task 没有这个商品的工作区")
			}
			if sessionID != nil && task.SessionID != *sessionID {
				return apperr.Conflict("Agent Task 与当前 Agent Session 不匹配")
			}
			q = q.Where("id = ?", *task.ConversationID)
		}
		var rec schema.AgentConversations
		err := q.Order("created_at DESC, id DESC").Take(&rec).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if sessionID != nil {
				return apperr.Conflict("当前 Agent Session 没有这个商品的工作区")
			}
			return apperr.Conflict("商品还没有 Agent 工作区")
		}
		if err != nil {
			return err
		}
		conv = conversationFromSchema(rec)
		return nil
	})
	if err != nil {
		return WorkbenchResponse{}, err
	}
	graphProj, err := s.Graph.TryCurrent(ctx, productID)
	if err != nil {
		return WorkbenchResponse{}, err
	}
	revision := 0
	if graphProj != nil {
		revision = graphProj.Revision
	}
	return WorkbenchResponse{
		Mode:                   "agent",
		Product:                productDetail,
		Conversation:           serializeConversation(conv),
		Graph:                  graphProj,
		LatestWorkflowRevision: revision,
	}, nil
}
