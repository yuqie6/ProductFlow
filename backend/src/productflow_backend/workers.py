"""Dramatiq 组合根：只 claim/投递并调用 application。time_limit 是进程 kill switch，不是业务超时。"""

from __future__ import annotations

import logging
import sys
from datetime import timedelta
from pathlib import Path
from threading import Event, Thread

import dramatiq
from sqlalchemy import update

from productflow_backend.application.agent.sync import execute_agent_turn_sync
from productflow_backend.application.async_delivery import (
    DEFAULT_DISPATCH_CONSUMER_LEASE_SECONDS,
    DEFAULT_DISPATCH_MAX_ATTEMPTS,
    claim_async_dispatch_for_consumption,
    mark_async_dispatch_consumed,
    mark_async_dispatch_failed,
    stage_async_dispatch_for_actor,
)
from productflow_backend.application.delivery_renditions import execute_delivery_rendition_job
from productflow_backend.application.image_sessions.service import execute_image_session_generation_task
from productflow_backend.application.local_image_edits.service import execute_local_image_edit_task
from productflow_backend.application.product_workflow.graph_execution import execute_graph_run
from productflow_backend.application.runtime_settings import get_runtime_settings
from productflow_backend.application.time import now_utc
from productflow_backend.domain.durable_generation_tasks import (
    DELIVERY_RENDITION_TASK_CONTRACT,
    GRAPH_RUN_GENERATION_TASK_CONTRACT,
    IMAGE_SESSION_GENERATION_TASK_CONTRACT,
    LOCAL_IMAGE_EDIT_TASK_CONTRACT,
    assert_actor_uses_durable_generation_contract,
)
from productflow_backend.domain.enums import AsyncDispatchStatus
from productflow_backend.infrastructure.db.models import AsyncDispatch
from productflow_backend.infrastructure.db.session import get_session_factory
from productflow_backend.infrastructure.image.chat_service import ImageChatService
from productflow_backend.infrastructure.image.factory import get_image_provider
from productflow_backend.infrastructure.logging import (
    cleanup_old_logs,
    configure_logging,
    reset_image_session_generation_task_id,
    reset_workflow_run_id,
    set_image_session_generation_task_id,
    set_workflow_run_id,
)
from productflow_backend.infrastructure.queue import (
    GRAPH_RUN_ACTOR_NAME,
    get_broker,
)
from productflow_backend.infrastructure.storage import LocalStorage

configure_logging()
get_broker()
logger = logging.getLogger(__name__)


def get_image_session_worker_failsafe_time_limit_ms() -> int:
    """Dramatiq 进程 kill switch，不是业务超时或 provider timeout。"""
    return int(get_runtime_settings().image_session_worker_failsafe_time_limit_minutes) * 60 * 1000


def get_product_workflow_worker_failsafe_time_limit_ms() -> int:
    return get_image_session_worker_failsafe_time_limit_ms()


IMAGE_SESSION_WORKER_FAILSAFE_TIME_LIMIT_MS = get_image_session_worker_failsafe_time_limit_ms()
PRODUCT_WORKFLOW_WORKER_FAILSAFE_TIME_LIMIT_MS = get_product_workflow_worker_failsafe_time_limit_ms()


def _execute_async_dispatch_target(actor_name: str, aggregate_id: str) -> None:
    if actor_name == GRAPH_RUN_ACTOR_NAME:
        execute_graph_run(aggregate_id)
    elif actor_name == IMAGE_SESSION_GENERATION_TASK_CONTRACT.actor_name:
        execute_image_session_generation_task(aggregate_id, chat_service_factory=ImageChatService)
    elif actor_name == "run_agent_turn_sync":
        execute_agent_turn_sync(
            aggregate_id,
            enqueue_later=lambda worker_session, target_id, delay_ms: stage_async_dispatch_for_actor(
                worker_session,
                "run_agent_turn_sync",
                target_id,
                delay_ms=delay_ms,
            ),
        )
    elif actor_name == DELIVERY_RENDITION_TASK_CONTRACT.actor_name:
        execute_delivery_rendition_job(aggregate_id)
    elif actor_name == LOCAL_IMAGE_EDIT_TASK_CONTRACT.actor_name:
        execute_local_image_edit_task(
            session_factory=get_session_factory(),
            task_id=aggregate_id,
            provider=get_image_provider(),
            storage=LocalStorage(),
        )
    else:
        raise RuntimeError(f"unknown async dispatch actor: {actor_name}")


def _renew_async_dispatch_lease(
    *,
    dispatch_id: str,
    aggregate_id: str,
    lease_token: str,
    stop: Event,
    lease_seconds: int = DEFAULT_DISPATCH_CONSUMER_LEASE_SECONDS,
) -> None:
    interval_seconds = max(1.0, lease_seconds / 3)
    while not stop.wait(interval_seconds):
        session = get_session_factory()()
        try:
            now = now_utc()
            result = session.execute(
                update(AsyncDispatch)
                .where(
                    AsyncDispatch.id == dispatch_id,
                    AsyncDispatch.aggregate_id == aggregate_id,
                    AsyncDispatch.status == AsyncDispatchStatus.SENT,
                    AsyncDispatch.lease_token == lease_token,
                )
                .values(
                    lease_expires_at=now + timedelta(seconds=lease_seconds),
                    updated_at=now,
                )
                .execution_options(synchronize_session=False)
            )
            session.commit()
            if result.rowcount != 1:
                return
        except Exception:  # noqa: BLE001
            session.rollback()
        finally:
            session.close()


def execute_async_dispatch(dispatch_id: str, aggregate_id: str) -> None:
    """消费 SENT dispatch：claim 后跑 target，成功才 CONSUMED。SENT 本身不表示业务完成。"""
    session = get_session_factory()()
    lease_token: str | None = None
    try:
        dispatch = session.get(AsyncDispatch, dispatch_id)
        if (
            dispatch is None
            or dispatch.aggregate_id != aggregate_id
            or dispatch.status != AsyncDispatchStatus.SENT
        ):
            return
        actor_name = dispatch.actor_name
        lease_token = claim_async_dispatch_for_consumption(
            session,
            dispatch_id=dispatch_id,
            aggregate_id=aggregate_id,
        )
        if lease_token is None:
            # 另一 worker 已持有 SENT 消费 lease，或行已不是 SENT。
            return
        stop_heartbeat = Event()
        heartbeat = Thread(
            target=_renew_async_dispatch_lease,
            kwargs={
                "dispatch_id": dispatch_id,
                "aggregate_id": aggregate_id,
                "lease_token": lease_token,
                "stop": stop_heartbeat,
            },
            name=f"async-dispatch-lease-{dispatch_id}",
            daemon=True,
        )
        heartbeat.start()
        try:
            _execute_async_dispatch_target(actor_name, aggregate_id)
        finally:
            stop_heartbeat.set()
            heartbeat.join(timeout=2)
        mark_async_dispatch_consumed(
            session,
            dispatch_id=dispatch_id,
            aggregate_id=aggregate_id,
            lease_token=lease_token,
        )
        session.commit()
    except Exception:
        session.rollback()
        if lease_token is not None:
            try:
                mark_async_dispatch_failed(
                    session,
                    dispatch_id=dispatch_id,
                    aggregate_id=aggregate_id,
                    lease_token=lease_token,
                    error="async target execution failed",
                    max_attempts=DEFAULT_DISPATCH_MAX_ATTEMPTS,
                )
                session.commit()
            except Exception:  # noqa: BLE001
                session.rollback()
                logger.exception("异步目标失败后无法落库 retry 状态: dispatch_id=%s", dispatch_id)
        raise
    finally:
        session.close()


@dramatiq.actor(max_retries=0, time_limit=PRODUCT_WORKFLOW_WORKER_FAILSAFE_TIME_LIMIT_MS)
def run_workflow_graph_run(graph_run_id: str) -> None:
    """schema-v3 graph worker。time_limit 是进程 kill switch；业务超时在 PostgreSQL 行上。"""
    token = set_workflow_run_id(graph_run_id)
    try:
        execute_graph_run(graph_run_id)
    finally:
        reset_workflow_run_id(token)


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
    """Turn 投影同步 worker。max_retries=0；未完成状态靠 PostgreSQL 恢复扫描补投递。"""
    execute_agent_turn_sync(
        projection_id,
        enqueue_later=lambda worker_session, target_id, delay_ms: stage_async_dispatch_for_actor(
            worker_session,
            "run_agent_turn_sync",
            target_id,
            delay_ms=delay_ms,
        ),
    )


@dramatiq.actor(max_retries=0, time_limit=IMAGE_SESSION_WORKER_FAILSAFE_TIME_LIMIT_MS)
def run_delivery_rendition_job(job_id: str) -> None:
    execute_delivery_rendition_job(job_id)


@dramatiq.actor(max_retries=0, time_limit=PRODUCT_WORKFLOW_WORKER_FAILSAFE_TIME_LIMIT_MS)
def run_local_image_edit_task(task_id: str) -> None:
    execute_local_image_edit_task(
        session_factory=get_session_factory(),
        task_id=task_id,
        provider=get_image_provider(),
        storage=LocalStorage(),
    )


@dramatiq.actor(max_retries=0, time_limit=PRODUCT_WORKFLOW_WORKER_FAILSAFE_TIME_LIMIT_MS)
def run_async_dispatch(dispatch_id: str, aggregate_id: str) -> None:
    """统一投递入口。broker 消息只是 attempt，claim 失败则安静退出。"""
    execute_async_dispatch(dispatch_id, aggregate_id)


assert_actor_uses_durable_generation_contract(GRAPH_RUN_GENERATION_TASK_CONTRACT, run_workflow_graph_run)
assert_actor_uses_durable_generation_contract(
    IMAGE_SESSION_GENERATION_TASK_CONTRACT,
    run_image_session_generation_task,
)
assert_actor_uses_durable_generation_contract(
    DELIVERY_RENDITION_TASK_CONTRACT,
    run_delivery_rendition_job,
)
assert_actor_uses_durable_generation_contract(
    LOCAL_IMAGE_EDIT_TASK_CONTRACT,
    run_local_image_edit_task,
)


def _running_under_dramatiq_cli() -> bool:
    return any(Path(arg).name == "dramatiq" for arg in sys.argv)


if _running_under_dramatiq_cli():
    cleanup_old_logs()
