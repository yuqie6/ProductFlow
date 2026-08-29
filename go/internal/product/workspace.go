package product

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/storage"
	"github.com/yuqie6/productflow/internal/platform/tx"
)

func (s Service) CreateAgentDraft(ctx context.Context, name, idempotencyKey string, agentSessionID *string) (WorkspaceSnapshotResponse, error) {
	normalizedName, err := normalizeName(name)
	if err != nil {
		return WorkspaceSnapshotResponse{}, err
	}
	key, err := normalizeIdempotencyKey(idempotencyKey)
	if err != nil {
		return WorkspaceSnapshotResponse{}, err
	}
	requestHash := draftRequestHash(normalizedName, agentSessionID)
	return s.upsertWorkspace(ctx, key, requestHash, func(pgxTx pgx.Tx) (canonicalCreation, Conversation, error) {
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
		conversation, err := openCanvas(ctx, pgxTx, creation.product, key, requestHash, agentSessionID)
		return creation, conversation, err
	})
}

func (s Service) CreateAgentWorkspace(ctx context.Context, name, selectionJSON, idempotencyKey string, agentSessionID *string, uploads []Upload) (WorkspaceCreateResponse, error) {
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
	snap, err := s.upsertWorkspace(ctx, key, requestHash, func(pgxTx pgx.Tx) (canonicalCreation, Conversation, error) {
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
		changeSet, err := graph.BuildDirectCreateTemplate(graph.DirectCreateInput{
			ImageTypes:        selectionToImageTypes(selection),
			ReferenceAssetIDs: assetIDs(creation.assets),
			ProductTitle:      creation.product.Name,
			SourceProductID:   &sourceID,
			FactSetVersionID:  creation.product.FactSetVersionID,
			SourceNote:        creation.product.SourceNote,
		})
		if err != nil {
			compensation.Rollback()
			return canonicalCreation{}, Conversation{}, err
		}
		if _, err := graph.StageNew(ctx, pgxTx, creation.product.ID, creation.product.Name, changeSet); err != nil {
			compensation.Rollback()
			return canonicalCreation{}, Conversation{}, err
		}
		conversation, err := openCanvas(ctx, pgxTx, creation.product, key, requestHash, agentSessionID)
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

func (s Service) upsertWorkspace(
	ctx context.Context,
	key, requestHash string,
	create func(pgx.Tx) (canonicalCreation, Conversation, error),
) (WorkspaceSnapshotResponse, error) {
	var snap WorkspaceSnapshotResponse
	err := tx.With(ctx, s.Pool, func(pgxTx pgx.Tx) error {
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
		if !errors.Is(err, pgx.ErrNoRows) {
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

func (s Service) stageUploads(ctx context.Context, pgxTx pgx.Tx, compensation *storage.Compensation, in CreateInput, setCover, writeFacts bool) (canonicalCreation, error) {
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

func openCanvas(ctx context.Context, tx pgx.Tx, product Product, key, requestHash string, agentSessionID *string) (Conversation, error) {
	sessionID := ""
	if agentSessionID != nil && *agentSessionID != "" {
		productID, status, err := loadSessionProduct(ctx, tx, *agentSessionID)
		if err != nil {
			return Conversation{}, err
		}
		if status != "active" {
			return Conversation{}, apperr.Conflict("已归档的 Agent Session 不能创建商品工作区")
		}
		if productID == nil {
			id, _, _, err := insertSession(ctx, tx, product.Name, product.ID)
			if err != nil {
				return Conversation{}, err
			}
			sessionID = id
		} else if *productID != product.ID {
			return Conversation{}, apperr.Conflict("Agent Session 不属于当前商品")
		} else {
			sessionID = *agentSessionID
		}
	} else {
		id, _, _, err := insertSession(ctx, tx, product.Name, product.ID)
		if err != nil {
			return Conversation{}, err
		}
		sessionID = id
	}
	return insertConversation(ctx, tx, sessionID, product.ID, key, requestHash)
}

func loadWorkspaceSnapshot(ctx context.Context, tx pgx.Tx, conversation Conversation, created bool) (WorkspaceSnapshotResponse, error) {
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
