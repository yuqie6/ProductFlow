package imageeval

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

const (
	AnnotationSelectionSchemaVersion         = "image-annotation-selection.v1"
	AnnotationSchemaVersion                  = "image-annotation.v2"
	AnnotationContractVersion                = "category-image-annotation.v2"
	AnnotationModeReference                  = "reference"
	AnnotationModeComparison                 = "comparison"
	AnnotationStatusComplete                 = "complete"
	AnnotationStatusUnknown                  = "unknown"
	AnnotationStatusFailed                   = "failed"
	AnnotationStatusUncomparable             = "uncomparable"
	AnnotationComparisonComparable           = "comparable"
	AnnotationComparisonDifferentPurpose     = "different_purpose"
	AnnotationComparisonInsufficientEvidence = "insufficient_evidence"
	AnnotationComparisonUncomparable         = "uncomparable"
)

// AnnotationSelection is an immutable input snapshot for one annotation run.
// Pool assets use paths relative to PoolPath/<case-id>; external candidates must
// use absolute paths so the runner never guesses a different storage root.
type AnnotationSelection struct {
	SchemaVersion   string                    `json:"schema_version"`
	ContractVersion string                    `json:"contract_version"`
	Mode            string                    `json:"mode"`
	PoolPath        string                    `json:"pool_path"`
	PoolIndexSHA256 string                    `json:"pool_index_sha256"`
	CreatedAt       time.Time                 `json:"created_at"`
	Commit          string                    `json:"commit"`
	N               int                       `json:"n"`
	Seed            int64                     `json:"seed"`
	Cases           []AnnotationCaseSelection `json:"cases"`
}

// AnnotationCaseSelection names every image that may be sent to the judge.
// A quality reference is one selected real e-commerce image for each type.
// Candidates are empty in reference mode and explicit in comparison mode.
type AnnotationCaseSelection struct {
	CaseID             string            `json:"case_id"`
	Category           string            `json:"category"`
	Title              string            `json:"title"`
	ManifestSHA256     string            `json:"manifest_sha256"`
	ImageTypes         []string          `json:"image_types"`
	IdentityReferences []AnnotationAsset `json:"identity_references"`
	QualityReferences  []AnnotationAsset `json:"quality_references"`
	Candidates         []AnnotationAsset `json:"candidates"`
}

// AnnotationAsset preserves the source identity of one image. Path is the
// selection path; resolved paths are only used in the in-memory judge input.
type AnnotationAsset struct {
	AssetID       string  `json:"asset_id"`
	Path          string  `json:"path"`
	SHA256        string  `json:"sha256"`
	Width         int     `json:"width"`
	Height        int     `json:"height"`
	ByteSize      int     `json:"byte_size"`
	ImageType     string  `json:"image_type"`
	Variant       string  `json:"variant,omitempty"`
	Role          string  `json:"role"`
	Source        string  `json:"source"`
	SourceURL     string  `json:"source_url,omitempty"`
	SourceRole    string  `json:"source_role,omitempty"`
	SourceTypeKey string  `json:"source_type_key,omitempty"`
	Confidence    float64 `json:"confidence,omitempty"`
}

// AnnotationInput is the material for one model request. Paths in this value
// are absolute and have already passed SHA/size verification.
type AnnotationInput struct {
	CaseID             string
	Category           string
	Title              string
	ImageType          string
	Variant            string
	Criteria           string
	IdentityReferences []AnnotationAsset
	QualityReference   *AnnotationAsset
	Target             AnnotationAsset
}

// AnnotationClient is the judge boundary. It returns parsed, validated model
// data; transport and model failures are represented by the returned error.
type AnnotationClient interface {
	Annotate(context.Context, AnnotationInput) (AnnotationResult, error)
}

// AnnotationClientFunc makes focused tests and local adapters concise.
type AnnotationClientFunc func(context.Context, AnnotationInput) (AnnotationResult, error)

func (f AnnotationClientFunc) Annotate(ctx context.Context, in AnnotationInput) (AnnotationResult, error) {
	return f(ctx, in)
}

// AnnotationResult is the strict model result before runner metadata and
// comparison deltas are added.
type AnnotationResult struct {
	rawOutput              string
	Status                 string                     `json:"status"`
	Scores                 *Scores                    `json:"scores"`
	QualityReferenceScores *Scores                    `json:"quality_reference_scores,omitempty"`
	ComparisonBasis        *AnnotationComparisonBasis `json:"comparison_basis,omitempty"`
	Identity               AnnotationIdentity         `json:"identity"`
	Strengths              []AnnotationObservation    `json:"strengths"`
	Weaknesses             []AnnotationObservation    `json:"weaknesses"`
	Uncertainties          []AnnotationUncertainty    `json:"uncertainties"`
	Recommendations        []AnnotationRecommendation `json:"recommendations"`
	CriticalErrors         []AnnotationObservation    `json:"critical_errors"`
}

type AnnotationIdentity struct {
	Status string                  `json:"status"`
	Claims []AnnotationObservation `json:"claims"`
}

type AnnotationObservation struct {
	Text             string   `json:"text"`
	EvidenceAssetIDs []string `json:"evidence_asset_ids"`
	Severity         string   `json:"severity"`
	Certainty        string   `json:"certainty"`
}

type AnnotationUncertainty struct {
	Text             string   `json:"text"`
	Reason           string   `json:"reason"`
	NextCheck        string   `json:"next_check"`
	EvidenceAssetIDs []string `json:"evidence_asset_ids"`
}

type AnnotationRecommendation struct {
	Text             string   `json:"text"`
	Rationale        string   `json:"rationale"`
	EvidenceAssetIDs []string `json:"evidence_asset_ids"`
	Validation       string   `json:"validation"`
}

// AnnotationRecord is the persisted assessment for one candidate or one raw
// quality reference. Comparison is nil for reference-mode records.
type AnnotationRecord struct {
	AssetID            string                     `json:"asset_id"`
	ImageType          string                     `json:"image_type"`
	Variant            string                     `json:"variant"`
	Role               string                     `json:"role"`
	Criteria           string                     `json:"criteria"`
	Status             string                     `json:"status"`
	Scores             *Scores                    `json:"scores,omitempty"`
	Identity           AnnotationIdentity         `json:"identity"`
	Strengths          []AnnotationObservation    `json:"strengths"`
	Weaknesses         []AnnotationObservation    `json:"weaknesses"`
	Uncertainties      []AnnotationUncertainty    `json:"uncertainties"`
	Recommendations    []AnnotationRecommendation `json:"recommendations"`
	CriticalErrors     []AnnotationObservation    `json:"critical_errors"`
	CriticalErrorCount int                        `json:"critical_error_count"`
	FailureCode        string                     `json:"failure_code,omitempty"`
	Diagnostic         *AnnotationDiagnostic      `json:"diagnostic,omitempty"`
	ComparisonBasis    *AnnotationComparisonBasis `json:"comparison_basis,omitempty"`
	Comparison         *AnnotationComparison      `json:"comparison,omitempty"`
}

// AnnotationDiagnostic explains rejected model output without changing scores
// or turning failed records into comparable evidence.
type AnnotationDiagnostic struct {
	Stage      string `json:"stage"`
	Reason     string `json:"reason"`
	OutputText string `json:"output_text,omitempty"`
}

// AnnotationComparisonBasis records the model's evidence for treating a
// candidate and its quality reference as comparable. The runner validates the
// structure here and binds the cited assets to the current request.
type AnnotationComparisonBasis struct {
	Status             string   `json:"status"`
	TargetPurpose      string   `json:"target_purpose"`
	ReferencePurpose   string   `json:"reference_purpose"`
	SharedRequirements []string `json:"shared_requirements"`
	Reason             string   `json:"reason"`
	EvidenceAssetIDs   []string `json:"evidence_asset_ids"`
}

type AnnotationComparison struct {
	Status                  string      `json:"status"`
	QualityReferenceAssetID string      `json:"quality_reference_asset_id"`
	QualityReferenceScores  *Scores     `json:"quality_reference_scores,omitempty"`
	Delta                   *ScoreDelta `json:"delta,omitempty"`
	Verdict                 string      `json:"verdict"`
}

// ScoreDelta is candidate minus quality-reference score, dimension by
// dimension. It is not a score and therefore is not constrained to 1..5.
type ScoreDelta struct {
	Fidelity   float64 `json:"fidelity"`
	Fit        float64 `json:"fit"`
	Utility    float64 `json:"utility"`
	Aesthetics float64 `json:"aesthetics"`
}

type AnnotationCaseReport struct {
	CaseID             string             `json:"case_id"`
	Category           string             `json:"category"`
	Title              string             `json:"title"`
	ManifestSHA256     string             `json:"manifest_sha256"`
	Mode               string             `json:"mode"`
	ImageTypes         []string           `json:"image_types"`
	IdentityReferences []AnnotationAsset  `json:"identity_references"`
	QualityReferences  []AnnotationAsset  `json:"quality_references"`
	Candidates         []AnnotationAsset  `json:"candidates"`
	Records            []AnnotationRecord `json:"records"`
	Status             string             `json:"status"`
}

type AnnotationGroupReport struct {
	Category            string         `json:"category"`
	ImageType           string         `json:"image_type"`
	Variant             string         `json:"variant"`
	Eligible            int            `json:"eligible"`
	Completed           int            `json:"completed"`
	Comparable          int            `json:"comparable"`
	Unknown             int            `json:"unknown"`
	Failed              int            `json:"failed"`
	Uncomparable        int            `json:"uncomparable"`
	CriticalErrorCount  int            `json:"critical_error_count"`
	ScoreCoverage       float64        `json:"score_coverage"`
	MeanScores          *Scores        `json:"mean_scores,omitempty"`
	MeanReferenceScores *Scores        `json:"mean_reference_scores,omitempty"`
	MeanDelta           *ScoreDelta    `json:"mean_delta,omitempty"`
	VerdictCounts       map[string]int `json:"verdict_counts,omitempty"`
	Status              string         `json:"status"`
}

type AnnotationProvenance struct {
	SelectionPath   string    `json:"selection_path"`
	SelectionSHA256 string    `json:"selection_sha256"`
	PoolPath        string    `json:"pool_path"`
	PoolIndexSHA256 string    `json:"pool_index_sha256"`
	SchemaVersion   string    `json:"schema_version"`
	ContractVersion string    `json:"contract_version"`
	PromptSHA256    string    `json:"prompt_sha256"`
	Model           string    `json:"model"`
	Commit          string    `json:"commit"`
	StartedAt       time.Time `json:"started_at"`
	FinishedAt      time.Time `json:"finished_at"`
}

type AnnotationReport struct {
	SchemaVersion   string                  `json:"schema_version"`
	ContractVersion string                  `json:"contract_version"`
	RunID           string                  `json:"run_id"`
	Mode            string                  `json:"mode"`
	Provenance      AnnotationProvenance    `json:"provenance"`
	Cases           []AnnotationCaseReport  `json:"cases"`
	Groups          []AnnotationGroupReport `json:"groups"`
}

// AnnotationOutputReservation occupies a fresh output directory before any
// model request. It makes the selection and run identity inspectable even if
// a later provider call fails.
type AnnotationOutputReservation struct {
	SchemaVersion   string    `json:"schema_version"`
	ContractVersion string    `json:"contract_version"`
	RunID           string    `json:"run_id"`
	Mode            string    `json:"mode"`
	SelectionPath   string    `json:"selection_path"`
	SelectionSHA256 string    `json:"selection_sha256"`
	PoolPath        string    `json:"pool_path"`
	PoolIndexSHA256 string    `json:"pool_index_sha256"`
	PromptSHA256    string    `json:"prompt_sha256"`
	Model           string    `json:"model,omitempty"`
	Commit          string    `json:"commit"`
	ReservedAt      time.Time `json:"reserved_at"`
}

type AnnotationRunConfig struct {
	SelectionPath string
	RunID         string
	Commit        string
	Model         string
	Judge         AnnotationClient
}

func (r AnnotationResult) criticalErrorCount() int {
	if len(r.CriticalErrors) > 0 || r.Identity.Status == "mismatch" {
		return 1
	}
	return 0
}

func (r AnnotationResult) toRecord(in AnnotationInput) AnnotationRecord {
	variant := in.Variant
	if variant == "" {
		variant = "reference"
	}
	return AnnotationRecord{
		AssetID:            in.Target.AssetID,
		ImageType:          in.ImageType,
		Variant:            variant,
		Role:               in.Target.Role,
		Criteria:           in.Criteria,
		Status:             r.Status,
		Scores:             r.Scores,
		Identity:           r.Identity,
		Strengths:          r.Strengths,
		Weaknesses:         r.Weaknesses,
		Uncertainties:      r.Uncertainties,
		Recommendations:    r.Recommendations,
		CriticalErrors:     r.CriticalErrors,
		CriticalErrorCount: r.criticalErrorCount(),
		ComparisonBasis:    r.ComparisonBasis,
	}
}

func (r AnnotationRecord) criticalErrorCount() int {
	if r.CriticalErrorCount > 0 {
		return 1
	}
	if len(r.CriticalErrors) > 0 || r.Identity.Status == "mismatch" {
		return 1
	}
	return 0
}

func (s AnnotationSelection) cleanPoolPath() string {
	path, _ := filepath.Abs(strings.TrimSpace(s.PoolPath))
	return filepath.Clean(path)
}

func (s AnnotationSelection) validateMode() error {
	switch s.Mode {
	case AnnotationModeReference, AnnotationModeComparison:
		return nil
	default:
		return fmt.Errorf("annotation selection mode %q is unsupported", s.Mode)
	}
}
