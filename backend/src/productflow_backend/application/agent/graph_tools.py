"""Agent graph ChangeSet 工具。mutation ledger 保证幂等。"""

from __future__ import annotations

from datetime import UTC, datetime
from typing import Any

from sqlalchemy import select
from sqlalchemy.orm import Session

from productflow_backend.application.agent.agent_context import _require_product_conversation
from productflow_backend.application.agent.conversations import get_agent_conversation_by_id_or_raise
from productflow_backend.application.agent.tool_ledger import (
    AgentToolMutationStage,
    AgentToolReconcileResult,
    apply_tool_mutation,
    reconcile_tool_mutation,
)
from productflow_backend.application.product_workflow.graph_commands import get_active_workflow_graph
from productflow_backend.application.product_workflow.graph_proposals import (
    apply_agent_graph_change_set,
    discard_graph_proposal,
    parse_agent_change_set,
    propose_graph_change_set,
)
from productflow_backend.application.product_workflow.graph_queries import project_workflow_graph
from productflow_backend.application.product_workflow.graph_runs import cancel_graph_run, get_graph_run
from productflow_backend.domain.enums import AgentToolMutationStatus, GraphProposalStatus, WorkflowRunStatus
from productflow_backend.domain.errors import BusinessValidationError, ConflictError, NotFoundError
from productflow_backend.infrastructure.db.models import (
    AgentConversation,
    AgentToolMutation,
    WorkflowGraphProposal,
    new_id,
)

APPLY_GRAPH_TOOL_NAME = "apply_graph_change_set_v1"
PROPOSE_GRAPH_TOOL_NAME = "propose_graph_change_set_v1"
GET_NODE_DETAIL_TOOL_NAME = "get_node_detail_v1"
DISCARD_PROPOSAL_TOOL_NAME = "discard_workflow_proposal_v1"
CANCEL_WORKFLOW_RUN_TOOL_NAME = "cancel_workflow_run_v1"
FOCUS_CANVAS_TOOL_NAME = "focus_canvas_items_v1"
MAX_CANVAS_FOCUS_ITEMS = 20


def apply_agent_graph_change_set_tool(
    session: Session,
    *,
    conversation_id: str,
    change_set: dict[str, Any],
    idempotency_key: str,
) -> dict[str, Any]:
    """应用一条可逆 graph ChangeSet。已有 applied ledger 行则回放。"""
    parsed = parse_agent_change_set(change_set)

    def stage(session: Session, conversation: AgentConversation) -> AgentToolMutationStage:
        graph = apply_agent_graph_change_set(
            session,
            conversation_id=conversation.id,
            change_set=parsed,
            commit=False,
        )
        return AgentToolMutationStage(
            result={
                "accepted": True,
                "applied": True,
                "graph_id": graph.id,
                "revision": graph.revision,
                "summary": parsed.summary,
            }
        )

    return apply_tool_mutation(
        session,
        conversation_id=conversation_id,
        idempotency_key=idempotency_key,
        tool_name=APPLY_GRAPH_TOOL_NAME,
        operation=APPLY_GRAPH_TOOL_NAME,
        before={"base_graph_revision": parsed.base_graph_revision},
        target=parsed.model_dump(mode="json"),
        stage=stage,
        validate_conversation=_require_product_conversation,
    )


def propose_agent_graph_change_set_tool(
    session: Session,
    *,
    conversation_id: str,
    change_set: dict[str, Any],
    idempotency_key: str,
) -> dict[str, Any]:
    """把未应用的 graph 预览写入 mutation ledger。"""
    parsed = parse_agent_change_set(change_set)

    def stage(session: Session, conversation: AgentConversation) -> AgentToolMutationStage:
        proposal = propose_graph_change_set(
            session,
            conversation_id=conversation.id,
            change_set=parsed,
            commit=False,
        )
        return AgentToolMutationStage(
            result={
                "accepted": True,
                "applied": False,
                "pending_confirmation": True,
                "proposal_id": proposal.id,
                "graph_id": proposal.graph_id,
                "base_graph_revision": proposal.base_graph_revision,
                "summary": proposal.summary,
            }
        )

    return apply_tool_mutation(
        session,
        conversation_id=conversation_id,
        idempotency_key=idempotency_key,
        tool_name=PROPOSE_GRAPH_TOOL_NAME,
        operation=PROPOSE_GRAPH_TOOL_NAME,
        before={"base_graph_revision": parsed.base_graph_revision},
        target=parsed.model_dump(mode="json"),
        stage=stage,
        validate_conversation=_require_product_conversation,
    )


def reconcile_agent_graph_change_set_tool(
    session: Session,
    *,
    conversation_id: str,
    change_set: dict[str, Any],
    idempotency_key: str,
    tool_name: str,
) -> AgentToolReconcileResult:
    """对账 graph apply/propose，不重放任一命令。"""
    if tool_name not in {APPLY_GRAPH_TOOL_NAME, PROPOSE_GRAPH_TOOL_NAME}:
        raise BusinessValidationError("不支持的图变更对账工具")
    parsed = parse_agent_change_set(change_set)

    def fallback(_session: Session, _conversation: AgentConversation) -> AgentToolReconcileResult:
        return AgentToolReconcileResult(state="not_applied", detail="图变更副作用尚未提交")

    return reconcile_tool_mutation(
        session,
        conversation_id=conversation_id,
        idempotency_key=idempotency_key,
        tool_name=tool_name,
        operation=tool_name,
        before={"base_graph_revision": parsed.base_graph_revision},
        target=parsed.model_dump(mode="json"),
        fallback=fallback,
        validate_conversation=_require_product_conversation,
    )


def get_agent_node_detail(
    session: Session,
    *,
    conversation_id: str,
    node_id: str,
) -> dict[str, Any]:
    """返回一个节点的配置、入边和当前 artifact 摘要。"""
    conversation = get_agent_conversation_by_id_or_raise(session, conversation_id)
    _require_product_conversation(conversation)
    graph = get_active_workflow_graph(session, product_id=conversation.product_id)
    if graph is None:
        raise ConflictError("当前商品没有可编辑的工作流")
    normalized_id = node_id.strip()
    if not normalized_id:
        raise BusinessValidationError("node_id 不能为空")
    projection = project_workflow_graph(session, graph)
    node = next((item for item in projection.nodes if item.id == normalized_id), None)
    if node is None:
        raise NotFoundError("节点不存在")
    nodes_by_id = {item.id: item for item in projection.nodes}
    return {
        "accepted": True,
        "graph_id": projection.id,
        "revision": projection.revision,
        "node": {
            "id": node.id,
            "node_type": node.node_type.value,
            "title": node.title,
            "config_status": node.config_status.value,
            "unused": node.unused,
            "group_id": node.group_id,
            "bound_asset_id": node.bound_asset_id,
            "preview_asset_id": node.preview_asset_id,
            "config": dict(node.config),
            "incoming": [
                {
                    "id": edge.id,
                    "node_id": edge.node_id,
                    "title": nodes_by_id[edge.node_id].title if edge.node_id in nodes_by_id else None,
                    "role": edge.role.value,
                    "data_type": edge.data_type.value,
                    "order": edge.order,
                }
                for edge in node.incoming
            ],
            "outgoing": [
                {
                    "id": edge.id,
                    "node_id": edge.node_id,
                    "title": nodes_by_id[edge.node_id].title if edge.node_id in nodes_by_id else None,
                    "role": edge.role.value,
                    "data_type": edge.data_type.value,
                    "order": edge.order,
                }
                for edge in node.outgoing
            ],
            "current_artifact": _artifact_summary(node),
        },
    }


def discard_agent_graph_proposal_tool(
    session: Session,
    *,
    conversation_id: str,
    idempotency_key: str,
    proposal_id: str | None = None,
) -> dict[str, Any]:
    """丢弃当前 pending GraphProposal。不写入 live 图。"""
    target_proposal_id = proposal_id.strip() if proposal_id else None

    def stage(session: Session, conversation: AgentConversation) -> AgentToolMutationStage:
        graph = _require_live_graph(session, conversation)
        resolved_id = target_proposal_id or _pending_proposal_id(session, graph.id)
        if resolved_id is None:
            raise ConflictError("没有待处理的图提案")
        discard_graph_proposal(
            session,
            product_id=conversation.product_id or "",
            graph_id=graph.id,
            proposal_id=resolved_id,
            commit=False,
        )
        return AgentToolMutationStage(
            result={
                "accepted": True,
                "discarded": True,
                "proposal_id": resolved_id,
                "graph_id": graph.id,
            }
        )

    return apply_tool_mutation(
        session,
        conversation_id=conversation_id,
        idempotency_key=idempotency_key,
        tool_name=DISCARD_PROPOSAL_TOOL_NAME,
        operation=DISCARD_PROPOSAL_TOOL_NAME,
        before={},
        target={"proposal_id": target_proposal_id},
        stage=stage,
        validate_conversation=_require_product_conversation,
    )


def reconcile_agent_graph_proposal_discard_tool(
    session: Session,
    *,
    conversation_id: str,
    idempotency_key: str,
    proposal_id: str | None = None,
) -> AgentToolReconcileResult:
    target_proposal_id = proposal_id.strip() if proposal_id else None

    def fallback(session: Session, conversation: AgentConversation) -> AgentToolReconcileResult:
        graph = get_active_workflow_graph(session, product_id=conversation.product_id)
        if graph is None:
            return AgentToolReconcileResult(state="not_applied", detail="图提案丢弃副作用尚未提交")
        resolved_id = target_proposal_id or _pending_proposal_id(session, graph.id)
        if resolved_id is None:
            return AgentToolReconcileResult(state="unknown", detail="无法证明图提案已丢弃")
        proposal = session.scalar(
            select(WorkflowGraphProposal).where(
                WorkflowGraphProposal.id == resolved_id,
                WorkflowGraphProposal.graph_id == graph.id,
            )
        )
        if proposal is not None and GraphProposalStatus(proposal.status) == GraphProposalStatus.DISCARDED:
            return AgentToolReconcileResult(
                state="applied",
                result={
                    "accepted": True,
                    "discarded": True,
                    "proposal_id": proposal.id,
                    "graph_id": graph.id,
                },
                detail="图提案已丢弃",
            )
        if proposal is not None and GraphProposalStatus(proposal.status) == GraphProposalStatus.PENDING:
            return AgentToolReconcileResult(state="not_applied", detail="图提案仍待处理")
        return AgentToolReconcileResult(state="unknown", detail="无法证明图提案已丢弃")

    return reconcile_tool_mutation(
        session,
        conversation_id=conversation_id,
        idempotency_key=idempotency_key,
        tool_name=DISCARD_PROPOSAL_TOOL_NAME,
        operation=DISCARD_PROPOSAL_TOOL_NAME,
        before={},
        target={"proposal_id": target_proposal_id},
        fallback=fallback,
        validate_conversation=_require_product_conversation,
    )


def cancel_agent_workflow_run_tool(
    session: Session,
    *,
    conversation_id: str,
    run_id: str,
    idempotency_key: str,
) -> dict[str, Any]:
    """取消当前商品 live graph 上的一次 WorkflowGraphRun。"""
    normalized_run_id = run_id.strip()
    if not normalized_run_id:
        raise BusinessValidationError("run_id 不能为空")

    def stage(session: Session, conversation: AgentConversation) -> AgentToolMutationStage:
        graph = _require_live_graph(session, conversation)
        run = cancel_graph_run(
            session,
            product_id=conversation.product_id or "",
            graph_id=graph.id,
            run_id=normalized_run_id,
            commit=False,
        )
        return AgentToolMutationStage(result=_cancel_result(run))

    return apply_tool_mutation(
        session,
        conversation_id=conversation_id,
        idempotency_key=idempotency_key,
        tool_name=CANCEL_WORKFLOW_RUN_TOOL_NAME,
        operation=CANCEL_WORKFLOW_RUN_TOOL_NAME,
        before={},
        target={"run_id": normalized_run_id},
        stage=stage,
        validate_conversation=_require_product_conversation,
    )


def reconcile_agent_workflow_run_cancel_tool(
    session: Session,
    *,
    conversation_id: str,
    run_id: str,
    idempotency_key: str,
) -> AgentToolReconcileResult:
    normalized_run_id = run_id.strip()
    if not normalized_run_id:
        raise BusinessValidationError("run_id 不能为空")

    def fallback(session: Session, conversation: AgentConversation) -> AgentToolReconcileResult:
        graph = get_active_workflow_graph(session, product_id=conversation.product_id)
        if graph is None:
            return AgentToolReconcileResult(state="not_applied", detail="工作流运行取消尚未提交")
        try:
            run = get_graph_run(
                session,
                product_id=conversation.product_id or "",
                graph_id=graph.id,
                run_id=normalized_run_id,
            )
        except NotFoundError:
            return AgentToolReconcileResult(state="not_applied", detail="工作流运行取消尚未提交")
        if run.status == WorkflowRunStatus.CANCELLED:
            return AgentToolReconcileResult(
                state="applied",
                result=_cancel_result(run),
                detail="工作流运行已取消",
            )
        return AgentToolReconcileResult(state="not_applied", detail="工作流运行仍未取消")

    return reconcile_tool_mutation(
        session,
        conversation_id=conversation_id,
        idempotency_key=idempotency_key,
        tool_name=CANCEL_WORKFLOW_RUN_TOOL_NAME,
        operation=CANCEL_WORKFLOW_RUN_TOOL_NAME,
        before={},
        target={"run_id": normalized_run_id},
        fallback=fallback,
        validate_conversation=_require_product_conversation,
    )


def focus_agent_canvas_items_tool(
    session: Session,
    *,
    conversation_id: str,
    node_ids: list[str],
    edge_ids: list[str] | None = None,
    group_ids: list[str] | None = None,
    idempotency_key: str,
) -> dict[str, Any]:
    """记录有界画布聚焦请求，供 Turn 投影交给工作台选中。"""
    focus = _normalize_canvas_focus(node_ids, edge_ids or [], group_ids or [])

    def stage(session: Session, conversation: AgentConversation) -> AgentToolMutationStage:
        graph = _require_live_graph(session, conversation)
        projection = project_workflow_graph(session, graph)
        _validate_canvas_focus(projection, focus)
        return AgentToolMutationStage(
            result={
                "accepted": True,
                "request_id": new_id(),
                "graph_id": projection.id,
                "revision": projection.revision,
                **focus,
            }
        )

    return apply_tool_mutation(
        session,
        conversation_id=conversation_id,
        idempotency_key=idempotency_key,
        tool_name=FOCUS_CANVAS_TOOL_NAME,
        operation=FOCUS_CANVAS_TOOL_NAME,
        before={},
        target=focus,
        stage=stage,
        validate_conversation=_require_product_conversation,
    )


def reconcile_agent_canvas_focus_tool(
    session: Session,
    *,
    conversation_id: str,
    node_ids: list[str],
    idempotency_key: str,
    edge_ids: list[str] | None = None,
    group_ids: list[str] | None = None,
) -> AgentToolReconcileResult:
    focus = _normalize_canvas_focus(node_ids, edge_ids or [], group_ids or [])

    def fallback(_session: Session, _conversation: AgentConversation) -> AgentToolReconcileResult:
        return AgentToolReconcileResult(state="not_applied", detail="画布聚焦请求尚未提交")

    return reconcile_tool_mutation(
        session,
        conversation_id=conversation_id,
        idempotency_key=idempotency_key,
        tool_name=FOCUS_CANVAS_TOOL_NAME,
        operation=FOCUS_CANVAS_TOOL_NAME,
        before={},
        target=focus,
        fallback=fallback,
        validate_conversation=_require_product_conversation,
    )


def canvas_focus_for_turn(session: Session, projection: Any) -> dict[str, Any] | None:
    """把本 Turn 期间最新的聚焦请求投影到 Turn 响应。"""
    mapping = canvas_focus_for_turns(session, [projection])
    return mapping.get(projection.id)


def canvas_focus_for_turns(session: Session, turns: list[Any]) -> dict[str, dict[str, Any]]:
    if not turns:
        return {}
    conversation_id = turns[0].conversation_id
    earliest = min(turn.created_at for turn in turns)
    mutations = list(
        session.scalars(
            select(AgentToolMutation)
            .where(
                AgentToolMutation.conversation_id == conversation_id,
                AgentToolMutation.tool_name == FOCUS_CANVAS_TOOL_NAME,
                AgentToolMutation.status == AgentToolMutationStatus.APPLIED,
                AgentToolMutation.created_at >= earliest,
            )
            .order_by(AgentToolMutation.created_at.desc(), AgentToolMutation.id.desc())
            .limit(200)
        )
    )
    mapping: dict[str, dict[str, Any]] = {}
    for turn in turns:
        latest = next(
            (
                mutation
                for mutation in mutations
                if _as_utc(turn.created_at) <= _as_utc(mutation.created_at)
                and (
                    turn.finished_at is None
                    or _as_utc(turn.finished_at) >= _as_utc(mutation.created_at)
                )
            ),
            None,
        )
        if latest is None or not isinstance(latest.result_json, dict):
            continue
        payload = _canvas_focus_payload(latest.result_json, latest.id)
        if payload is not None:
            mapping[turn.id] = payload
    return mapping


def _artifact_summary(node: Any) -> dict[str, Any] | None:
    if node.current_artifact_id is None and node.preview_asset_id is None:
        return None
    artifact_type = node.current_artifact_type
    return {
        "id": node.current_artifact_id,
        "type": artifact_type.value if artifact_type is not None else None,
        "preview_asset_id": node.preview_asset_id,
    }


def _require_live_graph(session: Session, conversation: AgentConversation):
    graph = get_active_workflow_graph(session, product_id=conversation.product_id)
    if graph is None:
        raise ConflictError("当前商品没有可编辑的工作流")
    return graph


def _pending_proposal_id(session: Session, graph_id: str) -> str | None:
    proposal = session.scalar(
        select(WorkflowGraphProposal).where(
            WorkflowGraphProposal.graph_id == graph_id,
            WorkflowGraphProposal.status == GraphProposalStatus.PENDING,
        )
    )
    return proposal.id if proposal is not None else None


def _cancel_result(run: Any) -> dict[str, Any]:
    status = run.status.value if hasattr(run.status, "value") else str(run.status)
    return {
        "accepted": True,
        "cancelled": True,
        "run_id": run.id,
        "graph_id": run.graph_id,
        "status": status,
    }


def _normalize_canvas_focus(
    node_ids: list[str],
    edge_ids: list[str],
    group_ids: list[str],
) -> dict[str, list[str]]:
    focus = {
        "node_ids": _unique_ids(node_ids, field="node_ids"),
        "edge_ids": _unique_ids(edge_ids, field="edge_ids"),
        "group_ids": _unique_ids(group_ids, field="group_ids"),
    }
    if not focus["node_ids"] and not focus["edge_ids"] and not focus["group_ids"]:
        raise BusinessValidationError("画布聚焦至少需要一个节点、边或分组")
    return focus


def _unique_ids(values: list[str], *, field: str) -> list[str]:
    cleaned = [item.strip() for item in values if item and item.strip()]
    if len(cleaned) != len(set(cleaned)):
        raise BusinessValidationError(f"{field} 不能重复")
    if len(cleaned) > MAX_CANVAS_FOCUS_ITEMS:
        raise BusinessValidationError(f"{field} 不能超过 {MAX_CANVAS_FOCUS_ITEMS} 项")
    return cleaned


def _validate_canvas_focus(projection: Any, focus: dict[str, list[str]]) -> None:
    known_nodes = {node.id for node in projection.nodes}
    known_edges = {edge.id for edge in projection.edges}
    known_groups = {group.id for group in projection.groups}
    missing_nodes = [node_id for node_id in focus["node_ids"] if node_id not in known_nodes]
    missing_edges = [edge_id for edge_id in focus["edge_ids"] if edge_id not in known_edges]
    missing_groups = [group_id for group_id in focus["group_ids"] if group_id not in known_groups]
    if missing_nodes or missing_edges or missing_groups:
        raise NotFoundError("部分画布对象不存在")


def _canvas_focus_payload(result: dict[str, Any], mutation_id: str) -> dict[str, Any] | None:
    node_ids = [item for item in result.get("node_ids") or [] if isinstance(item, str)]
    edge_ids = [item for item in result.get("edge_ids") or [] if isinstance(item, str)]
    group_ids = [item for item in result.get("group_ids") or [] if isinstance(item, str)]
    if not node_ids and not edge_ids and not group_ids:
        return None
    request_id = result.get("request_id")
    return {
        "request_id": request_id if isinstance(request_id, str) and request_id else mutation_id,
        "node_ids": node_ids,
        "edge_ids": edge_ids,
        "group_ids": group_ids,
    }


def _as_utc(value: datetime) -> datetime:
    if value.tzinfo is None:
        return value.replace(tzinfo=UTC)
    return value.astimezone(UTC)


__all__ = [
    "APPLY_GRAPH_TOOL_NAME",
    "CANCEL_WORKFLOW_RUN_TOOL_NAME",
    "DISCARD_PROPOSAL_TOOL_NAME",
    "FOCUS_CANVAS_TOOL_NAME",
    "GET_NODE_DETAIL_TOOL_NAME",
    "MAX_CANVAS_FOCUS_ITEMS",
    "PROPOSE_GRAPH_TOOL_NAME",
    "AgentToolReconcileResult",
    "apply_agent_graph_change_set_tool",
    "cancel_agent_workflow_run_tool",
    "canvas_focus_for_turn",
    "canvas_focus_for_turns",
    "discard_agent_graph_proposal_tool",
    "focus_agent_canvas_items_tool",
    "get_agent_node_detail",
    "propose_agent_graph_change_set_tool",
    "reconcile_agent_canvas_focus_tool",
    "reconcile_agent_graph_change_set_tool",
    "reconcile_agent_graph_proposal_discard_tool",
    "reconcile_agent_workflow_run_cancel_tool",
]
