package agent

import (
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/library"
	"github.com/yuqie6/productflow/internal/media"
	"github.com/yuqie6/productflow/internal/product"
	"github.com/yuqie6/productflow/internal/settings"
	"gorm.io/gorm"
)

// Service 拥有 Agent Session / Task / Conversation / Turn 投影与内部工具面。
type Service struct {
	DB       *gorm.DB
	Pool     *pgxpool.Pool
	Graph    graph.Service
	Product  product.Service
	Library  library.Service
	Media    media.Store
	Settings *settings.Store
	Gateway  Gateway
	Poll     time.Duration
	control  *controlHub
}

func (s Service) GatewayConfigured() bool {
	if s.Gateway == nil {
		return false
	}
	type configured interface{ Configured() bool }
	if g, ok := s.Gateway.(configured); ok {
		return g.Configured()
	}
	return true
}
