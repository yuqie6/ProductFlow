package imageeval

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const annotationReservationFilename = ".annotation-reservation.json"

type annotationCasePlan struct {
	selection  AnnotationCaseSelection
	identity   []AnnotationAsset
	quality    map[string]AnnotationAsset
	candidates []AnnotationAsset
}

// RunAnnotations evaluates only the explicit assets in a selection. It never
// creates products, workflows, graphs, or generated images.
func RunAnnotations(ctx context.Context, cfg AnnotationRunConfig) (AnnotationReport, error) {
	if cfg.Judge == nil {
		return AnnotationReport{}, fmt.Errorf("annotation judge is required")
	}
	if strings.TrimSpace(cfg.SelectionPath) == "" {
		return AnnotationReport{}, fmt.Errorf("annotation selection path is required")
	}
	rawSelection, err := os.ReadFile(cfg.SelectionPath)
	if err != nil {
		return AnnotationReport{}, err
	}
	selection, err := LoadAnnotationSelection(cfg.SelectionPath)
	if err != nil {
		return AnnotationReport{}, err
	}
	plans, err := buildAnnotationPlans(selection)
	if err != nil {
		return AnnotationReport{}, err
	}
	started := time.Now().UTC()
	runID := strings.TrimSpace(cfg.RunID)
	if runID == "" {
		runID = annotationRunID(started, rawSelection)
	}
	report := AnnotationReport{
		SchemaVersion:   AnnotationSchemaVersion,
		ContractVersion: AnnotationContractVersion,
		RunID:           runID,
		Mode:            selection.Mode,
		Provenance: AnnotationProvenance{
			SelectionPath:   cfg.SelectionPath,
			SelectionSHA256: FileSHA256(rawSelection),
			PoolPath:        selection.cleanPoolPath(),
			PoolIndexSHA256: selection.PoolIndexSHA256,
			SchemaVersion:   AnnotationSchemaVersion,
			ContractVersion: AnnotationContractVersion,
			PromptSHA256:    FileSHA256([]byte(AnnotationSystemPrompt)),
			Model:           strings.TrimSpace(cfg.Model),
			Commit:          firstNonEmptyString(cfg.Commit, selection.Commit, "unknown"),
			StartedAt:       started,
		},
		Cases: make([]AnnotationCaseReport, 0, len(plans)),
	}
	for _, plan := range plans {
		if ctx.Err() != nil {
			break
		}
		report.Cases = append(report.Cases, annotateCase(ctx, cfg.Judge, selection.Mode, plan))
	}
	// Cancellation is an explicit run-level gap; retain already completed case
	// records and add failed records for remaining planned cases.
	if len(report.Cases) < len(plans) {
		for _, plan := range plans[len(report.Cases):] {
			report.Cases = append(report.Cases, failedAnnotationCase(plan, selection.Mode, "context_cancelled"))
		}
	}
	report.Provenance.FinishedAt = time.Now().UTC()
	report.Groups = AggregateAnnotationGroups(report.Cases)
	return report, nil
}

// AnnotationRunID creates the stable identity used by a selection snapshot.
// Callers should generate it before reserving the output directory.
func AnnotationRunID(started time.Time, selection []byte) string {
	return annotationRunID(started, selection)
}

// ReserveAnnotationOutput atomically claims a fresh output directory before
// any model request. The reservation is retained as inspectable run metadata.
func ReserveAnnotationOutput(outputDir string, reservation AnnotationOutputReservation) error {
	if err := validateAnnotationReservation(reservation); err != nil {
		return err
	}
	outputDir, err := annotationOutputDirectory(outputDir, reservation.PoolPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(outputDir), 0o755); err != nil {
		return err
	}
	if err := os.Mkdir(outputDir, 0o755); err != nil {
		return fmt.Errorf("annotation output must be a new directory: %w", err)
	}
	body, err := json.MarshalIndent(reservation, "", "  ")
	if err != nil {
		_ = os.Remove(outputDir)
		return err
	}
	reservationPath := filepath.Join(outputDir, annotationReservationFilename)
	if err := writeBytesExclusive(reservationPath, body); err != nil {
		_ = os.Remove(reservationPath)
		_ = os.Remove(outputDir)
		return err
	}
	return nil
}

// WriteAnnotationReport writes JSON and Markdown side by side, without a
// latest pointer and without replacing an existing report. Direct package
// callers get the same reservation behavior as the CLI; the CLI reserves the
// directory before it resolves or calls the judge.
func WriteAnnotationReport(outputDir string, report AnnotationReport) error {
	if strings.TrimSpace(outputDir) == "" {
		return fmt.Errorf("annotation output directory is required")
	}
	if report.SchemaVersion != AnnotationSchemaVersion || report.ContractVersion != AnnotationContractVersion || report.RunID == "" {
		return fmt.Errorf("invalid annotation report metadata")
	}
	reservation := reservationFromReport(report)
	var err error
	outputDir, err = ensureAnnotationOutputReservation(outputDir, reservation)
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(outputDir, "report.json")); err == nil {
		return fmt.Errorf("annotation report already exists: %s", filepath.Join(outputDir, "report.json"))
	} else if !os.IsNotExist(err) {
		return err
	}
	if _, err := os.Stat(filepath.Join(outputDir, "report.md")); err == nil {
		return fmt.Errorf("annotation report already exists: %s", filepath.Join(outputDir, "report.md"))
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return err
	}
	jsonPath := filepath.Join(outputDir, "report.json")
	markdownPath := filepath.Join(outputDir, "report.md")
	jsonBody, err := marshalAnnotationReport(report)
	if err != nil {
		return err
	}
	markdownBody := []byte(RenderAnnotationMarkdown(report))
	if err := writeBytesExclusive(jsonPath, jsonBody); err != nil {
		return err
	}
	if err := writeBytesExclusive(markdownPath, markdownBody); err != nil {
		_ = os.Remove(jsonPath)
		return err
	}
	return nil
}

func validateAnnotationReservation(reservation AnnotationOutputReservation) error {
	if reservation.SchemaVersion != AnnotationSchemaVersion || reservation.ContractVersion != AnnotationContractVersion {
		return fmt.Errorf("invalid annotation reservation metadata")
	}
	if strings.TrimSpace(reservation.RunID) == "" || strings.TrimSpace(reservation.SelectionPath) == "" || strings.TrimSpace(reservation.PoolPath) == "" {
		return fmt.Errorf("annotation reservation requires run, selection, and pool metadata")
	}
	if err := (AnnotationSelection{Mode: reservation.Mode}).validateMode(); err != nil {
		return err
	}
	if err := validateSHA(reservation.SelectionSHA256, "selection"); err != nil {
		return err
	}
	if err := validateSHA(reservation.PoolIndexSHA256, "pool index"); err != nil {
		return err
	}
	if err := validateSHA(reservation.PromptSHA256, "prompt"); err != nil {
		return err
	}
	if strings.TrimSpace(reservation.Commit) == "" || reservation.ReservedAt.IsZero() {
		return fmt.Errorf("annotation reservation requires commit and reserved_at")
	}
	return nil
}

func annotationOutputDirectory(outputDir, poolPath string) (string, error) {
	if strings.TrimSpace(outputDir) == "" {
		return "", fmt.Errorf("annotation output directory is required")
	}
	if strings.TrimSpace(poolPath) == "" {
		return "", fmt.Errorf("annotation report pool path is required")
	}
	outputDir, err := filepath.Abs(outputDir)
	if err != nil {
		return "", err
	}
	// Keep immutable annotation artifacts outside image-evals itself. This
	// prevents a caller from creating a new directory under the historical
	// runs tree while still passing per-file overwrite checks.
	evaluationRoot := filepath.Dir(filepath.Clean(poolPath))
	if pathWithin(evaluationRoot, outputDir) {
		return "", fmt.Errorf("annotation output must be outside image-evals root")
	}
	return filepath.Clean(outputDir), nil
}

func reservationFromReport(report AnnotationReport) AnnotationOutputReservation {
	reservedAt := report.Provenance.StartedAt
	if reservedAt.IsZero() {
		reservedAt = time.Now().UTC()
	}
	return AnnotationOutputReservation{
		SchemaVersion:   report.SchemaVersion,
		ContractVersion: report.ContractVersion,
		RunID:           report.RunID,
		Mode:            report.Mode,
		SelectionPath:   report.Provenance.SelectionPath,
		SelectionSHA256: report.Provenance.SelectionSHA256,
		PoolPath:        report.Provenance.PoolPath,
		PoolIndexSHA256: report.Provenance.PoolIndexSHA256,
		PromptSHA256:    report.Provenance.PromptSHA256,
		Model:           report.Provenance.Model,
		Commit:          report.Provenance.Commit,
		ReservedAt:      reservedAt,
	}
}

func ensureAnnotationOutputReservation(outputDir string, reservation AnnotationOutputReservation) (string, error) {
	if err := validateAnnotationReservation(reservation); err != nil {
		return "", err
	}
	outputDir, err := annotationOutputDirectory(outputDir, reservation.PoolPath)
	if err != nil {
		return "", err
	}
	info, statErr := os.Stat(outputDir)
	switch {
	case statErr == nil:
		if !info.IsDir() {
			return "", fmt.Errorf("annotation output is not a directory: %s", outputDir)
		}
		stored, err := loadAnnotationReservation(outputDir)
		if err != nil {
			return "", err
		}
		if !reservationMatchesReport(stored, reservation) {
			return "", fmt.Errorf("annotation output reservation metadata does not match report")
		}
		return outputDir, nil
	case !os.IsNotExist(statErr):
		return "", statErr
	default:
		if err := ReserveAnnotationOutput(outputDir, reservation); err != nil {
			return "", err
		}
		return outputDir, nil
	}
}

func loadAnnotationReservation(outputDir string) (AnnotationOutputReservation, error) {
	raw, err := os.ReadFile(filepath.Join(outputDir, annotationReservationFilename))
	if err != nil {
		return AnnotationOutputReservation{}, fmt.Errorf("annotation output is not reserved: %w", err)
	}
	var reservation AnnotationOutputReservation
	if err := decodeStrictJSON(raw, &reservation); err != nil {
		return AnnotationOutputReservation{}, fmt.Errorf("annotation reservation: %w", err)
	}
	if err := validateAnnotationReservation(reservation); err != nil {
		return AnnotationOutputReservation{}, err
	}
	return reservation, nil
}

func reservationMatchesReport(stored, expected AnnotationOutputReservation) bool {
	return stored.SchemaVersion == expected.SchemaVersion &&
		stored.ContractVersion == expected.ContractVersion &&
		stored.RunID == expected.RunID &&
		stored.Mode == expected.Mode &&
		stored.SelectionPath == expected.SelectionPath &&
		strings.EqualFold(stored.SelectionSHA256, expected.SelectionSHA256) &&
		filepath.Clean(stored.PoolPath) == filepath.Clean(expected.PoolPath) &&
		strings.EqualFold(stored.PoolIndexSHA256, expected.PoolIndexSHA256) &&
		strings.EqualFold(stored.PromptSHA256, expected.PromptSHA256) &&
		stored.Commit == expected.Commit &&
		(stored.Model == "" || expected.Model == "" || stored.Model == expected.Model)
}

func LoadAnnotationReport(path string) (AnnotationReport, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return AnnotationReport{}, err
	}
	var report AnnotationReport
	if err := decodeStrictJSON(raw, &report); err != nil {
		return AnnotationReport{}, err
	}
	if report.SchemaVersion != AnnotationSchemaVersion || report.ContractVersion != AnnotationContractVersion || report.RunID == "" {
		return AnnotationReport{}, fmt.Errorf("invalid annotation report metadata")
	}
	return report, nil
}

func marshalAnnotationReport(report AnnotationReport) ([]byte, error) {
	// Keep report JSON strict on read while using the standard encoder for stable
	// field names and human-readable snapshots.
	return json.MarshalIndent(report, "", "  ")
}

func buildAnnotationPlans(selection AnnotationSelection) ([]annotationCasePlan, error) {
	poolPath := selection.cleanPoolPath()
	pool, indexSHA, err := loadAnnotationPool(poolPath)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(indexSHA, selection.PoolIndexSHA256) {
		return nil, fmt.Errorf("annotation pool index SHA256 changed")
	}
	byID := make(map[string]Manifest, len(pool))
	for _, item := range pool {
		byID[item.ID] = item
	}
	plans := make([]annotationCasePlan, 0, len(selection.Cases))
	for _, selected := range selection.Cases {
		m, ok := byID[selected.CaseID]
		if !ok {
			return nil, fmt.Errorf("selection case %s is absent from pool", selected.CaseID)
		}
		manifestRaw, err := os.ReadFile(filepath.Join(poolPath, selected.CaseID, "manifest.json"))
		if err != nil {
			return nil, err
		}
		if !strings.EqualFold(FileSHA256(manifestRaw), selected.ManifestSHA256) {
			return nil, fmt.Errorf("manifest SHA256 changed for case %s", selected.CaseID)
		}
		if m.Category != selected.Category || m.Title != selected.Title {
			return nil, fmt.Errorf("selection metadata changed for case %s", selected.CaseID)
		}
		if err := validateSelectedTypes(m, selected.ImageTypes); err != nil {
			return nil, fmt.Errorf("case %s: %w", selected.CaseID, err)
		}
		plan := annotationCasePlan{
			selection:  selected,
			quality:    make(map[string]AnnotationAsset, len(selected.QualityReferences)),
			candidates: append([]AnnotationAsset(nil), selected.Candidates...),
		}
		for _, asset := range selected.IdentityReferences {
			resolved, err := resolveAndVerifySelectedAsset(selection, selected.CaseID, asset, m.References, nil)
			if err != nil {
				return nil, err
			}
			plan.identity = append(plan.identity, resolved)
		}
		for _, asset := range selected.QualityReferences {
			resolved, err := resolveAndVerifySelectedAsset(selection, selected.CaseID, asset, m.Gold, []string{asset.ImageType})
			if err != nil {
				return nil, err
			}
			plan.quality[asset.ImageType] = resolved
		}
		for _, asset := range selected.Candidates {
			if asset.Source != "external" && asset.Source != "manifest.gold" && asset.Source != "manifest.references" {
				return nil, fmt.Errorf("case %s candidate %s has unsupported source", selected.CaseID, asset.AssetID)
			}
			var manifestAssets []PoolImage
			switch asset.Source {
			case "manifest.gold":
				manifestAssets = m.Gold
			case "manifest.references":
				manifestAssets = m.References
			}
			resolved, err := resolveAndVerifySelectedAsset(selection, selected.CaseID, asset, manifestAssets, nil)
			if err != nil {
				return nil, err
			}
			for i := range plan.candidates {
				if plan.candidates[i].AssetID == asset.AssetID {
					plan.candidates[i] = resolved
				}
			}
		}
		plans = append(plans, plan)
	}
	return plans, nil
}

func validateSelectedTypes(manifest Manifest, selected []string) error {
	if len(selected) == 0 || len(selected) > len(manifest.ImageTypes) {
		return fmt.Errorf("selected image type list is invalid")
	}
	manifestTypes := make(map[string]struct{}, len(manifest.ImageTypes))
	for _, item := range manifest.ImageTypes {
		manifestTypes[item.Key] = struct{}{}
	}
	for _, item := range selected {
		if _, ok := manifestTypes[item]; !ok {
			return fmt.Errorf("image type %q is absent from manifest", item)
		}
	}
	return nil
}

func resolveAndVerifySelectedAsset(selection AnnotationSelection, caseID string, asset AnnotationAsset, manifestAssets []PoolImage, allowedTypes []string) (AnnotationAsset, error) {
	path, err := resolveSelectedAssetPath(selection, caseID, asset)
	if err != nil {
		return AnnotationAsset{}, err
	}
	if len(manifestAssets) > 0 {
		matched := false
		for _, source := range manifestAssets {
			if source.Path == asset.Path && strings.EqualFold(source.SHA256, asset.SHA256) {
				if asset.SourceTypeKey != "" && source.TypeKey != "" && asset.SourceTypeKey != source.TypeKey {
					return AnnotationAsset{}, fmt.Errorf("case %s asset %s source type changed", caseID, asset.AssetID)
				}
				matched = true
				break
			}
		}
		if !matched {
			return AnnotationAsset{}, fmt.Errorf("case %s asset %s is not the selected manifest asset", caseID, asset.AssetID)
		}
	}
	for _, typeKey := range allowedTypes {
		if asset.ImageType != typeKey {
			return AnnotationAsset{}, fmt.Errorf("case %s asset %s image type changed", caseID, asset.AssetID)
		}
	}
	if err := verifyAnnotationAsset(path, asset); err != nil {
		return AnnotationAsset{}, fmt.Errorf("case %s asset %s: %w", caseID, asset.AssetID, err)
	}
	asset.Path = path
	return asset, nil
}

func resolveSelectedAssetPath(selection AnnotationSelection, caseID string, asset AnnotationAsset) (string, error) {
	if filepath.IsAbs(asset.Path) {
		return filepath.Clean(asset.Path), nil
	}
	if asset.Source == "external" {
		return "", fmt.Errorf("external asset %s path must be absolute", asset.AssetID)
	}
	caseRoot := filepath.Join(selection.cleanPoolPath(), caseID)
	path := filepath.Join(caseRoot, filepath.Clean(asset.Path))
	if !pathWithin(caseRoot, path) || filepath.Clean(path) == filepath.Clean(caseRoot) {
		return "", fmt.Errorf("asset %s path escapes case directory", asset.AssetID)
	}
	return path, nil
}

func annotateCase(ctx context.Context, judge AnnotationClient, mode string, plan annotationCasePlan) AnnotationCaseReport {
	selected := plan.selection
	out := AnnotationCaseReport{
		CaseID:             selected.CaseID,
		Category:           selected.Category,
		Title:              selected.Title,
		ManifestSHA256:     selected.ManifestSHA256,
		Mode:               mode,
		ImageTypes:         append([]string(nil), selected.ImageTypes...),
		IdentityReferences: append([]AnnotationAsset(nil), selected.IdentityReferences...),
		QualityReferences:  append([]AnnotationAsset(nil), selected.QualityReferences...),
		Candidates:         append([]AnnotationAsset(nil), selected.Candidates...),
		Records:            []AnnotationRecord{},
	}
	if mode == AnnotationModeReference {
		for _, imageType := range selected.ImageTypes {
			quality := plan.quality[imageType]
			input := AnnotationInput{
				CaseID: selected.CaseID, Category: selected.Category, Title: selected.Title,
				ImageType: imageType, Variant: "reference", Criteria: AnnotationCriteria(selected.Category, imageType),
				IdentityReferences: plan.identity, Target: quality,
			}
			out.Records = append(out.Records, runAnnotationRecord(ctx, judge, input, nil))
		}
	} else {
		for _, candidate := range plan.candidates {
			quality, ok := plan.quality[candidate.ImageType]
			if !ok {
				record := uncomparableAnnotationRecord(candidate, selected.Category, "uncomparable_material")
				record.Comparison = unavailableAnnotationComparison("")
				out.Records = append(out.Records, record)
				continue
			}
			input := AnnotationInput{
				CaseID: selected.CaseID, Category: selected.Category, Title: selected.Title,
				ImageType: candidate.ImageType, Variant: candidate.Variant,
				Criteria:           AnnotationCriteria(selected.Category, candidate.ImageType),
				IdentityReferences: plan.identity, QualityReference: &quality, Target: candidate,
			}
			out.Records = append(out.Records, runAnnotationRecord(ctx, judge, input, &quality))
		}
	}
	out.Status = annotationCaseStatus(out.Records)
	return out
}

func runAnnotationRecord(ctx context.Context, judge AnnotationClient, input AnnotationInput, quality *AnnotationAsset) AnnotationRecord {
	result, err := judge.Annotate(ctx, input)
	if err != nil {
		record := failedAnnotationRecord(input.Target, input.Category, annotationFailureCode(err))
		if quality != nil {
			record.Comparison = unavailableAnnotationComparison(quality.AssetID)
		}
		return record
	}
	if err := validateAnnotationResultForInput(result, input); err != nil {
		record := failedAnnotationRecord(input.Target, input.Category, "invalid_output")
		if quality != nil {
			record.Comparison = unavailableAnnotationComparison(quality.AssetID)
		}
		return record
	}
	record := result.toRecord(input)
	if quality != nil {
		record.Comparison = makeAnnotationComparison(record, result.QualityReferenceScores, quality.AssetID)
	}
	return record
}

func validateAnnotationResultForInput(result AnnotationResult, input AnnotationInput) error {
	if err := validateAnnotationShape(result); err != nil {
		return err
	}
	if input.QualityReference == nil && result.QualityReferenceScores != nil {
		return fmt.Errorf("reference annotation cannot contain quality reference scores")
	}
	ids := map[string]struct{}{input.Target.AssetID: {}}
	for _, asset := range input.IdentityReferences {
		ids[asset.AssetID] = struct{}{}
	}
	if input.QualityReference != nil {
		ids[input.QualityReference.AssetID] = struct{}{}
	}
	check := func(evidence []string) error {
		for _, id := range evidence {
			if _, ok := ids[id]; !ok {
				return fmt.Errorf("evidence asset id %q is not in input", id)
			}
		}
		return nil
	}
	for _, item := range result.Identity.Claims {
		if err := check(item.EvidenceAssetIDs); err != nil {
			return err
		}
	}
	for _, item := range result.Strengths {
		if err := check(item.EvidenceAssetIDs); err != nil {
			return err
		}
	}
	for _, item := range result.Weaknesses {
		if err := check(item.EvidenceAssetIDs); err != nil {
			return err
		}
	}
	for _, item := range result.CriticalErrors {
		if err := check(item.EvidenceAssetIDs); err != nil {
			return err
		}
	}
	for _, item := range result.Uncertainties {
		if err := check(item.EvidenceAssetIDs); err != nil {
			return err
		}
	}
	for _, item := range result.Recommendations {
		if err := check(item.EvidenceAssetIDs); err != nil {
			return err
		}
	}
	return nil
}

func makeAnnotationComparison(record AnnotationRecord, referenceScores *Scores, referenceAssetID string) *AnnotationComparison {
	comparison := unavailableAnnotationComparison(referenceAssetID)
	if record.Status != AnnotationStatusComplete || record.Scores == nil || referenceScores == nil {
		return comparison
	}
	comparison.Status = AnnotationComparisonComparable
	comparison.QualityReferenceScores = referenceScores
	comparison.Delta = &ScoreDelta{
		Fidelity:   record.Scores.Fidelity - referenceScores.Fidelity,
		Fit:        record.Scores.Fit - referenceScores.Fit,
		Utility:    record.Scores.Utility - referenceScores.Utility,
		Aesthetics: record.Scores.Aesthetics - referenceScores.Aesthetics,
	}
	if record.criticalErrorCount() > 0 || record.Identity.Status != "match" {
		return comparison
	}
	comparison.Verdict = scoreDeltaVerdict(*comparison.Delta)
	return comparison
}

func scoreDeltaVerdict(delta ScoreDelta) string {
	values := []float64{delta.Fidelity, delta.Fit, delta.Utility, delta.Aesthetics}
	allNonNegative, allNonPositive, anyPositive, anyNegative := true, true, false, false
	for _, value := range values {
		if value < 0 {
			allNonNegative = false
			anyNegative = true
		}
		if value > 0 {
			allNonPositive = false
			anyPositive = true
		}
	}
	switch {
	case allNonNegative && anyPositive:
		return "stronger"
	case allNonPositive && anyNegative:
		return "weaker"
	case !anyPositive && !anyNegative:
		return "equal"
	default:
		return "mixed"
	}
}

func AggregateAnnotationGroups(cases []AnnotationCaseReport) []AnnotationGroupReport {
	type groupKey struct{ category, imageType, variant string }
	type groupAccumulator struct {
		AnnotationGroupReport
		scoreSum Scores
		refSum   Scores
		deltaSum ScoreDelta
		scoreN   int
		refN     int
		deltaN   int
	}
	groups := map[groupKey]*groupAccumulator{}
	for _, item := range cases {
		for _, record := range item.Records {
			key := groupKey{item.Category, record.ImageType, record.Variant}
			acc := groups[key]
			if acc == nil {
				acc = &groupAccumulator{AnnotationGroupReport: AnnotationGroupReport{
					Category: item.Category, ImageType: record.ImageType, Variant: record.Variant,
					VerdictCounts: map[string]int{},
				}}
				groups[key] = acc
			}
			acc.Eligible++
			switch record.Status {
			case AnnotationStatusComplete:
				acc.Completed++
			case AnnotationStatusUnknown:
				acc.Unknown++
			case AnnotationStatusFailed:
				acc.Failed++
			}
			critical := record.criticalErrorCount()
			acc.CriticalErrorCount += critical
			recordUncomparable := record.Status == AnnotationStatusUncomparable
			if recordUncomparable {
				acc.Uncomparable++
			}
			if record.Scores != nil && record.Status == AnnotationStatusComplete {
				acc.scoreSum.Fidelity += record.Scores.Fidelity
				acc.scoreSum.Fit += record.Scores.Fit
				acc.scoreSum.Utility += record.Scores.Utility
				acc.scoreSum.Aesthetics += record.Scores.Aesthetics
				acc.scoreN++
			}
			if record.Comparison != nil {
				if record.Comparison.Status == AnnotationComparisonComparable {
					if critical == 0 && record.Identity.Status == "match" {
						acc.Comparable++
					}
					// Keep all valid dimension values visible, including records with
					// critical or identity-uncertain findings. The conclusive count
					// above and indeterminate verdict keep those findings from passing.
					if record.Comparison.QualityReferenceScores != nil {
						acc.refSum.Fidelity += record.Comparison.QualityReferenceScores.Fidelity
						acc.refSum.Fit += record.Comparison.QualityReferenceScores.Fit
						acc.refSum.Utility += record.Comparison.QualityReferenceScores.Utility
						acc.refSum.Aesthetics += record.Comparison.QualityReferenceScores.Aesthetics
						acc.refN++
					}
					if record.Comparison.Delta != nil {
						acc.deltaSum.Fidelity += record.Comparison.Delta.Fidelity
						acc.deltaSum.Fit += record.Comparison.Delta.Fit
						acc.deltaSum.Utility += record.Comparison.Delta.Utility
						acc.deltaSum.Aesthetics += record.Comparison.Delta.Aesthetics
						acc.deltaN++
					}
				} else if !recordUncomparable {
					acc.Uncomparable++
				}
				if record.Comparison.Verdict != "" {
					acc.VerdictCounts[record.Comparison.Verdict]++
				}
			}
		}
	}
	keys := make([]groupKey, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].category != keys[j].category {
			return keys[i].category < keys[j].category
		}
		if keys[i].imageType != keys[j].imageType {
			return keys[i].imageType < keys[j].imageType
		}
		return keys[i].variant < keys[j].variant
	})
	out := make([]AnnotationGroupReport, 0, len(keys))
	for _, key := range keys {
		acc := groups[key]
		if acc.Eligible > 0 {
			acc.ScoreCoverage = float64(acc.scoreN) / float64(acc.Eligible)
		}
		if acc.scoreN > 0 {
			acc.MeanScores = &Scores{
				Fidelity: acc.scoreSum.Fidelity / float64(acc.scoreN), Fit: acc.scoreSum.Fit / float64(acc.scoreN),
				Utility: acc.scoreSum.Utility / float64(acc.scoreN), Aesthetics: acc.scoreSum.Aesthetics / float64(acc.scoreN),
			}
		}
		if acc.refN > 0 {
			acc.MeanReferenceScores = &Scores{
				Fidelity: acc.refSum.Fidelity / float64(acc.refN), Fit: acc.refSum.Fit / float64(acc.refN),
				Utility: acc.refSum.Utility / float64(acc.refN), Aesthetics: acc.refSum.Aesthetics / float64(acc.refN),
			}
		}
		if acc.deltaN > 0 {
			acc.MeanDelta = &ScoreDelta{
				Fidelity: acc.deltaSum.Fidelity / float64(acc.deltaN), Fit: acc.deltaSum.Fit / float64(acc.deltaN),
				Utility: acc.deltaSum.Utility / float64(acc.deltaN), Aesthetics: acc.deltaSum.Aesthetics / float64(acc.deltaN),
			}
		}
		if acc.CriticalErrorCount > 0 {
			acc.Status = "critical"
		} else if acc.Completed < acc.Eligible || acc.Unknown > 0 || acc.Failed > 0 || acc.Uncomparable > 0 || acc.VerdictCounts["indeterminate"] > 0 {
			acc.Status = "partial"
		} else {
			acc.Status = "complete"
		}
		out = append(out, acc.AnnotationGroupReport)
	}
	return out
}

func annotationCaseStatus(records []AnnotationRecord) string {
	if len(records) == 0 {
		return AnnotationStatusFailed
	}
	critical := 0
	allComplete := true
	anyComplete := false
	anyIndeterminate := false
	for _, record := range records {
		critical += record.criticalErrorCount()
		if record.Status == AnnotationStatusComplete {
			anyComplete = true
		} else {
			allComplete = false
		}
		if record.Comparison != nil && record.Comparison.Verdict == "indeterminate" {
			anyIndeterminate = true
		}
	}
	if critical > 0 {
		return "critical"
	}
	if allComplete && !anyIndeterminate {
		return AnnotationStatusComplete
	}
	if anyComplete {
		return "partial"
	}
	return "partial"
}

func failedAnnotationCase(plan annotationCasePlan, mode, code string) AnnotationCaseReport {
	selected := plan.selection
	records := make([]AnnotationRecord, 0)
	if mode == AnnotationModeReference {
		for _, asset := range selected.QualityReferences {
			records = append(records, failedAnnotationRecord(asset, selected.Category, code))
		}
	} else {
		for _, asset := range selected.Candidates {
			record := failedAnnotationRecord(asset, selected.Category, code)
			if quality, ok := plan.quality[asset.ImageType]; ok {
				record.Comparison = unavailableAnnotationComparison(quality.AssetID)
			} else {
				record.Comparison = unavailableAnnotationComparison("")
			}
			records = append(records, record)
		}
	}
	return AnnotationCaseReport{
		CaseID: selected.CaseID, Category: selected.Category, Title: selected.Title,
		ManifestSHA256: selected.ManifestSHA256, Mode: mode,
		ImageTypes:         append([]string(nil), selected.ImageTypes...),
		IdentityReferences: append([]AnnotationAsset(nil), selected.IdentityReferences...),
		QualityReferences:  append([]AnnotationAsset(nil), selected.QualityReferences...),
		Candidates:         append([]AnnotationAsset(nil), selected.Candidates...),
		Records:            records, Status: AnnotationStatusFailed,
	}
}

func failedAnnotationRecord(asset AnnotationAsset, category, code string) AnnotationRecord {
	variant := asset.Variant
	if variant == "" {
		variant = "reference"
	}
	return AnnotationRecord{
		AssetID: asset.AssetID, ImageType: asset.ImageType, Variant: variant,
		Role: asset.Role, Criteria: AnnotationCriteria(category, asset.ImageType),
		Status: AnnotationStatusFailed, FailureCode: code,
	}
}

func uncomparableAnnotationRecord(asset AnnotationAsset, category, code string) AnnotationRecord {
	var record = failedAnnotationRecord(asset, category, code)
	record.Status = AnnotationStatusUncomparable
	return record
}

func unavailableAnnotationComparison(referenceAssetID string) *AnnotationComparison {
	return &AnnotationComparison{
		Status:                  AnnotationComparisonUncomparable,
		QualityReferenceAssetID: referenceAssetID,
		Verdict:                 "indeterminate",
	}
}

func annotationFailureCode(err error) string {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return "context_cancelled"
	}
	var responseErr annotationResponseError
	if errors.As(err, &responseErr) {
		return "invalid_output"
	}
	if strings.Contains(strings.ToLower(err.Error()), "json") || strings.Contains(strings.ToLower(err.Error()), "annotation status") {
		return "invalid_output"
	}
	return "provider_error"
}

func annotationRunID(started time.Time, selection []byte) string {
	sum := sha256.Sum256(selection)
	return "annotation-" + started.Format("20060102T150405Z") + "-" + fmt.Sprintf("%x", sum[:6])
}

func writeBytesExclusive(path string, body []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.Write(body); err != nil {
		return err
	}
	return file.Sync()
}
