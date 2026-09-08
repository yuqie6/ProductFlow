package imageeval

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func annotationPoolFixture(t *testing.T) (poolPath, storageRoot string, manifest Manifest, candidatePaths map[string]string) {
	t.Helper()
	storageRoot = t.TempDir()
	poolPath = filepath.Join(PoolRoot(storageRoot), PoolDir)
	refBody := solidPNG(t, 640, 640)
	heroBody := solidPNG(t, 641, 640)
	detailBody := solidPNG(t, 640, 641)
	manifest = Manifest{
		ID: "annotation-case-1", Source: "taobao", URL: "https://item.example/annotation-1",
		Title: "测试商品", Category: "home", ImageTypes: []TypeCount{{Key: "hero", Quantity: 1}, {Key: "detail", Quantity: 1}},
		References: []PoolImage{{Path: "references/ref.png", TypeKey: "sku", Role: "sku", Width: 640, Height: 640, ByteSize: len(refBody), SourceURL: "https://img.example/ref"}},
		Gold: []PoolImage{
			{Path: "gold/hero.png", TypeKey: "hero", Role: "gallery", Width: 641, Height: 640, ByteSize: len(heroBody), SourceURL: "https://img.example/hero"},
			{Path: "gold/detail.png", TypeKey: "detail", Role: "detail", Width: 640, Height: 641, ByteSize: len(detailBody), SourceURL: "https://img.example/detail"},
		},
	}
	manifest.References[0].SHA256 = FileSHA256(refBody)
	manifest.Gold[0].SHA256 = FileSHA256(heroBody)
	manifest.Gold[1].SHA256 = FileSHA256(detailBody)
	caseDir := filepath.Join(poolPath, manifest.ID)
	for _, dir := range []string{filepath.Join(caseDir, "references"), filepath.Join(caseDir, "gold")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for path, body := range map[string][]byte{
		filepath.Join(caseDir, manifest.References[0].Path): refBody,
		filepath.Join(caseDir, manifest.Gold[0].Path):       heroBody,
		filepath.Join(caseDir, manifest.Gold[1].Path):       detailBody,
	} {
		if err := os.WriteFile(path, body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := WriteAdmittedCase(storageRoot, manifest); err != nil {
		t.Fatal(err)
	}
	candidatePaths = map[string]string{}
	for _, imageType := range []string{"hero", "detail"} {
		width, height := 642, 640
		if imageType == "detail" {
			height = 641
		}
		body := solidPNG(t, width, height)
		path := filepath.Join(storageRoot, "candidate-"+imageType+".png")
		if err := os.WriteFile(path, body, 0o644); err != nil {
			t.Fatal(err)
		}
		candidatePaths[imageType] = path
	}
	return poolPath, storageRoot, manifest, candidatePaths
}

func annotationSelectionFixture(t *testing.T) (AnnotationSelection, string, map[string]string) {
	t.Helper()
	poolPath, storageRoot, _, candidates := annotationPoolFixture(t)
	selectionPath := filepath.Join(storageRoot, "selection.json")
	selection, err := PrepareAnnotationSelection(poolPath, 1, 7, "test-commit", selectionPath)
	if err != nil {
		t.Fatal(err)
	}
	return selection, selectionPath, candidates
}

func writeAnnotationSelection(t *testing.T, path string, selection AnnotationSelection) {
	t.Helper()
	raw, err := json.MarshalIndent(selection, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

func validAnnotationResult(in AnnotationInput) AnnotationResult {
	targetEvidence := []string{in.Target.AssetID}
	result := AnnotationResult{
		Status:          AnnotationStatusComplete,
		Scores:          &Scores{Fidelity: 4, Fit: 4, Utility: 3, Aesthetics: 4},
		Identity:        AnnotationIdentity{Status: "match"},
		Strengths:       []AnnotationObservation{{Text: "主体清楚", EvidenceAssetIDs: targetEvidence, Severity: "info", Certainty: "observed"}},
		Weaknesses:      []AnnotationObservation{{Text: "细节可加强", EvidenceAssetIDs: targetEvidence, Severity: "minor", Certainty: "likely"}},
		Recommendations: []AnnotationRecommendation{{Text: "补充局部细节", Rationale: "便于核验材质", EvidenceAssetIDs: targetEvidence, Validation: "人工复核细节图"}},
	}
	if in.QualityReference != nil {
		result.QualityReferenceScores = &Scores{Fidelity: 3, Fit: 3, Utility: 3, Aesthetics: 3}
		result.ComparisonBasis = &AnnotationComparisonBasis{
			Status:             AnnotationComparisonComparable,
			TargetPurpose:      "清楚展示候选商品主体与细节",
			ReferencePurpose:   "清楚展示真实商品主体与细节",
			SharedRequirements: []string{"主体身份和关键结构清楚可核验"},
			Reason:             "两图承担相同的商品展示职责，具备共同评分依据",
			EvidenceAssetIDs:   []string{in.Target.AssetID, in.QualityReference.AssetID},
		}
		result.Strengths[0].EvidenceAssetIDs = []string{in.Target.AssetID, in.QualityReference.AssetID}
	}
	return result
}

func comparisonAnnotationInput() AnnotationInput {
	quality := AnnotationAsset{AssetID: "quality", ImageType: "detail", Role: "quality_reference"}
	return AnnotationInput{
		Category: "home", ImageType: "detail", Variant: "workbench",
		Target:           AnnotationAsset{AssetID: "target", ImageType: "detail", Variant: "workbench", Role: "evaluated_result"},
		QualityReference: &quality,
	}
}

func TestPrepareAnnotationSelectionWritesExplicitSHAAndDoesNotOverwrite(t *testing.T) {
	selection, path, _ := annotationSelectionFixture(t)
	if selection.Mode != AnnotationModeReference || selection.N != 1 || len(selection.Cases) != 1 {
		t.Fatalf("selection metadata: %+v", selection)
	}
	if AnnotationSelectionSchemaVersion != "image-annotation-selection.v1" || AnnotationContractVersion != "category-image-annotation.v2" || selection.SchemaVersion != AnnotationSelectionSchemaVersion || selection.ContractVersion != AnnotationContractVersion {
		t.Fatalf("selection contract was not kept at v1 shape with v2 contract: %+v", selection)
	}
	item := selection.Cases[0]
	if len(item.IdentityReferences) != 1 || len(item.QualityReferences) != 2 || len(item.Candidates) != 0 {
		t.Fatalf("selection assets: refs=%d quality=%d candidates=%d", len(item.IdentityReferences), len(item.QualityReferences), len(item.Candidates))
	}
	for _, asset := range append(append(item.IdentityReferences, item.QualityReferences...), item.Candidates...) {
		if len(asset.SHA256) != 64 || asset.ByteSize < 1 || asset.Path == "" {
			t.Fatalf("asset missing explicit bytes metadata: %+v", asset)
		}
	}
	if _, err := PrepareAnnotationSelection(selection.PoolPath, 1, 7, "test-commit", path); err == nil {
		t.Fatal("expected existing selection refusal")
	}
	loaded, err := LoadAnnotationSelection(path)
	if err != nil || loaded.PoolIndexSHA256 != selection.PoolIndexSHA256 {
		t.Fatalf("load selection: %+v %v", loaded, err)
	}
}

func TestPrepareAnnotationSelectionForTypesSelectsOneRotatedType(t *testing.T) {
	poolPath, storageRoot, _, _ := annotationPoolFixture(t)
	path := filepath.Join(storageRoot, "rotated-selection.json")
	selection, err := PrepareAnnotationSelectionForTypes(poolPath, 1, 7, "test-commit", path, []string{"detail"})
	if err != nil {
		t.Fatal(err)
	}
	if len(selection.Cases) != 1 || len(selection.Cases[0].ImageTypes) != 1 || selection.Cases[0].ImageTypes[0] != "detail" {
		t.Fatalf("unexpected rotated selection: %+v", selection.Cases)
	}
	if _, err := RunAnnotations(context.Background(), AnnotationRunConfig{SelectionPath: path, Judge: AnnotationClientFunc(func(ctx context.Context, in AnnotationInput) (AnnotationResult, error) {
		return validAnnotationResult(in), nil
	})}); err != nil {
		t.Fatal(err)
	}
}

func TestRunAnnotationsReferenceModeProducesEvidenceAndIndependentReport(t *testing.T) {
	selection, selectionPath, _ := annotationSelectionFixture(t)
	calls := 0
	var seen []AnnotationInput
	judge := AnnotationClientFunc(func(ctx context.Context, in AnnotationInput) (AnnotationResult, error) {
		calls++
		seen = append(seen, in)
		return validAnnotationResult(in), nil
	})
	report, err := RunAnnotations(context.Background(), AnnotationRunConfig{SelectionPath: selectionPath, Commit: "commit", Model: "test-model", Judge: judge})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || len(report.Cases) != 1 || len(report.Cases[0].Records) != 2 {
		t.Fatalf("calls=%d report=%+v", calls, report)
	}
	for _, in := range seen {
		if in.QualityReference != nil || in.Target.Role != "quality_reference" || len(in.IdentityReferences) != 1 {
			t.Fatalf("reference input unexpectedly comparison-shaped: %+v", in)
		}
	}
	if report.Cases[0].Records[0].Comparison != nil || len(report.Groups) != 2 {
		t.Fatalf("reference report comparison/groups: %+v", report)
	}
	if report.Groups[0].MeanScores == nil || report.Groups[0].Comparable != 0 {
		t.Fatalf("reference group should have absolute scores only: %+v", report.Groups[0])
	}
	out := filepath.Join(filepath.Dir(filepath.Dir(selection.PoolPath)), "annotation-output")
	if err := WriteAnnotationReport(out, report); err != nil {
		t.Fatal(err)
	}
	if err := WriteAnnotationReport(selection.PoolPath, report); err == nil {
		t.Fatal("report output inside pool must be rejected")
	}
	if err := WriteAnnotationReport(filepath.Join(filepath.Dir(selection.PoolPath), RunsDir, "new-run"), report); err == nil {
		t.Fatal("report output inside historical image-evals root must be rejected")
	}
	if _, err := os.Stat(filepath.Join(out, "latest.json")); !os.IsNotExist(err) {
		t.Fatalf("annotation report must not create latest pointer: %v", err)
	}
	markdown, err := os.ReadFile(filepath.Join(out, "report.md"))
	if err != nil || !strings.Contains(string(markdown), "类目展示重点") || !strings.Contains(string(markdown), "quality:hero") {
		t.Fatalf("markdown missing criteria/evidence: err=%v body=%s", err, markdown)
	}
	loaded, err := LoadAnnotationReport(filepath.Join(out, "report.json"))
	if err != nil || loaded.RunID != report.RunID {
		t.Fatalf("report round trip: %+v %v", loaded, err)
	}
	if err := WriteAnnotationReport(out, report); err == nil {
		t.Fatal("expected report overwrite refusal")
	}
	_ = selection
}

func TestAnnotationOutputReservationIsExclusiveAndBoundToReport(t *testing.T) {
	selection, path, _ := annotationSelectionFixture(t)
	report, err := RunAnnotations(context.Background(), AnnotationRunConfig{SelectionPath: path, Commit: "commit", Model: "test-model", Judge: AnnotationClientFunc(func(ctx context.Context, in AnnotationInput) (AnnotationResult, error) {
		return validAnnotationResult(in), nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(filepath.Dir(filepath.Dir(selection.PoolPath)), "reserved-output")
	reservation := reservationFromReport(report)
	if err := ReserveAnnotationOutput(out, reservation); err != nil {
		t.Fatal(err)
	}
	stored, err := loadAnnotationReservation(out)
	if err != nil || !reservationMatchesReport(stored, reservation) {
		t.Fatalf("reservation metadata: %+v %v", stored, err)
	}
	if err := ReserveAnnotationOutput(out, reservation); err == nil {
		t.Fatal("reservation must refuse an existing output directory")
	}
	if err := WriteAnnotationReport(out, report); err != nil {
		t.Fatal(err)
	}
}

func TestRunAnnotationsComparisonRequiresExplicitCandidatesAndShowsFourDeltas(t *testing.T) {
	selection, path, candidates := annotationSelectionFixture(t)
	selection.Mode = AnnotationModeComparison
	selection.Cases[0].Candidates = []AnnotationAsset{}
	if err := ValidateAnnotationSelection(selection); err == nil || !strings.Contains(err.Error(), "requires explicit candidates") {
		t.Fatalf("expected comparison candidate requirement, got %v", err)
	}
	invalidPath := filepath.Join(filepath.Dir(path), "invalid-comparison-selection.json")
	writeAnnotationSelection(t, invalidPath, selection)
	if _, err := RunAnnotations(context.Background(), AnnotationRunConfig{SelectionPath: invalidPath, Judge: AnnotationClientFunc(func(context.Context, AnnotationInput) (AnnotationResult, error) {
		t.Fatal("judge must not run for invalid selection")
		return AnnotationResult{}, nil
	})}); err == nil {
		t.Fatal("invalid comparison selection should fail before judging")
	}

	for _, imageType := range []string{"hero", "detail"} {
		body, err := os.ReadFile(candidates[imageType])
		if err != nil {
			t.Fatal(err)
		}
		width, height := 642, 640
		if imageType == "detail" {
			height = 641
		}
		selection.Cases[0].Candidates = append(selection.Cases[0].Candidates, AnnotationAsset{
			AssetID: "candidate:" + imageType + ":workbench", Path: candidates[imageType], SHA256: FileSHA256(body),
			Width: width, Height: height, ByteSize: len(body), ImageType: imageType, Variant: "workbench",
			Role: "evaluated_result", Source: "external",
		})
	}
	withoutQuality := selection
	withoutQuality.Cases = append([]AnnotationCaseSelection(nil), selection.Cases...)
	withoutQuality.Cases[0] = selection.Cases[0]
	withoutQuality.Cases[0].QualityReferences = nil
	withoutQualityPath := filepath.Join(filepath.Dir(path), "without-quality-selection.json")
	writeAnnotationSelection(t, withoutQualityPath, withoutQuality)
	withoutQualityCalls := 0
	withoutQualityReport, err := RunAnnotations(context.Background(), AnnotationRunConfig{SelectionPath: withoutQualityPath, Judge: AnnotationClientFunc(func(context.Context, AnnotationInput) (AnnotationResult, error) {
		withoutQualityCalls++
		return AnnotationResult{}, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	if withoutQualityCalls != 0 || len(withoutQualityReport.Cases[0].Records) != 2 {
		t.Fatalf("missing quality reference must not call judge: calls=%d cases=%+v", withoutQualityCalls, withoutQualityReport.Cases)
	}
	for _, group := range withoutQualityReport.Groups {
		if group.Uncomparable != 1 || group.Failed != 0 {
			t.Fatalf("missing quality reference must count one uncomparable record: %+v", group)
		}
	}
	for _, record := range withoutQualityReport.Cases[0].Records {
		if record.Status != AnnotationStatusUncomparable || record.FailureCode != "uncomparable_material" || record.Comparison == nil || record.Comparison.Status != AnnotationComparisonUncomparable {
			t.Fatalf("missing quality reference was not explicit uncomparable: %+v", record)
		}
	}
	comparisonPath := filepath.Join(filepath.Dir(path), "comparison-selection.json")
	writeAnnotationSelection(t, comparisonPath, selection)
	calls := 0
	report, err := RunAnnotations(context.Background(), AnnotationRunConfig{SelectionPath: comparisonPath, Commit: "commit", Model: "test-model", Judge: AnnotationClientFunc(func(ctx context.Context, in AnnotationInput) (AnnotationResult, error) {
		calls++
		if in.QualityReference == nil || in.Target.Role != "evaluated_result" || in.Variant != "workbench" {
			t.Fatalf("candidate did not receive explicit baseline: %+v", in)
		}
		return validAnnotationResult(in), nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || len(report.Cases[0].Records) != 2 {
		t.Fatalf("comparison calls/records: %d %+v", calls, report.Cases)
	}
	for _, record := range report.Cases[0].Records {
		if record.Comparison == nil || record.Comparison.Status != AnnotationComparisonComparable || record.Comparison.Delta == nil || record.Comparison.Verdict != "stronger" {
			t.Fatalf("comparison missing four-dimensional result: %+v", record)
		}
	}
	for _, group := range report.Groups {
		if group.Comparable != 1 || group.MeanDelta == nil || group.MeanReferenceScores == nil {
			t.Fatalf("group comparison aggregation: %+v", group)
		}
	}
}

func TestComparisonBasisControlsDeltaAndVerdict(t *testing.T) {
	in := comparisonAnnotationInput()
	for _, status := range []string{AnnotationComparisonDifferentPurpose, AnnotationComparisonInsufficientEvidence} {
		t.Run(status, func(t *testing.T) {
			result := validAnnotationResult(in)
			result.ComparisonBasis.Status = status
			result.ComparisonBasis.SharedRequirements = []string{}
			result.QualityReferenceScores = nil
			record := runAnnotationRecord(context.Background(), AnnotationClientFunc(func(context.Context, AnnotationInput) (AnnotationResult, error) {
				return result, nil
			}), in, in.QualityReference)
			if record.Status != AnnotationStatusComplete || record.Scores == nil || record.ComparisonBasis == nil {
				t.Fatalf("non-comparable assessment lost target evidence: %+v", record)
			}
			if record.Comparison == nil || record.Comparison.Status != status || record.Comparison.QualityReferenceScores != nil || record.Comparison.Delta != nil || record.Comparison.Verdict != "" {
				t.Fatalf("non-comparable comparison produced a result: %+v", record.Comparison)
			}
			group := AggregateAnnotationGroups([]AnnotationCaseReport{{Category: in.Category, Records: []AnnotationRecord{record}}})[0]
			if group.Comparable != 0 || group.Uncomparable != 1 || group.MeanReferenceScores != nil || group.MeanDelta != nil || len(group.VerdictCounts) != 0 || group.Status != "partial" {
				t.Fatalf("non-comparable evidence entered aggregate comparison: %+v", group)
			}
			if got := annotationCaseStatus([]AnnotationRecord{record}); got != "partial" {
				t.Fatalf("non-comparable case was marked complete: %s", got)
			}
			markdown := RenderAnnotationMarkdown(AnnotationReport{Cases: []AnnotationCaseReport{{Category: in.Category, Records: []AnnotationRecord{record}}}})
			if !strings.Contains(markdown, "Comparison basis:") || !strings.Contains(markdown, "no reference scores, delta, or verdict") || !strings.Contains(markdown, record.ComparisonBasis.Reason) {
				t.Fatalf("comparison basis was not rendered without score claims: %s", markdown)
			}
		})
	}
	unknown := validAnnotationResult(in)
	unknown.Status = AnnotationStatusUnknown
	unknown.Scores = nil
	unknown.QualityReferenceScores = nil
	unknown.Uncertainties = []AnnotationUncertainty{{Text: "无法判断", Reason: "目标图证据不足", NextCheck: "人工复核", EvidenceAssetIDs: []string{"target"}}}
	record := runAnnotationRecord(context.Background(), AnnotationClientFunc(func(context.Context, AnnotationInput) (AnnotationResult, error) {
		return unknown, nil
	}), in, in.QualityReference)
	if record.Status != AnnotationStatusUnknown || record.Comparison == nil || record.Comparison.Status != AnnotationComparisonUncomparable || record.Comparison.Delta != nil || record.Comparison.Verdict != "indeterminate" {
		t.Fatalf("unknown assessment became comparable: record_status=%s comparison=%+v", record.Status, record.Comparison)
	}
	group := AggregateAnnotationGroups([]AnnotationCaseReport{{Category: in.Category, Records: []AnnotationRecord{record}}})[0]
	if group.Comparable != 0 || group.Uncomparable != 1 || group.MeanScores != nil || group.MeanDelta != nil || group.Status != "partial" {
		t.Fatalf("unknown comparison aggregation: %+v", group)
	}
}

func TestComparisonBasisValidationRejectsMissingContradictoryAndWrongEvidence(t *testing.T) {
	in := comparisonAnnotationInput()
	cases := []struct {
		name   string
		mutate func(*AnnotationResult)
		want   string
	}{
		{name: "missing", mutate: func(result *AnnotationResult) { result.ComparisonBasis = nil }, want: "requires comparison basis"},
		{name: "comparable without reference scores", mutate: func(result *AnnotationResult) { result.QualityReferenceScores = nil }, want: "requires quality reference scores"},
		{name: "different purpose with reference scores", mutate: func(result *AnnotationResult) {
			result.ComparisonBasis.Status = AnnotationComparisonDifferentPurpose
		}, want: "cannot contain quality reference scores"},
		{name: "wrong evidence", mutate: func(result *AnnotationResult) {
			result.ComparisonBasis.EvidenceAssetIDs = []string{"target", "other"}
		}, want: "quality reference asset"},
		{name: "empty shared requirement", mutate: func(result *AnnotationResult) {
			result.ComparisonBasis.SharedRequirements = nil
		}, want: "shared_requirements"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := validAnnotationResult(in)
			tc.mutate(&result)
			record := runAnnotationRecord(context.Background(), AnnotationClientFunc(func(context.Context, AnnotationInput) (AnnotationResult, error) {
				return result, nil
			}), in, in.QualityReference)
			if record.Status != AnnotationStatusFailed || record.FailureCode != "invalid_output" || record.Scores != nil || record.ComparisonBasis != nil {
				t.Fatalf("invalid output was persisted as usable evidence: %+v", record)
			}
			if record.Diagnostic == nil || !strings.Contains(record.Diagnostic.Reason, tc.want) {
				t.Fatalf("missing validation diagnostic: %+v", record.Diagnostic)
			}
			if record.Comparison == nil || record.Comparison.Status != AnnotationComparisonUncomparable || record.Comparison.Delta != nil || record.Comparison.Verdict != "indeterminate" {
				t.Fatalf("invalid comparison metadata: %+v", record.Comparison)
			}
		})
	}

	reference := validAnnotationResult(AnnotationInput{Target: in.Target})
	reference.ComparisonBasis = &AnnotationComparisonBasis{
		Status:             AnnotationComparisonDifferentPurpose,
		TargetPurpose:      "候选展示",
		ReferencePurpose:   "真实展示",
		SharedRequirements: []string{},
		Reason:             "有共同依据",
		EvidenceAssetIDs:   []string{"target", "quality"},
	}
	record := runAnnotationRecord(context.Background(), AnnotationClientFunc(func(context.Context, AnnotationInput) (AnnotationResult, error) {
		return reference, nil
	}), AnnotationInput{Target: in.Target}, in.QualityReference)
	if record.Status != AnnotationStatusFailed || record.FailureCode != "invalid_output" || record.Diagnostic == nil || !strings.Contains(record.Diagnostic.Reason, "reference annotation") {
		reason := ""
		if record.Diagnostic != nil {
			reason = record.Diagnostic.Reason
		}
		t.Fatalf("reference comparison basis was accepted: status=%s code=%s reason=%s", record.Status, record.FailureCode, reason)
	}
}

func TestRunAnnotationsRejectsTamperedBytesBeforeJudge(t *testing.T) {
	_, path, _ := annotationSelectionFixture(t)
	selection, err := LoadAnnotationSelection(path)
	if err != nil {
		t.Fatal(err)
	}
	assetPath := filepath.Join(selection.PoolPath, selection.Cases[0].CaseID, selection.Cases[0].QualityReferences[0].Path)
	if err := os.WriteFile(assetPath, []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	calls := 0
	_, err = RunAnnotations(context.Background(), AnnotationRunConfig{SelectionPath: path, Judge: AnnotationClientFunc(func(context.Context, AnnotationInput) (AnnotationResult, error) {
		calls++
		return validAnnotationResult(AnnotationInput{}), nil
	})})
	if err == nil || (!strings.Contains(err.Error(), "SHA256") && !strings.Contains(err.Error(), "byte size")) || calls != 0 {
		t.Fatalf("expected preflight SHA failure, err=%v calls=%d", err, calls)
	}
}

func TestParseAnnotationJSONStrictAndUnknownRequiresReason(t *testing.T) {
	good := `{"status":"complete","scores":{"fidelity":4,"fit":3,"utility":4,"aesthetics":5},"quality_reference_scores":null,"identity":{"status":"match","claims":[]},"strengths":[],"weaknesses":[],"uncertainties":[],"recommendations":[],"critical_errors":[]}`
	if _, err := ParseAnnotationJSON(good); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseAnnotationJSON(strings.TrimSuffix(good, "}") + `,"extra":true}`); err == nil {
		t.Fatal("unknown model field must be rejected")
	}
	unknown := `{"status":"unknown","scores":null,"quality_reference_scores":null,"identity":{"status":"unknown","claims":[]},"strengths":[],"weaknesses":[],"uncertainties":[],"recommendations":[],"critical_errors":[]}`
	if _, err := ParseAnnotationJSON(unknown); err == nil {
		t.Fatal("unknown result without uncertainty must be rejected")
	}
	unknown = `{"status":"unknown","scores":null,"quality_reference_scores":null,"identity":{"status":"unknown","claims":[]},"strengths":[],"weaknesses":[],"uncertainties":[{"text":"看不清","reason":"分辨率不足","next_check":"看原图","evidence_asset_ids":["target"]}],"recommendations":[],"critical_errors":[]}`
	if _, err := ParseAnnotationJSON(unknown); err != nil {
		t.Fatal(err)
	}
}

func TestCriticalIdentityMismatchRetainsScoresButBlocksComparison(t *testing.T) {
	selection, path, candidates := annotationSelectionFixture(t)
	selection.Mode = AnnotationModeComparison
	for _, imageType := range []string{"hero", "detail"} {
		body, err := os.ReadFile(candidates[imageType])
		if err != nil {
			t.Fatal(err)
		}
		width, height := 642, 640
		if imageType == "detail" {
			height = 641
		}
		selection.Cases[0].Candidates = append(selection.Cases[0].Candidates, AnnotationAsset{
			AssetID: "critical:" + imageType, Path: candidates[imageType], SHA256: FileSHA256(body), Width: width, Height: height, ByteSize: len(body), ImageType: imageType, Variant: "bad", Role: "evaluated_result", Source: "external",
		})
	}
	comparisonPath := filepath.Join(filepath.Dir(path), "critical-selection.json")
	writeAnnotationSelection(t, comparisonPath, selection)
	report, err := RunAnnotations(context.Background(), AnnotationRunConfig{SelectionPath: comparisonPath, Judge: AnnotationClientFunc(func(ctx context.Context, in AnnotationInput) (AnnotationResult, error) {
		result := validAnnotationResult(in)
		result.Identity.Status = "mismatch"
		result.CriticalErrors = []AnnotationObservation{{Text: "身份不一致", EvidenceAssetIDs: []string{in.Target.AssetID}, Severity: "critical", Certainty: "observed"}}
		return result, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	for _, group := range report.Groups {
		if group.CriticalErrorCount != 1 || group.Comparable != 0 || group.MeanScores == nil || group.MeanDelta == nil || group.ScoreCoverage != 1 || group.Status != "critical" {
			t.Fatalf("critical result was hidden or passed: %+v", group)
		}
	}
}

func TestUnknownIdentityKeepsDeltaWithoutConclusiveVerdict(t *testing.T) {
	selection, path, candidates := annotationSelectionFixture(t)
	selection.Mode = AnnotationModeComparison
	for _, imageType := range []string{"hero", "detail"} {
		body, err := os.ReadFile(candidates[imageType])
		if err != nil {
			t.Fatal(err)
		}
		width, height := 642, 640
		if imageType == "detail" {
			height = 641
		}
		selection.Cases[0].Candidates = append(selection.Cases[0].Candidates, AnnotationAsset{
			AssetID: "unknown-identity:" + imageType, Path: candidates[imageType], SHA256: FileSHA256(body), Width: width, Height: height, ByteSize: len(body), ImageType: imageType, Variant: "workbench", Role: "evaluated_result", Source: "external",
		})
	}
	comparisonPath := filepath.Join(filepath.Dir(path), "unknown-identity-selection.json")
	writeAnnotationSelection(t, comparisonPath, selection)
	report, err := RunAnnotations(context.Background(), AnnotationRunConfig{SelectionPath: comparisonPath, Judge: AnnotationClientFunc(func(ctx context.Context, in AnnotationInput) (AnnotationResult, error) {
		result := validAnnotationResult(in)
		result.Identity.Status = "unknown"
		return result, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	if report.Cases[0].Status != "partial" {
		t.Fatalf("unknown identity must make case partial: %+v", report.Cases[0])
	}
	for _, group := range report.Groups {
		if group.Comparable != 0 || group.MeanDelta == nil || group.VerdictCounts["indeterminate"] != 1 || group.Status != "partial" {
			t.Fatalf("unknown identity must keep delta without a conclusive verdict: %+v", group)
		}
	}
}

func TestAnnotationProviderFailureIsSeparateFromUnknown(t *testing.T) {
	selection, path, _ := annotationSelectionFixture(t)
	report, err := RunAnnotations(context.Background(), AnnotationRunConfig{SelectionPath: path, Judge: AnnotationClientFunc(func(ctx context.Context, in AnnotationInput) (AnnotationResult, error) {
		if in.ImageType == "hero" {
			return AnnotationResult{}, errors.New("provider body should not be persisted")
		}
		result := validAnnotationResult(in)
		result.Status = AnnotationStatusUnknown
		result.Scores = nil
		result.Uncertainties = []AnnotationUncertainty{{Text: "模型不确定", Reason: "证据不足", NextCheck: "人工复核", EvidenceAssetIDs: []string{in.Target.AssetID}}}
		return result, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	if report.Cases[0].Records[0].Status != AnnotationStatusFailed || report.Cases[0].Records[0].FailureCode != "provider_error" {
		t.Fatalf("provider failure not separated: %+v", report.Cases[0].Records[0])
	}
	if report.Cases[0].Records[1].Status != AnnotationStatusUnknown || report.Cases[0].Records[1].FailureCode != "" {
		t.Fatalf("unknown status not preserved: %+v", report.Cases[0].Records[1])
	}
	if selection.Cases[0].CaseID == "" {
		t.Fatal("fixture selection unexpectedly empty")
	}
}

func TestVisionJudgeAnnotationUsesCategoryCriteriaAndChatFallback(t *testing.T) {
	selection, _, _ := annotationSelectionFixture(t)
	selected := selection.Cases[0]
	identity := append([]AnnotationAsset(nil), selected.IdentityReferences...)
	for i := range identity {
		identity[i].Path = filepath.Join(selection.PoolPath, selected.CaseID, identity[i].Path)
	}
	target := selected.QualityReferences[0]
	target.Path = filepath.Join(selection.PoolPath, selected.CaseID, target.Path)
	input := AnnotationInput{
		CaseID: selected.CaseID, Category: selected.Category, Title: selected.Title,
		ImageType: target.ImageType, Variant: "reference", Criteria: AnnotationCriteria(selected.Category, target.ImageType),
		IdentityReferences: identity, Target: target,
	}
	responseBody, err := json.Marshal(validAnnotationResult(input))
	if err != nil {
		t.Fatal(err)
	}
	for _, responseStatus := range []int{0, http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusBadGateway} {
		responseStatus := responseStatus
		responsesHits, chatHits := 0, 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer test-key" {
				t.Errorf("missing judge authorization")
			}
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), "本请求没有质量对照图，quality_reference_scores 必须为 null") {
				t.Errorf("reference mode omitted explicit identity/quality role contract")
			}
			switch r.URL.Path {
			case "/v1/responses":
				responsesHits++
				if !strings.Contains(string(body), "分类电商图片质量评审员") || !strings.Contains(string(body), "关注材质触感") || !strings.Contains(string(body), target.AssetID) {
					t.Errorf("annotation request omitted prompt/criteria/asset id")
				}
				if responseStatus != 0 {
					w.WriteHeader(responseStatus)
					_, _ = io.WriteString(w, `{"error":"temporary"}`)
					return
				}
				_, _ = io.WriteString(w, `{"output_text":`+strconvQuote(string(responseBody))+`}`)
			case "/v1/chat/completions":
				chatHits++
				if !strings.Contains(string(body), "分类电商图片质量评审员") || !strings.Contains(string(body), "关注材质触感") {
					t.Errorf("chat annotation request omitted prompt/criteria")
				}
				_, _ = io.WriteString(w, `{"choices":[{"message":{"content":`+strconvQuote(string(responseBody))+`}}]}`)
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		judge := VisionJudge{BaseURL: srv.URL, APIKey: "test-key", Model: "test-model", HTTPClient: srv.Client()}
		got, err := judge.Annotate(context.Background(), input)
		srv.Close()
		if responseStatus == http.StatusBadGateway {
			if err == nil || chatHits != 0 {
				t.Fatalf("unsupported fallback status must not retry: err=%v responses=%d chat=%d", err, responsesHits, chatHits)
			}
			continue
		}
		if err != nil || got.Status != AnnotationStatusComplete {
			t.Fatalf("responseStatus=%d annotation result=%+v err=%v", responseStatus, got, err)
		}
		wantChat := responseStatus == http.StatusNotFound || responseStatus == http.StatusMethodNotAllowed
		if responsesHits != 1 || (wantChat && chatHits != 1) || (!wantChat && chatHits != 0) {
			t.Fatalf("responseStatus=%d responses=%d chat=%d", responseStatus, responsesHits, chatHits)
		}
	}
}

func TestVisionJudgeAnnotationDoesNotFallbackOnCanceledRequest(t *testing.T) {
	selection, _, _ := annotationSelectionFixture(t)
	selected := selection.Cases[0]
	identity := append([]AnnotationAsset(nil), selected.IdentityReferences...)
	for i := range identity {
		identity[i].Path = filepath.Join(selection.PoolPath, selected.CaseID, identity[i].Path)
	}
	target := selected.QualityReferences[0]
	target.Path = filepath.Join(selection.PoolPath, selected.CaseID, target.Path)
	input := AnnotationInput{
		CaseID: selected.CaseID, Category: selected.Category, Title: selected.Title,
		ImageType: target.ImageType, Variant: "reference", Criteria: AnnotationCriteria(selected.Category, target.ImageType),
		IdentityReferences: identity, Target: target,
	}
	responsesHits, chatHits := 0, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/responses" {
			responsesHits++
		} else if r.URL.Path == "/v1/chat/completions" {
			chatHits++
		}
		w.WriteHeader(http.StatusBadGateway)
	}))
	client := srv.Client()
	client.Transport = cancelRoundTripper{}
	judge := VisionJudge{BaseURL: srv.URL, APIKey: "test-key", Model: "test-model", HTTPClient: client}
	_, err := judge.Annotate(context.Background(), input)
	srv.Close()
	if err == nil || !errors.Is(err, context.Canceled) || responsesHits != 0 || chatHits != 0 {
		t.Fatalf("canceled request must not fallback: err=%v responses=%d chat=%d", err, responsesHits, chatHits)
	}
}

type cancelRoundTripper struct{}

func (cancelRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, context.Canceled
}

func strconvQuote(value string) string {
	raw, _ := json.Marshal(value)
	return string(raw)
}
