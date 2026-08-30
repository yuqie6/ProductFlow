package agent

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHTTPGatewayStreamTurnEventsForwardsAfterCursor(t *testing.T) {
	var gotAfter string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		gotAfter = r.URL.Query().Get("after")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "id: 9\nevent: tool.step\ndata: {\"kind\":\"tool.step\"}\n\n")
	}))
	t.Cleanup(upstream.Close)

	var out strings.Builder
	err := HTTPGateway{BaseURL: upstream.URL, Token: "tok", ConnectTimeout: time.Second}.StreamTurnEvents(
		context.Background(), "conv-1", "turn-1", nil, 8, &out,
	)
	if err != nil {
		t.Fatal(err)
	}
	if gotAfter != "8" {
		t.Fatalf("after %q", gotAfter)
	}
	if !strings.Contains(out.String(), "event: tool.step") {
		t.Fatalf("body %q", out.String())
	}
}
