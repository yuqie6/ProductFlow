package app

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/yuqie6/agent-harness/agenttask"
	"github.com/yuqie6/productflow-agent-service/internal/config"
	"github.com/yuqie6/productflow-agent-service/internal/productflow"
)

const (
	maxStartAssets        = 6
	maxPageContextBytes   = 32 << 10
	maxPageContextIDs     = 100
	maxPageContextFilters = 20
)

type Server struct {
	manager     *Manager
	internalKey string
	maxBody     int64
	mux         *http.ServeMux
}

type startTurnRequest struct {
	InputText      string       `json:"input_text"`
	AssetIDs       []string     `json:"asset_ids,omitempty"`
	IdempotencyKey string       `json:"idempotency_key"`
	PageContext    *pageContext `json:"page_context,omitempty"`
}

type pageContext struct {
	SnapshotID       string            `json:"snapshot_id"`
	Route            string            `json:"route"`
	PageType         string            `json:"page_type"`
	ProductID        *string           `json:"product_id"`
	WorkflowID       *string           `json:"workflow_id"`
	SelectedAssetIDs []string          `json:"selected_asset_ids"`
	VisibleAssetIDs  []string          `json:"visible_asset_ids"`
	Filters          map[string]string `json:"filters"`
	WorkflowRevision *int              `json:"workflow_revision"`
	LibraryRevision  *int              `json:"library_revision"`
	Digest           string            `json:"digest"`
	CapturedAt       string            `json:"captured_at"`
}

func NewServer(manager *Manager, internalKey string, maxBody int64) (*Server, error) {
	if manager == nil || len(strings.TrimSpace(internalKey)) < 32 || maxBody <= 0 {
		return nil, errors.New("agent server requires manager, internal token, and positive body limit")
	}
	server := &Server{manager: manager, internalKey: internalKey, maxBody: maxBody, mux: http.NewServeMux()}
	server.routes()
	return server, nil
}

func (server *Server) Handler() http.Handler { return server.mux }

func (server *Server) routes() {
	server.mux.HandleFunc("GET /healthz", server.health)
	server.mux.Handle("POST /internal/v1/conversations/{conversation_id}/turns", server.requireInternal(http.HandlerFunc(server.startTurn)))
	server.mux.Handle("GET /internal/v1/conversations/{conversation_id}/turns/{turn_id}", server.requireInternal(http.HandlerFunc(server.delegate)))
	server.mux.Handle("POST /internal/v1/conversations/{conversation_id}/turns/{turn_id}/cancel", server.requireInternal(http.HandlerFunc(server.delegate)))
	server.mux.Handle("POST /internal/v1/conversations/{conversation_id}/turns/{turn_id}/resume", server.requireInternal(http.HandlerFunc(server.delegate)))
	server.mux.Handle("POST /internal/v1/conversations/{conversation_id}/turns/{turn_id}/questions/{question_id}/answer", server.requireInternal(http.HandlerFunc(server.delegate)))
	server.mux.Handle("GET /internal/v1/conversations/{conversation_id}/turns/{turn_id}/events", server.requireInternal(http.HandlerFunc(server.delegate)))
	server.mux.Handle("POST /internal/v1/tasks/{task_id}/turns", server.requireInternal(http.HandlerFunc(server.startTaskTurn)))
	server.mux.Handle("GET /internal/v1/tasks/{task_id}/turns/{turn_id}", server.requireInternal(http.HandlerFunc(server.delegate)))
	server.mux.Handle("POST /internal/v1/tasks/{task_id}/turns/{turn_id}/cancel", server.requireInternal(http.HandlerFunc(server.delegate)))
	server.mux.Handle("POST /internal/v1/tasks/{task_id}/turns/{turn_id}/resume", server.requireInternal(http.HandlerFunc(server.delegate)))
	server.mux.Handle("POST /internal/v1/tasks/{task_id}/turns/{turn_id}/questions/{question_id}/answer", server.requireInternal(http.HandlerFunc(server.delegate)))
	server.mux.Handle("GET /internal/v1/tasks/{task_id}/turns/{turn_id}/events", server.requireInternal(http.HandlerFunc(server.delegate)))
}

func (server *Server) health(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]any{
		"status": "ok", "harness_commit": config.HarnessCommit, "api_version": agenttask.APIVersion,
	})
}

func (server *Server) requireInternal(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		provided, hasBearerScheme := strings.CutPrefix(request.Header.Get("Authorization"), "Bearer ")
		if !hasBearerScheme || len(provided) != len(server.internalKey) || subtle.ConstantTimeCompare([]byte(provided), []byte(server.internalKey)) != 1 {
			writeError(writer, http.StatusUnauthorized, "unauthorized", "internal service authentication failed")
			return
		}
		next.ServeHTTP(writer, request)
	})
}

func (server *Server) startTurn(writer http.ResponseWriter, request *http.Request) {
	entry, err := server.manager.Get(request.Context(), request.PathValue("conversation_id"))
	if err != nil {
		writeMappedError(writer, err)
		return
	}
	server.startTurnForEntry(writer, request, entry)
}

func (server *Server) startTaskTurn(writer http.ResponseWriter, request *http.Request) {
	entry, err := server.manager.GetTask(request.Context(), request.PathValue("task_id"))
	if err != nil {
		writeMappedError(writer, err)
		return
	}
	server.startTurnForEntry(writer, request, entry)
}

func (server *Server) startTurnForEntry(writer http.ResponseWriter, request *http.Request, entry *ConversationService) {
	var body startTurnRequest
	if !server.decodeJSON(writer, request, &body) {
		return
	}
	body.InputText = strings.TrimSpace(body.InputText)
	body.IdempotencyKey = strings.TrimSpace(body.IdempotencyKey)
	if body.InputText == "" || body.IdempotencyKey == "" || len(body.IdempotencyKey) > 200 {
		writeError(writer, http.StatusBadRequest, "invalid_argument", "input_text and an idempotency_key of at most 200 bytes are required")
		return
	}
	assetIDs, err := uniqueStartAssetIDs(body.AssetIDs)
	if err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_argument", err.Error())
		return
	}
	input, err := server.turnInput(request, entry, body.InputText, assetIDs, body.PageContext)
	if err != nil {
		writeMappedError(writer, err)
		return
	}
	state, err := entry.Service.StartTurn(request.Context(), agenttask.StartTurnRequest{
		RunID: entry.Scope.RunID, Input: input, IdempotencyKey: body.IdempotencyKey,
	})
	if err != nil {
		writeMappedError(writer, err)
		return
	}
	writer.Header().Set("Location", fmt.Sprintf(
		"%s/turns/%s", server.executionPath(entry), url.PathEscape(state.TurnID),
	))
	writeJSON(writer, http.StatusAccepted, state)
}

func (server *Server) turnInput(
	request *http.Request,
	entry *ConversationService,
	inputText string,
	assetIDs []string,
	contextSnapshot *pageContext,
) (agenttask.TurnInput, error) {
	content := []agenttask.InputContent{{Type: agenttask.ContentInputText, Text: inputText}}
	if contextSnapshot != nil {
		if err := contextSnapshot.validate(); err != nil {
			return agenttask.TurnInput{}, err
		}
		encoded, err := json.Marshal(contextSnapshot)
		if err != nil {
			return agenttask.TurnInput{}, err
		}
		if len(encoded) > maxPageContextBytes {
			return agenttask.TurnInput{}, errors.New("page_context exceeds the Agent Turn context limit")
		}
		content = append(content, agenttask.InputContent{
			Type: agenttask.ContentInputText,
			Text: "ProductFlow 页面上下文快照（仅用于理解用户指代；执行前必须重新读取业务事实）：" + string(encoded),
		})
	}
	var totalBytes int64
	for _, assetID := range assetIDs {
		image, err := server.manager.config.ProductFlow.AssetContent(request.Context(), entry.Scope.ConversationID, assetID)
		if err != nil {
			return agenttask.TurnInput{}, err
		}
		totalBytes += image.SizeBytes
		if totalBytes > agenttask.MaxTotalImageBytes {
			return agenttask.TurnInput{}, errors.New("selected assets exceed the Turn image byte limit")
		}
		content = append(content,
			agenttask.InputContent{Type: agenttask.ContentInputText, Text: "Product reference asset ID: " + assetID},
			agenttask.InputContent{Type: agenttask.ContentInputImage, Image: &agenttask.InputImage{
				Data: image.Data, MediaType: image.MediaType, SizeBytes: image.SizeBytes,
				Detail: agenttask.ImageDetailHigh, CheckpointMode: agenttask.ImageCheckpointEmbed,
			}},
		)
	}
	input := agenttask.TurnInput{SchemaVersion: agenttask.TurnInputSchemaVersion, Content: content}
	if err := input.Validate(); err != nil {
		return agenttask.TurnInput{}, err
	}
	return input, nil
}

func (server *Server) delegate(writer http.ResponseWriter, request *http.Request) {
	taskID := request.PathValue("task_id")
	conversationID := request.PathValue("conversation_id")
	var entry *ConversationService
	var err error
	if taskID != "" {
		entry, err = server.manager.GetTask(request.Context(), taskID)
	} else {
		entry, err = server.manager.Get(request.Context(), conversationID)
	}
	if err != nil {
		writeMappedError(writer, err)
		return
	}
	turnID := request.PathValue("turn_id")
	suffix := "/v1alpha1/runs/" + url.PathEscape(entry.Scope.RunID) + "/turns/" + url.PathEscape(turnID)
	path := request.URL.Path
	switch {
	case strings.HasSuffix(path, "/cancel"):
		suffix += "/cancel"
	case strings.HasSuffix(path, "/resume"):
		suffix += "/resume"
	case strings.HasSuffix(path, "/events"):
		suffix += "/events"
	case strings.Contains(path, "/questions/") && strings.HasSuffix(path, "/answer"):
		suffix += "/questions/" + url.PathEscape(request.PathValue("question_id")) + "/answer"
	}
	clone := request.Clone(request.Context())
	clonedURL := *request.URL
	clonedURL.Path = suffix
	clone.URL = &clonedURL
	clone.RequestURI = ""
	entry.Handler.ServeHTTP(writer, clone)
}

func (server *Server) executionPath(entry *ConversationService) string {
	if entry.Scope.TaskID != "" {
		return "/internal/v1/tasks/" + url.PathEscape(entry.Scope.TaskID)
	}
	return "/internal/v1/conversations/" + url.PathEscape(entry.Scope.ConversationID)
}

func (contextSnapshot *pageContext) validate() error {
	if contextSnapshot == nil {
		return nil
	}
	if strings.TrimSpace(contextSnapshot.SnapshotID) == "" || len(contextSnapshot.SnapshotID) > 64 {
		return errors.New("page_context.snapshot_id is required")
	}
	if strings.TrimSpace(contextSnapshot.Route) == "" || len(contextSnapshot.Route) > 512 ||
		strings.TrimSpace(contextSnapshot.PageType) == "" || len(contextSnapshot.PageType) > 80 {
		return errors.New("page_context route and page_type are invalid")
	}
	if len(contextSnapshot.SelectedAssetIDs) > maxPageContextIDs || len(contextSnapshot.VisibleAssetIDs) > maxPageContextIDs {
		return errors.New("page_context asset IDs exceed the limit")
	}
	if len(contextSnapshot.Filters) > maxPageContextFilters || strings.TrimSpace(contextSnapshot.CapturedAt) == "" {
		return errors.New("page_context filters or captured_at are invalid")
	}
	if len(contextSnapshot.Digest) != 64 {
		return errors.New("page_context.digest is invalid")
	}
	return nil
}

func (server *Server) decodeJSON(writer http.ResponseWriter, request *http.Request, target any) bool {
	if mediaType := strings.ToLower(strings.TrimSpace(strings.Split(request.Header.Get("Content-Type"), ";")[0])); mediaType != "application/json" {
		writeError(writer, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json")
		return false
	}
	request.Body = http.MaxBytesReader(writer, request.Body, server.maxBody)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		status := http.StatusBadRequest
		code := "invalid_json"
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			status, code = http.StatusRequestEntityTooLarge, "body_too_large"
		}
		writeError(writer, status, code, err.Error())
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(writer, http.StatusBadRequest, "invalid_json", "request body must contain exactly one JSON value")
		return false
	}
	return true
}

func uniqueStartAssetIDs(values []string) ([]string, error) {
	if len(values) > maxStartAssets {
		return nil, fmt.Errorf("asset_ids cannot contain more than %d values", maxStartAssets)
	}
	seen := make(map[string]bool, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, errors.New("asset_ids cannot contain empty values")
		}
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result, nil
}

func writeMappedError(writer http.ResponseWriter, err error) {
	var httpErr *productflow.HTTPError
	if errors.As(err, &httpErr) {
		status := httpErr.StatusCode
		if status < 400 || status > 599 {
			status = http.StatusBadGateway
		}
		writeError(writer, status, "productflow_"+strings.TrimSpace(httpErr.Code), httpErr.Message)
		return
	}
	if strings.Contains(strings.ToLower(err.Error()), "conflict") {
		writeError(writer, http.StatusConflict, "conflict", err.Error())
		return
	}
	writeError(writer, http.StatusBadGateway, "agent_service_error", err.Error())
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func writeError(writer http.ResponseWriter, status int, code, message string) {
	writeJSON(writer, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}
