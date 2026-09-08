package imageeval

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestAnnotationRejectedOutputKeepsDiagnostic(t *testing.T) {
	selection, _, _ := annotationSelectionFixture(t)
	selected := selection.Cases[0]
	target := selected.QualityReferences[0]
	target.Path = filepath.Join(selection.PoolPath, selected.CaseID, target.Path)
	input := AnnotationInput{Category: selected.Category, ImageType: target.ImageType, Target: target}
	valid := validAnnotationResult(input)
	validBody, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}
	invalidEvidence := valid
	invalidEvidence.Identity.Claims = []AnnotationObservation{{Text: "visible shape", EvidenceAssetIDs: []string{"not-supplied"}, Severity: "info", Certainty: "observed"}}
	invalidBody, err := json.Marshal(invalidEvidence)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, output, stage, reason string
		status                      int
	}{
		{"syntax", "not JSON", "parse", "annotation json", 200},
		{"shape", `{"status":"complete"}`, "parse", "requires scores", 200},
		{"evidence", string(invalidBody), "input_validation", "not in input", 200},
		{"valid", string(validBody), "", "", 200},
		{"upstream", `{"error":"private transport payload"}`, "", "", 502},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path != "/v1/responses" {
					t.Errorf("unexpected fallback: %s", r.URL.Path)
				}
				w.WriteHeader(tc.status)
				if tc.status != 200 {
					_, _ = w.Write([]byte(tc.output))
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]string{"output_text": tc.output})
			}))
			defer server.Close()
			judge := VisionJudge{BaseURL: server.URL, APIKey: "secret-test-key", Model: "test", HTTPClient: server.Client()}
			quality := &AnnotationAsset{AssetID: "quality"}
			record := runAnnotationRecord(context.Background(), judge, input, quality)
			if calls != 1 {
				t.Fatalf("unexpected retry count %d", calls)
			}
			body, err := json.Marshal(record)
			if err != nil {
				t.Fatal(err)
			}
			var persisted AnnotationRecord
			if err := json.Unmarshal(body, &persisted); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(body), "secret-test-key") || strings.Contains(string(body), "private transport payload") {
				t.Fatal("transport data leaked into report")
			}
			if tc.stage == "" {
				if persisted.Diagnostic != nil {
					t.Fatalf("unexpected diagnostic: %+v", persisted.Diagnostic)
				}
				if tc.status != 200 && persisted.FailureCode != "provider_error" {
					t.Fatalf("wrong provider classification: %+v", persisted)
				}
				if tc.status == 200 && persisted.Status != AnnotationStatusComplete {
					t.Fatalf("valid result rejected: %+v", persisted)
				}
				return
			}
			d := persisted.Diagnostic
			if d == nil || d.Stage != tc.stage || d.OutputText != tc.output || !strings.Contains(d.Reason, tc.reason) {
				t.Fatalf("missing exact rejection evidence: %+v", d)
			}
			if persisted.Status != AnnotationStatusFailed || persisted.FailureCode != "invalid_output" || persisted.Scores != nil || persisted.Comparison.Status != "uncomparable" {
				t.Fatalf("rejected output became usable evidence: %+v", persisted)
			}
		})
	}
}
