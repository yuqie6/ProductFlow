package agent

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

var sentenceSplit = regexp.MustCompile(`[\r\n。！？!?；;]`)
var headingPrefix = regexp.MustCompile(`^#+\s*`)

type sessionCursor struct {
	V      int    `json:"v"`
	RankAt string `json:"rank_at"`
	ID     string `json:"id"`
}

func (s Service) ListSessions(ctx context.Context, includeArchived bool, productID *string, after string, limit int) (SessionListResponse, error) {
	if limit < 1 || limit > sessionListMax {
		return SessionListResponse{}, apperr.Validationf("Agent Session 分页 limit 必须在 1 到 %d 之间", sessionListMax)
	}
	var cursor *sessionCursor
	if after != "" {
		var decoded sessionCursor
		if err := decodeCursor(after, &decoded, "Agent Session 分页 cursor 无效"); err != nil {
			return SessionListResponse{}, err
		}
		if decoded.V != sessionCursorVersion {
			return SessionListResponse{}, apperr.Validation("Agent Session 分页 cursor 无效")
		}
		cursor = &decoded
	}
	var out SessionListResponse
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		if productID == nil {
			if err := ensureGlobalConversations(ctx, pgxTx); err != nil {
				return err
			}
		}
		items, next, err := listSessions(ctx, pgxTx, includeArchived, productID, cursor, limit)
		if err != nil {
			return err
		}
		out.Items = items
		out.NextCursor = next
		return nil
	})
	return out, err
}

func (s Service) CreateSession(ctx context.Context) (SessionResponse, error) {
	var out SessionResponse
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		id := newID()
		now := time.Now().UTC()
		session := schema.AgentSessions{
			ID:        id,
			Title:     sessionDefaultTitle,
			Summary:   ptr("暂无 Agent Task"),
			Status:    "active",
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := pgxTx.WithContext(ctx).Create(&session).Error; err != nil {
			return err
		}
		convID := newID()
		conv := schema.AgentConversations{
			ID:           convID,
			ScopeType:    "global",
			SessionID:    &id,
			HarnessRunID: convID,
			Status:       "collecting",
			CreatedAt:    now,
			UpdatedAt:    now,
		}
		if err := pgxTx.WithContext(ctx).Create(&conv).Error; err != nil {
			return err
		}
		item, err := loadSession(ctx, pgxTx, id)
		if err != nil {
			return err
		}
		out = item
		return nil
	})
	if err != nil {
		return SessionResponse{}, err
	}
	publishSessionChanged(s.DB, out.ID)
	return out, nil
}

func (s Service) RenameSession(ctx context.Context, sessionID, title string) (SessionResponse, error) {
	normalized, err := normalizeSessionTitle(title)
	if err != nil {
		return SessionResponse{}, err
	}
	var out SessionResponse
	err = tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		res := pgxTx.WithContext(ctx).Model(&schema.AgentSessions{}).Where("id = ?", sessionID).Updates(map[string]any{
			"title":      normalized,
			"updated_at": time.Now().UTC(),
		})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return apperr.NotFound("Agent Session 不存在")
		}
		item, err := loadSession(ctx, pgxTx, sessionID)
		if err != nil {
			return err
		}
		out = item
		return nil
	})
	if err != nil {
		return SessionResponse{}, err
	}
	publishSessionChanged(s.DB, out.ID)
	return out, nil
}

func (s Service) ArchiveSession(ctx context.Context, sessionID string) (SessionResponse, error) {
	var out SessionResponse
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		if _, err := loadSession(ctx, pgxTx, sessionID); err != nil {
			return err
		}
		if err := pgxTx.WithContext(ctx).Model(&schema.AgentSessions{}).
			Where("id = ? AND status <> ?", sessionID, "archived").
			Updates(map[string]any{
				"status":      "archived",
				"archived_at": gorm.Expr("COALESCE(archived_at, NOW())"),
				"updated_at":  gorm.Expr("NOW()"),
			}).Error; err != nil {
			return err
		}
		item, err := loadSession(ctx, pgxTx, sessionID)
		if err != nil {
			return err
		}
		out = item
		return nil
	})
	if err != nil {
		return SessionResponse{}, err
	}
	publishSessionChanged(s.DB, out.ID)
	return out, nil
}

func ensureGlobalConversations(ctx context.Context, pgxTx *gorm.DB) error {
	var ids []string
	err := pgxTx.WithContext(ctx).Model(&schema.AgentSessions{}).
		Where("product_id IS NULL AND status <> ?", "archived").
		Where("NOT EXISTS (SELECT 1 FROM agent_conversations c WHERE c.session_id = agent_sessions.id AND c.scope_type = ?)", "global").
		Pluck("id", &ids).Error
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	for _, sessionID := range ids {
		convID := newID()
		sid := sessionID
		conv := schema.AgentConversations{
			ID:           convID,
			ScopeType:    "global",
			SessionID:    &sid,
			HarnessRunID: convID,
			Status:       "collecting",
			CreatedAt:    now,
			UpdatedAt:    now,
		}
		if err := pgxTx.WithContext(ctx).Create(&conv).Error; err != nil {
			return err
		}
	}
	return nil
}

func listSessions(ctx context.Context, pgxTx *gorm.DB, includeArchived bool, productID *string, cursor *sessionCursor, limit int) ([]SessionResponse, *string, error) {
	q := pgxTx.WithContext(ctx).Model(&schema.AgentSessions{})
	if productID == nil {
		q = q.Where("product_id IS NULL")
	} else {
		q = q.Where("product_id = ?", *productID)
	}
	if !includeArchived {
		q = q.Where("status = ?", "active")
	}
	rankSQL := sessionRankSQL
	if cursor != nil {
		rankAt, err := time.Parse(time.RFC3339Nano, cursor.RankAt)
		if err != nil {
			rankAt, err = time.Parse(time.RFC3339, cursor.RankAt)
			if err != nil {
				return nil, nil, apperr.Validation("Agent Session 分页 cursor 无效")
			}
		}
		q = q.Where("("+rankSQL+" < ? OR ("+rankSQL+" = ? AND agent_sessions.id < ?))", rankAt, rankAt, cursor.ID)
	}
	var ids []string
	err := q.Order(rankSQL+` DESC, agent_sessions.id DESC`).
		Limit(limit+1).
		Pluck("id", &ids).Error
	if err != nil {
		return nil, nil, err
	}
	hasMore := len(ids) > limit
	if hasMore {
		ids = ids[:limit]
	}
	out := make([]SessionResponse, 0, len(ids))
	for _, id := range ids {
		item, err := loadSession(ctx, pgxTx, id)
		if err != nil {
			return nil, nil, err
		}
		out = append(out, item)
	}
	var next *string
	if hasMore && len(out) > 0 {
		oldest := out[len(out)-1]
		rankAt := oldest.UpdatedAt
		for _, conv := range oldest.Conversations {
			if conv.UpdatedAt.After(rankAt) {
				rankAt = conv.UpdatedAt
			}
		}
		encoded, err := encodeCursor(sessionCursor{
			V: sessionCursorVersion, RankAt: rankAt.UTC().Format(time.RFC3339Nano), ID: oldest.ID,
		})
		if err != nil {
			return nil, nil, err
		}
		next = &encoded
	}
	return out, next, nil
}

const sessionRankSQL = `GREATEST(
			agent_sessions.updated_at,
			COALESCE((SELECT MAX(c.updated_at) FROM agent_conversations c WHERE c.session_id = agent_sessions.id), agent_sessions.updated_at)
		)`

func loadSession(ctx context.Context, pgxTx *gorm.DB, sessionID string) (SessionResponse, error) {
	var row schema.AgentSessions
	err := pgxTx.WithContext(ctx).Where("id = ?", sessionID).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return SessionResponse{}, apperr.NotFound("Agent Session 不存在")
	}
	if err != nil {
		return SessionResponse{}, err
	}
	var conversations []SessionConversation
	err = pgxTx.WithContext(ctx).Model(&schema.AgentConversations{}).
		Select(`agent_conversations.id AS conversation_id,
			agent_conversations.scope_type,
			agent_conversations.product_id,
			COALESCE(products.name, '全局 Agent') AS product_name,
			agent_conversations.status AS conversation_status,
			agent_conversations.updated_at`).
		Joins("LEFT JOIN products ON products.id = agent_conversations.product_id").
		Where("agent_conversations.session_id = ?", sessionID).
		Order("(agent_conversations.scope_type = 'global'), agent_conversations.updated_at DESC, agent_conversations.id").
		Limit(20).
		Find(&conversations).Error
	if err != nil {
		return SessionResponse{}, err
	}
	if conversations == nil {
		conversations = []SessionConversation{}
	}
	var count int64
	if err := pgxTx.WithContext(ctx).Model(&schema.AgentConversations{}).Where("session_id = ?", sessionID).Count(&count).Error; err != nil {
		return SessionResponse{}, err
	}
	return SessionResponse{
		ID: row.ID, ProductID: row.ProductID, Title: row.Title, Summary: row.Summary,
		Status: row.Status, ArchivedAt: row.ArchivedAt, ConversationCount: int(count),
		Conversations: conversations, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}, nil
}

func deriveSessionTitle(input string) string {
	normalized := strings.Join(strings.Fields(input), " ")
	if normalized == "" {
		return sessionDefaultTitle
	}
	first := sentenceSplit.Split(normalized, 2)[0]
	candidate := strings.Trim(headingPrefix.ReplaceAllString(first, ""), " ，,：:。！？!?；;")
	if candidate == "" {
		candidate = normalized
	}
	if utf8.RuneCountInString(candidate) > sessionTitleMax {
		candidate = string([]rune(candidate)[:sessionTitleMax])
	}
	return candidate
}

func autoNameSession(ctx context.Context, pgxTx *gorm.DB, sessionID, inputText string) error {
	var session schema.AgentSessions
	if err := pgxTx.WithContext(ctx).Select("title").Where("id = ?", sessionID).Take(&session).Error; err != nil {
		return err
	}
	if session.Title != sessionDefaultTitle {
		return nil
	}
	next := deriveSessionTitle(inputText)
	if next == sessionDefaultTitle {
		return nil
	}
	if err := pgxTx.WithContext(ctx).Model(&schema.AgentSessions{}).Where("id = ?", sessionID).Updates(map[string]any{
		"title":      next,
		"updated_at": time.Now().UTC(),
	}).Error; err != nil {
		return err
	}
	publishSessionChanged(pgxTx, sessionID)
	return nil
}
