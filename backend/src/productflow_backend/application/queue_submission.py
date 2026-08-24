"""Durable 入队。broker 失败必须留下可观察的 failed 业务状态，再抛 QueueUnavailableError。"""

from __future__ import annotations

from collections.abc import Callable
from typing import NoReturn

from productflow_backend.domain.durable_generation_tasks import QUEUE_UNAVAILABLE_DETAIL
from productflow_backend.domain.errors import QueueUnavailableError


def enqueue_or_mark_failed(
    task_id: str,
    *,
    enqueue: Callable[[str], None],
    mark_failed: Callable[[str, str], None],
) -> None:
    """投递 durable 消息。broker 失败先把已持久化任务标 failed，再抛 QueueUnavailableError。"""

    try:
        enqueue(task_id)
    except Exception as exc:  # noqa: BLE001
        mark_failed(task_id, QUEUE_UNAVAILABLE_DETAIL)
        raise_queue_unavailable(exc)


def raise_queue_unavailable(exc: Exception) -> NoReturn:
    raise QueueUnavailableError(QUEUE_UNAVAILABLE_DETAIL) from exc
