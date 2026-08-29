package library

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/canonjson"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/tx"
)

type OrganizationDraftRevision struct {
	ID                   string          `json:"id"`
	Version              int             `json:"version"`
	SchemaVersion        int             `json:"schema_version"`
	Payload              json.RawMessage `json:"payload"`
	PayloadHash          string          `json:"payload_hash"`
	SourceTurnID         *string         `json:"source_turn_id"`
	SourceArtifactStepID *string         `json:"source_artifact_step_id"`
	ConfirmedAt          *time.Time      `json:"confirmed_at"`
	CreatedAt            time.Time       `json:"created_at"`
}

type OrganizationDraft struct {
	ID                  string                     `json:"id"`
	ConversationID      string                     `json:"conversation_id"`
	Status              string                     `json:"status"`
	CurrentRevision     *OrganizationDraftRevision `json:"current_revision"`
	ConfirmedRevisionID *string                    `json:"confirmed_revision_id"`
	ConfirmationResult  json.RawMessage            `json:"confirmation_result"`
	ConfirmedAt         *time.Time                 `json:"confirmed_at"`
	CreatedAt           time.Time                  `json:"created_at"`
	UpdatedAt           time.Time                  `json:"updated_at"`
}

// GetOrganizationDraft 按全局 conversation 读取素材整理 Draft。
func (s Service) GetOrganizationDraft(ctx context.Context, conversationID string) (OrganizationDraft, error) {
	var out OrganizationDraft
	err := tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
		draft, err := loadOrganizationDraft(ctx, pgxTx, conversationID)
		if err != nil {
			return err
		}
		out = draft
		return nil
	})
	return out, err
}

// AppendOrganizationDraftRevision 追加一条整理 artifact。没有 Draft 时创建。
func (s Service) AppendOrganizationDraftRevision(ctx context.Context, conversationID string, payload json.RawMessage, sourceTurnID, sourceStepID string) (OrganizationDraft, error) {
	hash, err := canonjson.SHA256Hex(json.RawMessage(payload))
	if err != nil {
		return OrganizationDraft{}, apperr.Validation("素材整理 Draft payload 无效")
	}
	var out OrganizationDraft
	err = tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
		draft, err := loadOrganizationDraft(ctx, pgxTx, conversationID)
		if err != nil {
			var e apperr.Error
			if !errors.As(err, &e) || e.Status != 404 {
				return err
			}
			draftID := clockid.New()
			if _, err := pgxTx.Exec(ctx, `
				INSERT INTO library_organization_drafts (id, conversation_id, status, created_at, updated_at)
				VALUES ($1, $2, 'awaiting_confirmation', NOW(), NOW())
			`, draftID, conversationID); err != nil {
				return err
			}
			draft.ID = draftID
			draft.Status = "awaiting_confirmation"
		}
		if draft.Status == "confirmed" {
			return apperr.Conflict("素材整理 Draft 已确认，不能再追加 revision")
		}
		var version int
		_ = pgxTx.QueryRow(ctx, `
			SELECT COALESCE(MAX(version), 0) FROM library_organization_draft_revisions WHERE draft_id = $1
		`, draft.ID).Scan(&version)
		revID := clockid.New()
		now := s.now()
		if _, err := pgxTx.Exec(ctx, `
			INSERT INTO library_organization_draft_revisions (
				id, draft_id, version, schema_version, payload_json, payload_hash,
				source_turn_id, source_artifact_step_id, created_at
			) VALUES ($1, $2, $3, 1, $4, $5, $6, $7, $8)
		`, revID, draft.ID, version+1, payload, hash, nullable(sourceTurnID), nullable(sourceStepID), now); err != nil {
			return err
		}
		if _, err := pgxTx.Exec(ctx, `
			UPDATE library_organization_drafts
			SET current_revision_id = $2, status = 'awaiting_confirmation', updated_at = $3
			WHERE id = $1
		`, draft.ID, revID, now); err != nil {
			return err
		}
		out, err = loadOrganizationDraft(ctx, pgxTx, conversationID)
		return err
	})
	return out, err
}

// ConfirmOrganizationDraft 应用当前 revision 的组织变更，不复制媒体 bytes。
func (s Service) ConfirmOrganizationDraft(ctx context.Context, conversationID string, expectedVersion int, idempotencyKey string) (OrganizationDraft, error) {
	key := strings.TrimSpace(idempotencyKey)
	if key == "" {
		return OrganizationDraft{}, apperr.Validation("idempotency key 不能为空")
	}
	if len([]byte(key)) > maxIdempotency {
		return OrganizationDraft{}, apperr.Validation("idempotency key 不能超过 200 bytes")
	}
	var out OrganizationDraft
	err := tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
		draft, err := loadOrganizationDraftForUpdate(ctx, pgxTx, conversationID)
		if err != nil {
			return err
		}
		if draft.CurrentRevision == nil {
			return apperr.Conflict("素材整理 Draft 缺少 current revision")
		}
		requestHash, err := canonjson.SHA256Hex(map[string]any{
			"draft_id":               draft.ID,
			"expected_draft_version": expectedVersion,
		})
		if err != nil {
			return err
		}
		if draft.Status == "confirmed" {
			if draft.ConfirmedRevisionID == nil || *draft.ConfirmedRevisionID != draft.CurrentRevision.ID {
				return apperr.Conflict("素材整理 Draft 已使用其他确认请求完成")
			}
			var storedKey, storedHash *string
			_ = pgxTx.QueryRow(ctx, `
				SELECT confirmation_idempotency_key, confirmation_request_hash
				FROM library_organization_drafts WHERE id = $1
			`, draft.ID).Scan(&storedKey, &storedHash)
			if storedKey == nil || *storedKey != key || storedHash == nil || *storedHash != requestHash {
				return apperr.Conflict("素材整理 Draft 已使用其他确认请求完成")
			}
			out = draft
			return nil
		}
		if draft.Status != "awaiting_confirmation" {
			return apperr.Conflict("当前素材整理 Draft 不在待确认状态")
		}
		if draft.CurrentRevision.Version != expectedVersion {
			return apperr.Conflict("素材整理 Draft version 已变化，请确认最新 revision")
		}
		result, err := s.applyDraftOperations(ctx, pgxTx, draft.CurrentRevision.Payload)
		if err != nil {
			return err
		}
		now := s.now()
		if _, err := pgxTx.Exec(ctx, `
			UPDATE library_organization_draft_revisions SET confirmed_at = $2 WHERE id = $1
		`, draft.CurrentRevision.ID, now); err != nil {
			return err
		}
		if _, err := pgxTx.Exec(ctx, `
			UPDATE library_organization_drafts
			SET status = 'confirmed', confirmed_revision_id = $2, confirmation_idempotency_key = $3,
			    confirmation_request_hash = $4, confirmation_result_json = $5, confirmed_at = $6, updated_at = $6
			WHERE id = $1
		`, draft.ID, draft.CurrentRevision.ID, key, requestHash, result, now); err != nil {
			return err
		}
		if _, err := pgxTx.Exec(ctx, `
			UPDATE agent_conversations SET status = 'completed', updated_at = $2 WHERE id = $1
		`, conversationID, now); err != nil {
			return err
		}
		out, err = loadOrganizationDraft(ctx, pgxTx, conversationID)
		return err
	})
	return out, err
}

func (s Service) applyDraftOperations(ctx context.Context, pgxTx pgx.Tx, payload json.RawMessage) ([]byte, error) {
	var body struct {
		Operations []map[string]any `json:"operations"`
	}
	if err := json.Unmarshal(payload, &body); err != nil {
		return nil, apperr.Validation("素材整理 Draft payload 无效")
	}
	now := s.now()
	applied := 0
	for _, op := range body.Operations {
		kind, _ := op["operation"].(string)
		assetID, _ := op["asset_id"].(string)
		expected := jsonInt(op["expected_revision"])
		target, _ := op["target"].(map[string]any)
		var revision int
		err := pgxTx.QueryRow(ctx, `
			SELECT revision FROM media_library_assets WHERE id = $1 FOR UPDATE
		`, assetID).Scan(&revision)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperr.NotFound("素材不存在")
		}
		if err != nil {
			return nil, err
		}
		if expected > 0 && revision != expected {
			return nil, apperr.Conflict("素材已被其他操作修改")
		}
		switch kind {
		case "rename":
			name, _ := target["display_name"].(string)
			if _, err := pgxTx.Exec(ctx, `
				UPDATE media_library_assets SET display_name = $1, revision = revision + 1, updated_at = $2 WHERE id = $3
			`, name, now, assetID); err != nil {
				return nil, err
			}
		case "move":
			var folderID *string
			if raw, ok := target["folder_id"].(string); ok && raw != "" {
				folderID = &raw
			}
			if _, err := pgxTx.Exec(ctx, `
				UPDATE media_library_assets SET folder_id = $1, revision = revision + 1, updated_at = $2 WHERE id = $3
			`, folderID, now, assetID); err != nil {
				return nil, err
			}
		case "set_tags":
			if _, err := pgxTx.Exec(ctx, `DELETE FROM media_library_asset_tags WHERE asset_id = $1`, assetID); err != nil {
				return nil, err
			}
			rawTags, _ := target["tag_names"].([]any)
			for _, item := range rawTags {
				name, _ := item.(string)
				if name == "" {
					continue
				}
				var tagID string
				err := pgxTx.QueryRow(ctx, `SELECT id FROM media_library_tags WHERE name = $1`, name).Scan(&tagID)
				if errors.Is(err, pgx.ErrNoRows) {
					tagID = clockid.New()
					if _, err := pgxTx.Exec(ctx, `
						INSERT INTO media_library_tags (id, name, normalized_name, created_at, updated_at)
						VALUES ($1, $2, $3, $4, $4)
					`, tagID, name, name, now); err != nil {
						return nil, err
					}
				} else if err != nil {
					return nil, err
				}
				if _, err := pgxTx.Exec(ctx, `
					INSERT INTO media_library_asset_tags (asset_id, tag_id) VALUES ($1, $2) ON CONFLICT DO NOTHING
				`, assetID, tagID); err != nil {
					return nil, err
				}
			}
			if _, err := pgxTx.Exec(ctx, `
				UPDATE media_library_assets SET revision = revision + 1, updated_at = $2 WHERE id = $1
			`, assetID, now); err != nil {
				return nil, err
			}
		case "archive":
			if _, err := pgxTx.Exec(ctx, `
				UPDATE media_library_assets SET is_archived = TRUE, archived_at = $2, revision = revision + 1, updated_at = $2 WHERE id = $1
			`, assetID, now); err != nil {
				return nil, err
			}
		case "restore":
			if _, err := pgxTx.Exec(ctx, `
				UPDATE media_library_assets SET is_archived = FALSE, archived_at = NULL, revision = revision + 1, updated_at = $2 WHERE id = $1
			`, assetID, now); err != nil {
				return nil, err
			}
		case "link_workflow":
			workflowID, _ := target["workflow_id"].(string)
			if _, err := pgxTx.Exec(ctx, `
				INSERT INTO workflow_media_library_assets (workflow_id, media_library_asset_id, created_at)
				VALUES ($1, $2, $3)
				ON CONFLICT DO NOTHING
			`, workflowID, assetID, now); err != nil {
				return nil, err
			}
		default:
			return nil, apperr.Validation("素材整理操作不受支持")
		}
		applied++
	}
	return json.Marshal(map[string]any{"applied_operations": applied})
}

func loadOrganizationDraft(ctx context.Context, q pgx.Tx, conversationID string) (OrganizationDraft, error) {
	return scanOrganizationDraft(ctx, q, conversationID, false)
}

func loadOrganizationDraftForUpdate(ctx context.Context, q pgx.Tx, conversationID string) (OrganizationDraft, error) {
	return scanOrganizationDraft(ctx, q, conversationID, true)
}

func scanOrganizationDraft(ctx context.Context, q pgx.Tx, conversationID string, forUpdate bool) (OrganizationDraft, error) {
	sql := `
		SELECT d.id, d.conversation_id, d.status, d.confirmed_revision_id, d.confirmation_result_json,
		       d.confirmed_at, d.created_at, d.updated_at,
		       r.id, r.version, r.schema_version, r.payload_json, r.payload_hash,
		       r.source_turn_id, r.source_artifact_step_id, r.confirmed_at, r.created_at
		FROM library_organization_drafts d
		LEFT JOIN library_organization_draft_revisions r ON r.id = d.current_revision_id
		WHERE d.conversation_id = $1`
	if forUpdate {
		sql += " FOR UPDATE OF d"
	}
	var d OrganizationDraft
	var result []byte
	var rev OrganizationDraftRevision
	var revID *string
	var version, schema *int
	var payload []byte
	var payloadHash *string
	var sourceTurn, sourceStep *string
	var revConfirmed *time.Time
	var revCreated *time.Time
	err := q.QueryRow(ctx, sql, conversationID).Scan(
		&d.ID, &d.ConversationID, &d.Status, &d.ConfirmedRevisionID, &result,
		&d.ConfirmedAt, &d.CreatedAt, &d.UpdatedAt,
		&revID, &version, &schema, &payload, &payloadHash,
		&sourceTurn, &sourceStep, &revConfirmed, &revCreated,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return OrganizationDraft{}, apperr.NotFound("全局素材整理 Draft 不存在")
	}
	if err != nil {
		return OrganizationDraft{}, err
	}
	if len(result) > 0 {
		d.ConfirmationResult = result
	} else {
		d.ConfirmationResult = nil
	}
	if revID != nil {
		rev.ID = *revID
		if version != nil {
			rev.Version = *version
		}
		if schema != nil {
			rev.SchemaVersion = *schema
		}
		rev.Payload = payload
		if payloadHash != nil {
			rev.PayloadHash = *payloadHash
		}
		rev.SourceTurnID = sourceTurn
		rev.SourceArtifactStepID = sourceStep
		rev.ConfirmedAt = revConfirmed
		if revCreated != nil {
			rev.CreatedAt = *revCreated
		}
		d.CurrentRevision = &rev
	}
	return d, nil
}

func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func jsonInt(v any) int {
	switch typed := v.(type) {
	case float64:
		return int(typed)
	case json.Number:
		n, _ := typed.Int64()
		return int(n)
	case int:
		return typed
	default:
		return 0
	}
}
