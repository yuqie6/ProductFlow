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

type HTTP struct {
	Service  Service
	Settings interface {
		settings.RuntimeReader
	}
	InternalToken string
}

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
	v2 := engine.Group("/api/v2", admin)
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

func (h HTTP) createSession(c *gin.Context) {
	out, err := h.Service.CreateSession(c.Request.Context())
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, out)
}

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

func (h HTTP) archiveSession(c *gin.Context) {
	out, err := h.Service.ArchiveSession(c.Request.Context(), c.Param("session_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

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

func (h HTTP) getTask(c *gin.Context) {
	out, err := h.Service.GetTask(c.Request.Context(), c.Param("task_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

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

func (h HTTP) cancelTask(c *gin.Context) {
	out, err := h.Service.CancelTask(c.Request.Context(), c.Param("task_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h HTTP) pauseTask(c *gin.Context) {
	out, err := h.Service.PauseTask(c.Request.Context(), c.Param("task_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h HTTP) completeTask(c *gin.Context) {
	out, err := h.Service.CompleteTask(c.Request.Context(), c.Param("task_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h HTTP) resumeTask(c *gin.Context) {
	out, err := h.Service.ResumeTask(c.Request.Context(), c.Param("task_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h HTTP) getWorkbench(c *gin.Context) {
	out, err := h.Service.GetWorkbench(c.Request.Context(), c.Param("product_id"), queryOpt(c, "agent_session_id"), queryOpt(c, "agent_task_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

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

func (h HTTP) getProductConversation(c *gin.Context) {
	pid := c.Param("product_id")
	out, err := h.Service.GetConversation(c.Request.Context(), &pid, c.Param("conversation_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h HTTP) getProductRunRequest(c *gin.Context) {
	pid := c.Param("product_id")
	out, err := h.Service.GetWorkflowRunRequest(c.Request.Context(), &pid, c.Param("conversation_id"), nil)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h HTTP) confirmProductRunRequest(c *gin.Context) {
	pid := c.Param("product_id")
	out, err := h.Service.ConfirmWorkflowRunRequest(c.Request.Context(), &pid, c.Param("conversation_id"), c.Param("request_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h HTTP) cancelProductRunRequest(c *gin.Context) {
	pid := c.Param("product_id")
	out, err := h.Service.CancelWorkflowRunRequestHTTP(c.Request.Context(), &pid, c.Param("conversation_id"), c.Param("request_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h HTTP) listProductTurns(c *gin.Context) {
	h.listTurns(c, ptr(c.Param("product_id")))
}

func (h HTTP) submitProductTurn(c *gin.Context) {
	h.submitTurn(c, ptr(c.Param("product_id")))
}

func (h HTTP) getProductTurn(c *gin.Context) {
	h.getTurn(c, ptr(c.Param("product_id")), false)
}

func (h HTTP) cancelProductTurn(c *gin.Context) {
	h.controlTurn(c, ptr(c.Param("product_id")), "cancel")
}

func (h HTTP) resumeProductTurn(c *gin.Context) {
	h.controlTurn(c, ptr(c.Param("product_id")), "resume")
}

func (h HTTP) answerProductQuestion(c *gin.Context) {
	h.answerQuestion(c, ptr(c.Param("product_id")))
}

func (h HTTP) reconcileProductEffect(c *gin.Context) {
	h.reconcileEffect(c, ptr(c.Param("product_id")))
}

func (h HTTP) streamProductEvents(c *gin.Context) {
	h.streamEvents(c, ptr(c.Param("product_id")))
}

func (h HTTP) getLibraryDraft(c *gin.Context) {
	out, err := h.Service.GetLibraryDraftHTTP(c.Request.Context(), c.Param("conversation_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

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

func (h HTTP) getGlobalRunRequest(c *gin.Context) {
	out, err := h.Service.GetWorkflowRunRequest(c.Request.Context(), nil, c.Param("conversation_id"), queryOpt(c, "task_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h HTTP) confirmGlobalRunRequest(c *gin.Context) {
	out, err := h.Service.ConfirmWorkflowRunRequest(c.Request.Context(), nil, c.Param("conversation_id"), c.Param("request_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h HTTP) cancelGlobalRunRequest(c *gin.Context) {
	out, err := h.Service.CancelWorkflowRunRequestHTTP(c.Request.Context(), nil, c.Param("conversation_id"), c.Param("request_id"))
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h HTTP) listGlobalTurns(c *gin.Context) {
	h.listTurns(c, nil)
}

func (h HTTP) submitGlobalTurn(c *gin.Context) {
	h.submitTurn(c, nil)
}

func (h HTTP) getGlobalTurn(c *gin.Context) {
	h.getTurn(c, nil, true)
}

func (h HTTP) cancelGlobalTurn(c *gin.Context) {
	h.controlTurn(c, nil, "cancel")
}

func (h HTTP) resumeGlobalTurn(c *gin.Context) {
	h.controlTurn(c, nil, "resume")
}

func (h HTTP) answerGlobalQuestion(c *gin.Context) {
	h.answerQuestion(c, nil)
}

func (h HTTP) reconcileGlobalEffect(c *gin.Context) {
	h.reconcileEffect(c, nil)
}

func (h HTTP) streamGlobalEvents(c *gin.Context) {
	h.streamEvents(c, nil)
}

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

func (h HTTP) getTurn(c *gin.Context, productID *string, requireLibraryClear bool) {
	out, err := h.Service.GetTurn(c.Request.Context(), productID, c.Param("conversation_id"), c.Param("projection_id"), requireLibraryClear)
	if err != nil {
		httpx.AbortErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

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

func (h HTTP) streamControlEvents(c *gin.Context) {
	h.Service.StreamControlEvents(c)
}

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

func (h HTTP) pageProductEvents(c *gin.Context) {
	productID := c.Param("product_id")
	h.pageEvents(c, &productID)
}

func (h HTTP) pageGlobalEvents(c *gin.Context) { h.pageEvents(c, nil) }

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

func queryBool(c *gin.Context, key string, def bool) bool {
	raw := strings.TrimSpace(strings.ToLower(c.Query(key)))
	if raw == "" {
		return def
	}
	return raw == "true" || raw == "1" || raw == "yes"
}

func queryOpt(c *gin.Context, key string) *string {
	raw := strings.TrimSpace(c.Query(key))
	if raw == "" {
		return nil
	}
	return &raw
}

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

func requireIdempotency(c *gin.Context) (string, bool) {
	key := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if key == "" {
		httpx.AbortDetail(c, http.StatusBadRequest, "Idempotency-Key 不能为空")
		return "", false
	}
	return key, true
}
