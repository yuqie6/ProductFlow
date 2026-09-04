package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/platform/clockid"
)

const sigkillHelperEnv = "PRODUCTFLOW_SIGKILL_HELPER"

type sigkillReady struct {
	ExecutionID string `json:"execution_id"`
	LeaseToken  string `json:"lease_token"`
	Fencing     int    `json:"fencing_token"`
}

func TestSIGKILLWriterHelper(t *testing.T) {
	if os.Getenv(sigkillHelperEnv) == "" {
		t.Skip("SIGKILL lease-holder helper")
	}
	if err := runSIGKILLHelper(); err != nil {
		fmt.Fprintf(os.Stderr, "sigkill helper: %v\n", err)
		os.Exit(1)
	}
}

func TestSIGKILLLeaseHolderAgainstGoPG(t *testing.T) {
	if os.Getenv(sigkillHelperEnv) != "" {
		t.Skip("parent harness")
	}
	as := newAgentServer(t, mockGateway{}, "tok")
	if _, err := recoverUnfinishedTurns(context.Background(), as.svc, 1000); err != nil {
		t.Fatal(err)
	}

	t.Run("model_start", func(t *testing.T) {
		queued := createQueuedJournalTurn(t, as, "", nil, "模型开始后崩溃")
		ready := spawnAndKillSIGKILLHelper(t, as, "model_start", queued, nil)
		expireLeaseAndRun(t, as, queued, ready.ExecutionID, true)
		var status, reason, invocationStatus string
		if err := as.pool.QueryRow(context.Background(), `
			SELECT p.status, COALESCE(p.terminal_reason_code, ''), COALESCE(i.status, '')
			FROM agent_turn_projections p
			LEFT JOIN agent_model_invocations i ON i.turn_projection_id = p.id
			WHERE p.id = $1
		`, queued.turn.ID).Scan(&status, &reason, &invocationStatus); err != nil {
			t.Fatal(err)
		}
		if status != "unknown" || reason != "execution_interrupted" || invocationStatus != "interrupted" {
			t.Fatalf("status=%s reason=%s invocation=%s", status, reason, invocationStatus)
		}
		assertContinuousJournal(t, as, queued.turn.ID)
	})

	t.Run("mutation", func(t *testing.T) {
		queued := createQueuedJournalTurn(t, as, "", nil, "mutation 后崩溃")
		name := "sigkill-ws-" + clockid.New()[:12]
		ready := spawnAndKillSIGKILLHelper(t, as, "mutation", queued, map[string]string{
			"PRODUCTFLOW_SIGKILL_WORKSPACE_NAME": name,
		})
		expireLeaseAndRun(t, as, queued, ready.ExecutionID, true)
		var products int
		if err := as.pool.QueryRow(context.Background(), `
			SELECT COUNT(*) FROM products WHERE name = $1
		`, name).Scan(&products); err != nil {
			t.Fatal(err)
		}
		if products != 1 {
			t.Fatalf("workspace products %d", products)
		}
		var status string
		if err := as.pool.QueryRow(context.Background(), `
			SELECT status FROM agent_turn_projections WHERE id = $1
		`, queued.turn.ID).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if status != "unknown" {
			t.Fatalf("mutation crash status %s", status)
		}
		assertContinuousJournal(t, as, queued.turn.ID)
		assertSingleTurnEnd(t, as, queued.turn.ID)
	})

	t.Run("approval", func(t *testing.T) {
		task := seedProductGoalTask(t, as)
		queued := createQueuedJournalTurn(t, as, *task.ConversationID, task.ProductID, "审批后崩溃")
		graphID := activeGraphID(t, as, *task.ProductID)
		ready := spawnAndKillSIGKILLHelper(t, as, "approval", queued, map[string]string{
			"PRODUCTFLOW_SIGKILL_TASK_ID":  task.ID,
			"PRODUCTFLOW_SIGKILL_GRAPH_ID": graphID,
		})
		if _, err := as.pool.Exec(context.Background(), `
			UPDATE agent_turn_projections SET status = 'awaiting_confirmation', updated_at = NOW() WHERE id = $1
		`, queued.turn.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := as.pool.Exec(context.Background(), `
			UPDATE agent_turn_executions SET lease_expires_at = NOW() - INTERVAL '1 second', updated_at = NOW() WHERE id = $1
		`, ready.ExecutionID); err != nil {
			t.Fatal(err)
		}
		if _, err := recoverUnfinishedTurns(context.Background(), as.svc, 1000); err != nil {
			t.Fatal(err)
		}
		var status string
		if err := as.pool.QueryRow(context.Background(), `
			SELECT status FROM agent_turn_projections WHERE id = $1
		`, queued.turn.ID).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if status != "awaiting_confirmation" {
			t.Fatalf("approval crash became %s", status)
		}
		var requestCount, runCount, ends int
		if err := as.pool.QueryRow(context.Background(), `
			SELECT COUNT(*) FROM agent_workflow_run_requests WHERE conversation_id = $1
		`, queued.conversationID).Scan(&requestCount); err != nil {
			t.Fatal(err)
		}
		if err := as.pool.QueryRow(context.Background(), `
			SELECT COUNT(*) FROM workflow_graph_runs WHERE graph_id = $1
		`, graphID).Scan(&runCount); err != nil {
			t.Fatal(err)
		}
		if err := as.pool.QueryRow(context.Background(), `
			SELECT COUNT(*) FROM agent_turn_events WHERE turn_projection_id = $1 AND kind = 'turn/end'
		`, queued.turn.ID).Scan(&ends); err != nil {
			t.Fatal(err)
		}
		if requestCount != 1 || runCount != 0 || ends != 0 {
			t.Fatalf("approval side effects request=%d run=%d ends=%d", requestCount, runCount, ends)
		}
		assertContinuousJournal(t, as, queued.turn.ID)
	})

	t.Run("turn_end", func(t *testing.T) {
		queued := createQueuedJournalTurn(t, as, "", nil, "终态落库后崩溃")
		ready := spawnAndKillSIGKILLHelper(t, as, "turn_end", queued, nil)
		if _, err := as.pool.Exec(context.Background(), `
			UPDATE agent_turn_executions SET lease_expires_at = NOW() - INTERVAL '1 second', updated_at = NOW()
			WHERE id = $1 AND owner_id IS NOT NULL
		`, ready.ExecutionID); err != nil {
			t.Fatal(err)
		}
		if _, err := recoverUnfinishedTurns(context.Background(), as.svc, 1000); err != nil {
			t.Fatal(err)
		}
		var status string
		if err := as.pool.QueryRow(context.Background(), `
			SELECT status FROM agent_turn_projections WHERE id = $1
		`, queued.turn.ID).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if status != "succeeded" {
			t.Fatalf("turn/end crash status %s", status)
		}
		assertSingleTurnEnd(t, as, queued.turn.ID)
		assertContinuousJournal(t, as, queued.turn.ID)
		_, err := as.svc.AppendEvents(context.Background(), queued.conversationID, ready.ExecutionID, "sigkill-helper", ready.LeaseToken, []EventAppendInput{{
			Sequence: 3, SchemaVersion: 1, RunID: queued.turn.HarnessRunID, TurnID: *queued.turn.HarnessTurnID,
			Kind: "text.chunk", Payload: []byte(`{"delta":"死后写入","attempt_id":"a","content_index":0}`),
		}})
		if err == nil {
			t.Fatal("killed writer still appended after recover")
		}
	})
}

func createQueuedJournalTurn(t *testing.T, as *agentServer, conversationID string, productID *string, input string) claimedJournalTurn {
	t.Helper()
	if conversationID == "" {
		session := as.do(t, http.MethodPost, "/api/v2/agent-sessions", nil, "", nil)
		as.mustStatus(t, session, http.StatusCreated)
		var created SessionResponse
		as.decode(t, session, &created)
		conversationID = created.Conversations[0].ConversationID
	}
	key := clockid.New()
	path := "/api/v2/agent-conversations/" + conversationID + "/turns"
	if productID != nil && *productID != "" {
		path = "/api/v2/products/" + *productID + "/agent-conversations/" + conversationID + "/turns"
	}
	response := as.doJSON(t, http.MethodPost, path, map[string]any{
		"input_text": input, "idempotency_key": key,
	})
	as.mustStatus(t, response, http.StatusAccepted)
	var submitted SubmitTurnResponse
	as.decode(t, response, &submitted)
	if submitted.Turn.HarnessTurnID == nil {
		t.Fatal("missing harness turn ID")
	}
	return claimedJournalTurn{conversationID: conversationID, idempotencyKey: key, turn: submitted.Turn}
}

func spawnAndKillSIGKILLHelper(t *testing.T, as *agentServer, point string, queued claimedJournalTurn, extra map[string]string) sigkillReady {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestSIGKILLWriterHelper$", "-test.timeout=60s", "-test.count=1")
	env := append([]string{}, os.Environ()...)
	env = append(env,
		sigkillHelperEnv+"=1",
		"PRODUCTFLOW_SIGKILL_POINT="+point,
		"PRODUCTFLOW_SIGKILL_BASE_URL="+as.srv.URL,
		"PRODUCTFLOW_SIGKILL_TOKEN=tok",
		"PRODUCTFLOW_SIGKILL_CONV="+queued.conversationID,
		"PRODUCTFLOW_SIGKILL_KEY="+queued.idempotencyKey,
		"PRODUCTFLOW_SIGKILL_HARNESS_TURN="+*queued.turn.HarnessTurnID,
		"PRODUCTFLOW_SIGKILL_RUN_ID="+queued.turn.HarnessRunID,
	)
	for k, v := range extra {
		env = append(env, k+"="+v)
	}
	cmd.Env = env
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	readyCh := make(chan sigkillReady, 1)
	errCh := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			const prefix = "SIGKILL_READY "
			if strings.HasPrefix(line, prefix) {
				var ready sigkillReady
				if err := json.Unmarshal([]byte(strings.TrimPrefix(line, prefix)), &ready); err != nil {
					errCh <- err
					return
				}
				readyCh <- ready
				return
			}
		}
		if err := scanner.Err(); err != nil {
			errCh <- err
			return
		}
		errCh <- fmt.Errorf("helper stdout closed before SIGKILL_READY: %s", stderr.String())
	}()
	var ready sigkillReady
	select {
	case ready = <-readyCh:
	case err := <-errCh:
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("helper: %v", err)
	case <-time.After(20 * time.Second):
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("helper ready timeout stderr=%s", stderr.String())
	}
	if err := cmd.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	return ready
}

func expireLeaseAndRun(t *testing.T, as *agentServer, queued claimedJournalTurn, executionID string, forceRunning bool) {
	t.Helper()
	if forceRunning {
		if _, err := as.pool.Exec(context.Background(), `
			UPDATE agent_turn_projections SET status = 'running', updated_at = NOW() WHERE id = $1
		`, queued.turn.ID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := as.pool.Exec(context.Background(), `
		UPDATE agent_turn_executions SET phase = 'model', lease_expires_at = NOW() - INTERVAL '1 second', updated_at = NOW() WHERE id = $1
	`, executionID); err != nil {
		t.Fatal(err)
	}
	if _, err := recoverUnfinishedTurns(context.Background(), as.svc, 1000); err != nil {
		t.Fatal(err)
	}
}

func assertContinuousJournal(t *testing.T, as *agentServer, projectionID string) {
	t.Helper()
	var maxSeq, count int
	if err := as.pool.QueryRow(context.Background(), `
		SELECT COALESCE(MAX(sequence), 0), COUNT(*) FROM agent_turn_events WHERE turn_projection_id = $1
	`, projectionID).Scan(&maxSeq, &count); err != nil {
		t.Fatal(err)
	}
	if maxSeq != count {
		t.Fatalf("sequence hole max=%d count=%d", maxSeq, count)
	}
}

func assertSingleTurnEnd(t *testing.T, as *agentServer, projectionID string) {
	t.Helper()
	var n int
	if err := as.pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM agent_turn_events WHERE turn_projection_id = $1 AND kind = 'turn/end'
	`, projectionID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("turn/end count %d", n)
	}
}

func runSIGKILLHelper() error {
	base := os.Getenv("PRODUCTFLOW_SIGKILL_BASE_URL")
	token := os.Getenv("PRODUCTFLOW_SIGKILL_TOKEN")
	conv := os.Getenv("PRODUCTFLOW_SIGKILL_CONV")
	key := os.Getenv("PRODUCTFLOW_SIGKILL_KEY")
	harness := os.Getenv("PRODUCTFLOW_SIGKILL_HARNESS_TURN")
	runID := os.Getenv("PRODUCTFLOW_SIGKILL_RUN_ID")
	point := os.Getenv("PRODUCTFLOW_SIGKILL_POINT")
	client := &http.Client{Timeout: 15 * time.Second}
	auth := func(req *http.Request) {
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
	}
	claimBody, _ := json.Marshal(map[string]any{
		"idempotency_key": key, "harness_turn_id": harness, "owner_id": "sigkill-helper",
	})
	req, err := http.NewRequest(http.MethodPost, base+"/api/internal/v1/agent-conversations/"+conv+"/turn-executions/claim", bytes.NewReader(claimBody))
	if err != nil {
		return err
	}
	auth(req)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("claim %d %s", resp.StatusCode, raw)
	}
	var lease ExecutionLeaseResponse
	if err := json.Unmarshal(raw, &lease); err != nil {
		return err
	}
	postJSON := func(path string, payload any, extra http.Header) ([]byte, int, error) {
		body, err := json.Marshal(payload)
		if err != nil {
			return nil, 0, err
		}
		req, err := http.NewRequest(http.MethodPost, base+path, bytes.NewReader(body))
		if err != nil {
			return nil, 0, err
		}
		auth(req)
		for k, vs := range extra {
			for _, v := range vs {
				req.Header.Set(k, v)
			}
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, 0, err
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return raw, resp.StatusCode, nil
	}
	appendEvents := func(events []map[string]any) error {
		raw, status, err := postJSON("/api/internal/v1/agent-conversations/"+conv+"/turn-executions/"+lease.ExecutionID+"/events/batch", map[string]any{
			"owner_id": "sigkill-helper", "lease_token": lease.LeaseToken, "events": events,
		}, nil)
		if err != nil {
			return err
		}
		if status != http.StatusOK {
			return fmt.Errorf("events %d %s", status, raw)
		}
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	event := func(seq int, kind string, payload any) map[string]any {
		body, _ := json.Marshal(payload)
		return map[string]any{
			"sequence": seq, "schema_version": 1, "run_id": runID, "turn_id": harness,
			"kind": kind, "payload": json.RawMessage(body), "created_at": now,
		}
	}
	switch point {
	case "model_start":
		payload, _ := json.Marshal(map[string]any{
			"model_request_id": "model:sigkill-" + clockid.New(),
			"provider":         "openai-responses",
			"model":            "test-model",
			"execution_mode":   "foreground",
			"harness_hash":     testHarnessHash,
		})
		raw, status, err := postJSON("/api/internal/v1/agent-conversations/"+conv+"/turn-executions/"+lease.ExecutionID+"/checkpoints", map[string]any{
			"owner_id": "sigkill-helper", "lease_token": lease.LeaseToken,
			"sequence": 1, "kind": "before_model_request", "payload": json.RawMessage(payload),
		}, nil)
		if err != nil {
			return err
		}
		if status != http.StatusOK {
			return fmt.Errorf("checkpoint %d %s", status, raw)
		}
	case "mutation":
		name := os.Getenv("PRODUCTFLOW_SIGKILL_WORKSPACE_NAME")
		reqPayload, err := json.Marshal(map[string]any{"name": name})
		if err != nil {
			return err
		}
		intent := toolEffectIntentV1{
			SchemaVersion: 1, ToolName: "create_product_workspace_v1",
			ToolCallID: "call-sigkill", IdempotencyKey: key,
			RecoveryPolicy: "reconcile_then_retry", RequestPayload: reqPayload,
		}
		intentRaw, err := json.Marshal(intent)
		if err != nil {
			return err
		}
		raw, status, err := postJSON("/api/internal/v1/agent-conversations/"+conv+"/turn-executions/"+lease.ExecutionID+"/checkpoints", map[string]any{
			"owner_id": "sigkill-helper", "lease_token": lease.LeaseToken,
			"sequence": 1, "kind": "tool_effect_intent", "payload": json.RawMessage(intentRaw),
		}, nil)
		if err != nil {
			return err
		}
		if status != http.StatusOK {
			return fmt.Errorf("intent checkpoint %d %s", status, raw)
		}
		raw, status, err = postJSON("/api/internal/v1/agent-conversations/"+conv+"/product-workspaces", map[string]any{
			"name": name,
		}, http.Header{"Idempotency-Key": []string{key}})
		if err != nil {
			return err
		}
		if status != http.StatusCreated && status != http.StatusOK {
			return fmt.Errorf("workspace %d %s", status, raw)
		}
	case "approval":
		taskID := os.Getenv("PRODUCTFLOW_SIGKILL_TASK_ID")
		graphID := os.Getenv("PRODUCTFLOW_SIGKILL_GRAPH_ID")
		raw, status, err := postJSON("/api/internal/v1/agent-conversations/"+conv+"/workflow-run-requests", map[string]any{
			"expected_workflow_revision": 1,
			"workflow_id":                graphID,
			"source_step_id":             "run-1",
			"task_id":                    taskID,
		}, http.Header{"Idempotency-Key": []string{clockid.New()}})
		if err != nil {
			return err
		}
		if status != http.StatusOK {
			return fmt.Errorf("workflow request %d %s", status, raw)
		}
		var pending struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(raw, &pending); err != nil {
			return err
		}
		if err := appendEvents([]map[string]any{event(1, "approval/requested", map[string]any{
			"approval_id": pending.ID, "approval_kind": "workflow_run",
		})}); err != nil {
			return err
		}
	case "turn_end":
		if err := appendEvents([]map[string]any{
			event(1, "text.chunk", map[string]any{"delta": "完整回复", "attempt_id": "a", "content_index": 0}),
			event(2, "turn/end", map[string]any{"status": "succeeded", "reason": "completed", "output": "完整回复"}),
		}); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown crash point %s", point)
	}
	ready, _ := json.Marshal(sigkillReady{ExecutionID: lease.ExecutionID, LeaseToken: lease.LeaseToken, Fencing: lease.FencingToken})
	if _, err := fmt.Printf("SIGKILL_READY %s\n", ready); err != nil {
		return err
	}
	_ = os.Stdout.Sync()
	select {}
}
