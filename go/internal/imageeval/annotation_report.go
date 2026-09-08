package imageeval

import (
	"fmt"
	"sort"
	"strings"
)

// RenderAnnotationMarkdown is intentionally a plain report: reviewers can see
// material provenance, criteria, evidence IDs, status gaps, and per-dimension
// comparison deltas without opening the JSON.
func RenderAnnotationMarkdown(report AnnotationReport) string {
	var out strings.Builder
	fmt.Fprintf(&out, "# Category Image Annotation\n\n")
	fmt.Fprintf(&out, "- Schema: `%s`\n", report.SchemaVersion)
	fmt.Fprintf(&out, "- Contract: `%s`\n", report.ContractVersion)
	fmt.Fprintf(&out, "- Run: `%s`\n", report.RunID)
	fmt.Fprintf(&out, "- Mode: `%s`\n", report.Mode)
	fmt.Fprintf(&out, "- Model: `%s`\n", markdownText(report.Provenance.Model))
	fmt.Fprintf(&out, "- Commit: `%s`\n", markdownText(report.Provenance.Commit))
	fmt.Fprintf(&out, "- Selection: `%s`\n", markdownText(report.Provenance.SelectionPath))
	fmt.Fprintf(&out, "- Selection SHA256: `%s`\n", report.Provenance.SelectionSHA256)
	fmt.Fprintf(&out, "- Pool index SHA256: `%s`\n", report.Provenance.PoolIndexSHA256)
	fmt.Fprintf(&out, "- Prompt SHA256: `%s`\n\n", report.Provenance.PromptSHA256)

	fmt.Fprintf(&out, "## Groups\n\n")
	fmt.Fprintf(&out, "| Category | Type | Variant | Eligible | Completed | Comparable | Unknown | Failed | Uncomparable | Critical | Coverage | Scores | Reference | Delta | Verdicts | Status |\n")
	fmt.Fprintf(&out, "| --- | --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- | --- | --- | --- | --- |\n")
	for _, group := range report.Groups {
		fmt.Fprintf(&out, "| %s | %s | %s | %d | %d | %d | %d | %d | %d | %d | %.2f | %s | %s | %s | %s | %s |\n",
			markdownText(group.Category), markdownText(group.ImageType), markdownText(group.Variant),
			group.Eligible, group.Completed, group.Comparable, group.Unknown, group.Failed,
			group.Uncomparable, group.CriticalErrorCount, group.ScoreCoverage,
			formatScores(group.MeanScores), formatScores(group.MeanReferenceScores), formatDelta(group.MeanDelta),
			formatVerdicts(group.VerdictCounts), group.Status)
	}
	if len(report.Groups) == 0 {
		out.WriteString("No annotation groups were produced.\n")
	}

	fmt.Fprintf(&out, "\n## Cases\n\n")
	for _, item := range report.Cases {
		fmt.Fprintf(&out, "### %s: %s [%s]\n\n", markdownText(item.Category), markdownText(item.CaseID), item.Status)
		if item.Title != "" {
			fmt.Fprintf(&out, "Title: %s\n\n", markdownText(item.Title))
		}
		fmt.Fprintf(&out, "Mode: `%s`; manifest SHA256: `%s`\n\n", item.Mode, item.ManifestSHA256)
		fmt.Fprintf(&out, "| Asset | Role | Type | Variant | SHA256 | Path |\n| --- | --- | --- | --- | --- | --- |\n")
		assets := flattenCaseAssets(item)
		for _, asset := range assets {
			fmt.Fprintf(&out, "| `%s` | %s | %s | %s | `%s` | `%s` |\n",
				markdownText(asset.AssetID), markdownText(asset.Role), markdownText(asset.ImageType),
				markdownText(firstNonEmptyString(asset.Variant, "reference")), asset.SHA256, markdownText(asset.Path))
		}
		out.WriteString("\n")
		for _, record := range item.Records {
			fmt.Fprintf(&out, "#### `%s` %s/%s [%s]\n\n", markdownText(record.AssetID), markdownText(record.ImageType), markdownText(record.Variant), record.Status)
			fmt.Fprintf(&out, "Criteria: %s\n\n", markdownText(record.Criteria))
			fmt.Fprintf(&out, "Scores: %s\n\n", formatScores(record.Scores))
			fmt.Fprintf(&out, "Critical errors: %d\n\n", record.CriticalErrorCount)
			if record.FailureCode != "" {
				fmt.Fprintf(&out, "Failure code: `%s`\n\n", markdownText(record.FailureCode))
			}
			if record.Identity.Status != "" {
				fmt.Fprintf(&out, "Identity: **%s**\n\n", markdownText(record.Identity.Status))
			}
			writeObservations(&out, "Strengths", record.Strengths)
			writeObservations(&out, "Weaknesses", record.Weaknesses)
			writeObservations(&out, "Critical errors", record.CriticalErrors)
			if len(record.Identity.Claims) > 0 {
				writeObservations(&out, "Identity claims", record.Identity.Claims)
			}
			if len(record.Uncertainties) > 0 {
				out.WriteString("Uncertainties:\n\n")
				for _, uncertainty := range record.Uncertainties {
					fmt.Fprintf(&out, "- %s (reason: %s; next check: %s; evidence: `%s`)\n", markdownText(uncertainty.Text), markdownText(uncertainty.Reason), markdownText(uncertainty.NextCheck), strings.Join(uncertainty.EvidenceAssetIDs, "`, `"))
				}
				out.WriteString("\n")
			}
			if len(record.Recommendations) > 0 {
				out.WriteString("Recommendations:\n\n")
				for _, recommendation := range record.Recommendations {
					fmt.Fprintf(&out, "- %s (rationale: %s; validation: %s; evidence: `%s`)\n", markdownText(recommendation.Text), markdownText(recommendation.Rationale), markdownText(recommendation.Validation), strings.Join(recommendation.EvidenceAssetIDs, "`, `"))
				}
				out.WriteString("\n")
			}
			if record.ComparisonBasis != nil {
				basis := record.ComparisonBasis
				requirements := make([]string, 0, len(basis.SharedRequirements))
				for _, requirement := range basis.SharedRequirements {
					requirements = append(requirements, markdownText(requirement))
				}
				fmt.Fprintf(&out, "Comparison basis: `%s`; target purpose: %s; reference purpose: %s; shared requirements: %s; reason: %s; evidence: %s\n\n",
					markdownText(basis.Status), markdownText(basis.TargetPurpose), markdownText(basis.ReferencePurpose),
					formatList(requirements), markdownText(basis.Reason), formatEvidence(basis.EvidenceAssetIDs))
			}
			if record.Comparison != nil {
				if record.Comparison.Status == AnnotationComparisonDifferentPurpose || record.Comparison.Status == AnnotationComparisonInsufficientEvidence {
					fmt.Fprintf(&out, "Comparison: `%s` against `%s`; no reference scores, delta, or verdict.\n\n",
						record.Comparison.Status, markdownText(record.Comparison.QualityReferenceAssetID))
				} else {
					fmt.Fprintf(&out, "Comparison: `%s` against `%s`; reference scores: %s; delta: %s; verdict: **%s**\n\n",
						record.Comparison.Status, markdownText(record.Comparison.QualityReferenceAssetID),
						formatScores(record.Comparison.QualityReferenceScores), formatDelta(record.Comparison.Delta), markdownText(record.Comparison.Verdict))
				}
			}
		}
	}
	return out.String()
}

func formatList(items []string) string {
	if len(items) == 0 {
		return "none"
	}
	return "`" + strings.Join(items, "`, `") + "`"
}

func flattenCaseAssets(item AnnotationCaseReport) []AnnotationAsset {
	assets := make([]AnnotationAsset, 0, len(item.IdentityReferences)+len(item.QualityReferences)+len(item.Candidates))
	assets = append(assets, item.IdentityReferences...)
	assets = append(assets, item.QualityReferences...)
	assets = append(assets, item.Candidates...)
	sort.SliceStable(assets, func(i, j int) bool { return assets[i].AssetID < assets[j].AssetID })
	return assets
}

func writeObservations(out *strings.Builder, title string, items []AnnotationObservation) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintf(out, "%s:\n\n", title)
	for _, item := range items {
		fmt.Fprintf(out, "- %s (severity: `%s`; certainty: `%s`; evidence: %s)\n", markdownText(item.Text), item.Severity, item.Certainty, formatEvidence(item.EvidenceAssetIDs))
	}
	out.WriteString("\n")
}

func formatEvidence(ids []string) string {
	if len(ids) == 0 {
		return "none"
	}
	return "`" + strings.Join(ids, "`, `") + "`"
}

func formatScores(scores *Scores) string {
	if scores == nil {
		return "-"
	}
	return fmt.Sprintf("F %.2f / Fit %.2f / U %.2f / A %.2f (mean %.2f)", scores.Fidelity, scores.Fit, scores.Utility, scores.Aesthetics, scores.Mean())
}

func formatDelta(delta *ScoreDelta) string {
	if delta == nil {
		return "-"
	}
	return fmt.Sprintf("F %+0.2f / Fit %+0.2f / U %+0.2f / A %+0.2f", delta.Fidelity, delta.Fit, delta.Utility, delta.Aesthetics)
}

func formatVerdicts(counts map[string]int) string {
	if len(counts) == 0 {
		return "-"
	}
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+fmt.Sprint(counts[key]))
	}
	return strings.Join(parts, ", ")
}

func markdownText(value string) string {
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.ReplaceAll(value, "|", "\\|")
	return strings.TrimSpace(value)
}
