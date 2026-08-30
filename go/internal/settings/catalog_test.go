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
