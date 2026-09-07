package main

import (
	"github.com/gin-gonic/gin"
	"github.com/yuqie6/productflow/internal/agent"
	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/brand"
	"github.com/yuqie6/productflow/internal/delivery"
	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/imagesession"
	"github.com/yuqie6/productflow/internal/library"
	"github.com/yuqie6/productflow/internal/localedit"
	"github.com/yuqie6/productflow/internal/platform/httpx"
	"github.com/yuqie6/productflow/internal/product"
	"github.com/yuqie6/productflow/internal/quota"
	"github.com/yuqie6/productflow/internal/recipe"
	"github.com/yuqie6/productflow/internal/settings"
	"github.com/yuqie6/productflow/internal/visualsystem"
)

// apiHandlers 是线上 HTTP 面。契约测试必须注册同一组，避免 contracts/http-routes.json 与 cmd/productflow-api 漂移。
type apiHandlers struct {
	AllowedOrigins []string
	Auth           auth.HTTP
	Settings       settings.HTTP
	Product        product.HTTP
	Library        library.HTTP
	Graph          graph.HTTP
	Recipe         recipe.HTTP
	ImageSession   imagesession.HTTP
	Delivery       delivery.HTTP
	VisualSystem   visualsystem.HTTP
	Brand          brand.HTTP
	LocalEdit      localedit.HTTP
	Agent          agent.HTTP
	Quota          quota.HTTP
}

// registerAPI 按固定顺序挂上各垂直切片。增删路由必须同时改测试与 http-routes.json，不要只改一处。
func registerAPI(engine *gin.Engine, h apiHandlers) {
	engine.Use(httpx.BrowserStateProtection(httpx.BrowserStateConfig{
		AllowedOrigins: h.AllowedOrigins,
		InternalToken:  h.Agent.InternalToken,
	}))
	h.Auth.Register(engine)
	h.Settings.Register(engine)
	h.Quota.Register(engine)
	h.Product.Register(engine)
	h.Library.Register(engine)
	h.Graph.Register(engine)
	h.Recipe.Register(engine)
	h.ImageSession.Register(engine)
	h.Delivery.Register(engine)
	h.VisualSystem.Register(engine)
	h.Brand.Register(engine)
	h.LocalEdit.Register(engine)
	h.Agent.Register(engine)
}
