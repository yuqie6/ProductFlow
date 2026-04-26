from __future__ import annotations

from sqlalchemy import func, select
from sqlalchemy.orm import Session

from productflow_backend.config import get_runtime_settings
from productflow_backend.domain.enums import JobStatus, WorkflowRunStatus
from productflow_backend.domain.errors import ResourceBusyError
from productflow_backend.infrastructure.db.models import ImageSessionGenerationTask, JobRun, WorkflowRun

GENERATION_BUSY_DETAIL = "当前生成任务较多，请稍后再试"


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
