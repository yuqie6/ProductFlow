from __future__ import annotations

from dataclasses import dataclass
from typing import Literal

from sqlalchemy.orm import Session

from productflow_backend.application.product_workflow.graph_apply import (
    EMPTY_GRAPH,
    AppliedGraph,
    apply_workflow_change_set,
)
from productflow_backend.application.product_workflow.graph_commands import (
    GraphCommandResult,
    apply_graph_change_set,
    get_active_workflow_graph,
    load_applied_graph,
    stage_new_workflow_graph,
)
from productflow_backend.application.product_workflow.graph_contracts import (
    ConnectNodesOp,
    CreateGroupOp,
    CreateNodeOp,
    GraphOperation,
    WorkflowChangeSet,
)
from productflow_backend.application.workflow_recipes.contracts import RecipePayload
from productflow_backend.domain.enums import GraphActorType, GraphNodeType, WorkflowRecipeKind
from productflow_backend.domain.errors import BusinessValidationError, ConflictError
from productflow_backend.infrastructure.db.models import Product, new_id

RecipeApplyMode = Literal["create", "merge"]


@dataclass(frozen=True, slots=True)
class RecipePreviewNode:
    key: str
    node_type: GraphNodeType
    title: str
    position_x: int
    position_y: int


@dataclass(frozen=True, slots=True)
class RecipePreviewEdge:
    key: str
    source_node_key: str
    target_node_key: str
    role: str
    data_type: str
    order: int


@dataclass(frozen=True, slots=True)
class RecipePreviewGroup:
    key: str
    title: str
    member_keys: tuple[str, ...]


@dataclass(frozen=True, slots=True)
class RecipeApplyPreview:
    mode: RecipeApplyMode
    recipe_id: str
    recipe_version: int
    nodes: tuple[RecipePreviewNode, ...]
    edges: tuple[RecipePreviewEdge, ...]
    groups: tuple[RecipePreviewGroup, ...]


def build_recipe_change_set(
    payload: RecipePayload,
    *,
    base_graph_revision: int,
    summary: str,
    target_product_id: str | None,
    fact_set_version_id: str | None,
    position_offset: tuple[int, int] = (0, 0),
    actor_type: GraphActorType = GraphActorType.RECIPE,
) -> WorkflowChangeSet:
    token = new_id().replace("-", "")[:10]
    node_refs = {node.key: f"rn{token}{index:03d}" for index, node in enumerate(payload.nodes)}
    group_refs = {group.key: f"rg{token}{index:03d}" for index, group in enumerate(payload.groups)}
    operations: list[GraphOperation] = [
        CreateGroupOp(client_ref=group_refs[group.key], title=group.title, member_refs=())
        for group in payload.groups
    ]
    offset_x, offset_y = position_offset
    for node in payload.nodes:
        config = dict(node.config)
        if node.node_type == GraphNodeType.PRODUCT_SOURCE and target_product_id:
            config["source_product_id"] = target_product_id
            config["fact_set_version_id"] = fact_set_version_id
        operations.append(
            CreateNodeOp(
                client_ref=node_refs[node.key],
                node_type=node.node_type,
                title=node.title,
                position_x=node.position_x + offset_x,
                position_y=node.position_y + offset_y,
                config=config,
                bound_asset_id=None,
                group_ref=group_refs.get(node.group_key) if node.group_key else None,
            )
        )
    for index, edge in enumerate(payload.edges):
        operations.append(
            ConnectNodesOp(
                client_ref=f"re{token}{index:03d}",
                source_ref=node_refs[edge.source_node_key],
                target_ref=node_refs[edge.target_node_key],
                order=edge.order,
            )
        )
    if not operations:
        raise BusinessValidationError("配方没有可写入的节点或连线")
    return WorkflowChangeSet(
        base_graph_revision=base_graph_revision,
        summary=summary[:500],
        actor_type=actor_type,
        operations=operations,
    )


def preview_recipe_payload(
    session: Session,
    *,
    product_id: str,
    recipe_id: str,
    payload: RecipePayload,
    recipe_kind: WorkflowRecipeKind,
    recipe_version: int,
    summary: str,
) -> RecipeApplyPreview:
    product, existing, mode, offset = _target_graph_state(
        session,
        product_id=product_id,
        recipe_kind=recipe_kind,
    )
    change_set = build_recipe_change_set(
        payload,
        base_graph_revision=existing.revision,
        summary=summary,
        target_product_id=product.id,
        fact_set_version_id=product.current_fact_set_version_id,
        position_offset=offset,
    )
    try:
        after = apply_workflow_change_set(existing, change_set)
    except BusinessValidationError as exc:
        raise ConflictError(f"配方无法合并进当前工作流: {exc}") from exc
    existing_node_ids = {node.id for node in existing.nodes}
    existing_edge_ids = {edge.id for edge in existing.edges}
    existing_group_ids = {group.id for group in existing.groups}
    added_nodes = [node for node in after.nodes if node.id not in existing_node_ids]
    added_edges = [edge for edge in after.edges if edge.id not in existing_edge_ids]
    added_groups = [group for group in after.groups if group.id not in existing_group_ids]
    node_key = {node.id: node.id for node in added_nodes}
    return RecipeApplyPreview(
        mode=mode,
        recipe_id=recipe_id,
        recipe_version=recipe_version,
        nodes=tuple(
            RecipePreviewNode(
                key=node.id,
                node_type=node.node_type,
                title=node.title,
                position_x=node.position_x,
                position_y=node.position_y,
            )
            for node in added_nodes
        ),
        edges=tuple(
            RecipePreviewEdge(
                key=edge.id,
                source_node_key=node_key.get(edge.source_node_id, edge.source_node_id),
                target_node_key=node_key.get(edge.target_node_id, edge.target_node_id),
                role=edge.role.value,
                data_type=edge.data_type.value,
                order=edge.order,
            )
            for edge in added_edges
        ),
        groups=tuple(
            RecipePreviewGroup(
                key=group.id,
                title=group.title,
                member_keys=tuple(node.id for node in added_nodes if node.group_id == group.id),
            )
            for group in added_groups
        ),
    )


def apply_recipe_payload(
    session: Session,
    *,
    product_id: str,
    recipe_kind: WorkflowRecipeKind,
    payload: RecipePayload,
    summary: str,
    commit: bool = False,
) -> tuple[GraphCommandResult, RecipeApplyMode, tuple[str, ...], tuple[str, ...]]:
    product, existing, mode, offset = _target_graph_state(
        session,
        product_id=product_id,
        recipe_kind=recipe_kind,
    )
    change_set = build_recipe_change_set(
        payload,
        base_graph_revision=existing.revision,
        summary=summary,
        target_product_id=product.id,
        fact_set_version_id=product.current_fact_set_version_id,
        position_offset=offset,
    )
    before_node_ids = {node.id for node in existing.nodes}
    before_edge_ids = {edge.id for edge in existing.edges}
    try:
        if mode == "create":
            command = stage_new_workflow_graph(
                session,
                product_id=product_id,
                change_set=change_set,
                title=summary[:255] or "商品创意工作流",
            )
        else:
            live = get_active_workflow_graph(session, product_id=product_id)
            if live is None:
                raise ConflictError("商品没有可写入的 schema-v3 工作流")
            command = apply_graph_change_set(
                session,
                product_id=product_id,
                graph_id=live.id,
                change_set=change_set,
                commit=commit,
            )
    except BusinessValidationError as exc:
        raise ConflictError(f"配方无法合并进当前工作流: {exc}") from exc
    added_nodes = tuple(node.id for node in command.applied.nodes if node.id not in before_node_ids)
    added_edges = tuple(edge.id for edge in command.applied.edges if edge.id not in before_edge_ids)
    return command, mode, added_nodes, added_edges


def merge_position_offset(graph: AppliedGraph) -> tuple[int, int]:
    if not graph.nodes:
        return (0, 0)
    return (max(node.position_x for node in graph.nodes) + 280, 0)


def _target_graph_state(
    session: Session,
    *,
    product_id: str,
    recipe_kind: WorkflowRecipeKind,
) -> tuple[Product, AppliedGraph, RecipeApplyMode, tuple[int, int]]:
    product = session.get(Product, product_id)
    if product is None:
        raise ConflictError("商品不存在")
    live = get_active_workflow_graph(session, product_id=product_id)
    if live is None:
        if recipe_kind == WorkflowRecipeKind.RECIPE_FRAGMENT:
            raise ConflictError("片段配方需要已有 schema-v3 工作流")
        return product, EMPTY_GRAPH, "create", (0, 0)
    if recipe_kind != WorkflowRecipeKind.RECIPE_FRAGMENT:
        raise ConflictError("完整配方不能合并进已有工作流")
    existing = load_applied_graph(session, live)
    return product, existing, "merge", merge_position_offset(existing)


def recipe_application_summary(title: str) -> str:
    text = title.strip() or "工作流配方"
    return f"应用配方：{text}"[:500]


__all__ = [
    "RecipeApplyMode",
    "RecipeApplyPreview",
    "RecipePreviewEdge",
    "RecipePreviewGroup",
    "RecipePreviewNode",
    "apply_recipe_payload",
    "build_recipe_change_set",
    "preview_recipe_payload",
    "recipe_application_summary",
]
