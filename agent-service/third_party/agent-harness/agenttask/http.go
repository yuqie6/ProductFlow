package agenttask

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/yuqie6/agent-harness/durable"
	turnprotocol "github.com/yuqie6/agent-harness/turn"
)

const (
	HTTPAPIPrefix           = "/v1alpha1"
	DefaultHTTPMaxBodyBytes = int64(96 << 20)
)

type HTTPOptions struct {
	EventPollInterval time.Duration
	HeartbeatInterval time.Duration
	MaxBodyBytes      int64
}

type HTTPStartTurnRequest struct {
	Input          TurnInput `json:"input"`
	IdempotencyKey string    `json:"idempotency_key"`
}

type HTTPAnswerQuestionRequest struct {
	Answer QuestionAnswer `json:"answer"`
}

type serviceHTTPHandler struct {
	service           *Service
	pollInterval      time.Duration
	heartbeatInterval time.Duration
	maxBodyBytes      int64
	mux               *http.ServeMux
}

// NewHTTPHandler exposes the v1alpha1 Turn control plane and replayable SSE
// stream. Authentication, authorization, CORS, and request identity remain the
// embedding application's middleware responsibility.
func NewHTTPHandler(service *Service, options HTTPOptions) (http.Handler, error) {
	if err := service.validate(); err != nil {
		return nil, err
	}
	if options.EventPollInterval == 0 {
		options.EventPollInterval = 100 * time.Millisecond
	}
	if options.HeartbeatInterval == 0 {
		options.HeartbeatInterval = 15 * time.Second
	}
	if options.MaxBodyBytes == 0 {
		options.MaxBodyBytes = DefaultHTTPMaxBodyBytes
	}
	if options.EventPollInterval < time.Millisecond {
		return nil, errors.New("agenttask HTTP event poll interval must be at least 1ms")
	}
	if options.HeartbeatInterval < options.EventPollInterval {
		return nil, errors.New("agenttask HTTP heartbeat interval must not be shorter than event poll interval")
	}
	if options.MaxBodyBytes < 1 {
		return nil, errors.New("agenttask HTTP max body bytes must be positive")
	}
	handler := &serviceHTTPHandler{
		service: service, pollInterval: options.EventPollInterval,
		heartbeatInterval: options.HeartbeatInterval, maxBodyBytes: options.MaxBodyBytes,
		mux: http.NewServeMux(),
	}
	handler.routes()
	return handler, nil
}

func (h *serviceHTTPHandler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	h.mux.ServeHTTP(writer, request)
}

func (h *serviceHTTPHandler) routes() {
	h.mux.HandleFunc("POST "+HTTPAPIPrefix+"/runs/{run_id}/turns", h.startTurn)
	h.mux.HandleFunc("GET "+HTTPAPIPrefix+"/runs/{run_id}/turns/{turn_id}", h.getTurn)
	h.mux.HandleFunc("POST "+HTTPAPIPrefix+"/runs/{run_id}/turns/{turn_id}/cancel", h.cancelTurn)
	h.mux.HandleFunc("POST "+HTTPAPIPrefix+"/runs/{run_id}/turns/{turn_id}/resume", h.resumeTurn)
	h.mux.HandleFunc("POST "+HTTPAPIPrefix+"/runs/{run_id}/turns/{turn_id}/questions/{question_id}/answer", h.answerQuestion)
	h.mux.HandleFunc("GET "+HTTPAPIPrefix+"/runs/{run_id}/turns/{turn_id}/events", h.streamEvents)
}

func (h *serviceHTTPHandler) startTurn(writer http.ResponseWriter, request *http.Request) {
	runID := request.PathValue("run_id")
	if !controlID.MatchString(runID) {
		writeHTTPError(writer, http.StatusBadRequest, "invalid_argument", "invalid run_id")
		return
	}
	var body HTTPStartTurnRequest
	if !h.decodeJSON(writer, request, &body) {
		return
	}
	if strings.TrimSpace(body.IdempotencyKey) == "" || len(strings.TrimSpace(body.IdempotencyKey)) > 200 {
		writeHTTPError(writer, http.StatusBadRequest, "invalid_argument", "idempotency_key must be between 1 and 200 bytes")
		return
	}
	if err := body.Input.Validate(); err != nil {
		writeHTTPError(writer, http.StatusBadRequest, "invalid_argument", err.Error())
		return
	}
	state, err := h.service.StartTurn(request.Context(), StartTurnRequest{
		RunID: runID, Input: body.Input, IdempotencyKey: body.IdempotencyKey,
	})
	if err != nil {
		writeServiceError(writer, err)
		return
	}
	writer.Header().Set("Location", turnPath(state.RunID, state.TurnID))
	writeJSON(writer, http.StatusAccepted, state)
}

func (h *serviceHTTPHandler) getTurn(writer http.ResponseWriter, request *http.Request) {
	runID, turnID, ok := httpTurnIDs(writer, request)
	if !ok {
		return
	}
	state, err := h.service.GetTurn(request.Context(), runID, turnID)
	if err != nil {
		writeServiceError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, state)
}

func (h *serviceHTTPHandler) cancelTurn(writer http.ResponseWriter, request *http.Request) {
	runID, turnID, ok := httpTurnIDs(writer, request)
	if !ok {
		return
	}
	state, err := h.service.CancelTurn(request.Context(), runID, turnID)
	if err != nil {
		writeServiceError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, state)
}

func (h *serviceHTTPHandler) resumeTurn(writer http.ResponseWriter, request *http.Request) {
	runID, turnID, ok := httpTurnIDs(writer, request)
	if !ok {
		return
	}
	state, err := h.service.ResumeTurn(request.Context(), runID, turnID)
	if err != nil {
		writeServiceError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, state)
}

func (h *serviceHTTPHandler) answerQuestion(writer http.ResponseWriter, request *http.Request) {
	runID, turnID, ok := httpTurnIDs(writer, request)
	if !ok {
		return
	}
	var body HTTPAnswerQuestionRequest
	if !h.decodeJSON(writer, request, &body) {
		return
	}
	if _, err := normalizeQuestionAnswer(body.Answer); err != nil {
		writeHTTPError(writer, http.StatusBadRequest, "invalid_argument", err.Error())
		return
	}
	if body.Answer.Option != nil {
		state, err := h.service.GetTurn(request.Context(), runID, turnID)
		if err != nil {
			writeServiceError(writer, err)
			return
		}
		if state.Question != nil && *body.Answer.Option >= len(state.Question.Options) {
			writeHTTPError(writer, http.StatusBadRequest, "invalid_argument", "question option index out of range")
			return
		}
	}
	state, err := h.service.AnswerQuestion(
		request.Context(), runID, turnID,
		request.PathValue("question_id"), body.Answer,
	)
	if err != nil {
		writeServiceError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, state)
}

func (h *serviceHTTPHandler) streamEvents(writer http.ResponseWriter, request *http.Request) {
	runID, turnID, ok := httpTurnIDs(writer, request)
	if !ok {
		return
	}
	cursor, err := eventCursor(request)
	if err != nil {
		writeHTTPError(writer, http.StatusBadRequest, "invalid_cursor", err.Error())
		return
	}
	if _, err := h.service.GetTurn(request.Context(), runID, turnID); err != nil {
		writeServiceError(writer, err)
		return
	}
	flusher, ok := writer.(http.Flusher)
	if !ok {
		writeHTTPError(writer, http.StatusInternalServerError, "stream_unsupported", "HTTP writer does not support streaming")
		return
	}
	writer.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-cache, no-transform")
	writer.Header().Set("X-Accel-Buffering", "no")
	writer.WriteHeader(http.StatusOK)
	flusher.Flush()

	lastWrite := time.Now()
	writeEvents := func(events []TurnEvent) bool {
		for _, event := range events {
			encoded, err := json.Marshal(event)
			if err != nil {
				writeSSEError(writer, err)
				flusher.Flush()
				return false
			}
			if _, err := fmt.Fprintf(writer, "id: %d\nevent: %s\ndata: %s\n\n", event.Sequence, event.Kind, encoded); err != nil {
				return false
			}
			cursor = event.Sequence
			lastWrite = time.Now()
		}
		if len(events) > 0 {
			flusher.Flush()
		}
		return true
	}
	for {
		events, err := h.service.Events(request.Context(), runID, turnID, cursor)
		if err != nil {
			writeSSEError(writer, err)
			flusher.Flush()
			return
		}
		if !writeEvents(events) {
			return
		}
		state, err := h.service.GetTurn(request.Context(), runID, turnID)
		if err != nil {
			writeSSEError(writer, err)
			flusher.Flush()
			return
		}
		if terminalTurnStatus(state.Status) {
			// The terminal state and its final events commit atomically, but that
			// commit can land between the event query above and GetTurn. Drain every
			// remaining public page so a clean EOF means all terminal events were
			// delivered. An empty page is authoritative even when legacy journal
			// rows remain after the public cursor because the store filters them.
			for {
				finalEvents, err := h.service.Events(request.Context(), runID, turnID, cursor)
				if err != nil {
					writeSSEError(writer, err)
					flusher.Flush()
					return
				}
				if len(finalEvents) == 0 {
					return
				}
				if !writeEvents(finalEvents) {
					return
				}
			}
		}
		if time.Since(lastWrite) >= h.heartbeatInterval {
			if _, err := fmt.Fprintf(writer, ": keep-alive %d\n\n", time.Now().UTC().Unix()); err != nil {
				return
			}
			lastWrite = time.Now()
			flusher.Flush()
		}
		timer := time.NewTimer(h.pollInterval)
		select {
		case <-request.Context().Done():
			if !timer.Stop() {
				<-timer.C
			}
			return
		case <-timer.C:
		}
	}
}

func httpTurnIDs(writer http.ResponseWriter, request *http.Request) (string, string, bool) {
	runID, turnID := request.PathValue("run_id"), request.PathValue("turn_id")
	if !controlID.MatchString(runID) {
		writeHTTPError(writer, http.StatusBadRequest, "invalid_argument", "invalid run_id")
		return "", "", false
	}
	if !controlID.MatchString(turnID) {
		writeHTTPError(writer, http.StatusBadRequest, "invalid_argument", "invalid turn_id")
		return "", "", false
	}
	return runID, turnID, true
}

func (h *serviceHTTPHandler) decodeJSON(writer http.ResponseWriter, request *http.Request, target any) bool {
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeHTTPError(writer, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json")
		return false
	}
	request.Body = http.MaxBytesReader(writer, request.Body, h.maxBodyBytes)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		status := http.StatusBadRequest
		code := "invalid_json"
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			status = http.StatusRequestEntityTooLarge
			code = "body_too_large"
		}
		writeHTTPError(writer, status, code, err.Error())
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeHTTPError(writer, http.StatusBadRequest, "invalid_json", "request body must contain exactly one JSON value")
		return false
	}
	return true
}

func eventCursor(request *http.Request) (int64, error) {
	queryValue := strings.TrimSpace(request.URL.Query().Get("after"))
	headerValue := strings.TrimSpace(request.Header.Get("Last-Event-ID"))
	if queryValue != "" && headerValue != "" && queryValue != headerValue {
		return 0, errors.New("after and Last-Event-ID cursors conflict")
	}
	value := queryValue
	if value == "" {
		value = headerValue
	}
	if value == "" {
		return 0, nil
	}
	cursor, err := strconv.ParseInt(value, 10, 64)
	if err != nil || cursor < 0 {
		return 0, errors.New("event cursor must be a non-negative integer")
	}
	return cursor, nil
}

func terminalTurnStatus(status TurnStatus) bool {
	switch status {
	case TurnAwaitingConfirmation, TurnSucceeded, TurnFailed, TurnCanceled, TurnUnknown:
		return true
	default:
		return false
	}
}

func turnPath(runID, turnID string) string {
	return HTTPAPIPrefix + "/runs/" + runID + "/turns/" + turnID
}

func writeServiceError(writer http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, durable.ErrNotFound):
		writeHTTPError(writer, http.StatusNotFound, "not_found", err.Error())
	case errors.Is(err, turnprotocol.ErrIdempotencyConflict):
		writeHTTPError(writer, http.StatusConflict, "idempotency_conflict", err.Error())
	case errors.Is(err, turnprotocol.ErrQuestionAlreadyAnswered):
		writeHTTPError(writer, http.StatusConflict, "question_already_answered", err.Error())
	case errors.Is(err, turnprotocol.ErrQuestionExpired):
		writeHTTPError(writer, http.StatusConflict, "question_expired", err.Error())
	case errors.Is(err, turnprotocol.ErrConflict):
		writeHTTPError(writer, http.StatusConflict, "conflict", err.Error())
	default:
		writeHTTPError(writer, http.StatusInternalServerError, "internal_error", err.Error())
	}
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func writeHTTPError(writer http.ResponseWriter, status int, code, message string) {
	writeJSON(writer, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func writeSSEError(writer io.Writer, err error) {
	payload, _ := json.Marshal(map[string]any{"error": map[string]string{"code": "stream_error", "message": err.Error()}})
	_, _ = fmt.Fprintf(writer, "event: error\ndata: %s\n\n", payload)
}
