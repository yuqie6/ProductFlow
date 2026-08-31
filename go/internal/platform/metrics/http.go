package metrics

import (
	"crypto/subtle"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"gorm.io/gorm"
)

var AgentSSEConnections atomic.Int64
var AgentEventSequenceConflicts atomic.Int64
var AgentJournalCompactTurns atomic.Int64
var AgentJournalCompactErrors atomic.Int64
var AgentRecoveryUnknownExecutions atomic.Int64
var AgentEventBatchCount atomic.Int64
var AgentEventBatchLastMS atomic.Int64

func ObserveAgentEventBatch(elapsed time.Duration) {
	ms := elapsed.Milliseconds()
	if ms < 0 {
		ms = 0
	}
	AgentEventBatchCount.Add(1)
	AgentEventBatchLastMS.Store(ms)
}

type statusCount struct {
	Status string `gorm:"column:status"`
	Count  int64  `gorm:"column:count"`
}

// Register keeps metrics private by default: no token means no route.
func Register(engine *gin.Engine, db *gorm.DB, token string) {
	token = strings.TrimSpace(token)
	if token == "" {
		return
	}
	engine.GET("/metrics", func(c *gin.Context) {
		authorization := c.GetHeader("Authorization")
		if !strings.HasPrefix(authorization, "Bearer ") {
			c.Status(http.StatusUnauthorized)
			return
		}
		provided := strings.TrimPrefix(authorization, "Bearer ")
		if len(provided) != len(token) || subtle.ConstantTimeCompare([]byte(provided), []byte(token)) != 1 {
			c.Status(http.StatusUnauthorized)
			return
		}
		body, err := snapshot(db.WithContext(c.Request.Context()))
		if err != nil {
			c.Status(http.StatusInternalServerError)
			return
		}
		c.Data(http.StatusOK, "text/plain; version=0.0.4; charset=utf-8", []byte(body))
	})
}

func snapshot(db *gorm.DB) (string, error) {
	var turns, runs, dispatches, invocations, reconciliations, executions []statusCount
	queries := []struct {
		model any
		out   *[]statusCount
	}{
		{&schema.AgentTurnProjections{}, &turns},
		{&schema.WorkflowGraphRuns{}, &runs},
		{&schema.AsyncDispatches{}, &dispatches},
		{&schema.AgentModelInvocations{}, &invocations},
	}
	if err := db.Model(&schema.AgentTurnEffectReconciliations{}).
		Select("reconciliation_state AS status, COUNT(*) AS count").Group("reconciliation_state").Scan(&reconciliations).Error; err != nil {
		return "", err
	}
	if err := db.Model(&schema.AgentTurnExecutions{}).
		Select("phase AS status, COUNT(*) AS count").Group("phase").Scan(&executions).Error; err != nil {
		return "", err
	}
	var tokenTotals struct {
		InputTokens  int64 `gorm:"column:input_tokens"`
		OutputTokens int64 `gorm:"column:output_tokens"`
		UsageMissing int64 `gorm:"column:usage_missing"`
	}
	if err := db.Model(&schema.AgentModelInvocations{}).Select(`
		COALESCE(SUM(input_tokens), 0) AS input_tokens,
		COALESCE(SUM(output_tokens), 0) AS output_tokens,
		COUNT(*) FILTER (WHERE usage_source = 'unavailable') AS usage_missing`).Scan(&tokenTotals).Error; err != nil {
		return "", err
	}
	for _, query := range queries {
		if err := db.Model(query.model).Select("status, COUNT(*) AS count").Group("status").Scan(query.out).Error; err != nil {
			return "", err
		}
	}
	var pending struct {
		WorkflowRequests int64 `gorm:"column:workflow_requests"`
		GraphProposals   int64 `gorm:"column:graph_proposals"`
		LibraryDrafts    int64 `gorm:"column:library_drafts"`
		AskUser          int64 `gorm:"column:ask_user"`
		ExpiredLeases    int64 `gorm:"column:expired_leases"`
	}
	if err := db.Raw(`
		SELECT
			(SELECT COUNT(*) FROM agent_workflow_run_requests WHERE status = 'awaiting_confirmation') AS workflow_requests,
			(SELECT COUNT(*) FROM workflow_graph_proposals WHERE status = 'pending') AS graph_proposals,
			(SELECT COUNT(*) FROM library_organization_drafts WHERE status = 'awaiting_confirmation') AS library_drafts,
			(SELECT COUNT(*) FROM agent_turn_projections WHERE status = 'requires_input') AS ask_user,
			(SELECT COUNT(*) FROM agent_turn_executions WHERE phase <> 'terminal' AND lease_expires_at < NOW()) AS expired_leases
	`).Scan(&pending).Error; err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("# HELP productflow_agent_sse_connections Current Agent SSE connections.\n")
	b.WriteString("# TYPE productflow_agent_sse_connections gauge\n")
	fmt.Fprintf(&b, "productflow_agent_sse_connections %d\n", AgentSSEConnections.Load())
	writeStatusCounts(&b, "productflow_agent_turns", "Agent Turns by durable status.", turns)
	writeStatusCounts(&b, "productflow_graph_runs", "Workflow runs by durable status.", runs)
	writeStatusCounts(&b, "productflow_async_dispatches", "Async dispatch records by durable status.", dispatches)
	writeStatusCounts(&b, "productflow_agent_model_invocations", "Model invocations by durable status.", invocations)
	writeStatusCounts(&b, "productflow_agent_effect_reconciliations", "Effect reconciliations by bounded state.", reconciliations)
	writeStatusCounts(&b, "productflow_agent_executions", "Agent executions by phase.", executions)
	b.WriteString("# TYPE productflow_agent_model_input_tokens_observed gauge\n")
	fmt.Fprintf(&b, "productflow_agent_model_input_tokens_observed %d\n", tokenTotals.InputTokens)
	b.WriteString("# TYPE productflow_agent_model_output_tokens_observed gauge\n")
	fmt.Fprintf(&b, "productflow_agent_model_output_tokens_observed %d\n", tokenTotals.OutputTokens)
	b.WriteString("# TYPE productflow_agent_model_usage_missing gauge\n")
	fmt.Fprintf(&b, "productflow_agent_model_usage_missing %d\n", tokenTotals.UsageMissing)
	b.WriteString("# HELP productflow_agent_event_sequence_conflicts Agent event sequence conflicts.\n")
	b.WriteString("# TYPE productflow_agent_event_sequence_conflicts counter\n")
	fmt.Fprintf(&b, "productflow_agent_event_sequence_conflicts %d\n", AgentEventSequenceConflicts.Load())
	b.WriteString("# HELP productflow_agent_journal_compact_turns Compacted terminal Turn journals.\n")
	b.WriteString("# TYPE productflow_agent_journal_compact_turns counter\n")
	fmt.Fprintf(&b, "productflow_agent_journal_compact_turns %d\n", AgentJournalCompactTurns.Load())
	b.WriteString("# HELP productflow_agent_journal_compact_errors Compact job errors.\n")
	b.WriteString("# TYPE productflow_agent_journal_compact_errors counter\n")
	fmt.Fprintf(&b, "productflow_agent_journal_compact_errors %d\n", AgentJournalCompactErrors.Load())
	b.WriteString("# HELP productflow_agent_recovery_unknown_executions Recovered expired executions marked unknown.\n")
	b.WriteString("# TYPE productflow_agent_recovery_unknown_executions counter\n")
	fmt.Fprintf(&b, "productflow_agent_recovery_unknown_executions %d\n", AgentRecoveryUnknownExecutions.Load())
	b.WriteString("# HELP productflow_agent_event_batches Appended Agent event batches.\n")
	b.WriteString("# TYPE productflow_agent_event_batches counter\n")
	fmt.Fprintf(&b, "productflow_agent_event_batches %d\n", AgentEventBatchCount.Load())
	b.WriteString("# HELP productflow_agent_event_batch_last_ms Last Agent event batch duration in milliseconds.\n")
	b.WriteString("# TYPE productflow_agent_event_batch_last_ms gauge\n")
	fmt.Fprintf(&b, "productflow_agent_event_batch_last_ms %d\n", AgentEventBatchLastMS.Load())
	b.WriteString("# HELP productflow_agent_pending_approvals Pending merchant approvals by kind.\n")
	b.WriteString("# TYPE productflow_agent_pending_approvals gauge\n")
	fmt.Fprintf(&b, "productflow_agent_pending_approvals{kind=%q} %d\n", "workflow_request", pending.WorkflowRequests)
	fmt.Fprintf(&b, "productflow_agent_pending_approvals{kind=%q} %d\n", "graph_proposal", pending.GraphProposals)
	fmt.Fprintf(&b, "productflow_agent_pending_approvals{kind=%q} %d\n", "library_draft", pending.LibraryDrafts)
	fmt.Fprintf(&b, "productflow_agent_pending_approvals{kind=%q} %d\n", "ask_user", pending.AskUser)
	b.WriteString("# HELP productflow_agent_expired_leases Active executions whose lease has expired.\n")
	b.WriteString("# TYPE productflow_agent_expired_leases gauge\n")
	fmt.Fprintf(&b, "productflow_agent_expired_leases %d\n", pending.ExpiredLeases)
	return b.String(), nil
}

func writeStatusCounts(b *strings.Builder, name, help string, rows []statusCount) {
	fmt.Fprintf(b, "# HELP %s %s\n# TYPE %s gauge\n", name, help, name)
	for _, row := range rows {
		if safeLabel(row.Status) {
			fmt.Fprintf(b, "%s{status=%q} %d\n", name, row.Status, row.Count)
		}
	}
}

func safeLabel(value string) bool {
	if value == "" || len(value) > 40 {
		return false
	}
	for _, ch := range value {
		if (ch < 'a' || ch > 'z') && ch != '_' {
			return false
		}
	}
	return true
}
