"""启动恢复：PostgreSQL 业务行为是权威，Redis/Dramatiq 只补发 delivery attempt。"""

from __future__ import annotations

import logging
from collections.abc import Callable
from dataclasses import dataclass
from datetime import timedelta

from sqlalchemy import func, or_, select, update
from sqlalchemy.orm import Session

from productflow_backend.application.local_image_edits.service import (
    LOCAL_EDIT_STALE_CLAIM_AFTER,
    recover_local_image_edit_task,
)
from productflow_backend.application.product_workflow.graph_run_durability import (
    DEFAULT_STALE_RUNNING_AFTER,
    WorkflowRunRecoverySummary,
    recover_unfinished_graph_runs,
)
from productflow_backend.domain.durable_generation_tasks import (
    DELIVERY_RENDITION_TASK_CONTRACT,
    IMAGE_SESSION_GENERATION_TASK_CONTRACT,
    IMAGE_SESSION_PROVIDER_EFFECT_UNKNOWN_DETAIL,
    IMAGE_SESSION_PROVIDER_EFFECT_UNKNOWN_PHASE,
    LOCAL_IMAGE_EDIT_TASK_CONTRACT,
)
from productflow_backend.domain.enums import JobStatus
from productflow_backend.infrastructure.db.models import (
    DeliveryRenditionJob,
    ImageSessionGenerationTask,
    ImageSessionProviderEffect,
    LocalImageEditTask,
    utcnow,
)
from productflow_backend.infrastructure.db.session import get_session_factory
from productflow_backend.infrastructure.runtime_settings import get_runtime_settings

logger = logging.getLogger(__name__)

def get_image_session_stale_running_after() -> timedelta:
    return timedelta(minutes=int(get_runtime_settings().image_session_stale_running_after_minutes))


@dataclass(frozen=True, slots=True)
class ImageSessionGenerationTaskRecoverySummary:
    """启动恢复结果：把连续生图 durable 任务补回队列。"""

    queued_tasks: int = 0
    stale_running_tasks: int = 0
    enqueued_tasks: int = 0
    unknown_tasks: int = 0


@dataclass(frozen=True, slots=True)
class DeliveryRenditionJobRecoverySummary:
    queued_jobs: int = 0
    stale_running_jobs: int = 0
    enqueued_jobs: int = 0


@dataclass(frozen=True, slots=True)
class LocalImageEditTaskRecoverySummary:
    queued_tasks: int = 0
    stale_running_tasks: int = 0
    enqueued_tasks: int = 0
    unknown_tasks: int = 0


def recover_unfinished_workflow_runs(
    *,
    enqueue: Callable[[str], None] | None = None,
    stage_dispatch: Callable[[Session, str], None] | None = None,
    reset_stale_running: bool = False,
    stale_running_after: timedelta = DEFAULT_STALE_RUNNING_AFTER,
) -> WorkflowRunRecoverySummary:
    """Delegate graph-run recovery to its durability owner."""

    return recover_unfinished_graph_runs(
        enqueue=enqueue,
        stage_dispatch=stage_dispatch,
        reset_stale_running=reset_stale_running,
        stale_running_after=stale_running_after,
    )


def recover_unfinished_image_session_generation_tasks(
    *,
    enqueue: Callable[[str], None] | None = None,
    stage_dispatch: Callable[[Session, str], None] | None = None,
    reset_stale_running: bool = False,
    stale_running_after: timedelta | None = None,
) -> ImageSessionGenerationTaskRecoverySummary:
    """恢复 queued / stale running 的连续生图任务，Redis 只作为可补发 delivery。"""

    resolved_stale_running_after = (
        get_image_session_stale_running_after() if stale_running_after is None else stale_running_after
    )
    cutoff = utcnow() - resolved_stale_running_after
    session = get_session_factory()()
    task_ids_to_enqueue: list[str] = []
    queued_tasks = 0
    stale_running_tasks = 0
    unknown_tasks = 0

    try:
        last_progress_at = func.coalesce(
            ImageSessionGenerationTask.progress_updated_at,
            ImageSessionGenerationTask.started_at,
        )
        statement = select(ImageSessionGenerationTask).where(
            ImageSessionGenerationTask.is_retryable.is_(True),
            or_(
                ImageSessionGenerationTask.status.in_(IMAGE_SESSION_GENERATION_TASK_CONTRACT.queued_statuses),
                (
                    (ImageSessionGenerationTask.status.in_(IMAGE_SESSION_GENERATION_TASK_CONTRACT.running_statuses))
                    & (last_progress_at <= cutoff)
                )
                if reset_stale_running
                else False,
            ),
        )
        tasks = list(session.scalars(statement).all())
        for task in tasks:
            if IMAGE_SESSION_GENERATION_TASK_CONTRACT.is_running(task.status):
                observed_attempt_id = task.active_attempt_id
                if observed_attempt_id is None:
                    continue
                now = utcnow()
                safe_requeue_phases = {"running", "candidate_saved"}
                next_candidate_index = task.completed_candidates + 1
                has_unmaterialized_provider_effect = session.scalar(
                    select(ImageSessionProviderEffect.id)
                    .where(
                        ImageSessionProviderEffect.generation_task_id == task.id,
                        ImageSessionProviderEffect.effect_result.in_(
                            ("pending", "applied", "unknown")
                        ),
                        ImageSessionProviderEffect.candidate_start_index <= next_candidate_index,
                        (
                            ImageSessionProviderEffect.candidate_start_index
                            + ImageSessionProviderEffect.candidate_count
                            - 1
                        )
                        >= next_candidate_index,
                    )
                    .limit(1)
                ) is not None
                provider_effect_unknown = (
                    task.active_candidate_index is not None
                    or task.progress_phase not in safe_requeue_phases
                    or has_unmaterialized_provider_effect
                )
                values: dict[str, object]
                if provider_effect_unknown:
                    # 进行中或未物化的 provider effect 不能安全重入队。
                    metadata = dict(task.progress_metadata) if isinstance(task.progress_metadata, dict) else {}
                    metadata["unknown_provider_effect"] = {
                        "attempt_id": observed_attempt_id,
                        "active_candidate_index": task.active_candidate_index,
                        "observed_phase": task.progress_phase,
                        "next_candidate_index": next_candidate_index,
                        "has_unmaterialized_provider_effect": has_unmaterialized_provider_effect,
                    }
                    values = {
                        "status": JobStatus.UNKNOWN,
                        "active_attempt_id": None,
                        "finished_at": now,
                        "is_retryable": False,
                        "active_candidate_index": None,
                        "progress_phase": IMAGE_SESSION_PROVIDER_EFFECT_UNKNOWN_PHASE,
                        "progress_updated_at": now,
                        "failure_reason": IMAGE_SESSION_PROVIDER_EFFECT_UNKNOWN_DETAIL,
                        "progress_metadata": metadata,
                    }
                    session.execute(
                        update(ImageSessionProviderEffect)
                        .where(
                            ImageSessionProviderEffect.generation_task_id == task.id,
                            ImageSessionProviderEffect.attempt_id == observed_attempt_id,
                            ImageSessionProviderEffect.effect_result == "pending",
                        )
                        .values(
                            effect_result="unknown",
                            reconciliation_state="unknown",
                            detail=IMAGE_SESSION_PROVIDER_EFFECT_UNKNOWN_DETAIL,
                            updated_at=now,
                        )
                        .execution_options(synchronize_session=False)
                    )
                else:
                    values = {
                        "status": JobStatus.QUEUED,
                        "active_attempt_id": None,
                        "started_at": None,
                        "finished_at": None,
                        "active_candidate_index": None,
                        "provider_response_status": None,
                        "provider_response_id": None,
                        "progress_phase": "requeued_after_idle",
                        "progress_updated_at": now,
                    }
                reset = session.execute(
                    update(ImageSessionGenerationTask)
                    .where(
                        ImageSessionGenerationTask.id == task.id,
                        ImageSessionGenerationTask.status == JobStatus.RUNNING,
                        ImageSessionGenerationTask.active_attempt_id == observed_attempt_id,
                        last_progress_at <= cutoff,
                    )
                    .values(**values)
                    .execution_options(synchronize_session=False)
                )
                if reset.rowcount != 1:
                    if stage_dispatch is None:
                        session.rollback()
                    continue
                if provider_effect_unknown:
                    unknown_tasks += 1
                else:
                    stale_running_tasks += 1
                if not provider_effect_unknown:
                    task_ids_to_enqueue.append(task.id)
                    if stage_dispatch is not None:
                        stage_dispatch(session, task.id)
                if stage_dispatch is None:
                    session.commit()
            else:
                queued_tasks += 1
                task_ids_to_enqueue.append(task.id)
                if stage_dispatch is not None:
                    stage_dispatch(session, task.id)
        if stage_dispatch is not None:
            session.commit()
    except Exception:
        session.rollback()
        logger.exception("恢复滞留连续生图任务时读取数据库失败")
        raise
    finally:
        session.close()

    enqueued_tasks = len(task_ids_to_enqueue) if stage_dispatch is not None else 0
    if stage_dispatch is None:
        if enqueue is None:
            raise ValueError("enqueue or stage_dispatch is required")
        for task_id in task_ids_to_enqueue:
            try:
                enqueue(task_id)
                enqueued_tasks += 1
            except Exception:
                logger.exception("恢复滞留连续生图任务入队失败: task_id=%s", task_id)

    if task_ids_to_enqueue or unknown_tasks:
        logger.info(
            "已恢复滞留连续生图任务: queued=%s stale_running=%s unknown=%s enqueued=%s",
            queued_tasks,
            stale_running_tasks,
            unknown_tasks,
            enqueued_tasks,
        )
    return ImageSessionGenerationTaskRecoverySummary(
        queued_tasks=queued_tasks,
        stale_running_tasks=stale_running_tasks,
        enqueued_tasks=enqueued_tasks,
        unknown_tasks=unknown_tasks,
    )


def recover_unfinished_delivery_rendition_jobs(
    *,
    enqueue: Callable[[str], None] | None = None,
    stage_dispatch: Callable[[Session, str], None] | None = None,
    reset_stale_running: bool = False,
    stale_running_after: timedelta = DEFAULT_STALE_RUNNING_AFTER,
) -> DeliveryRenditionJobRecoverySummary:
    """恢复 queued / stale running 交付派生任务；数据库状态是权威。"""

    cutoff = utcnow() - stale_running_after
    session = get_session_factory()()
    job_ids_to_enqueue: list[str] = []
    queued_jobs = 0
    stale_running_jobs = 0
    try:
        statement = select(DeliveryRenditionJob).where(
            DeliveryRenditionJob.is_retryable.is_(True),
            or_(
                DeliveryRenditionJob.status.in_(DELIVERY_RENDITION_TASK_CONTRACT.queued_statuses),
                (
                    DeliveryRenditionJob.status.in_(DELIVERY_RENDITION_TASK_CONTRACT.running_statuses)
                    & (DeliveryRenditionJob.started_at <= cutoff)
                )
                if reset_stale_running
                else False,
            ),
        )
        for job in session.scalars(statement).all():
            if DELIVERY_RENDITION_TASK_CONTRACT.is_running(job.status):
                reset = session.execute(
                    update(DeliveryRenditionJob)
                    .where(
                        DeliveryRenditionJob.id == job.id,
                        DeliveryRenditionJob.status == JobStatus.RUNNING,
                        DeliveryRenditionJob.active_attempt_id == job.active_attempt_id,
                        DeliveryRenditionJob.started_at <= cutoff,
                    )
                    .values(
                        status=JobStatus.QUEUED,
                        active_attempt_id=None,
                        failure_reason=None,
                        started_at=None,
                        finished_at=None,
                        updated_at=utcnow(),
                    )
                    .execution_options(synchronize_session=False)
                )
                if reset.rowcount != 1:
                    continue
                stale_running_jobs += 1
            else:
                queued_jobs += 1
            job_ids_to_enqueue.append(job.id)
        if stage_dispatch is not None:
            for job_id in job_ids_to_enqueue:
                stage_dispatch(session, job_id)
            session.commit()
        elif stale_running_jobs:
            session.commit()
    except Exception:
        session.rollback()
        logger.exception("恢复滞留交付派生任务时读取数据库失败")
        raise
    finally:
        session.close()

    enqueued_jobs = len(job_ids_to_enqueue) if stage_dispatch is not None else 0
    if stage_dispatch is None:
        if enqueue is None:
            raise ValueError("enqueue or stage_dispatch is required")
        for job_id in job_ids_to_enqueue:
            try:
                enqueue(job_id)
                enqueued_jobs += 1
            except Exception:
                logger.exception("恢复滞留交付派生任务入队失败: job_id=%s", job_id)
    if job_ids_to_enqueue:
        logger.info(
            "已恢复滞留交付派生任务: queued=%s stale_running=%s enqueued=%s",
            queued_jobs,
            stale_running_jobs,
            enqueued_jobs,
        )
    return DeliveryRenditionJobRecoverySummary(
        queued_jobs=queued_jobs,
        stale_running_jobs=stale_running_jobs,
        enqueued_jobs=enqueued_jobs,
    )


def recover_unfinished_local_image_edit_tasks(
    *,
    enqueue: Callable[[str], None] | None = None,
    stage_dispatch: Callable[[Session, str], None] | None = None,
    reset_stale_running: bool = False,
    stale_running_after: timedelta = LOCAL_EDIT_STALE_CLAIM_AFTER,
) -> LocalImageEditTaskRecoverySummary:
    """Recover local-edit delivery through the service-owned fencing state machine."""

    session = get_session_factory()()
    task_ids_to_enqueue: list[str] = []
    queued_tasks = 0
    stale_running_tasks = 0
    unknown_tasks = 0
    try:
        task_ids = list(
            session.scalars(
                select(LocalImageEditTask.id).where(
                    LocalImageEditTask.status.in_(LOCAL_IMAGE_EDIT_TASK_CONTRACT.active_statuses)
                )
            ).all()
        )
        for task_id in task_ids:
            result = recover_local_image_edit_task(
                session,
                task_id=task_id,
                reset_stale_running=reset_stale_running,
                stale_after=stale_running_after,
            )
            if result.outcome == "queued":
                queued_tasks += 1
                task_ids_to_enqueue.append(task_id)
            elif result.outcome == "requeued":
                stale_running_tasks += 1
                task_ids_to_enqueue.append(task_id)
            elif result.outcome == "unknown":
                unknown_tasks += 1
            if stage_dispatch is not None and result.outcome in {"queued", "requeued"}:
                stage_dispatch(session, task_id)
                session.commit()
    except Exception:
        session.rollback()
        logger.exception("恢复滞留局部编辑任务时读取数据库失败")
        raise
    finally:
        session.close()

    enqueued_tasks = len(task_ids_to_enqueue) if stage_dispatch is not None else 0
    if stage_dispatch is None:
        if enqueue is None:
            raise ValueError("enqueue or stage_dispatch is required")
        for task_id in task_ids_to_enqueue:
            try:
                enqueue(task_id)
                enqueued_tasks += 1
            except Exception:
                logger.exception("恢复滞留局部编辑任务入队失败: task_id=%s", task_id)
    return LocalImageEditTaskRecoverySummary(
        queued_tasks=queued_tasks,
        stale_running_tasks=stale_running_tasks,
        enqueued_tasks=enqueued_tasks,
        unknown_tasks=unknown_tasks,
    )
