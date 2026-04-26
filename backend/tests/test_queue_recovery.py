from __future__ import annotations

from datetime import UTC, datetime, timedelta
from pathlib import Path

import pytest
from helpers import (
    _make_demo_image_bytes,
)

from productflow_backend.application.use_cases import (
    create_copy_job,
    create_product,
)
from productflow_backend.domain.enums import (
    JobKind,
    JobStatus,
)
from productflow_backend.infrastructure.queue import recover_unfinished_jobs


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
