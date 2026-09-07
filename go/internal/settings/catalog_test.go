package settings

import (
	"slices"
	"strings"
	"testing"
)

func requireErrContains(t *testing.T, err error, substr string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error containing %q", substr)
	}
	if !strings.Contains(err.Error(), substr) {
		t.Fatalf("error %q does not contain %q", err.Error(), substr)
	}
}

func TestParseImageToolAllowedFields(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		in        string
		want      []string
		errSubstr string
	}{
		{name: "unknown n", in: "n", errSubstr: "不支持的字段"},
		{name: "unknown foo", in: "foo", errSubstr: "不支持的字段"},
		{name: "quality and background catalog order", in: "quality,background", want: []string{"quality", "background"}},
		{name: "input order ignored", in: "background,quality", want: []string{"quality", "background"}},
		{name: "mix known and unknown", in: "quality,n", errSubstr: "不支持的字段"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseImageToolAllowedFields(tc.in)
			if tc.errSubstr != "" {
				requireErrContains(t, err, tc.errSubstr)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestNormalizeConfigValueImageToolFields(t *testing.T) {
	t.Parallel()
	_, err := normalizeConfigValue("image_tool_allowed_fields", []string{"n"})
	requireErrContains(t, err, "不支持的字段")
	_, err = normalizeConfigValue("image_tool_allowed_fields", []string{"foo"})
	requireErrContains(t, err, "不支持的字段")
	got, err := normalizeConfigValue("image_tool_allowed_fields", []string{"background", "quality"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "quality,background" {
		t.Fatalf("got %q", got)
	}
}

func TestPublicValueImageToolIgnoresUnknownStoredFields(t *testing.T) {
	t.Parallel()
	def, ok := definitionByKey("image_tool_allowed_fields")
	if !ok {
		t.Fatal("missing definition")
	}
	got, ok := publicValue(def, "quality,foo,n").([]string)
	if !ok {
		t.Fatalf("type %T", publicValue(def, "quality,foo,n"))
	}
	if !slices.Equal(got, []string{"quality"}) {
		t.Fatalf("got %v", got)
	}
}

func TestSMTPConfigCatalogAndDefaults(t *testing.T) {
	t.Parallel()
	wantKeys := []string{
		"smtp_host", "smtp_port", "smtp_security", "smtp_username",
		"smtp_password", "smtp_from_address", "smtp_from_name",
	}
	defs := configDefinitions()
	byKey := map[string]configDefinition{}
	for _, def := range defs {
		byKey[def.Key] = def
	}
	for _, key := range wantKeys {
		def, ok := byKey[key]
		if !ok {
			t.Fatalf("missing %s", key)
		}
		if def.Category != "安全与运维" {
			t.Fatalf("%s category %q", key, def.Category)
		}
	}
	if def := byKey["smtp_password"]; !def.Secret || def.InputType != "password" {
		t.Fatalf("password definition %+v", def)
	}
	if def := byKey["smtp_port"]; def.Minimum == nil || *def.Minimum != 1 || def.Maximum == nil || *def.Maximum != 65535 {
		t.Fatalf("port bounds %+v", def)
	}
	dummy := &Store{}
	if got := envDefault(dummy, "smtp_port"); got != "587" {
		t.Fatalf("port default %q", got)
	}
	if got := envDefault(dummy, "smtp_security"); got != "starttls" {
		t.Fatalf("security default %q", got)
	}
}

func TestNormalizeSMTPConfigValueRejectsMalformedInputs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		key   string
		value any
		want  string
	}{
		{name: "port zero", key: "smtp_port", value: 0, want: "1 到 65535"},
		{name: "port too high", key: "smtp_port", value: 65536, want: "1 到 65535"},
		{name: "port fraction", key: "smtp_port", value: 587.5, want: "1 到 65535"},
		{name: "security", key: "smtp_security", value: "plain", want: "starttls 或 tls"},
		{name: "host injection", key: "smtp_host", value: "smtp.example\r\nX: y", want: "不能包含"},
		{name: "name injection", key: "smtp_from_name", value: "Sender\nBcc: x", want: "不能包含"},
		{name: "address injection", key: "smtp_from_address", value: "a@example.com\r\nBcc: x", want: "不能包含"},
		{name: "address syntax", key: "smtp_from_address", value: "not-an-address", want: "邮箱地址格式无效"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			def, ok := definitionByKey(tc.key)
			if !ok {
				t.Fatal("missing definition")
			}
			_, err := normalizeSMTPConfigValue(def, tc.value)
			requireErrContains(t, err, tc.want)
		})
	}
}

func TestSMTPReadinessAllowsPartialConfigWithoutMalformedValues(t *testing.T) {
	t.Parallel()
	if err := validateMergedSMTP(map[string]string{"smtp_host": "smtp.example.com"}); err != nil {
		t.Fatal(err)
	}
	if err := validateMergedSMTP(map[string]string{"smtp_from_address": "a@example.com"}); err != nil {
		t.Fatal(err)
	}
	if (smtpConfig{host: "smtp.example.com", port: 587, security: "starttls", fromAddress: "a@example.com"}).ready() != true {
		t.Fatal("complete no-auth configuration should be ready")
	}
	if (smtpConfig{host: "smtp.example.com", port: 587, security: "starttls", username: "user", fromAddress: "a@example.com"}).ready() {
		t.Fatal("partial credentials should not be ready")
	}
}

func TestNormalizeSMTPPasswordPreservesSignificantSpaces(t *testing.T) {
	t.Parallel()
	def, ok := definitionByKey("smtp_password")
	if !ok {
		t.Fatal("missing password definition")
	}
	got, err := normalizeSMTPConfigValue(def, "  secret with spaces  ")
	if err != nil {
		t.Fatal(err)
	}
	if got != "  secret with spaces  " {
		t.Fatalf("password was changed to %q", got)
	}
}
