package quota

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/platform/httpx"
	"gorm.io/gorm"
)

// HTTP 是商家/Op 余额面（MP-C B4）。只读投影与 Op 调账；≠支付 webhook。
type HTTP struct {
	DB   *gorm.DB
	Auth auth.HTTP // RequireMembership 校验本商成员；请求体里的商家 ID 不授予权限
}

func (h HTTP) svc() *Service {
	return &Service{DB: h.DB}
}

// AccountView 是余额 HTTP 投影。
type AccountView struct {
	MerchantID     string `json:"merchant_id"`
	Currency       string `json:"currency"`
	AvailableUnits int64  `json:"available_units"`
	ReservedUnits  int64  `json:"reserved_units"`
	PriceVersionID string `json:"price_version_id"`
}

// PriceEntryView 是价格条目 HTTP 投影。
type PriceEntryView struct {
	EntryCode string `json:"entry_code"`
	UnitPrice int64  `json:"unit_price"`
}

// PriceVersionView 是价格版本摘要 HTTP 投影。
type PriceVersionView struct {
	PriceVersionID string           `json:"price_version_id"`
	Label          string           `json:"label"`
	Currency       string           `json:"currency"`
	IsDefault      bool             `json:"is_default"`
	Entries        []PriceEntryView `json:"entries"`
}

type adjustRequest struct {
	IdempotencyKey string `json:"idempotency_key"`
	DeltaUnits     int64  `json:"delta_units"`
	Reason         string `json:"reason"`
}

// Register 挂上商家只读余额/价格摘要与 Op 只读/调账/默认价格目录路由。
func (h HTTP) Register(engine *gin.Engine) {
	merchants := engine.Group("/api/merchants")
	merchants.GET("/:merchant_id/quota", h.Auth.RequireMembership("merchant_id"), h.getMerchantAccount)
	merchants.GET("/:merchant_id/quota/price", h.Auth.RequireMembership("merchant_id"), h.getMerchantPrice)

	ops := engine.Group("/api/ops")
	ops.Use(auth.RequireOperator())
	ops.GET("/merchants/:merchant_id/quota", h.getOpAccount)
	ops.POST("/merchants/:merchant_id/quota/adjust", h.adjust)
	ops.GET("/quota/price-versions/default", h.getDefaultPriceVersion)
}

// getMerchantAccount 是 GET /api/merchants/:merchant_id/quota：200 返回本商余额。
func (h HTTP) getMerchantAccount(c *gin.Context) {
	h.writeAccount(c, c.Param("merchant_id"))
}

// getOpAccount 是 GET /api/ops/merchants/:merchant_id/quota：200 返回指定商余额。
func (h HTTP) getOpAccount(c *gin.Context) {
	h.writeAccount(c, c.Param("merchant_id"))
}

func (h HTTP) writeAccount(c *gin.Context, merchantID string) {
	acct, err := h.svc().GetAccount(c.Request.Context(), merchantID)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, accountView(acct))
}

// getMerchantPrice 是 GET /api/merchants/:merchant_id/quota/price：200 返回本商生效价格版本摘要。
func (h HTTP) getMerchantPrice(c *gin.Context) {
	acct, err := h.svc().GetAccount(c.Request.Context(), c.Param("merchant_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	version, err := h.svc().GetPriceVersion(c.Request.Context(), acct.PriceVersionID)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, priceVersionView(version))
}

// getDefaultPriceVersion 是 GET /api/ops/quota/price-versions/default：200 返回默认版本与单价。
func (h HTTP) getDefaultPriceVersion(c *gin.Context) {
	version, err := h.svc().GetDefaultPriceVersion(c.Request.Context())
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, priceVersionView(version))
}

// adjust 是 POST /api/ops/merchants/:merchant_id/quota/adjust：200 返回调账后余额。
// actor 取自当前 Operator 会话，不接受请求体伪造。
func (h HTTP) adjust(c *gin.Context) {
	principal := auth.PrincipalFrom(c)
	if principal == nil {
		httpx.Unauthorized(c, "请先登录")
		return
	}
	var payload adjustRequest
	if err := bindJSONStrict(c, &payload); err != nil {
		httpx.WriteDetail(c, http.StatusBadRequest, "请求体无效")
		return
	}
	acct, err := h.svc().Adjust(
		c.Request.Context(),
		c.Param("merchant_id"),
		payload.IdempotencyKey,
		payload.DeltaUnits,
		payload.Reason,
		principal.UserID,
	)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, accountView(acct))
}

func accountView(acct Account) AccountView {
	return AccountView{
		MerchantID:     acct.MerchantID,
		Currency:       acct.Currency,
		AvailableUnits: acct.AvailableUnits,
		ReservedUnits:  acct.ReservedUnits,
		PriceVersionID: acct.PriceVersionID,
	}
}

func priceVersionView(v PriceVersion) PriceVersionView {
	entries := make([]PriceEntryView, 0, len(v.Entries))
	for _, e := range v.Entries {
		entries = append(entries, PriceEntryView{EntryCode: e.EntryCode, UnitPrice: e.UnitPrice})
	}
	return PriceVersionView{
		PriceVersionID: v.ID,
		Label:          v.Label,
		Currency:       v.Currency,
		IsDefault:      v.IsDefault,
		Entries:        entries,
	}
}

func bindJSONStrict(c *gin.Context, dest any) error {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return io.ErrUnexpectedEOF
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dest); err != nil {
		return err
	}
	if dec.More() {
		return io.ErrUnexpectedEOF
	}
	return nil
}
