package queue

import "time"

// Actor 名对齐 contracts/queue.md。HTTP 默认只投递 run_async_dispatch 信封。
const (
	// TaskRunAsyncDispatch 是 asynq 任务类型，唯一 HTTP 默认信封。
	TaskRunAsyncDispatch = "run_async_dispatch"

	// ActorGraphRun 执行 WorkflowGraphRun。
	ActorGraphRun = "run_workflow_graph_run"
	// ActorImageSession 执行图片会话生成任务。
	ActorImageSession = "run_image_session_generation_task"
	// ActorDelivery 执行交付转码作业。
	ActorDelivery = "run_delivery_rendition_job"
	// ActorLocalEdit 执行局部编辑任务。
	ActorLocalEdit = "run_local_image_edit_task"
	// ActorAgentTurnSync 把 Agent Turn 同步到业务投影。
	ActorAgentTurnSync = "run_agent_turn_sync"

	// StatusPending 表示尚未交给 broker。
	StatusPending = "pending"
	// StatusSent 表示已交给 broker，不等于业务完成。
	StatusSent = "sent"
	// StatusConsumed 表示 Actor 已成功结束。
	StatusConsumed = "consumed"
	// StatusDead 表示超过最大尝试次数，停止投递。
	StatusDead = "dead"

	// DefaultLeaseSeconds 是 dispatcher claim PENDING 时的短 lease（秒）。
	DefaultLeaseSeconds = 60
	// DefaultConsumerLeaseSeconds 是 worker 消费 lease，必须长于 [TaskTimeout]。
	DefaultConsumerLeaseSeconds = int((TaskTimeout + 5*time.Minute) / time.Second)
	// DefaultMaxAttempts 是 SENT 对账失败后标 dead 的尝试上限。
	DefaultMaxAttempts = 10
	// DefaultBackoffSeconds 是 MarkFailed 回到 PENDING 的退避。
	DefaultBackoffSeconds = 2
	// DefaultSentReconcileAfter 是 SENT 且无消费 lease 后开始对账的秒数。
	DefaultSentReconcileAfter = 5 * 60 // 秒
	// DefaultClaimLimit 是 dispatcher 每轮最多 claim 的 PENDING 条数。
	DefaultClaimLimit = 100
	// DefaultBusyRetrySeconds 是 [ErrBusy] 后重新 PENDING 的延迟。
	DefaultBusyRetrySeconds = 2
	// DefaultLaterRetrySeconds 是 [ErrLater] 后重新 PENDING 的延迟。
	DefaultLaterRetrySeconds = 1
)

// DeliveryKey 是提交与恢复共用的稳定幂等键，格式 actorName:aggregateID。
// 调用时机：Stage / Requeue / 恢复扫描。同一 Actor 的同一业务 id 必须复用，禁止每次 NewUUID。
// 不要把 GraphRun id 和 image_session task id 接到同一个 actor 名下。
func DeliveryKey(actorName, aggregateID string) string {
	return actorName + ":" + aggregateID
}
