package product

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"github.com/yuqie6/productflow/internal/recipe"
	"gorm.io/gorm"
)

type RecipeCreateInput struct {
	Product               CreateInput
	RecipeID              string
	ExpectedRecipeVersion int
	PreviewDigest         string
	IdempotencyKey        string
}

type RecipeCreateResponse struct {
	Created bool             `json:"created"`
	Product Detail           `json:"product"`
	Graph   graph.Projection `json:"graph"`
}

// CreateFromRecipe commits the product, uploads and first graph together, without an Agent session.
// A product-level key makes a lost HTTP response retryable without creating another product.
func (s Service) CreateFromRecipe(ctx context.Context, in RecipeCreateInput) (RecipeCreateResponse, error) {
	key, err := normalizeIdempotencyKey(in.IdempotencyKey)
	if err != nil {
		return RecipeCreateResponse{}, err
	}
	if len(key) > 120 {
		return RecipeCreateResponse{}, apperr.Validation("Idempotency-Key 不能超过 120 bytes")
	}
	if in.ExpectedRecipeVersion < 1 || strings.TrimSpace(in.RecipeID) == "" || len(in.PreviewDigest) != 64 {
		return RecipeCreateResponse{}, apperr.Validation("配方创建请求无效")
	}
	name, err := normalizeName(in.Product.Name)
	if err != nil {
		return RecipeCreateResponse{}, err
	}
	in.Product.Name = name
	images := make([]map[string]any, 0, len(in.Product.Uploads))
	for _, upload := range in.Product.Uploads {
		sum := sha256.Sum256(upload.Content)
		images = append(images, map[string]any{"filename": upload.Filename, "mime_type": upload.MIMEType, "sha256": hex.EncodeToString(sum[:])})
	}
	raw, err := canonicalJSON(map[string]any{
		"name": name, "category": in.Product.Category, "price": in.Product.Price,
		"source_note": in.Product.SourceNote, "images": images,
		"recipe_id": in.RecipeID, "recipe_version": in.ExpectedRecipeVersion, "preview_digest": in.PreviewDigest,
	})
	if err != nil {
		return RecipeCreateResponse{}, err
	}
	sum := sha256.Sum256(raw)
	hash := hex.EncodeToString(sum[:])
	if replay, found, err := s.recipeCreationReplay(ctx, key, hash); found || err != nil {
		return replay, err
	}

	var out RecipeCreateResponse
	err = s.createWithGraph(ctx, in.Product, true, true, func(db *gorm.DB, creation canonicalCreation) error {
		if err := db.Model(&schema.Products{}).Where("id = ?", creation.product.ID).Updates(map[string]any{
			"creation_idempotency_key": key, "creation_request_hash": hash,
		}).Error; err != nil {
			return err
		}
		recipes := recipe.Service{DB: db, Products: GraphGuard{}}
		preview, err := recipes.PreviewCreation(ctx, in.RecipeID, in.ExpectedRecipeVersion)
		if err != nil {
			return err
		}
		if preview.PreviewDigest != in.PreviewDigest {
			return apperr.Conflict("配方预览已变化，请重新预览后重试")
		}
		targetPreview, err := recipes.Preview(ctx, creation.product.ID, in.RecipeID, in.ExpectedRecipeVersion)
		if err != nil {
			return err
		}
		applied, err := recipes.Apply(ctx, recipe.ApplyInput{
			ProductID: creation.product.ID, RecipeID: in.RecipeID,
			ExpectedRecipeVersion: in.ExpectedRecipeVersion, ExpectedGraphRevision: 0,
			PreviewDigest: targetPreview.PreviewDigest, IdempotencyKey: key,
		})
		if err != nil {
			return err
		}
		out = RecipeCreateResponse{Created: true, Product: serializeDetail(creation.product), Graph: applied.Graph}
		return nil
	})
	// Concurrent confirmations serialize on the unique product key. Reload only after rollback.
	if uniqueViolation(err) {
		if replay, found, replayErr := s.recipeCreationReplay(ctx, key, hash); found || replayErr != nil {
			return replay, replayErr
		}
	}
	return out, err
}

func (s Service) recipeCreationReplay(ctx context.Context, key, hash string) (RecipeCreateResponse, bool, error) {
	var out RecipeCreateResponse
	found := false
	err := tx.WithGorm(ctx, s.DB, func(db *gorm.DB) error {
		var row schema.Products
		err := auth.ScopeMerchant(ctx, db.Where("creation_idempotency_key = ?", key), "merchant_id").Take(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		found = true
		if row.CreationRequestHash == nil || *row.CreationRequestHash != hash {
			return apperr.Conflict("相同 Idempotency-Key 不能创建不同的商品")
		}
		p, err := loadProduct(ctx, db, row.ID)
		if err != nil {
			return err
		}
		projection, err := (graph.Service{DB: db, Products: GraphGuard{}}).Current(ctx, row.ID)
		if err != nil {
			return err
		}
		out = RecipeCreateResponse{Created: false, Product: serializeDetail(p), Graph: projection}
		return nil
	})
	return out, found, err
}
