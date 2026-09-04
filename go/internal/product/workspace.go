package product

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/platform/agentsession"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/storage"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

// CreateAgentDraft 是名称-only 出生：写 product_source 图、空 intake、不设封面。
// 商品名或 Idempotency-Key 非法返回 Validation；同 key 哈希不同返回 Conflict。缺 Canvas 写入器返回 Internal。
func (s Service) CreateAgentDraft(ctx context.Context, name, idempotencyKey string, agentSessionID *string) (WorkspaceSnapshotResponse, error) {
	ctx = graph.WithProductGuard(ctx, GraphGuard{})
	normalizedName, err := normalizeName(name)
	if err != nil {
		return WorkspaceSnapshotResponse{}, err
	}
	key, err := normalizeIdempotencyKey(idempotencyKey)
	if err != nil {
		return WorkspaceSnapshotResponse{}, err
	}
	requestHash := draftRequestHash(normalizedName, agentSessionID)
	return s.upsertWorkspace(ctx, key, requestHash, func(pgxTx *gorm.DB) (canonicalCreation, Conversation, error) {
		creation, err := s.stageNameOnly(ctx, pgxTx, normalizedName)
		if err != nil {
			return canonicalCreation{}, Conversation{}, err
		}
		changeSet, err := graph.BuildProductSourceCreateGraph(creation.product.Name, creation.product.ID, creation.product.FactSetVersionID)
		if err != nil {
			return canonicalCreation{}, Conversation{}, err
		}
		if _, err := graph.WriteTx(ctx, pgxTx, graph.Command{
			ProductID: creation.product.ID,
			Title:     creation.product.Name,
			ChangeSet: changeSet,
		}); err != nil {
			return canonicalCreation{}, Conversation{}, err
		}
		conversation, err := s.openCanvas(ctx, pgxTx, creation.product, key, requestHash, agentSessionID)
		return creation, conversation, err
	})
}

// CreateAgentWorkspace 是表单完整 Agent 出生：写商品、参考图、intake 与直连模板图，并打开对话。Idempotency-Key 去重。
func (s Service) CreateAgentWorkspace(ctx context.Context, name, selectionJSON, idempotencyKey string, agentSessionID *string, uploads []Upload) (WorkspaceCreateResponse, error) {
	ctx = graph.WithProductGuard(ctx, GraphGuard{})
	normalizedName, err := normalizeName(name)
	if err != nil {
		return WorkspaceCreateResponse{}, err
	}
	key, err := normalizeIdempotencyKey(idempotencyKey)
	if err != nil {
		return WorkspaceCreateResponse{}, err
	}
	if len(uploads) == 0 {
		return WorkspaceCreateResponse{}, apperr.Validation("至少上传一张商品参考图")
	}
	if len(uploads) > 6 {
		return WorkspaceCreateResponse{}, apperr.Validation("商品参考图最多上传 6 张")
	}
	selection, err := parseSelection(selectionJSON)
	if err != nil {
		return WorkspaceCreateResponse{}, err
	}
	requestHash := workspaceRequestHash(normalizedName, selection, uploads, agentSessionID)
	snap, err := s.upsertWorkspace(ctx, key, requestHash, func(pgxTx *gorm.DB) (canonicalCreation, Conversation, error) {
		var compensation storage.Compensation
		creation, err := s.stageUploads(ctx, pgxTx, &compensation, CreateInput{
			Name:    normalizedName,
			Uploads: uploads,
		}, false, true)
		if err != nil {
			compensation.Rollback()
			return canonicalCreation{}, Conversation{}, err
		}
		payload, err := intakePayload(selection, assetIDs(creation.assets))
		if err != nil {
			compensation.Rollback()
			return canonicalCreation{}, Conversation{}, err
		}
		if err := setIntake(ctx, pgxTx, creation.product.ID, payload); err != nil {
			compensation.Rollback()
			return canonicalCreation{}, Conversation{}, err
		}
		creation.product.IntakeJSON = payload
		v := 1
		creation.product.IntakeVersion = &v
		sourceID := creation.product.ID
		deliverySpec, err := selectionDeliverySpec(selection)
		if err != nil {
			compensation.Rollback()
			return canonicalCreation{}, Conversation{}, err
		}
		changeSet, err := graph.BuildDirectCreateTemplate(graph.DirectCreateInput{
			ImageTypes:        selectionToImageTypes(selection),
			ReferenceAssetIDs: assetIDs(creation.assets),
			ProductTitle:      creation.product.Name,
			SourceProductID:   &sourceID,
			FactSetVersionID:  creation.product.FactSetVersionID,
			SourceNote:        creation.product.SourceNote,
			DeliverySpec:      deliverySpec,
		})
		if err != nil {
			compensation.Rollback()
			return canonicalCreation{}, Conversation{}, err
		}
		if _, err := graph.WriteTx(ctx, pgxTx, graph.Command{
			ProductID: creation.product.ID,
			Title:     creation.product.Name,
			ChangeSet: changeSet,
		}); err != nil {
			compensation.Rollback()
			return canonicalCreation{}, Conversation{}, err
		}
		conversation, err := s.openCanvas(ctx, pgxTx, creation.product, key, requestHash, agentSessionID)
		if err != nil {
			compensation.Rollback()
			return canonicalCreation{}, Conversation{}, err
		}
		compensation.Release()
		return creation, conversation, nil
	})
	if err != nil {
		return WorkspaceCreateResponse{}, err
	}
	return WorkspaceCreateResponse{
		Product:       snap.Product,
		CreatedAssets: snap.CreatedAssets,
		Conversation:  snap.Conversation,
	}, nil
}

// GetAgentWorkspace 按 conversation_id 读取商品工作区快照。找不到返回 NotFound。
func (s Service) GetAgentWorkspace(ctx context.Context, conversationID string) (WorkspaceSnapshotResponse, error) {
	var snap WorkspaceSnapshotResponse
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		conversation, err := loadConversationByID(ctx, pgxTx, conversationID)
		if err != nil {
			return err
		}
		loaded, err := loadWorkspaceSnapshot(ctx, pgxTx, conversation, false)
		if err != nil {
			return err
		}
		snap = loaded
		return nil
	})
	return snap, err
}

// FinalizeAgentIntake 把名称-only 图按模板展开套图，并写入参考图与 intake。重复 Idempotency-Key 且哈希不同返回 Conflict。
func (s Service) FinalizeAgentIntake(ctx context.Context, conversationID, selectionJSON, idempotencyKey string, sourceNote *string, uploads []Upload, taskID *string) (WorkspaceSnapshotResponse, error) {
	_ = taskID
	ctx = graph.WithProductGuard(ctx, GraphGuard{})
	key, err := normalizeIdempotencyKey(idempotencyKey)
	if err != nil {
		return WorkspaceSnapshotResponse{}, err
	}
	if len(uploads) == 0 {
		return WorkspaceSnapshotResponse{}, apperr.Validation("至少上传一张商品参考图")
	}
	if len(uploads) > 6 {
		return WorkspaceSnapshotResponse{}, apperr.Validation("商品参考图最多上传 6 张")
	}
	selection, err := parseSelection(selectionJSON)
	if err != nil {
		return WorkspaceSnapshotResponse{}, err
	}
	requestHash := intakeRequestHash(selection, uploads)
	var snap WorkspaceSnapshotResponse
	err = tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		conversation, storedKey, storedHash, err := loadConversationIntakeForUpdate(ctx, pgxTx, conversationID)
		if err != nil {
			return err
		}
		if conversation.ProductID == nil {
			return apperr.Conflict("Agent 商品工作空间聚合不完整")
		}
		product, err := loadProductForUpdate(ctx, pgxTx, *conversation.ProductID)
		if err != nil {
			return err
		}
		if storedKey != nil {
			if *storedKey != key || storedHash == nil || *storedHash != requestHash {
				return apperr.Conflict("Agent 商品输入已经确认，不能提交不同请求")
			}
			if len(product.IntakeJSON) == 0 {
				return apperr.Conflict("Agent 商品输入幂等记录与商品 intake 不一致")
			}
			loaded, loadErr := loadWorkspaceSnapshot(ctx, pgxTx, conversation, false)
			if loadErr != nil {
				return loadErr
			}
			snap = loaded
			return nil
		}
		if storedHash != nil {
			return apperr.Conflict("Agent 商品输入幂等记录不完整")
		}
		if len(product.IntakeJSON) > 0 || product.IntakeVersion != nil {
			return apperr.Conflict("Agent 商品输入已经确认")
		}
		if conversation.Status == "awaiting_confirmation" {
			return apperr.Conflict("当前 Agent conversation 状态不允许确认商品输入")
		}
		var compensation storage.Compensation
		assets, err := s.appendUploads(ctx, pgxTx, &compensation, product.ID, uploads)
		if err != nil {
			compensation.Rollback()
			return err
		}
		if sourceNote != nil {
			note := strings.TrimSpace(*sourceNote)
			var notePtr *string
			if note != "" {
				notePtr = &note
			}
			if err := setSourceNote(ctx, pgxTx, product.ID, notePtr); err != nil {
				compensation.Rollback()
				return err
			}
		}
		payload, err := intakePayload(selection, assetIDs(assets))
		if err != nil {
			compensation.Rollback()
			return err
		}
		if err := setIntake(ctx, pgxTx, product.ID, payload); err != nil {
			compensation.Rollback()
			return err
		}
		if err := setConversationIntake(ctx, pgxTx, conversation.ID, key, requestHash); err != nil {
			compensation.Rollback()
			return err
		}
		product, err = loadProduct(ctx, pgxTx, product.ID)
		if err != nil {
			compensation.Rollback()
			return err
		}
		if _, err := expandBirthGraphFromIntake(ctx, pgxTx, product, selection, assetIDs(assets)); err != nil {
			compensation.Rollback()
			return err
		}
		if err := refreshWorkspaceSessionSummary(ctx, pgxTx, conversation); err != nil {
			compensation.Rollback()
			return err
		}
		updated, err := loadConversationByID(ctx, pgxTx, conversation.ID)
		if err != nil {
			compensation.Rollback()
			return err
		}
		loaded, err := loadWorkspaceSnapshot(ctx, pgxTx, updated, true)
		if err != nil {
			compensation.Rollback()
			return err
		}
		compensation.Release()
		snap = loaded
		return nil
	})
	return snap, err
}

// appendUploads 在已有事务里 stage 媒体并写成商品图身份；失败须由调用方 Rollback compensation。
func (s Service) appendUploads(ctx context.Context, pgxTx *gorm.DB, compensation *storage.Compensation, productID string, uploads []Upload) ([]ImageAsset, error) {
	assets := make([]ImageAsset, 0, len(uploads))
	for _, upload := range uploads {
		obj, err := s.Media.Stage(ctx, pgxTx, upload.Content, upload.MIMEType, compensation)
		if err != nil {
			return nil, err
		}
		asset, err := insertAsset(ctx, pgxTx, productID, obj.ID, upload.Filename)
		if err != nil {
			return nil, err
		}
		asset.MIMEType = obj.MIMEType
		asset.ByteSize = intPtr(obj.ByteSize)
		asset.Width = intPtr(obj.Width)
		asset.Height = intPtr(obj.Height)
		asset.StoragePath = obj.StoragePath
		assets = append(assets, asset)
	}
	return loadAssetsByIDs(ctx, pgxTx, productID, assetIDs(assets))
}

// expandBirthGraphFromIntake 只在名称-only（仅一个 product_source）图上按模板展开套图。
// 已有其它节点则不改图并返回 false，避免二次 finalize 覆盖用户编辑。
// 副作用：graph.ExpandBirth 写 workflow_graphs。失败由调用方 Rollback。
func expandBirthGraphFromIntake(ctx context.Context, pgxTx *gorm.DB, product Product, selection Selection, assetIDs []string) (bool, error) {
	sourceID := product.ID
	deliverySpec, err := selectionDeliverySpec(selection)
	if err != nil {
		return false, err
	}
	expanded, _, err := graph.ExpandBirth(ctx, pgxTx, product.ID, product.Name, graph.DirectCreateInput{
		ImageTypes:        selectionToImageTypes(selection),
		ReferenceAssetIDs: assetIDs,
		ProductTitle:      product.Name,
		SourceProductID:   &sourceID,
		FactSetVersionID:  product.FactSetVersionID,
		SourceNote:        product.SourceNote,
		DeliverySpec:      deliverySpec,
	})
	return expanded, err
}

func liveGraphCounts(ctx context.Context, pgxTx *gorm.DB, productID string) (revision, nodeCount, groupCount int, err error) {
	live, err := graph.TryLive(ctx, pgxTx, productID)
	if err != nil || live == nil {
		return 0, 0, 0, err
	}
	return live.Applied.Revision, len(live.Applied.Nodes), len(live.Applied.Groups), nil
}

// upsertWorkspace 按 Idempotency-Key 复用已有对话。
// 同一 key 且哈希相同：原样返回已有快照（Created=false）。哈希不同：Conflict，禁止用同一 key 出生不同商品。
// 插入撞 23505 再读一次当命中。create 回调须只写调用方事务。
func (s Service) upsertWorkspace(
	ctx context.Context,
	key, requestHash string,
	create func(*gorm.DB) (canonicalCreation, Conversation, error),
) (WorkspaceSnapshotResponse, error) {
	var snap WorkspaceSnapshotResponse
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		existing, err := loadConversationByKey(ctx, pgxTx, key)
		if err == nil {
			storedHash, hashErr := conversationRequestHash(ctx, pgxTx, existing.ID)
			if hashErr != nil {
				return hashErr
			}
			if storedHash != requestHash {
				return apperr.Conflict("相同 Idempotency-Key 不能创建不同的 Agent 商品")
			}
			loaded, loadErr := loadWorkspaceSnapshot(ctx, pgxTx, existing, false)
			if loadErr != nil {
				return loadErr
			}
			snap = loaded
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		creation, conversation, err := create(pgxTx)
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				existing, loadErr := loadConversationByKey(ctx, pgxTx, key)
				if loadErr != nil {
					return err
				}
				storedHash, hashErr := conversationRequestHash(ctx, pgxTx, existing.ID)
				if hashErr != nil {
					return hashErr
				}
				if storedHash != requestHash {
					return apperr.Conflict("相同 Idempotency-Key 不能创建不同的 Agent 商品")
				}
				loaded, loadErr := loadWorkspaceSnapshot(ctx, pgxTx, existing, false)
				if loadErr != nil {
					return loadErr
				}
				snap = loaded
				return nil
			}
			return err
		}
		snap = WorkspaceSnapshotResponse{
			Created:         true,
			IntakeFinalized: len(creation.product.IntakeJSON) > 0,
			Product:         serializeDetail(creation.product),
			CreatedAssets:   serializeAssets(creation.assets),
			Conversation:    conversation,
		}
		return nil
	})
	return snap, err
}

// stageUploads 在调用方事务里写商品行、stage 媒体、写成参考图身份；可选设封面与首版 facts。
// 不 commit。媒体或写库失败须由调用方 Rollback compensation，否则磁盘留无主文件。
func (s Service) stageUploads(ctx context.Context, pgxTx *gorm.DB, compensation *storage.Compensation, in CreateInput, setCover, writeFacts bool) (canonicalCreation, error) {
	product, err := insertProduct(ctx, pgxTx, in.Name, nil, nil, nil)
	if err != nil {
		return canonicalCreation{}, err
	}
	assets := make([]ImageAsset, 0, len(in.Uploads))
	for _, upload := range in.Uploads {
		obj, err := s.Media.Stage(ctx, pgxTx, upload.Content, upload.MIMEType, compensation)
		if err != nil {
			return canonicalCreation{}, err
		}
		asset, err := insertAsset(ctx, pgxTx, product.ID, obj.ID, upload.Filename)
		if err != nil {
			return canonicalCreation{}, err
		}
		asset.MIMEType = obj.MIMEType
		asset.ByteSize = intPtr(obj.ByteSize)
		asset.Width = intPtr(obj.Width)
		asset.Height = intPtr(obj.Height)
		asset.StoragePath = obj.StoragePath
		assets = append(assets, asset)
	}
	if setCover && len(assets) > 0 {
		if err := assignCover(ctx, pgxTx, product.ID, assets[0].ID); err != nil {
			return canonicalCreation{}, err
		}
		product.CoverImageAssetID = &assets[0].ID
	}
	var facts []map[string]any
	if writeFacts {
		facts = metadataFacts(product)
		if len(facts) > 0 {
			id, _, err := insertFactSet(ctx, pgxTx, product.ID, facts)
			if err != nil {
				return canonicalCreation{}, err
			}
			product.FactSetVersionID = &id
		}
	}
	loaded, err := loadProduct(ctx, pgxTx, product.ID)
	if err != nil {
		return canonicalCreation{}, err
	}
	loadedAssets, err := loadAssetsByIDs(ctx, pgxTx, product.ID, assetIDs(assets))
	if err != nil {
		return canonicalCreation{}, err
	}
	return canonicalCreation{product: loaded, assets: loadedAssets, facts: facts}, nil
}

func (s Service) openCanvas(ctx context.Context, tx *gorm.DB, product Product, key, requestHash string, agentSessionID *string) (Conversation, error) {
	if s.Canvas == nil {
		return Conversation{}, apperr.Internal("商品工作区缺少会话写入器")
	}
	_, conv, err := s.Canvas(ctx, tx, product.ID, product.Name, key, requestHash, agentSessionID)
	if err != nil {
		return Conversation{}, err
	}
	if err := refreshWorkspaceSessionSummary(ctx, tx, conv); err != nil {
		return Conversation{}, err
	}
	return conv, nil
}

func refreshWorkspaceSessionSummary(ctx context.Context, pgxTx *gorm.DB, conversation Conversation) error {
	if conversation.SessionID == nil || strings.TrimSpace(*conversation.SessionID) == "" {
		return nil
	}
	return agentsession.RefreshSummary(ctx, pgxTx, *conversation.SessionID)
}

// loadWorkspaceSnapshot 只回放 intake 里的 reference_asset_ids，不把整库商品图当 CreatedAssets。
func loadWorkspaceSnapshot(ctx context.Context, tx *gorm.DB, conversation Conversation, created bool) (WorkspaceSnapshotResponse, error) {
	if conversation.ProductID == nil {
		return WorkspaceSnapshotResponse{}, apperr.Conflict("Agent 商品工作空间聚合不完整")
	}
	product, err := loadProduct(ctx, tx, *conversation.ProductID)
	if err != nil {
		return WorkspaceSnapshotResponse{}, err
	}
	var assets []ImageAsset
	if len(product.IntakeJSON) > 0 {
		var payload struct {
			ReferenceAssetIDs []string `json:"reference_asset_ids"`
		}
		_ = json.Unmarshal(product.IntakeJSON, &payload)
		assets, err = loadAssetsByIDs(ctx, tx, product.ID, payload.ReferenceAssetIDs)
		if err != nil {
			return WorkspaceSnapshotResponse{}, err
		}
	}
	return WorkspaceSnapshotResponse{
		Created:         created,
		IntakeFinalized: len(product.IntakeJSON) > 0,
		Product:         serializeDetail(product),
		CreatedAssets:   serializeAssets(assets),
		Conversation:    conversation,
	}, nil
}
