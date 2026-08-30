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
		if _, err := graph.StageNew(ctx, pgxTx, creation.product.ID, creation.product.Name, changeSet); err != nil {
			return canonicalCreation{}, Conversation{}, err
		}
		conversation, err := s.openCanvas(ctx, pgxTx, creation.product, key, requestHash, agentSessionID)
		return creation, conversation, err
	})
}

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
		if _, err := graph.StageNew(ctx, pgxTx, creation.product.ID, creation.product.Name, changeSet); err != nil {
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

func expandBirthGraphFromIntake(ctx context.Context, pgxTx *gorm.DB, product Product, selection Selection, assetIDs []string) (bool, error) {
	if len(selection.ImageTypes) == 0 || len(assetIDs) == 0 {
		return false, nil
	}
	sourceID := product.ID
	deliverySpec, err := selectionDeliverySpec(selection)
	if err != nil {
		return false, err
	}
	in := graph.DirectCreateInput{
		ImageTypes:        selectionToImageTypes(selection),
		ReferenceAssetIDs: assetIDs,
		ProductTitle:      product.Name,
		SourceProductID:   &sourceID,
		FactSetVersionID:  product.FactSetVersionID,
		SourceNote:        product.SourceNote,
		DeliverySpec:      deliverySpec,
	}
	identity, err := graph.LoadActiveGraphForUpdate(ctx, pgxTx, product.ID)
	if err != nil {
		return false, err
	}
	if identity == nil {
		changeSet, err := graph.BuildDirectCreateTemplate(in)
		if err != nil {
			return false, err
		}
		_, err = graph.StageNew(ctx, pgxTx, product.ID, product.Name, changeSet)
		return err == nil, err
	}
	applied, err := graph.LoadAppliedGraph(ctx, pgxTx, *identity)
	if err != nil {
		return false, err
	}
	var productSources []graph.AppliedNode
	for _, node := range applied.Nodes {
		if node.NodeType == graph.NodeProductSource {
			productSources = append(productSources, node)
			continue
		}
		return false, nil
	}
	if len(productSources) != 1 {
		return false, nil
	}
	changeSet, err := graph.TemplateForExistingProductSource(productSources[0].ID, applied.Revision, in)
	if err != nil {
		return false, err
	}
	_, err = graph.Mutate(ctx, pgxTx, product.ID, identity.ID, changeSet, graph.HistoryEdit)
	return err == nil, err
}

func liveGraphCounts(ctx context.Context, pgxTx *gorm.DB, productID string) (revision, nodeCount, groupCount int, err error) {
	identity, err := graph.TryLoadActiveGraph(ctx, pgxTx, productID)
	if err != nil || identity == nil {
		return 0, 0, 0, err
	}
	applied, err := graph.LoadAppliedGraph(ctx, pgxTx, *identity)
	if err != nil {
		return 0, 0, 0, err
	}
	return applied.Revision, len(applied.Nodes), len(applied.Groups), nil
}

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
