// Package product 实现商品四条出生命令、facts、封面与商品图库，HTTP 合同对齐 Python。
package product

import (
	"context"
	"time"

	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/media"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/storage"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

// Service 拥有商品出生、facts、封面与商品图库命令。
type Service struct {
	DB    *gorm.DB
	Media media.Store
	// Now 可注入，图库「最近生成」目录用它锚定 30 天窗口。
	Now func() time.Time
}

func (s Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

// GetAsset 按 id 读取商品图片身份（含媒体元数据）。
func (s Service) GetAsset(ctx context.Context, assetID string) (ImageAsset, error) {
	return loadAsset(ctx, s.DB, assetID)
}

type CreateInput struct {
	Name       string
	Category   string
	Price      string
	SourceNote string
	Uploads    []Upload
	SetCover   bool
}

func (s Service) CreateWithoutGraph(ctx context.Context, in CreateInput) (CreateResponse, error) {
	creation, err := s.createCanonical(ctx, in, true, false, nil)
	if err != nil {
		return CreateResponse{}, err
	}
	return CreateResponse{
		Product:       serializeDetail(creation.product),
		CreatedAssets: serializeAssets(creation.assets),
	}, nil
}

func (s Service) CreateDirect(ctx context.Context, in CreateInput, imageTypes []graph.DirectCreateImageType, generationSpec map[string]any, deliverySpec map[string]any) (DirectCreateResponse, error) {
	var result DirectCreateResponse
	err := s.createWithGraph(ctx, in, true, true, func(tx *gorm.DB, creation canonicalCreation) error {
		sourceID := creation.product.ID
		factID := creation.product.FactSetVersionID
		changeSet, err := graph.BuildDirectCreateTemplate(graph.DirectCreateInput{
			ImageTypes:        imageTypes,
			ReferenceAssetIDs: assetIDs(creation.assets),
			ProductTitle:      creation.product.Name,
			SourceProductID:   &sourceID,
			FactSetVersionID:  factID,
			SourceNote:        creation.product.SourceNote,
			GenerationSpec:    generationSpec,
			DeliverySpec:      deliverySpec,
		})
		if err != nil {
			return err
		}
		cmd, err := graph.StageNew(ctx, tx, creation.product.ID, creation.product.Name, changeSet)
		if err != nil {
			return err
		}
		result = DirectCreateResponse{
			Product:       serializeDetail(creation.product),
			CreatedAssets: serializeAssets(creation.assets),
			Graph:         projectGraph(cmd, creation.product, creation.facts),
		}
		return nil
	})
	return result, err
}

func (s Service) Get(ctx context.Context, id string) (Detail, error) {
	var detail Detail
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		p, err := loadProduct(ctx, pgxTx, id)
		if err != nil {
			return err
		}
		detail = serializeDetail(p)
		return nil
	})
	return detail, err
}

func (s Service) List(ctx context.Context, page, pageSize int, q, sort string) (ListResponse, error) {
	var out ListResponse
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		items, total, err := listProducts(ctx, pgxTx, page, pageSize, q, sort)
		if err != nil {
			return err
		}
		out = ListResponse{Items: items, Total: total, Page: page, PageSize: pageSize}
		if out.Page < 1 {
			out.Page = 1
		}
		if out.PageSize < 1 {
			out.PageSize = 20
		}
		return nil
	})
	return out, err
}

func (s Service) AssetForDownload(ctx context.Context, assetID string) (ImageAsset, error) {
	return loadAsset(ctx, s.DB, assetID)
}

type canonicalCreation struct {
	product Product
	assets  []ImageAsset
	facts   []map[string]any
}

func (s Service) createCanonical(ctx context.Context, in CreateInput, setCover, writeFacts bool, after func(*gorm.DB, canonicalCreation) error) (canonicalCreation, error) {
	var created canonicalCreation
	err := s.createWithGraph(ctx, in, setCover, writeFacts, func(tx *gorm.DB, creation canonicalCreation) error {
		created = creation
		if after != nil {
			return after(tx, creation)
		}
		return nil
	})
	return created, err
}

func (s Service) createWithGraph(ctx context.Context, in CreateInput, setCover, writeFacts bool, after func(*gorm.DB, canonicalCreation) error) error {
	name, err := normalizeName(in.Name)
	if err != nil {
		return err
	}
	category, err := optionalText(in.Category, "类目", 120)
	if err != nil {
		return err
	}
	price, err := normalizePrice(in.Price)
	if err != nil {
		return err
	}
	note, err := optionalText(in.SourceNote, "备注", 4000)
	if err != nil {
		return err
	}
	if len(in.Uploads) == 0 {
		return apperr.Validation("至少上传一张商品参考图")
	}
	if len(in.Uploads) > 6 {
		return apperr.Validation("商品参考图最多上传 6 张")
	}
	var compensation storage.Compensation
	err = tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		product, err := insertProduct(ctx, pgxTx, name, category, price, note)
		if err != nil {
			return err
		}
		assets := make([]ImageAsset, 0, len(in.Uploads))
		for _, upload := range in.Uploads {
			obj, err := s.Media.Stage(ctx, pgxTx, upload.Content, upload.MIMEType, &compensation)
			if err != nil {
				return err
			}
			asset, err := insertAsset(ctx, pgxTx, product.ID, obj.ID, upload.Filename)
			if err != nil {
				return err
			}
			asset.MIMEType = obj.MIMEType
			asset.ByteSize = intPtr(obj.ByteSize)
			asset.Width = intPtr(obj.Width)
			asset.Height = intPtr(obj.Height)
			asset.StoragePath = obj.StoragePath
			assets = append(assets, asset)
		}
		if setCover {
			if err := assignCover(ctx, pgxTx, product.ID, assets[0].ID); err != nil {
				return err
			}
			product.CoverImageAssetID = &assets[0].ID
		}
		var facts []map[string]any
		if writeFacts {
			facts = metadataFacts(product)
			if len(facts) > 0 {
				id, _, err := insertFactSet(ctx, pgxTx, product.ID, facts)
				if err != nil {
					return err
				}
				product.FactSetVersionID = &id
			}
		}
		loaded, err := loadProduct(ctx, pgxTx, product.ID)
		if err != nil {
			return err
		}
		loadedAssets, err := loadAssetsByIDs(ctx, pgxTx, product.ID, assetIDs(assets))
		if err != nil {
			return err
		}
		creation := canonicalCreation{product: loaded, assets: loadedAssets, facts: facts}
		if after != nil {
			if err := after(pgxTx, creation); err != nil {
				return err
			}
		}
		compensation.Release()
		return nil
	})
	if err != nil {
		compensation.Rollback()
		return err
	}
	return nil
}

func (s Service) stageNameOnly(ctx context.Context, tx *gorm.DB, name string) (canonicalCreation, error) {
	product, err := insertProduct(ctx, tx, name, nil, nil, nil)
	if err != nil {
		return canonicalCreation{}, err
	}
	facts := metadataFacts(product)
	if len(facts) > 0 {
		id, _, err := insertFactSet(ctx, tx, product.ID, facts)
		if err != nil {
			return canonicalCreation{}, err
		}
		product.FactSetVersionID = &id
	}
	loaded, err := loadProduct(ctx, tx, product.ID)
	if err != nil {
		return canonicalCreation{}, err
	}
	return canonicalCreation{product: loaded, facts: facts}, nil
}

func assetIDs(assets []ImageAsset) []string {
	ids := make([]string, 0, len(assets))
	for _, asset := range assets {
		ids = append(ids, asset.ID)
	}
	return ids
}

func intPtr(n int) *int { return &n }
