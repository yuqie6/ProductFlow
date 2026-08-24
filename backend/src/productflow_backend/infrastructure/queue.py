"""Redis/Dramatiq 投递适配。enqueue 只是 delivery attempt，不改变 PostgreSQL 业务状态。"""

from __future__ import annotations

from functools import lru_cache

import dramatiq
from dramatiq.brokers.redis import RedisBroker
from dramatiq.message import Message

from productflow_backend.config import get_settings
from productflow_backend.domain.durable_generation_tasks import (
    DELIVERY_RENDITION_TASK_CONTRACT,
    IMAGE_SESSION_GENERATION_TASK_CONTRACT,
    LOCAL_IMAGE_EDIT_TASK_CONTRACT,
)

DEFAULT_DRAMATIQ_QUEUE_NAME = "default"
GRAPH_RUN_ACTOR_NAME = "run_workflow_graph_run"
AGENT_TURN_SYNC_ACTOR_NAME = "run_agent_turn_sync"


@lru_cache(maxsize=1)
def get_broker() -> RedisBroker:
    """初始化 Dramatiq Redis Broker（单例）。"""
    settings = get_settings()
    broker = RedisBroker(url=settings.redis_url)
    dramatiq.set_broker(broker)
    return broker


def _enqueue_actor(actor_name: str, task_id: str, *, delay_ms: int | None = None) -> None:
    _enqueue_actor_args(actor_name, (task_id,), delay_ms=delay_ms)


def _enqueue_actor_args(actor_name: str, args: tuple[str, ...], *, delay_ms: int | None = None) -> None:
    message = Message(
        queue_name=DEFAULT_DRAMATIQ_QUEUE_NAME,
        actor_name=actor_name,
        args=args,
        kwargs={},
        options={},
    )
    broker = get_broker()
    if delay_ms is None:
        broker.enqueue(message)
    else:
        broker.enqueue(message, delay=delay_ms)


def enqueue_graph_run(run_id: str) -> None:
    _enqueue_actor(GRAPH_RUN_ACTOR_NAME, run_id)


def enqueue_image_session_generation_task(task_id: str) -> None:
    _enqueue_actor(IMAGE_SESSION_GENERATION_TASK_CONTRACT.actor_name, task_id)


def enqueue_image_session_generation_task_later(task_id: str, *, delay_ms: int) -> None:
    _enqueue_actor(IMAGE_SESSION_GENERATION_TASK_CONTRACT.actor_name, task_id, delay_ms=delay_ms)


def enqueue_agent_turn_sync(projection_id: str) -> None:
    """投递 Turn 同步 attempt。投影状态以 PostgreSQL 为准。"""
    _enqueue_actor(AGENT_TURN_SYNC_ACTOR_NAME, projection_id)


def enqueue_agent_turn_sync_later(projection_id: str, *, delay_ms: int) -> None:
    _enqueue_actor(AGENT_TURN_SYNC_ACTOR_NAME, projection_id, delay_ms=delay_ms)


def enqueue_delivery_rendition_job(job_id: str) -> None:
    _enqueue_actor(DELIVERY_RENDITION_TASK_CONTRACT.actor_name, job_id)


def enqueue_local_image_edit_task(task_id: str) -> None:
    _enqueue_actor(LOCAL_IMAGE_EDIT_TASK_CONTRACT.actor_name, task_id)


ASYNC_DISPATCH_ACTOR_NAME = "run_async_dispatch"


def enqueue_async_dispatch(dispatch_id: str, aggregate_id: str) -> None:
    """投递已 SENT 的 dispatch。失败不改 PostgreSQL 行，由 stale SENT 对账决定是否重试。"""
    _enqueue_actor_args(ASYNC_DISPATCH_ACTOR_NAME, (dispatch_id, aggregate_id))
