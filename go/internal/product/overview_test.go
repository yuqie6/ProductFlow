package product

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"golang.org/x/crypto/bcrypt"
)

type overviewFixture struct {
	MerchantID       string
	ProductID        string
	SecondProductID  string
	ImageSessionID   string
	AgentActiveID    string
	AgentWaitingID   string
	AgentUnknownID   string
	AgentPausedID    string
	AgentFailedID    string
	AgentOldFailedID string
	AgentSucceededID string
	AgentCanceledID  string
	GraphActiveID    string
	GraphUnknownID   string
	GraphFailedID    string
	GraphOldFailedID string
	GraphSucceededID string
	ImageActiveID    string
	ImageUnknownID   string
	ImageFailedID    string
	ImageOldFailedID string
	ImageSucceededID string
	LocalActiveID    string
	LocalUnknownID   string
	LocalDraftID     string
	LocalFailedID    string
	LocalOldFailedID string
	ExpectedAllCount int64
	ExpectedFailed7  int64
	ExpectedFailed30 int64
}

func loginOverviewUser(t *testing.T, ps *productServer, merchantID, label string) {
	t.Helper()
	now := time.Now().UTC()
	password := "overview-password-" + label
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	user := schema.Users{
		ID:           clockid.New(),
		Email:        "overview-" + label + "@example.test",
		PasswordHash: string(hash),
		DisplayName:  "概览用户-" + label,
		IsOperator:   false,
		MerchantID:   &merchantID,
		Status:       auth.UserStatusActive,
		Locale:       "zh-CN",
		Theme:        "system",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := ps.db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = ps.db.Where("user_id = ?", user.ID).Delete(&schema.AuthSessions{}).Error
		_ = ps.db.Where("id = ?", user.ID).Delete(&schema.Users{}).Error
	})

	body := strings.NewReader(`{"email":"` + user.Email + `","password":"` + password + `"}`)
	req, err := http.NewRequest(http.MethodPost, ps.srv.URL+"/api/auth/session", body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := ps.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("overview user login %d %s", resp.StatusCode, raw)
	}
	ps.cookies = resp.Cookies()
}

func overviewString(value string) *string { return &value }

func overviewTime(value time.Time) *time.Time { return &value }

func seedProductOverview(t *testing.T, ps *productServer, merchantID string, now time.Time) overviewFixture {
	t.Helper()
	db := ps.db
	old := now.Add(-40 * 24 * time.Hour)
	old30 := now.Add(-20 * 24 * time.Hour)
	recent := now.Add(-7 * 24 * time.Hour)
	recent2 := now.Add(-2 * 24 * time.Hour)

	productID := clockid.New()
	secondProductID := clockid.New()
	foreignMerchantID := clockid.New()
	foreignProductID := clockid.New()
	products := []schema.Products{
		{ID: productID, MerchantID: merchantID, Name: "Dashboard Product A", CreatedAt: old, UpdatedAt: old},
		{ID: secondProductID, MerchantID: merchantID, Name: "Dashboard Product B", CreatedAt: old, UpdatedAt: old},
		{ID: foreignProductID, MerchantID: foreignMerchantID, Name: "Other Merchant Product", CreatedAt: old, UpdatedAt: old},
	}
	foreignMerchant := schema.Merchants{ID: foreignMerchantID, Name: "概览他商", Status: auth.MerchantStatusActive, CreatedAt: old, UpdatedAt: old}
	if err := db.Create(&foreignMerchant).Error; err != nil {
		t.Fatal(err)
	}
	for i := range products {
		if err := db.Create(&products[i]).Error; err != nil {
			t.Fatal(err)
		}
	}

	byteSize := int64(64)
	width, height := 8, 8
	sha := strings.Repeat("d", 64)
	mediaID := clockid.New()
	maskMediaID := clockid.New()
	verifiedAt := old
	for _, id := range []string{mediaID, maskMediaID} {
		if err := db.Create(&schema.MediaObjects{
			ID: id, StoragePath: "overview/" + id + ".png", MIMEType: "image/png", ByteSize: &byteSize,
			Width: &width, Height: &height, SHA256: &sha, VerificationStatus: "verified", CreatedAt: old,
			VerifiedAt: &verifiedAt,
		}).Error; err != nil {
			t.Fatal(err)
		}
	}
	assetID := clockid.New()
	if err := db.Create(&schema.ProductImageAssets{
		ID: assetID, ProductID: productID, MediaObjectID: mediaID, OriginType: "upload",
		DisplayName: "Dashboard source", OriginalFilename: "source.png", CreatedAt: old, UpdatedAt: old,
	}).Error; err != nil {
		t.Fatal(err)
	}

	oldAdoptionID := clockid.New()
	currentAdoptionID := clockid.New()
	foreignAdoptionID := clockid.New()
	for _, version := range []schema.DeliveryAdoptionVersions{
		{ID: oldAdoptionID, ProductID: productID, Version: 1, CreatedAt: old},
		{ID: currentAdoptionID, ProductID: productID, Version: 2, CreatedAt: recent},
		{ID: foreignAdoptionID, ProductID: foreignProductID, Version: 1, CreatedAt: old},
	} {
		if err := db.Create(&version).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Model(&schema.Products{}).Where("id = ?", productID).Update("current_delivery_adoption_version_id", currentAdoptionID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&schema.Products{}).Where("id = ?", secondProductID).Update("current_delivery_adoption_version_id", foreignAdoptionID).Error; err != nil {
		t.Fatal(err)
	}

	agentSessionID := clockid.New()
	foreignAgentSessionID := clockid.New()
	for _, session := range []schema.AgentSessions{
		{ID: agentSessionID, MerchantID: merchantID, Title: "Dashboard Agent", Status: "active", CreatedAt: old, UpdatedAt: old, ActivityAt: old, ProductID: &productID},
		{ID: foreignAgentSessionID, MerchantID: foreignMerchantID, Title: "Other Agent", Status: "active", CreatedAt: old, UpdatedAt: old, ActivityAt: old, ProductID: &foreignProductID},
	} {
		if err := db.Create(&session).Error; err != nil {
			t.Fatal(err)
		}
	}

	fixture := overviewFixture{
		MerchantID:       merchantID,
		ProductID:        productID,
		SecondProductID:  secondProductID,
		ExpectedAllCount: 23,
		ExpectedFailed7:  4,
		ExpectedFailed30: 8,
	}
	addAgent := func(status string, created time.Time, failure, finished, canceled *time.Time, product *string) string {
		id := clockid.New()
		var failureReason *string
		if failure != nil {
			failureReason = overviewString("private provider payload")
		}
		row := schema.AgentTasks{
			ID: id, MerchantID: merchantID, SessionID: agentSessionID, ProductID: product,
			HarnessRunID: clockid.New(), Title: "Agent " + status, Goal: "private goal", Status: status,
			FailureReason: failureReason, CreatedAt: created, UpdatedAt: created,
			FinishedAt: finished, CanceledAt: canceled,
		}
		if status == "failed" && failure == nil {
			t.Fatalf("failed agent fixture needs failure")
		}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
		return id
	}
	fixture.AgentActiveID = addAgent("queued", old, nil, nil, nil, &productID)
	fixture.AgentWaitingID = addAgent("waiting_user", old, nil, nil, nil, &productID)
	fixture.AgentUnknownID = addAgent("unknown", old, nil, nil, nil, &productID)
	fixture.AgentPausedID = addAgent("paused", old, nil, nil, nil, &productID)
	fixture.AgentFailedID = addAgent("failed", recent, overviewTime(recent), overviewTime(recent), nil, &productID)
	fixture.AgentOldFailedID = addAgent("failed", old30, overviewTime(old30), overviewTime(old30), nil, &productID)
	fixture.AgentSucceededID = addAgent("succeeded", recent2, nil, overviewTime(recent2), nil, &productID)
	canceledAt := recent2
	fixture.AgentCanceledID = addAgent("canceled", recent2, nil, nil, &canceledAt, &productID)

	foreignTaskID := clockid.New()
	if err := db.Create(&schema.AgentTasks{
		ID: foreignTaskID, MerchantID: foreignMerchantID, SessionID: foreignAgentSessionID, ProductID: &foreignProductID,
		HarnessRunID: clockid.New(), Title: "Other task", Goal: "other goal", Status: "running",
		CreatedAt: old, UpdatedAt: old,
	}).Error; err != nil {
		t.Fatal(err)
	}

	graphID := clockid.New()
	if err := db.Create(&schema.WorkflowGraphs{
		ID: graphID, ProductID: productID, Title: "Dashboard Graph", Active: true,
		SchemaVersion: 3, Revision: 1, CreatedAt: old, UpdatedAt: old,
	}).Error; err != nil {
		t.Fatal(err)
	}
	addGraph := func(status string, started time.Time, failure, finished *time.Time) string {
		id := clockid.New()
		var failureReason *string
		if failure != nil {
			failureReason = overviewString("private graph payload")
		}
		row := schema.WorkflowGraphRuns{
			ID: id, GraphID: graphID, Status: status, RunScope: "graph", GraphRevision: 1,
			SnapshotJSON: "{}", FailureReason: failureReason, IsRetryable: true,
			StartedAt: started, FinishedAt: finished,
		}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
		return id
	}
	fixture.GraphActiveID = addGraph("running", old, nil, nil)
	fixture.GraphUnknownID = addGraph("unknown", old, nil, nil)
	fixture.GraphFailedID = addGraph("failed", recent, overviewTime(recent), overviewTime(recent))
	fixture.GraphOldFailedID = addGraph("failed", old30, overviewTime(old30), overviewTime(old30))
	fixture.GraphSucceededID = addGraph("succeeded", recent2, nil, overviewTime(recent2))

	imageSessionID := clockid.New()
	fixture.ImageSessionID = imageSessionID
	if err := db.Create(&schema.ImageSessions{ID: imageSessionID, MerchantID: merchantID, Title: "Dashboard Images", CreatedAt: old, UpdatedAt: old}).Error; err != nil {
		t.Fatal(err)
	}
	addImage := func(status string, created time.Time, failure, finished *time.Time) string {
		id := clockid.New()
		var failureReason *string
		if failure != nil {
			failureReason = overviewString("private image provider payload")
		}
		row := schema.ImageSessionGenerationTasks{
			ID: id, SessionID: imageSessionID, Status: status, Prompt: "private prompt", Size: "1024x1024",
			GenerationCount: 1, FailureReason: failureReason, CreatedAt: created, FinishedAt: finished,
		}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
		return id
	}
	fixture.ImageActiveID = addImage("queued", old, nil, nil)
	fixture.ImageUnknownID = addImage("unknown", old, nil, nil)
	fixture.ImageFailedID = addImage("failed", now, overviewTime(now), overviewTime(now))
	fixture.ImageOldFailedID = addImage("failed", old30, overviewTime(old30), overviewTime(old30))
	fixture.ImageSucceededID = addImage("succeeded", recent2, nil, overviewTime(recent2))

	addLocal := func(status string, created time.Time, failure, finished *time.Time) string {
		id := clockid.New()
		var failureReason *string
		if failure != nil {
			failureReason = overviewString("private local provider payload")
		}
		row := schema.LocalImageEditTasks{
			ID: id, ProductID: productID, SourceAssetID: assetID, SourceMediaSHA256: sha,
			MaskMediaObjectID: maskMediaID, Operation: "remove", MaskGeometryJSON: "{}", Status: status,
			Revision: 1, Attempts: 0, IsRetryable: true, FailureReason: failureReason,
			CreatedAt: created, UpdatedAt: created, FinishedAt: finished,
		}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
		return id
	}
	fixture.LocalActiveID = addLocal("queued", old, nil, nil)
	fixture.LocalUnknownID = addLocal("unknown", old, nil, nil)
	fixture.LocalDraftID = addLocal("draft", old, nil, nil)
	fixture.LocalFailedID = addLocal("failed", recent, overviewTime(recent), overviewTime(recent))
	fixture.LocalOldFailedID = addLocal("failed", old30, overviewTime(old30), overviewTime(old30))

	t.Cleanup(func() {
		_ = db.Where("id = ?", foreignTaskID).Delete(&schema.AgentTasks{}).Error
		_ = db.Where("session_id IN ?", []string{agentSessionID, foreignAgentSessionID}).Delete(&schema.AgentTasks{}).Error
		_ = db.Where("id IN ?", []string{agentSessionID, foreignAgentSessionID}).Delete(&schema.AgentSessions{}).Error
		_ = db.Where("graph_id = ?", graphID).Delete(&schema.WorkflowGraphRuns{}).Error
		_ = db.Where("id = ?", graphID).Delete(&schema.WorkflowGraphs{}).Error
		_ = db.Where("session_id = ?", imageSessionID).Delete(&schema.ImageSessionGenerationTasks{}).Error
		_ = db.Where("id = ?", imageSessionID).Delete(&schema.ImageSessions{}).Error
		_ = db.Where("product_id = ?", productID).Delete(&schema.LocalImageEditTasks{}).Error
		_ = db.Where("id IN ?", []string{productID, secondProductID, foreignProductID}).Delete(&schema.Products{}).Error
		_ = db.Where("id IN ?", []string{oldAdoptionID, currentAdoptionID, foreignAdoptionID}).Delete(&schema.DeliveryAdoptionVersions{}).Error
		_ = db.Where("id = ?", assetID).Delete(&schema.ProductImageAssets{}).Error
		_ = db.Where("id IN ?", []string{mediaID, maskMediaID}).Delete(&schema.MediaObjects{}).Error
		_ = db.Where("id = ?", foreignMerchantID).Delete(&schema.Merchants{}).Error
	})

	return fixture
}

func decodeOverview(t *testing.T, resp *http.Response, wantStatus int) ProductOverviewResponse {
	t.Helper()
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != wantStatus {
		t.Fatalf("overview status=%d want=%d body=%s", resp.StatusCode, wantStatus, raw)
	}
	var out ProductOverviewResponse
	if wantStatus == http.StatusOK {
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("decode overview: %v body=%s", err, raw)
		}
	}
	return out
}

func overviewRecordMap(items []ProductOverviewRecord) map[string]ProductOverviewRecord {
	out := make(map[string]ProductOverviewRecord, len(items))
	for _, item := range items {
		out[item.ID] = item
	}
	return out
}

func TestProductOverviewFourSourcesAndFilters(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	ps := newProductServerWith(t, Service{Now: freeze(now)})
	merchantID := clockid.New()
	if err := ps.db.Create(&schema.Merchants{ID: merchantID, Name: "概览商家", Status: auth.MerchantStatusActive, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ps.db.Where("id = ?", merchantID).Delete(&schema.Merchants{}).Error })
	loginOverviewUser(t, ps, merchantID, "records")
	fixture := seedProductOverview(t, ps, merchantID, now)

	all := decodeOverview(t, ps.do(t, http.MethodGet, "/api/v2/products/overview?page_size=100", nil, ""), http.StatusOK)
	if !all.AsOf.Equal(now) || !all.RecentWindow.From.Equal(now.Add(-30*24*time.Hour)) || !all.RecentWindow.To.Equal(now) {
		t.Fatalf("time window %#v", all)
	}
	if all.Days != 30 || all.Kind != "all" || all.State != "all" || all.RecentWindow.Days != 30 {
		t.Fatalf("query echo %#v", all)
	}
	if all.Products.Total != 2 || all.Products.CurrentAdopted != 1 {
		t.Fatalf("product counts %#v", all.Products)
	}
	if all.Work.Records.Total != fixture.ExpectedAllCount || len(all.Work.Records.Items) != int(fixture.ExpectedAllCount) || all.Work.Records.Page != 1 || all.Work.Records.PageSize != 100 {
		t.Fatalf("records page %#v", all.Work.Records)
	}
	counts := all.Work.BySource
	if counts.AgentTask != (ProductOverviewSourceCounts{Active: 1, Waiting: 1, Unknown: 1, RecentFailed: 2}) ||
		counts.WorkflowRun != (ProductOverviewSourceCounts{Active: 1, Unknown: 1, RecentFailed: 2}) ||
		counts.ImageSession != (ProductOverviewSourceCounts{Active: 1, Unknown: 1, RecentFailed: 2}) ||
		counts.LocalEdit != (ProductOverviewSourceCounts{Active: 1, Unknown: 1, RecentFailed: 2}) {
		t.Fatalf("source counts %#v", counts)
	}
	records := overviewRecordMap(all.Work.Records.Items)
	for _, id := range []string{fixture.AgentOldFailedID, fixture.GraphOldFailedID, fixture.ImageOldFailedID, fixture.LocalOldFailedID} {
		if _, ok := records[id]; !ok {
			t.Fatalf("30-day terminal record missing from default window: %s", id)
		}
	}
	for _, id := range []string{fixture.AgentPausedID, fixture.LocalDraftID, fixture.AgentCanceledID} {
		if _, ok := records[id]; !ok {
			t.Fatalf("current/nonterminal or recent cancelled record missing: %s", id)
		}
	}
	agent := records[fixture.AgentActiveID]
	if agent.ProductID == nil || *agent.ProductID != fixture.ProductID || agent.ProductName == nil || *agent.ProductName != "Dashboard Product A" || agent.SessionID == nil {
		t.Fatalf("agent identity projection %#v", agent)
	}
	image := records[fixture.ImageActiveID]
	if image.ProductID != nil || image.ProductName != nil || image.SessionID == nil || *image.SessionID != fixture.ImageSessionID {
		t.Fatalf("image identity projection %#v", image)
	}
	failed := records[fixture.AgentFailedID]
	if failed.FailureReason == nil || *failed.FailureReason != publicWorkFailureReason || strings.Contains(*failed.FailureReason, "provider") {
		t.Fatalf("failure redaction %#v", failed)
	}
	canceled := records[fixture.AgentCanceledID]
	if canceled.FinishedAt != nil {
		t.Fatalf("raw nullable finished_at invented %#v", canceled)
	}

	all7 := decodeOverview(t, ps.do(t, http.MethodGet, "/api/v2/products/overview?days=7&page_size=100", nil, ""), http.StatusOK)
	if all7.Work.BySource.AgentTask.RecentFailed != 1 || all7.Work.BySource.WorkflowRun.RecentFailed != 1 || all7.Work.BySource.ImageSession.RecentFailed != 1 || all7.Work.BySource.LocalEdit.RecentFailed != 1 {
		t.Fatalf("7-day source counts %#v", all7.Work.BySource)
	}

	filtered := decodeOverview(t, ps.do(t, http.MethodGet, "/api/v2/products/overview?days=7&kind=image_session&state=failed&page=1&page_size=1", nil, ""), http.StatusOK)
	if filtered.Days != 7 || filtered.Kind != "image_session" || filtered.State != "failed" || filtered.Work.Records.Total != 1 || len(filtered.Work.Records.Items) != 1 || filtered.Work.Records.Items[0].ID != fixture.ImageFailedID {
		t.Fatalf("filtered image failures %#v", filtered)
	}
	if filtered.Products != all.Products || filtered.Work.BySource != all7.Work.BySource {
		t.Fatalf("filter changed merchant-wide stats products=%#v source=%#v", filtered.Products, filtered.Work.BySource)
	}

	active := decodeOverview(t, ps.do(t, http.MethodGet, "/api/v2/products/overview?days=7&state=active&page_size=100", nil, ""), http.StatusOK)
	if active.Work.Records.Total != 4 {
		t.Fatalf("active total=%d", active.Work.Records.Total)
	}
	waiting := decodeOverview(t, ps.do(t, http.MethodGet, "/api/v2/products/overview?state=waiting&page_size=100", nil, ""), http.StatusOK)
	if waiting.Work.Records.Total != 1 || waiting.Work.Records.Items[0].ID != fixture.AgentWaitingID {
		t.Fatalf("waiting records %#v", waiting.Work.Records)
	}
	unknown := decodeOverview(t, ps.do(t, http.MethodGet, "/api/v2/products/overview?state=unknown&page_size=100", nil, ""), http.StatusOK)
	if unknown.Work.Records.Total != 4 {
		t.Fatalf("unknown total=%d", unknown.Work.Records.Total)
	}
	failed7 := decodeOverview(t, ps.do(t, http.MethodGet, "/api/v2/products/overview?days=7&state=failed&page_size=100", nil, ""), http.StatusOK)
	if failed7.Work.Records.Total != fixture.ExpectedFailed7 {
		t.Fatalf("failed7 total=%d", failed7.Work.Records.Total)
	}
	for _, item := range failed7.Work.Records.Items {
		if item.ID == fixture.AgentOldFailedID || item.ID == fixture.GraphOldFailedID || item.ID == fixture.ImageOldFailedID || item.ID == fixture.LocalOldFailedID {
			t.Fatalf("old failed record leaked into 7-day window: %s", item.ID)
		}
	}
	failed30 := decodeOverview(t, ps.do(t, http.MethodGet, "/api/v2/products/overview?days=30&state=failed&page_size=100", nil, ""), http.StatusOK)
	if failed30.Work.Records.Total != fixture.ExpectedFailed30 {
		t.Fatalf("failed30 total=%d", failed30.Work.Records.Total)
	}

	page := decodeOverview(t, ps.do(t, http.MethodGet, "/api/v2/products/overview?page=2&page_size=4", nil, ""), http.StatusOK)
	if page.Work.Records.Total != fixture.ExpectedAllCount || page.Work.Records.Page != 2 || page.Work.Records.PageSize != 4 || len(page.Work.Records.Items) != 4 {
		t.Fatalf("pagination %#v", page.Work.Records)
	}
	firstIDs := map[string]bool{}
	for _, item := range all.Work.Records.Items[:4] {
		firstIDs[item.ID] = true
	}
	for _, item := range page.Work.Records.Items {
		if firstIDs[item.ID] {
			t.Fatalf("pagination repeated item %s", item.ID)
		}
	}
}

func TestProductOverviewRejectsInvalidQueryAndNoMerchant(t *testing.T) {
	ps := newProductServer(t)
	for _, query := range []string{
		"?days=8", "?days=", "?kind=", "?kind=bogus", "?state=", "?state=bogus", "?page=0", "?page_size=101",
	} {
		decodeOverview(t, ps.do(t, http.MethodGet, "/api/v2/products/overview"+query, nil, ""), http.StatusBadRequest)
	}

	now := time.Now().UTC()
	password := "overview-operator-password"
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	user := schema.Users{
		ID: clockid.New(), Email: "overview-no-merchant@example.test", PasswordHash: string(hash), DisplayName: "无商家 Operator",
		IsOperator: true, Status: auth.UserStatusActive, Locale: "zh-CN", Theme: "system", CreatedAt: now, UpdatedAt: now,
	}
	if err := ps.db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = ps.db.Where("user_id = ?", user.ID).Delete(&schema.AuthSessions{}).Error
		_ = ps.db.Where("id = ?", user.ID).Delete(&schema.Users{}).Error
	})
	body := strings.NewReader(`{"email":"` + user.Email + `","password":"` + password + `"}`)
	req, err := http.NewRequest(http.MethodPost, ps.srv.URL+"/api/auth/session", body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := ps.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("operator login %d", resp.StatusCode)
	}
	ps.cookies = resp.Cookies()
	decodeOverview(t, ps.do(t, http.MethodGet, "/api/v2/products/overview", nil, ""), http.StatusForbidden)
}

func TestProductOverviewEmptySetUsesStableShape(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	ps := newProductServerWith(t, Service{Now: freeze(now)})
	merchantID := clockid.New()
	if err := ps.db.Create(&schema.Merchants{ID: merchantID, Name: "空概览商家", Status: auth.MerchantStatusActive, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ps.db.Where("id = ?", merchantID).Delete(&schema.Merchants{}).Error })
	loginOverviewUser(t, ps, merchantID, "empty")

	out := decodeOverview(t, ps.do(t, http.MethodGet, "/api/v2/products/overview", nil, ""), http.StatusOK)
	if out.Products.Total != 0 || out.Products.CurrentAdopted != 0 || out.Work.Records.Total != 0 || len(out.Work.Records.Items) != 0 {
		t.Fatalf("empty overview counts %#v", out)
	}
	if out.Work.Records.Items == nil {
		t.Fatal("empty records must encode as []")
	}
	if out.Work.BySource != (ProductOverviewBySource{}) {
		t.Fatalf("empty source counts %#v", out.Work.BySource)
	}
}
