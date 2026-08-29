package queue

import (
	"context"
	"time"
)

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

// ActorFunc 按 actor_name 执行业务目标。成功后调用方才标 CONSUMED。
type ActorFunc func(ctx context.Context, aggregateID string) error
