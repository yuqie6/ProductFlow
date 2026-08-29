package main

import (
	"github.com/gin-gonic/gin"
	"github.com/yuqie6/productflow/internal/agent"
	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/delivery"
	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/imagesession"
	"github.com/yuqie6/productflow/internal/library"
	"github.com/yuqie6/productflow/internal/localedit"
	"github.com/yuqie6/productflow/internal/product"
	"github.com/yuqie6/productflow/internal/recipe"
	"github.com/yuqie6/productflow/internal/settings"
)

// apiHandlers is the live HTTP surface. Tests register the same set so
// contracts/http-routes.json cannot drift from cmd/productflow-api.
type apiHandlers struct {
	Auth         auth.HTTP
	Settings     settings.HTTP
	Product      product.HTTP
	Library      library.HTTP
	Graph        graph.HTTP
	Recipe       recipe.HTTP
	ImageSession imagesession.HTTP
	Delivery     delivery.HTTP
	LocalEdit    localedit.HTTP
	Agent        agent.HTTP
}

func registerAPI(engine *gin.Engine, h apiHandlers) {
	h.Auth.Register(engine)
	h.Settings.Register(engine)
	h.Product.Register(engine)
	h.Library.Register(engine)
	h.Graph.Register(engine)
	h.Recipe.Register(engine)
	h.ImageSession.Register(engine)
	h.Delivery.Register(engine)
	h.LocalEdit.Register(engine)
	h.Agent.Register(engine)
}
