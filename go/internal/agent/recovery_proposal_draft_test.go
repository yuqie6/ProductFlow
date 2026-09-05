package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

func TestRecoverExpiredProposalTurnDoesNotCreateSecondProposal(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	if _, err := recoverUnfinishedTurns(context.Background(), as.svc, 1000); err != nil {
		t.Fatal(err)
	}
	task := seedProductGoalTask(t, as)
	graphID := activeGraphID(t, as, *task.ProductID)
	claimed := createClaimedTurnForConversation(t, as, *task.ConversationID, task.ProductID)
	key := clockid.New()
	changeSet, err := json.Marshal(map[string]any{
		"base_graph_revision": 1,
		"summary":             "恢复至多一次提案",
		"actor_type":          "agent",
		"operations": []map[string]any{
			{"op": "create_node", "client_ref": "n1", "node_type": "product_source", "title": "商品资料", "config": map[string]any{"source_product_id": *task.ProductID}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	auth := http.Header{"Authorization": []string{"Bearer tok"}, "Idempotency-Key": []string{key}}
	first := as.doJSONAuth(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+*task.ConversationID+"/graph/proposals", map[string]any{
		"change_set": json.RawMessage(changeSet),
	}, auth)
	as.mustStatus(t, first, http.StatusOK)
	var created map[string]any
	as.decode(t, first, &created)
	proposalID, _ := created["proposal_id"].(string)
	if proposalID == "" {
		t.Fatalf("missing proposal %+v", created)
	}
	expireClaimedTurn(t, as, claimed)
	if _, err := recoverUnfinishedTurns(context.Background(), as.svc, 1000); err != nil {
		t.Fatal(err)
	}
	replay := as.doJSONAuth(t, http.MethodPost, "/api/internal/v1/agent-conversations/"+*task.ConversationID+"/graph/proposals", map[string]any{
		"change_set": json.RawMessage(changeSet),
	}, auth)
	as.mustStatus(t, replay, http.StatusOK)
	var replayed map[string]any
	as.decode(t, replay, &replayed)
	if replayed["proposal_id"] != proposalID {
		t.Fatalf("replay proposal %v want %s", replayed["proposal_id"], proposalID)
	}
	var n int
	if err := as.pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM workflow_graph_proposals WHERE graph_id = $1
	`, graphID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("proposals %d", n)
	}
	confirm := as.do(t, http.MethodPost, "/api/v3/products/"+*task.ProductID+"/workflows/"+graphID+"/proposals/"+proposalID+"/confirm", nil, "", nil)
	as.mustStatus(t, confirm, http.StatusOK)
	confirm.Body.Close()
	if _, err := recoverUnfinishedTurns(context.Background(), as.svc, 1000); err != nil {
		t.Fatal(err)
	}
	second := as.do(t, http.MethodPost, "/api/v3/products/"+*task.ProductID+"/workflows/"+graphID+"/proposals/"+proposalID+"/confirm", nil, "", nil)
	mustNotPending(t, as, second, "图提案已经结束")
	if err := as.pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM workflow_graph_proposals WHERE graph_id = $1 AND status = 'confirmed'
	`, graphID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("confirmed proposals %d", n)
	}
	var revision int
	if err := as.pool.QueryRow(context.Background(), `
		SELECT revision FROM workflow_graphs WHERE id = $1
	`, graphID).Scan(&revision); err != nil {
		t.Fatal(err)
	}
	if revision != 2 {
		t.Fatalf("graph revision %d want 2", revision)
	}
}

func TestRecoverExpiredDraftTurnDoesNotCreateSecondDraft(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	if _, err := recoverUnfinishedTurns(context.Background(), as.svc, 1000); err != nil {
		t.Fatal(err)
	}
	session := as.do(t, http.MethodPost, "/api/v2/agent-sessions", nil, "", nil)
	as.mustStatus(t, session, http.StatusCreated)
	var sess SessionResponse
	as.decode(t, session, &sess)
	convID := sess.Conversations[0].ConversationID
	claimed := createClaimedTurnForConversation(t, as, convID, nil)
	payload := libraryRenamePayload(t, as)
	var revID string
	err := tx.WithGorm(context.Background(), as.db, func(pgxTx *gorm.DB) error {
		id, err := as.svc.Library.AppendOrganizationDraftRevisionTx(context.Background(), pgxTx, convID, payload, claimed.turn.ID, "draft-1")
		revID = id
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := as.pool.Exec(context.Background(), `
		UPDATE agent_turn_projections
		SET status = 'awaiting_confirmation', library_organization_draft_revision_id = $2, updated_at = NOW()
		WHERE id = $1
	`, claimed.turn.ID, revID); err != nil {
		t.Fatal(err)
	}
	if _, err := as.pool.Exec(context.Background(), `
		UPDATE agent_turn_executions SET lease_expires_at = NOW() - INTERVAL '1 second', updated_at = NOW() WHERE id = $1
	`, claimed.lease.ExecutionID); err != nil {
		t.Fatal(err)
	}
	if _, err := recoverUnfinishedTurns(context.Background(), as.svc, 1000); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := as.pool.QueryRow(context.Background(), `
		SELECT status FROM agent_turn_projections WHERE id = $1
	`, claimed.turn.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "awaiting_confirmation" {
		t.Fatalf("parked draft became %s", status)
	}
	var drafts int
	if err := as.pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM library_organization_drafts WHERE conversation_id = $1
	`, convID).Scan(&drafts); err != nil {
		t.Fatal(err)
	}
	if drafts != 1 {
		t.Fatalf("drafts %d", drafts)
	}
	key := clockid.New()
	first := as.doJSON(t, http.MethodPost, "/api/v2/agent-conversations/"+convID+"/library-organization-draft/confirm", map[string]any{
		"expected_draft_version": 1, "idempotency_key": key,
	})
	as.mustStatus(t, first, http.StatusOK)
	first.Body.Close()
	if _, err := recoverUnfinishedTurns(context.Background(), as.svc, 1000); err != nil {
		t.Fatal(err)
	}
	second := as.doJSON(t, http.MethodPost, "/api/v2/agent-conversations/"+convID+"/library-organization-draft/confirm", map[string]any{
		"expected_draft_version": 1, "idempotency_key": key,
	})
	as.mustStatus(t, second, http.StatusOK)
	second.Body.Close()
	third := as.doJSON(t, http.MethodPost, "/api/v2/agent-conversations/"+convID+"/library-organization-draft/confirm", map[string]any{
		"expected_draft_version": 1, "idempotency_key": clockid.New(),
	})
	mustNotPending(t, as, third, "素材整理 Draft 已使用其他确认请求完成")
	if err := as.pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM library_organization_drafts WHERE conversation_id = $1
	`, convID).Scan(&drafts); err != nil {
		t.Fatal(err)
	}
	if drafts != 1 {
		t.Fatalf("drafts after confirm %d", drafts)
	}
	var draftStatus string
	if err := as.pool.QueryRow(context.Background(), `
		SELECT status FROM library_organization_drafts WHERE conversation_id = $1
	`, convID).Scan(&draftStatus); err != nil {
		t.Fatal(err)
	}
	if draftStatus != "confirmed" {
		t.Fatalf("draft status %s", draftStatus)
	}
}
