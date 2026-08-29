package graph

import "testing"

func TestSelectRunNodeIDsEmptyGraph(t *testing.T) {
	_, err := SelectRunNodeIDs(EmptyGraph, RunScopeGraph, "", nil)
	if err == nil || err.Error() != "没有可运行的处理节点" {
		t.Fatalf("err %v", err)
	}
}

func TestSelectRunNodeIDsNodeScopeRequiresTarget(t *testing.T) {
	_, err := SelectRunNodeIDs(EmptyGraph, RunScopeNode, "", nil)
	if err == nil || err.Error() != "节点运行范围必须指定目标节点" {
		t.Fatalf("err %v", err)
	}
}
