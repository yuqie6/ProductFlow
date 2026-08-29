package recipe

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/platform/apperr"
)

func TestPayloadCanonicalJSONAndHash(t *testing.T) {
	payload := Payload{
		SchemaVersion: 3,
		Nodes: []PayloadNode{{
			Key:      "product",
			NodeType: graph.NodeProductSource,
			Title:    "商品资料",
			Config: map[string]any{
				"source_product_id":   nil,
				"fact_set_version_id": nil,
			},
		}},
		Edges:  []PayloadEdge{},
		Groups: []PayloadGroup{},
	}
	if err := validatePayload(payload); err != nil {
		t.Fatal(err)
	}
	encoded, err := payloadJSON(payload)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"edges":[],"groups":[],"nodes":[{"config":{"fact_set_version_id":null,"source_product_id":null},"group_key":null,"key":"product","node_type":"product_source","position_x":0,"position_y":0,"title":"商品资料"}],"schema_version":3}`
	if string(encoded) != want {
		t.Fatalf("canonical JSON\n got %s\nwant %s", encoded, want)
	}
	sum := sha256.Sum256(encoded)
	got, err := payloadHash(payload)
	if err != nil {
		t.Fatal(err)
	}
	if got != hex.EncodeToString(sum[:]) {
		t.Fatalf("hash %s", got)
	}
}

func TestExtractStripsIdentityAndPlanKeys(t *testing.T) {
	applied := graph.AppliedGraph{
		Revision: 1,
		Nodes: []graph.AppliedNode{
			{
				ID:       "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
				NodeType: graph.NodeProductSource,
				Title:    "商品资料",
				Config: map[string]any{
					"source_product_id":   "prod-1",
					"fact_set_version_id": "fact-1",
					"prompt_plan_key":     "retired",
				},
			},
			{
				ID:        "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
				NodeType:  graph.NodePromptGeneration,
				Title:     "主图提示词",
				PositionX: 200,
				Config: map[string]any{
					"image_type_key": "hero",
					"images":         []any{"x"},
					"fact_keys":      []any{"name"},
				},
			},
		},
		Edges: []graph.AppliedEdge{{
			ID:           "cccccccc-cccc-4ccc-8ccc-cccccccccccc",
			SourceNodeID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
			TargetNodeID: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
			DataType:     graph.DataProductFacts,
			Role:         graph.RoleFacts,
		}},
	}
	payload, err := extractPayload(applied, ExtractInput{SourceType: SourceWorkflow})
	if err != nil {
		t.Fatal(err)
	}
	dumped := payloadDict(payload)
	if dumped["schema_version"] != 3 {
		t.Fatalf("%+v", dumped)
	}
	if _, ok := dumped["image_types"]; ok {
		t.Fatal("image_types leaked")
	}
	source := payload.Nodes[0]
	if source.Config["source_product_id"] != nil || source.Config["fact_set_version_id"] != nil {
		t.Fatalf("identity %+v", source.Config)
	}
	if _, ok := source.Config["prompt_plan_key"]; ok {
		t.Fatal("plan key kept")
	}
	prompt := payload.Nodes[1]
	if _, ok := prompt.Config["images"]; ok {
		t.Fatal("images kept")
	}
	if _, ok := prompt.Config["fact_keys"]; ok {
		t.Fatal("fact_keys kept")
	}
}

func TestExtractSelectionKeepsInternalEdges(t *testing.T) {
	applied := graph.AppliedGraph{
		Revision: 1,
		Nodes: []graph.AppliedNode{
			{ID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", NodeType: graph.NodeProductSource, Title: "商品资料", Config: map[string]any{"source_product_id": nil, "fact_set_version_id": nil}},
			{ID: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", NodeType: graph.NodePromptGeneration, Title: "提示词", PositionX: 200, Config: map[string]any{"image_type_key": "hero"}},
			{ID: "cccccccc-cccc-4ccc-8ccc-cccccccccccc", NodeType: graph.NodeImageGeneration, Title: "主图", PositionX: 400, Config: map[string]any{"image_type_key": "hero"}},
		},
		Edges: []graph.AppliedEdge{
			{ID: "dddddddd-dddd-4ddd-8ddd-dddddddddddd", SourceNodeID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", TargetNodeID: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", DataType: graph.DataProductFacts, Role: graph.RoleFacts},
			{ID: "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee", SourceNodeID: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", TargetNodeID: "cccccccc-cccc-4ccc-8ccc-cccccccccccc", DataType: graph.DataPrompt, Role: graph.RolePrompt},
		},
	}
	payload, err := extractPayload(applied, ExtractInput{
		SourceType: SourceSelection,
		NodeIDs:    []string{"bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", "cccccccc-cccc-4ccc-8ccc-cccccccccccc"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(payload.Nodes) != 2 || len(payload.Edges) != 1 {
		t.Fatalf("nodes=%d edges=%d", len(payload.Nodes), len(payload.Edges))
	}
	if payload.Edges[0].SourceNodeKey != "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb" {
		t.Fatalf("%+v", payload.Edges[0])
	}
}

func TestExtractGroupIncludesMembers(t *testing.T) {
	gid := "gggggggg-gggg-4ggg-8ggg-gggggggggggg"
	prompt := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	image := "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	applied := graph.AppliedGraph{
		Groups: []graph.AppliedGroup{{ID: gid, Title: "主图组"}},
		Nodes: []graph.AppliedNode{
			{ID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", NodeType: graph.NodeProductSource, Title: "商品资料", Config: map[string]any{"source_product_id": nil, "fact_set_version_id": nil}},
			{ID: prompt, NodeType: graph.NodePromptGeneration, Title: "提示词", PositionX: 200, GroupID: strPtr(gid), Config: map[string]any{"image_type_key": "hero"}},
			{ID: image, NodeType: graph.NodeImageGeneration, Title: "主图", PositionX: 400, GroupID: strPtr(gid), Config: map[string]any{"image_type_key": "hero"}},
		},
		Edges: []graph.AppliedEdge{
			{ID: "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee", SourceNodeID: prompt, TargetNodeID: image, DataType: graph.DataPrompt, Role: graph.RolePrompt},
		},
	}
	payload, err := extractPayload(applied, ExtractInput{SourceType: SourceGroup, GroupID: strPtr(gid)})
	if err != nil {
		t.Fatal(err)
	}
	if len(payload.Nodes) != 2 || len(payload.Groups) != 1 {
		t.Fatalf("nodes=%d groups=%d", len(payload.Nodes), len(payload.Groups))
	}
	if payload.Groups[0].Title != "主图组" || len(payload.Groups[0].MemberKeys) != 2 {
		t.Fatalf("%+v", payload.Groups[0])
	}
}

func TestExtractSourceValidation(t *testing.T) {
	applied := graph.AppliedGraph{Nodes: []graph.AppliedNode{{ID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", NodeType: graph.NodeProductSource, Title: "商品资料", Config: map[string]any{"source_product_id": nil}}}}
	_, err := extractPayload(applied, ExtractInput{SourceType: SourceWorkflow, NodeIDs: []string{"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"}})
	assertAppErr(t, err, 400, "完整工作流来源不能指定 group_id 或 node_ids")
	_, err = extractPayload(applied, ExtractInput{SourceType: SourceGroup})
	assertAppErr(t, err, 400, "分组来源必须且只能指定 group_id")
	_, err = extractPayload(applied, ExtractInput{SourceType: SourceSelection})
	assertAppErr(t, err, 400, "多选来源必须且只能指定非空 node_ids")
}

func TestNormalizePreviewDigestAndIdempotency(t *testing.T) {
	if _, err := normalizePreviewDigest("xyz"); err == nil {
		t.Fatal("expected digest error")
	}
	got, err := normalizePreviewDigest("  " + strings.Repeat("A", 64) + "  ")
	if err != nil || got != strings.Repeat("a", 64) {
		t.Fatalf("%s %v", got, err)
	}
	if _, err := normalizeIdempotencyKey("   "); err == nil {
		t.Fatal("expected empty key")
	}
	key, err := normalizeIdempotencyKey("  apply-1  ")
	if err != nil || key != "apply-1" {
		t.Fatalf("%s %v", key, err)
	}
}

func TestWrapMergeErr(t *testing.T) {
	err := wrapMergeErr(apperr.Validation("节点类型不兼容，不能创建连线"))
	assertAppErr(t, err, 409, "配方无法合并进当前工作流: 节点类型不兼容，不能创建连线")
	conflict := wrapMergeErr(apperr.Conflict("图 revision 已变化，请刷新后重试"))
	assertAppErr(t, conflict, 409, "图 revision 已变化，请刷新后重试")
}

func assertAppErr(t *testing.T, err error, status int, detail string) {
	t.Helper()
	var e apperr.Error
	if err == nil || !errors.As(err, &e) || e.Status != status || !strings.Contains(e.Detail, detail) {
		t.Fatalf("err %v want %d %s", err, status, detail)
	}
}
