from __future__ import annotations

from collections.abc import Iterator
from contextlib import contextmanager
from threading import Lock

from sqlalchemy import func, select
from sqlalchemy.orm import Session

from productflow_backend.config import get_runtime_settings
from productflow_backend.domain.enums import JobStatus, WorkflowRunStatus
from productflow_backend.domain.errors import ResourceBusyError
from productflow_backend.infrastructure.db.models import JobRun, WorkflowRun

GENERATION_BUSY_DETAIL = "当前生成任务较多，请稍后再试"

_sync_generation_lock = Lock()
_sync_generation_active = 0


def _active_async_task_count(session: Session) -> int:
    active_jobs = session.scalar(
        select(func.count()).select_from(JobRun).where(JobRun.status.in_([JobStatus.QUEUED, JobStatus.RUNNING]))
    )
    active_workflow_runs = session.scalar(
        select(func.count()).select_from(WorkflowRun).where(WorkflowRun.status == WorkflowRunStatus.RUNNING)
    )
    return int(active_jobs or 0) + int(active_workflow_runs or 0)


def active_generation_task_count(session: Session) -> int:
    """Return globally active provider/worker work visible to the current API process."""

    with _sync_generation_lock:
        sync_active = _sync_generation_active
    return _active_async_task_count(session) + sync_active


def _raise_if_at_capacity(session: Session, *, sync_active: int | None = None) -> None:
    limit = get_runtime_settings().generation_max_concurrent_tasks
    if sync_active is None:
        with _sync_generation_lock:
            sync_active = _sync_generation_active
    active_count = _active_async_task_count(session) + sync_active
    if active_count >= limit:
        raise ResourceBusyError(GENERATION_BUSY_DETAIL)


def ensure_generation_capacity(session: Session) -> None:
    """Reject a new async resource-consuming action when the global cap is already full."""

    _raise_if_at_capacity(session)


@contextmanager
def admit_synchronous_generation(session: Session) -> Iterator[None]:
    """Reserve one in-process slot for the synchronous image-session generation endpoint.

    Image-session generation still executes inside the HTTP request, so it cannot be represented by a durable
    JobRun/WorkflowRun row yet. The in-process slot keeps this synchronous path under the same admission-control
    contract while async rows remain the cross-process source of truth.
    """

    global _sync_generation_active

    reserved = False
    with _sync_generation_lock:
        _raise_if_at_capacity(session, sync_active=_sync_generation_active)
        _sync_generation_active += 1
        reserved = True
    try:
        yield
    finally:
        if reserved:
            with _sync_generation_lock:
                _sync_generation_active = max(0, _sync_generation_active - 1)
