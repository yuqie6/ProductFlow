from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime
from enum import StrEnum

from sqlalchemy import func, select
from sqlalchemy.orm import Session, selectinload

from productflow_backend.domain.durable_generation_tasks import (
    GRAPH_RUN_GENERATION_TASK_CONTRACT,
    IMAGE_SESSION_GENERATION_TASK_CONTRACT,
    WorkflowRunDeliveryState,
    classify_workflow_run_delivery,
)
from productflow_backend.infrastructure.db.models import (
    ImageSessionGenerationTask,
    WorkflowGraphNodeRun,
    WorkflowGraphRun,
)
from productflow_backend.infrastructure.runtime_settings import get_runtime_settings

GENERATION_CAPACITY_LOCK_KEY = 42630001


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


def _graph_run_delivery_state(run: WorkflowGraphRun) -> WorkflowRunDeliveryState:
    return classify_workflow_run_delivery(run.status, [node_run.status for node_run in run.node_runs])


def _active_async_task_count(session: Session) -> int:
    active_graph_runs = session.scalar(
        select(func.count())
        .select_from(WorkflowGraphRun)
        .where(WorkflowGraphRun.status.in_(GRAPH_RUN_GENERATION_TASK_CONTRACT.active_statuses))
    )
    active_image_session_tasks = session.scalar(
        select(func.count())
        .select_from(ImageSessionGenerationTask)
        .where(ImageSessionGenerationTask.status.in_(IMAGE_SESSION_GENERATION_TASK_CONTRACT.active_statuses))
    )
    return int(active_graph_runs or 0) + int(active_image_session_tasks or 0)


def _running_async_task_count(session: Session) -> int:
    running_graph_node_runs = session.scalar(
        select(func.count())
        .select_from(WorkflowGraphNodeRun)
        .join(WorkflowGraphRun, WorkflowGraphRun.id == WorkflowGraphNodeRun.graph_run_id)
        .where(
            WorkflowGraphRun.status.in_(GRAPH_RUN_GENERATION_TASK_CONTRACT.active_statuses),
            WorkflowGraphNodeRun.status.in_(GRAPH_RUN_GENERATION_TASK_CONTRACT.execution_running_statuses),
        )
    )
    running_image_session_tasks = session.scalar(
        select(func.count())
        .select_from(ImageSessionGenerationTask)
        .where(ImageSessionGenerationTask.status.in_(IMAGE_SESSION_GENERATION_TASK_CONTRACT.running_statuses))
    )
    return int(running_graph_node_runs or 0) + int(running_image_session_tasks or 0)


def _status_count(session: Session, model: type, statuses: tuple[StrEnum, ...]) -> int:
    count = session.scalar(select(func.count()).select_from(model).where(model.status.in_(statuses)))
    return int(count or 0)


def _graph_run_queue_status_counts(session: Session) -> tuple[int, int]:
    runs = session.scalars(
        select(WorkflowGraphRun)
        .options(selectinload(WorkflowGraphRun.node_runs))
        .where(WorkflowGraphRun.status.in_(GRAPH_RUN_GENERATION_TASK_CONTRACT.active_statuses))
    ).all()
    running_count = 0
    queued_count = 0
    for run in runs:
        delivery_state = _graph_run_delivery_state(run)
        if delivery_state == WorkflowRunDeliveryState.RUNNING:
            running_count += 1
        elif delivery_state == WorkflowRunDeliveryState.QUEUED:
            queued_count += 1
    return running_count, queued_count


def get_generation_queue_overview(session: Session) -> GenerationQueueOverview:
    """返回全局耐久生成队列快照。"""

    graph_running_count, graph_queued_count = _graph_run_queue_status_counts(session)
    running_count = graph_running_count + _status_count(
        session,
        ImageSessionGenerationTask,
        IMAGE_SESSION_GENERATION_TASK_CONTRACT.running_statuses,
    )
    queued_count = graph_queued_count + _status_count(
        session,
        ImageSessionGenerationTask,
        IMAGE_SESSION_GENERATION_TASK_CONTRACT.queued_statuses,
    )
    return GenerationQueueOverview(
        active_count=running_count + queued_count,
        running_count=running_count,
        queued_count=queued_count,
        max_concurrent_tasks=get_runtime_settings(session).generation_max_concurrent_tasks,
    )


def get_queued_generation_positions(session: Session) -> dict[str, int]:
    queued_items: list[tuple[datetime, str, str]] = []
    graph_run_queued_at: dict[str, datetime] = {}
    for run in session.scalars(
        select(WorkflowGraphRun)
        .options(selectinload(WorkflowGraphRun.node_runs))
        .where(WorkflowGraphRun.status.in_(GRAPH_RUN_GENERATION_TASK_CONTRACT.active_statuses))
    ):
        if _graph_run_delivery_state(run) != WorkflowRunDeliveryState.QUEUED:
            continue
        graph_run_queued_at[run.id] = min(
            (
                node_run.started_at
                for node_run in run.node_runs
                if GRAPH_RUN_GENERATION_TASK_CONTRACT.execution_is_queued(node_run.status)
            ),
            default=run.started_at,
        )
    queued_items.extend((started_at, "workflow_graph", run_id) for run_id, started_at in graph_run_queued_at.items())
    queued_items.extend(
        (task.created_at, "image_session", task.id)
        for task in session.scalars(
            select(ImageSessionGenerationTask).where(
                ImageSessionGenerationTask.status.in_(IMAGE_SESSION_GENERATION_TASK_CONTRACT.queued_statuses)
            )
        ).all()
    )
    queued_items.sort(key=lambda item: (item[0], item[1], item[2]))
    return {item_id: index + 1 for index, (_created_at, _kind, item_id) in enumerate(queued_items)}


def get_workflow_run_queue_metadata(
    session: Session,
    run: WorkflowGraphRun,
    *,
    overview: GenerationQueueOverview | None = None,
    queued_positions: dict[str, int] | None = None,
) -> GenerationTaskQueueMetadata:
    overview = overview or get_generation_queue_overview(session)
    queued_ahead_count: int | None = None
    queue_position: int | None = None
    if _graph_run_delivery_state(run) == WorkflowRunDeliveryState.QUEUED:
        positions = queued_positions or get_queued_generation_positions(session)
        queue_position = positions.get(run.id)
        if queue_position is not None:
            queued_ahead_count = max(0, queue_position - 1)
    return GenerationTaskQueueMetadata(
        overview=overview,
        queued_ahead_count=queued_ahead_count,
        queue_position=queue_position,
    )


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
    if IMAGE_SESSION_GENERATION_TASK_CONTRACT.is_queued(task.status):
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
    """从耐久 DB 行统计全局正在进行的 provider/worker 工作。"""

    return _active_async_task_count(session)


def _lock_generation_capacity(session: Session) -> None:
    bind = session.get_bind()
    if bind.dialect.name == "postgresql":
        session.execute(select(func.pg_advisory_xact_lock(GENERATION_CAPACITY_LOCK_KEY)))


def generation_running_capacity_available(session: Session) -> bool:
    """串行化 worker claim 检查，判断是否还能再进一条任务到 provider 执行。"""

    _lock_generation_capacity(session)
    limit = get_runtime_settings(session).generation_max_concurrent_tasks
    return _running_async_task_count(session) < limit


def ensure_generation_capacity(session: Session) -> None:
    """锁定容量账本，不拒绝已耐久入队的提交。

    `generation_max_concurrent_tasks` 限制 worker 进入 provider/running 执行；耐久任务入队不受此限制。
    该入口保持 no-op 兼容垫片，避免调用方把队列深度变成提交时的 429。
    """

    _lock_generation_capacity(session)
