package graph

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/platform/canonjson"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/testdb"
	"github.com/yuqie6/productflow/internal/quota"
)

func TestProviderRequestEvidenceMatchesInvocation(t *testing.T) {
	for _, kind := range []string{"prompt", "image"} {
		for _, oversized := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/oversized=%v", kind, oversized), func(t *testing.T) {
				pool, db := testdb.IsolatedMigrated(t, fmt.Sprintf("pf_reqtrace_%d", time.Now().UnixNano()))
				ctx := context.Background()
				merchantID := auth.MustDevMerchantID(t, db)
				attempt := clockid.New()
				phase := "claimed"
				runID := insertGraphRunForMerchant(t, pool, merchantID, time.Now().UTC(), "running", &phase, &attempt)
				var nodeID string
				if err := pool.QueryRow(ctx, "SELECT id FROM workflow_graph_node_runs WHERE graph_run_id=$1", runID).Scan(&nodeID); err != nil {
					t.Fatal(err)
				}
				if _, err := (&quota.Service{DB: db}).Adjust(ctx, merchantID, clockid.New(), 10, "request trace fixture", ""); err != nil {
					t.Fatal(err)
				}
				refs := []ReferenceImage{{AssetID: clockid.New(), Role: "product_identity", MIME: "image/png", Filename: "reference.png", EdgeID: clockid.New(), Bytes: bytes.Repeat([]byte{0xa5}, 256*1024)}}
				title := "provider business input"
				if oversized {
					title = strings.Repeat("x", maxProviderEffectJSONBytes+1)
				}
				promptReq := PromptRequest{NodeType: NodeCreativeBrief, InputDigest: "digest", NodeTitle: title, References: refs, DocumentAction: "rewrite", DocumentSection: "goal", CurrentDocument: map[string]any{"goal": "original"}, Facts: []map[string]any{{"name": "material", "value": "cotton"}}, TextLanguage: "zh-CN"}
				imageReq := ImageRequest{InputDigest: "digest", NodeTitle: title, References: refs, GenerationSpec: map[string]any{"aspect_ratio": "1:1"}, Prompt: map[string]any{"goal": "hero"}, VariationInstruction: "side view", ProduceRoute: "generative", IncomingEdgeIDs: []string{refs[0].EdgeID}}
				calls := 0
				check := func(actual any) {
					calls++
					var raw, storedHash string
					if err := pool.QueryRow(ctx, "SELECT request_json,request_hash FROM workflow_graph_provider_effects WHERE node_run_id=$1", nodeID).Scan(&raw, &storedHash); err != nil {
						t.Fatal(err)
					}
					var evidence struct {
						Request json.RawMessage `json:"request"`
						Hashes  []string        `json:"reference_content_sha256"`
					}
					if err := json.Unmarshal([]byte(raw), &evidence); err != nil {
						t.Fatal(err)
					}
					if len(raw) >= maxProviderEffectJSONBytes {
						t.Fatalf("reference bytes leaked into ledger: %d", len(raw))
					}
					h := sha256.Sum256(refs[0].Bytes)
					if len(evidence.Hashes) != 1 || evidence.Hashes[0] != hex.EncodeToString(h[:]) {
						t.Fatalf("reference fingerprint=%v", evidence.Hashes)
					}
					var value any
					if err := json.Unmarshal([]byte(raw), &value); err != nil {
						t.Fatal(err)
					}
					hash, err := canonjson.SHA256Hex(value)
					if err != nil || hash != storedHash {
						t.Fatalf("stored hash mismatch %v", err)
					}
					if kind == "prompt" {
						var recorded PromptRequest
						if err := json.Unmarshal(evidence.Request, &recorded); err != nil {
							t.Fatal(err)
						}
						if len(recorded.References) != 1 || recorded.References[0].Bytes != nil {
							t.Fatal("invalid reference evidence")
						}
						recorded.References[0].Bytes = refs[0].Bytes
						if !reflect.DeepEqual(recorded, actual) || !reflect.DeepEqual(actual, promptReq) {
							t.Fatal("recorded prompt differs from provider invocation")
						}
					} else {
						var recorded ImageRequest
						if err := json.Unmarshal(evidence.Request, &recorded); err != nil {
							t.Fatal(err)
						}
						if len(recorded.References) != 1 || recorded.References[0].Bytes != nil {
							t.Fatal("invalid reference evidence")
						}
						recorded.References[0].Bytes = refs[0].Bytes
						if !reflect.DeepEqual(recorded, actual) || !reflect.DeepEqual(actual, imageReq) {
							t.Fatal("recorded image differs from provider invocation")
						}
					}
				}
				executor := Executor{Products: cmdTestProducts{}, DB: db}
				node := graphNodeRunRow{ID: nodeID, ActiveAttemptID: &attempt}
				var err error
				if kind == "prompt" {
					_, _, err = executor.callProvider(ctx, runID, node, "fixture", promptReq, func(_ context.Context, req PromptRequest) (PromptResult, error) {
						check(req)
						return PromptResult{}, nil
					})
				} else {
					_, _, err = executor.callImageProvider(ctx, runID, node, "fixture", imageReq, func(_ context.Context, req ImageRequest) (ImageResult, error) { check(req); return ImageResult{}, nil })
				}
				if oversized {
					if err == nil || calls != 0 {
						t.Fatalf("oversized request invoked provider: calls=%d err=%v", calls, err)
					}
					var actualPhase string
					var effects, holds int
					if err := pool.QueryRow(ctx, "SELECT progress_phase FROM workflow_graph_node_runs WHERE id=$1", nodeID).Scan(&actualPhase); err != nil {
						t.Fatal(err)
					}
					if err := pool.QueryRow(ctx, "SELECT count(*) FROM workflow_graph_provider_effects WHERE node_run_id=$1", nodeID).Scan(&effects); err != nil {
						t.Fatal(err)
					}
					if err := pool.QueryRow(ctx, "SELECT count(*) FROM merchant_quota_holds WHERE idempotency_key=$1", imageNodeQuotaKey(nodeID, attempt)).Scan(&holds); err != nil {
						t.Fatal(err)
					}
					if actualPhase != "claimed" || effects != 0 || holds != 0 {
						t.Fatalf("failed evidence crossed boundary: phase=%s effects=%d holds=%d", actualPhase, effects, holds)
					}
				} else if err != nil || calls != 1 {
					t.Fatalf("calls=%d err=%v", calls, err)
				}
			})
		}
	}
}
