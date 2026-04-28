from __future__ import annotations

from datetime import UTC, datetime, timedelta
from pathlib import Path

import pytest

from productflow_backend.application.image_sessions import create_image_session, create_image_session_generation_task
from productflow_backend.domain.enums import JobStatus
from productflow_backend.infrastructure.queue import recover_unfinished_image_session_generation_tasks


def test_recover_unfinished_image_session_generation_tasks_requeues_queued_tasks(
    db_session,
    configured_env: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    image_session = create_image_session(db_session, product_id=None, title="queued 恢复")
    result = create_image_session_generation_task(
        db_session,
        image_session_id=image_session.id,
        prompt="queued 任务应补发",
        size="1024x1024",
    )
    sent: list[str] = []
    monkeypatch.setattr(
        "productflow_backend.infrastructure.queue.enqueue_image_session_generation_task",
        lambda task_id: sent.append(task_id),
    )

    summary = recover_unfinished_image_session_generation_tasks()

    assert summary.queued_tasks == 1
    assert summary.stale_running_tasks == 0
    assert summary.enqueued_tasks == 1
    assert sent == [result.task.id]


def test_recover_unfinished_image_session_generation_tasks_resets_stale_running_tasks(
    db_session,
    configured_env: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    image_session = create_image_session(db_session, product_id=None, title="running 恢复")
    result = create_image_session_generation_task(
        db_session,
        image_session_id=image_session.id,
        prompt="stale running 任务应重置",
        size="1024x1024",
    )
    result.task.status = JobStatus.RUNNING
    result.task.started_at = datetime.now(UTC) - timedelta(hours=2)
    db_session.commit()
    sent: list[str] = []
    monkeypatch.setattr(
        "productflow_backend.infrastructure.queue.enqueue_image_session_generation_task",
        lambda task_id: sent.append(task_id),
    )

    summary = recover_unfinished_image_session_generation_tasks(
        reset_stale_running=True,
        stale_running_after=timedelta(minutes=30),
    )
    db_session.refresh(result.task)

    assert summary.queued_tasks == 0
    assert summary.stale_running_tasks == 1
    assert summary.enqueued_tasks == 1
    assert sent == [result.task.id]
    assert result.task.status == JobStatus.QUEUED
    assert result.task.started_at is None
