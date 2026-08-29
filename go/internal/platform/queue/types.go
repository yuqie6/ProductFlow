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
	DeliveryKey    string
	ActorName      string
	AggregateID    string
	PayloadJSON    []byte
	Status         string
	AvailableAt    time.Time
	LeaseToken     *string
	LeaseExpiresAt *time.Time
	Attempts       int
	LastError      *string
	SentAt         *time.Time
	ConsumedAt     *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type Summary struct {
	Pending    int `json:"pending"`
	Sent       int `json:"sent"`
	Reconciled int `json:"reconciled"`
	Dead       int `json:"dead"`
}

// EnqueueFunc 把已 SENT 的信封交给 broker。失败不得在本函数里改 PostgreSQL 行。
type EnqueueFunc func(dispatchID, aggregateID string) error

// ActorFunc 按 actor_name 执行业务目标。
// nil：业务结束，CONSUMED。
// ErrBusy / ErrLater：释放消费 lease，回到 PENDING，不 CONSUMED。
// 其他 error：MarkFailed。
type ActorFunc func(ctx context.Context, aggregateID string) error
