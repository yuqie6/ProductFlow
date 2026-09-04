// Package metrics 在配置了 METRICS_BEARER_TOKEN 时暴露 GET /metrics（Prometheus 文本）。
// token 为空不注册路由，避免把内部计数裸奔到公网。
package metrics

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/generation"
	"github.com/yuqie6/productflow/internal/platform/notify"
	"gorm.io/gorm"
)

// AgentSSEConnections 是当前 Agent SSE 连接数（gauge，仅本进程）。
// 连接建立 +1、断开 -1。不要拿它当跨实例总数，也不要和连续生图 SSE 混计。
var AgentSSEConnections atomic.Int64

// GraphSSEConnections 是当前 GraphRun SSE 连接数（gauge，仅本进程）。
var GraphSSEConnections atomic.Int64

// AgentEventSequenceConflicts 累计 Turn journal 序号冲突次数。
var AgentEventSequenceConflicts atomic.Int64

// AgentJournalCompactTurns 累计已压缩的终态 Turn journal 数。
var AgentJournalCompactTurns atomic.Int64

// AgentJournalCompactErrors 累计 compact 作业失败次数。
var AgentJournalCompactErrors atomic.Int64

// AgentRecoveryUnknownExecutions 累计恢复时被标 unknown 的过期 execution。
var AgentRecoveryUnknownExecutions atomic.Int64

// AgentEventBatchCount 累计追加的 Agent 事件批次。
var AgentEventBatchCount atomic.Int64

// AgentEventBatchLastMS 上一批事件写入耗时，单位毫秒。
var AgentEventBatchLastMS atomic.Int64

// ObserveAgentEventBatch 记一批：计数 +1，并把耗时写入 AgentEventBatchLastMS。负 duration 当 0。
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

type recoveryBacklogCount struct {
	Domain string `gorm:"column:domain"`
	Count  int64  `gorm:"column:count"`
}

var recoveryBacklogDomains = []string{"agent", "graph", "image_session", "delivery", "local_image_edit"}

// Register 仅在 token 非空时挂 GET /metrics。Authorization 必须是 Bearer 且恒定时间比较；
// 失败只回 401/500 状态码，不写 {"detail"}，避免探测。
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

// snapshot 扫 PostgreSQL 状态计数并拼 Prometheus 文本。任一查询失败整页失败，不返回半截指标。
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
	var recoveryBacklog []recoveryBacklogCount
	if err := db.Raw(`
		SELECT domain, COUNT(*) AS count
		FROM (
			SELECT 'agent' AS domain
			FROM agent_tasks t
			WHERE t.status = 'queued' AND t.current_turn_id IS NULL AND t.conversation_id IS NOT NULL
			UNION ALL
			SELECT 'agent' AS domain
			FROM agent_turn_projections p
			WHERE p.resume_required = FALSE
			  AND (p.status IN ('queued', 'running', 'cancel_requested') OR (p.status = 'requires_input' AND p.question_answer_json IS NOT NULL))
			  AND NOT EXISTS (
				SELECT 1 FROM async_dispatches d
				WHERE d.delivery_key = 'run_agent_turn_sync:' || p.id AND d.status IN ('pending', 'sent', 'dead')
			  )
			UNION ALL
			SELECT 'graph' AS domain
			FROM (
				SELECT DISTINCT r.graph_id
				FROM workflow_graph_runs r
				WHERE r.status = 'queued'
				  AND NOT EXISTS (
					SELECT 1 FROM workflow_graph_runs active
					WHERE active.graph_id = r.graph_id AND active.status = 'running'
				  )
			) graph_candidates
			UNION ALL
			SELECT 'image_session' AS domain
			FROM image_session_generation_tasks t
			WHERE t.is_retryable = TRUE AND t.status = 'queued'
			  AND NOT EXISTS (
				SELECT 1 FROM async_dispatches d
				WHERE d.delivery_key = 'run_image_session_generation_task:' || t.id AND d.status IN ('pending', 'sent', 'dead')
			  )
			UNION ALL
			SELECT 'delivery' AS domain
			FROM delivery_rendition_jobs j
			WHERE j.is_retryable = TRUE AND j.status = 'queued'
			  AND NOT EXISTS (
				SELECT 1 FROM async_dispatches d
				WHERE d.delivery_key = 'run_delivery_rendition_job:' || j.id AND d.status IN ('pending', 'sent', 'dead')
			  )
			UNION ALL
			SELECT 'local_image_edit' AS domain
			FROM local_image_edit_tasks t
			WHERE t.is_retryable = TRUE AND t.status = 'queued'
			  AND NOT EXISTS (
				SELECT 1 FROM async_dispatches d
				WHERE d.delivery_key = 'run_local_image_edit_task:' || t.id AND d.status IN ('pending', 'sent', 'dead')
			  )
		) candidates
		GROUP BY domain
	`).Scan(&recoveryBacklog).Error; err != nil {
		return "", err
	}
	var postgresLockWaiters int64
	if err := db.Raw(`
		SELECT COUNT(*)
		FROM pg_stat_activity
		WHERE wait_event_type = 'Lock' AND state <> 'idle'
	`).Scan(&postgresLockWaiters).Error; err != nil {
		return "", err
	}
	var running runningCounts
	if err := db.Raw(`
		SELECT
			(SELECT COUNT(*) FROM workflow_graph_runs WHERE status = 'running') AS graph,
			(SELECT COUNT(*) FROM image_session_generation_tasks WHERE status = 'running') AS image_session,
			(SELECT COUNT(*) FROM delivery_rendition_jobs WHERE status = 'running') AS delivery,
			(SELECT COUNT(*) FROM local_image_edit_tasks WHERE status = 'running') AS local_image_edit,
			(SELECT COUNT(*) FROM agent_turn_executions WHERE phase <> 'terminal') AS agent
	`).Scan(&running).Error; err != nil {
		return "", err
	}
	var generationLimitSetting schema.AppSettings
	if err := db.Where("key = ?", generation.MaxConcurrentSettingKey).Take(&generationLimitSetting).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return "", err
	}
	generationLimit := generation.ParseMaxConcurrent(generationLimitSetting.Value)
	var staleRunning []recoveryBacklogCount
	if err := db.Raw(`
		WITH image_threshold AS (
			SELECT COALESCE(
				(SELECT CASE WHEN value ~ '^[1-9][0-9]*$' THEN value::integer END
				 FROM app_settings WHERE key = 'image_session_stale_running_after_minutes'),
				90
			) AS image_minutes
		), candidates AS (
			SELECT 'graph' AS domain, r.id
			FROM workflow_graph_runs r
			WHERE r.status = 'running'
			  AND (r.execution_lease_expires_at IS NULL OR r.execution_lease_expires_at <= NOW())
			  AND EXISTS (
				SELECT 1 FROM workflow_graph_node_runs n
				WHERE n.graph_run_id = r.id AND n.status = 'running'
				  AND COALESCE(n.progress_updated_at, n.started_at) <= NOW() - INTERVAL '30 minutes'
			  )
			UNION ALL
			SELECT 'image_session' AS domain, t.id
			FROM image_session_generation_tasks t
			CROSS JOIN image_threshold
			WHERE t.is_retryable = TRUE AND t.status = 'running'
			  AND COALESCE(t.progress_updated_at, t.started_at) <= NOW() - image_threshold.image_minutes * INTERVAL '1 minute'
			UNION ALL
			SELECT 'delivery' AS domain, j.id
			FROM delivery_rendition_jobs j
			WHERE j.is_retryable = TRUE AND j.status = 'running'
			  AND j.started_at IS NOT NULL AND j.started_at <= NOW() - INTERVAL '30 minutes'
			UNION ALL
			SELECT 'local_image_edit' AS domain, t.id
			FROM local_image_edit_tasks t
			WHERE t.is_retryable = TRUE AND t.status = 'running'
			  AND (t.started_at IS NULL OR t.started_at <= NOW() - INTERVAL '10 minutes')
		)
		SELECT domain, COUNT(*) AS count
		FROM candidates
		GROUP BY domain
	`).Scan(&staleRunning).Error; err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("# HELP productflow_agent_sse_connections Current Agent SSE connections.\n")
	b.WriteString("# TYPE productflow_agent_sse_connections gauge\n")
	fmt.Fprintf(&b, "productflow_agent_sse_connections %d\n", AgentSSEConnections.Load())
	b.WriteString("# HELP productflow_graph_sse_connections Current GraphRun SSE connections.\n")
	b.WriteString("# TYPE productflow_graph_sse_connections gauge\n")
	fmt.Fprintf(&b, "productflow_graph_sse_connections %d\n", GraphSSEConnections.Load())
	b.WriteString("# HELP productflow_notify_listener_connections Current PostgreSQL LISTEN connections held by this process.\n")
	b.WriteString("# TYPE productflow_notify_listener_connections gauge\n")
	fmt.Fprintf(&b, "productflow_notify_listener_connections %d\n", notify.ListenerConnections.Load())
	writeStatusCounts(&b, "productflow_agent_turns", "Agent Turns by durable status.", turns)
	writeStatusCounts(&b, "productflow_graph_runs", "Workflow runs by durable status.", runs)
	// PENDING→SENT enqueue 只发生在 dispatcher。worker 与 API 共用本 snapshot 的
	// productflow_async_dispatches 状态计数，不另造进程内 enqueue counter。
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
	b.WriteString("# HELP productflow_postgres_lock_waiters PostgreSQL sessions currently waiting on a lock.\n")
	b.WriteString("# TYPE productflow_postgres_lock_waiters gauge\n")
	fmt.Fprintf(&b, "productflow_postgres_lock_waiters %d\n", postgresLockWaiters)
	writeRunningGauges(&b, running)
	b.WriteString("# HELP productflow_generation_max_concurrent_tasks Admission limit from app_settings generation_max_concurrent_tasks.\n")
	b.WriteString("# TYPE productflow_generation_max_concurrent_tasks gauge\n")
	fmt.Fprintf(&b, "productflow_generation_max_concurrent_tasks %d\n", generationLimit)
	writeRecoveryBacklog(&b, recoveryBacklog)
	writeRecoveryStaleRunning(&b, pending.ExpiredLeases, staleRunning)
	writeRecoveryHistograms(&b)
	writeWorkerProcessSeries(&b)
	return b.String(), nil
}

type runningCounts struct {
	Graph          int64 `gorm:"column:graph"`
	ImageSession   int64 `gorm:"column:image_session"`
	Delivery       int64 `gorm:"column:delivery"`
	LocalImageEdit int64 `gorm:"column:local_image_edit"`
	Agent          int64 `gorm:"column:agent"`
}

func writeRunningGauges(b *strings.Builder, running runningCounts) {
	counts := map[string]int64{
		"agent":            running.Agent,
		"delivery":         running.Delivery,
		"graph":            running.Graph,
		"image_session":    running.ImageSession,
		"local_image_edit": running.LocalImageEdit,
	}
	b.WriteString("# HELP productflow_running Durable running work by domain.\n")
	b.WriteString("# TYPE productflow_running gauge\n")
	for _, domain := range recoveryBacklogDomains {
		fmt.Fprintf(b, "productflow_running{domain=%q} %d\n", domain, counts[domain])
	}
}

func writeRecoveryBacklog(b *strings.Builder, rows []recoveryBacklogCount) {
	counts := make(map[string]int64, len(rows))
	for _, row := range rows {
		counts[row.Domain] = row.Count
	}
	b.WriteString("# HELP productflow_recovery_queued_backlog Durable queued recovery candidates without an active outbox.\n")
	b.WriteString("# TYPE productflow_recovery_queued_backlog gauge\n")
	for _, domain := range recoveryBacklogDomains {
		fmt.Fprintf(b, "productflow_recovery_queued_backlog{domain=%q} %d\n", domain, counts[domain])
	}
}

func writeRecoveryStaleRunning(b *strings.Builder, agentExpired int64, rows []recoveryBacklogCount) {
	counts := make(map[string]int64, len(rows))
	counts["agent"] = agentExpired
	for _, row := range rows {
		counts[row.Domain] = row.Count
	}
	b.WriteString("# HELP productflow_recovery_stale_running Durable running recovery candidates past their domain threshold.\n")
	b.WriteString("# TYPE productflow_recovery_stale_running gauge\n")
	for _, domain := range recoveryBacklogDomains {
		fmt.Fprintf(b, "productflow_recovery_stale_running{domain=%q} %d\n", domain, counts[domain])
	}
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
