package product

import (
	"context"
	"time"

	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

const (
	defaultOverviewDays     = 30
	defaultOverviewPage     = 1
	defaultOverviewPageSize = 10
	maxOverviewPage         = 100000
	maxOverviewPageSize     = 100
)

type overviewQuery struct {
	Days     int
	Kind     string
	State    string
	Page     int
	PageSize int
}

func normalizeOverviewQuery(query overviewQuery) (overviewQuery, error) {
	if query.Days == 0 {
		query.Days = defaultOverviewDays
	}
	if query.Days != 7 && query.Days != 30 {
		return overviewQuery{}, apperr.Validation("请求参数无效")
	}
	if query.Kind == "" {
		query.Kind = "all"
	}
	switch query.Kind {
	case "all", "agent_task", "workflow_run", "image_session", "local_edit":
	default:
		return overviewQuery{}, apperr.Validation("请求参数无效")
	}
	if query.State == "" {
		query.State = "all"
	}
	switch query.State {
	case "all", "active", "waiting", "unknown", "failed":
	default:
		return overviewQuery{}, apperr.Validation("请求参数无效")
	}
	if query.Page == 0 {
		query.Page = defaultOverviewPage
	}
	if query.Page < 1 || query.Page > maxOverviewPage {
		return overviewQuery{}, apperr.Validation("请求参数无效")
	}
	if query.PageSize == 0 {
		query.PageSize = defaultOverviewPageSize
	}
	if query.PageSize < 1 || query.PageSize > maxOverviewPageSize {
		return overviewQuery{}, apperr.Validation("请求参数无效")
	}
	return query, nil
}

// Overview reads the merchant product counts, current adoption pointers, and
// four source work records. Statistics intentionally ignore record filters.
func (s Service) Overview(ctx context.Context, requested overviewQuery) (ProductOverviewResponse, error) {
	query, err := normalizeOverviewQuery(requested)
	if err != nil {
		return ProductOverviewResponse{}, err
	}
	merchantID, err := auth.RequireMerchantID(ctx)
	if err != nil {
		return ProductOverviewResponse{}, err
	}
	asOf := s.now()
	from := asOf.Add(-time.Duration(query.Days) * 24 * time.Hour)

	var out ProductOverviewResponse
	err = tx.WithGorm(ctx, s.DB, func(pgxTx *gorm.DB) error {
		products, err := loadOverviewProductCounts(ctx, pgxTx, merchantID)
		if err != nil {
			return err
		}
		bySource, err := loadOverviewSourceCounts(ctx, pgxTx, merchantID, from, asOf)
		if err != nil {
			return err
		}
		records, err := loadOverviewRecords(ctx, pgxTx, merchantID, query, from, asOf)
		if err != nil {
			return err
		}
		out = ProductOverviewResponse{
			AsOf:         asOf,
			Days:         query.Days,
			Kind:         query.Kind,
			State:        query.State,
			RecentWindow: ProductOverviewTimeWindow{Days: query.Days, From: from, To: asOf},
			Products:     products,
			Work:         ProductOverviewWork{BySource: bySource, Records: records},
		}
		return nil
	})
	return out, err
}

func loadOverviewProductCounts(ctx context.Context, tx *gorm.DB, merchantID string) (ProductOverviewProducts, error) {
	query := tx.WithContext(ctx).Model(&schema.Products{}).Where("merchant_id = ?", merchantID)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return ProductOverviewProducts{}, err
	}
	var currentAdopted int64
	if err := tx.WithContext(ctx).
		Table("products AS p").
		Joins("JOIN delivery_adoption_versions AS v ON v.id = p.current_delivery_adoption_version_id AND v.product_id = p.id").
		Where("p.merchant_id = ?", merchantID).
		Distinct("p.id").
		Count(&currentAdopted).Error; err != nil {
		return ProductOverviewProducts{}, err
	}
	return ProductOverviewProducts{Total: total, CurrentAdopted: currentAdopted}, nil
}

type overviewSourceCountRow struct {
	Kind         string `gorm:"column:kind"`
	Active       int64  `gorm:"column:active"`
	Waiting      int64  `gorm:"column:waiting"`
	Unknown      int64  `gorm:"column:unknown"`
	RecentFailed int64  `gorm:"column:recent_failed"`
}

func loadOverviewSourceCounts(ctx context.Context, tx *gorm.DB, merchantID string, from, to time.Time) (ProductOverviewBySource, error) {
	var rows []overviewSourceCountRow
	query := merchantWorkRecordQuery(tx.WithContext(ctx), merchantID).
		Select(`kind,
			COUNT(*) FILTER (WHERE status IN ('queued', 'running')) AS active,
			COUNT(*) FILTER (WHERE status IN ('waiting_user', 'awaiting_confirmation')) AS waiting,
			COUNT(*) FILTER (WHERE status = 'unknown') AS unknown,
			COUNT(*) FILTER (WHERE status = 'failed' AND activity_at >= ? AND activity_at <= ?) AS recent_failed`, from, to).
		Group("kind")
	if err := query.Scan(&rows).Error; err != nil {
		return ProductOverviewBySource{}, err
	}
	out := ProductOverviewBySource{}
	for _, row := range rows {
		counts := ProductOverviewSourceCounts{
			Active:       row.Active,
			Waiting:      row.Waiting,
			Unknown:      row.Unknown,
			RecentFailed: row.RecentFailed,
		}
		switch row.Kind {
		case "agent_task":
			out.AgentTask = counts
		case "workflow_run":
			out.WorkflowRun = counts
		case "image_session":
			out.ImageSession = counts
		case "local_edit":
			out.LocalEdit = counts
		}
	}
	return out, nil
}

func loadOverviewRecords(ctx context.Context, tx *gorm.DB, merchantID string, query overviewQuery, from, to time.Time) (ProductOverviewRecordPage, error) {
	base := merchantWorkRecordQuery(tx.WithContext(ctx), merchantID)
	if query.Kind != "all" {
		base = base.Where("kind = ?", query.Kind)
	}
	base = applyOverviewStateFilter(base, query.State, from, to)

	out := ProductOverviewRecordPage{
		Items:    []ProductOverviewRecord{},
		Page:     query.Page,
		PageSize: query.PageSize,
	}
	if err := base.Count(&out.Total).Error; err != nil {
		return ProductOverviewRecordPage{}, err
	}
	var rows []merchantWorkRecord
	if err := base.Select("id, kind, product_id, product_name, session_id, title, status, created_at, started_at, finished_at, failure_reason").
		Order("created_at DESC, kind ASC, id DESC").
		Offset((query.Page - 1) * query.PageSize).
		Limit(query.PageSize).
		Scan(&rows).Error; err != nil {
		return ProductOverviewRecordPage{}, err
	}
	for _, row := range rows {
		out.Items = append(out.Items, ProductOverviewRecord{
			ID:            row.ID,
			Kind:          row.Kind,
			ProductID:     row.ProductID,
			ProductName:   row.ProductName,
			SessionID:     row.SessionID,
			Title:         row.Title,
			Status:        row.Status,
			CreatedAt:     row.CreatedAt,
			StartedAt:     row.StartedAt,
			FinishedAt:    row.FinishedAt,
			FailureReason: row.FailureReason,
		})
	}
	return out, nil
}

func applyOverviewStateFilter(query *gorm.DB, state string, from, to time.Time) *gorm.DB {
	switch state {
	case "active":
		return query.Where("status IN ('queued', 'running')")
	case "waiting":
		return query.Where("status IN ('waiting_user', 'awaiting_confirmation')")
	case "unknown":
		return query.Where("status = 'unknown'")
	case "failed":
		return query.Where("status = 'failed' AND activity_at >= ? AND activity_at <= ?", from, to)
	default:
		return query.Where(`
			status IN ('queued', 'running', 'waiting_user', 'awaiting_confirmation', 'paused', 'draft', 'unknown')
			OR (status IN ('succeeded', 'failed', 'canceled', 'cancelled') AND activity_at >= ? AND activity_at <= ?)`, from, to)
	}
}
