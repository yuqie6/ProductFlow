from __future__ import annotations

import logging
from collections.abc import Callable
from dataclasses import dataclass
from typing import Any

from dramatiq.middleware.time_limit import TimeLimitExceeded
from pydantic import ValidationError
from sqlalchemy.exc import IntegrityError
from sqlalchemy.orm import Session

from productflow_backend.application.admission import ensure_generation_capacity
from productflow_backend.application.contracts import ProductInput
from productflow_backend.application.copy_payloads import (
    normalize_copy_node_config,
    normalize_copy_payload,
)
from productflow_backend.application.product_workflow import graph as product_workflow_graph
from productflow_backend.application.product_workflow.artifacts import (
    copy_node_output,
    image_asset_output,
)
from productflow_backend.application.product_workflow.context import (
    collect_incoming_context,
    effective_product_context,
    find_source_asset,
    instruction_with_upstream_text,
    optional_config_text,
    product_context_values,
    reference_image_inputs_for_copy,
    source_asset_ids_from_config,
)
from productflow_backend.application.product_workflow.image_generation import (
    execute_workflow_image_generation,
)
from productflow_backend.application.product_workflow.mutations import get_or_create_product_workflow
from productflow_backend.application.product_workflow.query import WorkflowQueryService
from productflow_backend.application.product_workflow.run_state import (
    WORKFLOW_CANCELLED_REASON,
    WorkflowSafeExecutionError,
    claim_workflow_node_run,
    mark_workflow_node_run_failed,
    mark_workflow_run_cancelled,
    mark_workflow_run_failed,
    requeue_workflow_node_run_after_capacity_wait,
    workflow_node_failed_run_is_retryable,
    workflow_run_failure_context,
    workflow_run_failure_progress_metadata,
)
from productflow_backend.application.product_workflow_dependencies import (
    WorkflowExecutionDependencies,
    default_workflow_execution_dependencies,
)
from productflow_backend.application.queue_submission import enqueue_or_mark_failed
from productflow_backend.application.time import now_utc
from productflow_backend.domain.durable_generation_tasks import WORKFLOW_RUN_GENERATION_TASK_CONTRACT
from productflow_backend.domain.enums import (
    CopyStatus,
    WorkflowNodeStatus,
    WorkflowNodeType,
    WorkflowRunStatus,
)
from productflow_backend.domain.errors import BusinessError, BusinessValidationError, NotFoundError
from productflow_backend.domain.workflow_rules import (
    WorkflowRuleEdge,
    WorkflowRuleNode,
    ready_workflow_node_ids,
    selected_node_execution_plan,
    should_execute_missing_upstream,
)
from productflow_backend.infrastructure.db.models import (
    CopySet,
    CreativeBrief,
    Product,
    ProductWorkflow,
    WorkflowNode,
    WorkflowNodeRun,
    WorkflowRun,
)
from productflow_backend.infrastructure.db.session import get_session_factory
from productflow_backend.infrastructure.queue import enqueue_workflow_node_run, enqueue_workflow_run
from productflow_backend.infrastructure.storage import LocalStorage

logger = logging.getLogger(__name__)

COPY_PROVIDER_CONTRACT_MAX_ATTEMPTS = 2


@dataclass(frozen=True, slots=True)
class WorkflowRunKickoff:
    workflow: ProductWorkflow
    run_id: str
    created: bool
    should_enqueue: bool


def _active_workflow_run(workflow: ProductWorkflow) -> WorkflowRun | None:
    return next(
        (
            run
            for run in sorted(workflow.runs, key=lambda item: item.started_at, reverse=True)
            if WORKFLOW_RUN_GENERATION_TASK_CONTRACT.is_running(run.status)
        ),
        None,
    )


def _workflow_run_overlaps_nodes(run: WorkflowRun, node_ids: set[str]) -> bool:
    return any(node_run.node_id in node_ids for node_run in run.node_runs)


def _active_workflow_run_for_nodes(workflow: ProductWorkflow, node_ids: set[str]) -> WorkflowRun | None:
    return next(
        (
            run
            for run in sorted(workflow.runs, key=lambda item: item.started_at, reverse=True)
            if WORKFLOW_RUN_GENERATION_TASK_CONTRACT.is_active(run.status)
            and _workflow_run_overlaps_nodes(run, node_ids)
        ),
        None,
    )


def _workflow_run_should_enqueue(run: WorkflowRun) -> bool:
    if not WORKFLOW_RUN_GENERATION_TASK_CONTRACT.is_running(run.status):
        return False
    if any(WORKFLOW_RUN_GENERATION_TASK_CONTRACT.execution_is_running(node_run.status) for node_run in run.node_runs):
        return False
    has_queued_node_run = any(
        WORKFLOW_RUN_GENERATION_TASK_CONTRACT.execution_is_queued(node_run.status) for node_run in run.node_runs
    )
    return has_queued_node_run or (
        bool(run.node_runs) and all(node_run.status == WorkflowNodeStatus.SUCCEEDED for node_run in run.node_runs)
    )


def _latest_failed_workflow_run(workflow: ProductWorkflow) -> WorkflowRun | None:
    return next(
        (
            run
            for run in sorted(workflow.runs, key=lambda item: item.started_at, reverse=True)
            if run.status == WorkflowRunStatus.FAILED
        ),
        None,
    )


def _workflow_run_retry_node_ids(run: WorkflowRun) -> set[str] | None:
    retry_node_ids = {
        node_run.node_id
        for node_run in run.node_runs
        if node_run.status == WorkflowNodeStatus.FAILED and node_run.failure_reason != WORKFLOW_CANCELLED_REASON
    }
    if not retry_node_ids:
        return None
    ordered_nodes = product_workflow_graph.topological_nodes(run.workflow)
    if len(retry_node_ids) == len(ordered_nodes):
        return None
    return retry_node_ids


def start_product_workflow_run(
    session: Session,
    *,
    product_id: str,
    start_node_id: str | None = None,
    progress_metadata: dict[str, Any] | None = None,
    node_ids_to_run_override: set[str] | None = None,
) -> WorkflowRunKickoff:
    workflow = get_or_create_product_workflow(session, product_id)
    session.expire(workflow, ["nodes", "edges", "runs"])
    ordered_nodes = product_workflow_graph.topological_nodes(workflow)
    if start_node_id is not None:
        start_node = next((node for node in workflow.nodes if node.id == start_node_id), None)
        if start_node is None:
            raise BusinessValidationError("工作流节点不属于当前商品")
        if (
            start_node.status == WorkflowNodeStatus.FAILED
            and start_node.failure_reason != WORKFLOW_CANCELLED_REASON
            and not workflow_node_failed_run_is_retryable(start_node, workflow.runs)
        ):
            raise BusinessValidationError("该工作流节点不可重试")
    node_ids_to_run = (
        node_ids_to_run_override
        if node_ids_to_run_override is not None
        else _node_ids_to_run(session, workflow, start_node_id)
    )
    if not node_ids_to_run:
        raise BusinessValidationError("工作流没有可运行节点")
    active_run = _active_workflow_run_for_nodes(workflow, node_ids_to_run)
    if active_run is not None:
        return WorkflowRunKickoff(
            workflow=workflow,
            run_id=active_run.id,
            created=False,
            should_enqueue=_workflow_run_should_enqueue(active_run),
        )

    ensure_generation_capacity(session)
    run = WorkflowRun(
        workflow_id=workflow.id,
        status=WorkflowRunStatus.RUNNING,
        progress_metadata=progress_metadata,
    )
    logger.info(
        "创建商品工作流运行: product_id=%s workflow_id=%s start_node_id=%s",
        product_id,
        workflow.id,
        start_node_id,
    )
    session.add(run)
    session.flush()
    for node in ordered_nodes:
        if node.id not in node_ids_to_run:
            continue
        node.status = WorkflowNodeStatus.QUEUED
        node.failure_reason = None
        node.last_run_at = now_utc()
        session.add(
            WorkflowNodeRun(
                workflow_run_id=run.id,
                node_id=node.id,
                status=WorkflowNodeStatus.QUEUED,
            )
        )
    workflow.updated_at = now_utc()
    try:
        session.commit()
    except IntegrityError:
        session.rollback()
        workflow = product_workflow_graph.get_workflow_or_raise(session, workflow.id)
        active_run = _active_workflow_run_for_nodes(workflow, node_ids_to_run)
        if active_run is not None:
            return WorkflowRunKickoff(
                workflow=workflow,
                run_id=active_run.id,
                created=False,
                should_enqueue=_workflow_run_should_enqueue(active_run),
            )
        raise
    session.expire_all()
    return WorkflowRunKickoff(
        workflow=product_workflow_graph.get_workflow_or_raise(session, workflow.id),
        run_id=run.id,
        created=True,
        should_enqueue=True,
    )


def retry_product_workflow_run(
    session: Session,
    *,
    product_id: str,
    run_id: str | None = None,
    enqueue: Callable[[str], None] | None = None,
) -> ProductWorkflow:
    workflow = get_or_create_product_workflow(session, product_id)
    run = session.get(WorkflowRun, run_id) if run_id else _latest_failed_workflow_run(workflow)
    if run is None or run.workflow_id != workflow.id:
        raise NotFoundError("工作流运行不存在")
    if run.status != WorkflowRunStatus.FAILED:
        raise BusinessValidationError("只有失败的工作流运行可以重试")
    if not run.is_retryable:
        raise BusinessValidationError("该工作流运行不可重试")
    retry_node_ids = _workflow_run_retry_node_ids(run)
    node_ids_to_run = retry_node_ids if retry_node_ids is not None else _node_ids_to_run(session, workflow, None)
    if _active_workflow_run_for_nodes(workflow, node_ids_to_run) is not None:
        raise BusinessValidationError("相关节点运行中，不能重试")
    kickoff = start_product_workflow_run(
        session,
        product_id=product_id,
        progress_metadata=_workflow_run_retry_progress_metadata(run),
        node_ids_to_run_override=retry_node_ids,
    )
    if kickoff.should_enqueue:
        enqueue_or_mark_failed(
            kickoff.run_id,
            enqueue=lambda task_id: (enqueue or enqueue_workflow_run)(task_id),
            mark_failed=lambda task_id, reason: mark_workflow_run_enqueue_failed(
                session,
                run_id=task_id,
                reason=reason,
            ),
        )
        session.expire_all()
        return product_workflow_graph.get_workflow_or_raise(session, kickoff.workflow.id)
    return kickoff.workflow


def cancel_product_workflow_run(
    session: Session,
    *,
    product_id: str,
    run_id: str | None = None,
) -> ProductWorkflow:
    workflow = get_or_create_product_workflow(session, product_id)
    run = session.get(WorkflowRun, run_id) if run_id else _active_workflow_run(workflow)
    if run is None or run.workflow_id != workflow.id:
        raise NotFoundError("工作流运行不存在")
    if run.status == WorkflowRunStatus.CANCELLED:
        return product_workflow_graph.get_workflow_or_raise(session, workflow.id)
    if run.status in {WorkflowRunStatus.SUCCEEDED, WorkflowRunStatus.FAILED}:
        raise BusinessValidationError("已结束的工作流运行不能取消")
    mark_workflow_run_cancelled(session, run_id=run.id)
    session.expire_all()
    return product_workflow_graph.get_workflow_or_raise(session, workflow.id)


def run_product_workflow(
    session: Session,
    *,
    product_id: str,
    start_node_id: str | None = None,
    dependencies: WorkflowExecutionDependencies | None = None,
) -> ProductWorkflow:
    kickoff = start_product_workflow_run(session, product_id=product_id, start_node_id=start_node_id)
    if kickoff.created:
        _execute_product_workflow_run(
            session,
            run_id=kickoff.run_id,
            dependencies=dependencies,
            enqueue_node_run=lambda node_run_id: _execute_workflow_node_run(
                session,
                node_run_id=node_run_id,
                dependencies=dependencies,
                schedule_after_finish=False,
            ),
            return_after_dispatch=False,
        )
        session.expire_all()
        return product_workflow_graph.get_workflow_or_raise(session, kickoff.workflow.id)
    return kickoff.workflow


def submit_product_workflow_run(
    session: Session,
    *,
    product_id: str,
    start_node_id: str | None = None,
    enqueue: Callable[[str], None] | None = None,
    progress_metadata: dict[str, Any] | None = None,
) -> ProductWorkflow:
    kickoff = start_product_workflow_run(
        session,
        product_id=product_id,
        start_node_id=start_node_id,
        progress_metadata=progress_metadata,
    )
    if kickoff.should_enqueue:
        enqueue_or_mark_failed(
            kickoff.run_id,
            enqueue=enqueue or enqueue_workflow_run,
            mark_failed=lambda run_id, reason: mark_workflow_run_enqueue_failed(
                session,
                run_id=run_id,
                reason=reason,
            ),
        )
    return kickoff.workflow


def _workflow_run_retry_progress_metadata(run: WorkflowRun) -> dict[str, Any] | None:
    if not run.failure_reason:
        return None
    previous = run.progress_metadata if isinstance(run.progress_metadata, dict) else {}
    metadata = workflow_run_failure_progress_metadata(reason=run.failure_reason, retryable=run.is_retryable)
    if isinstance(previous.get("last_failure_category"), str):
        metadata["last_failure_category"] = previous["last_failure_category"]
    if isinstance(previous.get("retry_hint"), str):
        metadata["retry_hint"] = previous["retry_hint"]
    metadata["source_run_id"] = run.id
    metadata["manual_retry"] = True
    return metadata


def execute_product_workflow_run(
    run_id: str,
    *,
    dependencies: WorkflowExecutionDependencies | None = None,
) -> None:
    session_factory = get_session_factory()
    session = session_factory()
    try:
        try:
            _execute_product_workflow_run(session, run_id=run_id, dependencies=dependencies)
        except TimeLimitExceeded as exc:
            session.rollback()
            failure = workflow_run_failure_context(exc)
            mark_workflow_run_failed(
                session,
                run_id=run_id,
                failed_node_id=None,
                **failure,
            )
        except Exception as exc:  # noqa: BLE001
            session.rollback()
            failure = workflow_run_failure_context(exc)
            mark_workflow_run_failed(
                session,
                run_id=run_id,
                failed_node_id=None,
                **failure,
            )
    finally:
        session.close()


def execute_product_workflow_node_run(
    node_run_id: str,
    *,
    dependencies: WorkflowExecutionDependencies | None = None,
) -> None:
    session_factory = get_session_factory()
    session = session_factory()
    try:
        try:
            _execute_workflow_node_run(session, node_run_id=node_run_id, dependencies=dependencies)
        except TimeLimitExceeded as exc:
            session.rollback()
            _mark_node_run_failed_and_schedule(session, node_run_id=node_run_id, exc=exc)
        except Exception as exc:  # noqa: BLE001
            session.rollback()
            _mark_node_run_failed_and_schedule(session, node_run_id=node_run_id, exc=exc)
    finally:
        session.close()


def mark_workflow_run_enqueue_failed(session: Session, *, run_id: str, reason: str) -> None:
    """Mark a just-created workflow run failed when its durable queue message cannot be sent."""

    mark_workflow_run_failed(
        session,
        run_id=run_id,
        failed_node_id=None,
        reason=reason[:1000],
    )


def _execute_product_workflow_run(
    session: Session,
    *,
    run_id: str,
    dependencies: WorkflowExecutionDependencies | None = None,
    enqueue_node_run: Callable[[str], None] | None = None,
    return_after_dispatch: bool = True,
) -> None:
    dispatch_node_run = enqueue_node_run or enqueue_workflow_node_run
    queries = WorkflowQueryService(session)
    run = session.get(WorkflowRun, run_id)
    if run is None:
        return
    if not WORKFLOW_RUN_GENERATION_TASK_CONTRACT.is_running(run.status):
        return
    workflow = queries.get_workflow_or_raise(run.workflow_id)
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
                except Exception as exc:  # noqa: BLE001
                    logger.exception("工作流节点运行入队失败: workflow_node_run_id=%s", node_run.id)
                    failure = workflow_run_failure_context(exc)
                    mark_workflow_run_failed(session, run_id=run_id, failed_node_id=None, **failure)
                    return
            if return_after_dispatch:
                return
            continue

        if _mark_blocked_workflow_node_runs_failed(session, run=run, workflow=workflow):
            continue
        if any(WORKFLOW_RUN_GENERATION_TASK_CONTRACT.execution_is_running(node_run.status) for node_run in node_runs):
            return
        mark_workflow_run_failed(
            session,
            run_id=run_id,
            failed_node_id=None,
            reason="工作流调度失败：没有可执行的就绪节点",
        )
        return


def _execute_workflow_node_run(
    session: Session,
    *,
    node_run_id: str,
    dependencies: WorkflowExecutionDependencies | None = None,
    schedule_after_finish: bool = True,
) -> None:
    queries = WorkflowQueryService(session)
    node_run = session.get(WorkflowNodeRun, node_run_id)
    if node_run is None:
        return
    run = node_run.workflow_run
    if not WORKFLOW_RUN_GENERATION_TASK_CONTRACT.is_running(run.status):
        return
    if not WORKFLOW_RUN_GENERATION_TASK_CONTRACT.execution_is_queued(node_run.status):
        return
    workflow = queries.get_workflow_or_raise(run.workflow_id)
    node = queries.get_node_or_raise(node_run.node_id)
    claim = claim_workflow_node_run(session, node_run_id=node_run.id, node_id=node.id)
    if not claim.claimed:
        if claim.should_requeue:
            requeue_workflow_node_run_after_capacity_wait(node_run_id)
        return
    node = queries.get_node_or_raise(node_run.node_id)
    node_run = session.get(WorkflowNodeRun, node_run_id)
    if node_run is None:
        return
    try:
        logger.info(
            "开始执行工作流节点: run_id=%s node_id=%s node_type=%s",
            run.id,
            node.id,
            node.node_type.value,
        )
        output = _execute_node(session, workflow_id=workflow.id, node=node, dependencies=dependencies)
    except TimeLimitExceeded as exc:
        session.rollback()
        _mark_node_run_failed_and_schedule(
            session,
            node_run_id=node_run_id,
            exc=exc,
            schedule_after_finish=schedule_after_finish,
        )
        return
    except Exception as exc:  # noqa: BLE001
        session.rollback()
        _mark_node_run_failed_and_schedule(
            session,
            node_run_id=node_run_id,
            exc=exc,
            schedule_after_finish=schedule_after_finish,
        )
        return

    session.refresh(run)
    node_run = session.get(WorkflowNodeRun, node_run_id)
    if node_run is None:
        session.rollback()
        return
    session.refresh(node_run)
    if run.status == WorkflowRunStatus.CANCELLED:
        session.rollback()
        return
    if not WORKFLOW_RUN_GENERATION_TASK_CONTRACT.is_running(run.status):
        session.rollback()
        return
    if not WORKFLOW_RUN_GENERATION_TASK_CONTRACT.execution_is_running(node_run.status):
        session.rollback()
        return

    node.output_json = output
    node.status = WorkflowNodeStatus.SUCCEEDED
    node.failure_reason = None
    node.last_run_at = now_utc()
    node_run.status = WorkflowNodeStatus.SUCCEEDED
    node_run.output_json = output
    node_run.copy_set_id = output.get("copy_set_id")
    if isinstance(output.get("generated_poster_variant_ids"), list):
        poster_ids = output["generated_poster_variant_ids"]
    else:
        poster_ids = output.get("poster_variant_ids") if isinstance(output.get("poster_variant_ids"), list) else []
    node_run.poster_variant_id = poster_ids[0] if poster_ids else output.get("poster_variant_id")
    node_run.finished_at = now_utc()
    workflow.updated_at = now_utc()
    session.commit()
    logger.info("工作流节点执行成功: run_id=%s node_id=%s", run.id, node.id)
    if schedule_after_finish:
        _enqueue_workflow_run_safely(run.id)


def _mark_node_run_failed_and_schedule(
    session: Session,
    *,
    node_run_id: str,
    exc: BaseException,
    schedule_after_finish: bool = True,
) -> None:
    failure = workflow_run_failure_context(exc)
    run_id = mark_workflow_node_run_failed(session, node_run_id=node_run_id, **failure)
    if run_id is not None and schedule_after_finish:
        _enqueue_workflow_run_safely(run_id)


def _enqueue_workflow_run_safely(run_id: str) -> None:
    try:
        enqueue_workflow_run(run_id)
    except Exception:  # noqa: BLE001
        logger.exception("工作流节点完成后调度运行失败: workflow_run_id=%s", run_id)


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


def _incoming_node_ids_by_target(rule_edges: list[WorkflowRuleEdge]) -> dict[str, list[str]]:
    incoming: dict[str, list[str]] = {}
    for edge in rule_edges:
        incoming.setdefault(edge.target_node_id, []).append(edge.source_node_id)
    return incoming


def _mark_blocked_workflow_node_runs_failed(
    session: Session,
    *,
    run: WorkflowRun,
    workflow: ProductWorkflow,
) -> bool:
    incoming = _incoming_node_ids_by_target(_workflow_rule_edges(workflow))
    run_node_ids = {node_run.node_id for node_run in run.node_runs}
    failed_node_ids = {
        node_run.node_id for node_run in run.node_runs if node_run.status == WorkflowNodeStatus.FAILED
    }
    changed = False
    while True:
        changed_this_pass = False
        now = now_utc()
        for node_run in run.node_runs:
            if not WORKFLOW_RUN_GENERATION_TASK_CONTRACT.execution_is_queued(node_run.status):
                continue
            has_failed_upstream = any(
                source_id in run_node_ids and source_id in failed_node_ids
                for source_id in incoming.get(node_run.node_id, [])
            )
            if not has_failed_upstream:
                continue
            node = session.get(WorkflowNode, node_run.node_id)
            if node is not None:
                node.status = WorkflowNodeStatus.FAILED
                node.failure_reason = "上游节点失败"
                node.last_run_at = now
            node_run.status = WorkflowNodeStatus.FAILED
            node_run.failure_reason = "上游节点失败"
            node_run.finished_at = now
            failed_node_ids.add(node_run.node_id)
            changed = True
            changed_this_pass = True
        if not changed_this_pass:
            break
    if changed:
        run.workflow.updated_at = now_utc()
        session.commit()
    return changed


def _finalize_workflow_run_if_terminal(session: Session, *, run: WorkflowRun) -> bool:
    node_runs = list(run.node_runs)
    if any(
        WORKFLOW_RUN_GENERATION_TASK_CONTRACT.execution_is_queued(node_run.status)
        or WORKFLOW_RUN_GENERATION_TASK_CONTRACT.execution_is_running(node_run.status)
        for node_run in node_runs
    ):
        return False
    now = now_utc()
    if node_runs and all(node_run.status == WorkflowNodeStatus.SUCCEEDED for node_run in node_runs):
        run.status = WorkflowRunStatus.SUCCEEDED
        run.finished_at = now
        run.workflow.updated_at = now
        logger.info("工作流运行成功: run_id=%s workflow_id=%s", run.id, run.workflow_id)
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
        if failed_node_run and failed_node_run.failure_reason
        else "工作流部分节点失败"
    )
    failure_metadata = run.progress_metadata if isinstance(run.progress_metadata, dict) else {}
    is_retryable = failure_metadata.get("last_failure_retryable")
    if not isinstance(is_retryable, bool):
        is_retryable = True
    retry_hint = failure_metadata.get("retry_hint")
    failure_category = failure_metadata.get("last_failure_category")
    metadata_reason = failure_metadata.get("last_failure_reason")
    if is_retryable is False and isinstance(metadata_reason, str):
        reason = metadata_reason
    run.status = WorkflowRunStatus.FAILED
    run.failure_reason = reason
    run.is_retryable = is_retryable
    run.progress_metadata = workflow_run_failure_progress_metadata(
        reason=reason,
        retryable=is_retryable,
        retry_hint=retry_hint if isinstance(retry_hint, str) else None,
        failure_category=failure_category if isinstance(failure_category, str) else None,
    )
    run.finished_at = now
    run.workflow.updated_at = now
    logger.warning("工作流运行失败: run_id=%s reason=%s", run.id, reason)
    session.commit()
    return True


def _node_ids_to_run(session: Session, workflow: ProductWorkflow, start_node_id: str | None) -> set[str]:
    if start_node_id is None:
        return {node.id for node in workflow.nodes}
    rule_nodes = [
        WorkflowRuleNode(
            id=node.id,
            node_type=node.node_type,
            position_x=node.position_x,
            config_json=node.config_json,
        )
        for node in workflow.nodes
    ]
    rule_edges = [
        WorkflowRuleEdge(source_node_id=edge.source_node_id, target_node_id=edge.target_node_id)
        for edge in workflow.edges
    ]
    nodes_by_id = {node.id: node for node in workflow.nodes}
    if start_node_id not in nodes_by_id:
        raise BusinessValidationError("工作流节点不属于当前商品")
    reusable_edges: set[tuple[str, str]] = set()
    for edge in workflow.edges:
        source_node = nodes_by_id.get(edge.source_node_id)
        target_node = nodes_by_id.get(edge.target_node_id)
        if source_node is None or target_node is None:
            raise BusinessValidationError("工作流连线引用了不存在的节点")
        if _node_has_reusable_output(session, workflow, source_node, target_node=target_node):
            reusable_edges.add((edge.source_node_id, edge.target_node_id))
    return selected_node_execution_plan(
        nodes=rule_nodes,
        edges=rule_edges,
        start_node_id=start_node_id,
        reusable_edges=reusable_edges,
    )


def _node_has_reusable_output(
    session: Session,
    workflow: ProductWorkflow,
    node: WorkflowNode,
    *,
    target_node: WorkflowNode | None = None,
) -> bool:
    queries = WorkflowQueryService(session)
    if node.node_type == WorkflowNodeType.PRODUCT_CONTEXT:
        return True
    if node.status != WorkflowNodeStatus.SUCCEEDED:
        return False
    output = node.output_json or {}
    if node.node_type == WorkflowNodeType.REFERENCE_IMAGE:
        return _node_has_valid_reference_assets(session, workflow.product_id, node)
    if node.node_type == WorkflowNodeType.COPY_GENERATION:
        copy_set_id = output.get("copy_set_id")
        if not isinstance(copy_set_id, str):
            return False
        return queries.copy_set_for_product(copy_set_id, workflow.product_id) is not None
    if node.node_type == WorkflowNodeType.IMAGE_GENERATION:
        if target_node is not None and target_node.node_type == WorkflowNodeType.REFERENCE_IMAGE:
            return _image_generation_filled_reference_target(
                session,
                workflow=workflow,
                image_node=node,
                reference_node=target_node,
            )
        poster_ids = output.get("poster_variant_ids")
        if not isinstance(poster_ids, list):
            poster_ids = output.get("generated_poster_variant_ids")
        filled_ids = output.get("filled_source_asset_ids")
        source_asset_ids = source_asset_ids_from_config(output)
        if isinstance(filled_ids, list):
            source_asset_ids.extend(item for item in filled_ids if isinstance(item, str))
        has_source_assets = _valid_source_asset_ids(session, workflow.product_id, source_asset_ids)
        has_posters = False
        if isinstance(poster_ids, list):
            posters = queries.posters_by_ids([item for item in poster_ids if isinstance(item, str)])
            has_posters = any(poster.product_id == workflow.product_id for poster in posters)
        return has_source_assets or has_posters
    return False


def _image_generation_filled_reference_target(
    session: Session,
    *,
    workflow: ProductWorkflow,
    image_node: WorkflowNode,
    reference_node: WorkflowNode,
) -> bool:
    """Return whether an image node's previous output satisfies a specific reference slot edge."""
    output = image_node.output_json or {}
    filled_reference_node_ids = output.get("filled_reference_node_ids")
    output_names_target = (
        isinstance(filled_reference_node_ids, list) and reference_node.id in filled_reference_node_ids
    )
    target_has_assets = _node_has_valid_reference_assets(session, workflow.product_id, reference_node)
    if output_names_target:
        return target_has_assets
    # Older outputs may not name filled reference nodes. The target slot itself is still authoritative: if it
    # already exposes a live first-class image artifact, the upstream image node does not need to rerun.
    return target_has_assets


def _node_has_valid_reference_assets(session: Session, product_id: str, node: WorkflowNode) -> bool:
    asset_ids = list(
        dict.fromkeys(
            [
                *source_asset_ids_from_config(node.output_json or {}),
                *source_asset_ids_from_config(node.config_json or {}),
            ]
        )
    )
    return _valid_source_asset_ids(session, product_id, asset_ids)


def _valid_source_asset_ids(session: Session, product_id: str, asset_ids: list[str]) -> bool:
    return WorkflowQueryService(session).has_any_source_asset_for_product(product_id, asset_ids)


def _should_execute_missing_upstream(source_node: WorkflowNode, target_node: WorkflowNode) -> bool:
    return should_execute_missing_upstream(
        WorkflowRuleNode(
            id=source_node.id,
            node_type=source_node.node_type,
            position_x=source_node.position_x,
            config_json=source_node.config_json,
        ),
        WorkflowRuleNode(
            id=target_node.id,
            node_type=target_node.node_type,
            position_x=target_node.position_x,
            config_json=target_node.config_json,
        ),
    )


def _execute_node(
    session: Session,
    *,
    workflow_id: str,
    node: WorkflowNode,
    dependencies: WorkflowExecutionDependencies | None = None,
) -> dict[str, Any]:
    workflow = product_workflow_graph.get_workflow_or_raise(session, workflow_id)
    product = workflow.product
    dependencies = dependencies or default_workflow_execution_dependencies()
    if node.node_type == WorkflowNodeType.PRODUCT_CONTEXT:
        return _execute_product_context(product, node)
    if node.node_type == WorkflowNodeType.REFERENCE_IMAGE:
        return _execute_reference_image(session, workflow=workflow, node=node)
    if node.node_type == WorkflowNodeType.COPY_GENERATION:
        return _execute_copy_generation(session, workflow=workflow, node=node, dependencies=dependencies)
    if node.node_type == WorkflowNodeType.IMAGE_GENERATION:
        return execute_workflow_image_generation(session, workflow=workflow, node=node, dependencies=dependencies)
    raise BusinessValidationError("工作流节点类型不支持")


def _execute_product_context(product: Product, node: WorkflowNode) -> dict[str, Any]:
    context = product_context_values(product, node)
    source = find_source_asset(product)
    return {
        "product_id": product.id,
        "name": context["name"],
        "category": context["category"],
        "price": context["price"],
        "source_note": context["source_note"],
        "source_asset_id": source.id if source else None,
        "summary": "商品已读取。",
    }


def _execute_reference_image(session: Session, *, workflow: ProductWorkflow, node: WorkflowNode) -> dict[str, Any]:
    asset_ids = source_asset_ids_from_config(node.config_json)
    assets = WorkflowQueryService(session).source_assets_by_ids(asset_ids)
    assets = [asset for asset in assets if asset.product_id == workflow.product_id]
    if not assets:
        return image_asset_output([], summary="参考图为空")
    return image_asset_output(
        assets,
        summary=f"参考图 {len(assets)} 张",
        role=optional_config_text(node.config_json, "role"),
        label=optional_config_text(node.config_json, "label") or node.title,
    )


def _execute_copy_generation(
    session: Session,
    *,
    workflow: ProductWorkflow,
    node: WorkflowNode,
    dependencies: WorkflowExecutionDependencies | None = None,
) -> dict[str, Any]:
    dependencies = dependencies or default_workflow_execution_dependencies()
    product = workflow.product
    product_context = effective_product_context(workflow, node.id)
    has_product_context = any(value is not None for value in product_context.values())
    existing_output = node.output_json or {}
    existing_copy_set_id = existing_output.get("copy_set_id")
    if existing_output.get("manual_edit") is True and isinstance(existing_copy_set_id, str):
        copy_set = session.get(CopySet, existing_copy_set_id)
        if copy_set is not None and copy_set.product_id == product.id:
            return copy_node_output(copy_set, creative_brief_id=copy_set.creative_brief_id, manual_edit=True)

    storage = LocalStorage()
    source = find_source_asset(product) if has_product_context else None
    product_input = ProductInput(
        name=product_context["name"] or "自由创作",
        category=product_context["category"],
        price=product_context["price"],
        source_note=product_context["source_note"],
        image_path=str(storage.resolve(source.storage_path)) if source is not None else "",
    )
    incoming_context = collect_incoming_context(workflow, node.id)
    reference_images = reference_image_inputs_for_copy(session, workflow=workflow, node_id=node.id, storage=storage)
    config = _normalize_copy_node_config_for_execution(node.config_json)
    instruction = instruction_with_upstream_text(
        config.instruction,
        incoming_context,
    )
    config = config.model_copy(update={"instruction": instruction})
    provider = dependencies.text_provider()
    brief_payload, brief_model = _generate_brief_with_provider(provider, product_input, node_id=node.id)
    brief = CreativeBrief(
        product_id=product.id,
        payload=brief_payload.model_dump(),
        provider_name=provider.provider_name,
        model_name=brief_model,
        prompt_version=provider.prompt_version,
    )
    session.add(brief)
    session.flush()

    copy_payload, copy_model = _generate_copy_with_provider(
        provider,
        product_input,
        brief_payload,
        config=config,
        reference_images=reference_images,
        node_id=node.id,
    )
    structured_payload = copy_payload.model_dump(mode="json")
    copy_set = CopySet(
        product_id=product.id,
        creative_brief_id=brief.id,
        status=CopyStatus.DRAFT,
        structured_payload=structured_payload,
        model_structured_payload=structured_payload,
        provider_name=provider.provider_name,
        model_name=copy_model,
        prompt_version=provider.prompt_version,
    )
    session.add(copy_set)
    session.flush()
    product.updated_at = now_utc()
    output = copy_node_output(copy_set, creative_brief_id=brief.id)
    output["instruction"] = instruction
    output["context_summary"] = {
        "product_context": product_context,
        "reference_image_count": len(reference_images),
        "upstream_text_count": len(incoming_context.text_contexts),
    }
    output["context_sources"] = incoming_context.text_sources[:8]
    return output


def _normalize_copy_node_config_for_execution(raw_config: dict[str, Any] | None):
    try:
        return normalize_copy_node_config(raw_config)
    except (ValidationError, ValueError) as exc:
        raise WorkflowSafeExecutionError(
            "文案节点配置无效，请调整节点设置后重试",
            retryable=False,
            retry_hint="revise_input",
            failure_category="invalid_node_config",
        ) from exc


def _generate_brief_with_provider(
    provider: Any,
    product_input: ProductInput,
    *,
    node_id: str,
) -> tuple[Any, str]:
    return _call_text_provider_with_payload_retry(
        lambda: provider.generate_brief(product_input),
        operation="brief",
        node_id=node_id,
    )


def _generate_copy_with_provider(
    provider: Any,
    product_input: ProductInput,
    brief_payload: Any,
    *,
    config: Any,
    reference_images: list[Any],
    node_id: str | None = None,
) -> tuple[Any, str]:
    def generate_once() -> tuple[Any, str]:
        copy_payload, model_name = provider.generate_copy(
            product_input,
            brief_payload,
            config=config,
            reference_images=reference_images,
        )
        return normalize_copy_payload(copy_payload.model_dump(mode="json"), fallback_purpose=config.purpose), model_name

    return _call_text_provider_with_payload_retry(
        generate_once,
        operation="copy",
        node_id=node_id,
    )


def _call_text_provider_with_payload_retry(
    call: Callable[[], tuple[Any, str]],
    *,
    operation: str,
    node_id: str | None,
) -> tuple[Any, str]:
    for attempt in range(1, COPY_PROVIDER_CONTRACT_MAX_ATTEMPTS + 1):
        try:
            return call()
        except (ValidationError, ValueError) as exc:
            if isinstance(exc, BusinessError) or attempt >= COPY_PROVIDER_CONTRACT_MAX_ATTEMPTS:
                raise
            logger.warning(
                "文案 provider 返回字段不匹配，准备重试: operation=%s node_id=%s attempt=%s max_attempts=%s "
                "error_class=%s",
                operation,
                node_id,
                attempt,
                COPY_PROVIDER_CONTRACT_MAX_ATTEMPTS,
                exc.__class__.__name__,
            )
    raise RuntimeError("unreachable text provider retry state")
