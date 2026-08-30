package httpx

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/yuqie6/productflow/internal/platform/apperr"
)

func AbortDetail(c *gin.Context, status int, detail string) {
	c.AbortWithStatusJSON(status, gin.H{"detail": detail})
}

func WriteDetail(c *gin.Context, status int, detail string) {
	c.JSON(status, gin.H{"detail": detail})
}

func Unauthorized(c *gin.Context, detail string) {
	AbortDetail(c, http.StatusUnauthorized, detail)
}

func AbortErr(c *gin.Context, err error) {
	var e apperr.Error
	if errors.As(err, &e) {
		if e.Status >= 500 {
			_ = c.Error(err)
		}
		AbortDetail(c, e.Status, e.Detail)
		return
	}
	_ = c.Error(err)
	AbortDetail(c, http.StatusInternalServerError, "服务器内部错误")
}
