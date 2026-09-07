package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/platform/testdb"
	"gorm.io/gorm"
)

func TestAgentReadHTTPTargetScale(t *testing.T) {
	if os.Getenv("PRODUCTFLOW_RUN_AGENT_READ_LOAD") != "1" {
		t.Skip("set PRODUCTFLOW_RUN_AGENT_READ_LOAD=1 for the isolated Agent HTTP read gate")
	}
	if os.Getenv("DATABASE_URL") == "" {
		t.Fatal("Agent read gate requires DATABASE_URL")
	}
	pool, db := testdb.IsolatedMigrated(t, fmt.Sprintf("pf_aread_%d", time.Now().UnixNano()))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	merchantID := auth.MustDevMerchantID(t, db)
	seedTargetScaleAgentSessions(t, ctx, pool, merchantID)
	for _, seed := range []struct {
		query string
		args  []any
	}{
		{query: `UPDATE agent_sessions SET title=repeat('t',128), summary=repeat('s',1024)`},
		{query: `UPDATE products SET name=repeat('p',192) WHERE id='plan-product-0'`},
		{query: `INSERT INTO agent_conversations (id, merchant_id, product_id, harness_run_id, status, created_at, updated_at, session_id, scope_type)
		 SELECT 'wide-conv-' || s || '-' || c, $1, 'plan-product-0', 'wide-conv-' || s || '-' || c,
		 'collecting', NOW(), NOW(), 'plan-sess-' || lpad(s::text,5,'0'), 'product_workflow'
		 FROM generate_series(0,19) s CROSS JOIN generate_series(1,100) c`, args: []any{merchantID}},
		{query: `INSERT INTO agent_turn_projections (id, conversation_id, idempotency_key, request_hash, input_text,
		 input_asset_ids_json, status, resume_required, tool_steps_json, output_text, thinking_text, created_at, updated_at)
		 SELECT 'wide-turn-' || lpad(g::text,4,'0'), 'plan-conv-00000', 'read-key-' || g, repeat('b',64), repeat('i',512),
		 '[]', 'succeeded', FALSE, '[]', repeat('o',8192), repeat('h',2048), NOW() - g*INTERVAL '1 second', NOW()
		 FROM generate_series(1,1000) g`},
		{query: `ANALYZE agent_sessions`}, {query: `ANALYZE agent_conversations`}, {query: `ANALYZE agent_turn_projections`},
	} {
		if _, err := pool.Exec(ctx, seed.query, seed.args...); err != nil {
			t.Fatal(err)
		}
	}
	as := newAgentServerOnDB(t, mockGateway{}, "", pool, db)
	as.client.Timeout = 5 * time.Second
	var queries atomic.Int64
	count := func(q *gorm.DB) {
		if !q.DryRun {
			queries.Add(1)
		}
	}
	const callback = "test:agent_read_load"
	if err := db.Callback().Query().After("gorm:query").Register(callback, count); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Row().After("gorm:row").Register(callback, count); err != nil {
		t.Fatal(err)
	}
	defer db.Callback().Query().Remove(callback)
	defer db.Callback().Row().Remove(callback)
	read := func(t *testing.T, path string) []byte {
		t.Helper()
		resp := as.do(t, http.MethodGet, path, nil, "", nil)
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s: %d %s", path, resp.StatusCode, body)
		}
		return body
	}
	const turnsPath = "/api/v2/agent-conversations/plan-conv-00000/turns"
	for _, tc := range []struct {
		name, path string
		budget     time.Duration
		check      func(*testing.T, []byte)
	}{
		{"dock", "/api/v2/agent-sessions?limit=20", 300 * time.Millisecond, func(t *testing.T, body []byte) {
			var page SessionListResponse
			if err := json.Unmarshal(body, &page); err != nil {
				t.Fatal(err)
			}
			if len(page.Items) != 20 || page.NextCursor == nil {
				t.Fatal("incorrect dock page")
			}
			for _, item := range page.Items {
				if item.ConversationCount != 101 || len(item.Conversations) != 20 || len(item.Title) != 128 || item.Summary == nil || len(*item.Summary) != 1024 {
					t.Fatalf("incorrect session projection %s", item.ID)
				}
			}
		}},
		{"product", "/api/v2/agent-sessions?product_id=plan-product-0&limit=20", 300 * time.Millisecond, func(t *testing.T, body []byte) {
			var page SessionListResponse
			if err := json.Unmarshal(body, &page); err != nil {
				t.Fatal(err)
			}
			if len(page.Items) != 20 || page.NextCursor == nil {
				t.Fatal("incorrect product page")
			}
			for _, item := range page.Items {
				if item.ConversationCount != 1 || len(item.Conversations) != 1 {
					t.Fatal("incorrect product conversations")
				}
			}
		}},
		{"turns", turnsPath + "?limit=50", 500 * time.Millisecond, func(t *testing.T, body []byte) {
			var page TurnPageResponse
			if err := json.Unmarshal(body, &page); err != nil {
				t.Fatal(err)
			}
			if len(page.Items) != 50 || page.NextCursor == nil {
				t.Fatal("incorrect Turn page")
			}
			for _, item := range page.Items {
				if len(item.InputText) != 512 || item.OutputText == nil || len(*item.OutputText) != 8192 || item.ThinkingText == nil || len(*item.ThinkingText) != 2048 {
					t.Fatal("Turn content truncated")
				}
			}
		}},
		{"turn_detail", turnsPath + "/wide-turn-0001", 500 * time.Millisecond, func(t *testing.T, body []byte) {
			var item TurnResponse
			if err := json.Unmarshal(body, &item); err != nil {
				t.Fatal(err)
			}
			if item.ID != "wide-turn-0001" || item.OutputText == nil || len(*item.OutputText) != 8192 {
				t.Fatal("incorrect Turn detail")
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var times []time.Duration
			var totalQueries int64
			maxBytes := 0
			for i := 0; i < 110; i++ {
				before := queries.Load()
				start := time.Now()
				body := read(t, tc.path)
				elapsed := time.Since(start)
				if i >= 10 {
					times = append(times, elapsed)
					totalQueries += queries.Load() - before
					maxBytes = max(maxBytes, len(body))
				}
				tc.check(t, body)
			}
			slices.Sort(times)
			t.Logf("AGENT_READ path=%s sessions=25000 conversations=27000 turns=1000 concurrency=1 warmup=10 samples=100 p50=%s p95=%s max_body_bytes=%d query_row_callbacks=%d", tc.name, times[49], times[94], maxBytes, totalQueries)
			if tc.name == "turns" && totalQueries != 500 {
				t.Errorf("50-Turn pages made %d queries, want 500 over 100 requests", totalQueries)
			}
			if times[94] >= tc.budget || maxBytes >= 1<<20 {
				t.Errorf("read exceeds local budget: p95=%s budget=%s bytes=%d", times[94], tc.budget, maxBytes)
			}
		})
	}
	seen := map[string]bool{}
	after := ""
	for {
		var page TurnPageResponse
		if err := json.Unmarshal(read(t, turnsPath+"?limit=50&after="+url.QueryEscape(after)), &page); err != nil {
			t.Fatal(err)
		}
		for _, item := range page.Items {
			if seen[item.ID] {
				t.Fatalf("duplicate Turn %s", item.ID)
			}
			seen[item.ID] = true
		}
		if page.NextCursor == nil {
			break
		}
		after = *page.NextCursor
	}
	if len(seen) != 1000 {
		t.Fatalf("cursor returned %d Turns, want 1000", len(seen))
	}
}
