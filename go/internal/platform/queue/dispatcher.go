package queue

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yuqie6/productflow/internal/platform/clockid"
	pfdb "github.com/yuqie6/productflow/internal/platform/db"
	"github.com/yuqie6/productflow/internal/platform/db/schema"
	"github.com/yuqie6/productflow/internal/platform/generation"
	"github.com/yuqie6/productflow/internal/platform/tx"
	"gorm.io/gorm"
)

// sendClaimedConcurrency 限制同一轮已 claim 行上 SENT+enqueue 的并发。
// 每条仍先 SENT 再 enqueue；不提高 DefaultClaimLimit。
const sendClaimedConcurrency = 16

// RunDispatcherOnce 对账过期 lease 与陈旧 SENT，再 SKIP LOCKED claim PENDING、标 SENT 并 enqueue。
// HTTP 不得调用本函数入队。
// pool 为 nil 或对账/claim 写库失败、ctx 取消时返回 error。
func RunDispatcherOnce(ctx context.Context, pool *pgxpool.Pool, enqueue EnqueueFunc, limit int) (Summary, error) {
	if limit < 1 {
		limit = DefaultClaimLimit
	}
	gdb, err := gormFrom(pool)
	if err != nil {
		return Summary{}, err
	}
	now := time.Now().UTC()
	var summary Summary
	err = tx.WithGorm(ctx, gdb, func(dbTx *gorm.DB) error {
		expired, err := reconcileExpiredLeases(ctx, dbTx, now)
		if err != nil {
			return err
		}
		stale, err := reconcileStaleSent(ctx, dbTx, now, time.Duration(DefaultSentReconcileAfter)*time.Second, DefaultMaxAttempts, DefaultStaleSentReconcileLimit)
		if err != nil {
			return err
		}
		summary.Reconciled = expired + stale
		return nil
	})
	if err != nil {
		return Summary{}, err
	}

	claimed, hasMore, err := claimPending(ctx, gdb, now, limit, DefaultLeaseSeconds)
	if err != nil {
		return Summary{}, err
	}
	summary.Pending = len(claimed)
	summary.HasMore = hasMore
	summary.Sent = sendAllClaimed(ctx, gdb, claimed, enqueue, now)
	var dead int64
	if err := gdb.WithContext(ctx).Model(&schema.AsyncDispatches{}).Where("status = ?", StatusDead).Count(&dead).Error; err != nil {
		return Summary{}, err
	}
	summary.Dead = int(dead)
	return summary, nil
}

func reconcileExpiredLeases(ctx context.Context, dbTx *gorm.DB, now time.Time) (int, error) {
	res := dbTx.WithContext(ctx).Model(&schema.AsyncDispatches{}).
		Where("status = ? AND lease_expires_at IS NOT NULL AND lease_expires_at <= ?", StatusPending, now).
		Updates(map[string]any{
			"lease_token":      nil,
			"lease_expires_at": nil,
			"available_at":     now,
			"updated_at":       now,
		})
	if res.Error != nil {
		return 0, res.Error
	}
	return int(res.RowsAffected), nil
}

// reconcileStaleSent 找回「标了 SENT 但没人消费」的信封。只处理 sent_at 早于 cutoff、且没有有效消费 lease 的行。
// attempts 已到上限标 DEAD，否则清 lease/sent_at 拉回 PENDING。必须先 FOR UPDATE，避免和 worker 抢同一行。
func reconcileStaleSent(ctx context.Context, dbTx *gorm.DB, now time.Time, sentAfter time.Duration, maxAttempts, limit int) (int, error) {
	if limit < 1 {
		limit = DefaultStaleSentReconcileLimit
	}
	cutoff := now.Add(-sentAfter)
	var items []schema.AsyncDispatches
	if err := dbTx.WithContext(ctx).Model(&schema.AsyncDispatches{}).Clauses(pfdb.ForUpdate()).
		Where("status = ? AND sent_at IS NOT NULL AND sent_at <= ?", StatusSent, cutoff).
		Where("lease_token IS NULL OR (lease_expires_at IS NOT NULL AND lease_expires_at <= ?)", now).
		Order("sent_at ASC, id ASC").
		Limit(limit).
		Find(&items).Error; err != nil {
		return 0, err
	}
	for _, item := range items {
		nextStatus := StatusPending
		avail := item.AvailableAt
		if item.Attempts >= maxAttempts {
			nextStatus = StatusDead
		} else if !avail.After(now) {
			avail = now
		}
		if err := dbTx.WithContext(ctx).Model(&schema.AsyncDispatches{}).Where("id = ?", item.ID).Updates(map[string]any{
			"status":           nextStatus,
			"lease_token":      nil,
			"lease_expires_at": nil,
			"sent_at":          nil,
			"available_at":     avail,
			"updated_at":       now,
		}).Error; err != nil {
			return 0, err
		}
	}
	return len(items), nil
}

// generationActorNames 是共享全局生成槽的两个 durable actor。
// 其它 actor 仍走普通 pending 顺序，不应被生成公平预算挡住。
var generationActorNames = []string{ActorGraphRun, ActorImageSession}

type generationMerchantStats struct {
	Reserved  int64
	ServiceAt time.Time
	Served    bool
}

type generationPendingHead struct {
	ID          string
	MerchantID  string
	AvailableAt time.Time
	CreatedAt   time.Time
}

// claimPending 在同一事务内拿 generation admission lock，再 claim PENDING。
// generation envelope 以 SENT/有效 dispatcher lease 作为预取 reservation，并按商家
// 的服务历史交错选择；普通 actor 保留原有 available_at/id 顺序。
func claimPending(ctx context.Context, gdb *gorm.DB, now time.Time, limit, leaseSeconds int) ([]Dispatch, bool, error) {
	if limit < 1 {
		return nil, false, nil
	}
	var claimed []Dispatch
	hasMore := false
	err := tx.WithGorm(ctx, gdb, func(dbTx *gorm.DB) error {
		if err := generation.LockAdmission(ctx, dbTx); err != nil {
			return err
		}
		maxConcurrent, err := generation.LoadMaxConcurrent(ctx, dbTx)
		if err != nil {
			return err
		}
		running, err := generation.CountAdmissionRunning(ctx, dbTx)
		if err != nil {
			return err
		}
		stats, reserved, err := loadGenerationMerchantStats(ctx, dbTx, now)
		if err != nil {
			return err
		}
		generationBudget := maxConcurrent - max(running, reserved)
		if generationBudget < 0 {
			generationBudget = 0
		}
		availableGenerationBudget := generationBudget
		if generationBudget > limit {
			generationBudget = limit
		}

		blockedMerchants := map[string]bool{}
		generationClaims := 0
		generationClaimLimit := generationBudget
		if generationBudget == limit && generationBudget > 0 {
			heads, err := loadGenerationPendingHeads(ctx, dbTx, now)
			if err != nil {
				return err
			}
			generationHead, ok := chooseGenerationHead(heads, stats, blockedMerchants)
			if ok {
				ordinaryHeads, err := loadPendingRows(ctx, dbTx, now, 1)
				if err != nil {
					return err
				}
				if len(ordinaryHeads) > 0 && ordinaryHeadBeforeGeneration(ordinaryHeads[0], generationHead, stats) {
					// Reserve one slot for the older ordinary head whenever
					// generation could otherwise fill the complete batch.
					generationClaimLimit = limit - 1
				}
			}
		}
		for len(claimed) < generationClaimLimit {
			heads, err := loadGenerationPendingHeads(ctx, dbTx, now)
			if err != nil {
				return err
			}
			head, ok := chooseGenerationHead(heads, stats, blockedMerchants)
			if !ok {
				break
			}
			row, ok, err := lockGenerationPendingHead(ctx, dbTx, head.MerchantID, now)
			if err != nil {
				return err
			}
			if !ok {
				blockedMerchants[head.MerchantID] = true
				continue
			}
			dispatch, err := claimDispatchRow(ctx, dbTx, row, now, leaseSeconds)
			if err != nil {
				return err
			}
			claimed = append(claimed, dispatch)
			generationClaims++
			reserved++
			stat := stats[head.MerchantID]
			stat.Reserved++
			stat.ServiceAt = now
			stat.Served = true
			stats[head.MerchantID] = stat
		}

		remaining := limit - len(claimed)
		if remaining > 0 {
			rows, err := loadPendingRows(ctx, dbTx, now, remaining)
			if err != nil {
				return err
			}
			for _, row := range rows {
				dispatch, err := claimDispatchRow(ctx, dbTx, row, now, leaseSeconds)
				if err != nil {
					return err
				}
				claimed = append(claimed, dispatch)
			}
		}

		if len(claimed) == limit {
			if pending, err := hasPendingRows(ctx, dbTx, now, false); err != nil {
				return err
			} else {
				hasMore = pending
			}
			if !hasMore && availableGenerationBudget > generationClaims {
				pending, err := hasPendingRows(ctx, dbTx, now, true)
				if err != nil {
					return err
				}
				hasMore = pending
			}
		}
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return claimed, hasMore, nil
}

func loadGenerationMerchantStats(ctx context.Context, dbTx *gorm.DB, now time.Time) (map[string]generationMerchantStats, int, error) {
	type statRow struct {
		MerchantID string     `gorm:"column:merchant_id"`
		Reserved   int64      `gorm:"column:reserved_count"`
		ServiceAt  *time.Time `gorm:"column:service_at"`
	}
	var rows []statRow
	if err := dbTx.WithContext(ctx).Model(&schema.AsyncDispatches{}).
		Select(`COALESCE(merchant_id, '') AS merchant_id,
			COUNT(*) FILTER (WHERE status = ? OR
				(status = ? AND lease_token IS NOT NULL AND lease_expires_at IS NOT NULL AND lease_expires_at > ?)) AS reserved_count,
			MAX(CASE WHEN attempts > 0 THEN updated_at END) AS service_at`,
			StatusSent, StatusPending, now).
		Where("actor_name IN ?", generationActorNames).
		Group("COALESCE(merchant_id, '')").Scan(&rows).Error; err != nil {
		return nil, 0, err
	}
	stats := make(map[string]generationMerchantStats, len(rows))
	reserved := 0
	for _, row := range rows {
		stat := generationMerchantStats{Reserved: row.Reserved}
		reserved += int(row.Reserved)
		if row.ServiceAt != nil {
			stat.ServiceAt = row.ServiceAt.UTC()
			stat.Served = true
		}
		stats[row.MerchantID] = stat
	}
	return stats, reserved, nil
}

func loadGenerationPendingHeads(ctx context.Context, dbTx *gorm.DB, now time.Time) ([]generationPendingHead, error) {
	var heads []generationPendingHead
	err := dbTx.WithContext(ctx).Model(&schema.AsyncDispatches{}).
		Select("DISTINCT ON (COALESCE(merchant_id, '')) id, COALESCE(merchant_id, '') AS merchant_id, available_at, created_at").
		Where("actor_name IN ? AND status = ? AND available_at <= ? AND (lease_expires_at IS NULL OR lease_expires_at <= ?)", generationActorNames, StatusPending, now, now).
		Order("COALESCE(merchant_id, ''), available_at ASC, id ASC").
		Scan(&heads).Error
	return heads, err
}

func chooseGenerationHead(heads []generationPendingHead, stats map[string]generationMerchantStats, blocked map[string]bool) (generationPendingHead, bool) {
	var best generationPendingHead
	found := false
	for _, head := range heads {
		if blocked[head.MerchantID] {
			continue
		}
		if !found || generationHeadBefore(head, best, stats) {
			best = head
			found = true
		}
	}
	return best, found
}

func generationHeadBefore(left, right generationPendingHead, stats map[string]generationMerchantStats) bool {
	leftStat := stats[left.MerchantID]
	rightStat := stats[right.MerchantID]
	if leftStat.Reserved != rightStat.Reserved {
		return leftStat.Reserved < rightStat.Reserved
	}
	if leftStat.Served != rightStat.Served {
		return !leftStat.Served
	}
	if leftStat.Served && !leftStat.ServiceAt.Equal(rightStat.ServiceAt) {
		return leftStat.ServiceAt.Before(rightStat.ServiceAt)
	}
	if left.CreatedAt != right.CreatedAt {
		return left.CreatedAt.Before(right.CreatedAt)
	}
	if left.AvailableAt != right.AvailableAt {
		return left.AvailableAt.Before(right.AvailableAt)
	}
	if left.MerchantID != right.MerchantID {
		return left.MerchantID < right.MerchantID
	}
	return left.ID < right.ID
}

func ordinaryHeadBeforeGeneration(ordinary schema.AsyncDispatches, generation generationPendingHead, stats map[string]generationMerchantStats) bool {
	generationReadyAt := generation.AvailableAt
	if serviceAt := stats[generation.MerchantID].ServiceAt; serviceAt.After(generationReadyAt) {
		generationReadyAt = serviceAt
	}
	return ordinary.AvailableAt.Before(generationReadyAt)
}

func lockGenerationPendingHead(ctx context.Context, dbTx *gorm.DB, merchantID string, now time.Time) (schema.AsyncDispatches, bool, error) {
	var row schema.AsyncDispatches
	err := dbTx.WithContext(ctx).Model(&schema.AsyncDispatches{}).Clauses(pfdb.SkipLocked()).
		Where("COALESCE(merchant_id, '') = ? AND actor_name IN ? AND status = ? AND available_at <= ? AND (lease_expires_at IS NULL OR lease_expires_at <= ?)", merchantID, generationActorNames, StatusPending, now, now).
		Order("available_at ASC, id ASC").Limit(1).Find(&row).Error
	if err != nil {
		return schema.AsyncDispatches{}, false, err
	}
	return row, row.ID != "", nil
}

func loadPendingRows(ctx context.Context, dbTx *gorm.DB, now time.Time, limit int) ([]schema.AsyncDispatches, error) {
	if limit < 1 {
		return nil, nil
	}
	query := dbTx.WithContext(ctx).Model(&schema.AsyncDispatches{}).Clauses(pfdb.SkipLocked()).
		Where("status = ? AND available_at <= ? AND (lease_expires_at IS NULL OR lease_expires_at <= ?)", StatusPending, now, now).
		Where("actor_name NOT IN ?", generationActorNames)
	var rows []schema.AsyncDispatches
	if err := query.Order("available_at ASC, id ASC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func claimDispatchRow(ctx context.Context, dbTx *gorm.DB, row schema.AsyncDispatches, now time.Time, leaseSeconds int) (Dispatch, error) {
	token := clockid.New()
	leaseUntil := now.Add(time.Duration(leaseSeconds) * time.Second)
	if err := dbTx.WithContext(ctx).Model(&schema.AsyncDispatches{}).Where("id = ?", row.ID).Updates(map[string]any{
		"lease_token":      token,
		"lease_expires_at": leaseUntil,
		"attempts":         row.Attempts + 1,
		"updated_at":       now,
	}).Error; err != nil {
		return Dispatch{}, err
	}
	d := fromSchema(row)
	d.LeaseToken = &token
	d.LeaseExpiresAt = &leaseUntil
	d.Attempts = row.Attempts + 1
	d.UpdatedAt = now
	return d, nil
}

func hasPendingRows(ctx context.Context, dbTx *gorm.DB, now time.Time, generationOnly bool) (bool, error) {
	query := dbTx.WithContext(ctx).Model(&schema.AsyncDispatches{}).
		Where("status = ? AND available_at <= ? AND (lease_expires_at IS NULL OR lease_expires_at <= ?)", StatusPending, now, now)
	if generationOnly {
		query = query.Where("actor_name IN ?", generationActorNames)
	} else {
		query = query.Where("actor_name NOT IN ?", generationActorNames)
	}
	var row schema.AsyncDispatches
	err := query.Select("id").Limit(1).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return err == nil, err
}

func sendAllClaimed(ctx context.Context, gdb *gorm.DB, claimed []Dispatch, enqueue EnqueueFunc, now time.Time) int {
	if len(claimed) == 0 {
		return 0
	}
	n := sendClaimedConcurrency
	if n > len(claimed) {
		n = len(claimed)
	}
	var sent atomic.Int64
	var wg sync.WaitGroup
	sem := make(chan struct{}, n)
	for _, dispatch := range claimed {
		dispatch := dispatch
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			if sendClaimed(ctx, gdb, dispatch, enqueue, now) {
				sent.Add(1)
			}
		}()
	}
	wg.Wait()
	return int(sent.Load())
}

// sendClaimed 先把行标 SENT 再 enqueue。broker 失败只写 last_error，行保持 SENT 等对账，不回滚成 PENDING。
// 否则会出现「库里 PENDING、broker 里已有任务」的双投。enqueue==nil 只改库，给单测用。
func sendClaimed(ctx context.Context, gdb *gorm.DB, dispatch Dispatch, enqueue EnqueueFunc, now time.Time) bool {
	// 先标 SENT 再 enqueue；broker 失败只写 last_error，行保持 SENT 等对账，不回滚状态。
	err := tx.WithGorm(ctx, gdb, func(dbTx *gorm.DB) error {
		return dbTx.WithContext(ctx).Model(&schema.AsyncDispatches{}).Where("id = ?", dispatch.ID).Updates(map[string]any{
			"status":           StatusSent,
			"sent_at":          now,
			"lease_token":      nil,
			"lease_expires_at": nil,
			"last_error":       nil,
			"updated_at":       now,
		}).Error
	})
	if err != nil {
		return false
	}
	if enqueue == nil {
		return true
	}
	if err := enqueue(dispatch.ID, dispatch.AggregateID); err != nil {
		msg := err.Error()
		if len(msg) > 1000 {
			msg = msg[:1000]
		}
		_ = gdb.WithContext(ctx).Model(&schema.AsyncDispatches{}).
			Where("id = ? AND status = ? AND lease_token IS NULL", dispatch.ID, StatusSent).
			Updates(map[string]any{"last_error": msg, "updated_at": now}).Error
		return false
	}
	return true
}
