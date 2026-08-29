package agent

import (
	"context"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/canonjson"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

func (s Service) GetWorkbench(ctx context.Context, productID string, sessionID, taskID *string) (WorkbenchResponse, error) {
	return s.loadWorkbench(ctx, productID, sessionID, taskID)
}

func (s Service) EnsureWorkbench(ctx context.Context, productID, idempotencyKey string, sessionID *string, forceNew bool) (WorkbenchResponse, error) {
	key, err := normalizeIdempotency(idempotencyKey, "Idempotency-Key")
	if err != nil {
		return WorkbenchResponse{}, err
	}
	err = tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		var existingID string
		q := `SELECT id FROM agent_conversations WHERE product_id = $1 ORDER BY created_at DESC, id DESC LIMIT 1`
		_ = pfdb.QueryRow(ctx, pgxTx, q, productID).Scan(&existingID)
		if existingID != "" && !forceNew {
			if sessionID == nil {
				return nil
			}
			var sid *string
			_ = pfdb.QueryRow(ctx, pgxTx, `SELECT session_id FROM agent_conversations WHERE id = $1`, existingID).Scan(&sid)
			if sid != nil && *sid == *sessionID {
				return nil
			}
			var match string
			_ = pfdb.QueryRow(ctx, pgxTx, `
				SELECT id FROM agent_conversations WHERE product_id = $1 AND session_id = $2 LIMIT 1
			`, productID, *sessionID).Scan(&match)
			if match != "" {
				return nil
			}
		}
		var exists int
		if err := pfdb.QueryRow(ctx, pgxTx, `SELECT 1 FROM products WHERE id = $1`, productID).Scan(&exists); err != nil {
			return apperr.NotFound("商品不存在")
		}
		var graphExists int
		if err := pfdb.QueryRow(ctx, pgxTx, `SELECT 1 FROM workflow_graphs WHERE product_id = $1 AND active = TRUE LIMIT 1`, productID).Scan(&graphExists); err != nil {
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
		var byKey string
		err = pfdb.QueryRow(ctx, pgxTx, `SELECT id FROM agent_conversations WHERE creation_idempotency_key = $1`, key).Scan(&byKey)
		if err == nil {
			var stored string
			_ = pfdb.QueryRow(ctx, pgxTx, `SELECT creation_request_hash FROM agent_conversations WHERE id = $1`, byKey).Scan(&stored)
			if stored != hash {
				return apperr.Conflict("相同 Idempotency-Key 不能创建不同的 Agent 商品")
			}
			return nil
		}
		sessID := ""
		if sessionID != nil && *sessionID != "" {
			var productOf *string
			var status string
			err := pfdb.QueryRow(ctx, pgxTx, `SELECT product_id, status FROM agent_sessions WHERE id = $1`, *sessionID).Scan(&productOf, &status)
			if err != nil {
				return apperr.NotFound("Agent Session 不存在")
			}
			if status != "active" {
				return apperr.Conflict("已归档的 Agent Session 不能创建商品工作区")
			}
			if productOf == nil {
				newSID := newID()
				var name string
				_ = pfdb.QueryRow(ctx, pgxTx, `SELECT name FROM products WHERE id = $1`, productID).Scan(&name)
				if _, err := pfdb.Exec(ctx, pgxTx, `
					INSERT INTO agent_sessions (id, product_id, title, summary, status, created_at, updated_at)
					VALUES ($1, $2, $3, '暂无 Agent Task', 'active', NOW(), NOW())
				`, newSID, productID, name); err != nil {
					return err
				}
				sessID = newSID
			} else if *productOf != productID {
				return apperr.Conflict("Agent Session 不属于当前商品")
			} else {
				sessID = *sessionID
			}
		} else {
			newSID := newID()
			var name string
			_ = pfdb.QueryRow(ctx, pgxTx, `SELECT name FROM products WHERE id = $1`, productID).Scan(&name)
			if _, err := pfdb.Exec(ctx, pgxTx, `
				INSERT INTO agent_sessions (id, product_id, title, summary, status, created_at, updated_at)
				VALUES ($1, $2, $3, '暂无 Agent Task', 'active', NOW(), NOW())
			`, newSID, productID, name); err != nil {
				return err
			}
			sessID = newSID
		}
		convID := newID()
		if _, err := pfdb.Exec(ctx, pgxTx, `
			INSERT INTO agent_conversations (
				id, scope_type, session_id, product_id, harness_run_id, status,
				creation_idempotency_key, creation_request_hash, created_at, updated_at
			) VALUES ($1, 'product_workflow', $2, $3, $1, 'collecting', $4, $5, NOW(), NOW())
		`, convID, sessID, productID, key, hash); err != nil {
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

func (s Service) loadWorkbench(ctx context.Context, productID string, sessionID, taskID *string) (WorkbenchResponse, error) {
	productDetail, err := s.Product.Get(ctx, productID)
	if err != nil {
		return WorkbenchResponse{}, err
	}
	var conv conversationRow
	err = tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		q := `SELECT id, scope_type, session_id, product_id, harness_run_id, status, created_at, updated_at
			FROM agent_conversations WHERE product_id = $1`
		args := []any{productID}
		n := 2
		if sessionID != nil {
			if _, err := loadSession(ctx, pgxTx, *sessionID); err != nil {
				return err
			}
			q += ` AND session_id = $2`
			args = append(args, *sessionID)
			n = 3
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
			q += ` AND id = $` + itoaN(n)
			args = append(args, *task.ConversationID)
		}
		q += ` ORDER BY created_at DESC, id DESC LIMIT 1`
		err := pfdb.QueryRow(ctx, pgxTx, q, args...).Scan(
			&conv.ID, &conv.ScopeType, &conv.SessionID, &conv.ProductID, &conv.HarnessRunID, &conv.Status, &conv.CreatedAt, &conv.UpdatedAt,
		)
		if isNoRows(err) {
			if sessionID != nil {
				return apperr.Conflict("当前 Agent Session 没有这个商品的工作区")
			}
			return apperr.Conflict("商品还没有 Agent 工作区")
		}
		return err
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
