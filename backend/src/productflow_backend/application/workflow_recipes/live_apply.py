from __future__ import annotations

import hashlib
import json
from dataclasses import dataclass
from typing import Any, Literal

from sqlalchemy.orm import Session

from productflow_backend.application.product_workflow.graph_apply import apply_workflow_change_set
from productflow_backend.application.product_workflow.graph_commands import (
    EMPTY_GRAPH,
    AppliedGraph,
    AppliedGraphEdge,
    AppliedGraphGroup,
    AppliedGraphNode,
    GraphCommandResult,
    apply_graph_change_set,
    get_active_workflow_graph,
    load_applied_graph,
    stage_apply_graph_change_set,
    stage_new_workflow_graph,
)
from productflow_backend.application.product_workflow.graph_contracts import (
    ConnectNodesOp,
    CreateGroupOp,
    CreateNodeOp,
    GraphOperation,
    WorkflowChangeSet,
)
from productflow_backend.application.workflow_recipes.contracts import RecipePayload, recipe_payload_hash
from productflow_backend.domain.enums import (
    GraphActorType,
    GraphNodeType,
    WorkflowRecipeKind,
)
from productflow_backend.domain.errors import BusinessValidationError, ConflictError
from productflow_backend.domain.graph_catalog import normalize_node_config, run_required_inputs
from productflow_backend.infrastructure.db.models import Product, WorkflowGraph, new_id

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
class RecipePreviewUpdatedNode:
    id: str
    node_type: GraphNodeType
    title: str
    changed_config_keys: tuple[str, ...]


@dataclass(frozen=True, slots=True)
class RecipeApplyPreview:
    mode: RecipeApplyMode
    recipe_id: str
    recipe_version: int
    base_graph_revision: int
    preview_digest: str
    nodes: tuple[RecipePreviewNode, ...]
    edges: tuple[RecipePreviewEdge, ...]
    groups: tuple[RecipePreviewGroup, ...]
    updated_nodes: tuple[RecipePreviewUpdatedNode, ...]
    required_bindings: tuple[str, ...]


@dataclass(frozen=True, slots=True)
class RecipeApplyPlan:
    mode: RecipeApplyMode
    recipe_id: str
    recipe_version: int
    graph_id: str | None
    existing: AppliedGraph
    change_set: WorkflowChangeSet
    base_graph_revision: int
    preview_digest: str
    nodes: tuple[RecipePreviewNode, ...]
    edges: tuple[RecipePreviewEdge, ...]
    groups: tuple[RecipePreviewGroup, ...]
    updated_nodes: tuple[RecipePreviewUpdatedNode, ...]
    required_bindings: tuple[str, ...]

    def preview(self) -> RecipeApplyPreview:
        return RecipeApplyPreview(
            mode=self.mode,
            recipe_id=self.recipe_id,
            recipe_version=self.recipe_version,
            base_graph_revision=self.base_graph_revision,
            preview_digest=self.preview_digest,
            nodes=self.nodes,
            edges=self.edges,
            groups=self.groups,
            updated_nodes=self.updated_nodes,
            required_bindings=self.required_bindings,
        )


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


def plan_recipe_payload(
    session: Session,
    *,
    product_id: str,
    recipe_id: str,
    payload: RecipePayload,
    recipe_kind: WorkflowRecipeKind,
    recipe_version: int,
    summary: str,
    payload_hash: str | None = None,
    required_bindings: tuple[str, ...] = (),
    expected_graph_revision: int | None = None,
) -> RecipeApplyPlan:
    product, live, existing, mode, offset = _target_graph_state(
        session,
        product_id=product_id,
        recipe_kind=recipe_kind,
    )
    if expected_graph_revision is not None and existing.revision != expected_graph_revision:
        raise ConflictError("工作流已变化，请重新预览后重试")

    semantic: dict[str, Any]
    try:
        change_set = build_recipe_change_set(
            payload,
            base_graph_revision=existing.revision,
            summary=summary,
            target_product_id=product.id,
            fact_set_version_id=product.current_fact_set_version_id,
            position_offset=offset,
        )
        updated_nodes = ()
        connected_inputs: dict[str, list[str]] = {}
        if live is not None:
            change_set, connected_inputs = _attach_existing_shared_inputs(existing, change_set)
        semantic = {
            "operation": "add",
            "offset": [offset[0], offset[1]],
            "payload": _addition_semantic_payload(
                payload,
                target_product_id=product.id,
                fact_set_version_id=product.current_fact_set_version_id,
                position_offset=offset,
            ),
            "connected_inputs": connected_inputs,
        }
    except BusinessValidationError as exc:
        raise ConflictError(f"配方无法合并进当前工作流: {exc}") from exc
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
    _assert_required_bindings(
        after,
        required_bindings=required_bindings,
        affected_node_ids={node.id for node in added_nodes} | {node.id for node in updated_nodes},
    )
    nodes, edges, groups = _preview_additions(
        added_nodes=added_nodes,
        added_edges=added_edges,
        added_groups=added_groups,
    )
    digest = _recipe_preview_digest(
        product_id=product_id,
        recipe_id=recipe_id,
        recipe_version=recipe_version,
        payload_hash=payload_hash or recipe_payload_hash(payload),
        graph_id=live.id if live is not None else None,
        base_graph_revision=existing.revision,
        mode=mode,
        required_bindings=required_bindings,
        semantic=semantic,
    )
    return RecipeApplyPlan(
        mode=mode,
        recipe_id=recipe_id,
        recipe_version=recipe_version,
        graph_id=live.id if live is not None else None,
        existing=existing,
        change_set=change_set,
        base_graph_revision=existing.revision,
        preview_digest=digest,
        nodes=nodes,
        edges=edges,
        groups=groups,
        updated_nodes=tuple(updated_nodes),
        required_bindings=tuple(required_bindings),
    )


def apply_recipe_plan(
    session: Session,
    *,
    product_id: str,
    plan: RecipeApplyPlan,
    commit: bool = False,
) -> tuple[GraphCommandResult, RecipeApplyMode, tuple[str, ...], tuple[str, ...], tuple[str, ...]]:
    before_node_ids = {node.id for node in plan.existing.nodes}
    before_edge_ids = {edge.id for edge in plan.existing.edges}
    try:
        if plan.mode == "create":
            command = stage_new_workflow_graph(
                session,
                product_id=product_id,
                change_set=plan.change_set,
                title=plan.change_set.summary[:255] or "商品创意工作流",
            )
        else:
            live = get_active_workflow_graph(session, product_id=product_id)
            if live is None:
                raise ConflictError("商品没有可写入的 schema-v3 工作流")
            if plan.graph_id != live.id:
                raise ConflictError("工作流已变化，请重新预览后重试")
            command_fn = apply_graph_change_set if commit else stage_apply_graph_change_set
            command = command_fn(
                session,
                product_id=product_id,
                graph_id=live.id,
                change_set=plan.change_set,
            )
    except BusinessValidationError as exc:
        raise ConflictError(f"配方无法合并进当前工作流: {exc}") from exc
    added_nodes = tuple(node.id for node in command.applied.nodes if node.id not in before_node_ids)
    added_edges = tuple(edge.id for edge in command.applied.edges if edge.id not in before_edge_ids)
    return command, plan.mode, added_nodes, added_edges, tuple(node.id for node in plan.updated_nodes)


def merge_position_offset(graph: AppliedGraph) -> tuple[int, int]:
    if not graph.nodes:
        return (0, 0)
    return (max(node.position_x for node in graph.nodes) + 280, 0)


def _target_graph_state(
    session: Session,
    *,
    product_id: str,
    recipe_kind: WorkflowRecipeKind,
) -> tuple[Product, WorkflowGraph | None, AppliedGraph, RecipeApplyMode, tuple[int, int]]:
    product = session.get(Product, product_id)
    if product is None:
        raise ConflictError("商品不存在")
    live = get_active_workflow_graph(session, product_id=product_id)
    if live is None:
        if recipe_kind == WorkflowRecipeKind.RECIPE_FRAGMENT:
            raise ConflictError("片段配方需要已有 schema-v3 工作流")
        return product, None, EMPTY_GRAPH, "create", (0, 0)
    if recipe_kind != WorkflowRecipeKind.RECIPE_FRAGMENT:
        raise ConflictError("完整配方不能合并进已有工作流")
    existing = load_applied_graph(session, live)
    return product, live, existing, "merge", merge_position_offset(existing)


def _attach_existing_shared_inputs(
    existing: AppliedGraph,
    change_set: WorkflowChangeSet,
) -> tuple[WorkflowChangeSet, dict[str, list[str]]]:
    prompt_refs = [
        operation.client_ref
        for operation in change_set.operations
        if isinstance(operation, CreateNodeOp) and operation.node_type is GraphNodeType.PROMPT_GENERATION
    ]
    image_refs = [
        operation.client_ref
        for operation in change_set.operations
        if isinstance(operation, CreateNodeOp) and operation.node_type is GraphNodeType.IMAGE_GENERATION
    ]
    connected: dict[str, list[str]] = {
        "product_source": [],
        "creative_brief": [],
        "visual_system": [],
        "product_identity": [],
    }
    if not prompt_refs and not image_refs:
        return change_set, connected

    extra: list[GraphOperation] = []
    token = new_id().replace("-", "")[:10]
    index = 0

    def connect(source_id: str, target_ref: str, order: int) -> None:
        nonlocal index
        extra.append(
            ConnectNodesOp(
                client_ref=f"rs{token}{index:03d}",
                source_ref=source_id,
                target_ref=target_ref,
                order=order,
            )
        )
        index += 1

    product_source = _first_node(existing, GraphNodeType.PRODUCT_SOURCE)
    brief = _first_node(existing, GraphNodeType.CREATIVE_BRIEF)
    visual = _first_node(existing, GraphNodeType.VISUAL_SYSTEM)
    identities = tuple(
        sorted(
            _reference_asset_nodes(existing),
            key=lambda node: (node.position_y, node.position_x, node.id),
        )
    )
    for prompt_ref in prompt_refs:
        if product_source is not None:
            connect(product_source.id, prompt_ref, 0)
            connected["product_source"] = [product_source.id]
        if brief is not None:
            connect(brief.id, prompt_ref, 0)
            connected["creative_brief"] = [brief.id]
        if visual is not None:
            connect(visual.id, prompt_ref, 0)
            connected["visual_system"] = [visual.id]
        for order, identity in enumerate(identities):
            connect(identity.id, prompt_ref, order)
    for image_ref in image_refs:
        if visual is not None:
            connect(visual.id, image_ref, 0)
            connected["visual_system"] = [visual.id]
        for order, identity in enumerate(identities):
            connect(identity.id, image_ref, order)
    if identities:
        connected["product_identity"] = [
            node.id for node in identities if node.config.get("role") == "product_identity"
        ]
        if not connected["product_identity"]:
            connected["product_identity"] = [node.id for node in identities]
    if not extra:
        return change_set, connected
    return change_set.model_copy(update={"operations": [*change_set.operations, *extra]}), connected


def _assert_required_bindings(
    graph: AppliedGraph,
    *,
    required_bindings: tuple[str, ...],
    affected_node_ids: set[str],
) -> None:
    if not required_bindings:
        return
    image_nodes = [
        node
        for node in graph.nodes
        if node.id in affected_node_ids and node.node_type is GraphNodeType.IMAGE_GENERATION
    ]
    if not image_nodes:
        return
    identity_ids = {node.id for node in _product_identity_nodes(graph)}
    if "product_identity" in required_bindings and not identity_ids:
        raise ConflictError("配方需要商品身份参考图")
    for node in image_nodes:
        incoming = graph.incoming(node.id)
        for contract in run_required_inputs(node.node_type):
            if not any(edge.data_type is contract.data_type and edge.role is contract.role for edge in incoming):
                raise ConflictError("配方应用后镜头仍缺少运行所需的输入边")
        if "product_identity" in required_bindings and not any(
            edge.source_node_id in identity_ids for edge in incoming
        ):
            raise ConflictError("配方需要商品身份参考图")


def _first_node(graph: AppliedGraph, node_type: GraphNodeType) -> AppliedGraphNode | None:
    for node in graph.nodes:
        if node.node_type is node_type:
            return node
    return None


def _reference_asset_nodes(graph: AppliedGraph) -> tuple[AppliedGraphNode, ...]:
    return tuple(
        node
        for node in graph.nodes
        if node.node_type is GraphNodeType.IMAGE_ASSET
        and node.bound_asset_id
        and node.config.get("role") != "evidence"
    )


def _product_identity_nodes(graph: AppliedGraph) -> tuple[AppliedGraphNode, ...]:
    return tuple(node for node in _reference_asset_nodes(graph) if node.config.get("role") == "product_identity")


def _addition_semantic_payload(
    payload: RecipePayload,
    *,
    target_product_id: str,
    fact_set_version_id: str | None,
    position_offset: tuple[int, int],
) -> dict[str, Any]:
    offset_x, offset_y = position_offset
    nodes = []
    for node in payload.nodes:
        config = dict(node.config)
        if node.node_type == GraphNodeType.PRODUCT_SOURCE:
            config["source_product_id"] = target_product_id
            config["fact_set_version_id"] = fact_set_version_id
        config = normalize_node_config(node.node_type, config)
        nodes.append(
            {
                "key": node.key,
                "node_type": node.node_type.value,
                "title": node.title,
                "position_x": node.position_x + offset_x,
                "position_y": node.position_y + offset_y,
                "group_key": node.group_key,
                "config": config,
            }
        )
    return {
        "nodes": nodes,
        "edges": [
            {
                "key": edge.key,
                "source_node_key": edge.source_node_key,
                "target_node_key": edge.target_node_key,
                "data_type": edge.data_type.value,
                "role": edge.role.value,
                "order": edge.order,
            }
            for edge in payload.edges
        ],
        "groups": [
            {
                "key": group.key,
                "title": group.title,
                "member_keys": list(group.member_keys),
            }
            for group in payload.groups
        ],
    }


def _preview_additions(
    *,
    added_nodes: list[AppliedGraphNode],
    added_edges: list[AppliedGraphEdge],
    added_groups: list[AppliedGraphGroup],
) -> tuple[tuple[RecipePreviewNode, ...], tuple[RecipePreviewEdge, ...], tuple[RecipePreviewGroup, ...]]:
    node_key = {node.id: node.id for node in added_nodes}
    return (
        tuple(
            RecipePreviewNode(
                key=node.id,
                node_type=node.node_type,
                title=node.title,
                position_x=node.position_x,
                position_y=node.position_y,
            )
            for node in added_nodes
        ),
        tuple(
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
        tuple(
            RecipePreviewGroup(
                key=group.id,
                title=group.title,
                member_keys=tuple(node.id for node in added_nodes if node.group_id == group.id),
            )
            for group in added_groups
        ),
    )


def _recipe_preview_digest(
    *,
    product_id: str,
    recipe_id: str,
    recipe_version: int,
    payload_hash: str,
    graph_id: str | None,
    base_graph_revision: int,
    mode: RecipeApplyMode,
    required_bindings: tuple[str, ...],
    semantic: dict[str, Any],
) -> str:
    encoded = json.dumps(
        {
            "product_id": product_id,
            "recipe_id": recipe_id,
            "recipe_version": recipe_version,
            "payload_hash": payload_hash,
            "graph_id": graph_id,
            "base_graph_revision": base_graph_revision,
            "mode": mode,
            "required_bindings": list(required_bindings),
            "semantic": semantic,
        },
        ensure_ascii=False,
        sort_keys=True,
        separators=(",", ":"),
    ).encode("utf-8")
    return hashlib.sha256(encoded).hexdigest()


def recipe_application_summary(title: str) -> str:
    text = title.strip() or "工作流配方"
    return f"应用配方：{text}"[:500]


__all__ = [
    "RecipeApplyPlan",
    "RecipeApplyMode",
    "RecipeApplyPreview",
    "RecipePreviewEdge",
    "RecipePreviewGroup",
    "RecipePreviewNode",
    "RecipePreviewUpdatedNode",
    "apply_recipe_plan",
    "build_recipe_change_set",
    "plan_recipe_payload",
    "recipe_application_summary",
]
