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

type adjustRequest struct {
	IdempotencyKey string `json:"idempotency_key"`
	DeltaUnits     int64  `json:"delta_units"`
	Reason         string `json:"reason"`
}

// Register 挂上商家只读余额与 Op 只读/调账路由。
func (h HTTP) Register(engine *gin.Engine) {
	merchants := engine.Group("/api/merchants")
	merchants.GET("/:merchant_id/quota", h.Auth.RequireMembership("merchant_id"), h.getMerchantAccount)

	ops := engine.Group("/api/ops")
	ops.Use(auth.RequireOperator())
	ops.GET("/merchants/:merchant_id/quota", h.getOpAccount)
	ops.POST("/merchants/:merchant_id/quota/adjust", h.adjust)
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
