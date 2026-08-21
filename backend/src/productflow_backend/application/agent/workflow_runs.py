from __future__ import annotations

from dataclasses import dataclass

from sqlalchemy import select
from sqlalchemy.orm import Session, selectinload

from productflow_backend.application.agent.conversations import get_agent_conversation_by_id_or_raise
from productflow_backend.application.product_workflow.graph_commands import get_active_workflow_graph
from productflow_backend.application.product_workflow.graph_runs import list_graph_runs
from productflow_backend.application.product_workflow.v2_runs import list_v2_workflow_runs
from productflow_backend.application.workflow_drafts.materialization import get_active_v2_workflow_snapshot
from productflow_backend.domain.enums import AgentConversationScope
from productflow_backend.domain.errors import BusinessValidationError, ConflictError, NotFoundError
from productflow_backend.infrastructure.db.models import ProductWorkflow, WorkflowGraph, WorkflowGraphRun, WorkflowRun

AGENT_GLOBAL_WORKFLOW_INSPECT_MAX = 20
AGENT_GLOBAL_WORKFLOW_RUN_LIST_MAX = 10


@dataclass(frozen=True, slots=True)
class AgentWorkflowRunPage:
    workflow_id: str | None
    workflow_revision: int
    runs: tuple[WorkflowRun, ...]


def list_agent_workflow_runs(
    session: Session,
    *,
    conversation_id: str,
    limit: int = 20,
) -> AgentWorkflowRunPage:
    conversation = get_agent_conversation_by_id_or_raise(session, conversation_id)
    graph = get_active_workflow_graph(session, product_id=conversation.product_id)
    if graph is not None:
        return AgentWorkflowRunPage(
            workflow_id=graph.id,
            workflow_revision=graph.revision,
            runs=(),
        )
    snapshot = get_active_v2_workflow_snapshot(session, product_id=conversation.product_id)
    if snapshot.workflow is None:
        return AgentWorkflowRunPage(
            workflow_id=None,
            workflow_revision=snapshot.latest_revision,
            runs=(),
        )
    listed = list_v2_workflow_runs(
        session,
        product_id=conversation.product_id,
        workflow_id=snapshot.workflow.id,
        limit=limit,
    )
    return AgentWorkflowRunPage(
        workflow_id=listed.workflow.id,
        workflow_revision=listed.workflow.revision,
        runs=listed.runs,
    )


def inspect_agent_global_workflow_runs(
    session: Session,
    *,
    conversation_id: str,
    workflow_ids: list[str],
    limit: int = 5,
) -> list[dict[str, object]]:
    conversation = get_agent_conversation_by_id_or_raise(session, conversation_id)
    if conversation.scope_type != AgentConversationScope.GLOBAL:
        raise ConflictError("全局工作流运行检查只能使用 Global Agent conversation")
    normalized_ids = _normalize_global_workflow_ids(workflow_ids)
    if not 1 <= limit <= AGENT_GLOBAL_WORKFLOW_RUN_LIST_MAX:
        raise BusinessValidationError(
            f"workflow run limit 必须在 1 到 {AGENT_GLOBAL_WORKFLOW_RUN_LIST_MAX} 之间"
        )
    graphs = list(
        session.scalars(
            select(WorkflowGraph)
            .options(selectinload(WorkflowGraph.product))
            .where(WorkflowGraph.id.in_(normalized_ids))
        ).all()
    )
    graphs_by_id = {graph.id: graph for graph in graphs}
    remaining_ids = [item for item in normalized_ids if item not in graphs_by_id]
    workflows = list(
        session.scalars(
            select(ProductWorkflow)
            .options(selectinload(ProductWorkflow.product))
            .where(
                ProductWorkflow.id.in_(remaining_ids),
                ProductWorkflow.schema_version == 2,
            )
        ).all()
    ) if remaining_ids else []
    if len(graphs) + len(workflows) != len(normalized_ids):
        raise NotFoundError("部分工作流不存在")
    workflows_by_id = {workflow.id: workflow for workflow in workflows}
    result: list[dict[str, object]] = []
    for workflow_id in normalized_ids:
        graph = graphs_by_id.get(workflow_id)
        if graph is not None:
            runs = list_graph_runs(
                session,
                product_id=graph.product_id,
                graph_id=graph.id,
                limit=limit,
            )
            result.append(
                {
                    "product_id": graph.product_id,
                    "product_name": graph.product.name,
                    "workflow_id": graph.id,
                    "workflow_title": graph.title,
                    "workflow_revision": graph.revision,
                    "active": graph.active,
                    "runs": [_agent_global_graph_run_summary(run) for run in runs],
                }
            )
            continue
        workflow = workflows_by_id[workflow_id]
        runs = list(
            session.scalars(
                select(WorkflowRun)
                .options(selectinload(WorkflowRun.node_runs))
                .where(WorkflowRun.workflow_id == workflow.id)
                .order_by(WorkflowRun.started_at.desc(), WorkflowRun.id.desc())
                .limit(limit)
            ).all()
        )
        result.append(
            {
                "product_id": workflow.product_id,
                "product_name": workflow.product.name,
                "workflow_id": workflow.id,
                "workflow_title": workflow.title,
                "workflow_revision": workflow.revision,
                "active": workflow.active,
                "runs": [_agent_global_workflow_run_summary(run) for run in runs],
            }
        )
    return result


def _agent_global_graph_run_summary(run: WorkflowGraphRun) -> dict[str, object]:
    node_status_counts: dict[str, int] = {}
    for node_run in run.node_runs:
        status = node_run.status.value if hasattr(node_run.status, "value") else str(node_run.status)
        node_status_counts[status] = node_status_counts.get(status, 0) + 1
    return {
        "id": run.id,
        "status": run.status,
        "failure_reason": run.failure_reason,
        "started_at": run.started_at,
        "finished_at": run.finished_at,
        "node_status_counts": node_status_counts,
    }


def _agent_global_workflow_run_summary(run: WorkflowRun) -> dict[str, object]:
    node_status_counts: dict[str, int] = {}
    for node_run in run.node_runs:
        status = node_run.status.value
        node_status_counts[status] = node_status_counts.get(status, 0) + 1
    return {
        "id": run.id,
        "status": run.status,
        "failure_reason": run.failure_reason,
        "started_at": run.started_at,
        "finished_at": run.finished_at,
        "node_status_counts": node_status_counts,
    }


def _normalize_global_workflow_ids(values: list[str]) -> list[str]:
    if not 1 <= len(values) <= AGENT_GLOBAL_WORKFLOW_INSPECT_MAX:
        raise BusinessValidationError(
            f"workflow_ids 必须包含 1 到 {AGENT_GLOBAL_WORKFLOW_INSPECT_MAX} 个工作流"
        )
    normalized: list[str] = []
    seen: set[str] = set()
    for value in values:
        workflow_id = value.strip()
        if not workflow_id:
            raise BusinessValidationError("workflow_ids 不能包含空值")
        if workflow_id in seen:
            raise BusinessValidationError("workflow_ids 不能包含重复值")
        seen.add(workflow_id)
        normalized.append(workflow_id)
    return normalized


__all__ = [
    "AGENT_GLOBAL_WORKFLOW_INSPECT_MAX",
    "AGENT_GLOBAL_WORKFLOW_RUN_LIST_MAX",
    "AgentWorkflowRunPage",
    "inspect_agent_global_workflow_runs",
    "list_agent_workflow_runs",
]
