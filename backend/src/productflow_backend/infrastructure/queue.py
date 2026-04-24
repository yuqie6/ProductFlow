from __future__ import annotations

import logging
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta
from functools import lru_cache

import dramatiq
from dramatiq.brokers.redis import RedisBroker
from sqlalchemy import or_, select
from sqlalchemy.orm import selectinload

from productflow_backend.config import get_settings
from productflow_backend.domain.enums import JobKind, JobStatus, WorkflowNodeStatus, WorkflowRunStatus
from productflow_backend.infrastructure.db.models import JobRun, WorkflowNode, WorkflowRun, utcnow
from productflow_backend.infrastructure.db.session import get_session_factory

logger = logging.getLogger(__name__)

DEFAULT_STALE_RUNNING_AFTER = timedelta(minutes=30)


def _as_aware_utc(value: datetime) -> datetime:
    if value.tzinfo is None:
        return value.replace(tzinfo=UTC)
    return value


@dataclass(frozen=True, slots=True)
class JobRecoverySummary:
    """启动恢复结果：把数据库里还没结束、但 Redis 里可能已丢消息的任务补回队列。"""

    queued_jobs: int = 0
    stale_running_jobs: int = 0
    enqueued_jobs: int = 0


@dataclass(frozen=True, slots=True)
class WorkflowRunRecoverySummary:
    """启动恢复结果：把数据库里仍处于 active 的工作流运行补回队列。"""

    queued_runs: int = 0
    stale_running_runs: int = 0
    enqueued_runs: int = 0


@lru_cache(maxsize=1)
def get_broker() -> RedisBroker:
    """初始化 Dramatiq Redis Broker（单例）。"""
    settings = get_settings()
    broker = RedisBroker(url=settings.redis_url)
    dramatiq.set_broker(broker)
    return broker


def enqueue_copy_job(job_id: str) -> None:
    from productflow_backend.workers import run_copy_generation

    get_broker()
    run_copy_generation.send(job_id)


def enqueue_poster_job(job_id: str) -> None:
    from productflow_backend.workers import run_poster_generation

    get_broker()
    run_poster_generation.send(job_id)


def enqueue_workflow_run(run_id: str) -> None:
    from productflow_backend.workers import run_product_workflow_run

    get_broker()
    run_product_workflow_run.send(run_id)


def _send_job_to_queue(job_id: str, kind: JobKind) -> None:
    if kind == JobKind.COPY_GENERATION:
        enqueue_copy_job(job_id)
        return
    if kind == JobKind.POSTER_GENERATION:
        enqueue_poster_job(job_id)
        return
    raise ValueError(f"未知任务类型: {kind}")


def recover_unfinished_jobs(
    *,
    reset_stale_running: bool = False,
    stale_running_after: timedelta = DEFAULT_STALE_RUNNING_AFTER,
) -> JobRecoverySummary:
    """恢复重启期间滞留的异步任务。

    任务创建是“先写数据库、再发 Redis 消息”。如果后端在这两个动作之间重启，
    数据库会留下 queued 任务，但 Redis 里没有可消费消息；如果 worker 在执行中
    被重启，数据库会留下 running 任务，也不会再被 Dramatiq 自动消费。

    恢复策略保持幂等：
    - queued 任务直接补发到队列；
    - running 任务只在 worker 启动且超过宽限时间后重置为 queued 再补发；
    - 重复补发是安全的，真正执行前 `_mark_job_running` 会拒绝非 queued 任务。
    """

    cutoff = utcnow() - stale_running_after
    session = get_session_factory()()
    jobs_to_enqueue: list[tuple[str, JobKind]] = []
    queued_jobs = 0
    stale_running_jobs = 0

    try:
        statement = select(JobRun).where(
            JobRun.is_retryable.is_(True),
            or_(
                JobRun.status == JobStatus.QUEUED,
                (JobRun.status == JobStatus.RUNNING) & (JobRun.started_at <= cutoff)
                if reset_stale_running
                else False,
            ),
        )
        jobs = list(session.scalars(statement).all())
        for job in jobs:
            if job.status == JobStatus.RUNNING:
                job.status = JobStatus.QUEUED
                job.started_at = None
                stale_running_jobs += 1
            else:
                queued_jobs += 1
            jobs_to_enqueue.append((job.id, job.kind))
        if stale_running_jobs:
            session.commit()
    except Exception:
        session.rollback()
        logger.exception("恢复滞留任务时读取数据库失败")
        return JobRecoverySummary()
    finally:
        session.close()

    enqueued_jobs = 0
    for job_id, kind in jobs_to_enqueue:
        try:
            _send_job_to_queue(job_id, kind)
            enqueued_jobs += 1
        except Exception:
            logger.exception("恢复滞留任务入队失败: job_id=%s kind=%s", job_id, kind)

    if jobs_to_enqueue:
        logger.info(
            "已恢复滞留任务: queued=%s stale_running=%s enqueued=%s",
            queued_jobs,
            stale_running_jobs,
            enqueued_jobs,
        )
    return JobRecoverySummary(
        queued_jobs=queued_jobs,
        stale_running_jobs=stale_running_jobs,
        enqueued_jobs=enqueued_jobs,
    )


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
