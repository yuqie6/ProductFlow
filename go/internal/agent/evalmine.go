package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yuqie6/productflow/internal/platform/config"
)

const (
	evalMineMaxDays     = 90
	evalMineDefaultDays = 7
	evalMineSampleCap   = 50
)

var evalRedactPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)sk-[A-Za-z0-9]{8,}`),
	regexp.MustCompile(`(?i)Bearer\s+\S+`),
	regexp.MustCompile(`(?i)(api[_-]?key|access_token|secret|password)[=:]\s*\S+`),
	regexp.MustCompile(`(?i)[?&](token|key|signature|access_token)=[^&\s]+`),
	regexp.MustCompile(`postgres(?:ql)?(?:\+psycopg2?)?://[^\s]+`),
}

// EvalRate is a ratio that stays unavailable when the denominator is zero.
type EvalRate struct {
	Available   bool    `json:"available"`
	Value       float64 `json:"value,omitempty"`
	Numerator   int     `json:"numerator"`
	Denominator int     `json:"denominator"`
}

// EvalMineReport is the L6 production aggregation for one time window.
type EvalMineReport struct {
	SchemaVersion          int            `json:"schema_version"`
	GeneratedAt            string         `json:"generated_at"`
	Days                   int            `json:"days"`
	Since                  string         `json:"since"`
	TurnCount              int            `json:"turn_count"`
	TurnStatus             map[string]int `json:"turn_status"`
	RequiresInputRate      EvalRate       `json:"requires_input_rate"`
	UnknownRate            EvalRate       `json:"unknown_rate"`
	FailedRate             EvalRate       `json:"failed_rate"`
	TerminalReason         map[string]int `json:"terminal_reason_code"`
	ProposalStatus         map[string]int `json:"proposal_status"`
	ProposalConfirmRate    EvalRate       `json:"proposal_confirm_rate"`
	RunRequestStatus       map[string]int `json:"run_request_status"`
	RunRequestConfirmRate  EvalRate       `json:"run_request_confirm_rate"`
	AgentEdits             int            `json:"agent_edits"`
	UserUndoAfterAgentEdit int            `json:"user_undo_after_agent_edit"`
	UndoWithin5MinRate     EvalRate       `json:"undo_within_5min_rate"`
	RecentFailedTurns      []EvalTurnHint `json:"recent_failed_turns"`
}

// EvalTurnHint is a redacted pointer to a recent failed or unknown Turn.
type EvalTurnHint struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// EvalTurnExport is a desensitized task skeleton written to agent-evals/inbox/.
type EvalTurnExport struct {
	SchemaVersion int            `json:"schema_version"`
	Origin        string         `json:"origin"`
	Utterance     string         `json:"utterance"`
	Terminal      string         `json:"terminal"`
	PageContext   map[string]any `json:"page_context"`
	ToolNames     []string       `json:"tool_names"`
	WorldHint     string         `json:"world_hint"`
}

// RedactEvalText strips provider keys, bearer tokens, URL tokens, and database URLs.
func RedactEvalText(text string) string {
	out := text
	for _, pattern := range evalRedactPatterns {
		out = pattern.ReplaceAllString(out, "[redacted]")
	}
	return out
}

func evalRate(numerator, denominator int) EvalRate {
	if denominator <= 0 {
		return EvalRate{Available: false, Numerator: numerator, Denominator: denominator}
	}
	return EvalRate{
		Available:   true,
		Value:       float64(numerator) / float64(denominator),
		Numerator:   numerator,
		Denominator: denominator,
	}
}

func clampEvalMineDays(days int) int {
	if days <= 0 {
		return evalMineDefaultDays
	}
	if days > evalMineMaxDays {
		return evalMineMaxDays
	}
	return days
}

// MineEvalWindow aggregates production Agent rows in [now-days, now]. Read-only.
func MineEvalWindow(ctx context.Context, pool *pgxpool.Pool, days int) (EvalMineReport, error) {
	days = clampEvalMineDays(days)
	since := time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour)
	report := EvalMineReport{
		SchemaVersion:    1,
		GeneratedAt:      time.Now().UTC().Format(time.RFC3339),
		Days:             days,
		Since:            since.Format(time.RFC3339),
		TurnStatus:       map[string]int{},
		TerminalReason:   map[string]int{},
		ProposalStatus:   map[string]int{},
		RunRequestStatus: map[string]int{},
	}
	if err := scanCounts(ctx, pool, `
		SELECT status, COUNT(*) FROM agent_turn_projections
		WHERE created_at >= $1 GROUP BY status
	`, since, report.TurnStatus); err != nil {
		return report, err
	}
	for _, count := range report.TurnStatus {
		report.TurnCount += count
	}
	report.RequiresInputRate = evalRate(report.TurnStatus["requires_input"], report.TurnCount)
	report.UnknownRate = evalRate(report.TurnStatus["unknown"], report.TurnCount)
	report.FailedRate = evalRate(report.TurnStatus["failed"], report.TurnCount)
	if err := scanCounts(ctx, pool, `
		SELECT COALESCE(terminal_reason_code, '(none)'), COUNT(*)
		FROM agent_turn_projections
		WHERE created_at >= $1 GROUP BY 1
	`, since, report.TerminalReason); err != nil {
		return report, err
	}
	if err := scanCounts(ctx, pool, `
		SELECT status, COUNT(*) FROM workflow_graph_proposals
		WHERE created_at >= $1 GROUP BY status
	`, since, report.ProposalStatus); err != nil {
		return report, err
	}
	report.ProposalConfirmRate = evalRate(
		report.ProposalStatus["confirmed"],
		report.ProposalStatus["confirmed"]+report.ProposalStatus["discarded"],
	)
	if err := scanCounts(ctx, pool, `
		SELECT status, COUNT(*) FROM agent_workflow_run_requests
		WHERE created_at >= $1 GROUP BY status
	`, since, report.RunRequestStatus); err != nil {
		return report, err
	}
	confirmedRuns := report.RunRequestStatus["confirmed"] + report.RunRequestStatus["succeeded"]
	rejectedRuns := report.RunRequestStatus["cancelled"]
	report.RunRequestConfirmRate = evalRate(confirmedRuns, confirmedRuns+rejectedRuns)
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM workflow_operation_groups
		WHERE actor_type = 'agent' AND history_kind = 'edit' AND created_at >= $1
	`, since).Scan(&report.AgentEdits); err != nil {
		return report, err
	}
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(DISTINCT agent_edit.id)
		FROM workflow_operation_groups agent_edit
		JOIN workflow_operation_groups user_undo
		  ON user_undo.graph_id = agent_edit.graph_id
		 AND user_undo.actor_type = 'user'
		 AND user_undo.history_kind = 'undo'
		 AND user_undo.created_at >= agent_edit.created_at
		 AND user_undo.created_at < agent_edit.created_at + INTERVAL '5 minutes'
		WHERE agent_edit.actor_type = 'agent'
		  AND agent_edit.history_kind = 'edit'
		  AND agent_edit.created_at >= $1
	`, since).Scan(&report.UserUndoAfterAgentEdit); err != nil {
		return report, err
	}
	report.UndoWithin5MinRate = evalRate(report.UserUndoAfterAgentEdit, report.AgentEdits)
	rows, err := pool.Query(ctx, `
		SELECT id, status FROM agent_turn_projections
		WHERE created_at >= $1 AND status IN ('failed', 'unknown')
		ORDER BY created_at DESC, id DESC
		LIMIT $2
	`, since, evalMineSampleCap)
	if err != nil {
		return report, err
	}
	defer rows.Close()
	for rows.Next() {
		var hint EvalTurnHint
		if err := rows.Scan(&hint.ID, &hint.Status); err != nil {
			return report, err
		}
		report.RecentFailedTurns = append(report.RecentFailedTurns, hint)
	}
	return report, rows.Err()
}

func scanCounts(ctx context.Context, pool *pgxpool.Pool, query string, since time.Time, dest map[string]int) error {
	rows, err := pool.Query(ctx, query, since)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		var count int
		if err := rows.Scan(&key, &count); err != nil {
			return err
		}
		dest[key] = count
	}
	return rows.Err()
}

// ExportEvalTurn writes a desensitized task skeleton for one Turn projection.
func ExportEvalTurn(ctx context.Context, pool *pgxpool.Pool, turnID string) (EvalTurnExport, error) {
	turnID = strings.TrimSpace(turnID)
	if turnID == "" {
		return EvalTurnExport{}, fmt.Errorf("turn id is required")
	}
	var (
		inputText  string
		status     string
		toolSteps  string
		snapshotID *string
		pageType   *string
		route      *string
		productID  *string
		workflowID *string
	)
	err := pool.QueryRow(ctx, `
		SELECT p.input_text, p.status, COALESCE(p.tool_steps_json::text, '[]'), p.page_context_snapshot_id,
		       s.page_type, s.route, s.product_id, s.workflow_id
		FROM agent_turn_projections p
		LEFT JOIN agent_page_context_snapshots s ON s.id = p.page_context_snapshot_id
		WHERE p.id = $1
	`, turnID).Scan(&inputText, &status, &toolSteps, &snapshotID, &pageType, &route, &productID, &workflowID)
	if err != nil {
		return EvalTurnExport{}, err
	}
	names := toolNamesFromStepsJSON([]byte(toolSteps))
	page := map[string]any{}
	if route != nil {
		page["route"] = *route
	}
	if pageType != nil {
		page["page_type"] = *pageType
	}
	hint := "name-only-empty-intake"
	if pageType != nil && *pageType == "global_agent" {
		hint = "global-library"
	}
	if workflowID != nil && *workflowID != "" {
		hint = "expanded-rev3"
	}
	_ = snapshotID
	_ = productID
	return EvalTurnExport{
		SchemaVersion: 1,
		Origin:        "production:" + turnID,
		Utterance:     RedactEvalText(inputText),
		Terminal:      status,
		PageContext:   page,
		ToolNames:     names,
		WorldHint:     hint,
	}, nil
}

func toolNamesFromStepsJSON(raw []byte) []string {
	if len(raw) == 0 {
		return []string{}
	}
	var steps []map[string]any
	if err := json.Unmarshal(raw, &steps); err != nil {
		return []string{}
	}
	names := []string{}
	seen := map[string]struct{}{}
	for _, step := range steps {
		name, _ := step["tool_name"].(string)
		name = strings.TrimSpace(name)
		if name == "" || name == "productflow_context_injection" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	return names
}

func evalStorageRoot() (string, error) {
	return config.ResolveStorageRoot(os.Getenv("STORAGE_ROOT"))
}

// WriteEvalMineReport writes the aggregation under STORAGE_ROOT/agent-evals/mine/.
func WriteEvalMineReport(report EvalMineReport) (string, error) {
	root, err := evalStorageRoot()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, "agent-evals", "mine")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := fmt.Sprintf("mine-%s.json", strings.ReplaceAll(report.GeneratedAt, ":", ""))
	path := filepath.Join(dir, name)
	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", err
	}
	return path, os.WriteFile(path, append(raw, '\n'), 0o644)
}

// WriteEvalTurnExport writes a desensitized skeleton under STORAGE_ROOT/agent-evals/inbox/.
func WriteEvalTurnExport(export EvalTurnExport) (string, error) {
	root, err := evalStorageRoot()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, "agent-evals", "inbox")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	id := strings.TrimPrefix(export.Origin, "production:")
	if id == "" {
		id = "turn"
	}
	path := filepath.Join(dir, "turn-"+id+".json")
	raw, err := json.MarshalIndent(export, "", "  ")
	if err != nil {
		return "", err
	}
	return path, os.WriteFile(path, append(raw, '\n'), 0o644)
}
