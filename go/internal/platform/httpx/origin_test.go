package httpx

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestBrowserStateProtectionRequiresExactSource(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(BrowserStateProtection(BrowserStateConfig{
		AllowedOrigins: []string{"http://localhost:29283"},
		InternalToken:  "internal-test-token",
	}))
	engine.POST("/api/auth/session", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	for _, tc := range []struct {
		name       string
		origin     string
		referer    string
		wantStatus int
	}{
		{name: "exact origin", origin: "http://localhost:29283", wantStatus: http.StatusNoContent},
		{name: "referer fallback", referer: "http://localhost:29283/login?next=1", wantStatus: http.StatusNoContent},
		{name: "missing source", wantStatus: http.StatusForbidden},
		{name: "null origin", origin: "null", referer: "http://localhost:29283/login", wantStatus: http.StatusForbidden},
		{name: "mismatched origin", origin: "http://evil.test", referer: "http://localhost:29283/login", wantStatus: http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/auth/session", nil)
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			if tc.referer != "" {
				req.Header.Set("Referer", tc.referer)
			}
			resp := httptest.NewRecorder()
			engine.ServeHTTP(resp, req)
			if resp.Code != tc.wantStatus {
				t.Fatalf("status=%d want=%d body=%s", resp.Code, tc.wantStatus, resp.Body.String())
			}
		})
	}
}

func TestBrowserStateProtectionInternalBearerStillNeedsExactToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(BrowserStateProtection(BrowserStateConfig{
		AllowedOrigins: []string{"http://localhost:29283"},
		InternalToken:  "internal-test-token",
	}))
	engine.POST("/api/internal/v1/agent-conversations/c/turn-executions/e/events/batch", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	for _, tc := range []struct {
		name       string
		token      string
		wantStatus int
	}{
		{name: "valid token", token: "internal-test-token", wantStatus: http.StatusNoContent},
		{name: "wrong token", token: "wrong", wantStatus: http.StatusForbidden},
		{name: "missing token", wantStatus: http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/internal/v1/agent-conversations/c/turn-executions/e/events/batch", nil)
			if tc.token != "" {
				req.Header.Set("Authorization", "Bearer "+tc.token)
			}
			resp := httptest.NewRecorder()
			engine.ServeHTTP(resp, req)
			if resp.Code != tc.wantStatus {
				t.Fatalf("status=%d want=%d body=%s", resp.Code, tc.wantStatus, resp.Body.String())
			}
		})
	}
}

func TestCanonicalOriginRejectsCredentialAndPath(t *testing.T) {
	for _, raw := range []string{
		"http://user@example.test",
		"http://example.test/path",
		"http://example.test?x=1",
		"http://example.test, http://evil.test",
		"null",
	} {
		if origin, ok := canonicalOrigin(raw); ok {
			t.Fatalf("canonicalOrigin(%q)=%q, want rejection", raw, origin)
		}
	}
	if got, ok := canonicalOrigin("http://[2001:db8::1]:8443"); !ok || got != "http://[2001:db8::1]:8443" {
		t.Fatalf("IPv6 origin=%q ok=%v", got, ok)
	}
}

func TestBrowserStateProtectionPreservesMultipartAndRejectsDuplicateOrigin(t *testing.T) {
	engine := gin.New()
	engine.Use(BrowserStateProtection(BrowserStateConfig{AllowedOrigins: []string{"https://web.test"}}))
	engine.POST("/api/upload", func(c *gin.Context) {
		if c.PostForm("name") != "reference" {
			t.Error("multipart body was not preserved")
		}
		c.Status(http.StatusNoContent)
	})
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	if err := form.WriteField("name", "reference"); err != nil {
		t.Fatal(err)
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	for _, duplicate := range []bool{false, true} {
		req := httptest.NewRequest(http.MethodPost, "/api/upload", bytes.NewReader(body.Bytes()))
		req.Header.Set("Content-Type", form.FormDataContentType())
		req.Header.Set("Origin", "https://web.test")
		want := http.StatusNoContent
		if duplicate {
			req.Header.Add("Origin", "https://other.test")
			want = http.StatusForbidden
		}
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Fatalf("duplicate=%t: %d %s", duplicate, rec.Code, rec.Body.String())
		}
	}
}
