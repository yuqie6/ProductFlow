package visualsystem

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/canonjson"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const payloadSchemaVersion = 1

// Service 拥有商家内视觉方案版本与商品显式选择。
type Service struct {
	DB *gorm.DB
}

// CreateSystemInput 创建视觉方案并写入首版不可变 payload。
type CreateSystemInput struct {
	Name    string
	Payload map[string]any
}

// AppendVersionInput 追加新版本；不改动任何商品选择与旧交付。
type AppendVersionInput struct {
	SystemID string
	Payload  map[string]any
}

// SelectInput 显式选定（或采用）某一版本。
type SelectInput struct {
	ProductID             string
	VisualSystemVersionID string
}

// List 列出当前商家未归档视觉方案（含最新版本摘要）。
func (s Service) List(ctx context.Context, includeArchived bool) ([]SystemView, error) {
	var out []SystemView
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		q := auth.ScopeMerchant(ctx, pgxTx.WithContext(ctx).Model(&schema.VisualSystems{}), "merchant_id")
		if !includeArchived {
			q = q.Where("archived_at IS NULL")
		}
		var rows []schema.VisualSystems
		if err := q.Order("updated_at DESC, id DESC").Find(&rows).Error; err != nil {
			return err
		}
		out = make([]SystemView, 0, len(rows))
		for _, row := range rows {
			view, err := serializeSystem(ctx, pgxTx, row, false)
			if err != nil {
				return err
			}
			out = append(out, view)
		}
		return nil
	})
	return out, err
}

// Get 读取方案及全部版本。
func (s Service) Get(ctx context.Context, systemID string) (SystemView, error) {
	var out SystemView
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		row, err := loadSystem(ctx, pgxTx, systemID)
		if err != nil {
			return err
		}
		out, err = serializeSystem(ctx, pgxTx, row, true)
		return err
	})
	return out, err
}

// Create 在当前商家下新建方案与 v1。
func (s Service) Create(ctx context.Context, in CreateSystemInput) (SystemView, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return SystemView{}, apperr.Validation("视觉方案名称不能为空")
	}
	payload, hash, err := normalizePayload(in.Payload)
	if err != nil {
		return SystemView{}, err
	}
	var out SystemView
	err = tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		now := time.Now().UTC()
		systemID := clockid.New()
		versionID := clockid.New()
		merchantID := auth.ResolveMerchantID(ctx)
		sys := schema.VisualSystems{
			ID: systemID, MerchantID: merchantID, Name: name,
			CreatedAt: now, UpdatedAt: now,
		}
		if err := pgxTx.WithContext(ctx).Create(&sys).Error; err != nil {
			return err
		}
		ver := schema.VisualSystemVersions{
			ID: versionID, VisualSystemID: systemID, Version: 1,
			SchemaVersion: payloadSchemaVersion, PayloadJSON: payload, PayloadHash: hash,
			CreatedAt: now,
		}
		if err := pgxTx.WithContext(ctx).Create(&ver).Error; err != nil {
			return err
		}
		out, err = serializeSystem(ctx, pgxTx, sys, true)
		return err
	})
	return out, err
}

// AppendVersion 追加不可变版本；不更新 product_visual_selections 与交付快照。
func (s Service) AppendVersion(ctx context.Context, in AppendVersionInput) (VersionView, error) {
	payload, hash, err := normalizePayload(in.Payload)
	if err != nil {
		return VersionView{}, err
	}
	var out VersionView
	err = tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		sys, err := loadSystem(ctx, pgxTx, in.SystemID)
		if err != nil {
			return err
		}
		if sys.ArchivedAt != nil {
			return apperr.Conflict("已归档视觉方案不能追加版本")
		}
		var maxVersion int
		if err := pgxTx.WithContext(ctx).Model(&schema.VisualSystemVersions{}).
			Select("COALESCE(MAX(version), 0)").
			Where("visual_system_id = ?", sys.ID).
			Scan(&maxVersion).Error; err != nil {
			return err
		}
		now := time.Now().UTC()
		ver := schema.VisualSystemVersions{
			ID: clockid.New(), VisualSystemID: sys.ID, Version: maxVersion + 1,
			SchemaVersion: payloadSchemaVersion, PayloadJSON: payload, PayloadHash: hash,
			CreatedAt: now,
		}
		if err := pgxTx.WithContext(ctx).Create(&ver).Error; err != nil {
			return err
		}
		if err := pgxTx.WithContext(ctx).Model(&schema.VisualSystems{}).
			Where("id = ?", sys.ID).
			Updates(map[string]any{"updated_at": now}).Error; err != nil {
			return err
		}
		out, err = serializeVersion(ver)
		return err
	})
	return out, err
}

// GetVersion 预览某一版本 payload。
func (s Service) GetVersion(ctx context.Context, systemID, versionID string) (VersionView, error) {
	var out VersionView
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		if _, err := loadSystem(ctx, pgxTx, systemID); err != nil {
			return err
		}
		ver, err := loadVersion(ctx, pgxTx, versionID)
		if err != nil {
			return err
		}
		if ver.VisualSystemID != systemID {
			return auth.NotFoundCrossMerchant()
		}
		out, err = serializeVersion(ver)
		return err
	})
	return out, err
}

// Impact 列出仍钉在该方案旧版本上的商品；追加新版后调用以提示显式采用。
func (s Service) Impact(ctx context.Context, systemID, versionID string) (ImpactView, error) {
	var out ImpactView
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		if _, err := loadSystem(ctx, pgxTx, systemID); err != nil {
			return err
		}
		ver, err := loadVersion(ctx, pgxTx, versionID)
		if err != nil {
			return err
		}
		if ver.VisualSystemID != systemID {
			return auth.NotFoundCrossMerchant()
		}
		var olderIDs []string
		if err := pgxTx.WithContext(ctx).Model(&schema.VisualSystemVersions{}).
			Select("id").
			Where("visual_system_id = ? AND version < ?", systemID, ver.Version).
			Pluck("id", &olderIDs).Error; err != nil {
			return err
		}
		pinned := []ImpactProduct{}
		if len(olderIDs) > 0 {
			type row struct {
				ProductID             string
				ProductName           string
				VisualSystemVersionID string
				SelectedAt            time.Time
			}
			var rows []row
			if err := pgxTx.WithContext(ctx).Table("product_visual_selections AS s").
				Select("s.product_id, p.name AS product_name, s.visual_system_version_id, s.selected_at").
				Joins("JOIN products AS p ON p.id = s.product_id").
				Where("s.visual_system_version_id IN ?", olderIDs).
				Order("s.selected_at ASC, s.product_id ASC").
				Scan(&rows).Error; err != nil {
				return err
			}
			for _, item := range rows {
				pinned = append(pinned, ImpactProduct{
					ProductID: item.ProductID, ProductName: item.ProductName,
					VisualSystemVersionID: item.VisualSystemVersionID, SelectedAt: item.SelectedAt,
				})
			}
		}
		out = ImpactView{
			SystemID: systemID, LatestVersionID: ver.ID, LatestVersion: ver.Version,
			PinnedToOlder: pinned, BrandPlaceholder: DefaultBrandPlaceholder(),
		}
		return nil
	})
	return out, err
}

// GetSelection 读取商品当前显式选择。
func (s Service) GetSelection(ctx context.Context, productID string) (SelectionView, error) {
	var out SelectionView
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		if err := lockProduct(ctx, pgxTx, productID); err != nil {
			return err
		}
		var sel schema.ProductVisualSelections
		err := pgxTx.WithContext(ctx).Where("product_id = ?", productID).Take(&sel).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return apperr.NotFound("尚未选定视觉方案版本")
		}
		if err != nil {
			return err
		}
		ver, err := loadVersion(ctx, pgxTx, sel.VisualSystemVersionID)
		if err != nil {
			return err
		}
		sys, err := loadSystem(ctx, pgxTx, ver.VisualSystemID)
		if err != nil {
			return err
		}
		versionView, err := serializeVersion(ver)
		if err != nil {
			return err
		}
		systemView, err := serializeSystem(ctx, pgxTx, sys, false)
		if err != nil {
			return err
		}
		out = SelectionView{
			ProductID: productID, VisualSystemVersionID: sel.VisualSystemVersionID,
			SelectedAt: sel.SelectedAt, Version: versionView, System: systemView,
		}
		return nil
	})
	return out, err
}

// Select 显式选定/采用版本。不改旧交付采用快照；不自动改其他商品。
func (s Service) Select(ctx context.Context, in SelectInput) (SelectionView, error) {
	versionID := strings.TrimSpace(in.VisualSystemVersionID)
	if versionID == "" {
		return SelectionView{}, apperr.Validation("visual_system_version_id 不能为空")
	}
	var out SelectionView
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		if err := lockProduct(ctx, pgxTx, in.ProductID); err != nil {
			return err
		}
		ver, err := loadVersion(ctx, pgxTx, versionID)
		if err != nil {
			return err
		}
		sys, err := loadSystem(ctx, pgxTx, ver.VisualSystemID)
		if err != nil {
			return err
		}
		if sys.ArchivedAt != nil {
			return apperr.Conflict("不能选定已归档视觉方案的版本")
		}
		now := time.Now().UTC()
		row := schema.ProductVisualSelections{
			ProductID: in.ProductID, VisualSystemVersionID: versionID, SelectedAt: now,
		}
		if err := pgxTx.WithContext(ctx).Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "product_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"visual_system_version_id", "selected_at"}),
		}).Create(&row).Error; err != nil {
			return err
		}
		versionView, err := serializeVersion(ver)
		if err != nil {
			return err
		}
		systemView, err := serializeSystem(ctx, pgxTx, sys, false)
		if err != nil {
			return err
		}
		out = SelectionView{
			ProductID: in.ProductID, VisualSystemVersionID: versionID,
			SelectedAt: now, Version: versionView, System: systemView,
		}
		return nil
	})
	return out, err
}

// Inheritance 解析商品视觉继承，并提示同方案是否有更新版本可显式采用。
func (s Service) Inheritance(ctx context.Context, productID string, productOverride map[string]any) (InheritanceView, error) {
	var out InheritanceView
	err := tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		if err := lockProduct(ctx, pgxTx, productID); err != nil {
			return err
		}
		input := ResolveInput{ProductID: productID, ProductOverride: productOverride, ProductDefault: map[string]any{}}
		var sel schema.ProductVisualSelections
		err := pgxTx.WithContext(ctx).Where("product_id = ?", productID).Take(&sel).Error
		if err == nil {
			ver, err := loadVersion(ctx, pgxTx, sel.VisualSystemVersionID)
			if err != nil {
				return err
			}
			sys, err := loadSystem(ctx, pgxTx, ver.VisualSystemID)
			if err != nil {
				return err
			}
			payload, err := decodePayloadJSON(ver.PayloadJSON)
			if err != nil {
				return apperr.Conflict("视觉方案版本载荷无效")
			}
			id := ver.ID
			sysID := sys.ID
			sysName := sys.Name
			input.SelectedVersionID = &id
			input.SelectedSystemID = &sysID
			input.SelectedSystemName = &sysName
			input.SelectedPayload = payload

			var latest schema.VisualSystemVersions
			if err := pgxTx.WithContext(ctx).
				Where("visual_system_id = ?", sys.ID).
				Order("version DESC").
				Take(&latest).Error; err != nil {
				return err
			}
			if latest.ID != ver.ID {
				newer, err := serializeVersion(latest)
				if err != nil {
					return err
				}
				input.NewerVersion = &newer
			}
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		out = ResolveInheritance(input)
		return nil
	})
	return out, err
}

func normalizePayload(payload map[string]any) (string, string, error) {
	if payload == nil {
		payload = map[string]any{}
	}
	encoded, err := canonjson.Compact(payload)
	if err != nil {
		return "", "", apperr.Validation("视觉方案载荷无效")
	}
	if len(encoded) > 256*1024 {
		return "", "", apperr.Validation("视觉方案载荷过大")
	}
	hash, err := canonjson.SHA256Hex(payload)
	if err != nil {
		return "", "", apperr.Validation("视觉方案载荷无效")
	}
	return string(encoded), hash, nil
}

func loadSystem(ctx context.Context, tx *gorm.DB, systemID string) (schema.VisualSystems, error) {
	var row schema.VisualSystems
	q := auth.ScopeMerchant(ctx, tx.WithContext(ctx).Where("id = ?", systemID), "merchant_id")
	err := q.Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return schema.VisualSystems{}, auth.NotFoundCrossMerchant()
	}
	return row, err
}

func lockProduct(ctx context.Context, tx *gorm.DB, productID string) error {
	var row schema.Products
	q := auth.ScopeMerchant(ctx, tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}), "merchant_id")
	err := q.Select("id").Where("id = ?", productID).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return auth.NotFoundCrossMerchant()
	}
	return err
}

func loadVersion(ctx context.Context, tx *gorm.DB, versionID string) (schema.VisualSystemVersions, error) {
	var row schema.VisualSystemVersions
	err := tx.WithContext(ctx).Where("id = ?", versionID).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return schema.VisualSystemVersions{}, apperr.NotFound("视觉方案版本不存在")
	}
	return row, err
}

func serializeVersion(ver schema.VisualSystemVersions) (VersionView, error) {
	payload, err := decodePayloadJSON(ver.PayloadJSON)
	if err != nil {
		return VersionView{}, apperr.Conflict("视觉方案版本载荷无效")
	}
	return VersionView{
		ID: ver.ID, SystemID: ver.VisualSystemID, Version: ver.Version,
		SchemaVersion: ver.SchemaVersion, Payload: payload, PayloadHash: ver.PayloadHash,
		CreatedAt: ver.CreatedAt,
	}, nil
}

func serializeSystem(ctx context.Context, tx *gorm.DB, row schema.VisualSystems, withHistory bool) (SystemView, error) {
	view := SystemView{
		ID: row.ID, Name: row.Name, ArchivedAt: row.ArchivedAt,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
	var versions []schema.VisualSystemVersions
	q := tx.WithContext(ctx).Where("visual_system_id = ?", row.ID).Order("version DESC")
	if !withHistory {
		q = q.Limit(1)
	}
	if err := q.Find(&versions).Error; err != nil {
		return SystemView{}, err
	}
	if len(versions) > 0 {
		current, err := serializeVersion(versions[0])
		if err != nil {
			return SystemView{}, err
		}
		view.CurrentVersion = &current
		if withHistory {
			history := make([]VersionView, 0, len(versions))
			for _, ver := range versions {
				item, err := serializeVersion(ver)
				if err != nil {
					return SystemView{}, err
				}
				history = append(history, item)
			}
			view.Versions = history
		}
	}
	return view, nil
}