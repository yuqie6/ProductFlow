from __future__ import annotations

import logging
from dataclasses import dataclass
from typing import Any, cast

from dramatiq.middleware.time_limit import TimeLimitExceeded
from sqlalchemy import select, update
from sqlalchemy.engine import CursorResult
from sqlalchemy.orm import Session

from productflow_backend.application.admission import generation_running_capacity_available
from productflow_backend.application.async_delivery import (
    delivery_key_for_actor,
    enqueue_async_dispatch_for_actor,
    requeue_async_dispatch,
)
from productflow_backend.application.time import now_utc
from productflow_backend.domain.durable_generation_tasks import WORKFLOW_RUN_GENERATION_TASK_CONTRACT
from productflow_backend.domain.enums import WorkflowNodeStatus, WorkflowRunStatus
from productflow_backend.infrastructure.db.models import (
    ProductWorkflow,
    WorkflowNode,
    WorkflowNodeRun,
    WorkflowRun,
    new_id,
)
from productflow_backend.infrastructure.queue import WORKFLOW_NODE_RUN_ACTOR_NAME

logger = logging.getLogger(__name__)

WORKFLOW_WORKER_TIMEOUT_FAILURE = "工作流执行超时，请稍后重试"
WORKFLOW_CANCELLED_REASON = "已取消"
PRODUCT_WORKFLOW_CAPACITY_RETRY_DELAY_MS = 2000


def workflow_run_failure_progress_metadata(
    *,
    reason: str,
    retryable: bool,
    retry_hint: str | None = None,
    failure_category: str | None = None,
) -> dict[str, Any]:
    metadata = {
        "last_failure_reason": reason,
        "last_failure_retryable": retryable,
        "retry_hint": retry_hint or ("retry_later" if retryable else "revise_input"),
    }
    if failure_category:
        metadata["last_failure_category"] = failure_category
    return metadata


class WorkflowSafeExecutionError(RuntimeError):
    """Execution failure whose string is safe to persist and show to users."""

    def __init__(
        self,
        safe_message: str,
        *,
        retryable: bool = True,
        retry_hint: str | None = None,
        failure_category: str | None = None,
    ) -> None:
        super().__init__(safe_message)
        self.safe_message = safe_message
        self.retryable = retryable
        self.retry_hint = retry_hint
        self.failure_category = failure_category


@dataclass(frozen=True, slots=True)
class WorkflowNodeRunClaimResult:
    claimed: bool
    should_requeue: bool = False
    attempt_id: str | None = None


def safe_workflow_failure_reason(exc: BaseException) -> str:
    if isinstance(exc, TimeLimitExceeded):
        return WORKFLOW_WORKER_TIMEOUT_FAILURE
    if isinstance(exc, WorkflowSafeExecutionError):
        return exc.safe_message
    return str(exc)


def workflow_failure_retry_hint(exc: BaseException) -> str | None:
    value = getattr(exc, "retry_hint", None)
    return value if isinstance(value, str) else None


def workflow_failure_category(exc: BaseException) -> str | None:
    value = getattr(exc, "failure_category", None)
    return value if isinstance(value, str) else None


def workflow_run_failure_context(exc: BaseException) -> dict[str, Any]:
    return {
        "reason": safe_workflow_failure_reason(exc)[:1000],
        "is_retryable": getattr(exc, "retryable", True),
        "retry_hint": workflow_failure_retry_hint(exc),
        "failure_category": workflow_failure_category(exc),
    }


def workflow_node_failed_run_is_retryable(node: WorkflowNode, runs: list[WorkflowRun]) -> bool:
    if node.status != WorkflowNodeStatus.FAILED or node.failure_reason == WORKFLOW_CANCELLED_REASON:
        return False
    ordered_runs = sorted(runs, key=lambda item: (item.started_at, item.id), reverse=True)
    for run in ordered_runs:
        if run.status != WorkflowRunStatus.FAILED:
            continue
        if any(
            node_run.node_id == node.id and node_run.status == WorkflowNodeStatus.FAILED
            for node_run in run.node_runs
        ):
            return run.is_retryable
    return True


def lock_workflow_run_aggregate(
    session: Session,
    *,
    run_id: str,
) -> tuple[WorkflowRun | None, list[WorkflowNodeRun], list[WorkflowNode], ProductWorkflow | None]:
    """Lock a workflow run aggregate in deterministic member order.

    The order is `WorkflowNodeRun -> WorkflowRun -> WorkflowNode -> ProductWorkflow`, which matches the
    leaf-first order used by v2 result persistence. All run-level transitions (failure, cancellation, recovery)
    must use this helper so concurrent success/cancel/recovery paths cannot deadlock or overwrite a newer state.
    """
    node_runs = list(
        session.scalars(
            select(WorkflowNodeRun)
            .where(WorkflowNodeRun.workflow_run_id == run_id)
            .order_by(WorkflowNodeRun.id)
            .with_for_update()
            .execution_options(populate_existing=True)
        ).all()
    )
    run = session.scalar(
        select(WorkflowRun)
        .where(WorkflowRun.id == run_id)
        .with_for_update()
        .execution_options(populate_existing=True)
    )
    if run is None:
        return None, [], [], None
    node_ids = sorted({node_run.node_id for node_run in node_runs})
    nodes = (
        list(
            session.scalars(
                select(WorkflowNode)
                .where(WorkflowNode.id.in_(node_ids))
                .order_by(WorkflowNode.id)
                .with_for_update()
                .execution_options(populate_existing=True)
            ).all()
        )
        if node_ids
        else []
    )
    workflow = session.scalar(
        select(ProductWorkflow)
        .where(ProductWorkflow.id == run.workflow_id)
        .with_for_update()
        .execution_options(populate_existing=True)
    )
    return run, node_runs, nodes, workflow


def stage_workflow_run_dispatch(session: Session, *, run_id: str) -> None:
    """Stage the scheduler wake-up in the caller's transaction."""

    requeue_async_dispatch(
        session,
        delivery_key=delivery_key_for_actor(WORKFLOW_RUN_GENERATION_TASK_CONTRACT.actor_name, run_id),
        actor_name=WORKFLOW_RUN_GENERATION_TASK_CONTRACT.actor_name,
        aggregate_id=run_id,
    )


def claim_workflow_node_run(
    session: Session,
    *,
    node_run_id: str,
    node_id: str,
    attempt_id: str | None = None,
) -> WorkflowNodeRunClaimResult:
    """Atomically claim one queued node run so duplicate Dramatiq messages do not execute it twice."""

    now = now_utc()
    if not generation_running_capacity_available(session):
        session.commit()
        return WorkflowNodeRunClaimResult(claimed=False, should_requeue=True)
    resolved_attempt_id = attempt_id or new_id()
    result = cast(
        CursorResult[Any],
        session.execute(
            update(WorkflowNodeRun)
            .where(
                WorkflowNodeRun.id == node_run_id,
                WorkflowNodeRun.status == WORKFLOW_RUN_GENERATION_TASK_CONTRACT.execution_queued_statuses[0],
                WorkflowNodeRun.active_attempt_id.is_(None),
            )
            .values(
                status=WORKFLOW_RUN_GENERATION_TASK_CONTRACT.execution_running_statuses[0],
                attempts=WorkflowNodeRun.attempts + 1,
                active_attempt_id=resolved_attempt_id,
                failure_reason=None,
                started_at=now,
                finished_at=None,
            )
        ),
    )
    if result.rowcount != 1:
        session.rollback()
        return WorkflowNodeRunClaimResult(claimed=False)
    session.execute(
        update(WorkflowNode)
        .where(WorkflowNode.id == node_id)
        .values(status=WorkflowNodeStatus.RUNNING, failure_reason=None, last_run_at=now)
    )
    session.commit()
    return WorkflowNodeRunClaimResult(claimed=True, attempt_id=resolved_attempt_id)


def requeue_workflow_run_after_capacity_wait(run_id: str) -> None:
    try:
        enqueue_async_dispatch_for_actor(
            WORKFLOW_RUN_GENERATION_TASK_CONTRACT.actor_name,
            run_id,
            delay_ms=PRODUCT_WORKFLOW_CAPACITY_RETRY_DELAY_MS,
            allow_active_lease=True,
        )
    except Exception:  # noqa: BLE001
        logger.exception("商品工作流等待并发容量后重新入队失败: workflow_run_id=%s", run_id)


def requeue_workflow_node_run_after_capacity_wait(node_run_id: str) -> None:
    try:
        enqueue_async_dispatch_for_actor(
            WORKFLOW_NODE_RUN_ACTOR_NAME,
            node_run_id,
            delay_ms=PRODUCT_WORKFLOW_CAPACITY_RETRY_DELAY_MS,
            allow_active_lease=True,
        )
    except Exception:  # noqa: BLE001
        logger.exception("商品工作流节点等待并发容量后重新入队失败: workflow_node_run_id=%s", node_run_id)


def mark_workflow_node_run_failed(
    session: Session,
    *,
    node_run_id: str,
    attempt_id: str | None = None,
    reason: str,
    is_retryable: bool = True,
    retry_hint: str | None = None,
    failure_category: str | None = None,
    commit: bool = True,
) -> str | None:
    node_run = session.scalar(
        select(WorkflowNodeRun)
        .where(WorkflowNodeRun.id == node_run_id)
        .with_for_update()
        .execution_options(populate_existing=True)
    )
    if node_run is None:
        return None
    if attempt_id is None:
        if node_run.status != WorkflowNodeStatus.QUEUED or node_run.active_attempt_id is not None:
            session.rollback()
            return None
    elif node_run.status != WorkflowNodeStatus.RUNNING or node_run.active_attempt_id != attempt_id:
        session.rollback()
        return None
    run = session.scalar(
        select(WorkflowRun)
        .where(WorkflowRun.id == node_run.workflow_run_id)
        .with_for_update()
        .execution_options(populate_existing=True)
    )
    if run is None:
        session.rollback()
        return None
    if WORKFLOW_RUN_GENERATION_TASK_CONTRACT.is_terminal(run.status):
        session.rollback()
        return None
    now = now_utc()
    node = session.scalar(
        select(WorkflowNode)
        .where(WorkflowNode.id == node_run.node_id)
        .with_for_update()
        .execution_options(populate_existing=True)
    )
    if node is not None:
        node.status = WorkflowNodeStatus.FAILED
        node.failure_reason = reason
        node.last_run_at = now
    node_run.status = WorkflowNodeStatus.FAILED
    node_run.active_attempt_id = None
    node_run.failure_reason = reason
    node_run.finished_at = now
    current_metadata = run.progress_metadata if isinstance(run.progress_metadata, dict) else {}
    if current_metadata.get("last_failure_retryable") is not False or not is_retryable:
        run.progress_metadata = {
            **current_metadata,
            **workflow_run_failure_progress_metadata(
                reason=reason,
                retryable=is_retryable,
                retry_hint=retry_hint,
                failure_category=failure_category,
            ),
        }
    workflow = session.scalar(
        select(ProductWorkflow)
        .where(ProductWorkflow.id == run.workflow_id)
        .with_for_update()
        .execution_options(populate_existing=True)
    )
    if workflow is not None:
        workflow.updated_at = now
    if commit:
        session.commit()
    return run.id


def mark_workflow_run_failed(
    session: Session,
    *,
    run_id: str,
    failed_node_id: str | None,
    reason: str,
    is_retryable: bool = True,
    retry_hint: str | None = None,
    failure_category: str | None = None,
) -> None:
    persisted_run, node_runs, nodes, workflow = lock_workflow_run_aggregate(session, run_id=run_id)
    if persisted_run is None:
        session.rollback()
        return
    if WORKFLOW_RUN_GENERATION_TASK_CONTRACT.is_terminal(persisted_run.status):
        session.rollback()
        return
    nodes_by_id = {node.id: node for node in nodes}
    now = now_utc()
    if failed_node_id is not None:
        failed_node = nodes_by_id.get(failed_node_id)
        if failed_node is not None:
            failed_node.status = WorkflowNodeStatus.FAILED
            failed_node.failure_reason = reason
            failed_node.last_run_at = now
    for node_run in node_runs:
        if node_run.node_id == failed_node_id:
            node_run.status = WorkflowNodeStatus.FAILED
            node_run.active_attempt_id = None
            node_run.failure_reason = reason
            node_run.finished_at = now
        elif failed_node_id is None and WORKFLOW_RUN_GENERATION_TASK_CONTRACT.execution_is_running(node_run.status):
            failed_node = nodes_by_id.get(node_run.node_id)
            if failed_node is not None:
                failed_node.status = WorkflowNodeStatus.FAILED
                failed_node.failure_reason = reason
                failed_node.last_run_at = now
            node_run.status = WorkflowNodeStatus.FAILED
            node_run.active_attempt_id = None
            node_run.failure_reason = reason
            node_run.finished_at = now
        elif WORKFLOW_RUN_GENERATION_TASK_CONTRACT.execution_is_queued(node_run.status):
            skipped_node = nodes_by_id.get(node_run.node_id)
            if skipped_node is not None:
                skipped_node.status = WorkflowNodeStatus.IDLE
                skipped_node.failure_reason = None
            node_run.status = WorkflowNodeStatus.FAILED
            node_run.active_attempt_id = None
            node_run.failure_reason = "上游节点失败"
            node_run.finished_at = now
    logger.warning("工作流运行失败: run_id=%s failed_node_id=%s reason=%s", run_id, failed_node_id, reason)
    persisted_run.status = WorkflowRunStatus.FAILED
    persisted_run.failure_reason = reason
    persisted_run.is_retryable = is_retryable
    current_metadata = (
        persisted_run.progress_metadata if isinstance(persisted_run.progress_metadata, dict) else {}
    )
    persisted_run.progress_metadata = {
        **current_metadata,
        **workflow_run_failure_progress_metadata(
            reason=reason,
            retryable=is_retryable,
            retry_hint=retry_hint,
            failure_category=failure_category,
        ),
    }
    persisted_run.finished_at = now
    if workflow is not None:
        workflow.updated_at = now
    session.commit()


def mark_workflow_run_cancelled(session: Session, *, run_id: str) -> None:
    persisted_run, node_runs, nodes, workflow = lock_workflow_run_aggregate(session, run_id=run_id)
    if persisted_run is None:
        session.rollback()
        return
    if WORKFLOW_RUN_GENERATION_TASK_CONTRACT.is_terminal(persisted_run.status):
        session.rollback()
        return
    nodes_by_id = {node.id: node for node in nodes}
    now = now_utc()
    for node_run in node_runs:
        if WORKFLOW_RUN_GENERATION_TASK_CONTRACT.execution_is_queued(node_run.status):
            skipped_node = nodes_by_id.get(node_run.node_id)
            if skipped_node is not None:
                skipped_node.status = WorkflowNodeStatus.IDLE
                skipped_node.failure_reason = None
            node_run.status = WorkflowNodeStatus.FAILED
            node_run.active_attempt_id = None
            node_run.failure_reason = WORKFLOW_CANCELLED_REASON
            node_run.finished_at = now
        elif WORKFLOW_RUN_GENERATION_TASK_CONTRACT.execution_is_running(node_run.status):
            running_node = nodes_by_id.get(node_run.node_id)
            if running_node is not None:
                running_node.status = WorkflowNodeStatus.FAILED
                running_node.failure_reason = WORKFLOW_CANCELLED_REASON
                running_node.last_run_at = now
            node_run.status = WorkflowNodeStatus.FAILED
            node_run.active_attempt_id = None
            node_run.failure_reason = WORKFLOW_CANCELLED_REASON
            node_run.finished_at = now
    persisted_run.status = WorkflowRunStatus.CANCELLED
    persisted_run.failure_reason = WORKFLOW_CANCELLED_REASON
    persisted_run.finished_at = now
    if workflow is not None:
        workflow.updated_at = now
    session.commit()
