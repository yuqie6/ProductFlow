"""schema-v3 WorkflowGraphRun durable state, fencing, and recovery.

The graph provider-effect ledger remains the owner of provider-effect facts.
This module owns the graph-run state machine that decides whether a node can
be claimed, failed, marked unknown, or safely returned to the queue.
"""

from __future__ import annotations

import logging
import uuid
from collections.abc import Callable
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta

from sqlalchemy import select, update
from sqlalchemy.orm import Session, selectinload

from productflow_backend.application.async_delivery import (
    delivery_key_for_actor,
    stage_async_dispatch,
)
from productflow_backend.application.product_workflow.graph_provider_effects import (
    load_node_run_effect,
    reset_graph_node_run_for_safe_requeue,
)
from productflow_backend.application.product_workflow.graph_provider_effects import (
    mark_graph_run_provider_unknown as _mark_graph_run_provider_unknown,
)
from productflow_backend.application.product_workflow.graph_provider_effects import (
    node_run_effect_is_safe_to_requeue as _node_run_effect_is_safe_to_requeue,
)
from productflow_backend.application.queue_submission import enqueue_or_mark_failed
from productflow_backend.application.time import now_utc
from productflow_backend.domain.durable_generation_tasks import (
    GRAPH_RUN_GENERATION_TASK_CONTRACT,
    WORKFLOW_PROVIDER_EFFECT_CALL_PHASE,
    WORKFLOW_PROVIDER_EFFECT_RESULT_PHASE,
    WORKFLOW_PROVIDER_EFFECT_UNKNOWN_DETAIL,
    WorkflowRunDeliveryState,
    classify_workflow_run_delivery,
)
from productflow_backend.domain.enums import WorkflowNodeStatus, WorkflowRunStatus
from productflow_backend.infrastructure.db.models import (
    AsyncDispatch,
    WorkflowGraphNodeRun,
    WorkflowGraphRun,
    utcnow,
)
from productflow_backend.infrastructure.db.session import get_session_factory

logger = logging.getLogger(__name__)

DEFAULT_STALE_RUNNING_AFTER = timedelta(minutes=30)


@dataclass(frozen=True, slots=True)
class WorkflowRunRecoverySummary:
    """启动恢复结果：把数据库里仍处于 active 的工作流运行补回队列。"""

    queued_runs: int = 0
    stale_running_runs: int = 0
    enqueued_runs: int = 0
    unknown_runs: int = 0


def stage_graph_run_dispatch(session: Session, run_id: str) -> AsyncDispatch:
    """只 flush graph-run dispatch 行，不 commit；调用方拥有事务。"""

    return stage_async_dispatch(
        session,
        delivery_key=delivery_key_for_actor(GRAPH_RUN_GENERATION_TASK_CONTRACT.actor_name, run_id),
        actor_name=GRAPH_RUN_GENERATION_TASK_CONTRACT.actor_name,
        aggregate_id=run_id,
    )


def enqueue_graph_run_after_commit(
    session: Session,
    run_id: str,
    *,
    enqueue: Callable[[str], None],
) -> None:
    """Enqueue an already-committed run and persist a broker failure."""

    enqueue_or_mark_failed(
        run_id,
        enqueue=enqueue,
        mark_failed=lambda failed_run_id, reason: mark_graph_run_enqueue_failed(
            session,
            run_id=failed_run_id,
            reason=reason,
        ),
    )


def is_graph_node_run_safe_to_requeue(
    node_run: WorkflowGraphNodeRun,
    effect,
) -> bool:
    """Return whether a stale node can return to queued without replaying a provider call."""

    return (
        node_run.status == WorkflowNodeStatus.RUNNING
        and _node_run_effect_is_safe_to_requeue(node_run, effect)
    )


def requeue_graph_node_run(
    session: Session,
    node_run: WorkflowGraphNodeRun,
    *,
    effect=None,
) -> bool:
    """Safely return one stale pre-provider node to queued; only flushes."""

    resolved_effect = effect if effect is not None else load_node_run_effect(session, node_run.id)
    if not is_graph_node_run_safe_to_requeue(node_run, resolved_effect):
        return False
    reset_graph_node_run_for_safe_requeue(session, node_run)
    return True


def claim_queued_node_run(
    session: Session,
    node_run: WorkflowGraphNodeRun,
    *,
    notify: Callable[[str, WorkflowGraphNodeRun], None] | None = None,
) -> bool:
    """Atomically claim a queued node, then commit before provider work begins."""

    now = now_utc()
    attempt_id = str(uuid.uuid4())
    result = session.execute(
        update(WorkflowGraphNodeRun)
        .where(
            WorkflowGraphNodeRun.id == node_run.id,
            WorkflowGraphNodeRun.status == WorkflowNodeStatus.QUEUED,
        )
        .values(
            status=WorkflowNodeStatus.RUNNING,
            active_attempt_id=attempt_id,
            progress_phase="claimed",
            progress_updated_at=now,
            started_at=now,
            failure_reason=None,
        )
    )
    session.commit()
    session.refresh(node_run)
    claimed = result.rowcount == 1
    if claimed and notify is not None:
        notify("claimed", node_run)
    return claimed


def mark_graph_run_unknown(
    session: Session,
    *,
    run_id: str,
    node_run_id: str,
    attempt_id: str | None,
    detail: str = WORKFLOW_PROVIDER_EFFECT_UNKNOWN_DETAIL,
) -> bool:
    """Stage an unknown graph-run transition; the caller owns commit."""

    return _mark_graph_run_provider_unknown(
        session,
        run_id=run_id,
        node_run_id=node_run_id,
        attempt_id=attempt_id,
        detail=detail,
    )


def mark_graph_run_failed(
    session: Session,
    *,
    run_id: str,
    reason: str,
) -> bool:
    """Mark a still-active run and its unfinished nodes failed, then commit."""

    run = _locked_graph_run(session, run_id)
    if run is None or run.status != WorkflowRunStatus.RUNNING:
        session.rollback()
        return False
    _mark_graph_run_failed_locked(session, run, reason=reason)
    session.commit()
    return True


def mark_graph_run_enqueue_failed(session: Session, *, run_id: str, reason: str) -> None:
    """Persist a direct broker failure without overwriting a provider unknown."""

    run = _locked_graph_run(session, run_id)
    if run is None or run.status != WorkflowRunStatus.RUNNING:
        session.rollback()
        return
    provider_node = _active_provider_boundary_node(run)
    if provider_node is not None:
        mark_graph_run_unknown(
            session,
            run_id=run.id,
            node_run_id=provider_node.id,
            attempt_id=provider_node.active_attempt_id,
        )
        session.commit()
        return
    _mark_graph_run_failed_locked(session, run, reason=reason)
    session.commit()


def fail_claimed_node(
    session: Session,
    *,
    run_id: str,
    node_run_id: str,
    reason: str,
) -> None:
    """Fail a node/run unless its provider boundary makes the outcome unknown."""

    session.rollback()
    run = _locked_graph_run(session, run_id)
    if run is None:
        session.rollback()
        return
    node_run = session.scalar(
        select(WorkflowGraphNodeRun)
        .where(
            WorkflowGraphNodeRun.id == node_run_id,
            WorkflowGraphNodeRun.graph_run_id == run_id,
        )
        .with_for_update()
    )
    if node_run is not None and _node_run_is_past_provider_boundary(node_run):
        mark_graph_run_unknown(
            session,
            run_id=run_id,
            node_run_id=node_run_id,
            attempt_id=node_run.active_attempt_id,
        )
        session.commit()
        return
    _mark_graph_run_failed_locked(session, run, reason=reason)
    session.commit()


def fail_graph_run(session: Session, *, run_id: str, reason: str) -> None:
    """Fail an active run while preserving unknown provider-effect semantics."""

    session.rollback()
    run = _locked_graph_run(session, run_id)
    if run is None:
        session.rollback()
        return
    provider_node = _active_provider_boundary_node(run)
    if provider_node is not None:
        mark_graph_run_unknown(
            session,
            run_id=run.id,
            node_run_id=provider_node.id,
            attempt_id=provider_node.active_attempt_id,
        )
        session.commit()
        return
    _mark_graph_run_failed_locked(session, run, reason=reason)
    session.commit()


def recover_unfinished_graph_runs(
    *,
    enqueue: Callable[[str], None] | None = None,
    stage_dispatch: Callable[[Session, str], None] | None = None,
    reset_stale_running: bool = False,
    stale_running_after: timedelta = DEFAULT_STALE_RUNNING_AFTER,
) -> WorkflowRunRecoverySummary:
    """Recover queued or stale schema-v3 graph runs through this state machine."""

    if stage_dispatch is None and enqueue is None:
        raise ValueError("enqueue or stage_dispatch is required")

    cutoff = utcnow() - stale_running_after
    session = get_session_factory()()
    runs_to_enqueue: list[str] = []
    queued_runs = 0
    stale_running_runs = 0
    unknown_runs = 0

    try:
        runs = list(
            session.scalars(
                select(WorkflowGraphRun)
                .options(selectinload(WorkflowGraphRun.node_runs))
                .where(WorkflowGraphRun.status.in_(GRAPH_RUN_GENERATION_TASK_CONTRACT.active_statuses))
            ).all()
        )
        for run in runs:
            run_id = run.id
            delivery_state = classify_workflow_run_delivery(
                run.status,
                [node_run.status for node_run in run.node_runs],
            )
            if delivery_state == WorkflowRunDeliveryState.QUEUED:
                if stage_dispatch is None:
                    queued_runs += 1
                    runs_to_enqueue.append(run_id)
                    continue
                locked_run = _locked_graph_run(session, run_id)
                if locked_run is None:
                    session.rollback()
                    continue
                locked_delivery_state = classify_workflow_run_delivery(
                    locked_run.status,
                    [node_run.status for node_run in locked_run.node_runs],
                )
                if locked_delivery_state != WorkflowRunDeliveryState.QUEUED:
                    session.rollback()
                    continue
                stage_dispatch(session, run_id)
                session.commit()
                queued_runs += 1
                runs_to_enqueue.append(run_id)
                continue

            if delivery_state != WorkflowRunDeliveryState.RUNNING:
                continue
            running_node_runs = [
                node_run
                for node_run in run.node_runs
                if GRAPH_RUN_GENERATION_TASK_CONTRACT.execution_is_running(node_run.status)
            ]
            stale_node_runs = [
                node_run
                for node_run in running_node_runs
                if _graph_node_heartbeat(node_run) <= cutoff
            ]
            if not reset_stale_running or not stale_node_runs:
                continue

            locked_run = _locked_graph_run(session, run_id)
            if locked_run is None or GRAPH_RUN_GENERATION_TASK_CONTRACT.is_terminal(locked_run.status):
                session.rollback()
                continue
            locked_stale_node_runs = [
                node_run
                for node_run in locked_run.node_runs
                if GRAPH_RUN_GENERATION_TASK_CONTRACT.execution_is_running(node_run.status)
                and _graph_node_heartbeat(node_run) <= cutoff
            ]
            if not locked_stale_node_runs:
                session.rollback()
                continue

            marked_unknown = False
            safe_requeued = False
            for stale_node_run in sorted(locked_stale_node_runs, key=lambda item: (item.node_id or "", item.id)):
                if requeue_graph_node_run(session, stale_node_run):
                    safe_requeued = True
                    continue
                mark_graph_run_unknown(
                    session,
                    run_id=locked_run.id,
                    node_run_id=stale_node_run.id,
                    attempt_id=stale_node_run.active_attempt_id,
                )
                marked_unknown = True
                break
            if marked_unknown:
                session.commit()
                unknown_runs += 1
                continue

            locked_run.failure_reason = None
            if stage_dispatch is not None:
                stage_dispatch(session, run_id)
            session.commit()
            if safe_requeued:
                stale_running_runs += 1
                runs_to_enqueue.append(run_id)
    except Exception:
        session.rollback()
        logger.exception("恢复滞留工作流运行时读取数据库失败")
        raise
    finally:
        session.close()

    enqueued_runs = len(runs_to_enqueue) if stage_dispatch is not None else 0
    if stage_dispatch is None:
        assert enqueue is not None
        for run_id in runs_to_enqueue:
            try:
                recovery_session = get_session_factory()()
                try:
                    enqueue_graph_run_after_commit(recovery_session, run_id, enqueue=enqueue)
                finally:
                    recovery_session.close()
                enqueued_runs += 1
            except Exception:
                logger.exception("恢复滞留工作流运行入队失败: workflow_run_id=%s", run_id)

    if runs_to_enqueue or unknown_runs:
        logger.info(
            "已恢复滞留工作流运行: queued=%s stale_running=%s unknown=%s enqueued=%s",
            queued_runs,
            stale_running_runs,
            unknown_runs,
            enqueued_runs,
        )
    return WorkflowRunRecoverySummary(
        queued_runs=queued_runs,
        stale_running_runs=stale_running_runs,
        enqueued_runs=enqueued_runs,
        unknown_runs=unknown_runs,
    )


def _locked_graph_run(session: Session, run_id: str) -> WorkflowGraphRun | None:
    return session.scalar(
        select(WorkflowGraphRun)
        .options(selectinload(WorkflowGraphRun.node_runs))
        .where(WorkflowGraphRun.id == run_id)
        .with_for_update()
    )


def _mark_graph_run_failed_locked(session: Session, run: WorkflowGraphRun, *, reason: str) -> None:
    now = now_utc()
    normalized_reason = reason[:1000]
    for node_run in run.node_runs:
        if node_run.status not in {WorkflowNodeStatus.QUEUED, WorkflowNodeStatus.RUNNING}:
            continue
        node_run.status = WorkflowNodeStatus.FAILED
        node_run.failure_reason = normalized_reason
        node_run.finished_at = now
        node_run.active_attempt_id = None
        node_run.progress_updated_at = now
    run.status = WorkflowRunStatus.FAILED
    run.failure_reason = normalized_reason
    run.finished_at = now
    run.is_retryable = True
    session.flush()


def _active_provider_boundary_node(run: WorkflowGraphRun) -> WorkflowGraphNodeRun | None:
    for node_run in run.node_runs:
        if (
            node_run.status == WorkflowNodeStatus.RUNNING
            and _node_run_is_past_provider_boundary(node_run)
        ):
            return node_run
    return None


def _node_run_is_past_provider_boundary(node_run: WorkflowGraphNodeRun) -> bool:
    return node_run.progress_phase in {
        WORKFLOW_PROVIDER_EFFECT_CALL_PHASE,
        WORKFLOW_PROVIDER_EFFECT_RESULT_PHASE,
    }


def _as_aware_utc(value: datetime) -> datetime:
    if value.tzinfo is None:
        return value.replace(tzinfo=UTC)
    return value


def _graph_node_heartbeat(node_run: WorkflowGraphNodeRun) -> datetime:
    stamp = node_run.progress_updated_at or node_run.started_at
    return _as_aware_utc(stamp)


__all__ = [
    "DEFAULT_STALE_RUNNING_AFTER",
    "WorkflowRunRecoverySummary",
    "claim_queued_node_run",
    "enqueue_graph_run_after_commit",
    "fail_claimed_node",
    "fail_graph_run",
    "is_graph_node_run_safe_to_requeue",
    "mark_graph_run_enqueue_failed",
    "mark_graph_run_failed",
    "mark_graph_run_unknown",
    "recover_unfinished_graph_runs",
    "requeue_graph_node_run",
    "stage_graph_run_dispatch",
]
