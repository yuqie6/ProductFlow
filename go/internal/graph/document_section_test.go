package graph

import (
	"encoding/json"
	"testing"

	"github.com/yuqie6/productflow/internal/platform/db/schema"
)

func TestDocumentSectionSurvivesQueueMetadataAndSeparatesRequests(t *testing.T) {
	nodeID := "prompt"
	raw, _ := json.Marshal(runProgressMeta(RunScopeNode, &nodeID, nil, true, DocumentActionRewrite, "copy"))
	metadata := string(raw)
	run := graphRunFromSchema(schema.WorkflowGraphRuns{RunScope: RunScopeNode, RequestedNodeID: &nodeID, GraphRevision: 3, ProgressMetadata: &metadata})
	if !sameInFlightRun(run, RunScopeNode, &nodeID, nil, true, DocumentActionRewrite, "copy", 3) {
		t.Fatal("same section not deduplicated")
	}
	if sameInFlightRun(run, RunScopeNode, &nodeID, nil, true, DocumentActionRewrite, "composition", 3) {
		t.Fatal("different sections deduplicated")
	}
	if sameInFlightRun(run, RunScopeNode, &nodeID, nil, true, DocumentActionRewrite, "", 3) {
		t.Fatal("whole document deduplicated with section")
	}
}

func TestDocumentSectionValidation(t *testing.T) {
	if validateGraphRunRequest(GraphRunRequest{Scope: RunScopeNode, DocumentSection: "copy"}) == nil {
		t.Fatal("section accepted without action")
	}
	if validateDocumentSection(NodeCreativeBrief, "objective") == nil {
		t.Fatal("section accepted for wrong type")
	}
	if validateDocumentSection(NodeImagePrompt, "bogus") == nil {
		t.Fatal("unknown section accepted")
	}
	if err := validateDocumentSection(NodeImagePrompt, "copy"); err != nil {
		t.Fatal(err)
	}
}
