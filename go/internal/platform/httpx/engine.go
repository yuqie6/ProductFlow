package httpx

import (
	"fmt"
	"strings"
	"time"

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
		started := time.Now()
		c.Next()
		if logger == nil {
			return
		}
		status := c.Writer.Status()
		path := c.Request.URL.Path
		duration := time.Since(started)
		fields := []zap.Field{
			zap.String("method", c.Request.Method),
			zap.String("path", path),
			zap.Int("status", status),
			zap.Int64("duration_ms", duration.Milliseconds()),
			zap.String("request_id", c.Writer.Header().Get(requestIDHeader)),
		}
		if status >= 500 && len(c.Errors) > 0 {
			fields = append(fields, zap.String("error", c.Errors.Last().Error()))
		}
		msg := fmt.Sprintf("%s %s %d  %s", c.Request.Method, path, status, formatAccessDuration(duration))
		switch {
		case status >= 500:
			logger.Warn(msg, fields...)
		case quietAccessPath(path) && status < 400:
			logger.Debug(msg, fields...)
		default:
			logger.Info(msg, fields...)
		}
	}
}

func quietAccessPath(path string) bool {
	switch path {
	case "/healthz", "/healthz/ready":
		return true
	}
	return strings.HasSuffix(path, "/heartbeat")
}

func formatAccessDuration(d time.Duration) string {
	if d < time.Millisecond {
		return d.Round(time.Microsecond).String()
	}
	return d.Round(time.Millisecond).String()
}
