from __future__ import annotations

from dataclasses import dataclass

from sqlalchemy import select
from sqlalchemy.orm import Session

from productflow_backend.application.product_workflow.graph_apply import AppliedGraph, AppliedGraphNode
from productflow_backend.application.product_workflow.graph_commands import (
    get_active_workflow_graph,
    get_workflow_graph,
    last_operation_group,
    load_applied_graph,
)
from productflow_backend.application.product_workflow.graph_compiler import (
    GraphSourceRecord,
    compile_context_runtime,
    compile_image_runtime,
    compile_prompt_runtime,
)
from productflow_backend.application.product_workflow.product_sources import ProductSourceSnapshot
from productflow_backend.domain.enums import (
    GraphArtifactType,
    GraphConfigStatus,
    GraphEdgeDataType,
    GraphEdgeRole,
    GraphHistoryKind,
    GraphNodeType,
)
from productflow_backend.domain.errors import BusinessValidationError, NotFoundError
from productflow_backend.domain.graph_catalog import PROCESSING_NODE_TYPES
from productflow_backend.infrastructure.db.models import (
    WorkflowGraph,
    WorkflowGraphArtifact,
    WorkflowGraphNode,
    WorkflowOperationGroup,
)


@dataclass(frozen=True, slots=True)
class GraphEdgeView:
    id: str
    source_node_id: str
    target_node_id: str
    data_type: GraphEdgeDataType
    role: GraphEdgeRole
    order: int


@dataclass(frozen=True, slots=True)
class GraphEdgeSummary:
    id: str
    node_id: str
    data_type: GraphEdgeDataType
    role: GraphEdgeRole
    order: int


@dataclass(frozen=True, slots=True)
class GraphNodeView:
    id: str
    node_type: GraphNodeType
    title: str
    position_x: int
    position_y: int
    config: dict[str, object]
    product_source: ProductSourceSnapshot | None
    bound_asset_id: str | None
    group_id: str | None
    preview_asset_id: str | None
    config_status: GraphConfigStatus
    unused: bool
    current_artifact_id: str | None
    current_artifact_type: GraphArtifactType | None
    current_artifact_payload: dict[str, object] | None
    incoming: tuple[GraphEdgeSummary, ...]
    outgoing: tuple[GraphEdgeSummary, ...]


@dataclass(frozen=True, slots=True)
class GraphGroupView:
    id: str
    title: str
    member_ids: tuple[str, ...]


@dataclass(frozen=True, slots=True)
class GraphProjection:
    id: str
    product_id: str
    title: str
    schema_version: int
    revision: int
    source_draft_revision_id: str | None
    last_operation_group_id: str | None
    can_undo: bool
    can_redo: bool
    nodes: tuple[GraphNodeView, ...]
    edges: tuple[GraphEdgeView, ...]
    groups: tuple[GraphGroupView, ...]


def get_graph_projection(session: Session, *, product_id: str, graph_id: str) -> GraphProjection:
    graph = get_workflow_graph(session, product_id=product_id, graph_id=graph_id)
    return project_workflow_graph(session, graph)


def get_active_graph_projection(session: Session, *, product_id: str) -> GraphProjection:
    graph = get_active_workflow_graph(session, product_id=product_id)
    if graph is None:
        raise NotFoundError("商品工作流不存在")
    return project_workflow_graph(session, graph)


def project_workflow_graph(session: Session, graph: WorkflowGraph) -> GraphProjection:
    applied = load_applied_graph(session, graph)
    last = last_operation_group(session, graph)
    from productflow_backend.application.product_workflow.graph_runs import load_graph_sources

    return build_graph_projection(
        graph,
        applied,
        last_operation_group_id=last.id if last is not None else None,
        can_undo=_can_undo(last),
        can_redo=_can_redo(last),
        preview_asset_ids=_preview_asset_ids(session, graph.id),
        artifact_input_digests=_artifact_input_digests(session, graph.id),
        sources=load_graph_sources(session, graph, applied),
    )


def build_graph_projection(
    graph: WorkflowGraph,
    applied: AppliedGraph,
    *,
    last_operation_group_id: str | None,
    can_undo: bool = False,
    can_redo: bool = False,
    preview_asset_ids: dict[str, str] | None = None,
    artifact_input_digests: dict[str, str] | None = None,
    sources: dict[str, GraphSourceRecord] | None = None,
) -> GraphProjection:
    outgoing_ids = {edge.source_node_id for edge in applied.edges}
    member_ids: dict[str, list[str]] = {group.id: [] for group in applied.groups}
    for node in applied.nodes:
        if node.group_id is not None:
            member_ids.setdefault(node.group_id, []).append(node.id)
    return GraphProjection(
        id=graph.id,
        product_id=graph.product_id,
        title=graph.title,
        schema_version=graph.schema_version,
        revision=applied.revision,
        source_draft_revision_id=graph.source_draft_revision_id,
        last_operation_group_id=last_operation_group_id,
        can_undo=can_undo,
        can_redo=can_redo,
        nodes=tuple(
            _project_node(
                applied,
                node,
                unused=_is_unused(node, outgoing_ids),
                preview_asset_id=(preview_asset_ids or {}).get(node.id),
                artifact_input_digest=(artifact_input_digests or {}).get(node.id),
                sources=sources,
            )
            for node in applied.nodes
        ),
        edges=tuple(
            GraphEdgeView(
                id=edge.id,
                source_node_id=edge.source_node_id,
                target_node_id=edge.target_node_id,
                data_type=edge.data_type,
                role=edge.role,
                order=edge.order,
            )
            for edge in applied.edges
        ),
        groups=tuple(
            GraphGroupView(id=group.id, title=group.title, member_ids=tuple(member_ids.get(group.id, ())))
            for group in applied.groups
        ),
    )


def _project_node(
    applied: AppliedGraph,
    node: AppliedGraphNode,
    *,
    unused: bool,
    preview_asset_id: str | None,
    artifact_input_digest: str | None = None,
    sources: dict[str, GraphSourceRecord] | None = None,
) -> GraphNodeView:
    incoming = tuple(
        GraphEdgeSummary(
            id=edge.id,
            node_id=edge.source_node_id,
            data_type=edge.data_type,
            role=edge.role,
            order=edge.order,
        )
        for edge in applied.edges
        if edge.target_node_id == node.id
    )
    outgoing = tuple(
        GraphEdgeSummary(
            id=edge.id,
            node_id=edge.target_node_id,
            data_type=edge.data_type,
            role=edge.role,
            order=edge.order,
        )
        for edge in applied.edges
        if edge.source_node_id == node.id
    )
    record = (sources or {}).get(node.id)
    return GraphNodeView(
        id=node.id,
        node_type=node.node_type,
        title=node.title,
        position_x=node.position_x,
        position_y=node.position_y,
        config=dict(node.config),
        product_source=(sources or {}).get(node.id).product_source
        if node.node_type == GraphNodeType.PRODUCT_SOURCE and (sources or {}).get(node.id) is not None
        else None,
        bound_asset_id=node.bound_asset_id,
        group_id=node.group_id,
        preview_asset_id=preview_asset_id,
        config_status=_config_status_with_stale(
            applied,
            node,
            artifact_input_digest=artifact_input_digest,
            sources=sources,
        ),
        unused=unused,
        current_artifact_id=record.current_artifact_id if record is not None else None,
        current_artifact_type=record.current_artifact_type if record is not None else None,
        current_artifact_payload=dict(record.current_artifact_payload)
        if record is not None and record.current_artifact_payload is not None
        else None,
        incoming=incoming,
        outgoing=outgoing,
    )


def _preview_asset_ids(session: Session, graph_id: str) -> dict[str, str]:
    rows = session.scalars(select(WorkflowGraphNode).where(WorkflowGraphNode.graph_id == graph_id)).all()
    artifact_ids = [row.current_artifact_id for row in rows if row.current_artifact_id]
    artifacts = {
        artifact.id: artifact
        for artifact in session.scalars(
            select(WorkflowGraphArtifact).where(WorkflowGraphArtifact.id.in_(artifact_ids))
        ).all()
    } if artifact_ids else {}
    previews: dict[str, str] = {}
    for row in rows:
        if row.bound_image_asset_id:
            previews[row.id] = row.bound_image_asset_id
            continue
        artifact = artifacts.get(row.current_artifact_id) if row.current_artifact_id else None
        if artifact is not None and artifact.product_image_asset_id:
            previews[row.id] = artifact.product_image_asset_id
    return previews


def _is_unused(node: AppliedGraphNode, outgoing_ids: set[str]) -> bool:
    return node.node_type == GraphNodeType.IMAGE_ASSET and node.id not in outgoing_ids


def _can_undo(last: WorkflowOperationGroup | None) -> bool:
    return last is not None and GraphHistoryKind(last.history_kind) != GraphHistoryKind.UNDO


def _can_redo(last: WorkflowOperationGroup | None) -> bool:
    return last is not None and GraphHistoryKind(last.history_kind) == GraphHistoryKind.UNDO


def _artifact_input_digests(session: Session, graph_id: str) -> dict[str, str]:
    rows = session.scalars(select(WorkflowGraphNode).where(WorkflowGraphNode.graph_id == graph_id)).all()
    artifact_ids = [row.current_artifact_id for row in rows if row.current_artifact_id]
    if not artifact_ids:
        return {}
    artifacts = {
        artifact.id: artifact.input_digest
        for artifact in session.scalars(
            select(WorkflowGraphArtifact).where(WorkflowGraphArtifact.id.in_(artifact_ids))
        ).all()
    }
    return {
        row.id: artifacts[row.current_artifact_id]
        for row in rows
        if row.current_artifact_id and row.current_artifact_id in artifacts
    }


def _config_status_with_stale(
    applied: AppliedGraph,
    node: AppliedGraphNode,
    *,
    artifact_input_digest: str | None,
    sources: dict[str, GraphSourceRecord] | None,
) -> GraphConfigStatus:
    status = applied.config_status(node.id)
    if (
        status != GraphConfigStatus.READY
        or node.node_type not in PROCESSING_NODE_TYPES
        or not artifact_input_digest
        or sources is None
    ):
        return status
    try:
        if node.node_type == GraphNodeType.PROMPT_GENERATION:
            runtime = compile_prompt_runtime(applied, node.id, sources)
        elif node.node_type == GraphNodeType.IMAGE_GENERATION:
            runtime = compile_image_runtime(applied, node.id, sources)
        elif node.node_type in {GraphNodeType.CREATIVE_BRIEF, GraphNodeType.VISUAL_SYSTEM}:
            runtime = compile_context_runtime(applied, node.id, sources)
        else:
            return status
    except BusinessValidationError:
        return status
    if runtime.input_digest != artifact_input_digest:
        return GraphConfigStatus.STALE
    return status
