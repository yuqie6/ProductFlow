from __future__ import annotations

import logging
from collections.abc import Callable
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta

from sqlalchemy import func, or_, select, update
from sqlalchemy.orm import selectinload

from productflow_backend.application.runtime_settings import get_runtime_settings
from productflow_backend.domain.durable_generation_tasks import (
    DELIVERY_RENDITION_TASK_CONTRACT,
    IMAGE_SESSION_GENERATION_TASK_CONTRACT,
    WORKFLOW_RUN_GENERATION_TASK_CONTRACT,
    WorkflowRunDeliveryState,
    classify_workflow_run_delivery,
)
from productflow_backend.domain.enums import JobStatus, WorkflowNodeStatus
from productflow_backend.infrastructure.db.models import (
    DeliveryRenditionJob,
    ImageSessionGenerationTask,
    WorkflowNode,
    WorkflowRun,
    utcnow,
)
from productflow_backend.infrastructure.db.session import get_session_factory

logger = logging.getLogger(__name__)

DEFAULT_STALE_RUNNING_AFTER = timedelta(minutes=30)


def get_image_session_stale_running_after() -> timedelta:
    return timedelta(minutes=int(get_runtime_settings().image_session_stale_running_after_minutes))


def _as_aware_utc(value: datetime) -> datetime:
    if value.tzinfo is None:
        return value.replace(tzinfo=UTC)
    return value


@dataclass(frozen=True, slots=True)
class WorkflowRunRecoverySummary:
    """启动恢复结果：把数据库里仍处于 active 的工作流运行补回队列。"""

    queued_runs: int = 0
    stale_running_runs: int = 0
    enqueued_runs: int = 0


@dataclass(frozen=True, slots=True)
class ImageSessionGenerationTaskRecoverySummary:
    """启动恢复结果：把连续生图 durable 任务补回队列。"""

    queued_tasks: int = 0
    stale_running_tasks: int = 0
    enqueued_tasks: int = 0


@dataclass(frozen=True, slots=True)
class DeliveryRenditionJobRecoverySummary:
    queued_jobs: int = 0
    stale_running_jobs: int = 0
    enqueued_jobs: int = 0


def recover_unfinished_workflow_runs(
    *,
    enqueue: Callable[[str], None],
    reset_stale_running: bool = False,
    stale_running_after: timedelta = DEFAULT_STALE_RUNNING_AFTER,
) -> WorkflowRunRecoverySummary:
    """恢复重启期间滞留的商品工作流运行。

    `workflow_runs` 是 authoritative state，Redis/Dramatiq 只是 delivery attempt。当前工作流 run 沿用
    `running` 作为 active 状态：如果没有节点正在执行，说明消息可能丢失或还未消费，启动时可以补发；如果有节点正在
    `running`，只有 worker 启动并且节点运行超过 stale cutoff 时才把这些节点重置为 `queued` 再补发。
    """

    cutoff = utcnow() - stale_running_after
    session = get_session_factory()()
    runs_to_enqueue: list[str] = []
    queued_runs = 0
    stale_running_runs = 0

    try:
        runs = list(
            session.scalars(
                select(WorkflowRun)
                .options(selectinload(WorkflowRun.node_runs))
                .where(WorkflowRun.status.in_(WORKFLOW_RUN_GENERATION_TASK_CONTRACT.active_statuses))
            ).all()
        )
        for run in runs:
            delivery_state = classify_workflow_run_delivery(
                run.status,
                [node_run.status for node_run in run.node_runs],
            )
            running_node_runs = [
                node_run
                for node_run in run.node_runs
                if WORKFLOW_RUN_GENERATION_TASK_CONTRACT.execution_is_running(node_run.status)
            ]
            if delivery_state == WorkflowRunDeliveryState.RUNNING:
                stale_node_runs = [
                    node_run
                    for node_run in running_node_runs
                    if node_run.started_at is not None and _as_aware_utc(node_run.started_at) <= cutoff
                ]
                if not reset_stale_running or len(stale_node_runs) != len(running_node_runs):
                    continue
                for stale_node_run in stale_node_runs:
                    stale_node_run.status = WorkflowNodeStatus.QUEUED
                    node = session.get(WorkflowNode, stale_node_run.node_id)
                    if node is not None and WORKFLOW_RUN_GENERATION_TASK_CONTRACT.execution_is_running(node.status):
                        node.status = WorkflowNodeStatus.QUEUED
                        node.failure_reason = None
                run.failure_reason = None
                stale_running_runs += 1
                runs_to_enqueue.append(run.id)
                continue

            if delivery_state == WorkflowRunDeliveryState.QUEUED:
                queued_runs += 1
                runs_to_enqueue.append(run.id)

        if stale_running_runs:
            session.commit()
    except Exception:
        session.rollback()
        logger.exception("恢复滞留工作流运行时读取数据库失败")
        return WorkflowRunRecoverySummary()
    finally:
        session.close()

    enqueued_runs = 0
    for run_id in runs_to_enqueue:
        try:
            enqueue(run_id)
            enqueued_runs += 1
        except Exception:
            logger.exception("恢复滞留工作流运行入队失败: workflow_run_id=%s", run_id)

    if runs_to_enqueue:
        logger.info(
            "已恢复滞留工作流运行: queued=%s stale_running=%s enqueued=%s",
            queued_runs,
            stale_running_runs,
            enqueued_runs,
        )
    return WorkflowRunRecoverySummary(
        queued_runs=queued_runs,
        stale_running_runs=stale_running_runs,
        enqueued_runs=enqueued_runs,
    )


def recover_unfinished_image_session_generation_tasks(
    *,
    enqueue: Callable[[str], None],
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
                if task.completed_candidates:
                    now = utcnow()
                    task.status = JobStatus.FAILED
                    task.finished_at = now
                    task.is_retryable = False
                    task.active_candidate_index = None
                    task.progress_phase = "failed_idle_timeout"
                    task.progress_updated_at = now
                    task.failure_reason = (
                        f"已生成 {task.completed_candidates}/{task.generation_count} 张候选，"
                        "但任务超时，剩余候选未完成。"
                    )
                else:
                    task.status = JobStatus.QUEUED
                    task.started_at = None
                    task.active_candidate_index = None
                    task.provider_response_status = None
                    task.provider_response_id = None
                    task.progress_phase = "requeued_after_idle"
                    task.progress_updated_at = utcnow()
                    task_ids_to_enqueue.append(task.id)
                stale_running_tasks += 1
            else:
                queued_tasks += 1
                task_ids_to_enqueue.append(task.id)
        if stale_running_tasks:
            session.commit()
    except Exception:
        session.rollback()
        logger.exception("恢复滞留连续生图任务时读取数据库失败")
        return ImageSessionGenerationTaskRecoverySummary()
    finally:
        session.close()

    enqueued_tasks = 0
    for task_id in task_ids_to_enqueue:
        try:
            enqueue(task_id)
            enqueued_tasks += 1
        except Exception:
            logger.exception("恢复滞留连续生图任务入队失败: task_id=%s", task_id)

    if task_ids_to_enqueue:
        logger.info(
            "已恢复滞留连续生图任务: queued=%s stale_running=%s enqueued=%s",
            queued_tasks,
            stale_running_tasks,
            enqueued_tasks,
        )
    return ImageSessionGenerationTaskRecoverySummary(
        queued_tasks=queued_tasks,
        stale_running_tasks=stale_running_tasks,
        enqueued_tasks=enqueued_tasks,
    )


def recover_unfinished_delivery_rendition_jobs(
    *,
    enqueue: Callable[[str], None],
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
        if stale_running_jobs:
            session.commit()
    except Exception:
        session.rollback()
        logger.exception("恢复滞留交付派生任务时读取数据库失败")
        return DeliveryRenditionJobRecoverySummary()
    finally:
        session.close()

    enqueued_jobs = 0
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
