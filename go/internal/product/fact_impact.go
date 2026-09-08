package product

import (
	"context"
	"time"

	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

// FactsImpactPreviewInput 是 POST .../facts/impact-preview 的拟议资料。
type FactsImpactPreviewInput struct {
	Facts  *[]map[string]any
	Fields map[string]bool
}

// FactsImpactPreviewResponse 包装 graph.FactImpactPreview。
type FactsImpactPreviewResponse struct {
	graph.FactImpactPreview
	ProposedFactCount int `json:"proposed_fact_count"`
}

// FactsUpdateResponse 是 PUT facts 在携带 update_node_ids 时的扩展响应。
type FactsUpdateResponse struct {
	FactsResponse
	Impact           *graph.FactImpactPreview `json:"impact,omitempty"`
	PreservedNodeIDs []string                 `json:"preserved_node_ids,omitempty"`
	AdoptedFactSetID *string                  `json:"adopted_fact_set_version_id,omitempty"`
	UpdateNodeIDs    []string                 `json:"update_node_ids,omitempty"`
	DidAdoptFactSet  bool                     `json:"did_adopt_fact_set"`
}

// PreviewFactsImpact 在不写库的情况下，按拟议 facts 列出 RoleFacts 依赖图位。
func (s Service) PreviewFactsImpact(ctx context.Context, productID string, in FactsImpactPreviewInput) (FactsImpactPreviewResponse, error) {
	var out FactsImpactPreviewResponse
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		product, err := loadProduct(ctx, pgxTx, productID)
		if err != nil {
			return err
		}
		current, err := loadCurrentFactSet(ctx, pgxTx, product)
		if err != nil {
			return err
		}
		currentMaps := currentFactMaps(current)
		proposed := currentMaps
		if in.Facts != nil {
			proposed, err = normalizeFactMaps(*in.Facts)
			if err != nil {
				return err
			}
		}
		changed := graph.DiffFactKeys(currentMaps, proposed)
		identity, applied, sources, err := graph.LoadLiveGraphSources(ctx, GraphGuard{}, pgxTx, productID)
		if err != nil {
			return err
		}
		if identity == nil {
			preview := graph.FactImpactPreview{
				ProductID:            productID,
				ChangedFactKeys:      changed,
				Nodes:                []graph.FactImpactNode{},
				DefaultUpdateNodeIDs: []string{},
				Explanation:          "该商品尚无工作流图；保存事实不会触发图位更新。",
			}
			if current != nil {
				id := current.ID
				preview.CurrentFactSetVersionID = &id
			}
			out = FactsImpactPreviewResponse{FactImpactPreview: preview, ProposedFactCount: len(proposed)}
			return nil
		}
		sources = graph.MergeRoleFactsIntoSources(applied, sources)
		preview := graph.PreviewFactImpact(applied, sources, productID, changed)
		if current != nil {
			id := current.ID
			preview.CurrentFactSetVersionID = &id
		}
		out = FactsImpactPreviewResponse{FactImpactPreview: preview, ProposedFactCount: len(proposed)}
		return nil
	})
	return out, err
}

// UpdateFactsAndAdopt 保存事实；若 UpdateNodeIDsProvided，则钉住新版本、校验更新范围并保留未选中产物。
// 不入队全图运行。
func (s Service) UpdateFactsAndAdopt(ctx context.Context, productID string, in UpdateFactsInput) (FactsUpdateResponse, error) {
	var out FactsUpdateResponse
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		product, err := loadProductForUpdate(ctx, pgxTx, productID)
		if err != nil {
			return err
		}
		current, err := loadCurrentFactSet(ctx, pgxTx, product)
		if err != nil {
			return err
		}
		if err := checkExpectedFactVersion(in, product, current); err != nil {
			return err
		}
		currentMaps := currentFactMaps(current)
		if in.Fields["name"] {
			name, err := normalizeName(deref(in.Name))
			if err != nil {
				return err
			}
			product.Name = name
		}
		if in.Fields["category"] {
			category, err := optionalText(deref(in.Category), "类目", 120)
			if err != nil {
				return err
			}
			product.Category = category
		}
		if in.Fields["price"] {
			price, err := normalizePrice(deref(in.Price))
			if err != nil {
				return err
			}
			product.Price = price
		}
		if in.Fields["source_note"] {
			note, err := optionalText(deref(in.SourceNote), "备注", 4000)
			if err != nil {
				return err
			}
			product.SourceNote = note
		}
		if err := pgxTx.WithContext(ctx).Model(&schema.Products{}).Where("id = ?", product.ID).Updates(map[string]any{
			"name":        product.Name,
			"category":    product.Category,
			"price":       product.Price,
			"source_note": product.SourceNote,
			"updated_at":  time.Now().UTC(),
		}).Error; err != nil {
			return err
		}
		factPayload := currentMaps
		if in.Facts != nil {
			factPayload, err = normalizeFactMaps(*in.Facts)
			if err != nil {
				return err
			}
		}
		changed := graph.DiffFactKeys(currentMaps, factPayload)
		factSetID, _, err := insertFactSet(ctx, pgxTx, product.ID, factPayload)
		if err != nil {
			return err
		}
		product, err = loadProduct(ctx, pgxTx, product.ID)
		if err != nil {
			return err
		}
		factsOut, err := factsResponse(ctx, pgxTx, product)
		if err != nil {
			return err
		}
		out.FactsResponse = factsOut

		if !in.UpdateNodeIDsProvided {
			return nil
		}
		identity, applied, sources, err := graph.LoadLiveGraphSources(ctx, GraphGuard{}, pgxTx, productID)
		if err != nil {
			return err
		}
		updateIDs := in.UpdateNodeIDs
		if updateIDs == nil {
			updateIDs = []string{}
		}
		if identity == nil {
			if len(updateIDs) > 0 {
				return apperr.Validation("该商品尚无工作流图，不能指定更新图位")
			}
		} else {
			sources = graph.MergeRoleFactsIntoSources(applied, sources)
			preview := graph.PreviewFactImpact(applied, sources, productID, changed)
			id := factSetID
			preview.CurrentFactSetVersionID = &id
			if err := graph.ValidateUpdateNodeIDs(preview, updateIDs); err != nil {
				return err
			}
			out.Impact = &preview
		}
		adopted, err := graph.AdoptFactSetOnProductSources(ctx, GraphGuard{}, pgxTx, productID, factSetID)
		if err != nil {
			return err
		}
		out.DidAdoptFactSet = adopted
		out.AdoptedFactSetID = &factSetID
		out.UpdateNodeIDs = append([]string(nil), updateIDs...)
		preserved, err := graph.PreserveUnselectedFactArtifacts(ctx, GraphGuard{}, pgxTx, productID, updateIDs)
		if err != nil {
			return err
		}
		out.PreservedNodeIDs = preserved
		return nil
	})
	return out, err
}
