package httpx

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// RequireAdmin matches Python require_admin: skip when access is not required.
func RequireAdmin(reader func(c *gin.Context) (required bool, err error)) gin.HandlerFunc {
	return func(c *gin.Context) {
		required, err := reader(c)
		if err != nil {
			AbortDetail(c, http.StatusInternalServerError, "读取运行时设置失败")
			return
		}
		if !required {
			c.Next()
			return
		}
		if !SessionBool(c, "is_authenticated") {
			Unauthorized(c, "请先登录")
			return
		}
		c.Next()
	}
}
