package product

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/httpx"
	"gorm.io/gorm"
)

type operatorPage[T any] struct {
	Items    []T   `json:"items"`
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"page_size"`
}

// operatorAction is the public audit projection; persistence-only fields stay private.
type operatorAction struct {
	ID            string    `json:"id"`
	ActorUserID   string    `json:"actor_user_id"`
	ActorName     string    `json:"actor_name"`
	MerchantID    string    `json:"merchant_id"`
	ProductID     string    `json:"product_id"`
	ProductName   string    `json:"product_name"`
	Action        string    `json:"action"`
	CreatedAt     time.Time `json:"created_at"`
	Result        string    `json:"result"`
	FailureReason *string   `json:"failure_reason"`
}

type operatorTask struct {
	ID            string     `json:"id"`
	Kind          string     `json:"kind"`
	ProductID     *string    `json:"product_id"`
	Title         string     `json:"title"`
	Status        string     `json:"status"`
	CreatedAt     time.Time  `json:"created_at"`
	StartedAt     *time.Time `json:"started_at"`
	FinishedAt    *time.Time `json:"finished_at"`
	FailureReason *string    `json:"failure_reason"`
}

const publicWorkFailureReason = "任务执行未完成，请查看对应业务记录"

// merchantWorkRecord is the shared, redacted read row for merchant work.
// OperatorTask is a deliberately smaller legacy projection of this row.
type merchantWorkRecord struct {
	ID                 string     `gorm:"column:id"`
	Kind               string     `gorm:"column:kind"`
	ProductID          *string    `gorm:"column:product_id"`
	ProductName        *string    `gorm:"column:product_name"`
	SessionID          *string    `gorm:"column:session_id"`
	Title              string     `gorm:"column:title"`
	Status             string     `gorm:"column:status"`
	CreatedAt          time.Time  `gorm:"column:created_at"`
	StartedAt          *time.Time `gorm:"column:started_at"`
	FinishedAt         *time.Time `gorm:"column:finished_at"`
	OperatorFinishedAt *time.Time `gorm:"column:operator_finished_at"`
	FailureReason      *string    `gorm:"column:failure_reason"`
	ActivityAt         time.Time  `gorm:"column:activity_at"`
}

// merchantWorkRecordSQL is the one source of truth for the four merchant-owned
// work streams. It intentionally selects no prompts, goals, provider payloads,
// or other execution details.
const merchantWorkRecordSQL = `
SELECT t.id,
       'agent_task' AS kind,
       CASE WHEN p.id IS NULL THEN NULL ELSE t.product_id END AS product_id,
       p.name AS product_name,
       t.session_id,
       t.title,
       t.status::text AS status,
       t.created_at,
       t.started_at,
       t.finished_at,
       COALESCE(t.finished_at, t.canceled_at) AS operator_finished_at,
       CASE WHEN t.failure_reason IS NOT NULL THEN ? ELSE NULL END AS failure_reason,
       COALESCE(t.finished_at, t.canceled_at, t.started_at, t.created_at) AS activity_at
FROM agent_tasks t
LEFT JOIN products p ON p.id = t.product_id AND p.merchant_id = t.merchant_id
WHERE t.merchant_id = ?
UNION ALL
SELECT r.id,
       'workflow_run',
       p.id,
       p.name,
       NULL::varchar,
       p.name,
       r.status::text,
       r.started_at,
       r.started_at,
       r.finished_at,
       r.finished_at,
       CASE WHEN r.failure_reason IS NOT NULL THEN ? ELSE NULL END,
       COALESCE(r.finished_at, r.started_at)
FROM workflow_graph_runs r
JOIN workflow_graphs g ON g.id = r.graph_id
JOIN products p ON p.id = g.product_id
WHERE p.merchant_id = ?
UNION ALL
SELECT t.id,
       'image_session',
       NULL::varchar,
       NULL::varchar,
       t.session_id,
       s.title,
       t.status::text,
       t.created_at,
       t.started_at,
       t.finished_at,
       t.finished_at,
       CASE WHEN t.failure_reason IS NOT NULL THEN ? ELSE NULL END,
       COALESCE(t.finished_at, t.started_at, t.created_at)
FROM image_session_generation_tasks t
JOIN image_sessions s ON s.id = t.session_id
WHERE s.merchant_id = ?
UNION ALL
SELECT t.id,
       'local_edit',
       p.id,
       p.name,
       NULL::varchar,
       p.name,
       t.status::text,
       t.created_at,
       t.started_at,
       t.finished_at,
       t.finished_at,
       CASE WHEN t.failure_reason IS NOT NULL THEN ? ELSE NULL END,
       COALESCE(t.finished_at, t.started_at, t.created_at)
FROM local_image_edit_tasks t
JOIN products p ON p.id = t.product_id
WHERE p.merchant_id = ?`

func merchantWorkRecordQuery(db *gorm.DB, merchantID string) *gorm.DB {
	return db.Table("(?) AS work_records", db.Raw(
		merchantWorkRecordSQL,
		publicWorkFailureReason, merchantID,
		publicWorkFailureReason, merchantID,
		publicWorkFailureReason, merchantID,
		publicWorkFailureReason, merchantID,
	))
}

func (row merchantWorkRecord) operatorProjection() operatorTask {
	return operatorTask{
		ID:            row.ID,
		Kind:          row.Kind,
		ProductID:     row.ProductID,
		Title:         row.Title,
		Status:        row.Status,
		CreatedAt:     row.CreatedAt,
		StartedAt:     row.StartedAt,
		FinishedAt:    row.OperatorFinishedAt,
		FailureReason: row.FailureReason,
	}
}

func operatorPagination(c *gin.Context) (int, int, error) {
	page, err := parseQueryInt(c, "page", 1, 1, 100000)
	if err != nil {
		return 0, 0, err
	}
	size, err := parseQueryInt(c, "page_size", 20, 1, 100)
	return page, size, err
}

// Read native task states; this projection grants no runtime commands or provider data.
func (h HTTP) listOperatorTasks(c *gin.Context) {
	page, size, err := operatorPagination(c)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	merchantID := auth.ResolveMerchantID(c.Request.Context())
	productID := c.Query("product_id")
	if productID != "" {
		if _, err := loadProduct(c.Request.Context(), h.Service.DB, productID); err != nil {
			httpx.AbortErr(c, err)
			return
		}
	}
	db := h.Service.DB.WithContext(c.Request.Context())
	query := merchantWorkRecordQuery(db, merchantID)
	if productID != "" {
		query = query.Where("product_id = ?", productID)
	}
	out := operatorPage[operatorTask]{Items: []operatorTask{}, Page: page, PageSize: size}
	if err := query.Count(&out.Total).Error; err != nil {
		httpx.AbortErr(c, err)
		return
	}
	var rows []merchantWorkRecord
	if err := query.Select("id, kind, product_id, title, status, created_at, started_at, operator_finished_at, failure_reason").Order("created_at DESC, kind ASC, id DESC").Offset((page - 1) * size).Limit(size).Scan(&rows).Error; err != nil {
		httpx.AbortErr(c, err)
		return
	}
	for _, row := range rows {
		out.Items = append(out.Items, row.operatorProjection())
	}
	c.JSON(http.StatusOK, out)
}

func (h HTTP) listOperatorActions(c *gin.Context) {
	page, size, err := operatorPagination(c)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	query := h.Service.DB.WithContext(c.Request.Context()).Model(&schema.OperatorProductActions{}).Where("merchant_id = ?", auth.ResolveMerchantID(c.Request.Context()))
	if id := c.Query("product_id"); id != "" {
		query = query.Where("product_id = ?", id)
	}
	out := operatorPage[operatorAction]{Items: []operatorAction{}, Page: page, PageSize: size}
	if err := query.Count(&out.Total).Error; err != nil {
		httpx.AbortErr(c, err)
		return
	}
	if err := query.Select("id, actor_user_id, actor_name, merchant_id, product_id, product_name, action, created_at, result, failure_reason").Order("created_at DESC, id DESC").Offset((page - 1) * size).Limit(size).Scan(&out.Items).Error; err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}
