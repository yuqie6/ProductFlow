package quota_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/config"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/httpx"
	"github.com/yuqie6/productflow/internal/platform/testdb"
	"github.com/yuqie6/productflow/internal/quota"
	"github.com/yuqie6/productflow/internal/settings"
	"golang.org/x/crypto/bcrypt"
)

func newOpsReadsQuotaHTTPServer(t *testing.T) *quotaHTTPServer {
	t.Helper()
	pool, gdb := testdb.IsolatedMigrated(t, fmt.Sprintf("pf_ops_reads_0908_quota_%d", time.Now().UnixNano()))
	engine := httpx.NewEngine(nil)
	engine.Use(httpx.Session(httpx.NewCookieStore(httpx.SessionConfig{Secret: "test-session-secret-key"})))
	store := settings.NewStore(pool, config.Config{AdminAccessRequired: true})
	authHTTP := auth.MountTest(engine, gdb, store, auth.TestAdminKey)
	zero := int64(0)
	quota.HTTP{DB: gdb, Auth: authHTTP, TrialUnits: &zero}.Register(engine)
	srv := httptest.NewServer(engine)
	t.Cleanup(srv.Close)
	return &quotaHTTPServer{srv: srv, client: &http.Client{}, db: gdb}
}

func seedOpsQuotaOperatorWithoutMerchant(t *testing.T, qs *quotaHTTPServer) []*http.Cookie {
	t.Helper()
	now := time.Now().UTC()
	password := "ops-read-operator-password"
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	user := schema.Users{
		ID:           clockid.New(),
		Email:        "ops-read-no-home@test.local",
		PasswordHash: string(hash),
		DisplayName:  "无归属 Operator",
		IsOperator:   true,
		Status:       auth.UserStatusActive,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := qs.db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = qs.db.Where("user_id = ?", user.ID).Delete(&schema.AuthSessions{}).Error
		_ = qs.db.Where("id = ?", user.ID).Delete(&schema.Users{}).Error
	})
	req, err := http.NewRequest(http.MethodPost, qs.srv.URL+"/api/auth/session", strings.NewReader(
		`{"email":"ops-read-no-home@test.local","password":"`+password+`"}`,
	))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := qs.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("login operator without merchant %d %s", resp.StatusCode, raw)
	}
	return resp.Cookies()
}

func TestOperatorQuotaEventsReadsLedgerWithPaging(t *testing.T) {
	qs := newOpsReadsQuotaHTTPServer(t)
	dual := seedDualQuotaHTTP(t, qs)
	noHomeOperatorCookies := seedOpsQuotaOperatorWithoutMerchant(t, qs)
	now := time.Now().UTC().Truncate(time.Microsecond)
	holdID := clockid.New()
	if err := qs.db.Create(&schema.MerchantQuotaHolds{
		ID: holdID, MerchantID: dual.MerchantBID, IdempotencyKey: "ops-read-hold",
		AmountUnits: 25, Status: quota.StatusReserved, PriceVersionID: quota.DefaultPriceVersionID,
		CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	reason := "manual adjustment"
	actor := "ops-read-actor"
	holdRef := holdID
	rows := []schema.MerchantQuotaEvents{
		{ID: "ops-read-event-1", MerchantID: dual.MerchantBID, EventType: quota.EventAdjust, AmountUnits: 10, AvailableAfter: 10, ReservedAfter: 0, PriceVersionID: quota.DefaultPriceVersionID, IdempotencyKey: "ops-read-event-key-1", CreatedAt: now, Reason: &reason, ActorUserID: &actor},
		{ID: "ops-read-event-2", MerchantID: dual.MerchantBID, HoldID: &holdRef, EventType: quota.EventReserve, AmountUnits: -25, AvailableAfter: 10, ReservedAfter: 25, PriceVersionID: quota.DefaultPriceVersionID, IdempotencyKey: "ops-read-event-key-2", CreatedAt: now.Add(time.Minute)},
		{ID: "ops-read-event-3", MerchantID: dual.MerchantBID, EventType: quota.EventRelease, AmountUnits: 25, AvailableAfter: 35, ReservedAfter: 0, PriceVersionID: quota.DefaultPriceVersionID, IdempotencyKey: "ops-read-event-key-3", CreatedAt: now.Add(2 * time.Minute)},
	}
	for i := range rows {
		if err := qs.db.Create(&rows[i]).Error; err != nil {
			t.Fatal(err)
		}
	}

	pageOne := qs.do(t, http.MethodGet, "/api/ops/merchants/"+dual.MerchantBID+"/quota/events?page=1&page_size=2", "", noHomeOperatorCookies)
	if pageOne.StatusCode != http.StatusOK {
		t.Fatalf("events page one status %d detail %q", pageOne.StatusCode, readDetail(t, pageOne))
	}
	raw, err := io.ReadAll(pageOne.Body)
	pageOne.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	var page quota.EventPage
	if err := json.Unmarshal(raw, &page); err != nil {
		t.Fatal(err)
	}
	if page.Total != 3 || page.Page != 1 || page.PageSize != 2 || len(page.Items) != 2 {
		t.Fatalf("page metadata %#v", page)
	}
	if page.Items[0].ID != rows[2].ID || page.Items[1].ID != rows[1].ID {
		t.Fatalf("event order %#v", page.Items)
	}
	if page.Items[0].HoldID != nil || page.Items[0].Reason != nil || page.Items[0].ActorUserID != nil {
		t.Fatalf("nullable event projection %#v", page.Items[0])
	}
	if page.Items[1].HoldID == nil || *page.Items[1].HoldID != holdID {
		t.Fatalf("hold projection %#v", page.Items[1])
	}
	if page.Items[1].AvailableAfter != rows[1].AvailableAfter || page.Items[1].ReservedAfter != rows[1].ReservedAfter || page.Items[1].AmountUnits != rows[1].AmountUnits {
		t.Fatalf("ledger balances %#v", page.Items[1])
	}
	var wire struct {
		Items []map[string]json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"hold_id", "reason", "actor_user_id"} {
		if string(wire.Items[0][key]) != "null" {
			t.Fatalf("%s was not explicit null: %s", key, wire.Items[0][key])
		}
	}
	for _, key := range []string{"id", "merchant_id", "hold_id", "event_type", "amount_units", "available_after", "reserved_after", "reason", "actor_user_id", "created_at"} {
		if _, ok := wire.Items[0][key]; !ok {
			t.Fatalf("missing event field %q: %s", key, raw)
		}
	}
	for _, forbidden := range []string{"idempotency_key", "price_version_id"} {
		if _, ok := wire.Items[0][forbidden]; ok {
			t.Fatalf("private event field exposed %q", forbidden)
		}
	}

	pageTwo := qs.do(t, http.MethodGet, "/api/ops/merchants/"+dual.MerchantBID+"/quota/events?page=2&page_size=2", "", noHomeOperatorCookies)
	if pageTwo.StatusCode != http.StatusOK {
		t.Fatalf("events page two status %d detail %q", pageTwo.StatusCode, readDetail(t, pageTwo))
	}
	var pageTwoView quota.EventPage
	if err := json.NewDecoder(pageTwo.Body).Decode(&pageTwoView); err != nil {
		pageTwo.Body.Close()
		t.Fatal(err)
	}
	pageTwo.Body.Close()
	if len(pageTwoView.Items) != 1 || pageTwoView.Items[0].ID != rows[0].ID {
		t.Fatalf("second page %#v", pageTwoView)
	}

	empty := qs.do(t, http.MethodGet, "/api/ops/merchants/"+dual.MerchantBID+"/quota/events?page=3&page_size=2", "", noHomeOperatorCookies)
	if empty.StatusCode != http.StatusOK {
		t.Fatalf("empty page status %d detail %q", empty.StatusCode, readDetail(t, empty))
	}
	var emptyView quota.EventPage
	if err := json.NewDecoder(empty.Body).Decode(&emptyView); err != nil {
		empty.Body.Close()
		t.Fatal(err)
	}
	empty.Body.Close()
	if emptyView.Items == nil || len(emptyView.Items) != 0 || emptyView.Total != 3 {
		t.Fatalf("empty page %#v", emptyView)
	}

	denied := qs.do(t, http.MethodGet, "/api/ops/merchants/"+dual.MerchantBID+"/quota/events", "", dual.CookiesB)
	if denied.StatusCode != http.StatusForbidden {
		t.Fatalf("ordinary event read status %d", denied.StatusCode)
	}
	_ = readDetail(t, denied)

	missing := qs.do(t, http.MethodGet, "/api/ops/merchants/missing-merchant/quota/events", "", noHomeOperatorCookies)
	if missing.StatusCode != http.StatusNotFound {
		t.Fatalf("missing merchant event read status %d", missing.StatusCode)
	}
	if got := readDetail(t, missing); got != auth.CrossMerchantDetail {
		t.Fatalf("missing merchant detail %q", got)
	}

	for _, query := range []string{"?page=0", "?page_size=101"} {
		invalid := qs.do(t, http.MethodGet, "/api/ops/merchants/"+dual.MerchantBID+"/quota/events"+query, "", noHomeOperatorCookies)
		invalid.Body.Close()
		if invalid.StatusCode != http.StatusBadRequest {
			t.Fatalf("invalid event query %s status %d", query, invalid.StatusCode)
		}
	}
}

func TestOperatorAdjustRejectsMismatchedReplayAndInvalidMetadata(t *testing.T) {
	qs := newOpsReadsQuotaHTTPServer(t)
	dual := seedDualQuotaHTTP(t, qs)
	key := "ops-adjust-contract"
	first := qs.do(t, http.MethodPost, "/api/ops/merchants/"+dual.MerchantBID+"/quota/adjust",
		`{"idempotency_key":"`+key+`","delta_units":20,"reason":"  manual grant  "}`, dual.CookiesA)
	if first.StatusCode != http.StatusOK {
		t.Fatalf("first adjust status %d detail %q", first.StatusCode, readDetail(t, first))
	}
	firstView := readAccount(t, first)
	if firstView.AvailableUnits != 20 {
		t.Fatalf("first account %#v", firstView)
	}

	replay := qs.do(t, http.MethodPost, "/api/ops/merchants/"+dual.MerchantBID+"/quota/adjust",
		`{"idempotency_key":"`+key+`","delta_units":20,"reason":"manual grant"}`, dual.CookiesA)
	if replay.StatusCode != http.StatusOK {
		t.Fatalf("same replay status %d detail %q", replay.StatusCode, readDetail(t, replay))
	}
	if replayView := readAccount(t, replay); replayView.AvailableUnits != 20 {
		t.Fatalf("same replay changed account %#v", replayView)
	}

	for name, body := range map[string]string{
		"delta mismatch":  `{"idempotency_key":"` + key + `","delta_units":21,"reason":"manual grant"}`,
		"reason mismatch": `{"idempotency_key":"` + key + `","delta_units":20,"reason":"different reason"}`,
	} {
		resp := qs.do(t, http.MethodPost, "/api/ops/merchants/"+dual.MerchantBID+"/quota/adjust", body, dual.CookiesA)
		if resp.StatusCode != http.StatusConflict {
			t.Fatalf("%s status %d detail %q", name, resp.StatusCode, readDetail(t, resp))
		}
		_ = readDetail(t, resp)
	}

	service := &quota.Service{DB: qs.db}
	actorMismatch, err := service.Adjust(context.Background(), dual.MerchantBID, key, 20, "manual grant", "different-actor")
	if err == nil {
		t.Fatalf("actor mismatch unexpectedly succeeded: %#v", actorMismatch)
	}
	var appErr apperr.Error
	if !errors.As(err, &appErr) || appErr.Status != http.StatusConflict {
		t.Fatalf("actor mismatch error %T %v", err, err)
	}

	invalid := []string{
		`{"idempotency_key":"invalid-empty-reason","delta_units":1,"reason":"   "}`,
		`{"idempotency_key":"` + strings.Repeat("k", 201) + `","delta_units":1,"reason":"reason"}`,
		`{"idempotency_key":"invalid-long-reason","delta_units":1,"reason":"` + strings.Repeat("r", 2001) + `"}`,
	}
	for _, body := range invalid {
		resp := qs.do(t, http.MethodPost, "/api/ops/merchants/"+dual.MerchantBID+"/quota/adjust", body, dual.CookiesA)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("invalid adjust metadata status %d detail %q", resp.StatusCode, readDetail(t, resp))
		}
		_ = readDetail(t, resp)
	}

	var eventCount int64
	if err := qs.db.Model(&schema.MerchantQuotaEvents{}).
		Where("merchant_id = ? AND event_type = ? AND idempotency_key = ?", dual.MerchantBID, quota.EventAdjust, key).
		Count(&eventCount).Error; err != nil {
		t.Fatal(err)
	}
	if eventCount != 1 {
		t.Fatalf("mismatched replay changed event count: %d", eventCount)
	}
	account, err := service.GetAccount(context.Background(), dual.MerchantBID)
	if err != nil {
		t.Fatal(err)
	}
	if account.AvailableUnits != 20 {
		t.Fatalf("mismatched replay changed balance: %#v", account)
	}
}
