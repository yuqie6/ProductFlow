package product

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/httpx"
	"gorm.io/gorm"
)

type operatorActionKey struct{}

// Start outside the command transaction so interruption cannot erase the attempt.
// Only the command transaction can mark success; an uncertain commit remains unknown.
func (h HTTP) auditOperatorAction(action string) gin.HandlerFunc {
	return func(c *gin.Context) {
		principal := auth.PrincipalFrom(c)
		if principal == nil || !principal.IsOperator {
			httpx.AbortErr(c, apperr.Forbidden("仅管理员可操作"))
			return
		}
		target, err := loadProduct(c.Request.Context(), h.Service.DB, c.Param("product_id"))
		if err != nil {
			httpx.AbortErr(c, err)
			return
		}
		event := schema.OperatorProductActions{ID: clockid.New(), ActorUserID: principal.UserID, ActorName: principal.DisplayName, MerchantID: auth.ResolveMerchantID(c.Request.Context()), ProductID: target.ID, ProductName: target.Name, Action: action, CreatedAt: h.Service.now(), Result: "unknown"}
		if err := h.Service.DB.WithContext(c.Request.Context()).Create(&event).Error; err != nil {
			httpx.AbortErr(c, err)
			return
		}
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), operatorActionKey{}, event.ID))
		c.Next()
		if status := c.Writer.Status(); status >= 400 && status < 500 {
			ctx, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), 5*time.Second)
			defer cancel()
			// Store a public classification; response/provider payloads may contain private details.
			reason := fmt.Sprintf("HTTP %d %s", status, http.StatusText(status))
			if err := h.Service.DB.WithContext(ctx).Model(&schema.OperatorProductActions{}).Where("id = ? AND result = ?", event.ID, "unknown").Updates(map[string]any{"result": "rejected", "failure_reason": reason}).Error; err != nil {
				_ = c.Error(err)
			}
		}
	}
}

func completeOperatorAction(ctx context.Context, db *gorm.DB, product Product) error {
	id, _ := ctx.Value(operatorActionKey{}).(string)
	if id == "" {
		return nil
	}
	result := db.WithContext(ctx).Model(&schema.OperatorProductActions{}).
		Where("id = ? AND merchant_id = ? AND product_id = ? AND result = ?", id, auth.ResolveMerchantID(ctx), product.ID, "unknown").
		Updates(map[string]any{"result": "succeeded", "product_name": product.Name})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return apperr.Internal("管理员操作记录未能保存")
	}
	return nil
}
