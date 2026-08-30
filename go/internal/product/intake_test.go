package product

import (
	"testing"

	"github.com/yuqie6/productflow/internal/platform/apperr"
)

func init() {
	BindDeliveryPresetSpec(func(key string) (map[string]any, error) {
		specs := map[string]map[string]any{
			"taobao_tmall_hero": stubDeliverySpec(1200, 1600),
			"jd_hero":           stubDeliverySpec(1200, 1200),
			"amazon_hero":       stubDeliverySpec(1200, 1200),
			"detail_portrait":   stubDeliverySpec(1200, 1600),
			"scene_landscape":   stubDeliverySpec(1600, 1200),
		}
		spec, ok := specs[key]
		if !ok {
			return nil, apperr.Validation("交付预设不存在")
		}
		return spec, nil
	})
}

func stubDeliverySpec(width, height int) map[string]any {
	return map[string]any{
		"width": width, "height": height, "format": "png", "fit": "contain",
		"max_byte_size": nil, "background_color": nil, "crop_anchor": nil,
	}
}

func TestParseSelectionRejectsUnknownFieldsAndInvalidQuantities(t *testing.T) {
	valid := `{"schema_version":1,"image_types":[{"key":"hero","quantity":2,"order":0}]}`
	if _, err := parseSelection(valid); err != nil {
		t.Fatal(err)
	}
	if _, err := parseSelection(`{"schema_version":1,"image_types":[{"key":"certification","quantity":3,"order":0}]}`); err != nil {
		t.Fatalf("evidence quantity should be allowed: %v", err)
	}
	if _, err := parseSelection(`{"schema_version":1,"image_types":[{"key":"hero","quantity":2,"order":0}],"delivery_preset_key":"jd_hero"}`); err != nil {
		t.Fatal(err)
	}
	trimmed, err := parseSelection(`{"schema_version":1,"image_types":[{"key":" hero ","quantity":2,"order":0}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if trimmed.ImageTypes[0].Key != "hero" {
		t.Fatalf("persisted key %q", trimmed.ImageTypes[0].Key)
	}

	invalid := []string{
		`{"schema_version":1,"image_types":[]}`,
		`{"schema_version":2,"image_types":[{"key":"hero","quantity":2,"order":0}]}`,
		`{"schema_version":1,"image_types":[{"key":"unknown","quantity":2,"order":0}]}`,
		`{"schema_version":1,"image_types":[{"key":"hero","quantity":0,"order":0}]}`,
		`{"schema_version":1,"image_types":[{"key":"hero","quantity":7,"order":0}]}`,
		`{"schema_version":1,"image_types":[{"key":"hero","quantity":2,"order":0},{"key":"hero","quantity":2,"order":1}]}`,
		`{"schema_version":1,"image_types":[{"key":"hero","quantity":2,"order":1},{"key":"scene","quantity":2,"order":0}]}`,
		`{"schema_version":1,"image_types":[{"key":"hero","quantity":6,"order":0},{"key":"scene","quantity":6,"order":1},{"key":"detail","quantity":6,"order":2},{"key":"sku","quantity":6,"order":3},{"key":"faq","quantity":6,"order":4},{"key":"shipping","quantity":1,"order":5}]}`,
		`{"schema_version":1,"image_types":[{"key":"hero","quantity":2,"order":0,"hidden":true}]}`,
		`{"schema_version":1,"image_types":[{"key":"hero","quantity":2,"order":0}],"delivery_preset_key":"not-a-real-preset"}`,
		`{"schema_version":1,"image_types":[{"key":"certification","quantity":0,"order":0}]}`,
	}
	for _, raw := range invalid {
		_, err := parseSelection(raw)
		if err == nil {
			t.Fatalf("expected error for %s", raw)
		}
		var app apperr.Error
		if !apperrAs(err, &app) || app.Detail != selectionInvalidDetail {
			t.Fatalf("%s: %v", raw, err)
		}
	}
}

func TestParseDirectImageTypesCatalogQuantityAndUnknownFields(t *testing.T) {
	got, err := parseDirectImageTypes(`[{"key":"hero","quantity":1,"aspect_ratio":"3:4"}]`)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Key != "hero" || got[0].Quantity != 1 || got[0].Order != 0 || got[0].AspectRatio != "3:4" {
		t.Fatalf("%+v", got)
	}
	if _, err := parseDirectImageTypes(`[{"key":"factory","quantity":2}]`); err != nil {
		t.Fatalf("evidence type: %v", err)
	}

	if _, err := parseDirectImageTypes(`{"key":"hero"}`); err == nil || err.(apperr.Error).Detail != directTypesArrayDetail {
		t.Fatalf("array: %v", err)
	}
	invalidFormat := []string{
		`[{"key":"hero"}]`,
		`[{"key":"hero","quantity":0}]`,
		`[{"key":"hero","quantity":7}]`,
		`[{"key":"hero","quantity":1,"hidden":true}]`,
		`[{"key":"hero","quantity":1,"aspect_ratio":"wide"}]`,
	}
	for _, raw := range invalidFormat {
		_, err := parseDirectImageTypes(raw)
		if err == nil || err.(apperr.Error).Detail != directTypesFormatDetail {
			t.Fatalf("%s: %v", raw, err)
		}
	}
	_, err = parseDirectImageTypes(`[{"key":"not-a-type","quantity":1}]`)
	if err == nil || err.(apperr.Error).Detail != "不支持的图片类型: not-a-type" {
		t.Fatalf("unknown key: %v", err)
	}
	_, err = parseDirectImageTypes(`[{"key":"hero","quantity":6},{"key":"scene","quantity":6},{"key":"detail","quantity":6},{"key":"sku","quantity":6},{"key":"faq","quantity":6},{"key":"shipping","quantity":1}]`)
	if err == nil || err.(apperr.Error).Detail != "图片生成总数不能超过 30" {
		t.Fatalf("total: %v", err)
	}
	if _, err := parseDirectImageTypes(`[{"key":"hero","quantity":6},{"key":"scene","quantity":6},{"key":"detail","quantity":6},{"key":"sku","quantity":6},{"key":"faq","quantity":6},{"key":"factory","quantity":6}]`); err != nil {
		t.Fatalf("evidence quantity must not count toward 30: %v", err)
	}
}

func apperrAs(err error, dest *apperr.Error) bool {
	e, ok := err.(apperr.Error)
	if !ok {
		return false
	}
	*dest = e
	return true
}
