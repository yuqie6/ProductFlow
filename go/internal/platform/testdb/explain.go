package testdb

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ExplainResult 是 EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) 的顶层计划。
type ExplainResult struct {
	Plan          ExplainNode `json:"Plan"`
	ExecutionTime float64     `json:"Execution Time"`
	PlanningTime  float64     `json:"Planning Time"`
}

// ExplainNode 是计划树节点。只解析闸门需要的字段。
type ExplainNode struct {
	NodeType   string        `json:"Node Type"`
	ActualRows float64       `json:"Actual Rows"`
	Plans      []ExplainNode `json:"Plans"`
}

// ExplainAnalyze 跑一条 SQL 的 ANALYZE JSON 计划并打日志。
func ExplainAnalyze(t *testing.T, ctx context.Context, pool *pgxpool.Pool, query string, args ...any) ExplainResult {
	t.Helper()
	var raw []byte
	if err := pool.QueryRow(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+query, args...).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var result []ExplainResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("decode explain: %v: %s", err, raw)
	}
	if len(result) != 1 {
		t.Fatalf("unexpected explain result: %s", raw)
	}
	t.Logf("explain planning=%.3fms execution=%.3fms plan=%s", result[0].PlanningTime, result[0].ExecutionTime, raw)
	return result[0]
}

// AssertNoSeqScan 要求顶层 ActualRows 匹配，且整棵计划没有 Seq Scan。
func AssertNoSeqScan(t *testing.T, name string, result ExplainResult, wantRows float64) {
	t.Helper()
	if result.Plan.ActualRows != wantRows {
		t.Fatalf("%s returned %.0f rows, want %.0f", name, result.Plan.ActualRows, wantRows)
	}
	nodes := explainNodeTypes(result.Plan)
	for _, node := range nodes {
		if node == "Seq Scan" {
			t.Fatalf("%s used Seq Scan at target scale: %v", name, nodes)
		}
	}
}

func explainNodeTypes(node ExplainNode) []string {
	out := []string{node.NodeType}
	for _, child := range node.Plans {
		out = append(out, explainNodeTypes(child)...)
	}
	return out
}
