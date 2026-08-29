package httpx

import (
	"net/http"

	"github.com/gin-gonic/gin"
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
