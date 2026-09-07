package auth_test

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
	"github.com/yuqie6/productflow/internal/product"
	"github.com/yuqie6/productflow/internal/settings"
)

// B10 / MP-B 双商隔离门：种子 A/B（仅夹具）、分区 A 正反与因果商品交叉、自审场景可本地断言部分。
// 其余分区深度反测由各域 TestMerchant* 与 queue/merchantBoundary 套件采证。
func TestMerchantIsolationGateB10(t *testing.T) {
	pool, gdb := testdb.Open(t)
	engine := httpx.NewEngine(nil)
	engine.Use(httpx.Session(httpx.NewCookieStore(httpx.SessionConfig{Secret: "test-session-secret-key"})))
	store := settings.NewStore(pool, config.Config{
		AdminAccessRequired:      true,
		UploadMaxImageBytes:      10 * 1024 * 1024,
		UploadMaxPixels:          16_000_000,
		UploadAllowedMIMETypes:   "image/png,image/jpeg,image/webp",
		UploadMaxBatchFiles:      20,
		UploadMaxBatchBytes:      50 * 1024 * 1024,
		UploadMaxReferenceImages: 6,
	})
	if err := gdb.Exec(`
		INSERT INTO app_settings (key, value, created_at, updated_at)
		VALUES ('admin_access_required', 'true', NOW(), NOW())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = NOW()
	`).Error; err != nil {
		t.Fatal(err)
	}
	auth.MountTest(engine, gdb, store, auth.TestAdminKey)
	settings.HTTP{
		Store: store, DB: store, OperatorOnly: auth.RequireOperator(),
	}.Register(engine)
	product.HTTP{Service: product.Service{DB: gdb}, Settings: store}.Register(engine)
	srv := httptest.NewServer(engine)
	t.Cleanup(srv.Close)
	client := &http.Client{}

	dual := auth.SeedDualMerchants(t, gdb, client, srv.URL)
	do := func(method, path, body string, cookies []*http.Cookie, hdr http.Header) *http.Response {
		t.Helper()
		var reader io.Reader
		if body != "" {
			reader = strings.NewReader(body)
		}
		req, err := http.NewRequest(method, srv.URL+path, reader)
		if err != nil {
			t.Fatal(err)
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		for k, vals := range hdr {
			for _, v := range vals {
				req.Header.Add(k, v)
			}
		}
		for _, c := range cookies {
			req.AddCookie(c)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	assertCross404 := func(name string, resp *http.Response) {
		t.Helper()
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("%s want 404 got %d %s", name, resp.StatusCode, raw)
		}
		var payload struct {
			Detail string `json:"detail"`
		}
		_ = json.Unmarshal(raw, &payload)
		if payload.Detail != auth.CrossMerchantDetail {
			t.Fatalf("%s detail=%q want %q body=%s", name, payload.Detail, auth.CrossMerchantDetail, raw)
		}
	}

	now := time.Now().UTC()
	productA := schema.Products{
		ID: clockid.New(), MerchantID: dual.MerchantAID, Name: "门禁商家A商品", CreatedAt: now, UpdatedAt: now,
	}
	productB := schema.Products{
		ID: clockid.New(), MerchantID: dual.MerchantBID, Name: "门禁商家B商品", CreatedAt: now, UpdatedAt: now,
	}
	if err := gdb.Create(&productA).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&productB).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = gdb.Where("id IN ?", []string{productA.ID, productB.ID}).Delete(&schema.Products{}).Error
	})

	t.Run("seed_A_B_sessions", func(t *testing.T) {
		stateA := do(http.MethodGet, "/api/auth/session", "", dual.CookiesA, nil)
		defer stateA.Body.Close()
		if stateA.StatusCode != http.StatusOK {
			t.Fatalf("A session %d", stateA.StatusCode)
		}
		var bodyA struct {
			Memberships []auth.MembershipView `json:"memberships"`
		}
		_ = json.NewDecoder(stateA.Body).Decode(&bodyA)
		if len(bodyA.Memberships) != 1 || bodyA.Memberships[0].MerchantID != dual.MerchantAID {
			t.Fatalf("A memberships %#v", bodyA.Memberships)
		}

		stateB := do(http.MethodGet, "/api/auth/session", "", dual.CookiesB, nil)
		defer stateB.Body.Close()
		if stateB.StatusCode != http.StatusOK {
			t.Fatalf("B session %d", stateB.StatusCode)
		}
		var bodyB struct {
			Memberships []auth.MembershipView `json:"memberships"`
		}
		_ = json.NewDecoder(stateB.Body).Decode(&bodyB)
		if len(bodyB.Memberships) != 1 || bodyB.Memberships[0].MerchantID != dual.MerchantBID {
			t.Fatalf("B memberships %#v", bodyB.Memberships)
		}

		// 产品注册第二商仍拒（夹具 ≠ 开放第二外部商）。
		create2 := do(http.MethodPost, "/api/merchants", `{"name":"产品侧第二商"}`, dual.CookiesA, nil)
		defer create2.Body.Close()
		if create2.StatusCode != http.StatusConflict {
			raw, _ := io.ReadAll(create2.Body)
			t.Fatalf("CreateMerchant still blocked want 409 got %d %s", create2.StatusCode, raw)
		}
	})

	t.Run("partition_A_ops", func(t *testing.T) {
		// 正：Op 可读现有 settings provider-config
		ok := do(http.MethodGet, "/api/settings/provider-config", "", dual.CookiesA, nil)
		ok.Body.Close()
		if ok.StatusCode != http.StatusOK {
			t.Fatalf("op provider-config %d", ok.StatusCode)
		}
		// 反：商家 B 角色碰 settings / ops
		deny := do(http.MethodGet, "/api/settings", "", dual.CookiesB, nil)
		deny.Body.Close()
		if deny.StatusCode != http.StatusForbidden {
			t.Fatalf("B settings want 403 got %d", deny.StatusCode)
		}
		denyOps := do(http.MethodGet, "/api/settings/provider-config", "", dual.CookiesB, nil)
		denyOps.Body.Close()
		if denyOps.StatusCode != http.StatusForbidden {
			t.Fatalf("B provider-config want 403 got %d", denyOps.StatusCode)
		}
	})

	t.Run("scenario_合法读写", func(t *testing.T) {
		ownA := do(http.MethodGet, "/api/v2/products/"+productA.ID, "", dual.CookiesA, nil)
		ownA.Body.Close()
		if ownA.StatusCode != http.StatusOK {
			t.Fatalf("A own get %d", ownA.StatusCode)
		}
		ownB := do(http.MethodGet, "/api/v2/products/"+productB.ID, "", dual.CookiesB, nil)
		ownB.Body.Close()
		if ownB.StatusCode != http.StatusOK {
			t.Fatalf("B own get %d", ownB.StatusCode)
		}
		listA := do(http.MethodGet, "/api/v2/products?page=1&page_size=50", "", dual.CookiesA, nil)
		defer listA.Body.Close()
		if listA.StatusCode != http.StatusOK {
			t.Fatalf("A list %d", listA.StatusCode)
		}
		var page struct {
			Items []struct {
				ID string `json:"id"`
			} `json:"items"`
		}
		_ = json.NewDecoder(listA.Body).Decode(&page)
		sawA, sawB := false, false
		for _, item := range page.Items {
			if item.ID == productA.ID {
				sawA = true
			}
			if item.ID == productB.ID {
				sawB = true
			}
		}
		if !sawA || sawB {
			t.Fatalf("A list isolation sawA=%v sawB=%v items=%d", sawA, sawB, len(page.Items))
		}
	})

	t.Run("scenario_跨商替换ID", func(t *testing.T) {
		assertCross404("A reads B product", do(http.MethodGet, "/api/v2/products/"+productB.ID, "", dual.CookiesA, nil))
		assertCross404("B reads A product", do(http.MethodGet, "/api/v2/products/"+productA.ID, "", dual.CookiesB, nil))
	})

	t.Run("scenario_跨商绑定_声明头", func(t *testing.T) {
		hdr := http.Header{auth.MerchantHeaderName(): []string{dual.MerchantBID}}
		resp := do(http.MethodGet, "/api/v2/products/"+productB.ID, "", dual.CookiesA, hdr)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			raw, _ := io.ReadAll(resp.Body)
			t.Fatalf("A forged B merchant header want 403 got %d %s", resp.StatusCode, raw)
		}
	})

	t.Run("scenario_成员撤销后读写", func(t *testing.T) {
		now := time.Now().UTC()
		if err := gdb.Model(&schema.Memberships{}).
			Where("merchant_id = ? AND user_id = ?", dual.MerchantBID, dual.UserBID).
			Updates(map[string]any{"status": auth.MembershipStatusRevoked, "revoked_at": now, "updated_at": now}).Error; err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_ = gdb.Model(&schema.Memberships{}).
				Where("merchant_id = ? AND user_id = ?", dual.MerchantBID, dual.UserBID).
				Updates(map[string]any{"status": auth.MembershipStatusActive, "revoked_at": nil, "updated_at": time.Now().UTC()}).Error
		})

		denied := do(http.MethodGet, "/api/v2/products/"+productB.ID, "", dual.CookiesB, nil)
		defer denied.Body.Close()
		raw, _ := io.ReadAll(denied.Body)
		if denied.StatusCode != http.StatusForbidden {
			t.Fatalf("revoked B get want 403 got %d %s", denied.StatusCode, raw)
		}
		if !strings.Contains(string(raw), "不属于任何商家") {
			t.Fatalf("revoked detail %s", raw)
		}

		listLeak := do(http.MethodGet, "/api/v2/products?page=1&page_size=50", "", dual.CookiesB, nil)
		defer listLeak.Body.Close()
		if listLeak.StatusCode != http.StatusForbidden {
			leakBody, _ := io.ReadAll(listLeak.Body)
			t.Fatalf("revoked B list want 403 got %d %s", listLeak.StatusCode, leakBody)
		}
	})
}

// TestMerchantIsolationGateSuiteInventory 钉死 B10 套件入口与分区映射（可 go test -run 本函数核对清单存在）。
func TestMerchantIsolationGateSuiteInventory(t *testing.T) {
	type entry struct {
		partition string
		positive  string
		negative  string
		pkg       string
	}
	inventory := []entry{
		{"A", "TestOperatorSuspendBlocksMerchantWrites / TestOperatorProviderConfigOnly / gate partition_A", "TestMerchantRoleForbiddenOnSettingsAndQueue / TestSecondMerchantRejected", "auth"},
		{"B", "TestMerchantProductChainIsolation / TestMerchantRootOwnershipProductIsolation / gate 合法读写", "同测跨商 404", "product"},
		{"C", "TestMerchantGraphIsolation", "同测跨商 changeset/run/SSE 404", "graph"},
		{"D", "TestMerchantRecipeIsolation", "同测跨商 recipe/apply 404", "recipe"},
		{"E", "TestMerchantLibraryBindingIsolation", "同测跨商 from-*/sync 404 零写入", "library"},
		{"F", "TestMerchantImageSessionIsolation", "同测跨商 attach/download/SSE 404", "imagesession"},
		{"G", "TestMerchantDeliveryIsolation / TestMerchantLocalEditIsolation", "同测跨商 job/task 404", "delivery+localedit"},
		{"H/I", "TestMerchantAgentToolsIsolation", "伪造 scope / 跨商 content / 撤销后 confirm / 工具 harness", "agent"},
		{"J", "TestStageSnapshotsMerchantAndRejectsRewrite / TestRestageIfIdlePreservesMerchantMixedQueue / merchantBoundary vitest", "改信封商家 / 切换迟到", "queue+web"},
	}
	if len(inventory) < 9 {
		t.Fatal("inventory incomplete")
	}
	for _, row := range inventory {
		if row.partition == "" || row.positive == "" || row.negative == "" {
			t.Fatalf("bad inventory %#v", row)
		}
		t.Logf("partition %s pkg=%s +[%s] -[%s]", row.partition, row.pkg, row.positive, row.negative)
	}
	_ = fmt.Sprintf("suite command documented in merchant-isolation-gate.md")
}
