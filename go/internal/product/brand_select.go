package product

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

// GetBrandSelection 读取商品当前选定的 Brand；未选定时 brand_id 为 null。
func (s Service) GetBrandSelection(ctx context.Context, productID string) (BrandSelectionView, error) {
	var out BrandSelectionView
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		p, err := loadProduct(ctx, pgxTx, productID)
		if err != nil {
			return err
		}
		out = BrandSelectionView{ProductID: p.ID, BrandID: p.BrandID}
		return nil
	})
	return out, err
}

// SetBrandSelection 将商品绑定到本商家 Brand；brandID 为空或 nil 表示清除。跨商 Brand → 404。
func (s Service) SetBrandSelection(ctx context.Context, productID string, brandID *string) (BrandSelectionView, error) {
	var out BrandSelectionView
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		p, err := loadProductForUpdate(ctx, pgxTx, productID)
		if err != nil {
			return err
		}
		normalized, err := normalizeProductBrandID(ctx, pgxTx, brandID)
		if err != nil {
			return err
		}
		if err := pgxTx.WithContext(ctx).Model(&schema.Products{}).Where("id = ?", p.ID).Updates(map[string]any{
			"brand_id":   normalized,
			"updated_at": time.Now().UTC(),
		}).Error; err != nil {
			return err
		}
		out = BrandSelectionView{ProductID: p.ID, BrandID: normalized}
		return nil
	})
	return out, err
}

func normalizeProductBrandID(ctx context.Context, db *gorm.DB, raw *string) (*string, error) {
	if raw == nil {
		return nil, nil
	}
	id := strings.TrimSpace(*raw)
	if id == "" {
		return nil, nil
	}
	var brand schema.Brands
	q := auth.ScopeMerchant(ctx, db.WithContext(ctx).Where("id = ?", id), "merchant_id")
	err := q.Select("id").Take(&brand).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, auth.NotFoundCrossMerchant()
	}
	if err != nil {
		return nil, err
	}
	return &id, nil
}
