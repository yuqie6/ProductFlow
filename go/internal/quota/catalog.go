package quota

import (
	"context"
	"errors"
	"strings"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"gorm.io/gorm"
)

// 已接线收费入口的目录动作码（单价真相在 quota_price_entries，≠本常量）。
const (
	EntryImageSessionGenerate = "image_session.generate"
	EntryGraphImageGeneration = "graph.image_generation"
	EntryAgentModelRequest    = "agent.model_request"
	EntryLocalEdit            = "localedit.edit"
	EntryProductSourceNote    = "product.source_note"
)

// PriceEntry 是价格版本下一条入口单价。
type PriceEntry struct {
	EntryCode string
	UnitPrice int64
}

// PriceVersion 是可核验的价格版本摘要（含条目）。
type PriceVersion struct {
	ID        string
	Label     string
	Currency  string
	IsDefault bool
	Entries   []PriceEntry
}

// GetPriceVersion 读取指定版本及条目；不存在返回 NotFound。
func (s *Service) GetPriceVersion(ctx context.Context, versionID string) (PriceVersion, error) {
	versionID = strings.TrimSpace(versionID)
	if versionID == "" {
		return PriceVersion{}, apperr.Validation("缺少价格版本")
	}
	var row schema.QuotaPriceVersions
	err := s.DB.WithContext(ctx).Where("id = ?", versionID).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return PriceVersion{}, apperr.NotFound("价格版本不存在")
	}
	if err != nil {
		return PriceVersion{}, apperr.Internal("读取价格版本失败")
	}
	entries, err := listPriceEntries(s.DB.WithContext(ctx), versionID)
	if err != nil {
		return PriceVersion{}, err
	}
	return priceVersionFromRow(row, entries), nil
}

// GetDefaultPriceVersion 读取 is_default 版本；无默认行时回退 DefaultPriceVersionID。
func (s *Service) GetDefaultPriceVersion(ctx context.Context) (PriceVersion, error) {
	var row schema.QuotaPriceVersions
	err := s.DB.WithContext(ctx).Where("is_default = ?", true).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return s.GetPriceVersion(ctx, DefaultPriceVersionID)
	}
	if err != nil {
		return PriceVersion{}, apperr.Internal("读取默认价格版本失败")
	}
	entries, err := listPriceEntries(s.DB.WithContext(ctx), row.ID)
	if err != nil {
		return PriceVersion{}, err
	}
	return priceVersionFromRow(row, entries), nil
}

func requirePriceVersion(gdb *gorm.DB, versionID string) error {
	var n int64
	if err := gdb.Model(&schema.QuotaPriceVersions{}).Where("id = ?", versionID).Count(&n).Error; err != nil {
		return apperr.Internal("校验价格版本失败")
	}
	if n == 0 {
		return apperr.Validation("未知价格版本")
	}
	return nil
}

func listPriceEntries(gdb *gorm.DB, versionID string) ([]PriceEntry, error) {
	var rows []schema.QuotaPriceEntries
	if err := gdb.Where("price_version_id = ?", versionID).
		Order("entry_code ASC").
		Find(&rows).Error; err != nil {
		return nil, apperr.Internal("读取价格条目失败")
	}
	out := make([]PriceEntry, 0, len(rows))
	for _, row := range rows {
		out = append(out, PriceEntry{EntryCode: row.EntryCode, UnitPrice: row.UnitPrice})
	}
	return out, nil
}

func priceVersionFromRow(row schema.QuotaPriceVersions, entries []PriceEntry) PriceVersion {
	return PriceVersion{
		ID:        row.ID,
		Label:     row.Label,
		Currency:  row.Currency,
		IsDefault: row.IsDefault,
		Entries:   entries,
	}
}
