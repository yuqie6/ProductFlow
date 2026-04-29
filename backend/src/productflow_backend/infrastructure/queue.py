from __future__ import annotations

import logging
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta
from functools import lru_cache

import dramatiq
from dramatiq.brokers.redis import RedisBroker
from sqlalchemy import func, or_, select
from sqlalchemy.orm import selectinload

from productflow_backend.config import get_runtime_settings, get_settings
from productflow_backend.domain.enums import JobStatus, WorkflowNodeStatus, WorkflowRunStatus
from productflow_backend.infrastructure.db.models import (
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


@lru_cache(maxsize=1)
def get_broker() -> RedisBroker:
    """初始化 Dramatiq Redis Broker（单例）。"""
    settings = get_settings()
    broker = RedisBroker(url=settings.redis_url)
    dramatiq.set_broker(broker)
    return broker


def enqueue_workflow_run(run_id: str) -> None:
    from productflow_backend.workers import run_product_workflow_run

    get_broker()
    run_product_workflow_run.send(run_id)


def enqueue_image_session_generation_task(task_id: str) -> None:
    from productflow_backend.workers import run_image_session_generation_task

    get_broker()
    run_image_session_generation_task.send(task_id)


def enqueue_image_session_generation_task_later(task_id: str, *, delay_ms: int) -> None:
    from productflow_backend.workers import run_image_session_generation_task

    get_broker()
    run_image_session_generation_task.send_with_options(args=(task_id,), delay=delay_ms)


def recover_unfinished_workflow_runs(
    *,
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
                .where(WorkflowRun.status == WorkflowRunStatus.RUNNING)
            ).all()
        )
        for run in runs:
            running_node_runs = [
                node_run for node_run in run.node_runs if node_run.status == WorkflowNodeStatus.RUNNING
            ]
            if running_node_runs:
                stale_node_runs = [
                    node_run
                    for node_run in running_node_runs
                    if node_run.started_at is not None and _as_aware_utc(node_run.started_at) <= cutoff
                ]
                if not reset_stale_running or len(stale_node_runs) != len(running_node_runs):
                    continue
                for node_run in stale_node_runs:
                    node_run.status = WorkflowNodeStatus.QUEUED
                    node = session.get(WorkflowNode, node_run.node_id)
                    if node is not None and node.status == WorkflowNodeStatus.RUNNING:
                        node.status = WorkflowNodeStatus.QUEUED
                        node.failure_reason = None
                run.failure_reason = None
                stale_running_runs += 1
                runs_to_enqueue.append(run.id)
                continue

            if any(node_run.status == WorkflowNodeStatus.QUEUED for node_run in run.node_runs) or (
                run.node_runs
                and all(node_run.status == WorkflowNodeStatus.SUCCEEDED for node_run in run.node_runs)
            ):
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
            enqueue_workflow_run(run_id)
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
                ImageSessionGenerationTask.status == JobStatus.QUEUED,
                (
                    (ImageSessionGenerationTask.status == JobStatus.RUNNING)
                    & (last_progress_at <= cutoff)
                )
                if reset_stale_running
                else False,
            ),
        )
        tasks = list(session.scalars(statement).all())
        for task in tasks:
            if task.status == JobStatus.RUNNING:
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
            enqueue_image_session_generation_task(task_id)
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
