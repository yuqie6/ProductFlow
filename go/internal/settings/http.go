package settings

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/yuqie6/productflow/internal/platform/httpx"
)

type LockState struct {
	Unlocked   bool `json:"unlocked"`
	Configured bool `json:"configured"`
}

type HTTP struct {
	Store               RuntimeReader
	SettingsAccessToken string
}

func (h HTTP) Register(engine *gin.Engine) {
	group := engine.Group("/api/settings")
	group.Use(h.requireAdmin())
	group.GET("/lock-state", h.lockState)
	group.GET("/runtime", h.runtime)
}

func (h HTTP) requireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		runtime, err := h.Store.Runtime(c.Request.Context())
		if err != nil {
			httpx.AbortDetail(c, http.StatusInternalServerError, "读取运行时设置失败")
			return
		}
		if !runtime.AdminAccessRequired {
			c.Next()
			return
		}
		if !httpx.SessionBool(c, "is_authenticated") {
			httpx.Unauthorized(c, "请先登录")
			return
		}
		c.Next()
	}
}

func (h HTTP) lockState(c *gin.Context) {
	configured := h.SettingsAccessToken != ""
	c.JSON(http.StatusOK, LockState{
		Unlocked:   configured && httpx.SessionBool(c, "settings_unlocked"),
		Configured: configured,
	})
}

func (h HTTP) runtime(c *gin.Context) {
	runtime, err := h.Store.Runtime(c.Request.Context())
	if err != nil {
		httpx.AbortDetail(c, http.StatusInternalServerError, "读取运行时设置失败")
		return
	}
	c.JSON(http.StatusOK, runtime)
}
