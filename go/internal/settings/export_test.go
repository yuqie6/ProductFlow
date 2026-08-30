package settings

import (
	"context"
	"slices"
	"testing"

	"github.com/yuqie6/productflow/internal/platform/config"
)

func testImportRuntime() map[string]any {
	dummy := &Store{env: config.Config{
		UploadMaxImageBytes:      1024,
		UploadMaxBatchBytes:      2048,
		UploadMaxBatchFiles:      2,
		UploadMaxReferenceImages: 1,
		UploadMaxPixels:          1000,
		UploadAllowedMIMETypes:   "image/png",
		AdminAccessRequired:      true,
	}}
	out := map[string]any{}
	for _, def := range configDefinitions() {
		out[def.Key] = envDefault(dummy, def.Key)
	}
	return out
}

func mockImportBindings(omitPurpose string) []any {
	all := []map[string]any{
		{"purpose": "prompt", "provider_kind": "mock", "model_settings": map[string]any{"model": "mock-prompt"}, "config": map[string]any{}},
		{"purpose": "agent", "provider_kind": "mock", "model_settings": map[string]any{"model": "mock-agent"}, "config": map[string]any{}},
		{"purpose": "image", "provider_kind": "mock", "model_settings": map[string]any{"model": "mock-image"}, "config": map[string]any{}},
	}
	out := make([]any, 0, len(all))
	for _, item := range all {
		if omitPurpose != "" && item["purpose"] == omitPurpose {
			continue
		}
		out = append(out, item)
	}
	return out
}

func testImportDoc(profiles, bindings []any) map[string]any {
	return map[string]any{
		"metadata": map[string]any{
			"schema_version": exportSchemaVersion,
			"exported_at":    "2024-01-01T00:00:00Z",
			"app":            "productflow",
			"app_version":    exportAppVersion,
			"compatibility":  exportCompatibility,
		},
		"runtime_config":    testImportRuntime(),
		"provider_profiles": profiles,
		"provider_bindings": bindings,
	}
}

func sampleImportProfile(id, name string, caps []string) map[string]any {
	return map[string]any{
		"id": id, "name": name, "provider_type": "openai_compatible",
		"capabilities": caps, "enabled": true,
		"default_models": map[string]any{}, "config": map[string]any{},
	}
}

func TestPreviewImportValidatesProfilesAndBindings(t *testing.T) {
	t.Parallel()
	store := &Store{}
	tests := []struct {
		name      string
		doc       map[string]any
		errSubstr string
	}{
		{
			name: "empty profile id",
			doc: testImportDoc(
				[]any{sampleImportProfile("", "A", []string{"text_responses"})},
				mockImportBindings(""),
			),
			errSubstr: "供应商档案 ID 不能为空",
		},
		{
			name: "duplicate profile id",
			doc: testImportDoc(
				[]any{
					sampleImportProfile("p1", "A", []string{"text_responses"}),
					sampleImportProfile("p1", "B", []string{"text_responses"}),
				},
				mockImportBindings(""),
			),
			errSubstr: "供应商档案不能重复",
		},
		{
			name: "missing image binding",
			doc: testImportDoc(
				[]any{sampleImportProfile("p1", "A", []string{"text_responses"})},
				mockImportBindings("image"),
			),
			errSubstr: "配置文件缺少供应商绑定",
		},
		{
			name: "empty provider type",
			doc: testImportDoc(
				[]any{map[string]any{
					"id": "p1", "name": "A", "provider_type": "",
					"capabilities": []string{"text_responses"}, "enabled": true,
					"default_models": map[string]any{}, "config": map[string]any{},
				}},
				mockImportBindings(""),
			),
			errSubstr: "供应商类型不支持",
		},
		{
			name: "unknown capability",
			doc: testImportDoc(
				[]any{sampleImportProfile("p1", "A", []string{"not_a_capability"})},
				mockImportBindings(""),
			),
			errSubstr: "供应商能力不支持",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := store.PreviewImport(tc.doc)
			requireErrContains(t, err, tc.errSubstr)
		})
	}
}

func TestPreviewImportNormalizesAndApplyImportRevalidates(t *testing.T) {
	t.Parallel()
	store := &Store{}
	doc := testImportDoc(
		[]any{sampleImportProfile("p1", "  档案  ", []string{"text_responses"})},
		[]any{
			map[string]any{
				"purpose": "prompt", "provider_kind": "mock",
				"provider_profile_id": "p1",
				"model_settings":      map[string]any{"model": "mock-prompt"},
				"config":              map[string]any{},
			},
			map[string]any{
				"purpose": "agent", "provider_kind": "mock",
				"model_settings": map[string]any{"model": "mock-agent"},
				"config":         map[string]any{},
			},
			map[string]any{
				"purpose": "image", "provider_kind": "mock",
				"model_settings": map[string]any{"model": "mock-image"},
				"config":         map[string]any{},
			},
		},
	)
	preview, normalized, err := store.PreviewImport(doc)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(preview.ProviderProfileNames, []string{"档案"}) {
		t.Fatalf("names %v", preview.ProviderProfileNames)
	}
	if !slices.Equal(preview.ProviderBindingPurposes, []string{"agent", "image", "prompt"}) {
		t.Fatalf("purposes %v", preview.ProviderBindingPurposes)
	}
	profiles, _ := normalized["provider_profiles"].([]any)
	if len(profiles) != 1 {
		t.Fatalf("profiles %d", len(profiles))
	}
	profile, _ := profiles[0].(map[string]any)
	if profile["name"] != "档案" {
		t.Fatalf("normalized name %v", profile["name"])
	}
	caps, ok := profile["capabilities"].([]string)
	if !ok || !slices.Equal(caps, []string{"text_responses"}) {
		t.Fatalf("capabilities %T %v", profile["capabilities"], profile["capabilities"])
	}
	bindings, _ := normalized["provider_bindings"].([]any)
	prompt, _ := bindings[0].(map[string]any)
	if prompt["provider_profile_id"] != nil {
		t.Fatalf("mock profile id %v", prompt["provider_profile_id"])
	}

	err = store.ApplyImport(context.Background(), testImportDoc(
		[]any{
			sampleImportProfile("dup", "A", []string{"text_responses"}),
			sampleImportProfile("dup", "B", []string{"text_responses"}),
		},
		mockImportBindings(""),
	))
	requireErrContains(t, err, "供应商档案不能重复")
}

func TestPreviewImportRejectsMalformedJSONTypes(t *testing.T) {
	t.Parallel()
	store := &Store{}
	tests := []struct {
		name string
		doc  map[string]any
	}{
		{
			name: "numeric profile id",
			doc: testImportDoc(
				[]any{map[string]any{
					"id": 123, "name": "A", "provider_type": "openai_compatible",
					"capabilities": []string{"text_responses"}, "enabled": true,
				}},
				mockImportBindings(""),
			),
		},
		{
			name: "explicit null profile id",
			doc: testImportDoc(
				[]any{map[string]any{
					"id": nil, "name": "A", "provider_type": "openai_compatible",
					"capabilities": []string{"text_responses"}, "enabled": true,
				}},
				mockImportBindings(""),
			),
		},
		{
			name: "non-bool enabled",
			doc: testImportDoc(
				[]any{map[string]any{
					"id": "p1", "name": "A", "provider_type": "openai_compatible",
					"capabilities": []string{"text_responses"}, "enabled": "yes",
				}},
				mockImportBindings(""),
			),
		},
		{
			name: "non-string capability",
			doc: testImportDoc(
				[]any{map[string]any{
					"id": "p1", "name": "A", "provider_type": "openai_compatible",
					"capabilities": []any{1}, "enabled": true,
				}},
				mockImportBindings(""),
			),
		},
		{
			name: "string schema_version",
			doc: func() map[string]any {
				doc := testImportDoc(nil, mockImportBindings(""))
				meta := doc["metadata"].(map[string]any)
				meta["schema_version"] = "3"
				return doc
			}(),
		},
		{
			name: "explicit null metadata exported_at",
			doc: func() map[string]any {
				doc := testImportDoc(nil, mockImportBindings(""))
				meta := doc["metadata"].(map[string]any)
				meta["exported_at"] = nil
				return doc
			}(),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := store.PreviewImport(tc.doc)
			requireErrContains(t, err, "请求体无效")
		})
	}
}
