from __future__ import annotations

from datetime import UTC, datetime, timedelta
from pathlib import Path

import pytest
from helpers import (
    _make_demo_image_bytes,
)

from productflow_backend.application.image_sessions import create_image_session, create_image_session_generation_task
from productflow_backend.application.use_cases import (
    create_copy_job,
    create_product,
)
from productflow_backend.domain.enums import (
    JobKind,
    JobStatus,
)
from productflow_backend.infrastructure.queue import (
    recover_unfinished_image_session_generation_tasks,
    recover_unfinished_jobs,
)


def test_recover_unfinished_jobs_requeues_queued_jobs(
    db_session,
    configured_env: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    product = create_product(
        db_session,
        name="收纳盒",
        category="家居",
        price="19.90",
        source_note=None,
        image_bytes=_make_demo_image_bytes(),
        filename="box.png",
        content_type="image/png",
    )
    job = create_copy_job(db_session, product_id=product.id).job
    sent: list[tuple[str, JobKind]] = []

    monkeypatch.setattr(
        "productflow_backend.infrastructure.queue._send_job_to_queue",
        lambda job_id, kind: sent.append((job_id, kind)),
    )

    summary = recover_unfinished_jobs()

    assert summary.queued_jobs == 1
    assert summary.stale_running_jobs == 0
    assert summary.enqueued_jobs == 1
    assert sent == [(job.id, JobKind.COPY_GENERATION)]

def test_recover_unfinished_jobs_resets_stale_running_jobs(
    db_session,
    configured_env: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    product = create_product(
        db_session,
        name="收纳盒",
        category="家居",
        price="19.90",
        source_note=None,
        image_bytes=_make_demo_image_bytes(),
        filename="box.png",
        content_type="image/png",
    )
    job = create_copy_job(db_session, product_id=product.id).job
    job.status = JobStatus.RUNNING
    job.started_at = datetime.now(UTC) - timedelta(hours=2)
    db_session.commit()
    sent: list[tuple[str, JobKind]] = []

    monkeypatch.setattr(
        "productflow_backend.infrastructure.queue._send_job_to_queue",
        lambda job_id, kind: sent.append((job_id, kind)),
    )

    summary = recover_unfinished_jobs(reset_stale_running=True, stale_running_after=timedelta(minutes=30))
    db_session.refresh(job)

    assert summary.queued_jobs == 0
    assert summary.stale_running_jobs == 1
    assert summary.enqueued_jobs == 1
    assert sent == [(job.id, JobKind.COPY_GENERATION)]
    assert job.status == JobStatus.QUEUED
    assert job.started_at is None


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
