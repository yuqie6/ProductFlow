from __future__ import annotations

import logging
from dataclasses import dataclass
from typing import Any, cast

from dramatiq.middleware.time_limit import TimeLimitExceeded
from sqlalchemy import update
from sqlalchemy.engine import CursorResult
from sqlalchemy.orm import Session

from productflow_backend.application.admission import generation_running_capacity_available
from productflow_backend.application.time import now_utc
from productflow_backend.domain.durable_generation_tasks import WORKFLOW_RUN_GENERATION_TASK_CONTRACT
from productflow_backend.domain.enums import WorkflowNodeStatus, WorkflowRunStatus
from productflow_backend.infrastructure.db.models import WorkflowNode, WorkflowNodeRun, WorkflowRun
from productflow_backend.infrastructure.queue import enqueue_workflow_node_run_later, enqueue_workflow_run_later

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


def claim_workflow_node_run(session: Session, *, node_run_id: str, node_id: str) -> WorkflowNodeRunClaimResult:
    """Atomically claim one queued node run so duplicate Dramatiq messages do not execute it twice."""

    now = now_utc()
    if not generation_running_capacity_available(session):
        session.commit()
        return WorkflowNodeRunClaimResult(claimed=False, should_requeue=True)
    result = cast(
        CursorResult[Any],
        session.execute(
            update(WorkflowNodeRun)
            .where(
                WorkflowNodeRun.id == node_run_id,
                WorkflowNodeRun.status == WORKFLOW_RUN_GENERATION_TASK_CONTRACT.execution_queued_statuses[0],
            )
            .values(status=WORKFLOW_RUN_GENERATION_TASK_CONTRACT.execution_running_statuses[0], started_at=now)
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
    return WorkflowNodeRunClaimResult(claimed=True)


def requeue_workflow_run_after_capacity_wait(run_id: str) -> None:
    try:
        enqueue_workflow_run_later(run_id, delay_ms=PRODUCT_WORKFLOW_CAPACITY_RETRY_DELAY_MS)
    except Exception:  # noqa: BLE001
        logger.exception("商品工作流等待并发容量后重新入队失败: workflow_run_id=%s", run_id)


def requeue_workflow_node_run_after_capacity_wait(node_run_id: str) -> None:
    try:
        enqueue_workflow_node_run_later(node_run_id, delay_ms=PRODUCT_WORKFLOW_CAPACITY_RETRY_DELAY_MS)
    except Exception:  # noqa: BLE001
        logger.exception("商品工作流节点等待并发容量后重新入队失败: workflow_node_run_id=%s", node_run_id)


def mark_workflow_node_run_failed(
    session: Session,
    *,
    node_run_id: str,
    reason: str,
    is_retryable: bool = True,
    retry_hint: str | None = None,
    failure_category: str | None = None,
) -> str | None:
    node_run = session.get(WorkflowNodeRun, node_run_id)
    if node_run is None:
        return None
    run = node_run.workflow_run
    if WORKFLOW_RUN_GENERATION_TASK_CONTRACT.is_terminal(run.status):
        return None
    now = now_utc()
    node = session.get(WorkflowNode, node_run.node_id)
    if node is not None:
        node.status = WorkflowNodeStatus.FAILED
        node.failure_reason = reason
        node.last_run_at = now
    node_run.status = WorkflowNodeStatus.FAILED
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
    run.workflow.updated_at = now
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
    persisted_run = session.get(WorkflowRun, run_id)
    if persisted_run is None:
        return
    if WORKFLOW_RUN_GENERATION_TASK_CONTRACT.is_terminal(persisted_run.status):
        return
    now = now_utc()
    if failed_node_id is not None:
        failed_node = session.get(WorkflowNode, failed_node_id)
        if failed_node is not None:
            failed_node.status = WorkflowNodeStatus.FAILED
            failed_node.failure_reason = reason
            failed_node.last_run_at = now
    for node_run in persisted_run.node_runs:
        if node_run.node_id == failed_node_id:
            node_run.status = WorkflowNodeStatus.FAILED
            node_run.failure_reason = reason
            node_run.finished_at = now
        elif failed_node_id is None and WORKFLOW_RUN_GENERATION_TASK_CONTRACT.execution_is_running(node_run.status):
            failed_node = session.get(WorkflowNode, node_run.node_id)
            if failed_node is not None:
                failed_node.status = WorkflowNodeStatus.FAILED
                failed_node.failure_reason = reason
                failed_node.last_run_at = now
            node_run.status = WorkflowNodeStatus.FAILED
            node_run.failure_reason = reason
            node_run.finished_at = now
        elif WORKFLOW_RUN_GENERATION_TASK_CONTRACT.execution_is_queued(node_run.status):
            skipped_node = session.get(WorkflowNode, node_run.node_id)
            if skipped_node is not None:
                skipped_node.status = WorkflowNodeStatus.IDLE
                skipped_node.failure_reason = None
            node_run.status = WorkflowNodeStatus.FAILED
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
    persisted_run.workflow.updated_at = now
    session.commit()


def mark_workflow_run_cancelled(session: Session, *, run_id: str) -> None:
    persisted_run = session.get(WorkflowRun, run_id)
    if persisted_run is None:
        return
    if WORKFLOW_RUN_GENERATION_TASK_CONTRACT.is_terminal(persisted_run.status):
        return
    now = now_utc()
    for node_run in persisted_run.node_runs:
        if WORKFLOW_RUN_GENERATION_TASK_CONTRACT.execution_is_queued(node_run.status):
            skipped_node = session.get(WorkflowNode, node_run.node_id)
            if skipped_node is not None:
                skipped_node.status = WorkflowNodeStatus.IDLE
                skipped_node.failure_reason = None
            node_run.status = WorkflowNodeStatus.FAILED
            node_run.failure_reason = WORKFLOW_CANCELLED_REASON
            node_run.finished_at = now
        elif WORKFLOW_RUN_GENERATION_TASK_CONTRACT.execution_is_running(node_run.status):
            running_node = session.get(WorkflowNode, node_run.node_id)
            if running_node is not None:
                running_node.status = WorkflowNodeStatus.FAILED
                running_node.failure_reason = WORKFLOW_CANCELLED_REASON
                running_node.last_run_at = now
            node_run.status = WorkflowNodeStatus.FAILED
            node_run.failure_reason = WORKFLOW_CANCELLED_REASON
            node_run.finished_at = now
    persisted_run.status = WorkflowRunStatus.CANCELLED
    persisted_run.failure_reason = WORKFLOW_CANCELLED_REASON
    persisted_run.finished_at = now
    persisted_run.workflow.updated_at = now
    session.commit()
