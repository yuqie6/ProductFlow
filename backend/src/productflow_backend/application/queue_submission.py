from __future__ import annotations

from collections.abc import Callable
from typing import NoReturn

from productflow_backend.domain.errors import QueueUnavailableError

QUEUE_UNAVAILABLE_DETAIL = "任务队列暂不可用，请稍后重试"


def enqueue_or_mark_failed(
    task_id: str,
    *,
    enqueue: Callable[[str], None],
    mark_failed: Callable[[str, str], None],
) -> None:
    """Send the durable queue message, marking persisted task state failed if delivery fails."""

    try:
        enqueue(task_id)
    except Exception as exc:  # noqa: BLE001
        mark_failed(task_id, QUEUE_UNAVAILABLE_DETAIL)
        raise_queue_unavailable(exc)


def raise_queue_unavailable(exc: Exception) -> NoReturn:
    raise QueueUnavailableError(QUEUE_UNAVAILABLE_DETAIL) from exc
