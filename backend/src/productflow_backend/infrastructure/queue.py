from __future__ import annotations

from functools import lru_cache

import dramatiq
from dramatiq.brokers.redis import RedisBroker
from dramatiq.message import Message

from productflow_backend.config import get_settings
from productflow_backend.domain.durable_generation_tasks import (
    IMAGE_SESSION_GENERATION_TASK_CONTRACT,
    WORKFLOW_RUN_GENERATION_TASK_CONTRACT,
)

DEFAULT_DRAMATIQ_QUEUE_NAME = "default"
WORKFLOW_NODE_RUN_ACTOR_NAME = "run_product_workflow_node_run"
AGENT_TURN_SYNC_ACTOR_NAME = "run_agent_turn_sync"


@lru_cache(maxsize=1)
def get_broker() -> RedisBroker:
    """初始化 Dramatiq Redis Broker（单例）。"""
    settings = get_settings()
    broker = RedisBroker(url=settings.redis_url)
    dramatiq.set_broker(broker)
    return broker


def _enqueue_actor(actor_name: str, task_id: str, *, delay_ms: int | None = None) -> None:
    message = Message(
        queue_name=DEFAULT_DRAMATIQ_QUEUE_NAME,
        actor_name=actor_name,
        args=(task_id,),
        kwargs={},
        options={},
    )
    broker = get_broker()
    if delay_ms is None:
        broker.enqueue(message)
    else:
        broker.enqueue(message, delay=delay_ms)


def enqueue_workflow_run(run_id: str) -> None:
    _enqueue_actor(WORKFLOW_RUN_GENERATION_TASK_CONTRACT.actor_name, run_id)


def enqueue_workflow_run_later(run_id: str, *, delay_ms: int) -> None:
    _enqueue_actor(WORKFLOW_RUN_GENERATION_TASK_CONTRACT.actor_name, run_id, delay_ms=delay_ms)


def enqueue_workflow_node_run(node_run_id: str) -> None:
    _enqueue_actor(WORKFLOW_NODE_RUN_ACTOR_NAME, node_run_id)


def enqueue_workflow_node_run_later(node_run_id: str, *, delay_ms: int) -> None:
    _enqueue_actor(WORKFLOW_NODE_RUN_ACTOR_NAME, node_run_id, delay_ms=delay_ms)


def enqueue_image_session_generation_task(task_id: str) -> None:
    _enqueue_actor(IMAGE_SESSION_GENERATION_TASK_CONTRACT.actor_name, task_id)


def enqueue_image_session_generation_task_later(task_id: str, *, delay_ms: int) -> None:
    _enqueue_actor(IMAGE_SESSION_GENERATION_TASK_CONTRACT.actor_name, task_id, delay_ms=delay_ms)


def enqueue_agent_turn_sync(projection_id: str) -> None:
    _enqueue_actor(AGENT_TURN_SYNC_ACTOR_NAME, projection_id)


def enqueue_agent_turn_sync_later(projection_id: str, *, delay_ms: int) -> None:
    _enqueue_actor(AGENT_TURN_SYNC_ACTOR_NAME, projection_id, delay_ms=delay_ms)
