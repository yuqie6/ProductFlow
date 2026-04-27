from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime

from sqlalchemy import func, select
from sqlalchemy.orm import Session

from productflow_backend.config import get_runtime_settings
from productflow_backend.domain.enums import JobStatus, WorkflowRunStatus
from productflow_backend.domain.errors import ResourceBusyError
from productflow_backend.infrastructure.db.models import ImageSessionGenerationTask, JobRun, WorkflowRun

GENERATION_BUSY_DETAIL = "当前生成任务较多，请稍后再试"


@dataclass(frozen=True, slots=True)
class GenerationQueueOverview:
    active_count: int
    running_count: int
    queued_count: int
    max_concurrent_tasks: int


@dataclass(frozen=True, slots=True)
class GenerationTaskQueueMetadata:
    overview: GenerationQueueOverview
    queued_ahead_count: int | None
    queue_position: int | None


def _active_async_task_count(session: Session) -> int:
    active_jobs = session.scalar(
        select(func.count()).select_from(JobRun).where(JobRun.status.in_([JobStatus.QUEUED, JobStatus.RUNNING]))
    )
    active_workflow_runs = session.scalar(
        select(func.count()).select_from(WorkflowRun).where(WorkflowRun.status == WorkflowRunStatus.RUNNING)
    )
    active_image_session_tasks = session.scalar(
        select(func.count())
        .select_from(ImageSessionGenerationTask)
        .where(ImageSessionGenerationTask.status.in_([JobStatus.QUEUED, JobStatus.RUNNING]))
    )
    return int(active_jobs or 0) + int(active_workflow_runs or 0) + int(active_image_session_tasks or 0)


def _status_count(session: Session, model: type, status: JobStatus | WorkflowRunStatus) -> int:
    count = session.scalar(select(func.count()).select_from(model).where(model.status == status))
    return int(count or 0)


def get_generation_queue_overview(session: Session) -> GenerationQueueOverview:
    """Return the global durable generation queue snapshot."""

    running_count = (
        _status_count(session, JobRun, JobStatus.RUNNING)
        + _status_count(session, WorkflowRun, WorkflowRunStatus.RUNNING)
        + _status_count(session, ImageSessionGenerationTask, JobStatus.RUNNING)
    )
    queued_count = (
        _status_count(session, JobRun, JobStatus.QUEUED)
        + _status_count(session, ImageSessionGenerationTask, JobStatus.QUEUED)
    )
    return GenerationQueueOverview(
        active_count=running_count + queued_count,
        running_count=running_count,
        queued_count=queued_count,
        max_concurrent_tasks=get_runtime_settings().generation_max_concurrent_tasks,
    )


def get_queued_generation_positions(session: Session) -> dict[str, int]:
    queued_items: list[tuple[datetime, str, str]] = []
    queued_items.extend(
        (job.created_at, "job", job.id)
        for job in session.scalars(select(JobRun).where(JobRun.status == JobStatus.QUEUED)).all()
    )
    queued_items.extend(
        (task.created_at, "image_session", task.id)
        for task in session.scalars(
            select(ImageSessionGenerationTask).where(ImageSessionGenerationTask.status == JobStatus.QUEUED)
        ).all()
    )
    queued_items.sort(key=lambda item: (item[0], item[1], item[2]))
    return {item_id: index + 1 for index, (_created_at, _kind, item_id) in enumerate(queued_items)}


def get_generation_task_queue_metadata(
    session: Session,
    task: ImageSessionGenerationTask,
    *,
    overview: GenerationQueueOverview | None = None,
    queued_positions: dict[str, int] | None = None,
) -> GenerationTaskQueueMetadata:
    overview = overview or get_generation_queue_overview(session)
    queued_ahead_count: int | None = None
    queue_position: int | None = None
    if task.status == JobStatus.QUEUED:
        positions = queued_positions or get_queued_generation_positions(session)
        queue_position = positions.get(task.id)
        if queue_position is not None:
            queued_ahead_count = max(0, queue_position - 1)
    return GenerationTaskQueueMetadata(
        overview=overview,
        queued_ahead_count=queued_ahead_count,
        queue_position=queue_position,
    )


def active_generation_task_count(session: Session) -> int:
    """Return globally active provider/worker work from durable DB rows."""

    return _active_async_task_count(session)


def _raise_if_at_capacity(session: Session) -> None:
    limit = get_runtime_settings().generation_max_concurrent_tasks
    active_count = _active_async_task_count(session)
    if active_count >= limit:
        raise ResourceBusyError(GENERATION_BUSY_DETAIL)


def ensure_generation_capacity(session: Session) -> None:
    """Reject a new async resource-consuming action when the global cap is already full."""

    _raise_if_at_capacity(session)
