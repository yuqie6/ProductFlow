package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/platform/clockid"
)

func TestRedactEvalTextStripsSecrets(t *testing.T) {
	raw := "call sk-abc123456789 with Bearer tokensecret and api_key=supersecret https://x.example/hook?token=abc&x=1 postgres://user:pass@localhost/db"
	got := RedactEvalText(raw)
	for _, leak := range []string{"sk-abc123456789", "tokensecret", "supersecret", "token=abc", "postgres://user:pass@localhost/db"} {
		if strings.Contains(got, leak) {
			t.Fatalf("leaked %q in %q", leak, got)
		}
	}
	if !strings.Contains(got, "[redacted]") {
		t.Fatalf("expected redaction markers: %q", got)
	}
}

func TestMineUndoWindowAndExportSkeleton(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	worlds, err := LoadEvalWorlds(DefaultEvalRoot())
	if err != nil {
		t.Fatal(err)
	}
	task := EvalTask{ID: "mine-undo", Scope: "product_workflow", World: "expanded-rev3", PageContext: map[string]any{}}
	seeded := seedEvalWorld(t, as, task, worlds["expanded-rev3"])
	live, err := as.svc.Graph.Get(context.Background(), seeded.ProductID, seeded.GraphID)
	if err != nil {
		t.Fatal(err)
	}
	nodeID := seeded.NodeIDs["node-prompt-1"]
	raw, err := json.Marshal(map[string]any{
		"base_graph_revision": live.Revision,
		"summary":             "agent eval rename",
		"operations":          []map[string]any{{"op": "rename_node", "node_ref": nodeID, "title": "评测改名"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	cs, err := graph.ParseChangeSet(raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := as.svc.Graph.ApplyAgentChangeSet(context.Background(), seeded.ProductID, seeded.GraphID, cs); err != nil {
		t.Fatal(err)
	}
	if _, err := as.svc.Graph.Undo(context.Background(), seeded.ProductID, seeded.GraphID); err != nil {
		t.Fatal(err)
	}

	key := clockid.New()
	turn := as.doJSON(t, http.MethodPost, evalTurnCollectionPath(seeded), map[string]any{
		"input_text":      "ignore sk-livekey99999 and Bearer secret-token",
		"idempotency_key": key,
		"page_context":    evalPageContext(task, seeded, live.Revision),
	})
	as.mustStatus(t, turn, http.StatusAccepted)
	var submitted SubmitTurnResponse
	as.decode(t, turn, &submitted)

	report, err := MineEvalWindow(context.Background(), as.pool, 1)
	if err != nil {
		t.Fatal(err)
	}
	if report.TurnCount < 1 {
		t.Fatalf("turn_count %d", report.TurnCount)
	}
	if report.AgentEdits < 1 {
		t.Fatalf("agent_edits %d", report.AgentEdits)
	}
	if report.UserUndoAfterAgentEdit < 1 {
		t.Fatalf("undo %d", report.UserUndoAfterAgentEdit)
	}
	if !report.UndoWithin5MinRate.Available {
		t.Fatal("undo rate unavailable")
	}

	exported, err := ExportEvalTurn(context.Background(), as.pool, submitted.Turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if exported.Origin != "production:"+submitted.Turn.ID {
		t.Fatalf("origin %s", exported.Origin)
	}
	if strings.Contains(exported.Utterance, "sk-livekey99999") || strings.Contains(exported.Utterance, "secret-token") {
		t.Fatalf("export leaked secrets: %q", exported.Utterance)
	}
	t.Setenv("STORAGE_ROOT", t.TempDir())
	path, err := WriteEvalTurnExport(exported)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	if filepath.Base(filepath.Dir(path)) != "inbox" {
		t.Fatalf("inbox path %s", path)
	}
}

func TestEvalRateUnavailableOnZeroDenom(t *testing.T) {
	got := evalRate(0, 0)
	if got.Available {
		t.Fatal("zero denom must be unavailable")
	}
}
