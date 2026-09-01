package agent

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yuqie6/productflow/internal/platform/notify"
)

// subscribeAgentNotifications 保留 Agent SSE 的调用边界；实际 listener 由 notify fanout 统一复用。
func subscribeAgentNotifications(pool *pgxpool.Pool, channel string) (<-chan notify.Notification, func()) {
	return notify.Subscribe(pool, channel)
}
