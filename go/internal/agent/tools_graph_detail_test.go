package agent

import (
	"net/http"
	"reflect"
	"testing"
)

func TestWorkflowRunDetailEnforcesConversationScope(t *testing.T) {
	as := newAgentServer(t, mockGateway{}, "tok")
	task := seedProductGoalTask(t, as)
	graphID := activeGraphID(t, as, *task.ProductID)
	runID := insertGraphRun(t, as, graphID, "failed", true)
	auth := http.Header{"Authorization": []string{"Bearer tok"}}

	productPath := "/api/internal/v1/agent-conversations/" + *task.ConversationID + "/workflow-runs/" + runID
	resp := as.do(t, http.MethodGet, productPath, nil, "", auth)
	as.mustStatus(t, resp, http.StatusOK)
	var detail map[string]any
	as.decode(t, resp, &detail)
	if detail["schema_version"] != float64(1) || detail["run_id"] != runID || detail["workflow_id"] != graphID {
		t.Fatalf("detail %+v", detail)
	}
	if nodes, ok := detail["nodes"].([]any); !ok || len(nodes) != 0 {
		t.Fatalf("nodes %+v", detail["nodes"])
	}

	other := seedProductGoalTask(t, as)
	foreignGraphID := activeGraphID(t, as, *other.ProductID)
	foreignRunID := insertGraphRun(t, as, foreignGraphID, "failed", true)
	foreign := as.do(t, http.MethodGet, "/api/internal/v1/agent-conversations/"+*task.ConversationID+"/workflow-runs/"+foreignRunID, nil, "", auth)
	as.mustStatus(t, foreign, http.StatusNotFound)
	foreign.Body.Close()

	globalConversationID := newGlobalConversationID(t, as)
	globalPath := "/api/internal/v1/agent-conversations/" + globalConversationID + "/workflow-runs/" + runID
	global := as.do(t, http.MethodGet, globalPath, nil, "", auth)
	as.mustStatus(t, global, http.StatusOK)
	global.Body.Close()
}

func TestBoundedNodeArtifactFallbackIsDeterministic(t *testing.T) {
	output := map[string]any{
		"k10": 10, "k09": 9, "k08": 8, "k07": 7, "k06": 6,
		"k05": 5, "k04": 4, "k03": 3, "k02": 2, "k01": 1,
	}
	want := []string{"k01", "k02", "k03", "k04", "k05", "k06", "k07", "k08"}
	for range 20 {
		got, ok := boundedNodeArtifact(output)["keys"].([]string)
		if !ok || !reflect.DeepEqual(got, want) {
			t.Fatalf("keys %#v want %#v", got, want)
		}
	}
}
