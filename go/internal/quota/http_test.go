package quota_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/config"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/httpx"
	"github.com/yuqie6/productflow/internal/platform/testdb"
	"github.com/yuqie6/productflow/internal/quota"
	"github.com/yuqie6/productflow/internal/settings"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type quotaHTTPServer struct {
	srv    *httptest.Server
	client *http.Client
	db     *gorm.DB
}

type dualQuotaFixture struct {
	MerchantAID string
	MerchantBID string
	CookiesA    []*http.Cookie // Operator + Owner of A
	CookiesB    []*http.Cookie // non-Op Owner of B
}

func newQuotaHTTPServer(t *testing.T) *quotaHTTPServer {
	t.Helper()
	pool, gdb := testdb.Open(t)
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

func (qs *quotaHTTPServer) do(t *testing.T, method, path, body string, cookies []*http.Cookie) *http.Response {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, qs.srv.URL+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	resp, err := qs.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func readAccount(t *testing.T, resp *http.Response) quota.AccountView {
	t.Helper()
	defer resp.Body.Close()
	var view quota.AccountView
	if err := json.NewDecoder(resp.Body).Decode(&view); err != nil {
		t.Fatal(err)
	}
	return view
}

func readDetail(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	var payload map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&payload)
	return payload["detail"]
}

// seedDualQuotaHTTP 在可能已有 service 夹具商家的包库上种子双商 + 登录。
// 不用 bootstrap：quota 包测会先留下无用户的 merchants 行，引导会 Conflict。
func seedDualQuotaHTTP(t *testing.T, qs *quotaHTTPServer) dualQuotaFixture {
	t.Helper()
	now := time.Now().UTC()
	suffix := fmt.Sprintf("%d", now.UnixNano())
	emailA := "quota-op-" + suffix + "@test.local"
	emailB := "quota-b-" + suffix + "@test.local"
	passA := "quota-op-password-ok"
	passB := "quota-b-password-ok"

	hashA, err := bcrypt.GenerateFromPassword([]byte(passA), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	hashB, err := bcrypt.GenerateFromPassword([]byte(passB), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}

	userA := schema.Users{
		ID: clockid.New(), Email: emailA, PasswordHash: string(hashA), DisplayName: "Quota Op",
		IsOperator: true, Status: "active", CreatedAt: now, UpdatedAt: now,
	}
	userB := schema.Users{
		ID: clockid.New(), Email: emailB, PasswordHash: string(hashB), DisplayName: "Quota B Owner",
		IsOperator: false, Status: "active", CreatedAt: now, UpdatedAt: now,
	}
	merchantA := schema.Merchants{
		ID: clockid.New(), Name: "quota-http-A-" + suffix, Status: "active", CreatedAt: now, UpdatedAt: now,
	}
	merchantB := schema.Merchants{
		ID: clockid.New(), Name: "quota-http-B-" + suffix, Status: "active", CreatedAt: now, UpdatedAt: now,
	}
	memA := schema.Memberships{
		ID: clockid.New(), MerchantID: merchantA.ID, UserID: userA.ID, Role: "owner",
		Status: "active", CreatedAt: now, UpdatedAt: now,
	}
	memB := schema.Memberships{
		ID: clockid.New(), MerchantID: merchantB.ID, UserID: userB.ID, Role: "owner",
		Status: "active", CreatedAt: now, UpdatedAt: now,
	}
	for _, row := range []any{&userA, &userB, &merchantA, &merchantB, &memA, &memB} {
		if err := qs.db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_ = qs.db.Where("user_id IN ?", []string{userA.ID, userB.ID}).Delete(&schema.AuthSessions{}).Error
		_ = qs.db.Where("id IN ?", []string{memA.ID, memB.ID}).Delete(&schema.Memberships{}).Error
		_ = qs.db.Where("merchant_id IN ?", []string{merchantA.ID, merchantB.ID}).Delete(&schema.MerchantQuotaEvents{}).Error
		_ = qs.db.Where("merchant_id IN ?", []string{merchantA.ID, merchantB.ID}).Delete(&schema.MerchantQuotaHolds{}).Error
		_ = qs.db.Where("merchant_id IN ?", []string{merchantA.ID, merchantB.ID}).Delete(&schema.MerchantQuotaAccounts{}).Error
		_ = qs.db.Where("id IN ?", []string{merchantA.ID, merchantB.ID}).Delete(&schema.Merchants{}).Error
		_ = qs.db.Where("id IN ?", []string{userA.ID, userB.ID}).Delete(&schema.Users{}).Error
	})

	login := func(email, password string) []*http.Cookie {
		t.Helper()
		body := `{"email":"` + email + `","password":"` + password + `"}`
		req, err := http.NewRequest(http.MethodPost, qs.srv.URL+"/api/auth/session", strings.NewReader(body))
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
			t.Fatalf("login %s %d %s", email, resp.StatusCode, raw)
		}
		return resp.Cookies()
	}

	return dualQuotaFixture{
		MerchantAID: merchantA.ID,
		MerchantBID: merchantB.ID,
		CookiesA:    login(emailA, passA),
		CookiesB:    login(emailB, passB),
	}
}

func TestMerchantQuotaCrossMerchantDenied(t *testing.T) {
	qs := newQuotaHTTPServer(t)
	dual := seedDualQuotaHTTP(t, qs)

	deny := qs.do(t, http.MethodGet, "/api/merchants/"+dual.MerchantAID+"/quota", "", dual.CookiesB)
	if deny.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-merchant status %d detail %q", deny.StatusCode, readDetail(t, deny))
	}
	detail := readDetail(t, deny)
	if detail != "不是该商家成员" {
		t.Fatalf("detail %q", detail)
	}

	ok := qs.do(t, http.MethodGet, "/api/merchants/"+dual.MerchantAID+"/quota", "", dual.CookiesA)
	if ok.StatusCode != http.StatusOK {
		t.Fatalf("own merchant status %d detail %q", ok.StatusCode, readDetail(t, ok))
	}
	view := readAccount(t, ok)
	if view.MerchantID != dual.MerchantAID || view.Currency != quota.CurrencyInternalUnits {
		t.Fatalf("view %#v", view)
	}
	if view.AvailableUnits != 0 || view.ReservedUnits != 0 || view.PriceVersionID != quota.DefaultPriceVersionID {
		t.Fatalf("zero projection %#v", view)
	}
}

func TestOpAdjustIdempotentAndInsufficientConflict(t *testing.T) {
	qs := newQuotaHTTPServer(t)
	dual := seedDualQuotaHTTP(t, qs)

	grantBody := `{"idempotency_key":"http-grant-1","delta_units":50,"reason":"trial"}`
	first := qs.do(t, http.MethodPost, "/api/ops/merchants/"+dual.MerchantBID+"/quota/adjust", grantBody, dual.CookiesA)
	if first.StatusCode != http.StatusOK {
		t.Fatalf("adjust %d %s", first.StatusCode, readDetail(t, first))
	}
	afterGrant := readAccount(t, first)
	if afterGrant.AvailableUnits != 50 {
		t.Fatalf("after grant %#v", afterGrant)
	}

	replay := qs.do(t, http.MethodPost, "/api/ops/merchants/"+dual.MerchantBID+"/quota/adjust", grantBody, dual.CookiesA)
	if replay.StatusCode != http.StatusOK {
		t.Fatalf("replay %d %s", replay.StatusCode, readDetail(t, replay))
	}
	afterReplay := readAccount(t, replay)
	if afterReplay.AvailableUnits != 50 {
		t.Fatalf("replay mutated balance %#v", afterReplay)
	}

	readOp := qs.do(t, http.MethodGet, "/api/ops/merchants/"+dual.MerchantBID+"/quota", "", dual.CookiesA)
	if readOp.StatusCode != http.StatusOK {
		t.Fatalf("op read %d %s", readOp.StatusCode, readDetail(t, readOp))
	}
	opView := readAccount(t, readOp)
	if opView.AvailableUnits != 50 || opView.MerchantID != dual.MerchantBID {
		t.Fatalf("op view %#v", opView)
	}

	deny := qs.do(t, http.MethodPost, "/api/ops/merchants/"+dual.MerchantBID+"/quota/adjust",
		`{"idempotency_key":"http-grant-b","delta_units":1,"reason":"nope"}`, dual.CookiesB)
	if deny.StatusCode != http.StatusForbidden {
		t.Fatalf("non-op adjust %d %s", deny.StatusCode, readDetail(t, deny))
	}
	_ = readDetail(t, deny)

	conflict := qs.do(t, http.MethodPost, "/api/ops/merchants/"+dual.MerchantBID+"/quota/adjust",
		`{"idempotency_key":"http-drain-over","delta_units":-51,"reason":"overdraw"}`, dual.CookiesA)
	if conflict.StatusCode != http.StatusConflict {
		t.Fatalf("insufficient status %d %s", conflict.StatusCode, readDetail(t, conflict))
	}
	conflictDetail := readDetail(t, conflict)
	if conflictDetail != "可用额度不足，无法调账" {
		t.Fatalf("conflict detail %q", conflictDetail)
	}
	still := qs.do(t, http.MethodGet, "/api/ops/merchants/"+dual.MerchantBID+"/quota", "", dual.CookiesA)
	stillView := readAccount(t, still)
	if stillView.AvailableUnits != 50 {
		t.Fatalf("conflict mutated balance %#v", stillView)
	}
}

func TestPriceCatalogHTTP(t *testing.T) {
	qs := newQuotaHTTPServer(t)
	dual := seedDualQuotaHTTP(t, qs)

	opDefault := qs.do(t, http.MethodGet, "/api/ops/quota/price-versions/default", "", dual.CookiesA)
	if opDefault.StatusCode != http.StatusOK {
		t.Fatalf("op default %d %s", opDefault.StatusCode, readDetail(t, opDefault))
	}
	opView := readPriceVersion(t, opDefault)
	if opView.PriceVersionID != quota.DefaultPriceVersionID || !opView.IsDefault {
		t.Fatalf("op view %#v", opView)
	}
	if len(opView.Entries) < 5 {
		t.Fatalf("entries %#v", opView.Entries)
	}

	deny := qs.do(t, http.MethodGet, "/api/ops/quota/price-versions/default", "", dual.CookiesB)
	if deny.StatusCode != http.StatusForbidden {
		t.Fatalf("non-op default %d %s", deny.StatusCode, readDetail(t, deny))
	}
	_ = readDetail(t, deny)

	merchant := qs.do(t, http.MethodGet, "/api/merchants/"+dual.MerchantAID+"/quota/price", "", dual.CookiesA)
	if merchant.StatusCode != http.StatusOK {
		t.Fatalf("merchant price %d %s", merchant.StatusCode, readDetail(t, merchant))
	}
	mView := readPriceVersion(t, merchant)
	if mView.PriceVersionID != quota.DefaultPriceVersionID || mView.Currency != quota.CurrencyInternalUnits {
		t.Fatalf("merchant view %#v", mView)
	}

	cross := qs.do(t, http.MethodGet, "/api/merchants/"+dual.MerchantAID+"/quota/price", "", dual.CookiesB)
	if cross.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-merchant price %d %s", cross.StatusCode, readDetail(t, cross))
	}
	_ = readDetail(t, cross)
}

func readPriceVersion(t *testing.T, resp *http.Response) quota.PriceVersionView {
	t.Helper()
	defer resp.Body.Close()
	var view quota.PriceVersionView
	if err := json.NewDecoder(resp.Body).Decode(&view); err != nil {
		t.Fatal(err)
	}
	return view
}
