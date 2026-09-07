package httpx

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// AuthenticatedFunc 由 auth 包注入：校验 cookie 中的可撤销 User 会话。
// 未注入时 RequireAdmin 在门禁开启下一律拒绝，禁止回退到 is_authenticated 布尔。
var AuthenticatedFunc func(c *gin.Context) bool

// RequireAdmin 在 AdminAccessRequired 时要求有效 User 会话。
// 未开启门禁时直接放行（本地开发无登录）；不再接受共享 admin 布尔 cookie。
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
		if AuthenticatedFunc == nil || !AuthenticatedFunc(c) {
			Unauthorized(c, "请先登录")
			return
		}
		c.Next()
	}
}
