package queue

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/hibiken/asynq"
)

// ParseRedis 把 REDIS_URL 交给 asynq。asynq 不是 Run 状态源。
func ParseRedis(redisURL string) (asynq.RedisConnOpt, error) {
	if redisURL == "" {
		return nil, fmt.Errorf("REDIS_URL is required")
	}
	opt, err := asynq.ParseRedisURI(redisURL)
	if err != nil {
		return nil, err
	}
	return opt, nil
}

type TaskPayload struct {
	DispatchID  string `json:"dispatch_id"`
	AggregateID string `json:"aggregate_id"`
}

// NewTask 构造 HTTP 默认信封。MaxRetry=0，broker 重试不是业务状态机。
func NewTask(dispatchID, aggregateID string) (*asynq.Task, error) {
	body, err := json.Marshal(TaskPayload{DispatchID: dispatchID, AggregateID: aggregateID})
	if err != nil {
		return nil, err
	}
	return asynq.NewTask(TaskRunAsyncDispatch, body, asynq.MaxRetry(0), asynq.Timeout(30*time.Minute)), nil
}

func EnqueueWith(client *asynq.Client) EnqueueFunc {
	return func(dispatchID, aggregateID string) error {
		task, err := NewTask(dispatchID, aggregateID)
		if err != nil {
			return err
		}
		_, err = client.Enqueue(task)
		return err
	}
}
