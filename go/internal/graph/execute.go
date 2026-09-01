package graph

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"strings"
	"sync"
	"time"

	sqldb "database/sql"

	"github.com/yuqie6/productflow/internal/platform/apperr"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/queue"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

const (
	graphRunAdvisoryLockNamespace = 847261
)

// graphRunLocks 是进程内互斥：同一 runID 只允许一个 ExecuteRun 持锁，另一条返回 queue.ErrBusy。
var graphRunLocks sync.Map

// Executor 是 GraphRun worker。本包不得 import product；图片写入与交付排队走 [Dependencies]。
type Executor struct {
	DB   *gorm.DB     // 命令事务与 advisory lock 入口
	Deps Dependencies // Prompt/Image 为 nil 时用 Mock；Delivery 排队失败只记日志，不失败 cook
	Log  *zap.Logger  // nil 时用 Nop
	// AfterRunStatus 在 ExecuteRun 到达终态（成功、failed、unknown）后回调，供 Agent 同步 Task。
	// nil 跳过。失败被吞掉，不回滚已写入的 run 状态。
	AfterRunStatus func(ctx context.Context, tx *gorm.DB, runID string) error
	// Products 在 ExecuteRun 开头挂到 ctx，供 compile/cook 锁商品、读 facts 与绑定图。
	// nil 时需要守卫的路径返回 Internal。
	Products ProductGuard
}

// ExecuteRun 是 asynq worker 入口：同一 run 只允许一个 worker。
//
// 先抢进程内互斥，再抢 PostgreSQL advisory lock；任一把未拿到返回 queue.ErrBusy，让 asynq 稍后再投递。
// 找不到 run 视为已消费，返回 nil。无法证明的 provider 结果标 unknown，返回 nil，不把 run 标 failed、不自动重试。
// 已证明的节点失败会把 run 标 failed 后仍返回 nil，让 worker 消费任务。
// 不要在这里打 broker，也不要把 unknown 改成 failed。副作用见 executeLoop / claim / persist。
func (e Executor) ExecuteRun(ctx context.Context, runID string) error {
	ctx = WithProductGuard(ctx, e.Products)
	e.logger().Info("graph run", zap.String("workflow_run_id", runID))
	unlock, ok := tryProcessLock(runID)
	if !ok {
		return queue.ErrBusy
	}
	defer unlock()

	sqlDB, err := e.DB.DB()
	if err != nil {
		return err
	}
	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	acquired, err := tryAdvisoryLock(ctx, conn, runID)
	if err != nil {
		return err
	}
	if !acquired {
		return queue.ErrBusy
	}
	defer func() { _ = releaseAdvisoryLock(context.Background(), conn, runID) }()

	if err := e.executeLoop(ctx, runID); err != nil {
		if errors.Is(err, queue.ErrBusy) || errors.Is(err, queue.ErrLater) {
			return err
		}
		if isMissingGraphRun(err) {
			return nil
		}
		if isProviderUnknown(err) {
			e.notifyRunStatus(ctx, runID)
			return nil
		}
		if failErr := failGraphRun(ctx, e.DB, runID, "工作流运行失败"); failErr != nil {
			if isMissingGraphRun(failErr) {
				return nil
			}
			return failErr
		}
		e.notifyRunStatus(ctx, runID)
		return nil
	}
	e.notifyRunStatus(ctx, runID)
	return nil
}

func (e Executor) notifyRunStatus(ctx context.Context, runID string) {
	if e.AfterRunStatus == nil {
		return
	}
	_ = tx.WithGorm(ctx, e.DB, func(pgxTx *gorm.DB) error {
		return e.AfterRunStatus(ctx, pgxTx, runID)
	})
}

func (e Executor) logger() *zap.Logger {
	if e.Log != nil {
		return e.Log
	}
	return zap.NewNop()
}

// executeLoop 是单次 GraphRun 的调度循环：从 snapshot 找上游 ready 的 queued 节点，claim 后并发 cook。
// 只在 ExecuteRun 已持进程锁 + PG advisory lock 之后调用。循环里再 fail 被挡住的 queued、检查终态。
// 容量不足返回 errWaitingCapacity 后短睡再试；真正抢不到锁才把 queue.ErrBusy 抛给 asynq。
// unknown 不停止 claim（noteNodeOutcome 把它当成功）；已证明失败才 stopClaiming。
// 不要在这里打 broker，也不要把 missing run 当错误——当作已消费返回 nil。
func (e Executor) executeLoop(ctx context.Context, runID string) error {
	const capacityWait = 250 * time.Millisecond
	type nodeOutcome struct{ err error }
	outcomes := make(chan nodeOutcome, 64)
	inflight := 0
	stopClaiming := false
	defer func() {
		for inflight > 0 {
			<-outcomes
			inflight--
		}
	}()

	waitOne := func(block bool) error {
		if inflight == 0 {
			return nil
		}
		if block {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case item := <-outcomes:
				inflight--
				return noteNodeOutcome(item.err, &stopClaiming)
			}
		}
		timer := time.NewTimer(capacityWait)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case item := <-outcomes:
			inflight--
			return noteNodeOutcome(item.err, &stopClaiming)
		case <-timer.C:
			return nil
		}
	}

	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		var (
			stop bool
			run  graphRunRow
		)
		err := tx.WithGorm(ctx, e.DB, func(pgxTx *gorm.DB) error {
			loaded, err := loadGraphRunByID(ctx, pgxTx, runID)
			if isMissingGraphRun(err) {
				stop = true
				return nil
			}
			if err != nil {
				return err
			}
			run = loaded
			if run.Status != RunStatusRunning {
				stop = true
				return nil
			}
			applied, err := appliedGraphFromSnapshot(run.Snapshot)
			if err != nil {
				return err
			}
			if err := failBlockedQueuedNodes(ctx, pgxTx, runID, applied, run.NodeRuns); err != nil {
				return err
			}
			done, err := completeGraphRunIfNodesTerminal(ctx, pgxTx, runID)
			if err != nil {
				return err
			}
			if done {
				stop = true
				return nil
			}
			return nil
		})
		if err != nil || stop {
			return err
		}

		applied, err := appliedGraphFromSnapshot(run.Snapshot)
		if err != nil {
			return err
		}
		var ready []graphNodeRunRow
		for _, item := range run.NodeRuns {
			if item.Status == NodeRunQueued && processingUpstreamState(applied, run.NodeRuns, item) == "ready" {
				ready = append(ready, item)
			}
		}
		if (stopClaiming || len(ready) == 0) && inflight == 0 {
			return e.finishOrLater(ctx, runID)
		}

		claimed := 0
		waitingCapacity := false
		if !stopClaiming {
			for _, nodeRun := range ready {
				ok, attemptID, err := claimQueuedNodeRun(ctx, e.DB, nodeRun.ID)
				if errors.Is(err, errWaitingCapacity) {
					waitingCapacity = true
					continue
				}
				if err != nil {
					return err
				}
				if !ok {
					continue
				}
				claimed++
				inflight++
				go func(nodeRunID, attemptID string) {
					outcomes <- nodeOutcome{err: e.executeClaimedNode(ctx, runID, nodeRunID, attemptID)}
				}(nodeRun.ID, attemptID)
			}
		}
		if inflight > 0 {
			block := claimed > 0 || !waitingCapacity
			if err := waitOne(block); err != nil {
				return err
			}
			continue
		}
		if waitingCapacity {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(capacityWait):
			}
			continue
		}
		return queue.ErrBusy
	}
}

func noteNodeOutcome(err error, stopClaiming *bool) error {
	if err == nil || isProviderUnknown(err) {
		return nil
	}
	*stopClaiming = true
	return err
}

func isMissingGraphRun(err error) bool {
	return err != nil && (apperr.IsNotFound(err) || errors.Is(err, gorm.ErrRecordNotFound))
}

func (e Executor) loadRun(ctx context.Context, runID string) (graphRunRow, error) {
	var run graphRunRow
	err := tx.WithGorm(ctx, e.DB, func(pgxTx *gorm.DB) error {
		loaded, err := loadGraphRunByID(ctx, pgxTx, runID)
		if err != nil {
			return err
		}
		run = loaded
		return nil
	})
	return run, err
}

// failBlockedQueuedNodes 把上游已 failed/unknown/cancelled 的 queued 处理节点级联标 failed。
// 先锁 run，再改 node 并写 node.failed 事件；否则会与取消 / recovery 的 run -> node 形成 node -> run 环。
// 调用方须已在事务里；只改 status=queued 的行。不是 unknown：上游失败已证明。
// 循环直到没有新的 blocked，避免漏标间接下游。RowsAffected!=1 表示并发抢先，跳过即可。
func failBlockedQueuedNodes(ctx context.Context, tx *gorm.DB, runID string, graph AppliedGraph, nodeRuns []graphNodeRunRow) error {
	var run schema.WorkflowGraphRuns
	if err := tx.WithContext(ctx).Clauses(pfdb.ForUpdate()).Select("id", "status").Where("id = ?", runID).Take(&run).Error; err != nil {
		return err
	}
	if run.Status != RunStatusRunning {
		return nil
	}
	now := time.Now().UTC()
	for {
		progressed := false
		for i := range nodeRuns {
			item := nodeRuns[i]
			if item.Status != NodeRunQueued {
				continue
			}
			if processingUpstreamState(graph, nodeRuns, item) != "blocked" {
				continue
			}
			updates := terminalNodeRunUpdates(NodeRunFailed, now)
			updates["failure_reason"] = "上游处理节点未成功"
			result := tx.WithContext(ctx).Model(&schema.WorkflowGraphNodeRuns{}).
				Where("id = ? AND status = ?", item.ID, "queued").
				Updates(updates)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				continue
			}
			if err := appendGraphRunEventLocked(ctx, tx, item.GraphRunID, "node.failed", &item.ID, map[string]any{
				"status": NodeRunFailed, "node_id": item.NodeID, "reason": "上游处理节点未成功",
			}); err != nil {
				return err
			}
			nodeRuns[i].Status = NodeRunFailed
			progressed = true
		}
		if !progressed {
			return nil
		}
	}
}

// processingUpstreamState 只看处理节点入边：succeeded/skipped 视为就绪，queued/running 为 wait，
// failed/unknown/cancelled 为 blocked。非处理源（product_source / image_asset）不挡下游。
// 返回 ready / wait / blocked。改它会同时影响 claim 与 failBlockedQueuedNodes。
func processingUpstreamState(graph AppliedGraph, nodeRuns []graphNodeRunRow, nodeRun graphNodeRunRow) string {
	if nodeRun.NodeID == nil {
		return "ready"
	}
	byNode := map[string]graphNodeRunRow{}
	for _, item := range nodeRuns {
		if item.NodeID != nil {
			byNode[*item.NodeID] = item
		}
	}
	waiting := false
	for _, edge := range graph.Incoming(*nodeRun.NodeID) {
		source, err := graph.Node(edge.SourceNodeID)
		if err != nil {
			continue
		}
		if !IsProcessingNode(source.NodeType) {
			continue
		}
		upstream, ok := byNode[source.ID]
		if !ok {
			continue
		}
		if upstream.Status == NodeRunSucceeded || upstream.Status == NodeRunSkipped {
			continue
		}
		if upstream.Status == NodeRunQueued || upstream.Status == NodeRunRunning {
			waiting = true
			continue
		}
		return "blocked"
	}
	if waiting {
		return "wait"
	}
	return "ready"
}

// tryProcessLock 抢进程内互斥。未抢到时调用方应返回 queue.ErrBusy，而不是失败 run。
func tryProcessLock(runID string) (func(), bool) {
	mu := &sync.Mutex{}
	actual, _ := graphRunLocks.LoadOrStore(runID, mu)
	lock := actual.(*sync.Mutex)
	if !lock.TryLock() {
		return nil, false
	}
	return func() {
		lock.Unlock()
		graphRunLocks.CompareAndDelete(runID, lock)
	}, true
}

func graphRunAdvisoryKeys(runID string) (int32, int32) {
	hexID := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(runID)), "-", "")
	mod := uint64(1 << 31)
	if len(hexID) == 32 {
		var acc uint64
		for i := 0; i < 32; i += 2 {
			var b byte
			hi := hexNibble(hexID[i])
			lo := hexNibble(hexID[i+1])
			b = hi<<4 | lo
			acc = (acc*256 + uint64(b)) % mod
		}
		return graphRunAdvisoryLockNamespace, int32(acc)
	}
	sum := sha256.Sum256([]byte(runID))
	ident := binary.BigEndian.Uint64(sum[:8]) % mod
	return graphRunAdvisoryLockNamespace, int32(ident)
}

func hexNibble(ch byte) byte {
	switch {
	case ch >= '0' && ch <= '9':
		return ch - '0'
	case ch >= 'a' && ch <= 'f':
		return ch - 'a' + 10
	default:
		return 0
	}
}

func tryAdvisoryLock(ctx context.Context, conn *sqldb.Conn, runID string) (bool, error) {
	ns, key := graphRunAdvisoryKeys(runID)
	var acquired bool
	err := conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock($1, $2)`, ns, key).Scan(&acquired)
	return acquired, err
}

func releaseAdvisoryLock(ctx context.Context, conn *sqldb.Conn, runID string) error {
	ns, key := graphRunAdvisoryKeys(runID)
	_, err := conn.ExecContext(ctx, `SELECT pg_advisory_unlock($1, $2)`, ns, key)
	return err
}

// finishOrLater 在没有可 claim 节点且 inflight=0 时收口：再 fail blocked，再 completeGraphRunIfNodesTerminal。
// run 已消失或不再 running 返回 nil。不要在这里标 unknown——那是 failClaimedNode / failGraphRun 的职责。
func (e Executor) finishOrLater(ctx context.Context, runID string) error {
	return tx.WithGorm(ctx, e.DB, func(pgxTx *gorm.DB) error {
		run, err := loadGraphRunByID(ctx, pgxTx, runID)
		if isMissingGraphRun(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if run.Status != RunStatusRunning {
			return nil
		}
		applied, err := appliedGraphFromSnapshot(run.Snapshot)
		if err != nil {
			return err
		}
		if err := failBlockedQueuedNodes(ctx, pgxTx, runID, applied, run.NodeRuns); err != nil {
			return err
		}
		_, err = completeGraphRunIfNodesTerminal(ctx, pgxTx, runID)
		return err
	})
}

func loadProductIDForGraph(ctx context.Context, tx *gorm.DB, graphID string) (string, error) {
	var rec schema.WorkflowGraphs
	err := tx.WithContext(ctx).Select("product_id").Where("id = ?", graphID).Take(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", apperr.NotFound("商品工作流不存在")
	}
	if err != nil {
		return "", err
	}
	return rec.ProductID, nil
}
