package product

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/graph"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"gorm.io/gorm"
)

func TestDeleteProductWithCanvasImageReferences(t *testing.T) {
	for _, scenario := range []string{"reference", "artifact", "rollback", "external_node", "external_artifact", "running"} {
		t.Run(scenario, func(t *testing.T) {
			ps := newProductServer(t)
			ps.enableDeletion(t)
			ctx := ps.merchantCtx(t)
			created, err := ps.svc.CreateDirect(ctx, CreateInput{Name: "待删除画布", Uploads: []Upload{{Filename: "reference.png", MIMEType: "image/png", Content: pngFile(t, 8, 6)}}}, []graph.DirectCreateImageType{{Key: "hero", Quantity: 1}}, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			productID := created.Product.ID
			assetID := created.CreatedAssets[0].ID
			var node schema.WorkflowGraphNodes
			if err := ps.db.Where("bound_image_asset_id = ?", assetID).Take(&node).Error; err != nil {
				t.Fatal(err)
			}
			var asset schema.ProductImageAssets
			if err := ps.db.Where("id = ?", assetID).Take(&asset).Error; err != nil {
				t.Fatal(err)
			}
			var media schema.MediaObjects
			if err := ps.db.Where("id = ?", asset.MediaObjectID).Take(&media).Error; err != nil {
				t.Fatal(err)
			}
			mediaPath := filepath.Join(ps.svc.Media.Files.Root, media.StoragePath)
			artifact := schema.WorkflowGraphArtifacts{ID: clockid.New(), GraphID: node.GraphID, NodeID: &node.ID, ArtifactType: "image", SchemaVersion: 3, GraphRevision: 1, PayloadJSON: `{}`, PayloadHash: strings.Repeat("a", 64), InputDigest: strings.Repeat("b", 64), ProductImageAssetID: &assetID, CreatedAt: time.Now().UTC()}
			if scenario != "reference" {
				if err := ps.db.Create(&artifact).Error; err != nil {
					t.Fatal(err)
				}
			}
			wantStatus := http.StatusNoContent
			var externalNode schema.WorkflowGraphNodes
			var externalArtifact schema.WorkflowGraphArtifacts
			switch scenario {
			case "rollback":
				// 在解绑后故意让商品 DELETE 失败，检查整笔事务和磁盘文件保持完整。
				callback := "test:fail_canvas_product_delete"
				injected := errors.New("injected delete failure")
				if err := ps.db.Callback().Delete().Before("gorm:delete").Register(callback, func(db *gorm.DB) {
					if db.Statement.Table == "products" {
						db.AddError(injected)
					}
				}); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = ps.db.Callback().Delete().Remove(callback) })
				wantStatus = http.StatusInternalServerError
			case "external_node", "external_artifact":
				other := ps.createV2(t, "外部引用商品", nil, 1)
				otherGraph := schema.WorkflowGraphs{ID: clockid.New(), ProductID: other.Product.ID, Title: "外部画布", Active: true, SchemaVersion: 3, Revision: 1, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
				if err := ps.db.Create(&otherGraph).Error; err != nil {
					t.Fatal(err)
				}
				if scenario == "external_node" {
					externalNode = node
					externalNode.ID = clockid.New()
					externalNode.GraphID = otherGraph.ID
					if err := ps.db.Create(&externalNode).Error; err != nil {
						t.Fatal(err)
					}
				} else {
					externalArtifact = artifact
					externalArtifact.ID = clockid.New()
					externalArtifact.GraphID = otherGraph.ID
					externalArtifact.NodeID = nil
					if err := ps.db.Create(&externalArtifact).Error; err != nil {
						t.Fatal(err)
					}
				}
				// 即使存在跨商品引用，清理也不能解除别人的绑定来强行删图。
				wantStatus = http.StatusInternalServerError
			case "running":
				run := schema.WorkflowGraphRuns{ID: clockid.New(), GraphID: node.GraphID, Status: "running", RunScope: "graph", GraphRevision: 1, SnapshotJSON: `{}`, StartedAt: time.Now().UTC()}
				if err := ps.db.Create(&run).Error; err != nil {
					t.Fatal(err)
				}
				wantStatus = http.StatusBadRequest
			}
			resp := ps.do(t, http.MethodDelete, "/api/v2/products/"+productID, nil, "")
			raw, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode != wantStatus {
				t.Fatalf("delete HTTP %d want %d: %s", resp.StatusCode, wantStatus, raw)
			}
			if wantStatus == http.StatusNoContent {
				for table, id := range map[string]string{"products": productID, "workflow_graphs": node.GraphID, "workflow_graph_nodes": node.ID, "product_image_assets": assetID, "media_objects": media.ID, "workflow_graph_artifacts": artifact.ID} {
					var count int64
					if err := ps.db.Table(table).Where("id = ?", id).Count(&count).Error; err != nil {
						t.Fatal(err)
					}
					if count != 0 {
						t.Fatalf("%s still has deleted row %s", table, id)
					}
				}
				if _, err := os.Stat(mediaPath); !os.IsNotExist(err) {
					t.Fatalf("deleted file remains: %v", err)
				}
				return
			}
			if _, err := ps.svc.Get(ctx, productID); err != nil {
				t.Fatal(err)
			}
			if err := ps.db.Where("id = ?", node.ID).Take(&node).Error; err != nil {
				t.Fatal(err)
			}
			if node.BoundImageAssetID == nil || *node.BoundImageAssetID != assetID {
				t.Fatal("failed deletion changed own image binding")
			}
			if err := ps.db.Where("id = ?", artifact.ID).Take(&artifact).Error; err != nil {
				t.Fatal(err)
			}
			if artifact.ProductImageAssetID == nil || *artifact.ProductImageAssetID != assetID {
				t.Fatal("failed deletion changed artifact image binding")
			}
			if content, err := os.ReadFile(mediaPath); err != nil || len(content) == 0 {
				t.Fatalf("failed deletion removed file: %v", err)
			}
			if externalNode.ID != "" {
				if err := ps.db.Where("id = ?", externalNode.ID).Take(&externalNode).Error; err != nil {
					t.Fatal(err)
				}
				if externalNode.BoundImageAssetID == nil || *externalNode.BoundImageAssetID != assetID {
					t.Fatal("external node lost reference")
				}
			}
			if externalArtifact.ID != "" {
				if err := ps.db.Where("id = ?", externalArtifact.ID).Take(&externalArtifact).Error; err != nil {
					t.Fatal(err)
				}
				if externalArtifact.ProductImageAssetID == nil || *externalArtifact.ProductImageAssetID != assetID {
					t.Fatal("external artifact lost reference")
				}
			}
		})
	}
}
