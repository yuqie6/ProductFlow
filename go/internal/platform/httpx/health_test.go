package httpx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
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
