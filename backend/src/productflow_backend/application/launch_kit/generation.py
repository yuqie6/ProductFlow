from __future__ import annotations

from collections.abc import Callable
from datetime import UTC, datetime

from sqlalchemy import select
from sqlalchemy.orm import Session

from productflow_backend.application.launch_kit.query import get_launch_kit
from productflow_backend.application.queue_submission import enqueue_or_mark_failed
from productflow_backend.domain.enums import JobStatus
from productflow_backend.domain.errors import BusinessValidationError
from productflow_backend.domain.launch_kits import LaunchKitFailureCategory, LaunchKitProgressStage, LaunchKitStatus
from productflow_backend.infrastructure.db.models import LaunchKit, LaunchKitGenerationTask
from productflow_backend.infrastructure.queue import enqueue_launch_kit_generation_task

ACTIVE_TASK_STATUSES = (JobStatus.QUEUED, JobStatus.RUNNING)


def _now() -> datetime:
    return datetime.now(UTC)


def _has_active_generation_task(session: Session, launch_kit_id: str) -> bool:
    return session.scalar(
        select(LaunchKitGenerationTask.id)
        .where(LaunchKitGenerationTask.launch_kit_id == launch_kit_id)
        .where(LaunchKitGenerationTask.status.in_(ACTIVE_TASK_STATUSES))
        .limit(1)
    ) is not None


def create_launch_kit_generation_task(session: Session, *, launch_kit_id: str) -> LaunchKitGenerationTask:
    launch_kit = get_launch_kit(session, launch_kit_id)
    if _has_active_generation_task(session, launch_kit.id):
        raise BusinessValidationError("LaunchKit generation is already queued or running")

    task = LaunchKitGenerationTask(
        launch_kit_id=launch_kit.id,
        status=JobStatus.QUEUED,
        progress_stage=LaunchKitProgressStage.EXTRACTING_FACTS,
        attempt_count=0,
        is_retryable=True,
        is_cancelable=True,
        provider_metadata_json={"schema_version": 1},
    )
    launch_kit.status = LaunchKitStatus.GENERATING
    launch_kit.updated_at = _now()
    session.add(task)
    session.commit()
    session.expire_all()
    return session.get(LaunchKitGenerationTask, task.id) or task


def mark_launch_kit_generation_task_enqueue_failed(session: Session, *, task_id: str, reason: str) -> None:
    task = session.get(LaunchKitGenerationTask, task_id)
    if task is None:
        return
    task.status = JobStatus.FAILED
    task.failure_category = LaunchKitFailureCategory.QUEUE_UNAVAILABLE.value
    task.failure_detail = reason
    task.is_retryable = True
    task.is_cancelable = False
    task.finished_at = _now()
    task.progress_updated_at = task.finished_at
    launch_kit = session.get(LaunchKit, task.launch_kit_id)
    if launch_kit is not None:
        launch_kit.status = LaunchKitStatus.FAILED
        launch_kit.updated_at = task.finished_at
    session.commit()


def submit_launch_kit_generation_task(
    session: Session,
    *,
    launch_kit_id: str,
    enqueue: Callable[[str], None] | None = None,
) -> LaunchKit:
    task = create_launch_kit_generation_task(session, launch_kit_id=launch_kit_id)
    enqueue_or_mark_failed(
        task.id,
        enqueue=enqueue or enqueue_launch_kit_generation_task,
        mark_failed=lambda task_id, reason: mark_launch_kit_generation_task_enqueue_failed(
            session,
            task_id=task_id,
            reason=reason,
        ),
    )
    session.expire_all()
    return get_launch_kit(session, launch_kit_id)
