package auth_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/platform/config"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

const (
	saasCapacityFixtureEnv           = "PRODUCTFLOW_RUN_SAAS_CAPACITY_FIXTURE"
	saasCapacityPrefix               = "cap0908-"
	saasCapacityPassword             = "capacity-password-0908"
	saasCapacityMerchants            = 10
	saasCapacityProducts             = 1000
	saasCapacityAssets               = 5000
	saasCapacityQuota                = 10000
	saasCapacityOpeningBalance int64 = 1_000_000
)

type saasCapacityMerchant struct {
	ID        string `json:"merchant_id"`
	Email     string `json:"email"`
	Password  string `json:"password"`
	SessionID string `json:"image_session_id"`
}

type saasCapacityManifest struct {
	FixtureVersion              string                 `json:"fixture_version"`
	Commit                      string                 `json:"commit"`
	CreatedAt                   time.Time              `json:"created_at"`
	MerchantCount               int                    `json:"merchant_count"`
	ProductsPerMerchant         int                    `json:"products_per_merchant"`
	AssetsPerMerchant           int                    `json:"assets_per_merchant"`
	QuotaEventsTotal            int                    `json:"quota_events_total"`
	GenerationSlots             int                    `json:"generation_slots"`
	OperatorEmail               string                 `json:"operator_email"`
	OperatorPassword            string                 `json:"operator_password"`
	Merchants                   []saasCapacityMerchant `json:"merchants"`
	ProviderMode                string                 `json:"provider_mode"`
	ProviderBaseURL             string                 `json:"provider_base_url,omitempty"`
	SourceIdentitySHA256        string                 `json:"source_identity_sha256"`
	ImageName                   string                 `json:"image_name"`
	ImageID                     string                 `json:"image_id"`
	OpeningBalanceUnits         int64                  `json:"opening_balance_units"`
	QuotaAdjustUnitsPerMerchant int64                  `json:"quota_adjust_units_per_merchant"`
	CrossMerchantProbe          map[string]string      `json:"cross_merchant_probe"`
}

// TestSaaSCapacityFixture creates the exact candidate scale in a dedicated database.
// It is deliberately opt-in and refuses a non-empty database so a rerun cannot mix
// another test's rows into the capacity denominator.
func TestSaaSCapacityFixture(t *testing.T) {
	if os.Getenv(saasCapacityFixtureEnv) != "1" {
		t.Skip("set PRODUCTFLOW_RUN_SAAS_CAPACITY_FIXTURE=1 to create the SaaS capacity fixture")
	}
	rawURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	manifestPath := strings.TrimSpace(os.Getenv("SAAS_CAPACITY_MANIFEST"))
	if rawURL == "" || manifestPath == "" {
		t.Fatal("capacity fixture requires DATABASE_URL and SAAS_CAPACITY_MANIFEST")
	}
	if _, err := os.Stat(manifestPath); err == nil {
		t.Fatalf("capacity fixture manifest already exists: %s", manifestPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("inspect capacity fixture manifest: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	pool, err := pfdb.Connect(ctx, config.NormalizePostgresURL(rawURL))
	if err != nil {
		t.Fatalf("connect capacity database: %v", err)
	}
	defer pool.Close()
	gdb, err := pfdb.OpenGorm(pool)
	if err != nil {
		t.Fatalf("open capacity gorm: %v", err)
	}

	for _, model := range []any{&schema.Merchants{}, &schema.Products{}, &schema.MediaLibraryAssets{}, &schema.MerchantQuotaEvents{}} {
		var count int64
		if err := gdb.Model(model).Count(&count).Error; err != nil {
			t.Fatalf("inspect empty capacity database: %v", err)
		}
		if count != 0 {
			t.Fatalf("capacity database is not empty: %T has %d rows", model, count)
		}
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	hash, err := bcrypt.GenerateFromPassword([]byte(saasCapacityPassword), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash fixture password: %v", err)
	}
	providerBaseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("SAAS_CAPACITY_MOCK_PROVIDER_URL")), "/")
	commit := strings.TrimSpace(os.Getenv("SAAS_CAPACITY_COMMIT"))
	sourceIdentitySHA256 := strings.TrimSpace(os.Getenv("SAAS_CAPACITY_SOURCE_IDENTITY_SHA256"))
	imageName := strings.TrimSpace(os.Getenv("SAAS_CAPACITY_IMAGE_NAME"))
	imageID := strings.TrimSpace(os.Getenv("SAAS_CAPACITY_IMAGE_ID"))
	if commit == "" || sourceIdentitySHA256 == "" || imageName == "" || imageID == "" {
		t.Fatal("capacity fixture requires commit, source identity, image name, and image id")
	}
	if err := gdb.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`
			INSERT INTO merchants (id, name, status, created_at, updated_at)
			SELECT 'cap0908-m-' || lpad(g::text, 2, '0'), '容量商家-' || lpad(g::text, 2, '0'), 'active', ?, ?
			FROM generate_series(0, 9) AS g`, now, now).Error; err != nil {
			return fmt.Errorf("insert merchants: %w", err)
		}
		if err := tx.Exec(`
			INSERT INTO users (id, email, password_hash, display_name, is_operator, merchant_id, status, locale, theme, created_at, updated_at)
			VALUES ('cap0908-op', 'capacity-operator-0908@test.local', ?, 'Capacity operator', TRUE, NULL, 'active', 'zh-CN', 'system', ?, ?)
			ON CONFLICT (email) DO NOTHING`, string(hash), now, now).Error; err != nil {
			return fmt.Errorf("insert operator: %w", err)
		}
		if err := tx.Exec(`
			INSERT INTO users (id, email, password_hash, display_name, is_operator, merchant_id, status, locale, theme, created_at, updated_at)
			SELECT 'cap0908-u-' || lpad(g::text, 2, '0'),
			       'capacity-merchant-' || lpad(g::text, 2, '0') || '@test.local', ?,
			       '容量商家账号-' || lpad(g::text, 2, '0'), FALSE,
			       'cap0908-m-' || lpad(g::text, 2, '0'), 'active', 'zh-CN', 'system', ?, ?
			FROM generate_series(0, 9) AS g`, string(hash), now, now).Error; err != nil {
			return fmt.Errorf("insert merchant users: %w", err)
		}
		if err := tx.Exec(`
			INSERT INTO quota_price_versions (id, label, currency, is_default, created_at, updated_at)
			VALUES ('pv-placeholder-v0', 'capacity default price v0', 'iu', TRUE, ?, ?)
			ON CONFLICT (id) DO NOTHING`, now, now).Error; err != nil {
			return fmt.Errorf("insert quota price version: %w", err)
		}
		if err := tx.Exec(`
			INSERT INTO quota_price_entries (price_version_id, entry_code, unit_price, created_at)
			VALUES
			('pv-placeholder-v0', 'image_session.generate', 1, ?),
			('pv-placeholder-v0', 'graph.image_generation', 1, ?),
			('pv-placeholder-v0', 'agent.model_request', 1, ?),
			('pv-placeholder-v0', 'localedit.edit', 1, ?),
			('pv-placeholder-v0', 'product.source_note', 1, ?)
			ON CONFLICT (price_version_id, entry_code) DO NOTHING`, now, now, now, now, now).Error; err != nil {
			return fmt.Errorf("insert quota price entries: %w", err)
		}
		if err := tx.Exec(`
		INSERT INTO merchant_quota_accounts
			(merchant_id, currency, available_units, reserved_units, price_version_id, created_at, updated_at)
			SELECT 'cap0908-m-' || lpad(g::text, 2, '0'), 'iu', ?, 0, 'pv-placeholder-v0', ?, ?
			FROM generate_series(0, 9) AS g`, saasCapacityOpeningBalance+saasCapacityQuota, now, now).Error; err != nil {
			return fmt.Errorf("insert quota accounts: %w", err)
		}
		if err := tx.Exec(`
			INSERT INTO merchant_quota_events
			(id, merchant_id, hold_id, event_type, amount_units, idempotency_key, available_after, reserved_after, price_version_id, reason, actor_user_id, created_at)
			SELECT 'cap0908-q-' || lpad(m::text, 2, '0') || '-' || lpad(e::text, 5, '0'),
			       'cap0908-m-' || lpad(m::text, 2, '0'), NULL, 'adjust', 1,
			       'cap0908-adjust-' || lpad(m::text, 2, '0') || '-' || lpad(e::text, 5, '0'),
			       ? + 1 + e, 0, 'pv-placeholder-v0', 'capacity fixture', NULL,
			       CAST(? AS timestamptz) + (m * ? + e) * interval '1 microsecond'
			FROM generate_series(0, 9) AS m CROSS JOIN generate_series(0, 9999) AS e`, saasCapacityOpeningBalance, now, saasCapacityQuota).Error; err != nil {
			return fmt.Errorf("insert quota events: %w", err)
		}
		if err := tx.Exec(`
			INSERT INTO products (id, merchant_id, name, category, price, source_note, created_at, updated_at)
			SELECT 'cap0908-p-' || lpad(m::text, 2, '0') || '-' || lpad(p::text, 4, '0'),
			       'cap0908-m-' || lpad(m::text, 2, '0'),
			       '容量商品-' || lpad(m::text, 2, '0') || '-' || lpad(p::text, 4, '0'),
			       'capacity-fixture', '1.00', 'fixture product', ?, ?
			FROM generate_series(0, 9) AS m CROSS JOIN generate_series(0, 999) AS p`, now, now).Error; err != nil {
			return fmt.Errorf("insert products: %w", err)
		}
		if err := tx.Exec(`
			INSERT INTO media_objects
			(id, storage_path, mime_type, byte_size, width, height, sha256, verification_status, created_at, verified_at)
			SELECT 'cap0908-mo-' || lpad(m::text, 2, '0') || '-' || lpad(a::text, 4, '0'),
			       'capacity-fixture/' || lpad(m::text, 2, '0') || '/' || lpad(a::text, 4, '0') || '.png',
			       'image/png', 68, 1, 1, repeat('a', 64), 'verified', ?, ?
			FROM generate_series(0, 9) AS m CROSS JOIN generate_series(0, 4999) AS a`, now, now).Error; err != nil {
			return fmt.Errorf("insert media objects: %w", err)
		}
		if err := tx.Exec(`
			INSERT INTO media_library_assets
			(id, merchant_id, media_object_id, source_type, source_id, provenance_json, provenance_hash, revision, display_name, original_filename, is_archived, created_at, updated_at)
			SELECT 'cap0908-a-' || lpad(m::text, 2, '0') || '-' || lpad(a::text, 4, '0'),
			       'cap0908-m-' || lpad(m::text, 2, '0'),
			       'cap0908-mo-' || lpad(m::text, 2, '0') || '-' || lpad(a::text, 4, '0'),
			       'direct_upload', 'cap0908-a-' || lpad(m::text, 2, '0') || '-' || lpad(a::text, 4, '0'),
			       '{}'::json, repeat('b', 64), 1,
			       '容量素材-' || lpad(m::text, 2, '0') || '-' || lpad(a::text, 4, '0'),
			       'capacity-fixture.png', FALSE, ?, ?
			FROM generate_series(0, 9) AS m CROSS JOIN generate_series(0, 4999) AS a`, now, now).Error; err != nil {
			return fmt.Errorf("insert media library assets: %w", err)
		}
		if err := tx.Exec(`
			INSERT INTO image_sessions (id, merchant_id, title, created_at, updated_at)
			SELECT 'cap0908-s-' || lpad(g::text, 2, '0'), 'cap0908-m-' || lpad(g::text, 2, '0'), '容量基线会话-' || lpad(g::text, 2, '0'), ?, ?
			FROM generate_series(0, 9) AS g`, now, now).Error; err != nil {
			return fmt.Errorf("insert image sessions: %w", err)
		}
		if err := tx.Exec(`
			INSERT INTO app_settings (key, value, created_at, updated_at)
			VALUES ('generation_max_concurrent_tasks', '3', ?, ?)
			ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = EXCLUDED.updated_at`, now, now).Error; err != nil {
			return fmt.Errorf("set generation capacity: %w", err)
		}
		if err := tx.Exec(`
			INSERT INTO app_settings (key, value, created_at, updated_at)
			VALUES ('admin_access_required', 'true', ?, ?)
			ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = EXCLUDED.updated_at`, now, now).Error; err != nil {
			return fmt.Errorf("set admin access: %w", err)
		}
		if providerBaseURL == "" {
			return nil
		}
		if err := tx.Exec(`
			INSERT INTO provider_profiles
			(id, name, provider_type, base_url, api_key, capabilities_json, default_models_json, config_json, enabled, created_at, updated_at)
			VALUES ('cap0908-provider', 'capacity local mock', 'openai_compatible', ?, 'capacity-local-mock', '["image_images"]', '{"image_model":"capacity-mock"}', '{}', TRUE, ?, ?)
			ON CONFLICT (id) DO UPDATE SET base_url = EXCLUDED.base_url, api_key = EXCLUDED.api_key, enabled = TRUE, updated_at = EXCLUDED.updated_at`, providerBaseURL, now, now).Error; err != nil {
			return fmt.Errorf("insert mock provider profile: %w", err)
		}
		if err := tx.Exec(`
			INSERT INTO provider_bindings
			(id, purpose, provider_kind, provider_profile_id, model_settings_json, config_json, created_at, updated_at)
			VALUES ('cap0908-image-binding', 'image', 'openai_images', 'cap0908-provider', '{"model":"capacity-mock"}', '{}', ?, ?)
			ON CONFLICT (purpose) DO UPDATE SET provider_kind = EXCLUDED.provider_kind, provider_profile_id = EXCLUDED.provider_profile_id, model_settings_json = EXCLUDED.model_settings_json, config_json = EXCLUDED.config_json, updated_at = EXCLUDED.updated_at`, now, now).Error; err != nil {
			return fmt.Errorf("bind mock provider: %w", err)
		}
		return nil
	}); err != nil {
		t.Fatalf("seed capacity fixture: %v", err)
	}

	assertCapacityCounts(t, gdb)
	merchants := make([]saasCapacityMerchant, 0, saasCapacityMerchants)
	for i := 0; i < saasCapacityMerchants; i++ {
		merchants = append(merchants, saasCapacityMerchant{
			ID:        fmt.Sprintf("%sm-%02d", saasCapacityPrefix, i),
			Email:     fmt.Sprintf("capacity-merchant-%02d@test.local", i),
			Password:  saasCapacityPassword,
			SessionID: fmt.Sprintf("%ss-%02d", saasCapacityPrefix, i),
		})
	}
	manifest := saasCapacityManifest{
		FixtureVersion:              "saas-capacity-0908-v1",
		Commit:                      commit,
		CreatedAt:                   now,
		MerchantCount:               saasCapacityMerchants,
		ProductsPerMerchant:         saasCapacityProducts,
		AssetsPerMerchant:           saasCapacityAssets,
		QuotaEventsTotal:            saasCapacityMerchants * saasCapacityQuota,
		GenerationSlots:             3,
		OperatorEmail:               "capacity-operator-0908@test.local",
		OperatorPassword:            saasCapacityPassword,
		Merchants:                   merchants,
		ProviderMode:                "local-mock-openai-compatible",
		ProviderBaseURL:             providerBaseURL,
		SourceIdentitySHA256:        sourceIdentitySHA256,
		ImageName:                   imageName,
		ImageID:                     imageID,
		OpeningBalanceUnits:         saasCapacityOpeningBalance,
		QuotaAdjustUnitsPerMerchant: saasCapacityQuota,
		CrossMerchantProbe: map[string]string{
			"merchant_a":         merchants[0].ID,
			"merchant_b":         merchants[1].ID,
			"merchant_a_product": "cap0908-p-00-0000",
			"merchant_b_product": "cap0908-p-01-0000",
			"merchant_a_asset":   "cap0908-a-00-0000",
			"merchant_b_asset":   "cap0908-a-01-0000",
		},
	}
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatalf("encode capacity manifest: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o755); err != nil {
		t.Fatalf("create manifest directory: %v", err)
	}
	if err := os.WriteFile(manifestPath, append(encoded, '\n'), 0o644); err != nil {
		t.Fatalf("write capacity manifest: %v", err)
	}
	t.Logf("SAAS_CAPACITY_FIXTURE merchants=%d products=%d assets=%d quota_events=%d generation_slots=%d manifest=%s provider_mode=%s", saasCapacityMerchants, saasCapacityMerchants*saasCapacityProducts, saasCapacityMerchants*saasCapacityAssets, saasCapacityMerchants*saasCapacityQuota, 3, manifestPath, manifest.ProviderMode)
}

func assertCapacityCounts(t *testing.T, gdb *gorm.DB) {
	t.Helper()
	want := map[string]int64{
		"merchants":               saasCapacityMerchants,
		"users":                   saasCapacityMerchants + 1,
		"products":                saasCapacityMerchants * saasCapacityProducts,
		"media_objects":           saasCapacityMerchants * saasCapacityAssets,
		"media_library_assets":    saasCapacityMerchants * saasCapacityAssets,
		"image_sessions":          saasCapacityMerchants,
		"merchant_quota_accounts": saasCapacityMerchants,
		"merchant_quota_events":   saasCapacityMerchants * saasCapacityQuota,
	}
	for table, expected := range want {
		var got int64
		predicate := "id LIKE ?"
		args := []any{saasCapacityPrefix + "%"}
		switch table {
		case "products", "media_library_assets", "image_sessions", "merchant_quota_events":
			predicate = "id LIKE ? OR merchant_id LIKE ?"
			args = []any{saasCapacityPrefix + "%", saasCapacityPrefix + "%"}
		case "merchant_quota_accounts":
			predicate = "merchant_id LIKE ?"
			args = []any{saasCapacityPrefix + "%"}
		}
		if err := gdb.Table(table).Where(predicate, args...).Count(&got).Error; err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if got != expected {
			t.Fatalf("fixture count %s=%d want %d", table, got, expected)
		}
	}
	var perMerchantProducts, perMerchantAssets, perMerchantEvents int64
	if err := gdb.Table("products").Where("merchant_id = ?", "cap0908-m-00").Count(&perMerchantProducts).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Table("media_library_assets").Where("merchant_id = ?", "cap0908-m-00").Count(&perMerchantAssets).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Table("merchant_quota_events").Where("merchant_id = ?", "cap0908-m-00").Count(&perMerchantEvents).Error; err != nil {
		t.Fatal(err)
	}
	if perMerchantProducts != saasCapacityProducts || perMerchantAssets != saasCapacityAssets || perMerchantEvents != saasCapacityQuota {
		t.Fatalf("merchant 00 counts products=%d assets=%d quota_events=%d", perMerchantProducts, perMerchantAssets, perMerchantEvents)
	}
}
