package queue

import "time"

// Actor 名对齐 contracts/queue.md。HTTP 默认只投递 run_async_dispatch 信封。
const (
	TaskRunAsyncDispatch = "run_async_dispatch"

	ActorGraphRun      = "run_workflow_graph_run"
	ActorImageSession  = "run_image_session_generation_task"
	ActorDelivery      = "run_delivery_rendition_job"
	ActorLocalEdit     = "run_local_image_edit_task"
	ActorAgentTurnSync = "run_agent_turn_sync"

	StatusPending  = "pending"
	StatusSent     = "sent"
	StatusConsumed = "consumed"
	StatusDead     = "dead"

	DefaultLeaseSeconds         = 60
	DefaultConsumerLeaseSeconds = int((TaskTimeout + 5*time.Minute) / time.Second)
	DefaultMaxAttempts          = 10
	DefaultBackoffSeconds       = 2
	DefaultSentReconcileAfter   = 5 * 60 // 秒
	DefaultClaimLimit           = 100
	DefaultBusyRetrySeconds     = 2
	DefaultLaterRetrySeconds    = 1
)

// DeliveryKey 是提交与恢复共用的稳定幂等键。
func DeliveryKey(actorName, aggregateID string) string {
	return actorName + ":" + aggregateID
}
