package agent

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/httpx"
	"github.com/yuqie6/productflow/internal/settings"
)

// HTTP 挂载 Agent 的浏览器路由（/api/v2，管理员 session）和 agent-service 内部工具面（/api/internal/v1，Bearer token）。
//
// InternalToken 空则内部路由 503。Settings 给管理员 session 门闩。handler 只做绑定与状态码，改图必须走 Service → graph 包。
type HTTP struct {
	Service Service // 浏览器与内部工具面共用的 Agent 用例
	// Settings 读 AdminAccessRequired；nil 则不要求管理员口令。
	Settings interface {
		settings.RuntimeReader
	}
	// InternalToken 是内部 /api/internal/v1 的 Bearer，不是管理员 cookie。空则内部路由 503。
	InternalToken string
}

// Register 挂上 /api/v2 Agent 浏览器路由，全部走管理员 session cookie。
//
// 路径前缀 /api/v2。浏览器只打这里（Session / Task / 工作台 / Turn / SSE）。内部 claim、journal、工具面由 registerInternal 挂 /api/internal/v1，不要把那些接口暴露给 Web。
func (h HTTP) Register(engine *gin.Engine) {
	admin := httpx.RequireAdmin(func(c *gin.Context) (bool, error) {
		if h.Settings == nil {
			return true, nil
		}
		runtime, err := h.Settings.Runtime(c.Request.Context())
		if err != nil {
			return false, err
		}
		return runtime.AdminAccessRequired, nil
	})
	v2 := engine.Group("/api/v2", admin, abortIfNoMerchant)
	v2.GET("/agent-sessions", h.listSessions)
	v2.POST("/agent-sessions", h.createSession)
	v2.PATCH("/agent-sessions/:session_id", h.renameSession)
	v2.POST("/agent-sessions/:session_id/archive", h.archiveSession)

	v2.GET("/agent-tasks", h.listTasks)
	v2.POST("/agent-tasks", h.createTask)
	v2.GET("/agent-tasks/:task_id", h.getTask)
	v2.PATCH("/agent-tasks/:task_id", h.renameTask)
	v2.POST("/agent-tasks/:task_id/cancel", h.cancelTask)
	v2.POST("/agent-tasks/:task_id/pause", h.pauseTask)
	v2.POST("/agent-tasks/:task_id/complete", h.completeTask)
	v2.POST("/agent-tasks/:task_id/resume", h.resumeTask)
	v2.GET("/agent-control/events", h.streamControlEvents)

	v2.GET("/products/:product_id/agent-workbench", h.getWorkbench)
	v2.POST("/products/:product_id/agent-workbench", h.ensureWorkbench)

	productConv := v2.Group("/products/:product_id/agent-conversations")
	productConv.GET("/:conversation_id", h.getProductConversation)
	productConv.GET("/:conversation_id/workflow-run-request", h.getProductRunRequest)
	productConv.POST("/:conversation_id/workflow-run-request/:request_id/confirm", h.confirmProductRunRequest)
	productConv.POST("/:conversation_id/workflow-run-request/:request_id/cancel", h.cancelProductRunRequest)
	productConv.GET("/:conversation_id/turns", h.listProductTurns)
	productConv.POST("/:conversation_id/turns", h.submitProductTurn)
	productConv.GET("/:conversation_id/turns/:projection_id", h.getProductTurn)
	productConv.POST("/:conversation_id/turns/:projection_id/cancel", h.cancelProductTurn)
	productConv.POST("/:conversation_id/turns/:projection_id/resume", h.resumeProductTurn)
	productConv.POST("/:conversation_id/turns/:projection_id/questions/:question_id/answer", h.answerProductQuestion)
	productConv.POST("/:conversation_id/turns/:projection_id/effect-reconciliation", h.reconcileProductEffect)
	productConv.GET("/:conversation_id/turns/:projection_id/events", h.streamProductEvents)
	productConv.GET("/:conversation_id/turns/:projection_id/events/page", h.pageProductEvents)

	global := v2.Group("/agent-conversations")
	global.GET("/:conversation_id/library-organization-draft", h.getLibraryDraft)
	global.POST("/:conversation_id/library-organization-draft/confirm", h.confirmLibraryDraft)
	global.GET("/:conversation_id/workflow-run-request", h.getGlobalRunRequest)
	global.POST("/:conversation_id/workflow-run-request/:request_id/confirm", h.confirmGlobalRunRequest)
	global.POST("/:conversation_id/workflow-run-request/:request_id/cancel", h.cancelGlobalRunRequest)
	global.GET("/:conversation_id/turns", h.listGlobalTurns)
	global.POST("/:conversation_id/turns", h.submitGlobalTurn)
	global.GET("/:conversation_id/turns/:projection_id", h.getGlobalTurn)
	global.POST("/:conversation_id/turns/:projection_id/cancel", h.cancelGlobalTurn)
	global.POST("/:conversation_id/turns/:projection_id/resume", h.resumeGlobalTurn)
	global.POST("/:conversation_id/turns/:projection_id/questions/:question_id/answer", h.answerGlobalQuestion)
	global.POST("/:conversation_id/turns/:projection_id/effect-reconciliation", h.reconcileGlobalEffect)
	global.GET("/:conversation_id/turns/:projection_id/events", h.streamGlobalEvents)
	global.GET("/:conversation_id/turns/:projection_id/events/page", h.pageGlobalEvents)

	h.registerInternal(engine)
}

// listSessions 处理 GET /api/v2/agent-sessions。
//
// 200 返回 SessionListResponse。query：product_id 可选（空=全局 Dock）；include_archived 默认 false；after 是 opaque cursor 不是页码；limit 默认 20、上限 100。
// limit/cursor 非法 400。不改 Goal、lease、journal。
func (h HTTP) listSessions(c *gin.Context) {
	var productID *string
	if raw := strings.TrimSpace(c.Query("product_id")); raw != "" {
		productID = &raw
	}
	limit, err := queryInt(c, "limit", sessionListDefaultLimit, 1, sessionListMax)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	out, err := h.Service.ListSessions(c.Request.Context(), queryBool(c, "include_archived", false), productID, c.Query("after"), limit)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// createSession 处理 POST /api/v2/agent-sessions。
//
// 201 返回 SessionResponse（全局 Dock + 一条 collecting conversation）。无请求体。不创建 Task / Turn / Goal。
func (h HTTP) createSession(c *gin.Context) {
	out, err := h.Service.CreateSession(c.Request.Context())
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, out)
}

// renameSession 处理 PATCH /api/v2/agent-sessions/:session_id。
//
// 200 返回 SessionResponse。JSON {"title"}；未知字段或标题非法 400；找不到 404。只改标题，不归档、不改 Goal。
func (h HTTP) renameSession(c *gin.Context) {
	var req struct {
		Title string `json:"title"`
	}
	if err := bindJSONStrict(c, &req); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	out, err := h.Service.RenameSession(c.Request.Context(), c.Param("session_id"), req.Title)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// archiveSession 处理 POST /api/v2/agent-sessions/:session_id/archive。
//
// 200 返回 SessionResponse。已归档幂等。找不到 404。归档不是取消 Goal。
func (h HTTP) archiveSession(c *gin.Context) {
	out, err := h.Service.ArchiveSession(c.Request.Context(), c.Param("session_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// listTasks 处理 GET /api/v2/agent-tasks。
//
// 200 返回 TaskListResponse。query：session_id 可选；include_terminal 默认 true；after 是 opaque cursor 不是页码（须匹配当前筛选）；limit 默认 50、上限 100。
// limit/cursor 非法 400。列表 sync 不得把 goal_loop 标成 succeeded。
func (h HTTP) listTasks(c *gin.Context) {
	limit, err := queryInt(c, "limit", taskListDefaultLimit, 1, taskListMaxLimit)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	out, err := h.Service.ListTasks(c.Request.Context(), queryOpt(c, "session_id"), queryBool(c, "include_terminal", true), c.Query("after"), limit)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// createTask 处理 POST /api/v2/agent-tasks。
//
// 201 返回 TaskResponse。JSON session_id / title / goal，conversation_id 可选。标题或 Goal 非法 400；Session/对话不存在 404；对话不属于 Session 409。
// 这是用户显式 Goal，后续 Turn 成功不会自动完成它。未知字段 400。
func (h HTTP) createTask(c *gin.Context) {
	var req struct {
		SessionID      string  `json:"session_id"`
		Title          string  `json:"title"`
		Goal           string  `json:"goal"`
		ConversationID *string `json:"conversation_id"`
	}
	if err := bindJSONStrict(c, &req); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	out, err := h.Service.CreateTask(c.Request.Context(), req.SessionID, req.Title, req.Goal, req.ConversationID)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, out)
}

// getTask 处理 GET /api/v2/agent-tasks/:task_id。
//
// 200 返回 TaskResponse。找不到 404。会 sync 关联 GraphRun，但 waiting_reason=goal_loop 不得覆盖。
func (h HTTP) getTask(c *gin.Context) {
	out, err := h.Service.GetTask(c.Request.Context(), c.Param("task_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// renameTask 处理 PATCH /api/v2/agent-tasks/:task_id。
//
// 200 返回 TaskResponse。JSON {"title"}。标题非法 400；找不到 404。只改标题，不改 Goal 正文或 status。
func (h HTTP) renameTask(c *gin.Context) {
	var req struct {
		Title string `json:"title"`
	}
	if err := bindJSONStrict(c, &req); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	out, err := h.Service.RenameTask(c.Request.Context(), c.Param("task_id"), req.Title)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// cancelTask 处理 POST /api/v2/agent-tasks/:task_id/cancel。
//
// 200 返回 TaskResponse。已终态幂等。找不到 404。这是用户取消 Goal，可能连带取消 GraphRun / 阻塞中的 Turn。
func (h HTTP) cancelTask(c *gin.Context) {
	out, err := h.Service.CancelTask(c.Request.Context(), c.Param("task_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// pauseTask 处理 POST /api/v2/agent-tasks/:task_id/pause。
//
// 200 返回 TaskResponse。已暂停或已终态幂等。进行中的 harness Turn 未取消时 409。找不到 404。暂停不是完成 Goal。
func (h HTTP) pauseTask(c *gin.Context) {
	out, err := h.Service.PauseTask(c.Request.Context(), c.Param("task_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// completeTask 处理 POST /api/v2/agent-tasks/:task_id/complete。
//
// 200 返回 TaskResponse（status=succeeded）。已终态或仍有忙碌 Turn 409；找不到 404。Turn / GraphRun 成功不能代替本调用。
func (h HTTP) completeTask(c *gin.Context) {
	out, err := h.Service.CompleteTask(c.Request.Context(), c.Param("task_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// resumeTask 处理 POST /api/v2/agent-tasks/:task_id/resume。
//
// 200 返回 TaskResponse。非 paused 幂等返回当前行。找不到 404。不自动提交新 Turn。
func (h HTTP) resumeTask(c *gin.Context) {
	out, err := h.Service.ResumeTask(c.Request.Context(), c.Param("task_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// getWorkbench 处理 GET /api/v2/products/:product_id/agent-workbench。
//
// 200 返回 WorkbenchResponse。query agent_session_id / agent_task_id 可选。没有工作区 409；商品不存在 404。只读。
func (h HTTP) getWorkbench(c *gin.Context) {
	out, err := h.Service.GetWorkbench(c.Request.Context(), c.Param("product_id"), queryOpt(c, "agent_session_id"), queryOpt(c, "agent_task_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// ensureWorkbench 处理 POST /api/v2/products/:product_id/agent-workbench。
//
// 200 返回 WorkbenchResponse。必须带 Idempotency-Key，缺则 400。query agent_session_id 可选；new_session=true 强制新会话。
// 无 live 图 409；同键不同请求 409；商品/Session 不存在 404。不创建 Goal。
func (h HTTP) ensureWorkbench(c *gin.Context) {
	key := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if key == "" {
		httpx.AbortDetail(c, http.StatusBadRequest, "Idempotency-Key 不能为空")
		return
	}
	out, err := h.Service.EnsureWorkbench(c.Request.Context(), c.Param("product_id"), key, queryOpt(c, "agent_session_id"), queryBool(c, "new_session", false))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// getProductConversation 处理 GET /api/v2/products/:product_id/agent-conversations/:conversation_id。
//
// 200 返回 ConversationResponse。商品或对话不存在、对话不属于该商品 404。只读，不改 Turn / Goal。
func (h HTTP) getProductConversation(c *gin.Context) {
	pid := c.Param("product_id")
	out, err := h.Service.GetConversation(c.Request.Context(), &pid, c.Param("conversation_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// getProductRunRequest 处理 GET /api/v2/products/:product_id/agent-conversations/:conversation_id/workflow-run-request。
//
// 200 返回最新 WorkflowRunRequestResponse，没有请求时 body 为 null。对话不匹配 404。读路径 sync GraphRun 不得覆盖 goal_loop。
func (h HTTP) getProductRunRequest(c *gin.Context) {
	pid := c.Param("product_id")
	out, err := h.Service.GetWorkflowRunRequest(c.Request.Context(), &pid, c.Param("conversation_id"), nil)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// confirmProductRunRequest 处理 POST /api/v2/products/:product_id/agent-conversations/:conversation_id/workflow-run-request/:request_id/confirm。
//
// 200 返回 WorkflowRunRequestResponse。已确认回放。已取消 409（code=not_pending）。找不到 404。确认会经 graph 提交 GraphRun，不完成 Goal。
func (h HTTP) confirmProductRunRequest(c *gin.Context) {
	pid := c.Param("product_id")
	out, err := h.Service.ConfirmWorkflowRunRequest(c.Request.Context(), &pid, c.Param("conversation_id"), c.Param("request_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// cancelProductRunRequest 处理 POST /api/v2/products/:product_id/agent-conversations/:conversation_id/workflow-run-request/:request_id/cancel。
//
// 200 返回 WorkflowRunRequestResponse。找不到 404。取消执行请求不是取消 Goal。
func (h HTTP) cancelProductRunRequest(c *gin.Context) {
	pid := c.Param("product_id")
	out, err := h.Service.CancelWorkflowRunRequestHTTP(c.Request.Context(), &pid, c.Param("conversation_id"), c.Param("request_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// listProductTurns 处理 GET /api/v2/products/:product_id/agent-conversations/:conversation_id/turns。
//
// 转给 listTurns（productID 非空）。200 TurnPageResponse；after 不是页码。
func (h HTTP) listProductTurns(c *gin.Context) {
	h.listTurns(c, ptr(c.Param("product_id")))
}

// submitProductTurn 处理 POST /api/v2/products/:product_id/agent-conversations/:conversation_id/turns。
//
// 转给 submitTurn（productID 非空）。202 SubmitTurnResponse；网关未配置 503。
func (h HTTP) submitProductTurn(c *gin.Context) {
	h.submitTurn(c, ptr(c.Param("product_id")))
}

// getProductTurn 处理 GET /api/v2/products/:product_id/agent-conversations/:conversation_id/turns/:projection_id。
//
// 转给 getTurn（不要求 library draft 已清）。200 TurnResponse。找不到 404。
func (h HTTP) getProductTurn(c *gin.Context) {
	h.getTurn(c, ptr(c.Param("product_id")), false)
}

// cancelProductTurn 处理 POST /api/v2/products/:product_id/agent-conversations/:conversation_id/turns/:projection_id/cancel。
//
// 转给 controlTurn("cancel")。200 TurnResponse。取消 Turn 不完成也不取消 Goal。
func (h HTTP) cancelProductTurn(c *gin.Context) {
	h.controlTurn(c, ptr(c.Param("product_id")), "cancel")
}

// resumeProductTurn 处理 POST /api/v2/products/:product_id/agent-conversations/:conversation_id/turns/:projection_id/resume。
//
// 转给 controlTurn("resume")。200 TurnResponse。网关未配置 503。
func (h HTTP) resumeProductTurn(c *gin.Context) {
	h.controlTurn(c, ptr(c.Param("product_id")), "resume")
}

// answerProductQuestion 处理 POST /api/v2/products/:product_id/agent-conversations/:conversation_id/turns/:projection_id/questions/:question_id/answer。
//
// 转给 answerQuestion。200 QuestionAnswerResponse。必须且只能提供 option、text 或 skip。
func (h HTTP) answerProductQuestion(c *gin.Context) {
	h.answerQuestion(c, ptr(c.Param("product_id")))
}

// reconcileProductEffect 处理 POST /api/v2/products/:product_id/agent-conversations/:conversation_id/turns/:projection_id/effect-reconciliation。
//
// 转给 reconcileEffect。200 EffectReconciliationResponse。仅 unknown Turn。
func (h HTTP) reconcileProductEffect(c *gin.Context) {
	h.reconcileEffect(c, ptr(c.Param("product_id")))
}

// streamProductEvents 处理 GET /api/v2/products/:product_id/agent-conversations/:conversation_id/turns/:projection_id/events。
//
// 转给 streamEvents。200 text/event-stream，从 PostgreSQL journal 游标回放。after / Last-Event-ID 非法 400；连接过多 503。
func (h HTTP) streamProductEvents(c *gin.Context) {
	h.streamEvents(c, ptr(c.Param("product_id")))
}

// getLibraryDraft 处理 GET /api/v2/agent-conversations/:conversation_id/library-organization-draft。
//
// 200 返回全局素材整理 Draft。必须是 global conversation，否则校验失败；找不到 404。只读，确认才写图库。
func (h HTTP) getLibraryDraft(c *gin.Context) {
	out, err := h.Service.GetLibraryDraftHTTP(c.Request.Context(), c.Param("conversation_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// confirmLibraryDraft 处理 POST /api/v2/agent-conversations/:conversation_id/library-organization-draft/confirm。
//
// 200 返回确认后的 Draft。JSON expected_draft_version 与 idempotency_key；缺键或版本落后 400/409。未知字段 400。
func (h HTTP) confirmLibraryDraft(c *gin.Context) {
	var req struct {
		ExpectedDraftVersion int    `json:"expected_draft_version"`
		IdempotencyKey       string `json:"idempotency_key"`
	}
	if err := bindJSONStrict(c, &req); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	out, err := h.Service.ConfirmLibraryDraftHTTP(c.Request.Context(), c.Param("conversation_id"), req.ExpectedDraftVersion, req.IdempotencyKey)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// getGlobalRunRequest 处理 GET /api/v2/agent-conversations/:conversation_id/workflow-run-request。
//
// 200 返回最新 WorkflowRunRequestResponse，没有请求时 body 为 null。query task_id 可选。对话不存在 404。
func (h HTTP) getGlobalRunRequest(c *gin.Context) {
	out, err := h.Service.GetWorkflowRunRequest(c.Request.Context(), nil, c.Param("conversation_id"), queryOpt(c, "task_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// confirmGlobalRunRequest 处理 POST /api/v2/agent-conversations/:conversation_id/workflow-run-request/:request_id/confirm。
//
// 200 返回 WorkflowRunRequestResponse。已取消 409（code=not_pending）。找不到 404。确认 GraphRun 不完成 Goal。
func (h HTTP) confirmGlobalRunRequest(c *gin.Context) {
	out, err := h.Service.ConfirmWorkflowRunRequest(c.Request.Context(), nil, c.Param("conversation_id"), c.Param("request_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// cancelGlobalRunRequest 处理 POST /api/v2/agent-conversations/:conversation_id/workflow-run-request/:request_id/cancel。
//
// 200 返回 WorkflowRunRequestResponse。找不到 404。取消执行请求不是取消 Goal。
func (h HTTP) cancelGlobalRunRequest(c *gin.Context) {
	out, err := h.Service.CancelWorkflowRunRequestHTTP(c.Request.Context(), nil, c.Param("conversation_id"), c.Param("request_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// listGlobalTurns 处理 GET /api/v2/agent-conversations/:conversation_id/turns。
//
// 转给 listTurns（productID=nil）。200 TurnPageResponse；after 不是页码。
func (h HTTP) listGlobalTurns(c *gin.Context) {
	h.listTurns(c, nil)
}

// submitGlobalTurn 处理 POST /api/v2/agent-conversations/:conversation_id/turns。
//
// 转给 submitTurn（productID=nil）。202 SubmitTurnResponse；网关未配置 503。
func (h HTTP) submitGlobalTurn(c *gin.Context) {
	h.submitTurn(c, nil)
}

// getGlobalTurn 处理 GET /api/v2/agent-conversations/:conversation_id/turns/:projection_id。
//
// 转给 getTurn（requireLibraryClear=true：挂着素材整理 Draft 时不向 Pi 刷新）。200 TurnResponse。找不到 404。
func (h HTTP) getGlobalTurn(c *gin.Context) {
	h.getTurn(c, nil, true)
}

// cancelGlobalTurn 处理 POST /api/v2/agent-conversations/:conversation_id/turns/:projection_id/cancel。
//
// 转给 controlTurn("cancel")。200 TurnResponse。取消 Turn 不改 Goal。
func (h HTTP) cancelGlobalTurn(c *gin.Context) {
	h.controlTurn(c, nil, "cancel")
}

// resumeGlobalTurn 处理 POST /api/v2/agent-conversations/:conversation_id/turns/:projection_id/resume。
//
// 转给 controlTurn("resume")。200 TurnResponse。网关未配置 503。
func (h HTTP) resumeGlobalTurn(c *gin.Context) {
	h.controlTurn(c, nil, "resume")
}

// answerGlobalQuestion 处理 POST /api/v2/agent-conversations/:conversation_id/turns/:projection_id/questions/:question_id/answer。
//
// 转给 answerQuestion。200 QuestionAnswerResponse。必须且只能提供 option、text 或 skip。
func (h HTTP) answerGlobalQuestion(c *gin.Context) {
	h.answerQuestion(c, nil)
}

// reconcileGlobalEffect 处理 POST /api/v2/agent-conversations/:conversation_id/turns/:projection_id/effect-reconciliation。
//
// 转给 reconcileEffect。200 EffectReconciliationResponse。仅 unknown Turn。
func (h HTTP) reconcileGlobalEffect(c *gin.Context) {
	h.reconcileEffect(c, nil)
}

// streamGlobalEvents 处理 GET /api/v2/agent-conversations/:conversation_id/turns/:projection_id/events。
//
// 转给 streamEvents。200 text/event-stream，从 PostgreSQL journal 游标回放。after / Last-Event-ID 非法 400；连接过多 503。
func (h HTTP) streamGlobalEvents(c *gin.Context) {
	h.streamEvents(c, nil)
}

// listTurns 是商品/全局 Turn 列表的共用实现。
//
// 200 返回 TurnPageResponse。query：task_id 可选；after 是 opaque cursor 不是页码（须匹配当前 conversation）；limit 默认 20、上限 50。
// limit/cursor 非法 400；对话不匹配 404。只读投影，完整对话以 journal SSE 为准。
func (h HTTP) listTurns(c *gin.Context, productID *string) {
	limit, err := queryInt(c, "limit", turnDefaultPageSize, 1, turnMaxPageSize)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	out, err := h.Service.ListTurns(c.Request.Context(), productID, c.Param("conversation_id"), c.Query("task_id"), c.Query("after"), limit)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// submitTurn 是商品/全局提交 Turn 的共用实现。
//
// 202 返回 SubmitTurnResponse。JSON input_text / asset_ids / idempotency_key，task_id 与 page_context 可选。
// 网关未配置 503；未知字段或校验失败 400；对话不存在 404。Created=false 是幂等回放。不完成 Goal，也不 claim lease。
func (h HTTP) submitTurn(c *gin.Context, productID *string) {
	if !h.Service.GatewayConfigured() {
		httpx.AbortDetail(c, http.StatusServiceUnavailable, "Agent 服务尚未配置或暂时不可用")
		return
	}
	var req struct {
		InputText      string         `json:"input_text"`
		AssetIDs       []string       `json:"asset_ids"`
		IdempotencyKey string         `json:"idempotency_key"`
		TaskID         *string        `json:"task_id"`
		PageContext    map[string]any `json:"page_context"`
	}
	if err := bindJSONStrict(c, &req); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	out, err := h.Service.SubmitTurn(c.Request.Context(), productID, c.Param("conversation_id"), startTurnInput{
		InputText: req.InputText, AssetIDs: req.AssetIDs, IdempotencyKey: req.IdempotencyKey,
		TaskID: req.TaskID, PageContext: req.PageContext,
	})
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusAccepted, out)
}

// getTurn 是商品/全局读取 Turn 投影的共用实现。
//
// 200 返回 TurnResponse。找不到 404。进行中可能向 Gateway 刷新，失败回落 PostgreSQL。
// requireLibraryClear=true（全局路由）时，若仍挂着素材整理 Draft 则不向 Pi 刷新，避免和确认竞态。
func (h HTTP) getTurn(c *gin.Context, productID *string, requireLibraryClear bool) {
	out, err := h.Service.GetTurn(c.Request.Context(), productID, c.Param("conversation_id"), c.Param("projection_id"), requireLibraryClear)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// controlTurn 是商品/全局 cancel / resume Turn 的共用实现。
//
// 200 返回 TurnResponse。网关未配置 503；找不到 404；终态 resume 409。command 只允许 cancel 或 resume。
// 取消 Turn 不完成也不取消 Goal（商品 Goal 仍可能停在 goal_loop）。
func (h HTTP) controlTurn(c *gin.Context, productID *string, command string) {
	if !h.Service.GatewayConfigured() {
		httpx.AbortDetail(c, http.StatusServiceUnavailable, "Agent 服务尚未配置或暂时不可用")
		return
	}
	out, err := h.Service.ControlTurn(c.Request.Context(), productID, c.Param("conversation_id"), c.Param("projection_id"), command)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// answerQuestion 是商品/全局回答 question 的共用实现。
//
// 200 返回 QuestionAnswerResponse（已回答父 Turn + 续写 Turn）。JSON 必须且只能提供 option、text 或 skip，否则 400。
// 问题已过期 409（code=not_pending）。未知字段 400。回答问题不完成 Goal。
func (h HTTP) answerQuestion(c *gin.Context, productID *string) {
	var req struct {
		Option *int    `json:"option"`
		Text   *string `json:"text"`
		Skip   *bool   `json:"skip"`
	}
	if err := bindJSONStrict(c, &req); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	answer := map[string]any{}
	hasOption := req.Option != nil
	hasText := req.Text != nil && strings.TrimSpace(*req.Text) != ""
	hasSkip := req.Skip != nil && *req.Skip
	count := 0
	if hasOption {
		count++
	}
	if hasText {
		count++
	}
	if hasSkip {
		count++
	}
	if count != 1 {
		httpx.AbortDetail(c, http.StatusBadRequest, "回答必须且只能提供 option、text 或 skip")
		return
	}
	if hasSkip {
		answer["skip"] = true
	} else if hasOption {
		answer["option"] = *req.Option
	} else {
		answer["text"] = strings.TrimSpace(*req.Text)
	}
	out, err := h.Service.AnswerQuestion(c.Request.Context(), productID, c.Param("conversation_id"), c.Param("projection_id"), c.Param("question_id"), answer)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// reconcileEffect 是商品/全局对账 unknown Turn 副作用的共用实现。
//
// 200 返回 EffectReconciliationResponse。JSON {"tool_call_id"}。非 unknown 或找不到 intent 409；找不到 Turn 404。
// 对账不延长 lease、不改 Goal。
func (h HTTP) reconcileEffect(c *gin.Context, productID *string) {
	var req struct {
		ToolCallID string `json:"tool_call_id"`
	}
	if err := bindJSONStrict(c, &req); err != nil {
		httpx.AbortErr(c, err)
		return
	}
	out, err := h.Service.ReconcileTurnEffect(c.Request.Context(), productID, c.Param("conversation_id"), c.Param("projection_id"), req.ToolCallID)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// streamControlEvents 处理 GET /api/v2/agent-control/events。
//
// 200 text/event-stream，推 session/task/lease 控制面变更（不是 Turn journal）。只读通知 + 水位轮询，不写 Goal、不延长 lease。
func (h HTTP) streamControlEvents(c *gin.Context) {
	h.Service.StreamControlEvents(c)
}

// streamEvents 是商品/全局 Turn SSE 的共用实现。
//
// 200 text/event-stream，从 PostgreSQL agent_turn_events 按游标回放。query after 与 Header Last-Event-ID 取较大者，都不是页码。
// after / Last-Event-ID 非法 400；连接数超限 503；Turn 不存在 404。agent-service 不要打这条。
func (h HTTP) streamEvents(c *gin.Context, productID *string) {
	after := 0
	if raw := strings.TrimSpace(c.Query("after")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 || parsed > maxEventSequence {
			httpx.AbortDetail(c, http.StatusBadRequest, "Last-Event-ID 无效")
			return
		}
		after = parsed
	}
	h.Service.StreamTurnEvents(c, productID, c.Param("conversation_id"), c.Param("projection_id"), after, c.GetHeader("Last-Event-ID"))
}

// pageProductEvents 处理 GET /api/v2/products/:product_id/agent-conversations/:conversation_id/turns/:projection_id/events/page。
//
// 转给 pageEvents。200 历史页；after 是事件 sequence 游标不是页码。
func (h HTTP) pageProductEvents(c *gin.Context) {
	productID := c.Param("product_id")
	h.pageEvents(c, &productID)
}

// pageGlobalEvents 处理 GET /api/v2/agent-conversations/:conversation_id/turns/:projection_id/events/page。
//
// 转给 pageEvents（productID=nil）。200 历史页；after 是事件 sequence 游标不是页码。
func (h HTTP) pageGlobalEvents(c *gin.Context) { h.pageEvents(c, nil) }

// pageEvents 是商品/全局 Turn 事件历史页的共用实现。
//
// 200 返回 {items, next_after, has_more, stream_state}。query after 默认 0（sequence 游标不是页码）；limit 默认 250、上限 250。
// 非法 400；Turn 不存在 404。只读 journal，不延长 lease。
func (h HTTP) pageEvents(c *gin.Context, productID *string) {
	after, err := queryInt(c, "after", 0, 0, maxEventSequence)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	limit, err := queryInt(c, "limit", 250, 1, 250)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	out, err := h.Service.ListProjectedEventPage(c.Request.Context(), productID, c.Param("conversation_id"), c.Param("projection_id"), after, limit)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// queryBool 把 1/true/yes 当 true；空则用 def。非法值当 false 且不报错——调用方不要靠它做门闩校验。
func queryBool(c *gin.Context, key string, def bool) bool {
	raw := strings.TrimSpace(strings.ToLower(c.Query(key)))
	if raw == "" {
		return def
	}
	return raw == "true" || raw == "1" || raw == "yes"
}

// queryOpt 读非空查询参数；空串返回 nil，用来区分「没传」和「传了空」。
func queryOpt(c *gin.Context, key string) *string {
	raw := strings.TrimSpace(c.Query(key))
	if raw == "" {
		return nil
	}
	return &raw
}

// queryInt 读查询整数；缺省 def。越界或非数字 400，不要夹紧。
func queryInt(c *gin.Context, key string, def, min, max int) (int, error) {
	raw := strings.TrimSpace(c.Query(key))
	if raw == "" {
		return def, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < min || n > max {
		return 0, apperr.Validation("请求参数无效")
	}
	return n, nil
}

// bindJSONStrict 用 DisallowUnknownFields 解码 JSON。多字段或尾随内容一律 400「请求体无效」。内部与浏览器路由共用。
func bindJSONStrict(c *gin.Context, dest any) error {
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return apperr.Validation("请求体无效")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dest); err != nil {
		return apperr.Validation("请求体无效")
	}
	if dec.More() {
		return apperr.Validation("请求体无效")
	}
	return nil
}

// requireIdempotency 读取 Idempotency-Key 头。空则直接 400 并 Abort，调用方看到 false 必须立刻 return。
func requireIdempotency(c *gin.Context) (string, bool) {
	key := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if key == "" {
		httpx.AbortDetail(c, http.StatusBadRequest, "Idempotency-Key 不能为空")
		return "", false
	}
	return key, true
}
