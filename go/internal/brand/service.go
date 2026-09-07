package brand

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

// Service 拥有商家内 Brand 主档 CRUD。
type Service struct {
	DB *gorm.DB
}

// List 列出当前商家全部品牌。
func (s Service) List(ctx context.Context) ([]View, error) {
	var out []View
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		q := auth.ScopeMerchant(ctx, pgxTx.WithContext(ctx).Model(&schema.Brands{}), "merchant_id")
		var rows []schema.Brands
		if err := q.Order("updated_at DESC, id DESC").Find(&rows).Error; err != nil {
			return err
		}
		out = make([]View, 0, len(rows))
		for _, row := range rows {
			out = append(out, serialize(row))
		}
		return nil
	})
	return out, err
}

// Get 按 id 读取；缺失与跨商统一 404。
func (s Service) Get(ctx context.Context, brandID string) (View, error) {
	var out View
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		row, err := loadBrand(ctx, pgxTx, brandID)
		if err != nil {
			return err
		}
		out = serialize(row)
		return nil
	})
	return out, err
}

// Create 在当前商家下新建品牌；可选挂接本商家视觉方案。
func (s Service) Create(ctx context.Context, in CreateInput) (View, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return View{}, apperr.Validation("品牌名称不能为空")
	}
	var out View
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		merchantID := auth.ResolveMerchantID(ctx)
		if merchantID == "" {
			return apperr.Forbidden("需要有效商家上下文")
		}
		visualID, err := normalizeVisualSystemID(ctx, pgxTx, in.VisualSystemID)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		row := schema.Brands{
			ID: clockid.New(), MerchantID: merchantID, Name: name,
			VisualSystemID: visualID, CreatedAt: now, UpdatedAt: now,
		}
		if err := pgxTx.WithContext(ctx).Create(&row).Error; err != nil {
			return err
		}
		out = serialize(row)
		return nil
	})
	return out, err
}

// Update 部分更新名称或视觉方案挂接。
func (s Service) Update(ctx context.Context, brandID string, in UpdateInput) (View, error) {
	if in.Name == nil && in.VisualSystemID == nil {
		return View{}, apperr.Validation("没有可更新的字段")
	}
	var out View
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		row, err := loadBrand(ctx, pgxTx, brandID)
		if err != nil {
			return err
		}
		updates := map[string]any{"updated_at": time.Now().UTC()}
		if in.Name != nil {
			name := strings.TrimSpace(*in.Name)
			if name == "" {
				return apperr.Validation("品牌名称不能为空")
			}
			updates["name"] = name
		}
		if in.VisualSystemID != nil {
			visualID, err := normalizeVisualSystemID(ctx, pgxTx, *in.VisualSystemID)
			if err != nil {
				return err
			}
			updates["visual_system_id"] = visualID
		}
		if err := pgxTx.WithContext(ctx).Model(&schema.Brands{}).Where("id = ?", row.ID).Updates(updates).Error; err != nil {
			return err
		}
		row, err = loadBrand(ctx, pgxTx, brandID)
		if err != nil {
			return err
		}
		out = serialize(row)
		return nil
	})
	return out, err
}

func loadBrand(ctx context.Context, db *gorm.DB, brandID string) (schema.Brands, error) {
	var row schema.Brands
	q := auth.ScopeMerchant(ctx, db.WithContext(ctx).Where("id = ?", brandID), "merchant_id")
	err := q.Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return schema.Brands{}, auth.NotFoundCrossMerchant()
	}
	return row, err
}

// normalizeVisualSystemID 清空或校验同商家视觉方案；跨商/缺失 → 404。
func normalizeVisualSystemID(ctx context.Context, db *gorm.DB, raw *string) (*string, error) {
	if raw == nil {
		return nil, nil
	}
	id := strings.TrimSpace(*raw)
	if id == "" {
		return nil, nil
	}
	var sys schema.VisualSystems
	q := auth.ScopeMerchant(ctx, db.WithContext(ctx).Where("id = ?", id), "merchant_id")
	err := q.Select("id").Take(&sys).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, auth.NotFoundCrossMerchant()
	}
	if err != nil {
		return nil, err
	}
	return &id, nil
}

func serialize(row schema.Brands) View {
	return View{
		ID: row.ID, MerchantID: row.MerchantID, Name: row.Name,
		VisualSystemID: row.VisualSystemID, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}
