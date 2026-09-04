package agent

import (
	"context"
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

// ListSessions 分页列出 Agent Session，给 Dock 和商品工作台侧栏。
//
// productID 为 nil 只列全局 Dock（并给缺 global conversation 的 Session 补一条 collecting 对话）；非 nil 只列该商品画布 Session。
// after 是上次返回的 NextCursor，不是页码；非法或版本不匹配返回 Validation。limit 须在 1–100。
// includeArchived=false 只列 active。除补 conversation 外只读，不改 Goal、lease、journal。
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

// CreateSession 创建全局 Dock Session 及其 global conversation。数据库失败返回 error。
func (s Service) CreateSession(ctx context.Context) (SessionResponse, error) {
	var out SessionResponse
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		id := newID()
		now := time.Now().UTC()
		session := schema.AgentSessions{
			ID:         id,
			Title:      sessionDefaultTitle,
			Summary:    ptr("暂无 Agent Task"),
			Status:     "active",
			CreatedAt:  now,
			UpdatedAt:  now,
			ActivityAt: now,
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

// RenameSession 把 Session.title 改成规范化标题，给 PATCH /api/v2/agent-sessions/:session_id。
//
// 空或超过 160 字返回 Validation。找不到返回 NotFound。只改 title 与 updated_at，不归档、不改 Goal、不碰 journal。
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

// ArchiveSession 归档 Session；已归档则幂等返回当前行。Session 不存在返回 NotFound。数据库失败返回 error。
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

// ensureGlobalConversations 给尚未挂 global conversation 的 Dock Session 补一条 collecting 对话。
//
// ListSessions / CreateSession 路径调用。不创建 Task 或 Turn。已有 global conversation 的 Session 跳过。
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

// listSessions 按 Session.activity_at 分页。productID=nil 只列全局 Dock；否则只列该商品画布 Session。
//
// cursor 无效返回 Validation。不写表，不改 Goal。
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
	if cursor != nil {
		rankAt, err := time.Parse(time.RFC3339Nano, cursor.RankAt)
		if err != nil {
			rankAt, err = time.Parse(time.RFC3339, cursor.RankAt)
			if err != nil {
				return nil, nil, apperr.Validation("Agent Session 分页 cursor 无效")
			}
		}
		q = q.Where("(activity_at < ? OR (activity_at = ? AND agent_sessions.id < ?))", rankAt, rankAt, cursor.ID)
	}
	var ids []string
	err := q.Order(`activity_at DESC, agent_sessions.id DESC`).
		Limit(limit+1).
		Pluck("id", &ids).Error
	if err != nil {
		return nil, nil, err
	}
	hasMore := len(ids) > limit
	if hasMore {
		ids = ids[:limit]
	}
	out, err := loadSessions(ctx, pgxTx, ids)
	if err != nil {
		return nil, nil, err
	}
	var next *string
	if hasMore && len(out) > 0 {
		oldest := out[len(out)-1]
		encoded, err := encodeCursor(sessionCursor{
			V: sessionCursorVersion, RankAt: oldest.ActivityAt.UTC().Format(time.RFC3339Nano), ID: oldest.ID,
		})
		if err != nil {
			return nil, nil, err
		}
		next = &encoded
	}
	return out, next, nil
}

const sessionConversationLimit = 20

type sessionConversationRow struct {
	SessionID          string    `gorm:"column:session_id"`
	ConversationID     string    `gorm:"column:conversation_id"`
	ScopeType          string    `gorm:"column:scope_type"`
	ProductID          *string   `gorm:"column:product_id"`
	ProductName        string    `gorm:"column:product_name"`
	ConversationStatus string    `gorm:"column:conversation_status"`
	UpdatedAt          time.Time `gorm:"column:updated_at"`
	ConversationRank   int       `gorm:"column:conversation_rank"`
}

// loadSessions 批量组装 Session 投影，避免列表页按 session 逐条读取 conversations 和 count。
// 每个 session 仍只返回最近 20 条 conversation；调用方传入的 ids 顺序决定返回顺序。
func loadSessions(ctx context.Context, pgxTx *gorm.DB, ids []string) ([]SessionResponse, error) {
	if len(ids) == 0 {
		return []SessionResponse{}, nil
	}
	var rows []schema.AgentSessions
	if err := pgxTx.WithContext(ctx).Where("id IN ?", ids).Find(&rows).Error; err != nil {
		return nil, err
	}
	sessionByID := make(map[string]schema.AgentSessions, len(rows))
	for _, row := range rows {
		sessionByID[row.ID] = row
	}
	for _, id := range ids {
		if _, ok := sessionByID[id]; !ok {
			return nil, apperr.NotFound("Agent Session 不存在")
		}
	}

	conversationQuery := pgxTx.WithContext(ctx).Model(&schema.AgentConversations{}).
		Select(`agent_conversations.session_id,
			agent_conversations.id AS conversation_id,
			agent_conversations.scope_type,
			agent_conversations.product_id,
			COALESCE(products.name, '全局 Agent') AS product_name,
			agent_conversations.status AS conversation_status,
			agent_conversations.updated_at,
			ROW_NUMBER() OVER (
				PARTITION BY agent_conversations.session_id
				ORDER BY (agent_conversations.scope_type = 'global'), agent_conversations.updated_at DESC, agent_conversations.id
			) AS conversation_rank`).
		Joins("LEFT JOIN products ON products.id = agent_conversations.product_id").
		Where("agent_conversations.session_id IN ?", ids)
	var conversationRows []sessionConversationRow
	if err := pgxTx.WithContext(ctx).Table("(?) AS ranked", conversationQuery).
		Where("conversation_rank <= ?", sessionConversationLimit).
		Order("session_id, conversation_rank").
		Scan(&conversationRows).Error; err != nil {
		return nil, err
	}
	conversationsBySession := make(map[string][]SessionConversation, len(ids))
	for _, row := range conversationRows {
		conversationsBySession[row.SessionID] = append(conversationsBySession[row.SessionID], SessionConversation{
			ConversationID: row.ConversationID, ScopeType: row.ScopeType, ProductID: row.ProductID,
			ProductName: row.ProductName, ConversationStatus: row.ConversationStatus, UpdatedAt: row.UpdatedAt,
		})
	}

	type conversationCount struct {
		SessionID string `gorm:"column:session_id"`
		Count     int64  `gorm:"column:count"`
	}
	var counts []conversationCount
	if err := pgxTx.WithContext(ctx).Model(&schema.AgentConversations{}).
		Select("session_id, COUNT(*) AS count").
		Where("session_id IN ?", ids).
		Group("session_id").
		Scan(&counts).Error; err != nil {
		return nil, err
	}
	countBySession := make(map[string]int64, len(counts))
	for _, item := range counts {
		countBySession[item.SessionID] = item.Count
	}

	out := make([]SessionResponse, 0, len(ids))
	for _, id := range ids {
		row := sessionByID[id]
		conversations := conversationsBySession[id]
		if conversations == nil {
			conversations = []SessionConversation{}
		}
		out = append(out, SessionResponse{
			ID: row.ID, ProductID: row.ProductID, Title: row.Title, Summary: row.Summary,
			Status: row.Status, ArchivedAt: row.ArchivedAt, ConversationCount: int(countBySession[id]),
			Conversations: conversations, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
			ActivityAt: row.ActivityAt,
		})
	}
	return out, nil
}

// loadSession 组装一条 Session 投影：标题、摘要、至多 20 条对话。缺失返回 NotFound。只读，不触发 GraphRun 同步。
func loadSession(ctx context.Context, pgxTx *gorm.DB, sessionID string) (SessionResponse, error) {
	items, err := loadSessions(ctx, pgxTx, []string{sessionID})
	if err != nil {
		return SessionResponse{}, err
	}
	return items[0], nil
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

// autoNameSession 仅当标题仍是默认值时，用首条用户输入生成标题。用户已改名则不动。写 agent_sessions.title。
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
