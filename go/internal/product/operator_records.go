package product

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/httpx"
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
	union := `SELECT t.id, 'agent_task' AS kind, t.product_id, t.title, t.status::text AS status, t.created_at, t.started_at, COALESCE(t.finished_at,t.canceled_at) AS finished_at, t.failure_reason IS NOT NULL AS has_failure
 FROM agent_tasks t WHERE t.merchant_id = ?
 UNION ALL SELECT r.id, 'workflow_run', p.id, p.name, r.status, r.started_at, r.started_at, r.finished_at, r.failure_reason IS NOT NULL
 FROM workflow_graph_runs r JOIN workflow_graphs g ON g.id = r.graph_id JOIN products p ON p.id = g.product_id WHERE p.merchant_id = ?
 UNION ALL SELECT t.id, 'image_session', NULL, s.title, t.status::text, t.created_at, t.started_at, t.finished_at, t.failure_reason IS NOT NULL
 FROM image_session_generation_tasks t JOIN image_sessions s ON s.id = t.session_id WHERE s.merchant_id = ?
 UNION ALL SELECT t.id, 'local_edit', p.id, p.name, t.status::text, t.created_at, t.started_at, t.finished_at, t.failure_reason IS NOT NULL
 FROM local_image_edit_tasks t JOIN products p ON p.id = t.product_id WHERE p.merchant_id = ?`
	db := h.Service.DB.WithContext(c.Request.Context())
	query := db.Table("(?) AS tasks", db.Raw(union, merchantID, merchantID, merchantID, merchantID))
	if productID != "" {
		query = query.Where("product_id = ?", productID)
	}
	out := operatorPage[operatorTask]{Items: []operatorTask{}, Page: page, PageSize: size}
	if err := query.Count(&out.Total).Error; err != nil {
		httpx.AbortErr(c, err)
		return
	}
	if err := query.Select("id, kind, product_id, title, status, created_at, started_at, finished_at, CASE WHEN has_failure THEN '任务执行未完成，请查看对应业务记录' ELSE NULL END AS failure_reason").Order("created_at DESC, kind ASC, id DESC").Offset((page - 1) * size).Limit(size).Scan(&out.Items).Error; err != nil {
		httpx.AbortErr(c, err)
		return
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
