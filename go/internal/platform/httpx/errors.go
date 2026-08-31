package httpx

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/yuqie6/productflow/internal/platform/apperr"
)

// AbortDetail 写入 {"detail"} 并 Abort，后续 handler 不再执行。
func AbortDetail(c *gin.Context, status int, detail string) {
	c.AbortWithStatusJSON(status, gin.H{"detail": detail})
}

// WriteDetail 写入 {"detail"} 且不 Abort，后续中间件仍会执行。
// 调用时机：登录失败等需要写完 body 但仍走完 handler 的路径。普通业务错误用 [AbortErr]/[AbortDetail]。
func WriteDetail(c *gin.Context, status int, detail string) {
	c.JSON(status, gin.H{"detail": detail})
}

// Unauthorized 以 401 调用 [AbortDetail]。
func Unauthorized(c *gin.Context, detail string) {
	AbortDetail(c, http.StatusUnauthorized, detail)
}

// AbortErr 把 [apperr.Error] 写成 {"detail"}，Code 非空时附加 {"code"}；其他 error 记 Gin error 并返回 500「服务器内部错误」。
func AbortErr(c *gin.Context, err error) {
	var e apperr.Error
	if errors.As(err, &e) {
		if e.Status >= 500 {
			_ = c.Error(err)
		}
		body := gin.H{"detail": e.Detail}
		if e.Code != "" {
			body["code"] = e.Code
		}
		c.AbortWithStatusJSON(e.Status, body)
		return
	}
	_ = c.Error(err)
	AbortDetail(c, http.StatusInternalServerError, "服务器内部错误")
}
