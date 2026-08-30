package agent

import (
	"net/http"
	"strings"
	"testing"
)

func TestContractSystemPrompts(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	auth := http.Header{"Authorization": []string{"Bearer tok"}}

	session := as.do(t, http.MethodPost, "/api/v2/agent-sessions", nil, "", nil)
	as.mustStatus(t, session, http.StatusCreated)
	var sess SessionResponse
	as.decode(t, session, &sess)
	globalConv := sess.Conversations[0].ConversationID

	global := as.do(t, http.MethodGet, "/api/internal/v1/agent-conversations/"+globalConv+"/contract", nil, "", auth)
	as.mustStatus(t, global, http.StatusOK)
	var globalContract ContractResponse
	as.decode(t, global, &globalContract)
	if !strings.Contains(globalContract.SystemPrompt, "不能在全局会话上改某个商品的 live graph") {
		t.Fatalf("global prompt %q", globalContract.SystemPrompt)
	}
	if strings.Contains(globalContract.SystemPrompt, "不得自行宣布 Goal 完成") {
		t.Fatal("global conversation contract must not include goal loop")
	}

	task := seedProductGoalTask(t, as)
	if task.ConversationID == nil {
		t.Fatal("product task missing conversation")
	}
	workflow := as.do(t, http.MethodGet, "/api/internal/v1/agent-conversations/"+*task.ConversationID+"/contract", nil, "", auth)
	as.mustStatus(t, workflow, http.StatusOK)
	var workflowContract ContractResponse
	as.decode(t, workflow, &workflowContract)
	if !strings.Contains(workflowContract.SystemPrompt, "不得提交第二份完整拓扑") {
		t.Fatalf("workflow prompt %q", workflowContract.SystemPrompt)
	}
	if strings.Contains(workflowContract.SystemPrompt, "不得自行宣布 Goal 完成") {
		t.Fatal("conversation contract must not include goal loop until the task contract")
	}

	taskContractResp := as.do(t, http.MethodGet, "/api/internal/v1/agent-tasks/"+task.ID+"/contract", nil, "", auth)
	as.mustStatus(t, taskContractResp, http.StatusOK)
	var taskContract ContractResponse
	as.decode(t, taskContractResp, &taskContract)
	if !strings.Contains(taskContract.SystemPrompt, "不得提交第二份完整拓扑") {
		t.Fatalf("task prompt missing topology rule: %q", taskContract.SystemPrompt)
	}
	if !strings.Contains(taskContract.SystemPrompt, "不得自行宣布 Goal 完成") {
		t.Fatalf("task prompt missing goal loop: %q", taskContract.SystemPrompt)
	}
}
