package httpx

import (
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

const requestIDHeader = "x-request-id"

func NewEngine(logger *zap.Logger) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	engine := gin.New()
	engine.Use(gin.Recovery())
	engine.Use(requestID())
	engine.Use(accessLog(logger))
	return engine
}

func requestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(requestIDHeader)
		if id == "" {
			id = newRequestID()
		}
		c.Set(requestIDHeader, id)
		c.Writer.Header().Set(requestIDHeader, id)
		c.Next()
	}
}

func accessLog(logger *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		if logger == nil {
			return
		}
		logger.Info("http",
			zap.String("method", c.Request.Method),
			zap.String("path", c.Request.URL.Path),
			zap.Int("status", c.Writer.Status()),
			zap.String("request_id", c.Writer.Header().Get(requestIDHeader)),
		)
	}
}
