"""Agent 图提案：提案不是 live 图；确认后才经 Graph Command 写入。"""

from __future__ import annotations

from dataclasses import dataclass
from typing import Any

from pydantic import TypeAdapter, ValidationError
from sqlalchemy import select
from sqlalchemy.orm import Session

from productflow_backend.application.product_workflow.graph_commands import (
    AppliedGraph,
    apply_graph_change_set,
    get_active_workflow_graph,
    load_applied_graph,
    preview_applied_graph_change_set,
    stage_apply_graph_change_set,
)
from productflow_backend.application.product_workflow.graph_contracts import WorkflowChangeSet
from productflow_backend.application.time import now_utc
from productflow_backend.domain.enums import GraphActorType, GraphNodeType, GraphProposalStatus
from productflow_backend.domain.errors import BusinessValidationError, ConflictError, NotFoundError
from productflow_backend.infrastructure.db.models import (
    AgentConversation,
    WorkflowGraph,
    WorkflowGraphProposal,
)


@dataclass(frozen=True, slots=True)
class GraphProposalNodeView:
    id: str
    node_type: GraphNodeType
    title: str
    position_x: int
    position_y: int
    group_id: str | None
    config: dict[str, Any]


@dataclass(frozen=True, slots=True)
class GraphProposalEdgeView:
    id: str
    source_node_id: str
    target_node_id: str
    role: str
    data_type: str
    order: int


@dataclass(frozen=True, slots=True)
class GraphProposalView:
    id: str
    summary: str
    base_graph_revision: int
    stale: bool
    added_nodes: tuple[GraphProposalNodeView, ...]
    added_edges: tuple[GraphProposalEdgeView, ...]
    deleted_node_ids: tuple[str, ...]
    deleted_edge_ids: tuple[str, ...]
    changed_node_ids: tuple[str, ...]


def parse_agent_change_set(value: dict[str, Any]) -> WorkflowChangeSet:
    payload = dict(value)
    payload["actor_type"] = GraphActorType.AGENT
    try:
        return WorkflowChangeSet.model_validate(payload)
    except ValidationError as exc:
        raise BusinessValidationError("图 ChangeSet 无效") from exc


def apply_agent_graph_change_set(
    session: Session,
    *,
    conversation_id: str,
    change_set: WorkflowChangeSet,
    commit: bool = True,
) -> WorkflowGraph:
    """立即写入只接受一条可逆命令；真正写库仍走 Graph Command。"""

    if len(change_set.operations) != 1:
        raise BusinessValidationError("立即写入只接受一条可逆改图命令；多步改图请提交提案")
    graph = _live_graph_for_conversation(session, conversation_id)
    parsed = WorkflowChangeSet(
        base_graph_revision=change_set.base_graph_revision,
        summary=change_set.summary,
        actor_type=GraphActorType.AGENT,
        operations=change_set.operations,
    )
    command = apply_graph_change_set if commit else stage_apply_graph_change_set
    result = command(
        session,
        product_id=graph.product_id,
        graph_id=graph.id,
        change_set=parsed,
    )
    return result.graph


def propose_graph_change_set(
    session: Session,
    *,
    conversation_id: str,
    change_set: WorkflowChangeSet,
    commit: bool = True,
) -> WorkflowGraphProposal:
    """校验后只存 PENDING 提案，不改 live 图。"""

    graph = session.scalar(
        select(WorkflowGraph)
        .where(
            WorkflowGraph.product_id == _conversation_product_id(session, conversation_id),
            WorkflowGraph.active.is_(True),
        )
        .with_for_update()
    )
    if graph is None:
        raise ConflictError("当前商品没有可编辑的工作流")
    parsed = WorkflowChangeSet(
        base_graph_revision=change_set.base_graph_revision,
        summary=change_set.summary,
        actor_type=GraphActorType.AGENT,
        operations=change_set.operations,
    )
    if parsed.base_graph_revision != graph.revision:
        raise ConflictError("图 revision 已变化，请刷新后重试")
    pending = _pending_proposal(session, graph.id)
    if pending is not None:
        raise ConflictError("已有未应用的图提案，请先确认或取消")
    before = load_applied_graph(session, graph)
    try:
        preview_applied_graph_change_set(before, parsed)
    except BusinessValidationError as exc:
        raise ConflictError(f"图提案无法应用到当前工作流: {exc}") from exc
    proposal = WorkflowGraphProposal(
        graph_id=graph.id,
        conversation_id=conversation_id,
        status=GraphProposalStatus.PENDING,
        summary=parsed.summary,
        base_graph_revision=graph.revision,
        change_set_json=parsed.model_dump(mode="json"),
    )
    session.add(proposal)
    session.flush()
    if commit:
        session.commit()
        session.expire_all()
        loaded = session.get(WorkflowGraphProposal, proposal.id)
        assert loaded is not None
        return loaded
    return proposal


def confirm_graph_proposal(
    session: Session,
    *,
    product_id: str,
    graph_id: str,
    proposal_id: str,
) -> WorkflowGraph:
    """确认提案时 Graph Command 只 stage，本函数一次性 commit 提案状态和 live 图。"""

    graph = session.scalar(
        select(WorkflowGraph)
        .where(WorkflowGraph.id == graph_id, WorkflowGraph.product_id == product_id)
        .with_for_update()
    )
    if graph is None:
        raise NotFoundError("商品工作流不存在")
    proposal = session.scalar(
        select(WorkflowGraphProposal)
        .where(WorkflowGraphProposal.id == proposal_id, WorkflowGraphProposal.graph_id == graph.id)
        .with_for_update()
    )
    if proposal is None:
        raise NotFoundError("图提案不存在")
    if GraphProposalStatus(proposal.status) != GraphProposalStatus.PENDING:
        raise ConflictError("图提案已经结束")
    if proposal.base_graph_revision != graph.revision:
        raise ConflictError("图 revision 已变化，请刷新后重试")
    parsed = TypeAdapter(WorkflowChangeSet).validate_python(proposal.change_set_json)
    parsed = WorkflowChangeSet(
        base_graph_revision=graph.revision,
        summary=parsed.summary,
        actor_type=GraphActorType.AGENT,
        operations=parsed.operations,
    )
    result = stage_apply_graph_change_set(
        session,
        product_id=product_id,
        graph_id=graph.id,
        change_set=parsed,
    )
    proposal.status = GraphProposalStatus.CONFIRMED
    proposal.resolved_at = now_utc()
    proposal.operation_group_id = result.operation_group.id
    session.commit()
    session.expire_all()
    return get_active_workflow_graph(session, product_id=product_id) or result.graph


def discard_graph_proposal(
    session: Session,
    *,
    product_id: str,
    graph_id: str,
    proposal_id: str,
) -> None:
    graph = session.scalar(
        select(WorkflowGraph).where(WorkflowGraph.id == graph_id, WorkflowGraph.product_id == product_id)
    )
    if graph is None:
        raise NotFoundError("商品工作流不存在")
    proposal = session.scalar(
        select(WorkflowGraphProposal)
        .where(WorkflowGraphProposal.id == proposal_id, WorkflowGraphProposal.graph_id == graph.id)
        .with_for_update()
    )
    if proposal is None:
        raise NotFoundError("图提案不存在")
    if GraphProposalStatus(proposal.status) != GraphProposalStatus.PENDING:
        raise ConflictError("图提案已经结束")
    proposal.status = GraphProposalStatus.DISCARDED
    proposal.resolved_at = now_utc()
    session.commit()


def discard_graph_proposal_for_conversation(
    session: Session,
    *,
    conversation_id: str,
    proposal_id: str,
) -> None:
    graph = _live_graph_for_conversation(session, conversation_id)
    discard_graph_proposal(
        session,
        product_id=graph.product_id,
        graph_id=graph.id,
        proposal_id=proposal_id,
    )


def confirm_graph_proposal_for_conversation(
    session: Session,
    *,
    conversation_id: str,
    proposal_id: str,
) -> WorkflowGraph:
    graph = _live_graph_for_conversation(session, conversation_id)
    return confirm_graph_proposal(
        session,
        product_id=graph.product_id,
        graph_id=graph.id,
        proposal_id=proposal_id,
    )


def pending_proposal_view(session: Session, graph: WorkflowGraph, applied: AppliedGraph) -> GraphProposalView | None:
    """把提案投影到当前图上作预览；stale 或非法时不写入。"""

    proposal = _pending_proposal(session, graph.id)
    if proposal is None:
        return None
    stale = proposal.base_graph_revision != applied.revision
    added_nodes: tuple[GraphProposalNodeView, ...] = ()
    added_edges: tuple[GraphProposalEdgeView, ...] = ()
    deleted_node_ids: tuple[str, ...] = ()
    deleted_edge_ids: tuple[str, ...] = ()
    changed_node_ids: tuple[str, ...] = ()
    if not stale:
        try:
            change_set = TypeAdapter(WorkflowChangeSet).validate_python(proposal.change_set_json)
            after = preview_applied_graph_change_set(applied, change_set)
        except (ValidationError, BusinessValidationError, ConflictError):
            stale = True
        else:
            before_nodes = {node.id: node for node in applied.nodes}
            after_nodes = {node.id: node for node in after.nodes}
            before_edges = {edge.id: edge for edge in applied.edges}
            after_edges = {edge.id: edge for edge in after.edges}
            added_nodes = tuple(
                GraphProposalNodeView(
                    id=node.id,
                    node_type=node.node_type,
                    title=node.title,
                    position_x=node.position_x,
                    position_y=node.position_y,
                    group_id=node.group_id,
                    config=dict(node.config),
                )
                for node in after.nodes
                if node.id not in before_nodes
            )
            added_edges = tuple(
                GraphProposalEdgeView(
                    id=edge.id,
                    source_node_id=edge.source_node_id,
                    target_node_id=edge.target_node_id,
                    role=edge.role.value,
                    data_type=edge.data_type.value,
                    order=edge.order,
                )
                for edge in after.edges
                if edge.id not in before_edges
            )
            deleted_node_ids = tuple(node_id for node_id in before_nodes if node_id not in after_nodes)
            deleted_edge_ids = tuple(edge_id for edge_id in before_edges if edge_id not in after_edges)
            changed_node_ids = tuple(
                node.id
                for node in after.nodes
                if node.id in before_nodes
                and (
                    node.title != before_nodes[node.id].title
                    or node.config != before_nodes[node.id].config
                    or node.position_x != before_nodes[node.id].position_x
                    or node.position_y != before_nodes[node.id].position_y
                    or node.group_id != before_nodes[node.id].group_id
                    or node.bound_asset_id != before_nodes[node.id].bound_asset_id
                )
            )
    return GraphProposalView(
        id=proposal.id,
        summary=proposal.summary,
        base_graph_revision=proposal.base_graph_revision,
        stale=stale,
        added_nodes=added_nodes,
        added_edges=added_edges,
        deleted_node_ids=deleted_node_ids,
        deleted_edge_ids=deleted_edge_ids,
        changed_node_ids=changed_node_ids,
    )


def _pending_proposal(session: Session, graph_id: str) -> WorkflowGraphProposal | None:
    return session.scalar(
        select(WorkflowGraphProposal).where(
            WorkflowGraphProposal.graph_id == graph_id,
            WorkflowGraphProposal.status == GraphProposalStatus.PENDING,
        )
    )


def _conversation_product_id(session: Session, conversation_id: str) -> str:
    conversation = session.get(AgentConversation, conversation_id)
    if conversation is None or conversation.product_id is None:
        raise ConflictError("当前 Agent conversation 不是商品工作流作用域")
    return conversation.product_id


def _live_graph_for_conversation(session: Session, conversation_id: str) -> WorkflowGraph:
    product_id = _conversation_product_id(session, conversation_id)
    graph = get_active_workflow_graph(session, product_id=product_id)
    if graph is None:
        raise ConflictError("当前商品没有可编辑的工作流")
    return graph


__all__ = [
    "GraphProposalEdgeView",
    "GraphProposalNodeView",
    "GraphProposalView",
    "apply_agent_graph_change_set",
    "confirm_graph_proposal",
    "confirm_graph_proposal_for_conversation",
    "discard_graph_proposal",
    "discard_graph_proposal_for_conversation",
    "parse_agent_change_set",
    "pending_proposal_view",
    "propose_graph_change_set",
]
