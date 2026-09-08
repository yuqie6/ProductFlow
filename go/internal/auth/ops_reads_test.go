package auth_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/config"
	platformdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/httpx"
	"github.com/yuqie6/productflow/internal/settings"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

func openOpsReadsAuthDB(t *testing.T, name string) (*pgxpool.Pool, *gorm.DB) {
	t.Helper()
	raw := os.Getenv("DATABASE_URL")
	if raw == "" {
		t.Skip("DATABASE_URL not set")
	}
	normalized := config.NormalizePostgresURL(raw)
	parsed, err := url.Parse(normalized)
	if err != nil {
		t.Fatalf("parse DATABASE_URL: %v", err)
	}
	parsed.Path = "/" + name
	targetURL := parsed.String()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	admin, err := pgxpool.New(ctx, normalized)
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	if err := admin.Ping(ctx); err != nil {
		admin.Close()
		t.Skipf("postgres ping: %v", err)
	}
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name+" TEMPLATE template0"); err != nil {
		admin.Close()
		t.Skipf("cannot create database: %v", err)
	}
	var target *pgxpool.Pool
	t.Cleanup(func() {
		if target != nil {
			target.Close()
		}
		dropCtx, dropCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer dropCancel()
		_, _ = admin.Exec(dropCtx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
		admin.Close()
	})
	target, err = pgxpool.New(ctx, targetURL)
	if err != nil {
		t.Fatal(err)
	}
	if err := target.Ping(ctx); err != nil {
		target.Close()
		t.Fatal(err)
	}
	gdb, err := platformdb.OpenGorm(target)
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.Apply(gdb); err != nil {
		t.Fatal(err)
	}
	return target, gdb
}

func newOpsReadsAuthServer(t *testing.T) *authServer {
	t.Helper()
	pool, gdb := openOpsReadsAuthDB(t, fmt.Sprintf("pf_ops_reads_0908_auth_%d", time.Now().UnixNano()))
	engine := httpx.NewEngine(nil)
	engine.Use(httpx.Session(httpx.NewCookieStore(httpx.SessionConfig{Secret: "test-session-secret-key"})))
	store := settings.NewStore(pool, config.Config{AdminAccessRequired: true})
	auth.MountTest(engine, gdb, store, auth.TestAdminKey)
	srv := httptest.NewServer(engine)
	t.Cleanup(srv.Close)
	return &authServer{srv: srv, client: &http.Client{}, db: gdb}
}

func seedOpsReadOperatorWithoutMerchant(t *testing.T, db *gorm.DB, email string) schema.Users {
	t.Helper()
	now := time.Now().UTC()
	hash, err := bcrypt.GenerateFromPassword([]byte("ops-read-operator-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	user := schema.Users{
		ID:           clockid.New(),
		Email:        email,
		PasswordHash: string(hash),
		DisplayName:  "无归属 Operator",
		IsOperator:   true,
		Status:       auth.UserStatusActive,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = db.Where("user_id = ?", user.ID).Delete(&schema.AuthSessions{}).Error
		_ = db.Where("id = ?", user.ID).Delete(&schema.Users{}).Error
	})
	return user
}

func readMerchantPage(t *testing.T, resp *http.Response) auth.MerchantPage {
	t.Helper()
	defer resp.Body.Close()
	var page auth.MerchantPage
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		t.Fatal(err)
	}
	return page
}

func TestOperatorMerchantDirectoryFiltersAndDetail(t *testing.T) {
	as := newOpsReadsAuthServer(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	merchants := []schema.Merchants{
		{ID: "ops-read-alpha-active", Name: "Alpha Active", Status: auth.MerchantStatusActive, CreatedAt: now, UpdatedAt: now},
		{ID: "ops-read-alpha-suspended", Name: "Alpha Suspended", Status: auth.MerchantStatusSuspended, CreatedAt: now.Add(time.Second), UpdatedAt: now.Add(time.Second)},
		{ID: "ops-read-beta-active", Name: "Beta Active", Status: auth.MerchantStatusActive, CreatedAt: now.Add(2 * time.Second), UpdatedAt: now.Add(2 * time.Second)},
		{ID: "ops-read-literal-percent", Name: "Literal% Name", Status: auth.MerchantStatusActive, CreatedAt: now.Add(3 * time.Second), UpdatedAt: now.Add(3 * time.Second)},
	}
	for i := range merchants {
		if err := as.db.Create(&merchants[i]).Error; err != nil {
			t.Fatal(err)
		}
		id := merchants[i].ID
		t.Cleanup(func() { _ = as.db.Where("id = ?", id).Delete(&schema.Merchants{}).Error })
	}
	operator := seedOpsReadOperatorWithoutMerchant(t, as.db, "ops-read-no-home@test.local")
	operatorCookies := loginDirectUser(t, as, operator.Email, "ops-read-operator-password")
	ordinary := seedDirectUser(t, as.db, merchants[0].ID, "ops-read-ordinary@test.local", "ops-read-ordinary-password", "普通账号", false)
	ordinaryCookies := loginDirectUser(t, as, ordinary.Email, "ops-read-ordinary-password")

	all := as.do(t, http.MethodGet, "/api/ops/merchants?page=1&page_size=2", "", operatorCookies)
	if all.StatusCode != http.StatusOK {
		t.Fatalf("operator without merchant list status %d", all.StatusCode)
	}
	page := readMerchantPage(t, all)
	if page.Total != int64(len(merchants)) || page.Page != 1 || page.PageSize != 2 || len(page.Items) != 2 {
		t.Fatalf("unexpected first page %#v", page)
	}
	if page.Items[0].ID != merchants[0].ID || page.Items[1].ID != merchants[1].ID {
		t.Fatalf("created order %#v", page.Items)
	}

	second := as.do(t, http.MethodGet, "/api/ops/merchants?page=2&page_size=2", "", operatorCookies)
	if second.StatusCode != http.StatusOK {
		t.Fatalf("second page status %d", second.StatusCode)
	}
	secondPage := readMerchantPage(t, second)
	if len(secondPage.Items) != 2 || secondPage.Items[0].ID != merchants[2].ID || secondPage.Items[1].ID != merchants[3].ID {
		t.Fatalf("unexpected second page %#v", secondPage)
	}

	filtered := as.do(t, http.MethodGet, "/api/ops/merchants?q=alpha&status=active&page_size=10", "", operatorCookies)
	if filtered.StatusCode != http.StatusOK {
		t.Fatalf("filtered status %d", filtered.StatusCode)
	}
	filteredPage := readMerchantPage(t, filtered)
	if filteredPage.Total != 1 || len(filteredPage.Items) != 1 || filteredPage.Items[0].ID != merchants[0].ID {
		t.Fatalf("filtered page %#v", filteredPage)
	}

	statusOnly := as.do(t, http.MethodGet, "/api/ops/merchants?status=suspended&page_size=10", "", operatorCookies)
	if statusOnly.StatusCode != http.StatusOK {
		t.Fatalf("status filter %d", statusOnly.StatusCode)
	}
	statusPage := readMerchantPage(t, statusOnly)
	if statusPage.Total != 1 || len(statusPage.Items) != 1 || statusPage.Items[0].ID != merchants[1].ID {
		t.Fatalf("status page %#v", statusPage)
	}

	literal := as.do(t, http.MethodGet, "/api/ops/merchants?q=%25&page_size=10", "", operatorCookies)
	if literal.StatusCode != http.StatusOK {
		t.Fatalf("literal wildcard filter %d", literal.StatusCode)
	}
	literalPage := readMerchantPage(t, literal)
	if literalPage.Total != 1 || len(literalPage.Items) != 1 || literalPage.Items[0].ID != merchants[3].ID {
		t.Fatalf("wildcard was not escaped %#v", literalPage)
	}

	underscore := as.do(t, http.MethodGet, "/api/ops/merchants?q=_&page_size=10", "", operatorCookies)
	if underscore.StatusCode != http.StatusOK {
		t.Fatalf("literal underscore filter %d", underscore.StatusCode)
	}
	underscorePage := readMerchantPage(t, underscore)
	if underscorePage.Total != 0 || len(underscorePage.Items) != 0 {
		t.Fatalf("underscore wildcard was not escaped %#v", underscorePage)
	}

	detail := as.do(t, http.MethodGet, "/api/ops/merchants/"+merchants[1].ID, "", operatorCookies)
	if detail.StatusCode != http.StatusOK {
		t.Fatalf("merchant detail %d", detail.StatusCode)
	}
	var detailView auth.MerchantView
	if err := json.NewDecoder(detail.Body).Decode(&detailView); err != nil {
		detail.Body.Close()
		t.Fatal(err)
	}
	detail.Body.Close()
	if detailView.ID != merchants[1].ID || detailView.Name != merchants[1].Name || detailView.Status != merchants[1].Status {
		t.Fatalf("detail %#v", detailView)
	}

	missing := as.do(t, http.MethodGet, "/api/ops/merchants/does-not-exist", "", operatorCookies)
	if missing.StatusCode != http.StatusNotFound {
		t.Fatalf("missing detail status %d", missing.StatusCode)
	}
	if got := readDetail(t, missing); got != auth.CrossMerchantDetail {
		t.Fatalf("missing detail %q", got)
	}

	denied := as.do(t, http.MethodGet, "/api/ops/merchants", "", ordinaryCookies)
	if denied.StatusCode != http.StatusForbidden {
		t.Fatalf("ordinary list status %d", denied.StatusCode)
	}
	_ = readDetail(t, denied)
	deniedDetail := as.do(t, http.MethodGet, "/api/ops/merchants/"+merchants[0].ID, "", ordinaryCookies)
	if deniedDetail.StatusCode != http.StatusForbidden {
		t.Fatalf("ordinary detail status %d", deniedDetail.StatusCode)
	}
	_ = readDetail(t, deniedDetail)

	for _, query := range []string{"?status=unknown", "?q=" + strings.Repeat("x", 101)} {
		resp := as.do(t, http.MethodGet, "/api/ops/merchants"+query, "", operatorCookies)
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("invalid directory query %s status %d", query, resp.StatusCode)
		}
	}
}
