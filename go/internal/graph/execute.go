package graph

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/yuqie6/productflow/internal/auth"
	"github.com/yuqie6/productflow/internal/platform/apperr"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/queue"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// graphRunLocks 是进程内互斥：同一 runID 只允许一个 ExecuteRun 持锁，另一条返回 queue.ErrBusy。
var graphRunLocks sync.Map

// Executor 是 GraphRun worker。本包不得 import product；图片写入与交付排队走 [Dependencies]。
type Executor struct {
	DB   *gorm.DB     // 命令事务与 GraphRun execution lease 入口
	Deps Dependencies // Prompt/Image 为 nil 时用 Mock；Delivery 排队失败只记日志，不失败 cook
	Log  *zap.Logger  // nil 时用 Nop
	// AfterRunStatus 在 ExecuteRun 到达终态（成功、failed、unknown）后回调，供 Agent 同步 Task。
	// nil 跳过。失败被吞掉，不回滚已写入的 run 状态。
	AfterRunStatus func(ctx context.Context, tx *gorm.DB, runID string) error
	// Products 显式传入执行事务，用于锁商品、读 facts 与绑定图。
	// nil 时需要守卫的路径返回 Internal。
	Products ProductGuard
}

// ExecuteRun 是 asynq worker 入口：同一 run 只允许一个 worker。
//
// 进程内互斥只做同进程重复提交的快速门禁；跨实例执行权由 GraphRun 行上的 token/expiry lease 决定。
// lease 丢失时取消本次执行并返回 queue.ErrBusy，旧 worker 不能再收口 run 或晋升产物。
// 找不到 run 视为已消费，返回 nil。无法证明的 provider 结果标 unknown，返回 nil，不把 run 标 failed、不自动重试。
// 已证明的节点失败会把 run 标 failed 后仍返回 nil，让 worker 消费任务。
// 不要在这里打 broker，也不要把 unknown 改成 failed。副作用见 executeLoop / claim / persist。
func (e Executor) ExecuteRun(ctx context.Context, runID string) error {
	e.logger().Info("graph run", zap.String("workflow_run_id", runID))
	unlock, ok := tryProcessLock(runID)
	if !ok {
		return queue.ErrBusy
	}
	defer unlock()
	if e.DB == nil {
		return errors.New("graph run executor requires database")
	}

	token := clockid.New()
	active, acquired, err := acquireGraphRunLease(ctx, e.DB, runID, token)
	if err != nil {
		return err
	}
	if !active {
		e.notifyRunStatus(ctx, runID)
		return nil
	}
	if !acquired {
		return queue.ErrBusy
	}

	leaseCtx, cancelLease := context.WithCancel(withGraphRunLease(ctx, token))
	leaseLost := make(chan struct{})
	var leaseLostOnce sync.Once
	markLeaseLost := func() {
		leaseLostOnce.Do(func() {
			close(leaseLost)
			cancelLease()
		})
	}
	leaseDone := maintainGraphRunLease(leaseCtx, e.DB, runID, token, markLeaseLost)
	defer func() {
		cancelLease()
		<-leaseDone
		releaseCtx, cancelRelease := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelRelease()
		_, _ = releaseGraphRunLease(releaseCtx, e.DB, runID, token)
	}()
	merchantID, err := merchantIDForGraphRun(ctx, e.DB, runID)
	if err != nil {
		return err
	}
	ctx = auth.WithMerchantID(ctx, merchantID)
	leaseCtx = auth.WithMerchantID(leaseCtx, merchantID)

	if err := e.executeLoop(leaseCtx, runID); err != nil {
		select {
		case <-leaseLost:
			return queue.ErrBusy
		default:
		}
		if errors.Is(err, errGraphRunLeaseLost) {
			return queue.ErrBusy
		}
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
		if failErr := failGraphRun(leaseCtx, e.Products, e.DB, runID, "工作流运行失败"); failErr != nil {
			if isMissingGraphRun(failErr) {
				return nil
			}
			if errors.Is(failErr, errGraphRunLeaseLost) {
				return queue.ErrBusy
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
// 只在 ExecuteRun 已持进程锁 + execution lease 之后调用。循环里再 fail 被挡住的 queued、检查终态。
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
			done, err := completeGraphRunIfNodesTerminal(ctx, e.Products, pgxTx, runID)
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
	if err := tx.WithContext(ctx).Clauses(pfdb.ForUpdate()).Select("id", "status", "execution_lease_token", "execution_lease_expires_at").Where("id = ?", runID).Take(&run).Error; err != nil {
		return err
	}
	if run.Status != RunStatusRunning {
		return nil
	}
	if err := graphRunLeaseSchemaOwned(ctx, run); err != nil {
		return err
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
		_, err = completeGraphRunIfNodesTerminal(ctx, e.Products, pgxTx, runID)
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
