package graph

import (
	"context"
	"reflect"
	"testing"

	"gorm.io/gorm"
)

type batchSourceGuard struct {
	sources       map[string]*SourceProduct
	factSets      map[string]*FactSet
	sourceBatches [][]string
	factBatches   [][]string
}

func (g *batchSourceGuard) Lock(context.Context, *gorm.DB, string) error                { return nil }
func (g *batchSourceGuard) HasAssets(context.Context, *gorm.DB, string, []string) error { return nil }
func (g *batchSourceGuard) LoadSource(_ context.Context, _ *gorm.DB, productID string) (*SourceProduct, error) {
	return g.sources[productID], nil
}
func (g *batchSourceGuard) LoadSources(_ context.Context, _ *gorm.DB, productIDs []string) (map[string]*SourceProduct, error) {
	g.sourceBatches = append(g.sourceBatches, append([]string(nil), productIDs...))
	out := map[string]*SourceProduct{}
	for _, productID := range productIDs {
		if source := g.sources[productID]; source != nil {
			out[productID] = source
		}
	}
	return out, nil
}
func (g *batchSourceGuard) LoadFactSet(_ context.Context, _ *gorm.DB, factSetID string, _ string) (*FactSet, error) {
	return g.factSets[factSetID], nil
}
func (g *batchSourceGuard) LoadFactSets(_ context.Context, _ *gorm.DB, factSetIDs []string) (map[string]*FactSet, error) {
	g.factBatches = append(g.factBatches, append([]string(nil), factSetIDs...))
	out := map[string]*FactSet{}
	for _, factSetID := range factSetIDs {
		if set := g.factSets[factSetID]; set != nil {
			out[factSetID] = set
		}
	}
	return out, nil
}
func (g *batchSourceGuard) BoundAssetMetas(context.Context, *gorm.DB, string, []string) (map[string]BoundAssetMetadata, error) {
	return map[string]BoundAssetMetadata{}, nil
}

func TestLoadProductSourceSnapshotsBatchesProductAndFactReads(t *testing.T) {
	factOne := &FactSet{ID: "fact-1", ProductID: "source-1", Version: 1, Facts: []map[string]any{{"key": "color", "value": "red"}}}
	factTwo := &FactSet{ID: "fact-2", ProductID: "source-2", Version: 2, Facts: []map[string]any{{"key": "size", "value": "M"}}}
	guard := &batchSourceGuard{
		sources: map[string]*SourceProduct{
			"source-1": {ID: "source-1", Name: "One", CurrentFactSetID: &factOne.ID},
			"source-2": {ID: "source-2", Name: "Two", CurrentFactSetID: &factTwo.ID},
		},
		factSets: map[string]*FactSet{"fact-1": factOne, "fact-2": factTwo},
	}
	ctx := WithProductGuard(context.Background(), guard)
	nodes := []AppliedNode{
		{ID: "node-1", NodeType: NodeProductSource, Config: map[string]any{"source_product_id": "source-1"}},
		{ID: "node-2", NodeType: NodeProductSource, Config: map[string]any{"source_product_id": "source-1", "fact_set_version_id": "fact-1"}},
		{ID: "node-3", NodeType: NodeProductSource, Config: map[string]any{"source_product_id": "source-2"}},
	}

	got, err := loadProductSourceSnapshots(ctx, nil, "graph-product", nodes)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(guard.sourceBatches, [][]string{{"source-1", "source-2"}}) {
		t.Fatalf("source batches %+v", guard.sourceBatches)
	}
	if !reflect.DeepEqual(guard.factBatches, [][]string{{"fact-1", "fact-2"}}) {
		t.Fatalf("fact batches %+v", guard.factBatches)
	}
	for _, nodeID := range []string{"node-1", "node-2", "node-3"} {
		if got[nodeID].SourceProduct == nil || got[nodeID].FactSetVersion == nil {
			t.Fatalf("node %s snapshot %+v", nodeID, got[nodeID])
		}
	}
	if got["node-1"].FactSetVersionID == nil || *got["node-1"].FactSetVersionID != "fact-1" {
		t.Fatalf("implicit fact set %+v", got["node-1"])
	}
	if got["node-3"].FactSetVersionID == nil || *got["node-3"].FactSetVersionID != "fact-2" {
		t.Fatalf("current fact set %+v", got["node-3"])
	}
}
