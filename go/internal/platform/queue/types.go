// Package queue 负责 async_dispatches：HTTP 不 enqueue broker，只写业务行与 PENDING；dispatcher 标 SENT 再入队；worker MaxRetry=0。
//
// 职责：把「已持久化的业务作业」交给 asynq。SENT 只表示信封已给 broker，不等于 GraphRun 成功。
// 调用时机：命令路径 Stage/StageForActor；dispatcher RunDispatcherOnce；worker Consume。
// 副作用：写 async_dispatches；Consume 成功标 CONSUMED。ErrBusy 不得标 CONSUMED（别人正在跑）。
// ErrLater 回到 PENDING，不进死信。改 actor 名要同时改 worker 路由，否则任务会丢。
package queue

import (
	"context"
	"errors"
	"time"
)

// ErrBusy 另一 worker 已持有同一聚合。Consume 不得标 CONSUMED。
var ErrBusy = errors.New("queue: aggregate already running")

// ErrLater 这次不能做（容量等），信封回到 PENDING，不记死信。
var ErrLater = errors.New("queue: retry later")

// Dispatch 是 async_dispatches 行。SENT 只表示已交给 broker，不等于业务完成。
type Dispatch struct {
	ID             string
	DeliveryKey    string // 幂等键，格式 actorName:aggregateID
	ActorName      string // worker 路由名；改名必须同步路由
	AggregateID    string
	MerchantID     string // 受理商家快照；Restage/重试保持不变，改写返回 Conflict
	PayloadJSON    []byte // 信封负载（业务字段；商家归属看 MerchantID）
	Status         string // pending | sent | consumed | dead
	AvailableAt    time.Time
	LeaseToken     *string // dispatcher/worker 围栏；错配不得 CONSUMED
	LeaseExpiresAt *time.Time
	Attempts       int     // 对账失败累计，到上限标 dead
	LastError      *string // 最近失败摘要
	SentAt         *time.Time
	ConsumedAt     *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// Summary 是 dispatcher 一轮的计数。Pending 是本轮 claim 数，Sent 是成功标 SENT 并入队的条数。
type Summary struct {
	Pending    int  `json:"pending"`    // 本轮 claim 数
	Sent       int  `json:"sent"`       // 成功标 SENT 并入队
	Reconciled int  `json:"reconciled"` // 本轮回收过期/陈旧 SENT
	Dead       int  `json:"dead"`       // 库中 dead 行数，不是本轮新增
	HasMore    bool `json:"has_more"`   // 本轮 claim 已满 limit，下一轮继续探测到期 PENDING
}

// EnqueueFunc 把已 SENT 的信封交给 broker。失败不得在本函数里改 PostgreSQL 行。
type EnqueueFunc func(dispatchID, aggregateID string) error

// ActorFunc 按 actor_name 执行业务目标。
// nil：业务结束，CONSUMED。
// ErrBusy / ErrLater：释放消费 lease，回到 PENDING，不 CONSUMED。
// 其他 error：MarkFailed。
type ActorFunc func(ctx context.Context, aggregateID string) error
