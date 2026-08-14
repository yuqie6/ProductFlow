from __future__ import annotations

import sys
from pathlib import Path

import dramatiq

from productflow_backend.application.agent_sync import (
    execute_agent_turn_sync,
    recover_unfinished_agent_turn_syncs,
)
from productflow_backend.application.delivery_renditions import execute_delivery_rendition_job
from productflow_backend.application.durable_recovery import (
    recover_unfinished_delivery_rendition_jobs,
    recover_unfinished_image_session_generation_tasks,
    recover_unfinished_workflow_runs,
)
from productflow_backend.application.image_sessions import execute_image_session_generation_task
from productflow_backend.application.product_workflows import (
    execute_product_workflow_node_run,
    execute_product_workflow_run,
)
from productflow_backend.application.runtime_settings import get_runtime_settings
from productflow_backend.domain.durable_generation_tasks import (
    DELIVERY_RENDITION_TASK_CONTRACT,
    IMAGE_SESSION_GENERATION_TASK_CONTRACT,
    WORKFLOW_RUN_GENERATION_TASK_CONTRACT,
    assert_actor_uses_durable_generation_contract,
)
from productflow_backend.infrastructure.image.chat_service import ImageChatService
from productflow_backend.infrastructure.logging import (
    cleanup_old_logs,
    configure_logging,
    reset_image_session_generation_task_id,
    reset_workflow_node_run_id,
    reset_workflow_run_id,
    set_image_session_generation_task_id,
    set_workflow_node_run_id,
    set_workflow_run_id,
)
from productflow_backend.infrastructure.queue import (
    enqueue_agent_turn_sync,
    enqueue_agent_turn_sync_later,
    enqueue_delivery_rendition_job,
    enqueue_image_session_generation_task,
    enqueue_workflow_run,
    get_broker,
)

configure_logging()
get_broker()


def get_image_session_worker_failsafe_time_limit_ms() -> int:
    return int(get_runtime_settings().image_session_worker_failsafe_time_limit_minutes) * 60 * 1000


def get_product_workflow_worker_failsafe_time_limit_ms() -> int:
    return get_image_session_worker_failsafe_time_limit_ms()


IMAGE_SESSION_WORKER_FAILSAFE_TIME_LIMIT_MS = get_image_session_worker_failsafe_time_limit_ms()
PRODUCT_WORKFLOW_WORKER_FAILSAFE_TIME_LIMIT_MS = get_product_workflow_worker_failsafe_time_limit_ms()


@dramatiq.actor(max_retries=0, time_limit=PRODUCT_WORKFLOW_WORKER_FAILSAFE_TIME_LIMIT_MS)
def run_product_workflow_run(workflow_run_id: str) -> None:
    """商品工作流 scheduler：发现 ready 节点并派发独立节点任务。"""
    token = set_workflow_run_id(workflow_run_id)
    try:
        execute_product_workflow_run(workflow_run_id)
    finally:
        reset_workflow_run_id(token)


@dramatiq.actor(max_retries=0, time_limit=PRODUCT_WORKFLOW_WORKER_FAILSAFE_TIME_LIMIT_MS)
def run_product_workflow_node_run(workflow_node_run_id: str) -> None:
    """商品工作流节点 worker：执行单个 WorkflowNodeRun，完成后唤醒 scheduler。"""
    token = set_workflow_node_run_id(workflow_node_run_id)
    try:
        execute_product_workflow_node_run(workflow_node_run_id)
    finally:
        reset_workflow_node_run_id(token)


@dramatiq.actor(max_retries=0, time_limit=IMAGE_SESSION_WORKER_FAILSAFE_TIME_LIMIT_MS)
def run_image_session_generation_task(task_id: str) -> None:
    """连续生图 worker：执行失败落库为通用安全错误。"""
    token = set_image_session_generation_task_id(task_id)
    try:
        execute_image_session_generation_task(task_id, chat_service_factory=ImageChatService)
    finally:
        reset_image_session_generation_task_id(token)


@dramatiq.actor(max_retries=0, time_limit=PRODUCT_WORKFLOW_WORKER_FAILSAFE_TIME_LIMIT_MS)
def run_agent_turn_sync(projection_id: str) -> None:
    execute_agent_turn_sync(
        projection_id,
        enqueue_later=lambda target_id, delay_ms: enqueue_agent_turn_sync_later(
            target_id,
            delay_ms=delay_ms,
        ),
    )


@dramatiq.actor(max_retries=0, time_limit=IMAGE_SESSION_WORKER_FAILSAFE_TIME_LIMIT_MS)
def run_delivery_rendition_job(job_id: str) -> None:
    execute_delivery_rendition_job(job_id)


assert_actor_uses_durable_generation_contract(WORKFLOW_RUN_GENERATION_TASK_CONTRACT, run_product_workflow_run)
assert_actor_uses_durable_generation_contract(
    IMAGE_SESSION_GENERATION_TASK_CONTRACT,
    run_image_session_generation_task,
)
assert_actor_uses_durable_generation_contract(
    DELIVERY_RENDITION_TASK_CONTRACT,
    run_delivery_rendition_job,
)


def _running_under_dramatiq_cli() -> bool:
    return any(Path(arg).name == "dramatiq" for arg in sys.argv)


if _running_under_dramatiq_cli():
    cleanup_old_logs()
    recover_unfinished_workflow_runs(enqueue=enqueue_workflow_run, reset_stale_running=True)
    recover_unfinished_image_session_generation_tasks(
        enqueue=enqueue_image_session_generation_task,
        reset_stale_running=True,
    )
    recover_unfinished_agent_turn_syncs(enqueue=enqueue_agent_turn_sync)
    recover_unfinished_delivery_rendition_jobs(
        enqueue=enqueue_delivery_rendition_job,
        reset_stale_running=True,
    )
