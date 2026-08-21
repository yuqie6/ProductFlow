from __future__ import annotations

from dataclasses import dataclass, replace

from pydantic import TypeAdapter
from sqlalchemy import select
from sqlalchemy.orm import Session

from productflow_backend.application.product_workflow.graph_apply import (
    EMPTY_GRAPH,
    AppliedGraph,
    AppliedGraphEdge,
    AppliedGraphGroup,
    AppliedGraphNode,
    apply_workflow_change_set,
    invert_applied_graph,
)
from productflow_backend.application.product_workflow.graph_contracts import GraphOperation, WorkflowChangeSet
from productflow_backend.application.time import now_utc
from productflow_backend.domain.enums import GraphActorType, GraphEdgeDataType, GraphEdgeRole, GraphNodeType
from productflow_backend.domain.errors import BusinessValidationError, ConflictError, NotFoundError
from productflow_backend.infrastructure.db.models import (
    GRAPH_SCHEMA_VERSION,
    Product,
    ProductImageAsset,
    WorkflowGraph,
    WorkflowGraphEdge,
    WorkflowGraphGroup,
    WorkflowGraphNode,
    WorkflowOperationGroup,
    new_id,
)

DEFAULT_GRAPH_TITLE = "商品创意工作流"


@dataclass(frozen=True, slots=True)
class GraphCommandResult:
    graph: WorkflowGraph
    applied: AppliedGraph
    operation_group: WorkflowOperationGroup


def get_workflow_graph(session: Session, *, product_id: str, graph_id: str) -> WorkflowGraph:
    graph = session.scalar(
        select(WorkflowGraph).where(WorkflowGraph.id == graph_id, WorkflowGraph.product_id == product_id)
    )
    if graph is None:
        raise NotFoundError("商品工作流不存在")
    return graph


def get_active_workflow_graph(session: Session, *, product_id: str) -> WorkflowGraph | None:
    return session.scalar(
        select(WorkflowGraph).where(WorkflowGraph.product_id == product_id, WorkflowGraph.active.is_(True))
    )


def load_applied_graph(session: Session, graph: WorkflowGraph) -> AppliedGraph:
    groups = session.scalars(
        select(WorkflowGraphGroup)
        .where(WorkflowGraphGroup.graph_id == graph.id)
        .order_by(WorkflowGraphGroup.sort_order, WorkflowGraphGroup.id)
    ).all()
    nodes = session.scalars(
        select(WorkflowGraphNode).where(WorkflowGraphNode.graph_id == graph.id).order_by(WorkflowGraphNode.id)
    ).all()
    edges = session.scalars(
        select(WorkflowGraphEdge).where(WorkflowGraphEdge.graph_id == graph.id).order_by(WorkflowGraphEdge.id)
    ).all()
    return AppliedGraph(
        revision=graph.revision,
        nodes=tuple(
            AppliedGraphNode(
                id=node.id,
                node_type=GraphNodeType(node.node_type),
                title=node.title,
                position_x=node.position_x,
                position_y=node.position_y,
                config=dict(node.config_json or {}),
                bound_asset_id=node.bound_image_asset_id,
                group_id=node.group_id,
            )
            for node in nodes
        ),
        edges=tuple(
            AppliedGraphEdge(
                id=edge.id,
                source_node_id=edge.source_node_id,
                target_node_id=edge.target_node_id,
                data_type=GraphEdgeDataType(edge.data_type),
                role=GraphEdgeRole(edge.role),
                order=edge.sort_order,
            )
            for edge in edges
        ),
        groups=tuple(AppliedGraphGroup(id=group.id, title=group.title) for group in groups),
    )


def stage_new_workflow_graph(
    session: Session,
    *,
    product_id: str,
    change_set: WorkflowChangeSet,
    title: str = DEFAULT_GRAPH_TITLE,
    source_draft_revision_id: str | None = None,
) -> GraphCommandResult:
    if change_set.base_graph_revision != 0:
        raise ConflictError("新建图的 base_graph_revision 必须为 0")
    _lock_product(session, product_id)
    if get_active_workflow_graph(session, product_id=product_id) is not None:
        raise ConflictError("商品已有 active schema-v3 工作流")
    applied = apply_workflow_change_set(EMPTY_GRAPH, change_set)
    applied = assign_persistent_ids(EMPTY_GRAPH, applied)
    _validate_bound_assets(session, product_id=product_id, graph=applied)
    graph = WorkflowGraph(
        product_id=product_id,
        title=title,
        active=True,
        schema_version=GRAPH_SCHEMA_VERSION,
        revision=applied.revision,
        source_draft_revision_id=source_draft_revision_id,
    )
    session.add(graph)
    session.flush()
    _replace_graph_contents(session, graph, applied)
    operation_group = _record_operation_group(
        session,
        graph=graph,
        change_set=change_set,
        inverse_operations=invert_applied_graph(EMPTY_GRAPH, applied),
        base_revision=0,
        result_revision=applied.revision,
    )
    graph.updated_at = now_utc()
    session.flush()
    return GraphCommandResult(graph=graph, applied=applied, operation_group=operation_group)


def apply_graph_change_set(
    session: Session,
    *,
    product_id: str,
    graph_id: str,
    change_set: WorkflowChangeSet,
    commit: bool = True,
) -> GraphCommandResult:
    try:
        graph = session.scalar(
            select(WorkflowGraph)
            .where(WorkflowGraph.id == graph_id, WorkflowGraph.product_id == product_id)
            .with_for_update()
        )
        if graph is None:
            raise NotFoundError("商品工作流不存在")
        if not graph.active:
            raise ConflictError("只能修改 active schema-v3 工作流")
        if graph.schema_version != GRAPH_SCHEMA_VERSION:
            raise ConflictError("画布修改只支持 schema-v3 工作流")
        before = load_applied_graph(session, graph)
        proposed = apply_workflow_change_set(before, change_set)
        after = assign_persistent_ids(before, proposed)
        _validate_bound_assets(session, product_id=product_id, graph=after)
        graph.revision = after.revision
        graph.updated_at = now_utc()
        _replace_graph_contents(session, graph, after)
        operation_group = _record_operation_group(
            session,
            graph=graph,
            change_set=change_set,
            inverse_operations=invert_applied_graph(before, after),
            base_revision=before.revision,
            result_revision=after.revision,
        )
        session.flush()
        if commit:
            session.commit()
            session.expire_all()
            graph = get_workflow_graph(session, product_id=product_id, graph_id=graph_id)
            after = load_applied_graph(session, graph)
            operation_group = session.get(WorkflowOperationGroup, operation_group.id)
            assert operation_group is not None
        return GraphCommandResult(graph=graph, applied=after, operation_group=operation_group)
    except Exception:
        session.rollback()
        raise


def undo_last_graph_change_set(
    session: Session,
    *,
    product_id: str,
    graph_id: str,
    commit: bool = True,
) -> GraphCommandResult:
    graph = get_workflow_graph(session, product_id=product_id, graph_id=graph_id)
    last = session.scalar(
        select(WorkflowOperationGroup).where(
            WorkflowOperationGroup.graph_id == graph.id,
            WorkflowOperationGroup.result_revision == graph.revision,
        )
    )
    if last is None:
        raise ConflictError("没有可撤销的图操作")
    inverse = TypeAdapter(list[GraphOperation]).validate_python(last.inverse_operations_json)
    if not inverse:
        raise ConflictError("该操作没有可撤销的 inverse")
    summary = f"撤销：{last.summary}"
    return apply_graph_change_set(
        session,
        product_id=product_id,
        graph_id=graph_id,
        change_set=WorkflowChangeSet(
            base_graph_revision=graph.revision,
            summary=summary[:500],
            actor_type=GraphActorType.USER,
            operations=inverse,
        ),
        commit=commit,
    )


def assign_persistent_ids(before: AppliedGraph, after: AppliedGraph) -> AppliedGraph:
    id_map = {node.id: node.id for node in before.nodes}
    id_map.update({edge.id: edge.id for edge in before.edges})
    id_map.update({group.id: group.id for group in before.groups})
    for group in after.groups:
        id_map.setdefault(group.id, new_id())
    for node in after.nodes:
        id_map.setdefault(node.id, new_id())
    for edge in after.edges:
        id_map.setdefault(edge.id, new_id())
    return AppliedGraph(
        revision=after.revision,
        nodes=tuple(
            replace(
                node,
                id=id_map[node.id],
                group_id=id_map[node.group_id] if node.group_id is not None else None,
            )
            for node in after.nodes
        ),
        edges=tuple(
            replace(
                edge,
                id=id_map[edge.id],
                source_node_id=id_map[edge.source_node_id],
                target_node_id=id_map[edge.target_node_id],
            )
            for edge in after.edges
        ),
        groups=tuple(replace(group, id=id_map[group.id]) for group in after.groups),
    )


def _lock_product(session: Session, product_id: str) -> Product:
    product = session.scalar(select(Product).where(Product.id == product_id).with_for_update())
    if product is None:
        raise NotFoundError("商品不存在")
    return product


def _validate_bound_assets(session: Session, *, product_id: str, graph: AppliedGraph) -> None:
    asset_ids = {node.bound_asset_id for node in graph.nodes if node.bound_asset_id}
    if not asset_ids:
        return
    found = set(
        session.scalars(
            select(ProductImageAsset.id).where(
                ProductImageAsset.product_id == product_id,
                ProductImageAsset.id.in_(sorted(asset_ids)),
            )
        ).all()
    )
    if found != asset_ids:
        raise BusinessValidationError("节点绑定了不属于该商品的图片")


def _replace_graph_contents(session: Session, graph: WorkflowGraph, applied: AppliedGraph) -> None:
    existing_groups = {
        group.id: group
        for group in session.scalars(select(WorkflowGraphGroup).where(WorkflowGraphGroup.graph_id == graph.id))
    }
    existing_nodes = {
        node.id: node
        for node in session.scalars(select(WorkflowGraphNode).where(WorkflowGraphNode.graph_id == graph.id))
    }
    existing_edges = {
        edge.id: edge
        for edge in session.scalars(select(WorkflowGraphEdge).where(WorkflowGraphEdge.graph_id == graph.id))
    }
    next_group_ids = {group.id for group in applied.groups}
    next_node_ids = {node.id for node in applied.nodes}
    next_edge_ids = {edge.id for edge in applied.edges}

    for edge_id, edge in existing_edges.items():
        if edge_id not in next_edge_ids:
            session.delete(edge)
    session.flush()

    removed_nodes = [node for node_id, node in existing_nodes.items() if node_id not in next_node_ids]
    for node in removed_nodes:
        node.current_artifact_id = None
    session.flush()
    for node in removed_nodes:
        session.delete(node)
    session.flush()

    for group_id, group in existing_groups.items():
        if group_id not in next_group_ids:
            session.delete(group)
    session.flush()

    group_order = {group.id: index for index, group in enumerate(applied.groups)}
    for group in applied.groups:
        row = existing_groups.get(group.id)
        if row is None:
            session.add(
                WorkflowGraphGroup(
                    id=group.id,
                    graph_id=graph.id,
                    title=group.title,
                    sort_order=group_order[group.id],
                )
            )
            continue
        row.title = group.title
        row.sort_order = group_order[group.id]
    session.flush()

    for node in applied.nodes:
        row = existing_nodes.get(node.id)
        if row is None:
            session.add(
                WorkflowGraphNode(
                    id=node.id,
                    graph_id=graph.id,
                    node_type=node.node_type,
                    title=node.title,
                    position_x=node.position_x,
                    position_y=node.position_y,
                    config_json=dict(node.config),
                    bound_image_asset_id=node.bound_asset_id,
                    group_id=node.group_id,
                )
            )
            continue
        row.node_type = node.node_type
        row.title = node.title
        row.position_x = node.position_x
        row.position_y = node.position_y
        row.config_json = dict(node.config)
        row.bound_image_asset_id = node.bound_asset_id
        row.group_id = node.group_id
    session.flush()

    for edge in applied.edges:
        row = existing_edges.get(edge.id)
        if row is None:
            session.add(
                WorkflowGraphEdge(
                    id=edge.id,
                    graph_id=graph.id,
                    source_node_id=edge.source_node_id,
                    target_node_id=edge.target_node_id,
                    data_type=edge.data_type,
                    role=edge.role,
                    sort_order=edge.order,
                )
            )
            continue
        row.source_node_id = edge.source_node_id
        row.target_node_id = edge.target_node_id
        row.data_type = edge.data_type
        row.role = edge.role
        row.sort_order = edge.order
    session.flush()


def _record_operation_group(
    session: Session,
    *,
    graph: WorkflowGraph,
    change_set: WorkflowChangeSet,
    inverse_operations: list,
    base_revision: int,
    result_revision: int,
) -> WorkflowOperationGroup:
    operation_group = WorkflowOperationGroup(
        graph_id=graph.id,
        actor_type=change_set.actor_type,
        summary=change_set.summary,
        base_revision=base_revision,
        result_revision=result_revision,
        operations_json=[operation.model_dump(mode="json") for operation in change_set.operations],
        inverse_operations_json=[operation.model_dump(mode="json") for operation in inverse_operations],
    )
    session.add(operation_group)
    session.flush()
    return operation_group
