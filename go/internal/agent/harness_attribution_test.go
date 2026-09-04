package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
)

func TestModelInvocationHarnessIdentity(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	claimed := claimOpenTurn(t, as)
	t.Cleanup(func() {
		if _, err := as.pool.Exec(context.Background(), `DELETE FROM agent_turn_projections WHERE id = $1`, claimed.projectionID); err != nil {
			t.Error(err)
		}
	})
	post := func(sequence int, hash any) *http.Response {
		t.Helper()
		payload := map[string]any{
			"model_request_id": "model:harness", "provider": "openai-responses",
			"model": "test-model", "execution_mode": "foreground",
		}
		if hash != nil {
			payload["harness_hash"] = hash
		}
		return as.doJSONAuth(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+claimed.conversationID+"/turn-executions/"+claimed.lease.ExecutionID+"/checkpoints", map[string]any{
			"owner_id": "worker-1", "lease_token": claimed.lease.LeaseToken,
			"sequence": sequence, "kind": "before_model_request", "payload": payload,
		}, http.Header{"Authorization": []string{"Bearer tok"}})
	}
	for _, invalid := range []any{nil, "", "abc", strings.Repeat("g", 64), strings.ToUpper(testHarnessHash), " " + testHarnessHash, 123} {
		resp := post(1, invalid)
		as.mustStatus(t, resp, http.StatusBadRequest)
		resp.Body.Close()
	}
	var n int
	if err := as.pool.QueryRow(context.Background(), `SELECT count(*) FROM agent_turn_checkpoints WHERE execution_id = $1`, claimed.lease.ExecutionID).Scan(&n); err != nil || n != 0 {
		t.Fatalf("invalid checkpoint persisted: count=%d err=%v", n, err)
	}
	if err := as.pool.QueryRow(context.Background(), `SELECT count(*) FROM agent_model_invocations WHERE execution_id = $1`, claimed.lease.ExecutionID).Scan(&n); err != nil || n != 0 {
		t.Fatalf("invalid invocation persisted: count=%d err=%v", n, err)
	}
	for _, sequence := range []int{1, 1, 2} {
		resp := post(sequence, testHarnessHash)
		as.mustStatus(t, resp, http.StatusOK)
		resp.Body.Close()
	}
	for _, sequence := range []int{2, 3} {
		resp := post(sequence, strings.Repeat("a", 64))
		as.mustStatus(t, resp, http.StatusConflict)
		resp.Body.Close()
	}
	var stored, checkpoint string
	if err := as.pool.QueryRow(context.Background(), `
		SELECT i.harness_hash, c.payload_json::jsonb->>'harness_hash'
		FROM agent_model_invocations i JOIN agent_turn_checkpoints c ON c.execution_id = i.execution_id
		WHERE i.execution_id = $1 AND c.sequence = 1`, claimed.lease.ExecutionID).Scan(&stored, &checkpoint); err != nil {
		t.Fatal(err)
	}
	if stored != testHarnessHash || checkpoint != stored {
		t.Fatalf("identity drift: invocation=%q checkpoint=%q", stored, checkpoint)
	}
	if err := as.pool.QueryRow(context.Background(), `SELECT count(*) FROM agent_turn_checkpoints WHERE execution_id = $1`, claimed.lease.ExecutionID).Scan(&n); err != nil || n != 2 {
		t.Fatalf("conflicting checkpoint persisted: count=%d err=%v", n, err)
	}
	if _, err := as.pool.Exec(context.Background(), `UPDATE agent_model_invocations SET harness_hash = NULL WHERE execution_id = $1`, claimed.lease.ExecutionID); err != nil {
		t.Fatal(err)
	}
	resp := post(2, testHarnessHash)
	as.mustStatus(t, resp, http.StatusConflict)
	resp.Body.Close()
	var historical *string
	if err := as.pool.QueryRow(context.Background(), `SELECT harness_hash FROM agent_model_invocations WHERE execution_id = $1`, claimed.lease.ExecutionID).Scan(&historical); err != nil || historical != nil {
		t.Fatalf("historical identity was backfilled: %v err=%v", historical, err)
	}
}

func TestHarnessHashSchemaAddsNullableColumnWithoutBackfill(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	claimed := claimOpenTurn(t, as)
	t.Cleanup(func() {
		if _, err := as.pool.Exec(context.Background(), `DELETE FROM agent_turn_projections WHERE id = $1`, claimed.projectionID); err != nil {
			t.Error(err)
		}
	})
	payload := json.RawMessage(`{"model_request_id":"model:historical","provider":"openai","model":"test","execution_mode":"foreground","harness_hash":"` + testHarnessHash + `"}`)
	if _, err := as.svc.AppendCheckpoint(context.Background(), claimed.conversationID, claimed.lease.ExecutionID, "worker-1", claimed.lease.LeaseToken, 1, "before_model_request", payload); err != nil {
		t.Fatal(err)
	}
	gdb, err := pfdb.OpenGorm(as.pool)
	if err != nil {
		t.Fatal(err)
	}
	tx := gdb.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer tx.Rollback()
	// Recreate the pre-P2 column shape inside a rollback-only transaction.
	if err := tx.Migrator().DropColumn(&schema.AgentModelInvocations{}, "HarnessHash"); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := schema.Apply(tx); err != nil {
			t.Fatal(err)
		}
	}
	var row schema.AgentModelInvocations
	if err := tx.Where("execution_id = ?", claimed.lease.ExecutionID).Take(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.HarnessHash != nil || row.ModelRequestID != "model:historical" {
		t.Fatalf("historical invocation changed: %+v", row)
	}
	var nullable string
	if err := tx.Raw(`SELECT is_nullable FROM information_schema.columns WHERE table_schema = 'public' AND table_name = 'agent_model_invocations' AND column_name = 'harness_hash'`).Scan(&nullable).Error; err != nil || nullable != "YES" {
		t.Fatalf("nullable=%q err=%v", nullable, err)
	}
}

func TestL2MetadataUsesRunningAgentHarness(t *testing.T) {
	for _, hash := range []string{testHarnessHash, "", strings.ToUpper(testHarnessHash)} {
		t.Run(hash, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/healthz" {
					t.Errorf("unexpected identity path %s", r.URL.Path)
				}
				_ = json.NewEncoder(w).Encode(map[string]string{"harness_hash": hash, "skill_catalog_hash": testHarnessHash})
			}))
			defer server.Close()
			dir := t.TempDir()
			_, err := startL2Run(dir, "run-harness", 1, []EvalTask{{ID: "fixture", World: "world"}}, map[string]EvalWorld{"world": {}}, server.URL)
			if hash != testHarnessHash {
				if err == nil {
					t.Fatal("accepted missing or noncanonical Agent identity")
				}
				if _, err := os.Stat(filepath.Join(dir, "run.json")); !os.IsNotExist(err) {
					t.Fatalf("metadata published without verified identity: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(filepath.Join(dir, "run.json"))
			if err != nil {
				t.Fatal(err)
			}
			var meta map[string]any
			if err := json.Unmarshal(raw, &meta); err != nil || meta["harness_hash"] != hash {
				t.Fatalf("metadata identity=%v err=%v", meta, err)
			}
		})
	}
}

func assertProductionHarnessAttribution(t *testing.T, as *agentServer, projectionID, agentURL string) {
	t.Helper()
	meta, err := readL2AgentIdentity(agentURL)
	if err != nil {
		t.Fatal(err)
	}
	var matched, total int
	if err := as.pool.QueryRow(context.Background(), `
		SELECT count(*) FILTER (WHERE i.harness_hash = $2 AND c.payload_json::jsonb->>'harness_hash' = $2), count(*)
		FROM agent_model_invocations i
		LEFT JOIN agent_turn_checkpoints c ON c.turn_projection_id = i.turn_projection_id
			AND c.kind = 'before_model_request' AND c.payload_json::jsonb->>'model_request_id' = i.model_request_id
		WHERE i.turn_projection_id = $1`, projectionID, meta.HarnessHash).Scan(&matched, &total); err != nil {
		t.Fatal(err)
	}
	if total < 2 || matched != total {
		t.Fatalf("production health/eval/checkpoint/invocation identity mismatch: matched=%d total=%d", matched, total)
	}
	requests, err := readL2RequestIdentities(as, projectionID, map[string]any{"harness_hash": meta.HarnessHash, "skill_catalog_hash": meta.SkillHash})
	if err != nil || len(requests) != total {
		t.Fatalf("L2 actual SDK identity missing: requests=%d total=%d err=%v", len(requests), total, err)
	}
	dir := t.TempDir()
	record := map[string]any{"passed": true, "errors": []string{}, "transcript_path": "trial.json", "details": map[string]any{"turn_projection_id": projectionID}}
	if err := writeL2JSON(filepath.Join(dir, "trial.json"), map[string]any{"turn_id": projectionID}, true); err != nil {
		t.Fatal(err)
	}
	if err := attachL2TrialProvenance(as, record, dir, map[string]any{"harness_hash": meta.HarnessHash, "skill_catalog_hash": meta.SkillHash}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "trial.json"))
	if err != nil {
		t.Fatal(err)
	}
	var transcript struct {
		Provenance struct {
			Valid    bool                `json:"valid"`
			Requests []l2RequestIdentity `json:"model_requests"`
		} `json:"provenance"`
	}
	if err := json.Unmarshal(raw, &transcript); err != nil || !transcript.Provenance.Valid || len(transcript.Provenance.Requests) != total {
		t.Fatalf("L2 exported request identity incomplete: %s err=%v", raw, err)
	}
}
