package agent

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"

	sqldb "database/sql"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

var sentenceSplit = regexp.MustCompile(`[\r\n。！？!?；;]`)
var headingPrefix = regexp.MustCompile(`^#+\s*`)

func (s Service) ListSessions(ctx context.Context, includeArchived bool, productID *string) (SessionListResponse, error) {
	var out SessionListResponse
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		if productID == nil {
			if err := ensureGlobalConversations(ctx, pgxTx); err != nil {
				return err
			}
		}
		items, err := listSessions(ctx, pgxTx, includeArchived, productID)
		if err != nil {
			return err
		}
		out.Items = items
		return nil
	})
	return out, err
}

func (s Service) CreateSession(ctx context.Context) (SessionResponse, error) {
	var out SessionResponse
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		id := newID()
		if _, err := pfdb.Exec(ctx, pgxTx, `
			INSERT INTO agent_sessions (id, product_id, title, summary, status, created_at, updated_at)
			VALUES ($1, NULL, $2, '暂无 Agent Task', 'active', NOW(), NOW())
		`, id, sessionDefaultTitle); err != nil {
			return err
		}
		convID := newID()
		if _, err := pfdb.Exec(ctx, pgxTx, `
			INSERT INTO agent_conversations (
				id, scope_type, session_id, product_id, harness_run_id, status, created_at, updated_at
			) VALUES ($1, 'global', $2, NULL, $1, 'collecting', NOW(), NOW())
		`, convID, id); err != nil {
			return err
		}
		item, err := loadSession(ctx, pgxTx, id)
		if err != nil {
			return err
		}
		out = item
		return nil
	})
	return out, err
}

func (s Service) RenameSession(ctx context.Context, sessionID, title string) (SessionResponse, error) {
	normalized, err := normalizeSessionTitle(title)
	if err != nil {
		return SessionResponse{}, err
	}
	var out SessionResponse
	err = tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		n, err := pfdb.Exec(ctx, pgxTx, `
			UPDATE agent_sessions SET title = $2, updated_at = NOW() WHERE id = $1
		`, sessionID, normalized)
		if err != nil {
			return err
		}
		if n == 0 {
			return apperr.NotFound("Agent Session 不存在")
		}
		item, err := loadSession(ctx, pgxTx, sessionID)
		if err != nil {
			return err
		}
		out = item
		return nil
	})
	return out, err
}

func (s Service) ArchiveSession(ctx context.Context, sessionID string) (SessionResponse, error) {
	var out SessionResponse
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		if _, err := loadSession(ctx, pgxTx, sessionID); err != nil {
			return err
		}
		if _, err := pfdb.Exec(ctx, pgxTx, `
			UPDATE agent_sessions
			SET status = 'archived', archived_at = COALESCE(archived_at, NOW()), updated_at = NOW()
			WHERE id = $1 AND status <> 'archived'
		`, sessionID); err != nil {
			return err
		}
		item, err := loadSession(ctx, pgxTx, sessionID)
		if err != nil {
			return err
		}
		out = item
		return nil
	})
	return out, err
}

func ensureGlobalConversations(ctx context.Context, pgxTx *gorm.DB) error {
	rows, err := pfdb.Query(ctx, pgxTx, `
		SELECT s.id
		FROM agent_sessions s
		WHERE s.product_id IS NULL AND s.status <> 'archived'
		  AND NOT EXISTS (
			SELECT 1 FROM agent_conversations c
			WHERE c.session_id = s.id AND c.scope_type = 'global'
		  )
	`)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, sessionID := range ids {
		convID := newID()
		if _, err := pfdb.Exec(ctx, pgxTx, `
			INSERT INTO agent_conversations (
				id, scope_type, session_id, product_id, harness_run_id, status, created_at, updated_at
			) VALUES ($1, 'global', $2, NULL, $1, 'collecting', NOW(), NOW())
		`, convID, sessionID); err != nil {
			return err
		}
	}
	return nil
}

func listSessions(ctx context.Context, pgxTx *gorm.DB, includeArchived bool, productID *string) ([]SessionResponse, error) {
	q := `
		SELECT s.id
		FROM agent_sessions s
		WHERE ($1::text IS NULL AND s.product_id IS NULL) OR ($1::text IS NOT NULL AND s.product_id = $1)
	`
	args := []any{productID}
	if !includeArchived {
		q += ` AND s.status = 'active'`
	}
	q += `
		ORDER BY GREATEST(
			s.updated_at,
			COALESCE((SELECT MAX(c.updated_at) FROM agent_conversations c WHERE c.session_id = s.id), s.updated_at)
		) DESC, s.id DESC
		LIMIT $2
	`
	args = append(args, sessionListMax)
	rows, err := pfdb.Query(ctx, pgxTx, q, args...)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]SessionResponse, 0, len(ids))
	for _, id := range ids {
		item, err := loadSession(ctx, pgxTx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}

func loadSession(ctx context.Context, pgxTx *gorm.DB, sessionID string) (SessionResponse, error) {
	var row sessionRow
	err := pfdb.QueryRow(ctx, pgxTx, `
		SELECT id, product_id, title, summary, status, archived_at, created_at, updated_at
		FROM agent_sessions WHERE id = $1
	`, sessionID).Scan(&row.ID, &row.ProductID, &row.Title, &row.Summary, &row.Status, &row.ArchivedAt, &row.CreatedAt, &row.UpdatedAt)
	if errors.Is(err, sqldb.ErrNoRows) {
		return SessionResponse{}, apperr.NotFound("Agent Session 不存在")
	}
	if err != nil {
		return SessionResponse{}, err
	}
	convRows, err := pfdb.Query(ctx, pgxTx, `
		SELECT c.id, c.scope_type, c.product_id, COALESCE(p.name, '全局 Agent'), c.status, c.updated_at
		FROM agent_conversations c
		LEFT JOIN products p ON p.id = c.product_id
		WHERE c.session_id = $1
		ORDER BY (c.scope_type = 'global'), c.updated_at DESC, c.id
		LIMIT 20
	`, sessionID)
	if err != nil {
		return SessionResponse{}, err
	}
	defer convRows.Close()
	conversations := []SessionConversation{}
	for convRows.Next() {
		var item SessionConversation
		if err := convRows.Scan(&item.ConversationID, &item.ScopeType, &item.ProductID, &item.ProductName, &item.ConversationStatus, &item.UpdatedAt); err != nil {
			return SessionResponse{}, err
		}
		conversations = append(conversations, item)
	}
	if err := convRows.Err(); err != nil {
		return SessionResponse{}, err
	}
	var count int
	if err := pfdb.QueryRow(ctx, pgxTx, `SELECT COUNT(*) FROM agent_conversations WHERE session_id = $1`, sessionID).Scan(&count); err != nil {
		return SessionResponse{}, err
	}
	return SessionResponse{
		ID: row.ID, ProductID: row.ProductID, Title: row.Title, Summary: row.Summary,
		Status: row.Status, ArchivedAt: row.ArchivedAt, ConversationCount: count,
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
	var title string
	if err := pfdb.QueryRow(ctx, pgxTx, `SELECT title FROM agent_sessions WHERE id = $1`, sessionID).Scan(&title); err != nil {
		return err
	}
	if title != sessionDefaultTitle {
		return nil
	}
	next := deriveSessionTitle(inputText)
	if next == sessionDefaultTitle {
		return nil
	}
	_, err := pfdb.Exec(ctx, pgxTx, `UPDATE agent_sessions SET title = $2, updated_at = NOW() WHERE id = $1`, sessionID, next)
	return err
}
