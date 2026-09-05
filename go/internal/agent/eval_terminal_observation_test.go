package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestEvalGoTerminalWaitsForProjection(t *testing.T) {
	var reads atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v2/agent-conversations/conversation/turns/turn" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		status := "running"
		if reads.Add(1) >= 3 {
			status = "succeeded"
		}
		_ = json.NewEncoder(w).Encode(TurnResponse{Status: status, ToolSteps: []map[string]any{{"tool_name": "list_products_v1", "status": status}}})
	}))
	t.Cleanup(srv.Close)
	as := &agentServer{srv: srv, client: srv.Client()}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	got, err := waitEvalGoTurnTerminal(ctx, as, seededEvalWorld{ConvID: "conversation"}, "turn", "succeeded")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "succeeded" || reads.Load() != 3 {
		t.Fatalf("status=%s reads=%d; must wait for PG projection", got.Status, reads.Load())
	}
	names, calls := toolCallsFromSteps(got.ToolSteps)
	if len(names) != 1 || names[0] != "list_products_v1" || len(calls) != 1 {
		t.Fatalf("lost terminal tool steps: %+v", got.ToolSteps)
	}
}

func TestEvalToolStepsRequireObservedSuccess(t *testing.T) {
	for _, status := range []string{"", "running", "failed", "succeeded"} {
		t.Run(status, func(t *testing.T) {
			names, calls := toolCallsFromSteps([]map[string]any{{"tool_name": "apply_workflow_operations_v1", "status": status}})
			want := "unknown"
			if status == "failed" || status == "succeeded" {
				want = status
			}
			if len(calls) != 1 || calls[0]["outcome"] != want || (len(names) == 1) != (status == "succeeded") {
				t.Fatalf("status=%q names=%v calls=%v", status, names, calls)
			}
		})
	}
}

func TestEvalGoTerminalAcceptsEveryObservedTerminal(t *testing.T) {
	for _, status := range []string{"succeeded", "failed", "canceled", "unknown", "requires_input", "awaiting_confirmation"} {
		t.Run(status, func(t *testing.T) {
			var reads atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				reads.Add(1)
				cookie, err := r.Cookie("session")
				if err != nil || cookie.Value != "fixture" {
					t.Errorf("lost authentication cookie: %v", err)
				}
				if r.URL.Path != "/api/v2/products/product/agent-conversations/conversation/turns/turn" {
					t.Errorf("wrong scoped path: %s", r.URL.Path)
				}
				_ = json.NewEncoder(w).Encode(TurnResponse{Status: status})
			}))
			t.Cleanup(srv.Close)
			as := &agentServer{srv: srv, client: srv.Client(), cookies: []*http.Cookie{{Name: "session", Value: "fixture"}}}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			got, err := waitEvalGoTurnTerminal(ctx, as, seededEvalWorld{ProductID: "product", ConvID: "conversation"}, "turn", "succeeded")
			if err != nil || got.Status != status || reads.Load() != 1 {
				t.Fatalf("terminal=%s reads=%d err=%v", got.Status, reads.Load(), err)
			}
		})
	}
}

func TestEvalGoTerminalTimeoutKeepsLastObservation(t *testing.T) {
	for _, status := range []string{"queued", "running", "cancel_requested"} {
		t.Run(status, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(TurnResponse{Status: status})
			}))
			t.Cleanup(srv.Close)
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			got, err := waitEvalGoTurnTerminal(ctx, &agentServer{srv: srv, client: srv.Client()}, seededEvalWorld{ConvID: "conversation"}, "turn", "succeeded")
			if !errors.Is(err, context.DeadlineExceeded) || got.Status != status {
				t.Fatalf("last=%+v err=%v", got, err)
			}
			for _, want := range []string{"L2 observation failed", `Node status="succeeded"`, fmt.Sprintf("Go status=%q", status)} {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("missing %q in %v", want, err)
				}
			}
		})
	}
}

func TestEvalGoTerminalReadErrorsPreserveLastObservation(t *testing.T) {
	for _, kind := range []string{"http", "json", "transport"} {
		t.Run(kind, func(t *testing.T) {
			var reads atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if reads.Add(1) == 1 {
					_ = json.NewEncoder(w).Encode(TurnResponse{Status: "running"})
					return
				}
				switch kind {
				case "http":
					w.WriteHeader(http.StatusServiceUnavailable)
				case "json":
					_, _ = io.WriteString(w, "invalid JSON")
				case "transport":
					conn, _, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					_ = conn.Close()
				}
			}))
			t.Cleanup(srv.Close)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			got, err := waitEvalGoTurnTerminal(ctx, &agentServer{srv: srv, client: srv.Client()}, seededEvalWorld{ConvID: "conversation"}, "turn", "succeeded")
			if err == nil || errors.Is(err, context.DeadlineExceeded) || got.Status != "running" {
				t.Fatalf("last=%+v err=%v", got, err)
			}
		})
	}
}

func TestEvalGoTerminalDeadlineInterruptsBodyRead(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := waitEvalGoTurnTerminal(ctx, &agentServer{srv: srv, client: srv.Client()}, seededEvalWorld{ConvID: "conversation"}, "turn", "succeeded")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("body read must obey observation deadline: %v", err)
	}
}

type evalObservationTransport func(*http.Request) (*http.Response, error)

func (f evalObservationTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestEvalL2TrialObservationFailureSkipsBusinessGrading(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, gopgPiInternalToken)
	transport := as.client.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	as.client.Transport = evalObservationTransport(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/turns/") {
			return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: io.NopCloser(strings.NewReader("unavailable")), Header: http.Header{}}, nil
		}
		return transport.RoundTrip(r)
	})
	pi := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "succeeded"})
	}))
	t.Cleanup(pi.Close)
	tasks, worlds, err := LoadEvalTasks(DefaultEvalRoot(), "l2")
	if err != nil {
		t.Fatal(err)
	}
	task := FilterEvalTasks(tasks, "graph-editing-rename-node")[0]
	minNodes := 999999
	task.Expect.State = &EvalStateExpect{MinNodeCount: &minNodes}
	task.Expect.Tools.Required = []string{"must_not_be_graded"}
	task.Expect.Terminal = []string{"awaiting_confirmation"}
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "transcripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	record := runL2Trial(t, as, piAgentProc{baseURL: pi.URL}, task, worlds[task.World], "observation-test", 1, filepath.Join(dir, "transcripts"))
	if record["passed"] != false || record["status"] != "observation_failed" || record["terminal"] != nil {
		t.Fatalf("invalid observation classification: %+v", record)
	}
	issues := record["errors"].([]string)
	if len(issues) != 1 || !strings.Contains(issues[0], "L2 observation failed") || !strings.Contains(issues[0], "HTTP 503") {
		t.Fatalf("business grading ran or observation error lost: %v", issues)
	}
	raw, err := os.ReadFile(filepath.Join(dir, record["transcript_path"].(string)))
	if err != nil {
		t.Fatal(err)
	}
	var transcript map[string]any
	if err := json.Unmarshal(raw, &transcript); err != nil {
		t.Fatal(err)
	}
	observation := transcript["observation"].(map[string]any)
	if transcript["terminal_status"] != nil || observation["node_status"] != "succeeded" || observation["go_status"] != "" {
		t.Fatalf("misleading terminal evidence: %s", raw)
	}
}
