package queue

// Actor names identify the five durable business task types.
const (
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
)
