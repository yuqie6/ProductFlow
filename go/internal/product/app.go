// Package product 实现商品四条出生命令、facts、封面与商品图库。
//
// 职责：无图创建、直连创建、Agent 名称-only、Agent 表单工作区。商品图、封面、图库、节点绑定
// 一律引用 ProductImageAsset id，合同与 intake 不得出现存储路径。
//
// 调用时机：HTTP 出生/facts/图库走 [Service]；graph 编译与跑图通过 [GraphGuard] 读商品与 fact，
// 不得反向 import 本包。创建页看图起草走 [SourceNoteGenerator]，不走画布 cook。
//
// 副作用：写 products、product_image_assets、fact 版本、intake JSON、用户文件夹；
// 直连/Agent 出生还会经 graph.WriteTx 写 workflow_graphs。媒体先 stage 再 commit，
// after 失败必须 Rollback 已 stage 的文件。Idempotency-Key 去重工作区与保真检查。
//
// 错误：缺行 NotFound；乐观锁/重复 key 哈希不一致 Conflict；未知字段 extra=forbid 为 Validation。
// GraphGuard 找不到商品或 fact 版本返回 nil, nil，不要改成 NotFound（compiler 把缺源当空输入）。
//
// HTTP：POST /api/v2/products、POST /api/v3/products、Agent 工作区与 intake、
// GET/PUT /api/v3/products/:id/facts、图库与封面。删除受 runtime.DeletionEnabled 门闩。
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

// Service 拥有商品出生、facts、封面与商品图库命令，给 HTTP 与 Agent 工具调用。
// 必须注入 DB 与 Media；Canvas/SourceNote 直连测试可空。改图经 graph.WriteTx，本结构不写 workflow_* 表。
// 媒体先 stage 再 commit，after 失败必须 Rollback。不要在 handler 里绕过本入口直接插 products。
type Service struct {
	DB    *gorm.DB    // 命令事务入口
	Media media.Store // 参考图 stage/commit；after 失败必须 Rollback
	// Now 可注入，图库「最近生成」目录用它锚定 30 天窗口。
	Now func() time.Time
	// Canvas 写入 Agent 商品工作区会话；直连创建可以不设。
	Canvas CanvasWriter
	// SourceNote 创建页看图起草商品说明；直连测试可以不设。
	SourceNote SourceNoteGenerator
}

// SourceNoteGenerator 用 prompt 供应商看参考图起草 source_note，不走画布 cook。
type SourceNoteGenerator interface {
	GenerateSourceNote(ctx context.Context, req graph.PromptRequest) (graph.PromptResult, error)
}

// CanvasWriter 在商品出生事务里写入 agent_sessions / agent_conversations。
type CanvasWriter func(ctx context.Context, tx *gorm.DB, productID, title, key, hash string, agentSessionID *string) (sessionID string, conv Conversation, err error)

func (s Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

// GetAsset 按 ProductImageAsset id 读取身份（join media 元数据），供 Agent 工具检视商品图。
// 找不到返回 NotFound。不锁行。图库详情请走带文件夹/生成摘要的 GetGalleryAsset；下载走 AssetForDownload。
func (s Service) GetAsset(ctx context.Context, assetID string) (ImageAsset, error) {
	return loadAsset(ctx, s.DB, assetID)
}

// CreateInput 是商品出生时的名称、可选资料与参考图。Uploads 引用随后写成的 ProductImageAsset，不是存储路径。
type CreateInput struct {
	Name       string
	Category   string   // 空表示未填
	Price      string   // 空表示未填
	SourceNote string   // 空表示未填；不是 CreativeBrief
	Uploads    []Upload // 已校验参考图；至少一张、最多六张
	SetCover   bool     // 出生入参位；当前封面由 createWithGraph 的 setCover 参数决定，本字段未被读取
}

// CreateWithoutGraph 是 v2 无图出生：写商品与参考图，不创建 workflow_graphs。至少一张、最多六张参考图。
// 名称、资料或参考图数量非法返回 Validation。
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

// CreateDirect 是 v3 直连创建：同一事务写商品、参考图与 schema-v3 模板图，不创建 Agent 对话。
// 名称、资料、图种或参考图非法返回 Validation；已有 active 图时 WriteTx 返回 Conflict。
func (s Service) CreateDirect(ctx context.Context, in CreateInput, imageTypes []graph.DirectCreateImageType, generationSpec map[string]any, deliverySpec map[string]any) (DirectCreateResponse, error) {
	ctx = graph.WithProductGuard(ctx, GraphGuard{})
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
		cmd, err := graph.WriteTx(ctx, tx, graph.Command{
			ProductID: creation.product.ID,
			Title:     creation.product.Name,
			ChangeSet: changeSet,
		})
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

// Get 读取商品详情 HTTP 合同（不含图列表与图画布）。找不到 products 行返回 NotFound。
// 只读，不锁。facts 请走 GetFacts；画布请走 graph.Service。
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

// List 分页列出商品。sort 为 updated_desc、created_desc 或 name_asc。
// 库查询失败原样返回。
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

// AssetForDownload 按 ProductImageAsset id 读取身份供下载；缺失文件由 HTTP 层再判 verification_status。
// 找不到图片返回 NotFound。
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

// createWithGraph 在同一事务里写 products、参考图身份，并调用 after（直连/Agent 在此 WriteTx 图）。
// 媒体先 stage 再 commit；after 或写库失败必须 Rollback compensation，否则磁盘会留无主文件。
// 不 commit——调用方 tx.WithGorm 负责。不要在 after 里另开事务。
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
