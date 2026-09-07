package graph_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"testing"

	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/product"
)

func TestLocalSubjectComposeSucceedsWithoutImageQuota(t *testing.T) {
	gs := newIsolatedGraphServer(t)
	merchantID := auth.MustDevMerchantID(t, gs.db)
	before := loadQuotaAccount(t, gs.db, merchantID)
	ref := image.NewNRGBA(image.Rect(0, 0, 100, 80))
	draw.Draw(ref, ref.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.Draw(ref, image.Rect(25, 20, 70, 60), image.NewUniform(color.NRGBA{R: 40, G: 80, B: 160, A: 255}), image.Point{}, draw.Src)
	var buf bytes.Buffer
	if err := png.Encode(&buf, ref); err != nil {
		t.Fatal(err)
	}
	productID, graphID := gs.createDirectGraphWithReference(t, `[{"key":"hero","quantity":1}]`, buf.Bytes())
	resp := gs.doJSON(t, "POST", "/api/v3/products/"+productID+"/workflows/"+graphID+"/runs", map[string]any{"scope": "graph"})
	gs.mustStatus(t, resp, 201)
	var run graph.GraphRunResponse
	gs.decode(t, resp, &run)
	provider := &countingImage{}
	gs.executeLocally(t, run.ID, graph.Executor{DB: gs.db, Deps: graph.Dependencies{Prompt: graph.MockPromptProvider{}, Image: provider, Assets: product.Service{DB: gs.db, Media: gs.media}}})
	if provider.imageCalls() != 0 {
		t.Fatalf("compose called image provider %d times", provider.imageCalls())
	}
	var status string
	if err := gs.pool.QueryRow(context.Background(), "SELECT status FROM workflow_graph_runs WHERE id=$1", run.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "succeeded" {
		t.Fatalf("compose status=%s", status)
	}
	artifacts, assets := countGraphImageArtifacts(t, gs, run.ID, productID)
	if artifacts != 1 || assets != 1 {
		t.Fatalf("compose artifacts=%d assets=%d", artifacts, assets)
	}
	after := loadQuotaAccount(t, gs.db, merchantID)
	if before.AvailableUnits != after.AvailableUnits || before.ReservedUnits != after.ReservedUnits {
		t.Fatal("local compose changed image quota")
	}
}
