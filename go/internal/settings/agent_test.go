package settings_test

import (
	"context"
	"testing"

	"github.com/yuqie6/productflow/internal/platform/clockid"
	"github.com/yuqie6/productflow/internal/platform/config"
	"github.com/yuqie6/productflow/internal/platform/testdb"
	"github.com/yuqie6/productflow/internal/settings"
)

func TestResolveAgentProviderBackgroundResumableIsNeverTrue(t *testing.T) {
	pool := testdb.Pool(t)
	store := settings.NewStore(pool, config.Config{})
	profileID := clockid.New()
	bindingID := clockid.New()
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO provider_profiles (
			id, name, provider_type, api_key, capabilities_json, default_models_json, config_json,
			enabled, created_at, updated_at
		) VALUES (
			$1, 'Agent OpenAI', 'openai_compatible', 'sk-test',
			'["text_responses","background_responses","background_resumable"]'::json,
			'{"agent_model":"gpt-test"}'::json, '{}'::json,
			TRUE, NOW(), NOW()
		)
	`, profileID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), `DELETE FROM provider_bindings WHERE purpose = 'agent'`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO provider_bindings (
			id, purpose, provider_kind, provider_profile_id, model_settings_json, config_json, created_at, updated_at
		) VALUES (
			$1, 'agent', 'openai', $2, '{"model":"gpt-test"}'::json, '{}'::json, NOW(), NOW()
		)
	`, bindingID, profileID); err != nil {
		t.Fatal(err)
	}
	cfg, err := store.ResolveAgentProvider(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BackgroundResumable {
		t.Fatal("Pi adapter must keep background_resumable false even when the profile advertises background")
	}
	if cfg.Model != "gpt-test" || cfg.ProviderKind != "openai" {
		t.Fatalf("config %+v", cfg)
	}
}
