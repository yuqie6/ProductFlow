package agent

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/yuqie6/productflow/internal/platform/apperr"
)

func TestNormalizePageContextPersistsEmptyCollectionsAndRevisions(t *testing.T) {
	got, err := normalizePageContext(map[string]any{
		"route":              "/products/p1",
		"page_type":          "product_workbench",
		"product_id":         "p1",
		"workflow_id":        nil,
		"selected_asset_ids": nil,
		"filters":            map[string]any{"tab": "agent"},
		"workflow_revision":  2,
		"library_revision":   3,
		"captured_at":        "2026-08-17T12:00:00+00:00",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Route != "/products/p1" || got.PageType != "product_workbench" {
		t.Fatalf("identity %+v", got)
	}
	if got.ProductID == nil || *got.ProductID != "p1" || got.WorkflowID != nil {
		t.Fatalf("ids %+v", got)
	}
	if got.SelectedAssetIDs == nil || len(got.SelectedAssetIDs) != 0 {
		t.Fatalf("selected %+v", got.SelectedAssetIDs)
	}
	if got.VisibleAssetIDs == nil || len(got.VisibleAssetIDs) != 0 {
		t.Fatalf("visible %+v", got.VisibleAssetIDs)
	}
	if string(got.SelectedJSON) != "[]" || string(got.VisibleJSON) != "[]" {
		t.Fatalf("json %s %s", got.SelectedJSON, got.VisibleJSON)
	}
	if got.Filters["tab"] != "agent" || string(got.FiltersJSON) != `{"tab":"agent"}` {
		t.Fatalf("filters %s", got.FiltersJSON)
	}
	if got.WorkflowRevision == nil || *got.WorkflowRevision != 2 || got.LibraryRevision == nil || *got.LibraryRevision != 3 {
		t.Fatalf("revisions %+v %+v", got.WorkflowRevision, got.LibraryRevision)
	}
	if got.Digest == "" || len(got.Digest) != 64 {
		t.Fatalf("digest %s", got.Digest)
	}

	again, err := normalizePageContext(map[string]any{
		"route":             "/products/p1",
		"page_type":         "product_workbench",
		"product_id":        "p1",
		"filters":           map[string]any{"tab": "agent"},
		"workflow_revision": 2,
		"library_revision":  3,
		"captured_at":       "2026-08-17T12:00:00+00:00",
	})
	if err != nil {
		t.Fatal(err)
	}
	if again.Digest != got.Digest {
		t.Fatalf("digest should cover omitted empty collections: %s vs %s", got.Digest, again.Digest)
	}

	changed, err := normalizePageContext(map[string]any{
		"route":              "/products/p1",
		"page_type":          "product_workbench",
		"product_id":         "p1",
		"selected_asset_ids": []any{"asset-1"},
		"filters":            map[string]any{"tab": "agent"},
		"workflow_revision":  2,
		"library_revision":   3,
		"captured_at":        "2026-08-17T12:00:00+00:00",
	})
	if err != nil {
		t.Fatal(err)
	}
	if changed.Digest == got.Digest {
		t.Fatal("digest must include selected asset ids")
	}
}

func TestNormalizePageContextRejectsUnknownAndInvalidFields(t *testing.T) {
	mustValidation := func(err error) {
		t.Helper()
		if err == nil {
			t.Fatal("expected validation")
		}
		var ae apperr.Error
		if !errors.As(err, &ae) || ae.Status != 400 {
			t.Fatalf("err %v", err)
		}
	}
	_, err := normalizePageContext(map[string]any{
		"route": "/x", "page_type": "p", "captured_at": "2026-08-17T12:00:00+00:00", "extra": 1,
	})
	mustValidation(err)
	_, err = normalizePageContext(map[string]any{"page_type": "p", "captured_at": "2026-08-17T12:00:00+00:00"})
	mustValidation(err)
	_, err = normalizePageContext(map[string]any{
		"route": "/x", "page_type": "p", "captured_at": "2026-08-17T12:00:00",
	})
	mustValidation(err)
	_, err = normalizePageContext(map[string]any{
		"route": "/x", "page_type": "p", "workflow_revision": -1, "captured_at": "2026-08-17T12:00:00+00:00",
	})
	mustValidation(err)
	_, err = normalizePageContext(map[string]any{
		"route": "/x", "page_type": "p", "selected_asset_ids": []any{"a", "a"}, "captured_at": "2026-08-17T12:00:00+00:00",
	})
	mustValidation(err)
}

func TestIsoCapturedAtMatchesPythonUTC(t *testing.T) {
	parsed, err := time.Parse(time.RFC3339, "2026-08-17T12:00:00+00:00")
	if err != nil {
		t.Fatal(err)
	}
	if got := isoCapturedAt(parsed); got != "2026-08-17T12:00:00+00:00" {
		t.Fatalf("got %s", got)
	}
	if !strings.HasSuffix(isoCapturedAt(parsed.Add(123*time.Microsecond)), "+00:00") {
		t.Fatal(isoCapturedAt(parsed.Add(123 * time.Microsecond)))
	}
}
