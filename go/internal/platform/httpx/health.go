package httpx

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RegisterHealth 注册 GET /healthz（进程存活）与 GET /healthz/ready（Ping PostgreSQL）。
func RegisterHealth(engine *gin.Engine, pool *pgxpool.Pool) {
	engine.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	engine.GET("/healthz/ready", func(c *gin.Context) {
		if pool == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unready", "detail": "database pool missing"})
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		if err := pool.Ping(ctx); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unready", "detail": "database unavailable"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
}
