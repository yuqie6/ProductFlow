from __future__ import annotations

import sys
from pathlib import Path

import dramatiq

from productflow_backend.application.image_sessions import execute_image_session_generation_task
from productflow_backend.application.launch_kit.generation import execute_launch_kit_generation_task
from productflow_backend.application.product_workflows import (
    execute_product_workflow_node_run,
    execute_product_workflow_run,
)
from productflow_backend.config import get_runtime_settings
from productflow_backend.domain.durable_generation_tasks import (
    IMAGE_SESSION_GENERATION_TASK_CONTRACT,
    WORKFLOW_RUN_GENERATION_TASK_CONTRACT,
    assert_actor_uses_durable_generation_contract,
)
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
    get_broker,
    recover_unfinished_image_session_generation_tasks,
    recover_unfinished_workflow_runs,
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
        execute_image_session_generation_task(task_id)
    finally:
        reset_image_session_generation_task_id(token)


@dramatiq.actor(max_retries=0, time_limit=IMAGE_SESSION_WORKER_FAILSAFE_TIME_LIMIT_MS)
def run_launch_kit_generation_task(task_id: str) -> None:
    """LaunchKit worker: deterministic v1 manual-export generator."""
    from productflow_backend.infrastructure.db.session import get_session_factory

    execute_launch_kit_generation_task(task_id, session_factory=get_session_factory())


assert_actor_uses_durable_generation_contract(WORKFLOW_RUN_GENERATION_TASK_CONTRACT, run_product_workflow_run)
assert_actor_uses_durable_generation_contract(
    IMAGE_SESSION_GENERATION_TASK_CONTRACT,
    run_image_session_generation_task,
)


def _running_under_dramatiq_cli() -> bool:
    return any(Path(arg).name == "dramatiq" for arg in sys.argv)


if _running_under_dramatiq_cli():
    cleanup_old_logs()
    recover_unfinished_workflow_runs(reset_stale_running=True)
    recover_unfinished_image_session_generation_tasks(reset_stale_running=True)
