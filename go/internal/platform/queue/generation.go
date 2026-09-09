package queue

import (
	"context"
	"time"

	"github.com/riverqueue/river/rivertype"
	"gorm.io/gorm"
)

var generationReadyStates = []rivertype.JobState{rivertype.JobStateAvailable, rivertype.JobStateRunning, rivertype.JobStateRetryable, rivertype.JobStateScheduled}

// readyGenerationMerchants excludes obsolete executions and business terminal
// records. A fetched River job is still waiting until its business claim succeeds.
func readyGenerationMerchants(ctx context.Context, tx *gorm.DB, now time.Time) *gorm.DB {
	return tx.WithContext(ctx).Table("river_job j").
		Select("j.args ->> 'merchant_id' AS merchant_id, MIN(j.created_at) AS ready_since").
		Where("j.kind = ? AND j.queue = ? AND j.state IN ? AND j.scheduled_at <= ?", RiverTaskKind, "generation", generationReadyStates, now).
		Where(`(j.args ->> 'actor' = ? AND EXISTS (
    SELECT 1 FROM image_session_generation_tasks t JOIN image_sessions s ON s.id=t.session_id
    WHERE t.id=j.args ->> 'aggregate_id' AND t.queue_execution_id=j.args ->> 'execution_id'
      AND s.merchant_id=j.args ->> 'merchant_id' AND t.status='queued')) OR
   (j.args ->> 'actor' = ? AND EXISTS (
    SELECT 1 FROM workflow_graph_runs r JOIN workflow_graphs g ON g.id=r.graph_id JOIN products p ON p.id=g.product_id
    WHERE r.id=j.args ->> 'aggregate_id' AND r.queue_execution_id=j.args ->> 'execution_id'
      AND p.merchant_id=j.args ->> 'merchant_id' AND r.status='running'
      AND EXISTS (SELECT 1 FROM workflow_graph_node_runs n WHERE n.graph_run_id=r.id AND n.status='queued')))`, ActorImageSession, ActorGraphRun).
		Group("j.args ->> 'merchant_id'")
}

// GenerationTurn compares actual occupied slots and the last business claim,
// never River delivery attempts. The caller holds generation.LockAdmission
// through this decision and its queued->running write. This is non-preemptive.
func GenerationTurn(ctx context.Context, tx *gorm.DB, merchantID string) (bool, error) {
	if _, queued := TaskArgsFromContext(ctx); !queued {
		return true, nil
	}
	sessions := tx.Table("image_session_generation_tasks t").
		Joins("JOIN image_sessions s ON s.id=t.session_id").
		Select("s.merchant_id, COUNT(*) FILTER (WHERE t.status='running') AS running, MAX(t.started_at) AS served_at").Group("s.merchant_id")
	graphs := tx.Table("workflow_graph_node_runs n").
		Joins("JOIN workflow_graph_runs r ON r.id=n.graph_run_id").
		Joins("JOIN workflow_graphs g ON g.id=r.graph_id").Joins("JOIN products p ON p.id=g.product_id").
		Select("p.merchant_id, COUNT(*) FILTER (WHERE n.status='running' AND r.status='running') AS running, MAX(CASE WHEN n.attempt_count > 0 THEN n.started_at END) AS served_at").Group("p.merchant_id")
	activity := tx.Table("(? UNION ALL ?) activity", sessions, graphs).
		Select("merchant_id, SUM(running) AS running, MAX(served_at) AS served_at").Group("merchant_id")
	var chosen struct{ MerchantID string }
	err := tx.WithContext(ctx).Table("(?) ready", readyGenerationMerchants(ctx, tx, time.Now().UTC())).
		Joins("LEFT JOIN (?) activity ON activity.merchant_id=ready.merchant_id", activity).
		Select("ready.merchant_id").Order("COALESCE(activity.running,0), activity.served_at ASC NULLS FIRST, ready.ready_since, ready.merchant_id").Limit(1).Scan(&chosen).Error
	if err != nil {
		return false, err
	}
	return chosen.MerchantID == "" || chosen.MerchantID == merchantID, nil
}

func OtherReadyGenerationMerchant(ctx context.Context, tx *gorm.DB, merchantID string, now time.Time) (bool, error) {
	var count int64
	err := tx.WithContext(ctx).Table("(?) ready", readyGenerationMerchants(ctx, tx, now)).Where("merchant_id <> ?", merchantID).Count(&count).Error
	return count > 0, err
}
