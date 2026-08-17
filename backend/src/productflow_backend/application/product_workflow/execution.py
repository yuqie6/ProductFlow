from __future__ import annotations

import logging
from collections.abc import Callable

from dramatiq.middleware.time_limit import TimeLimitExceeded
from sqlalchemy.orm import Session

from productflow_backend.application.async_delivery import delivery_key_for_actor, requeue_async_dispatch
from productflow_backend.application.product_workflow.run_state import (
    lock_workflow_run_aggregate,
    mark_workflow_node_run_failed,
    mark_workflow_run_failed,
    stage_workflow_run_dispatch,
    workflow_run_failure_context,
    workflow_run_failure_progress_metadata,
)
from productflow_backend.application.product_workflow.v2_execution import execute_v2_workflow_node_run
from productflow_backend.application.product_workflow_dependencies import WorkflowExecutionDependencies
from productflow_backend.application.time import now_utc
from productflow_backend.domain.durable_generation_tasks import (
    QUEUE_UNAVAILABLE_DETAIL,
    WORKFLOW_RUN_GENERATION_TASK_CONTRACT,
)
from productflow_backend.domain.enums import WorkflowNodeStatus, WorkflowNodeType, WorkflowRunStatus
from productflow_backend.domain.errors import ConflictError, NotFoundError
from productflow_backend.domain.workflow_rules import WorkflowRuleEdge, WorkflowRuleNode, ready_workflow_node_ids
from productflow_backend.infrastructure.db.models import ProductWorkflow, WorkflowNodeRun, WorkflowRun
from productflow_backend.infrastructure.db.session import get_session_factory
from productflow_backend.infrastructure.queue import (
    WORKFLOW_NODE_RUN_ACTOR_NAME,
    enqueue_workflow_node_run,  # noqa: F401  # kept for test monkeypatch compatibility
    enqueue_workflow_run,  # noqa: F401  # kept for test monkeypatch compatibility
)

logger = logging.getLogger(__name__)

V2_WORKFLOW_SCHEMA_VERSION = 2
V2_RUNNABLE_NODE_TYPES = {
    WorkflowNodeType.PROMPT_GENERATION,
    WorkflowNodeType.IMAGE_GENERATION,
}


def execute_product_workflow_run(run_id: str) -> None:
    """Schedule ready nodes for one schema-v2 workflow run."""

    session = get_session_factory()()
    try:
        try:
            _execute_product_workflow_run(session, run_id=run_id)
        except TimeLimitExceeded as exc:
            session.rollback()
            mark_workflow_run_failed(
                session,
                run_id=run_id,
                failed_node_id=None,
                **workflow_run_failure_context(exc),
            )
        except Exception as exc:  # noqa: BLE001
            session.rollback()
            mark_workflow_run_failed(
                session,
                run_id=run_id,
                failed_node_id=None,
                **workflow_run_failure_context(exc),
            )
    finally:
        session.close()


def execute_product_workflow_node_run(
    node_run_id: str,
    *,
    dependencies: WorkflowExecutionDependencies | None = None,
) -> None:
    """Execute one schema-v2 prompt or image node and wake its scheduler."""

    session = get_session_factory()()
    try:
        node_run = session.get(WorkflowNodeRun, node_run_id)
        if node_run is None:
            return
        try:
            if (
                node_run.workflow_run.workflow.schema_version != V2_WORKFLOW_SCHEMA_VERSION
                or node_run.node.schema_version != V2_WORKFLOW_SCHEMA_VERSION
            ):
                raise ConflictError("工作流节点运行不符合 schema-v2")
            execute_v2_workflow_node_run(
                session,
                node_run_id=node_run_id,
                dependencies=dependencies,
            )
        except TimeLimitExceeded as exc:
            session.rollback()
            _mark_node_run_failed_and_schedule(session, node_run_id=node_run_id, exc=exc)
        except Exception as exc:  # noqa: BLE001
            session.rollback()
            _mark_node_run_failed_and_schedule(session, node_run_id=node_run_id, exc=exc)
    finally:
        session.close()


def _execute_product_workflow_run(
    session: Session,
    *,
    run_id: str,
    enqueue_node_run: Callable[[str], None] | None = None,
    return_after_dispatch: bool = True,
) -> None:
    durable_dispatch = enqueue_node_run is None
    if durable_dispatch:
        def dispatch_node_run(node_run_id: str) -> None:
            _stage_workflow_node_run_dispatch(session, node_run_id=node_run_id)
    else:
        dispatch_node_run = enqueue_node_run
    run = session.get(WorkflowRun, run_id)
    if run is None or not WORKFLOW_RUN_GENERATION_TASK_CONTRACT.is_running(run.status):
        return
    workflow = session.get(ProductWorkflow, run.workflow_id)
    if workflow is None:
        raise NotFoundError("工作流不存在")
    if workflow.schema_version != V2_WORKFLOW_SCHEMA_VERSION:
        raise ConflictError("工作流运行不符合 schema-v2")
    _validate_v2_workflow_run_for_scheduling(run=run, workflow=workflow)
    rule_nodes = _workflow_rule_nodes(workflow)
    rule_edges = _workflow_rule_edges(workflow)

    while True:
        session.expire(run, ["status", "node_runs"])
        session.refresh(run)
        if run.status == WorkflowRunStatus.CANCELLED:
            return
        if not WORKFLOW_RUN_GENERATION_TASK_CONTRACT.is_running(run.status):
            return
        node_runs = list(run.node_runs)
        node_runs_by_node_id = {node_run.node_id: node_run for node_run in node_runs}
        queued_node_ids = {
            node_run.node_id
            for node_run in node_runs
            if WORKFLOW_RUN_GENERATION_TASK_CONTRACT.execution_is_queued(node_run.status)
        }
        succeeded_node_ids = {
            node_run.node_id for node_run in node_runs if node_run.status == WorkflowNodeStatus.SUCCEEDED
        }
        if _finalize_workflow_run_if_terminal(session, run=run):
            return

        ready_node_ids = ready_workflow_node_ids(
            nodes=rule_nodes,
            edges=rule_edges,
            run_node_ids=node_runs_by_node_id.keys(),
            queued_node_ids=queued_node_ids,
            succeeded_node_ids=succeeded_node_ids,
        )
        if ready_node_ids:
            for ready_node_id in ready_node_ids:
                node_run = node_runs_by_node_id.get(ready_node_id)
                if node_run is None:
                    continue
                try:
                    dispatch_node_run(node_run.id)
                except Exception:  # noqa: BLE001
                    logger.exception("工作流节点运行入队失败: workflow_node_run_id=%s", node_run.id)
                    session.rollback()
                    mark_workflow_run_failed(
                        session,
                        run_id=run_id,
                        failed_node_id=node_run.node_id,
                        reason=QUEUE_UNAVAILABLE_DETAIL,
                    )
                    return
            if durable_dispatch:
                session.commit()
            if return_after_dispatch:
                return
            continue

        if _mark_blocked_workflow_node_runs_failed(session, run=run, workflow=workflow):
            continue
        if any(WORKFLOW_RUN_GENERATION_TASK_CONTRACT.execution_is_running(item.status) for item in node_runs):
            return
        mark_workflow_run_failed(
            session,
            run_id=run_id,
            failed_node_id=None,
            reason="工作流调度失败：没有可执行的就绪节点",
        )
        return


def _validate_v2_workflow_run_for_scheduling(*, run: WorkflowRun, workflow: ProductWorkflow) -> None:
    node_runs = list(run.node_runs)
    if not node_runs:
        raise ConflictError("schema-v2 工作流运行缺少 WorkflowNodeRun")
    nodes_by_id = {node.id: node for node in workflow.nodes}
    for node_run in node_runs:
        node = nodes_by_id.get(node_run.node_id)
        if node is None or node.workflow_id != workflow.id:
            raise ConflictError("schema-v2 节点运行引用了无效节点")
        if node.schema_version != V2_WORKFLOW_SCHEMA_VERSION or node.node_type not in V2_RUNNABLE_NODE_TYPES:
            raise ConflictError("schema-v2 工作流运行包含不支持的节点")


def _mark_node_run_failed_and_schedule(
    session: Session,
    *,
    node_run_id: str,
    exc: BaseException,
) -> None:
    run_id = mark_workflow_node_run_failed(
        session,
        node_run_id=node_run_id,
        commit=False,
        **workflow_run_failure_context(exc),
    )
    if run_id is not None:
        stage_workflow_run_dispatch(session, run_id=run_id)
        session.commit()


def _stage_workflow_node_run_dispatch(session: Session, *, node_run_id: str) -> None:
    requeue_async_dispatch(
        session,
        delivery_key=delivery_key_for_actor(WORKFLOW_NODE_RUN_ACTOR_NAME, node_run_id),
        actor_name=WORKFLOW_NODE_RUN_ACTOR_NAME,
        aggregate_id=node_run_id,
    )


def _workflow_rule_nodes(workflow: ProductWorkflow) -> list[WorkflowRuleNode]:
    return [
        WorkflowRuleNode(
            id=node.id,
            node_type=node.node_type,
            position_x=node.position_x,
            config_json=node.config_json,
        )
        for node in workflow.nodes
    ]


def _workflow_rule_edges(workflow: ProductWorkflow) -> list[WorkflowRuleEdge]:
    return [
        WorkflowRuleEdge(source_node_id=edge.source_node_id, target_node_id=edge.target_node_id)
        for edge in workflow.edges
    ]


def _mark_blocked_workflow_node_runs_failed(
    session: Session,
    *,
    run: WorkflowRun,
    workflow: ProductWorkflow,
) -> bool:
    locked_run, node_runs, nodes, locked_workflow = lock_workflow_run_aggregate(session, run_id=run.id)
    if locked_run is None or locked_workflow is None:
        session.rollback()
        return False
    if WORKFLOW_RUN_GENERATION_TASK_CONTRACT.is_terminal(locked_run.status):
        session.rollback()
        return False

    incoming: dict[str, list[str]] = {}
    for edge in _workflow_rule_edges(locked_workflow):
        incoming.setdefault(edge.target_node_id, []).append(edge.source_node_id)
    run_node_ids = {node_run.node_id for node_run in node_runs}
    failed_node_ids = {
        node_run.node_id for node_run in node_runs if node_run.status == WorkflowNodeStatus.FAILED
    }
    nodes_by_id = {node.id: node for node in nodes}
    changed = False
    while True:
        changed_this_pass = False
        now = now_utc()
        for node_run in node_runs:
            if not WORKFLOW_RUN_GENERATION_TASK_CONTRACT.execution_is_queued(node_run.status):
                continue
            if not any(
                source_id in run_node_ids and source_id in failed_node_ids
                for source_id in incoming.get(node_run.node_id, [])
            ):
                continue
            node = nodes_by_id.get(node_run.node_id)
            if node is not None:
                node.status = WorkflowNodeStatus.FAILED
                node.failure_reason = "上游节点失败"
                node.last_run_at = now
            node_run.status = WorkflowNodeStatus.FAILED
            node_run.active_attempt_id = None
            node_run.failure_reason = "上游节点失败"
            node_run.finished_at = now
            failed_node_ids.add(node_run.node_id)
            changed = True
            changed_this_pass = True
        if not changed_this_pass:
            break
    if changed:
        locked_workflow.updated_at = now_utc()
        session.commit()
    else:
        session.rollback()
    return changed


def _finalize_workflow_run_if_terminal(session: Session, *, run: WorkflowRun) -> bool:
    locked_run, node_runs, _, workflow = lock_workflow_run_aggregate(session, run_id=run.id)
    if locked_run is None:
        session.rollback()
        return True
    if WORKFLOW_RUN_GENERATION_TASK_CONTRACT.is_terminal(locked_run.status):
        session.rollback()
        return True
    if any(
        WORKFLOW_RUN_GENERATION_TASK_CONTRACT.execution_is_queued(node_run.status)
        or WORKFLOW_RUN_GENERATION_TASK_CONTRACT.execution_is_running(node_run.status)
        for node_run in node_runs
    ):
        session.rollback()
        return False
    now = now_utc()
    if node_runs and all(node_run.status == WorkflowNodeStatus.SUCCEEDED for node_run in node_runs):
        locked_run.status = WorkflowRunStatus.SUCCEEDED
        locked_run.finished_at = now
        if workflow is not None:
            workflow.updated_at = now
        logger.info("工作流运行成功: run_id=%s workflow_id=%s", locked_run.id, locked_run.workflow_id)
        session.commit()
        return True

    failed_node_run = next(
        (
            node_run
            for node_run in node_runs
            if node_run.status == WorkflowNodeStatus.FAILED and node_run.failure_reason != "上游节点失败"
        ),
        None,
    ) or next((node_run for node_run in node_runs if node_run.status == WorkflowNodeStatus.FAILED), None)
    reason = (
        failed_node_run.failure_reason
        if failed_node_run is not None and failed_node_run.failure_reason
        else "工作流部分节点失败"
    )
    failure_metadata = locked_run.progress_metadata if isinstance(locked_run.progress_metadata, dict) else {}
    is_retryable = failure_metadata.get("last_failure_retryable")
    if not isinstance(is_retryable, bool):
        is_retryable = True
    retry_hint = failure_metadata.get("retry_hint")
    failure_category = failure_metadata.get("last_failure_category")
    metadata_reason = failure_metadata.get("last_failure_reason")
    if is_retryable is False and isinstance(metadata_reason, str):
        reason = metadata_reason
    locked_run.status = WorkflowRunStatus.FAILED
    locked_run.failure_reason = reason
    locked_run.is_retryable = is_retryable
    locked_run.progress_metadata = {
        **failure_metadata,
        **workflow_run_failure_progress_metadata(
            reason=reason,
            retryable=is_retryable,
            retry_hint=retry_hint if isinstance(retry_hint, str) else None,
            failure_category=failure_category if isinstance(failure_category, str) else None,
        ),
    }
    locked_run.finished_at = now
    if workflow is not None:
        workflow.updated_at = now
    logger.warning("工作流运行失败: run_id=%s reason=%s", locked_run.id, reason)
    session.commit()
    return True


__all__ = ["execute_product_workflow_node_run", "execute_product_workflow_run"]
