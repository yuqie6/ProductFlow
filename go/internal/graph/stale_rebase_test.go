package graph

import "testing"

func TestNodeConfigWriteTargetsRejectsMixedOps(t *testing.T) {
	ids, ok := nodeConfigWriteTargets([]Operation{
		UpdateNodeConfigOp{NodeRef: "brief"},
	})
	if !ok || len(ids) != 1 || ids[0] != "brief" {
		t.Fatalf("targets %+v ok=%v", ids, ok)
	}
	_, ok = nodeConfigWriteTargets([]Operation{
		UpdateNodeConfigOp{NodeRef: "brief"},
		RenameNodeOp{NodeRef: "brief", Title: "x"},
	})
	if ok {
		t.Fatal("mixed ops must not rebase")
	}
	_, ok = nodeConfigWriteTargets(nil)
	if ok {
		t.Fatal("empty ops must not rebase")
	}
}
