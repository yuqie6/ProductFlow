package httpx

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestHealthzMatchesPythonContract(t *testing.T) {
	engine := NewEngine(nil)
	RegisterHealth(engine, nil)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var payload map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["status"] != "ok" {
		t.Fatalf("payload %#v", payload)
	}
	if rec.Header().Get("x-request-id") == "" {
		t.Fatal("missing x-request-id")
	}
}

func TestHealthzEchoesRequestID(t *testing.T) {
	engine := NewEngine(nil)
	RegisterHealth(engine, nil)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("x-request-id", "req-from-client")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	if rec.Header().Get("x-request-id") != "req-from-client" {
		t.Fatalf("got %q", rec.Header().Get("x-request-id"))
	}
}

func TestReadyWithoutPool(t *testing.T) {
	engine := NewEngine(nil)
	RegisterHealth(engine, nil)
	req := httptest.NewRequest(http.MethodGet, "/healthz/ready", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestAccessLogIncludesDurationAnd5xxError(t *testing.T) {
	core, logs := observer.New(zapcore.InfoLevel)
	engine := NewEngine(zap.New(core))
	engine.GET("/ok", func(c *gin.Context) { c.Status(http.StatusOK) })
	engine.GET("/boom", func(c *gin.Context) { AbortErr(c, errors.New("disk full")) })

	okReq := httptest.NewRequest(http.MethodGet, "/ok", nil)
	okRec := httptest.NewRecorder()
	engine.ServeHTTP(okRec, okReq)

	boomReq := httptest.NewRequest(http.MethodGet, "/boom", nil)
	boomRec := httptest.NewRecorder()
	engine.ServeHTTP(boomRec, boomReq)
	if boomRec.Code != http.StatusInternalServerError {
		t.Fatalf("status %d", boomRec.Code)
	}

	entries := logs.All()
	if len(entries) < 2 {
		t.Fatalf("log count %d", len(entries))
	}
	okLog := entries[0]
	if okLog.Level != zapcore.InfoLevel {
		t.Fatalf("ok level %s", okLog.Level)
	}
	if !strings.Contains(okLog.Message, "GET /ok 200") {
		t.Fatalf("ok message %q", okLog.Message)
	}
	okFields := okLog.ContextMap()
	if _, ok := okFields["duration_ms"]; !ok {
		t.Fatalf("missing duration_ms: %+v", okFields)
	}
	boomLog := entries[len(entries)-1]
	if boomLog.Level != zapcore.WarnLevel {
		t.Fatalf("boom level %s", boomLog.Level)
	}
	boomFields := boomLog.ContextMap()
	if boomFields["error"] != "disk full" {
		t.Fatalf("error field %+v", boomFields)
	}
}

func TestAccessLogHealthzAndHeartbeatAreDebug(t *testing.T) {
	core, logs := observer.New(zapcore.InfoLevel)
	engine := NewEngine(zap.New(core))
	RegisterHealth(engine, nil)
	engine.POST("/api/internal/v1/agent-conversations/c1/turn-executions/e1/heartbeat", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	health := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	engine.ServeHTTP(httptest.NewRecorder(), health)
	beat := httptest.NewRequest(http.MethodPost, "/api/internal/v1/agent-conversations/c1/turn-executions/e1/heartbeat", nil)
	engine.ServeHTTP(httptest.NewRecorder(), beat)

	if logs.Len() != 0 {
		t.Fatalf("quiet routes logged at info: %+v", logs.All())
	}

	debugCore, debugLogs := observer.New(zapcore.DebugLevel)
	debugEngine := NewEngine(zap.New(debugCore))
	RegisterHealth(debugEngine, nil)
	debugEngine.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if debugLogs.Len() != 1 {
		t.Fatalf("debug count %d", debugLogs.Len())
	}
	if debugLogs.All()[0].Level != zapcore.DebugLevel {
		t.Fatalf("level %s", debugLogs.All()[0].Level)
	}
}
