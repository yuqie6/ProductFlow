package imagesession

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/product"
)

// B5：本商 generate/SSE/attach 成功；跨商 attach/download/retry 统一 404（CrossMerchantDetail）。
func TestMerchantImageSessionIsolation(t *testing.T) {
	ss := newSessionServer(t)

	created := ss.doJSON(t, http.MethodPost, "/api/image-sessions", map[string]any{"title": "本商会话"})
	ss.mustStatus(t, created, http.StatusCreated)
	var session DetailResponse
	ss.decode(t, created, &session)

	gen := ss.doJSON(t, http.MethodPost, "/api/image-sessions/"+session.ID+"/generate", map[string]any{
		"prompt": "本商生图", "size": "1024x1024", "generation_count": 1,
	})
	ss.mustStatus(t, gen, http.StatusAccepted)
	ss.decode(t, gen, &session)
	if len(session.GenerationTasks) != 1 {
		t.Fatalf("own generate tasks %d", len(session.GenerationTasks))
	}
	taskID := session.GenerationTasks[0].ID
	ss.dropDispatch(t, taskID)
	if err := (Executor{DB: ss.db, Media: ss.media, Provider: MockChatProvider{}}).Execute(context.Background(), taskID); err != nil {
		t.Fatal(err)
	}
	got := ss.do(t, http.MethodGet, "/api/image-sessions/"+session.ID, nil, "")
	ss.mustStatus(t, got, http.StatusOK)
	ss.decode(t, got, &session)
	if len(session.Rounds) == 0 {
		t.Fatal("own generate produced no round")
	}
	ownAssetID := session.Rounds[0].GeneratedAsset.ID

	sseCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sseReq, err := http.NewRequestWithContext(sseCtx, http.MethodGet, ss.srv.URL+"/api/image-sessions/"+session.ID+"/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range ss.cookies {
		sseReq.AddCookie(c)
	}
	sseResp, err := ss.client.Do(sseReq)
	if err != nil {
		t.Fatal(err)
	}
	if sseResp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(sseResp.Body)
		sseResp.Body.Close()
		t.Fatalf("own SSE %d %s", sseResp.StatusCode, raw)
	}
	cancel()
	_, _ = io.Copy(io.Discard, sseResp.Body)
	sseResp.Body.Close()

	dl := ss.do(t, http.MethodGet, "/api/image-session-assets/"+ownAssetID+"/download", nil, "")
	ss.mustStatus(t, dl, http.StatusOK)
	dl.Body.Close()

	body, ctype := productMultipart(t)
	prod := ss.do(t, http.MethodPost, "/api/v2/products", body, ctype)
	ss.mustStatus(t, prod, http.StatusCreated)
	var createdProd product.CreateResponse
	ss.decode(t, prod, &createdProd)

	attached := ss.doJSON(t, http.MethodPost, "/api/v2/image-sessions/"+session.ID+"/assets/"+ownAssetID+"/attach-to-product", map[string]any{
		"product_id": createdProd.Product.ID,
	})
	ss.mustStatus(t, attached, http.StatusOK)
	attached.Body.Close()

	foreign := seedForeignImageSession(t, ss)

	assertCross404 := func(name string, resp *http.Response) {
		t.Helper()
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("%s want 404 got %d %s", name, resp.StatusCode, raw)
		}
		var body struct {
			Detail string `json:"detail"`
		}
		_ = json.Unmarshal(raw, &body)
		if body.Detail != auth.CrossMerchantDetail {
			t.Fatalf("%s detail=%q want %q body=%s", name, body.Detail, auth.CrossMerchantDetail, raw)
		}
	}

	var beforeAttach int64
	if err := ss.db.Model(&schema.ProductImageAssets{}).Where("product_id = ?", foreign.productID).Count(&beforeAttach).Error; err != nil {
		t.Fatal(err)
	}

	assertCross404("foreign generate", ss.doJSON(t, http.MethodPost, "/api/image-sessions/"+foreign.sessionID+"/generate", map[string]any{
		"prompt": "跨商", "size": "1024x1024", "generation_count": 1,
	}))
	assertCross404("foreign SSE", ss.do(t, http.MethodGet, "/api/image-sessions/"+foreign.sessionID+"/events", nil, ""))
	assertCross404("foreign retry", ss.do(t, http.MethodPost, "/api/image-sessions/"+foreign.sessionID+"/generation-tasks/"+foreign.taskID+"/retry", nil, ""))
	assertCross404("foreign download", ss.do(t, http.MethodGet, "/api/image-session-assets/"+foreign.assetID+"/download", nil, ""))
	assertCross404("foreign attach foreign product", ss.doJSON(t, http.MethodPost,
		"/api/v2/image-sessions/"+foreign.sessionID+"/assets/"+foreign.assetID+"/attach-to-product", map[string]any{
			"product_id": foreign.productID,
		}))
	assertCross404("own session attach foreign product", ss.doJSON(t, http.MethodPost,
		"/api/v2/image-sessions/"+session.ID+"/assets/"+ownAssetID+"/attach-to-product", map[string]any{
			"product_id": foreign.productID,
		}))
	assertCross404("foreign session attach own product", ss.doJSON(t, http.MethodPost,
		"/api/v2/image-sessions/"+foreign.sessionID+"/assets/"+foreign.assetID+"/attach-to-product", map[string]any{
			"product_id": createdProd.Product.ID,
		}))

	var afterAttach int64
	if err := ss.db.Model(&schema.ProductImageAssets{}).Where("product_id = ?", foreign.productID).Count(&afterAttach).Error; err != nil {
		t.Fatal(err)
	}
	if afterAttach != beforeAttach {
		t.Fatalf("cross-merchant attach wrote product assets: before=%d after=%d", beforeAttach, afterAttach)
	}
}

type foreignImageSessionFixture struct {
	merchantID string
	sessionID  string
	assetID    string
	taskID     string
	productID  string
}

func seedForeignImageSession(t *testing.T, ss *sessionServer) foreignImageSessionFixture {
	t.Helper()
	now := time.Now().UTC()
	foreignMerchant := schema.Merchants{
		ID: clockid.New(), Name: "夹具他商-B5-imagesession", Status: "active", CreatedAt: now, UpdatedAt: now,
	}
	if err := ss.db.Create(&foreignMerchant).Error; err != nil {
		t.Fatal(err)
	}

	sessionID := clockid.New()
	if err := ss.db.Create(&schema.ImageSessions{
		ID: sessionID, MerchantID: foreignMerchant.ID, Title: "他商会话", CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}

	byteSize := int64(64)
	width, height := 8, 8
	sha := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	mediaID := clockid.New()
	assetID := clockid.New()
	storagePath := "image-session-foreign/" + mediaID + ".png"
	verifiedAt := now
	if err := ss.db.Create(&schema.MediaObjects{
		ID: mediaID, StoragePath: storagePath, MIMEType: "image/png", ByteSize: &byteSize,
		Width: &width, Height: &height, SHA256: &sha, VerificationStatus: "verified",
		CreatedAt: now, VerifiedAt: &verifiedAt,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := ss.db.Create(&schema.ImageSessionAssets{
		ID: assetID, SessionID: sessionID, Kind: kindGenerated, OriginalFilename: "foreign.png",
		MIMEType: "image/png", StoragePath: storagePath, CreatedAt: now, MediaObjectID: mediaID,
	}).Error; err != nil {
		t.Fatal(err)
	}

	taskID := clockid.New()
	failReason := "夹具失败"
	if err := ss.db.Create(&schema.ImageSessionGenerationTasks{
		ID: taskID, SessionID: sessionID, Status: "failed", Prompt: "他商任务", Size: "1024x1024",
		GenerationCount: 1, CompletedCandidates: 0, Attempts: 1, IsRetryable: true,
		FailureReason: &failReason, CreatedAt: now, FinishedAt: &now,
	}).Error; err != nil {
		t.Fatal(err)
	}

	productID := clockid.New()
	if err := ss.db.Create(&schema.Products{
		ID: productID, MerchantID: foreignMerchant.ID, Name: "他商商品", CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		_ = ss.db.Where("id = ?", taskID).Delete(&schema.ImageSessionGenerationTasks{}).Error
		_ = ss.db.Where("id = ?", assetID).Delete(&schema.ImageSessionAssets{}).Error
		_ = ss.db.Where("id = ?", mediaID).Delete(&schema.MediaObjects{}).Error
		_ = ss.db.Where("id = ?", sessionID).Delete(&schema.ImageSessions{}).Error
		_ = ss.db.Where("id = ?", productID).Delete(&schema.Products{}).Error
		_ = ss.db.Where("id = ?", foreignMerchant.ID).Delete(&schema.Merchants{}).Error
	})

	return foreignImageSessionFixture{
		merchantID: foreignMerchant.ID,
		sessionID:  sessionID,
		assetID:    assetID,
		taskID:     taskID,
		productID:  productID,
	}
}
