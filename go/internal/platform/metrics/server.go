package metrics

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// NewServer builds the optional metrics HTTP server (dispatcher or worker). An empty address or token disables it.
func NewServer(addr string, db *gorm.DB, token string) *http.Server {
	addr = strings.TrimSpace(addr)
	if addr == "" || db == nil || strings.TrimSpace(token) == "" {
		return nil
	}
	engine := gin.New()
	Register(engine, db, token)
	return &http.Server{Addr: addr, Handler: engine}
}
