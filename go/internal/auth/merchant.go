package auth

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/httpx"
	"gorm.io/gorm"
)

// 跨商家根对象统一对外 404，防枚举（B1 钉死；全站一致，不用 403）。
const CrossMerchantDetail = "资源不存在"

type merchantContextKey struct{}

const ginMerchantKey = "productflow.auth.merchant_id"
const merchantHeader = "X-ProductFlow-Merchant-Id"

// WithMerchantID 把当前工作商家写入 context，供 store 强制归属过滤。
func WithMerchantID(ctx context.Context, merchantID string) context.Context {
	merchantID = strings.TrimSpace(merchantID)
	if merchantID == "" {
		return ctx
	}
	return context.WithValue(ctx, merchantContextKey{}, merchantID)
}

// MerchantIDFrom 读取 context 中的工作商家；未设置返回 false。
func MerchantIDFrom(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	v, ok := ctx.Value(merchantContextKey{}).(string)
	if !ok || strings.TrimSpace(v) == "" {
		return "", false
	}
	return v, true
}

// RequireMerchantID 读写根对象时优先用 context 商家；缺失时由调用方或 DB 触发器回落。
// 查询过滤请用 ScopeMerchant / MerchantIDFrom。
func RequireMerchantID(ctx context.Context) (string, error) {
	id, ok := MerchantIDFrom(ctx)
	if !ok {
		return "", apperr.Internal("缺少商家上下文")
	}
	return id, nil
}

// ResolveMerchantID 写入用：有 context 则用之，否则空串交由 migrate 触发器填唯一商家。
func ResolveMerchantID(ctx context.Context) string {
	id, _ := MerchantIDFrom(ctx)
	return id
}

// MerchantIDFromGin 读取 AttachWorkingMerchant 挂上的商家。
func MerchantIDFromGin(c *gin.Context) (string, bool) {
	if c == nil {
		return "", false
	}
	v, ok := c.Get(ginMerchantKey)
	if !ok {
		return "", false
	}
	id, _ := v.(string)
	id = strings.TrimSpace(id)
	if id == "" {
		return "", false
	}
	return id, true
}

// AttachWorkingMerchant 解析当前工作商家并写入 Gin + Request context。
// 声明头 X-ProductFlow-Merchant-Id 仅作工作商家选择，须有有效 Membership，否则 403。
// 无声明时：有会话则取首个有效成员商家；无门禁且无会话时回落到实例唯一商家（单商开发）。
func (h HTTP) AttachWorkingMerchant() gin.HandlerFunc {
	return func(c *gin.Context) {
		merchantID, err := h.resolveWorkingMerchant(c)
		if err != nil {
			httpx.AbortErr(c, err)
			return
		}
		if merchantID != "" {
			c.Set(ginMerchantKey, merchantID)
			c.Request = c.Request.WithContext(WithMerchantID(c.Request.Context(), merchantID))
		}
		c.Next()
	}
}

func (h HTTP) resolveWorkingMerchant(c *gin.Context) (string, error) {
	declared := strings.TrimSpace(c.GetHeader(merchantHeader))
	principal := PrincipalFrom(c)
	if principal != nil {
		if declared != "" {
			if _, err := h.svc().ActiveMembership(c.Request.Context(), principal.UserID, declared); err != nil {
				return "", err
			}
			return declared, nil
		}
		rows, err := h.svc().ListMemberships(c.Request.Context(), principal.UserID)
		if err != nil {
			return "", err
		}
		if len(rows) == 0 {
			return "", nil
		}
		return rows[0].MerchantID, nil
	}
	if declared != "" {
		return "", apperr.Error{Status: http.StatusUnauthorized, Detail: "请先登录"}
	}
	// 本地无门禁：回落唯一开发商家，便于未登录探测路由；有多商时不猜。
	return h.svc().SoleMerchantID(c.Request.Context())
}

// SoleMerchantID 返回实例内唯一商家；0 个或多于 1 个时返回空串（不猜）。
func (s Service) SoleMerchantID(ctx context.Context) (string, error) {
	if err := s.requireDB(); err != nil {
		return "", err
	}
	var rows []schema.Merchants
	if err := s.DB.WithContext(ctx).Select("id").Order("created_at ASC, id ASC").Limit(2).Find(&rows).Error; err != nil {
		return "", err
	}
	if len(rows) != 1 {
		return "", nil
	}
	return rows[0].ID, nil
}

// DevMerchantID 测试/夹具：取库中首个商家。
func DevMerchantID(ctx context.Context, db *gorm.DB) (string, error) {
	if db == nil {
		return "", apperr.Internal("数据库未配置")
	}
	var row schema.Merchants
	err := db.WithContext(ctx).Select("id").Order("created_at ASC, id ASC").Take(&row).Error
	if err == gorm.ErrRecordNotFound {
		return "", apperr.NotFound("商家不存在")
	}
	return row.ID, err
}

// ScopeMerchant 在已有工作商家时追加 merchant_id 过滤；无商家上下文时不加（worker 按主键加载）。
func ScopeMerchant(ctx context.Context, q *gorm.DB, column string) *gorm.DB {
	if column == "" {
		column = "merchant_id"
	}
	if id, ok := MerchantIDFrom(ctx); ok {
		return q.Where(column+" = ?", id)
	}
	return q
}

// NotFoundCrossMerchant 统一跨商/缺失根对象错误（404）。
func NotFoundCrossMerchant() error {
	return apperr.NotFound(CrossMerchantDetail)
}

// AbortIfNoMerchant 在需商家上下文的业务路由上：已认证但无商家则 403。
func AbortIfNoMerchant(c *gin.Context) bool {
	if _, ok := MerchantIDFromGin(c); ok {
		return false
	}
	if PrincipalFrom(c) != nil {
		httpx.AbortDetail(c, http.StatusForbidden, "当前用户不属于任何商家")
		return true
	}
	return false
}

// RequireWorkingMerchant 浏览器业务路由中间件：已登录但无有效 Membership 时 403（成员撤销后读写）。
func RequireWorkingMerchant() gin.HandlerFunc {
	return func(c *gin.Context) {
		if AbortIfNoMerchant(c) {
			return
		}
		c.Next()
	}
}

// RejectSuspendedMerchantWrites 停用商家后拒绝非 Operator 的业务写请求。
// 读路径与 /api/auth、/api/settings、/api/ops、generation-queue 放行；Operator 可继续写（含启停与支持）。
func (h HTTP) RejectSuspendedMerchantWrites() gin.HandlerFunc {
	return func(c *gin.Context) {
		switch c.Request.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		default:
			c.Next()
			return
		}
		if skipSuspendedWriteGate(c.Request.URL.Path) {
			c.Next()
			return
		}
		principal := PrincipalFrom(c)
		if principal != nil && principal.IsOperator {
			c.Next()
			return
		}
		merchantID, ok := MerchantIDFromGin(c)
		if !ok {
			c.Next()
			return
		}
		status, err := h.svc().MerchantStatus(c.Request.Context(), merchantID)
		if err != nil {
			httpx.AbortErr(c, err)
			return
		}
		if status == MerchantStatusSuspended {
			httpx.AbortDetail(c, http.StatusForbidden, "商家已停用，无法写入")
			return
		}
		c.Next()
	}
}

func skipSuspendedWriteGate(path string) bool {
	switch {
	case strings.HasPrefix(path, "/api/auth/"):
		return true
	case strings.HasPrefix(path, "/api/settings"):
		return true
	case path == "/api/generation-queue":
		return true
	case strings.HasPrefix(path, "/api/ops/"):
		return true
	default:
		return false
	}
}
