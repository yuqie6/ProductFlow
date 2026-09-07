package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

func TestRecoverableToolNamesMatchManifestRetrySet(t *testing.T) {
	got := recoverableToolNames()
	want := []string{
		"apply_graph_change_set_v1",
		"cancel_workflow_run_v1",
		"create_product_workspace_v1",
		"discard_workflow_proposal_v1",
		"finalize_product_intake_v1",
		"propose_graph_change_set_v1",
		"request_global_workflow_run_v1",
		"request_workflow_run_v1",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("recoverable tools %v want %v", got, want)
	}
}

func TestReconcileEffectIntentEightToolStateMatrix(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	productTask := seedProductGoalTask(t, as)
	graphID := activeGraphID(t, as, *productTask.ProductID)
	globalConv := newGlobalConversationID(t, as)

	for _, toolName := range recoverableToolNames() {
		toolName := toolName
		t.Run(toolName, func(t *testing.T) {
			t.Run("applied", func(t *testing.T) {
				intent, conversationID, productID := seedEffectCase(t, toolName, productTask, graphID, globalConv)
				seedEffectLedger(t, as, toolName, conversationID, intent, "applied")
				out := runEffectReconcile(t, as, conversationID, productID, intent, false)
				if out.EffectResult != effectResultApplied || out.ReconciliationState != reconStateApplied {
					t.Fatalf("applied %+v", out)
				}
			})
			t.Run("conflict", func(t *testing.T) {
				intent, conversationID, productID := seedEffectCase(t, toolName, productTask, graphID, globalConv)
				seedEffectLedger(t, as, toolName, conversationID, intent, "conflict")
				out := runEffectReconcile(t, as, conversationID, productID, intent, false)
				if out.EffectResult != effectResultFailed || out.ReconciliationState != reconStateConflict {
					t.Fatalf("conflict %+v", out)
				}
			})
			t.Run("unknown", func(t *testing.T) {
				intent, conversationID, productID := seedEffectCase(t, toolName, productTask, graphID, globalConv)
				seedEffectLedger(t, as, toolName, conversationID, intent, "unknown")
				out := runEffectReconcile(t, as, conversationID, productID, intent, false)
				if out.EffectResult != effectResultUnknown || out.ReconciliationState != reconStateUnknown {
					t.Fatalf("unknown %+v", out)
				}
			})
			t.Run("not_applied_retry", func(t *testing.T) {
				intent, conversationID, productID := seedEffectCase(t, toolName, productTask, graphID, globalConv)
				out := runEffectReconcile(t, as, conversationID, productID, intent, true)
				if out.ReconciliationState != reconStateApplied && out.ReconciliationState != reconStateUnknown && out.ReconciliationState != reconStateConflict {
					t.Fatalf("retry final state %+v", out)
				}
				if out.ReconciliationState == "not_applied" || out.EffectResult == "not_applied" {
					t.Fatalf("retry must not persist not_applied: %+v", out)
				}
			})
		})
	}
}

func TestLiveManualAndScannerEffectReconciliationShareInterpreter(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	if _, err := recoverUnfinishedTurns(context.Background(), as.svc, 1000); err != nil {
		t.Fatal(err)
	}

	live := createClaimedJournalTurn(t, as)
	liveIntent := workspaceIntent("运行中工作区")
	insertIntentCheckpoint(t, as, live, liveIntent)
	seedEffectLedger(t, as, liveIntent.ToolName, live.conversationID, liveIntent, "applied")
	livePath := "/api/internal/v1/agent-conversations/" + live.conversationID + "/turn-executions/" + live.lease.ExecutionID + "/effects/reconcile"
	wrongLease := as.doJSONAuth(t, http.MethodPost, livePath, map[string]any{
		"owner_id": "worker-1", "lease_token": "wrong-lease", "tool_call_id": liveIntent.ToolCallID,
	}, http.Header{"Authorization": []string{"Bearer tok"}})
	as.mustStatus(t, wrongLease, http.StatusConflict)
	wrongLease.Body.Close()
	liveResponse := as.doJSONAuth(t, http.MethodPost, livePath, map[string]any{
		"owner_id": "worker-1", "lease_token": live.lease.LeaseToken, "tool_call_id": liveIntent.ToolCallID,
	}, http.Header{"Authorization": []string{"Bearer tok"}})
	as.mustStatus(t, liveResponse, http.StatusOK)
	var liveOut EffectReconciliationResponse
	as.decode(t, liveResponse, &liveOut)
	if liveOut.EffectResult != effectResultApplied || liveOut.ReconciliationState != reconStateApplied {
		t.Fatalf("live %+v", liveOut)
	}

	claimed := createClaimedJournalTurn(t, as)
	intent := workspaceIntent("共享解释器工作区")
	insertIntentCheckpoint(t, as, claimed, intent)
	seedEffectLedger(t, as, intent.ToolName, claimed.conversationID, intent, "applied")

	if _, err := as.pool.Exec(context.Background(), `
		UPDATE agent_turn_projections SET status = 'unknown', terminal_reason_code = 'execution_interrupted', updated_at = NOW() WHERE id = $1
	`, claimed.turn.ID); err != nil {
		t.Fatal(err)
	}
	manual, err := as.svc.ReconcileTurnEffect(context.Background(), nil, claimed.conversationID, claimed.turn.ID, intent.ToolCallID)
	if err != nil {
		t.Fatal(err)
	}
	if manual.EffectResult != effectResultApplied || manual.ReconciliationState != reconStateApplied {
		t.Fatalf("manual %+v", manual)
	}

	scanner := createClaimedJournalTurn(t, as)
	scannerIntent := workspaceIntent("扫描器工作区")
	insertIntentCheckpoint(t, as, scanner, scannerIntent)
	seedEffectLedger(t, as, scannerIntent.ToolName, scanner.conversationID, scannerIntent, "applied")
	if _, err := as.svc.AppendEvents(context.Background(), scanner.conversationID, scanner.lease.ExecutionID, "worker-1", scanner.lease.LeaseToken, []EventAppendInput{{
		Sequence: 1, SchemaVersion: 1, RunID: scanner.turn.HarnessRunID, TurnID: *scanner.turn.HarnessTurnID,
		Kind: "text.chunk", Payload: json.RawMessage(`{"delta":"中断","attempt_id":"a","content_index":0}`),
	}}); err != nil {
		t.Fatal(err)
	}
	expireClaimedTurn(t, as, scanner)
	if _, err := recoverUnfinishedTurns(context.Background(), as.svc, 1000); err != nil {
		t.Fatal(err)
	}
	var kinds []string
	if err := as.db.Model(&schema.AgentTurnEvents{}).Where("turn_projection_id = ?", scanner.turn.ID).
		Order("sequence").Pluck("kind", &kinds).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(kinds, []string{"text.chunk", "tool/result", "assistant/message", "turn/end"}) {
		t.Fatalf("scanner events %v", kinds)
	}
	var recon schema.AgentTurnEffectReconciliations
	if err := as.db.Where("turn_projection_id = ? AND tool_call_id = ?", scanner.turn.ID, scannerIntent.ToolCallID).Take(&recon).Error; err != nil {
		t.Fatal(err)
	}
	if recon.EffectResult != effectResultApplied || recon.ReconciliationState != reconStateApplied {
		t.Fatalf("scanner recon %+v", recon)
	}
}

func TestExpiredExecutionRecoveryUsesBoundedSkipLockedBatch(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	drainAgentRecovery(t, as)
	first := createClaimedJournalTurn(t, as)
	second := createClaimedJournalTurn(t, as)
	expireClaimedTurn(t, as, first)
	expireClaimedTurn(t, as, second)
	summary, err := recoverUnfinishedTurns(context.Background(), as.svc, 1)
	if err != nil {
		t.Fatal(err)
	}
	if summary.UnknownExecutions < 1 {
		t.Fatalf("bounded batch recovered %+v", summary)
	}
	var unknownHere int
	if err := as.pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM agent_turn_projections
		WHERE id IN ($1, $2) AND status = 'unknown'
	`, first.turn.ID, second.turn.ID).Scan(&unknownHere); err != nil {
		t.Fatal(err)
	}
	if unknownHere != 1 {
		t.Fatalf("unknown among fixture turns %d, summary=%+v", unknownHere, summary)
	}
	var remaining int
	if err := as.pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM agent_turn_executions
		WHERE id IN ($1, $2) AND owner_id IS NOT NULL
	`, first.lease.ExecutionID, second.lease.ExecutionID).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 1 {
		t.Fatalf("remaining owned expired executions %d", remaining)
	}
}

func TestWorkspaceReconcileDoesNotTreatDatabaseErrorsAsNotApplied(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	out, err := as.svc.ReconcileWorkspaceFromGlobal(context.Background(), "missing-conversation", "名称", clockid.New())
	if err == nil && out.State == "not_applied" {
		t.Fatal("missing conversation must not be inferred as not_applied")
	}
}

func runEffectReconcile(t *testing.T, as *agentServer, conversationID string, productID *string, intent toolEffectIntentV1, retry bool) effectReconcileOutcome {
	t.Helper()
	claimed := createClaimedTurnForConversation(t, as, conversationID, productID)
	insertIntentCheckpoint(t, as, claimed, intent)
	var out effectReconcileOutcome
	err := tx.WithGorm(context.Background(), as.db, func(gdb *gorm.DB) error {
		item, err := as.svc.reconcileEffectIntent(context.Background(), gdb, conversationID, claimed.turn.ID, intent, retry)
		out = item
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func seedEffectCase(t *testing.T, toolName string, task TaskResponse, graphID, globalConv string) (toolEffectIntentV1, string, *string) {
	t.Helper()
	key := clockid.New()
	callID := "call-" + clockid.New()
	switch toolName {
	case "create_product_workspace_v1":
		return mustIntent(t, toolName, callID, key, map[string]any{"name": "矩阵工作区-" + key[:8]}), globalConv, nil
	case "finalize_product_intake_v1":
		return mustIntent(t, toolName, callID, key, map[string]any{
			"selection": map[string]any{
				"schema_version": 1,
				"image_types":    []any{map[string]any{"key": "hero", "quantity": 1, "order": 0}},
			},
			"reference_asset_ids": []string{clockid.New()},
			"task_id":             nil,
		}), *task.ConversationID, task.ProductID
	case applyGraphTool, proposeGraphTool:
		return mustIntent(t, toolName, callID, key, map[string]any{
			"change_set": sampleChangeSet(*task.ProductID, key),
		}), *task.ConversationID, task.ProductID
	case discardProposalTool:
		return mustIntent(t, toolName, callID, key, map[string]any{"proposal_id": nil}), *task.ConversationID, task.ProductID
	case cancelRunTool:
		return mustIntent(t, toolName, callID, key, map[string]any{"run_id": clockid.New()}), *task.ConversationID, task.ProductID
	case "request_workflow_run_v1":
		return mustIntent(t, toolName, callID, key, map[string]any{
			"expected_workflow_revision": 1,
			"workflow_id":                graphID,
			"source_step_id":             "step-" + key[:8],
			"task_id":                    task.ID,
			"source_run_id":              nil,
			"scope":                      "graph",
			"node_id":                    nil,
			"node_ids":                   []string{},
			"force":                      false,
			"document_action":            nil,
		}), *task.ConversationID, task.ProductID
	case "request_global_workflow_run_v1":
		return mustIntent(t, toolName, callID, key, map[string]any{
			"expected_workflow_revision": 1,
			"workflow_id":                graphID,
			"product_id":                 *task.ProductID,
			"source_step_id":             "step-" + key[:8],
			"task_id":                    nil,
			"source_run_id":              nil,
			"scope":                      "graph",
			"node_id":                    nil,
			"node_ids":                   []string{},
			"force":                      false,
			"document_action":            nil,
		}), globalConv, nil
	default:
		t.Fatalf("unhandled tool %s", toolName)
		return toolEffectIntentV1{}, "", nil
	}
}

func seedEffectLedger(t *testing.T, as *agentServer, toolName, conversationID string, intent toolEffectIntentV1, state string) {
	t.Helper()
	now := time.Now().UTC()
	result := `{"accepted":true}`
	switch toolName {
	case "create_product_workspace_v1":
		if state == "unknown" {
			return
		}
		name := "冲突工作区"
		fields, _ := decodeIntentPayload(intent.RequestPayload)
		if state == "applied" {
			name = fieldString(fields, "name")
		}
		if _, err := as.svc.LaunchWorkspaceFromGlobal(context.Background(), conversationID, name, intent.IdempotencyKey); err != nil {
			t.Fatal(err)
		}
		return
	case "request_workflow_run_v1", "request_global_workflow_run_v1":
		if state == "unknown" {
			return
		}
		hash := matchingWorkflowHash(t, conversationID, intent)
		if state == "conflict" {
			hash = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		}
		fields, _ := decodeIntentPayload(intent.RequestPayload)
		productID := fieldString(fields, "product_id")
		if productID == "" {
			_, conv, err := as.svc.loadScopedConversation(context.Background(), conversationID)
			if err != nil {
				t.Fatal(err)
			}
			productID = *conv.ProductID
		}
		row := schema.AgentWorkflowRunRequests{
			ID: clockid.New(), ConversationID: conversationID, TaskID: fieldOptionalString(fields, "task_id"),
			ProductID: productID, GraphID: fieldString(fields, "workflow_id"),
			ExpectedWorkflowRevision: 1, IdempotencyKey: intent.IdempotencyKey, RequestHash: hash,
			SourceStepID: fieldString(fields, "source_step_id"), Status: "awaiting_confirmation",
			CreatedAt: now, UpdatedAt: now,
		}
		if err := as.db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
		return
	}
	hash := matchingMutationHash(t, conversationID, intent)
	status := "applied"
	var resultJSON *string
	if state == "applied" {
		resultJSON = &result
	}
	if state == "conflict" {
		hash = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		resultJSON = &result
	}
	if state == "unknown" {
		status = "unknown"
	}
	row := schema.AgentToolMutations{
		ID: clockid.New(), ConversationID: conversationID, ToolName: toolName,
		IdempotencyKey: intent.IdempotencyKey, RequestHash: hash, Status: status,
		ResultJSON: resultJSON, PreparedJSON: "{}", CreatedAt: now, UpdatedAt: now,
	}
	if err := as.db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
}

func matchingMutationHash(t *testing.T, conversationID string, intent toolEffectIntentV1) string {
	t.Helper()
	fields, err := decodeIntentPayload(intent.RequestPayload)
	if err != nil {
		t.Fatal(err)
	}
	var before, target map[string]any
	switch intent.ToolName {
	case "finalize_product_intake_v1":
		target = map[string]any{"selection": fields["selection"], "reference_asset_ids": fieldStrings(fields, "reference_asset_ids")}
		before = map[string]any{}
	case applyGraphTool, proposeGraphTool:
		var parseErr error
		before, target, _, parseErr = graphChangeSetPrepared(fields["change_set"])
		if parseErr != nil {
			t.Fatal(parseErr)
		}
	case discardProposalTool:
		target = map[string]any{"proposal_id": any(nil)}
		if id := fieldOptionalString(fields, "proposal_id"); id != nil {
			target["proposal_id"] = *id
		}
		before = map[string]any{}
	case cancelRunTool:
		target = map[string]any{"run_id": fieldString(fields, "run_id")}
		before = map[string]any{}
	default:
		t.Fatalf("hash for %s", intent.ToolName)
	}
	hash, err := toolRequestHash(intent.ToolName, toolPrepared(conversationID, intent.ToolName, before, target))
	if err != nil {
		t.Fatal(err)
	}
	return hash
}

func matchingWorkflowHash(t *testing.T, conversationID string, intent toolEffectIntentV1) string {
	t.Helper()
	fields, err := decodeIntentPayload(intent.RequestPayload)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := runScopeFromFields(fields)
	if err != nil {
		t.Fatal(err)
	}
	var productID *string
	if intent.ToolName == "request_global_workflow_run_v1" {
		id := fieldString(fields, "product_id")
		productID = &id
	}
	hash, err := hashWorkflowRunRequest(
		conversationID, productID, fieldString(fields, "workflow_id"), fieldString(fields, "source_step_id"),
		fieldInt(fields, "expected_workflow_revision"), fieldOptionalString(fields, "task_id"), fieldOptionalString(fields, "source_run_id"), spec,
	)
	if err != nil {
		t.Fatal(err)
	}
	return hash
}

func mustIntent(t *testing.T, toolName, callID, key string, payload map[string]any) toolEffectIntentV1 {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	intent := toolEffectIntentV1{
		SchemaVersion: 1, ToolName: toolName, ToolCallID: callID, IdempotencyKey: key,
		RecoveryPolicy: toolRecoveryPolicies[toolName], RequestPayload: raw,
	}
	if _, err := parseToolEffectIntent(mustMarshalIntent(t, intent)); err != nil {
		t.Fatal(err)
	}
	return intent
}

func mustMarshalIntent(t *testing.T, intent toolEffectIntentV1) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(intent)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func sampleChangeSet(productID, suffix string) map[string]any {
	return map[string]any{
		"base_graph_revision": 1,
		"summary":             "添加商品资料",
		"operations": []map[string]any{{
			"op": "create_node", "client_ref": "n-" + suffix[:8], "node_type": "product_source",
			"title": "商品资料", "position_x": 120, "position_y": 80,
			"config": map[string]any{"source_product_id": productID},
		}},
	}
}

func workspaceIntent(name string) toolEffectIntentV1 {
	raw, _ := json.Marshal(map[string]any{"name": name})
	return toolEffectIntentV1{
		SchemaVersion: 1, ToolName: "create_product_workspace_v1",
		ToolCallID: "call-" + clockid.New(), IdempotencyKey: clockid.New(),
		RecoveryPolicy: "reconcile_then_retry", RequestPayload: raw,
	}
}

func insertIntentCheckpoint(t *testing.T, as *agentServer, claimed claimedJournalTurn, intent toolEffectIntentV1) {
	t.Helper()
	now := time.Now().UTC()
	row := schema.AgentTurnCheckpoints{
		ID: clockid.New(), TurnProjectionID: claimed.turn.ID, ExecutionID: claimed.lease.ExecutionID,
		Attempt: claimed.lease.Attempt, FencingToken: claimed.lease.FencingToken, Sequence: 1,
		Kind: "tool_effect_intent", PayloadJSON: string(mustMarshalIntent(t, intent)), CreatedAt: now,
	}
	if err := as.db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
}

func createClaimedTurnForConversation(t *testing.T, as *agentServer, conversationID string, productID *string) claimedJournalTurn {
	t.Helper()
	key := clockid.New()
	path := "/api/v2/agent-conversations/" + conversationID + "/turns"
	if productID != nil && *productID != "" {
		path = "/api/v2/products/" + *productID + "/agent-conversations/" + conversationID + "/turns"
	}
	response := as.doJSON(t, http.MethodPost, path, map[string]any{
		"input_text": "effect reconcile", "idempotency_key": key,
	})
	as.mustStatus(t, response, http.StatusAccepted)
	var submitted SubmitTurnResponse
	as.decode(t, response, &submitted)
	if submitted.Turn.HarnessTurnID == nil {
		t.Fatal("missing harness turn ID")
	}
	lease, err := as.svc.ClaimExecution(context.Background(), conversationID, nil, key, *submitted.Turn.HarnessTurnID, "worker-1")
	if err != nil {
		t.Fatal(err)
	}
	return claimedJournalTurn{conversationID: conversationID, idempotencyKey: key, turn: submitted.Turn, lease: lease}
}

func expireClaimedTurn(t *testing.T, as *agentServer, claimed claimedJournalTurn) {
	t.Helper()
	if _, err := as.pool.Exec(context.Background(), `
		UPDATE agent_turn_projections SET status = 'running', updated_at = NOW() WHERE id = $1
	`, claimed.turn.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := as.pool.Exec(context.Background(), `
		UPDATE agent_turn_executions SET phase = 'model', lease_expires_at = NOW() - INTERVAL '1 second', updated_at = NOW() WHERE id = $1
	`, claimed.lease.ExecutionID); err != nil {
		t.Fatal(err)
	}
}
