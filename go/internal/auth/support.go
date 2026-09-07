package auth

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yuqie6/productflow/internal/platform/httpx"
)

// A8 支持会话/审计合同草案（B9）。完整 MP-D 产品化另发；本文件钉字段与入口占位。

const (
	SupportContractVersion = 1
	SupportNotImplemented  = "支持会话尚未产品化（合同草案 A8）"
)

// SupportAccessContract 是 Op 支持访问商家数据的合同投影；implemented=false 表示仅草案。
type SupportAccessContract struct {
	ContractVersion int      `json:"contract_version"`
	Implemented     bool     `json:"implemented"`
	Summary         string   `json:"summary"`
	Rules           []string `json:"rules"`
	SessionFields   []string `json:"session_fields"`
	AuditFields     []string `json:"audit_fields"`
	Actions         []string `json:"actions"`
}

// SupportSessionDraft 是支持会话行的目标字段（未持久化）。
type SupportSessionDraft struct {
	ID             string     `json:"id"`
	MerchantID     string     `json:"merchant_id"`
	OperatorUserID string     `json:"operator_user_id"`
	Purpose        string     `json:"purpose"`
	StartedAt      time.Time  `json:"started_at"`
	EndedAt        *time.Time `json:"ended_at"`
}

// SupportAuditEventDraft 是支持访问审计行的目标字段（未持久化）。
type SupportAuditEventDraft struct {
	ID               string         `json:"id"`
	SupportSessionID string         `json:"support_session_id"`
	ActorUserID      string         `json:"actor_user_id"`
	Action           string         `json:"action"`
	ResourceType     string         `json:"resource_type"`
	ResourceID       string         `json:"resource_id"`
	MerchantID       string         `json:"merchant_id"`
	CreatedAt        time.Time      `json:"created_at"`
	Detail           map[string]any `json:"detail"`
}

// SupportContractDocument 返回冻结的 A8 草案正文。
func SupportContractDocument() SupportAccessContract {
	return SupportAccessContract{
		ContractVersion: SupportContractVersion,
		Implemented:     false,
		Summary:         "运营读取商家内容须显式支持会话、目的与审计；禁止隐式全局商家身份。",
		Rules: []string{
			"仅站点 Operator 可开启支持会话",
			"会话必须绑定目标 merchant_id 与 purpose",
			"支持期内每次读商家资源须写审计事件",
			"会话结束后不得继续用支持身份读商家内容",
			"运营密钥与实例 settings 不进入商家上下文",
			"本入口不开放第二外部商，也不宣称完整 MP-D",
		},
		SessionFields: []string{
			"id", "merchant_id", "operator_user_id", "purpose", "started_at", "ended_at",
		},
		AuditFields: []string{
			"id", "support_session_id", "actor_user_id", "action",
			"resource_type", "resource_id", "merchant_id", "created_at", "detail",
		},
		Actions: []string{
			"open_session", "read_resource", "end_session",
		},
	}
}

func (h HTTP) registerSupportOps(engine *gin.Engine) {
	ops := engine.Group("/api/ops")
	ops.Use(RequireOperator())
	ops.GET("/support-contract", h.getSupportContract)
	ops.POST("/support-sessions", h.createSupportSessionPlaceholder)
	ops.GET("/support-sessions/:session_id", h.getSupportSessionPlaceholder)
	ops.POST("/support-sessions/:session_id/end", h.endSupportSessionPlaceholder)
}

func (h HTTP) getSupportContract(c *gin.Context) {
	c.JSON(http.StatusOK, SupportContractDocument())
}

func (h HTTP) createSupportSessionPlaceholder(c *gin.Context) {
	httpx.AbortDetail(c, http.StatusNotImplemented, SupportNotImplemented)
}

func (h HTTP) getSupportSessionPlaceholder(c *gin.Context) {
	httpx.AbortDetail(c, http.StatusNotImplemented, SupportNotImplemented)
}

func (h HTTP) endSupportSessionPlaceholder(c *gin.Context) {
	httpx.AbortDetail(c, http.StatusNotImplemented, SupportNotImplemented)
}
