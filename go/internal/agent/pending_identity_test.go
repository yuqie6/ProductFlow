package agent

import (
	"net/http"
	"testing"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
)

func TestPendingIdentityConflictsShareNotPendingCode(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")

	t.Run("ask_user", func(t *testing.T) {
		session := as.do(t, http.MethodPost, "/api/v2/agent-sessions", nil, "", nil)
		as.mustStatus(t, session, http.StatusCreated)
		var sess SessionResponse
		as.decode(t, session, &sess)
		convID := sess.Conversations[0].ConversationID
		turn := as.doJSON(t, http.MethodPost, "/api/v2/agent-conversations/"+convID+"/turns", map[string]any{
			"input_text": "提问", "idempotency_key": clockid.New(),
		})
		as.mustStatus(t, turn, http.StatusAccepted)
		var submitted SubmitTurnResponse
		as.decode(t, turn, &submitted)
		resp := as.doJSON(t, http.MethodPost, "/api/v2/agent-conversations/"+convID+"/turns/"+submitted.Turn.ID+"/questions/missing/answer", map[string]any{
			"text": "迟到回答",
		})
		mustNotPending(t, as, resp, "当前 Agent Turn 不在等待回答状态")
	})

	t.Run("ask_user_duplicate", func(t *testing.T) {
		gw := &questionGateway{}
		dup := newAgentServer(t, gw, "tok")
		convID, turnID, _ := dup.submitAndParkQuestion(t, "question-dup-1")
		first := dup.doJSON(t, http.MethodPost, "/api/v2/agent-conversations/"+convID+"/turns/"+turnID+"/questions/question-dup-1/answer", map[string]any{
			"text": "第一次回答",
		})
		dup.mustStatus(t, first, http.StatusOK)
		first.Body.Close()
		second := dup.doJSON(t, http.MethodPost, "/api/v2/agent-conversations/"+convID+"/turns/"+turnID+"/questions/question-dup-1/answer", map[string]any{
			"text": "第二次回答",
		})
		mustNotPending(t, dup, second, "当前 Agent Turn 不在等待回答状态")
	})

	t.Run("workflow_request", func(t *testing.T) {
		task, pending := createProductGoalRunRequest(t, as, false)
		cancel := as.do(t, http.MethodPost, "/api/v2/products/"+*task.ProductID+"/agent-conversations/"+*task.ConversationID+"/workflow-run-request/"+pending.ID+"/cancel", nil, "", nil)
		as.mustStatus(t, cancel, http.StatusOK)
		cancel.Body.Close()
		confirm := as.do(t, http.MethodPost, "/api/v2/products/"+*task.ProductID+"/agent-conversations/"+*task.ConversationID+"/workflow-run-request/"+pending.ID+"/confirm", nil, "", nil)
		mustNotPending(t, as, confirm, "已取消的工作流执行请求不能确认")
	})

	t.Run("graph_proposal", func(t *testing.T) {
		productID, graphID, convID := seedProductGraphConversation(t, as)
		proposalID, _, _ := seedPendingGraphProposalTurn(t, as, productID, graphID, convID)
		discard := as.do(t, http.MethodPost, "/api/v3/products/"+productID+"/workflows/"+graphID+"/proposals/"+proposalID+"/discard", nil, "", nil)
		as.mustStatus(t, discard, http.StatusOK)
		discard.Body.Close()
		confirm := as.do(t, http.MethodPost, "/api/v3/products/"+productID+"/workflows/"+graphID+"/proposals/"+proposalID+"/confirm", nil, "", nil)
		mustNotPending(t, as, confirm, "图提案已经结束")
	})

	t.Run("global_draft", func(t *testing.T) {
		session := as.do(t, http.MethodPost, "/api/v2/agent-sessions", nil, "", nil)
		as.mustStatus(t, session, http.StatusCreated)
		var sess SessionResponse
		as.decode(t, session, &sess)
		convID := sess.Conversations[0].ConversationID
		confirm := as.doJSON(t, http.MethodPost, "/api/v2/agent-conversations/"+convID+"/library-organization-draft/confirm", map[string]any{
			"expected_draft_version": 1, "idempotency_key": clockid.New(),
		})
		if confirm.StatusCode != http.StatusConflict && confirm.StatusCode != http.StatusNotFound {
			raw := make([]byte, 256)
			n, _ := confirm.Body.Read(raw)
			confirm.Body.Close()
			t.Fatalf("draft confirm %d %s", confirm.StatusCode, raw[:n])
		}
		if confirm.StatusCode == http.StatusConflict {
			mustNotPending(t, as, confirm, "当前素材整理 Draft 不在待确认状态")
			return
		}
		confirm.Body.Close()
	})
}

func mustNotPending(t *testing.T, as *agentServer, resp *http.Response, detail string) {
	t.Helper()
	as.mustStatus(t, resp, http.StatusConflict)
	var body map[string]any
	as.decode(t, resp, &body)
	if body["code"] != apperr.CodeNotPending {
		t.Fatalf("code %v want %s", body["code"], apperr.CodeNotPending)
	}
	if body["detail"] != detail {
		t.Fatalf("detail %v want %s", body["detail"], detail)
	}
}
