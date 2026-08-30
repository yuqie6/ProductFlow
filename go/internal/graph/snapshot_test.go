package graph

import "testing"

func TestProductSourceDictRecordsLegacyFallback(t *testing.T) {
	id := "prod-1"
	got, ok := productSourceDict(&productSourceSnapshot{
		SourceProductID: &id,
		Facts:           []map[string]any{},
		LegacyFallback:  true,
	}).(map[string]any)
	if !ok {
		t.Fatal("dict type")
	}
	if got["legacy_fallback"] != true {
		t.Fatalf("legacy_fallback %+v", got["legacy_fallback"])
	}
	explicit, ok := productSourceDict(&productSourceSnapshot{
		SourceProductID: &id,
		Facts:           []map[string]any{},
		LegacyFallback:  false,
	}).(map[string]any)
	if !ok || explicit["legacy_fallback"] != false {
		t.Fatalf("explicit %+v", explicit)
	}
	roundTrip := productSourceFromDict(got)
	if !roundTrip.LegacyFallback {
		t.Fatalf("from dict %+v", roundTrip)
	}
}
