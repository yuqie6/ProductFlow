package library

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/canonjson"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// OrganizationDraftRevision 是全局 Agent 素材整理 Draft 的一版 payload。
type OrganizationDraftRevision struct {
	ID                   string          `json:"id"`
	Version              int             `json:"version"`        // 从 1 起的 revision 序号
	SchemaVersion        int             `json:"schema_version"` // 当前为 1
	Payload              json.RawMessage `json:"payload"`        // 整理 operations JSON
	PayloadHash          string          `json:"payload_hash"`   // payload 的 canonjson SHA256
	SourceTurnID         *string         `json:"source_turn_id"`
	SourceArtifactStepID *string         `json:"source_artifact_step_id"`
	ConfirmedAt          *time.Time      `json:"confirmed_at"`
	CreatedAt            time.Time       `json:"created_at"`
}

// OrganizationDraft 是绑定全局 conversation 的素材整理 Draft。
type OrganizationDraft struct {
	ID                  string                     `json:"id"`
	ConversationID      string                     `json:"conversation_id"`
	Status              string                     `json:"status"`
	CurrentRevision     *OrganizationDraftRevision `json:"current_revision"` // nil 表示还没有 revision
	ConfirmedRevisionID *string                    `json:"confirmed_revision_id"`
	ConfirmationResult  json.RawMessage            `json:"confirmation_result"` // 未确认时为 null；确认后是应用结果
	ConfirmedAt         *time.Time                 `json:"confirmed_at"`
	CreatedAt           time.Time                  `json:"created_at"`
	UpdatedAt           time.Time                  `json:"updated_at"`
}

// GetOrganizationDraft 按全局 conversation 读取素材整理 Draft。
// 找不到 Draft 返回 NotFound。
func (s Service) GetOrganizationDraft(ctx context.Context, conversationID string) (OrganizationDraft, error) {
	var out OrganizationDraft
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
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
// payload 无效返回 Validation；已确认 Draft 返回 Conflict。
func (s Service) AppendOrganizationDraftRevision(ctx context.Context, conversationID string, payload json.RawMessage, sourceTurnID, sourceStepID string) (OrganizationDraft, error) {
	var out OrganizationDraft
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		_, err := s.AppendOrganizationDraftRevisionTx(ctx, pgxTx, conversationID, payload, sourceTurnID, sourceStepID)
		if err != nil {
			return err
		}
		loaded, err := loadOrganizationDraft(ctx, pgxTx, conversationID)
		out = loaded
		return err
	})
	return out, err
}

// AppendOrganizationDraftRevisionTx 在调用方事务里追加 revision，返回 revision id。
// payload 无法 canonjson 时返回 Validation；已确认 Draft 返回 Conflict。
func (s Service) AppendOrganizationDraftRevisionTx(ctx context.Context, pgxTx *gorm.DB, conversationID string, payload json.RawMessage, sourceTurnID, sourceStepID string) (string, error) {
	hash, err := canonjson.SHA256Hex(json.RawMessage(payload))
	if err != nil {
		return "", apperr.Validation("素材整理 Draft payload 无效")
	}
	draft, err := loadOrganizationDraft(ctx, pgxTx, conversationID)
	if err != nil {
		var e apperr.Error
		if !errors.As(err, &e) || e.Status != 404 {
			return "", err
		}
		now := time.Now().UTC()
		draftID := clockid.New()
		if err := pgxTx.WithContext(ctx).Create(&schema.LibraryOrganizationDrafts{
			ID:             draftID,
			ConversationID: conversationID,
			Status:         "awaiting_confirmation",
			CreatedAt:      now,
			UpdatedAt:      now,
		}).Error; err != nil {
			return "", err
		}
		draft.ID = draftID
		draft.Status = "awaiting_confirmation"
	}
	if draft.Status == "confirmed" {
		return "", apperr.Conflict("素材整理 Draft 已确认，不能再追加 revision")
	}
	var version int
	if err := pgxTx.WithContext(ctx).Model(&schema.LibraryOrganizationDraftRevisions{}).
		Where("draft_id = ?", draft.ID).
		Select("COALESCE(MAX(version), 0)").
		Scan(&version).Error; err != nil {
		return "", err
	}
	revID := clockid.New()
	now := s.now()
	var turnID, stepID *string
	if v := nullableString(sourceTurnID); v != nil {
		turnID = v
	}
	if v := nullableString(sourceStepID); v != nil {
		stepID = v
	}
	if err := pgxTx.WithContext(ctx).Create(&schema.LibraryOrganizationDraftRevisions{
		ID:                   revID,
		DraftID:              draft.ID,
		Version:              version + 1,
		SchemaVersion:        1,
		PayloadJSON:          string(payload),
		PayloadHash:          hash,
		SourceTurnID:         turnID,
		SourceArtifactStepID: stepID,
		CreatedAt:            now,
	}).Error; err != nil {
		return "", err
	}
	if err := pgxTx.WithContext(ctx).Model(&schema.LibraryOrganizationDrafts{}).Where("id = ?", draft.ID).Updates(map[string]any{
		"current_revision_id": revID,
		"status":              "awaiting_confirmation",
		"updated_at":          now,
	}).Error; err != nil {
		return "", err
	}
	return revID, nil
}

// ConfirmOrganizationDraft 应用当前 revision 的组织变更，不复制媒体 bytes。
// idempotency key 为空或过长返回 Validation。
func (s Service) ConfirmOrganizationDraft(ctx context.Context, conversationID string, expectedVersion int, idempotencyKey string) (OrganizationDraft, error) {
	key := strings.TrimSpace(idempotencyKey)
	if key == "" {
		return OrganizationDraft{}, apperr.Validation("idempotency key 不能为空")
	}
	if len([]byte(key)) > maxIdempotency {
		return OrganizationDraft{}, apperr.Validation("idempotency key 不能超过 200 bytes")
	}
	var out OrganizationDraft
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		loaded, err := s.ConfirmOrganizationDraftTx(ctx, pgxTx, conversationID, expectedVersion, key)
		out = loaded
		return err
	})
	return out, err
}

// ConfirmOrganizationDraftTx 在调用方事务里确认 Draft。
// 找不到 Draft 返回 NotFound；无 current revision 或 version 已变返回 Conflict；非待确认或幂等键对不上返回 NotPending。
func (s Service) ConfirmOrganizationDraftTx(ctx context.Context, pgxTx *gorm.DB, conversationID string, expectedVersion int, key string) (OrganizationDraft, error) {
	draft, err := loadOrganizationDraftForUpdate(ctx, pgxTx, conversationID)
	if err != nil {
		return OrganizationDraft{}, err
	}
	if draft.CurrentRevision == nil {
		return OrganizationDraft{}, apperr.Conflict("素材整理 Draft 缺少 current revision")
	}
	requestHash, err := canonjson.SHA256Hex(map[string]any{
		"draft_id":               draft.ID,
		"expected_draft_version": expectedVersion,
	})
	if err != nil {
		return OrganizationDraft{}, err
	}
	if draft.Status == "confirmed" {
		if draft.ConfirmedRevisionID == nil || *draft.ConfirmedRevisionID != draft.CurrentRevision.ID {
			return OrganizationDraft{}, apperr.NotPending("素材整理 Draft 已使用其他确认请求完成")
		}
		var rec schema.LibraryOrganizationDrafts
		_ = pgxTx.WithContext(ctx).Select("confirmation_idempotency_key, confirmation_request_hash").
			Where("id = ?", draft.ID).Take(&rec).Error
		if rec.ConfirmationIdempotencyKey == nil || *rec.ConfirmationIdempotencyKey != key || rec.ConfirmationRequestHash == nil || *rec.ConfirmationRequestHash != requestHash {
			return OrganizationDraft{}, apperr.NotPending("素材整理 Draft 已使用其他确认请求完成")
		}
		return draft, nil
	}
	if draft.Status != "awaiting_confirmation" {
		return OrganizationDraft{}, apperr.NotPending("当前素材整理 Draft 不在待确认状态")
	}
	if draft.CurrentRevision.Version != expectedVersion {
		return OrganizationDraft{}, apperr.Conflict("素材整理 Draft version 已变化，请确认最新 revision")
	}
	result, err := s.applyDraftOperations(ctx, pgxTx, draft.CurrentRevision.Payload)
	if err != nil {
		return OrganizationDraft{}, err
	}
	now := s.now()
	if err := pgxTx.WithContext(ctx).Model(&schema.LibraryOrganizationDraftRevisions{}).Where("id = ?", draft.CurrentRevision.ID).Updates(map[string]any{
		"confirmed_at": now,
	}).Error; err != nil {
		return OrganizationDraft{}, err
	}
	resultStr := string(result)
	if err := pgxTx.WithContext(ctx).Model(&schema.LibraryOrganizationDrafts{}).Where("id = ?", draft.ID).Updates(map[string]any{
		"status":                       "confirmed",
		"confirmed_revision_id":        draft.CurrentRevision.ID,
		"confirmation_idempotency_key": key,
		"confirmation_request_hash":    requestHash,
		"confirmation_result_json":     resultStr,
		"confirmed_at":                 now,
		"updated_at":                   now,
	}).Error; err != nil {
		return OrganizationDraft{}, err
	}
	if err := pgxTx.WithContext(ctx).Model(&schema.AgentConversations{}).Where("id = ?", conversationID).Updates(map[string]any{
		"status":     "completed",
		"updated_at": now,
	}).Error; err != nil {
		return OrganizationDraft{}, err
	}
	return loadOrganizationDraft(ctx, pgxTx, conversationID)
}

// applyDraftOperations 在同一事务里按 expected_revision 执行整理操作。冲突或素材缺失立即失败，不做部分提交。
func (s Service) applyDraftOperations(ctx context.Context, pgxTx *gorm.DB, payload json.RawMessage) ([]byte, error) {
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
		var rec schema.MediaLibraryAssets
		err := pgxTx.WithContext(ctx).Clauses(pfdb.ForUpdate()).Select("revision").Where("id = ?", assetID).Take(&rec).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, apperr.NotFound("素材不存在")
		}
		if err != nil {
			return nil, err
		}
		if expected > 0 && rec.Revision != expected {
			return nil, apperr.Conflict("素材已被其他操作修改")
		}
		switch kind {
		case "rename":
			name, _ := target["display_name"].(string)
			if err := pgxTx.WithContext(ctx).Model(&schema.MediaLibraryAssets{}).Where("id = ?", assetID).Updates(map[string]any{
				"display_name": name,
				"revision":     gorm.Expr("revision + 1"),
				"updated_at":   now,
			}).Error; err != nil {
				return nil, err
			}
		case "move":
			var folderID *string
			if raw, ok := target["folder_id"].(string); ok && raw != "" {
				folderID = &raw
			}
			if err := pgxTx.WithContext(ctx).Model(&schema.MediaLibraryAssets{}).Where("id = ?", assetID).Updates(map[string]any{
				"folder_id":  folderID,
				"revision":   gorm.Expr("revision + 1"),
				"updated_at": now,
			}).Error; err != nil {
				return nil, err
			}
		case "set_tags":
			if err := pgxTx.WithContext(ctx).Where("asset_id = ?", assetID).Delete(&schema.MediaLibraryAssetTags{}).Error; err != nil {
				return nil, err
			}
			rawTags, _ := target["tag_names"].([]any)
			for _, item := range rawTags {
				name, _ := item.(string)
				if name == "" {
					continue
				}
				var tag schema.MediaLibraryTags
				err := pgxTx.WithContext(ctx).Select("id").Where("name = ?", name).Take(&tag).Error
				if errors.Is(err, gorm.ErrRecordNotFound) {
					tag.ID = clockid.New()
					if err := pgxTx.WithContext(ctx).Create(&schema.MediaLibraryTags{
						ID:             tag.ID,
						Name:           name,
						NormalizedName: name,
						CreatedAt:      now,
						UpdatedAt:      now,
					}).Error; err != nil {
						return nil, err
					}
				} else if err != nil {
					return nil, err
				}
				if err := pgxTx.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&schema.MediaLibraryAssetTags{
					AssetID:   assetID,
					TagID:     tag.ID,
					CreatedAt: now,
				}).Error; err != nil {
					return nil, err
				}
			}
			if err := pgxTx.WithContext(ctx).Model(&schema.MediaLibraryAssets{}).Where("id = ?", assetID).Updates(map[string]any{
				"revision":   gorm.Expr("revision + 1"),
				"updated_at": now,
			}).Error; err != nil {
				return nil, err
			}
		case "archive":
			if err := pgxTx.WithContext(ctx).Model(&schema.MediaLibraryAssets{}).Where("id = ?", assetID).Updates(map[string]any{
				"is_archived": true,
				"archived_at": now,
				"revision":    gorm.Expr("revision + 1"),
				"updated_at":  now,
			}).Error; err != nil {
				return nil, err
			}
		case "restore":
			if err := pgxTx.WithContext(ctx).Model(&schema.MediaLibraryAssets{}).Where("id = ?", assetID).Updates(map[string]any{
				"is_archived": false,
				"archived_at": nil,
				"revision":    gorm.Expr("revision + 1"),
				"updated_at":  now,
			}).Error; err != nil {
				return nil, err
			}
		case "link_workflow":
			workflowID, _ := target["workflow_id"].(string)
			if err := pgxTx.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&schema.WorkflowMediaLibraryAssets{
				WorkflowID:          workflowID,
				MediaLibraryAssetID: assetID,
				CreatedAt:           now,
			}).Error; err != nil {
				return nil, err
			}
		default:
			return nil, apperr.Validation("素材整理操作不受支持")
		}
		applied++
	}
	return json.Marshal(map[string]any{"applied_operations": applied})
}

func loadOrganizationDraft(ctx context.Context, q *gorm.DB, conversationID string) (OrganizationDraft, error) {
	return scanOrganizationDraft(ctx, q, conversationID, false)
}

func loadOrganizationDraftForUpdate(ctx context.Context, q *gorm.DB, conversationID string) (OrganizationDraft, error) {
	return scanOrganizationDraft(ctx, q, conversationID, true)
}

type organizationDraftScan struct {
	ID                     string     `gorm:"column:id"`
	ConversationID         string     `gorm:"column:conversation_id"`
	Status                 string     `gorm:"column:status"`
	ConfirmedRevisionID    *string    `gorm:"column:confirmed_revision_id"`
	ConfirmationResultJSON *string    `gorm:"column:confirmation_result_json"`
	ConfirmedAt            *time.Time `gorm:"column:confirmed_at"`
	CreatedAt              time.Time  `gorm:"column:created_at"`
	UpdatedAt              time.Time  `gorm:"column:updated_at"`
	RevID                  *string    `gorm:"column:rev_id"`
	RevVersion             *int       `gorm:"column:rev_version"`
	RevSchema              *int       `gorm:"column:rev_schema"`
	RevPayload             *string    `gorm:"column:rev_payload"`
	RevPayloadHash         *string    `gorm:"column:rev_payload_hash"`
	RevSourceTurn          *string    `gorm:"column:rev_source_turn"`
	RevSourceStep          *string    `gorm:"column:rev_source_step"`
	RevConfirmedAt         *time.Time `gorm:"column:rev_confirmed_at"`
	RevCreatedAt           *time.Time `gorm:"column:rev_created_at"`
}

// scanOrganizationDraft 联表读当前 revision。forUpdate 只锁 drafts 别名 d，避免锁住 revision 行。
func scanOrganizationDraft(ctx context.Context, q *gorm.DB, conversationID string, forUpdate bool) (OrganizationDraft, error) {
	query := q.WithContext(ctx).Table("library_organization_drafts AS d").
		Select(`d.id, d.conversation_id, d.status, d.confirmed_revision_id, d.confirmation_result_json,
		       d.confirmed_at, d.created_at, d.updated_at,
		       r.id AS rev_id, r.version AS rev_version, r.schema_version AS rev_schema, r.payload_json AS rev_payload, r.payload_hash AS rev_payload_hash,
		       r.source_turn_id AS rev_source_turn, r.source_artifact_step_id AS rev_source_step, r.confirmed_at AS rev_confirmed_at, r.created_at AS rev_created_at`).
		Joins("LEFT JOIN library_organization_draft_revisions r ON r.id = d.current_revision_id").
		Where("d.conversation_id = ?", conversationID)
	if forUpdate {
		query = query.Clauses(pfdb.ForUpdateOf("d"))
	}
	var row organizationDraftScan
	err := query.Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return OrganizationDraft{}, apperr.NotFound("全局素材整理 Draft 不存在")
	}
	if err != nil {
		return OrganizationDraft{}, err
	}
	d := OrganizationDraft{
		ID:                  row.ID,
		ConversationID:      row.ConversationID,
		Status:              row.Status,
		ConfirmedRevisionID: row.ConfirmedRevisionID,
		ConfirmedAt:         row.ConfirmedAt,
		CreatedAt:           row.CreatedAt,
		UpdatedAt:           row.UpdatedAt,
	}
	if row.ConfirmationResultJSON != nil && *row.ConfirmationResultJSON != "" {
		d.ConfirmationResult = json.RawMessage(*row.ConfirmationResultJSON)
	} else {
		d.ConfirmationResult = nil
	}
	if row.RevID != nil {
		rev := OrganizationDraftRevision{
			ID:                   *row.RevID,
			SourceTurnID:         row.RevSourceTurn,
			SourceArtifactStepID: row.RevSourceStep,
			ConfirmedAt:          row.RevConfirmedAt,
		}
		if row.RevVersion != nil {
			rev.Version = *row.RevVersion
		}
		if row.RevSchema != nil {
			rev.SchemaVersion = *row.RevSchema
		}
		if row.RevPayload != nil {
			rev.Payload = json.RawMessage(*row.RevPayload)
		}
		if row.RevPayloadHash != nil {
			rev.PayloadHash = *row.RevPayloadHash
		}
		if row.RevCreatedAt != nil {
			rev.CreatedAt = *row.RevCreatedAt
		}
		d.CurrentRevision = &rev
	}
	return d, nil
}

func nullableString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
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
