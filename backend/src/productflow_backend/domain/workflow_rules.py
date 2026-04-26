from __future__ import annotations

from collections import defaultdict, deque
from collections.abc import Iterable, Mapping
from dataclasses import dataclass
from typing import Any

from productflow_backend.domain.enums import WorkflowNodeType
from productflow_backend.domain.errors import BusinessValidationError


@dataclass(frozen=True, slots=True)
class WorkflowRuleNode:
    """Small DB-free node shape used by workflow graph business rules."""

    id: str
    node_type: WorkflowNodeType
    position_x: int = 0
    config_json: Mapping[str, Any] | None = None


@dataclass(frozen=True, slots=True)
class WorkflowRuleEdge:
    """Small DB-free edge shape used by workflow graph business rules."""

    source_node_id: str
    target_node_id: str


def source_asset_ids_from_config(config: Mapping[str, Any] | None) -> tuple[str, ...]:
    """Extract source asset identifiers from workflow node config/output JSON."""

    if not config:
        return ()
    raw = config.get("source_asset_ids")
    if isinstance(raw, str):
        return (raw,)
    if isinstance(raw, list):
        return tuple(item for item in raw if isinstance(item, str))
    single = config.get("source_asset_id")
    if isinstance(single, str):
        return (single,)
    return ()


def should_execute_missing_upstream(source_node: WorkflowRuleNode, target_node: WorkflowRuleNode) -> bool:
    """Return whether a missing upstream artifact should be produced for this edge.

    This is the selected-node run rule: successful upstream artifacts are reusable context, but a missing
    upstream may be required when the target cannot be satisfied from existing first-class artifacts.
    """

    if source_node.node_type == WorkflowNodeType.PRODUCT_CONTEXT:
        return False
    if source_node.node_type == WorkflowNodeType.REFERENCE_IMAGE:
        return bool(source_asset_ids_from_config(source_node.config_json))
    if source_node.node_type == WorkflowNodeType.COPY_GENERATION:
        return target_node.node_type in {WorkflowNodeType.COPY_GENERATION, WorkflowNodeType.IMAGE_GENERATION}
    if source_node.node_type == WorkflowNodeType.IMAGE_GENERATION:
        return target_node.node_type in {WorkflowNodeType.REFERENCE_IMAGE, WorkflowNodeType.IMAGE_GENERATION}
    return False


def topological_node_ids(nodes: Iterable[WorkflowRuleNode], edges: Iterable[WorkflowRuleEdge]) -> list[str]:
    """Return graph node ids in executable order and reject broken/cyclic DAGs."""

    nodes_by_id = {node.id: node for node in nodes}
    incoming_count = {node_id: 0 for node_id in nodes_by_id}
    outgoing: dict[str, list[str]] = {node_id: [] for node_id in nodes_by_id}
    for edge in edges:
        if edge.source_node_id not in nodes_by_id or edge.target_node_id not in nodes_by_id:
            raise BusinessValidationError("工作流连线引用了不存在的节点")
        outgoing[edge.source_node_id].append(edge.target_node_id)
        incoming_count[edge.target_node_id] += 1

    queue = deque(
        sorted(
            [node_id for node_id, count in incoming_count.items() if count == 0],
            key=lambda item: nodes_by_id[item].position_x,
        )
    )
    ordered: list[str] = []
    while queue:
        node_id = queue.popleft()
        ordered.append(node_id)
        for target_id in outgoing[node_id]:
            incoming_count[target_id] -= 1
            if incoming_count[target_id] == 0:
                queue.append(target_id)
    if len(ordered) != len(nodes_by_id):
        raise ValueError("工作流不能包含循环依赖")
    return ordered


def selected_node_execution_plan(
    *,
    nodes: Iterable[WorkflowRuleNode],
    edges: Iterable[WorkflowRuleEdge],
    start_node_id: str,
    reusable_edges: Iterable[tuple[str, str]] = (),
) -> set[str]:
    """Plan a selected-node run from pure graph/rule inputs.

    `reusable_edges` contains `(source_node_id, target_node_id)` pairs whose source artifact has already been proven
    reusable by the application/query layer. The returned set contains the selected node plus missing
    required upstreams.
    """

    nodes_by_id = {node.id: node for node in nodes}
    if start_node_id not in nodes_by_id:
        raise BusinessValidationError("工作流节点不属于当前商品")
    edge_list = list(edges)
    topological_node_ids(nodes_by_id.values(), edge_list)

    reusable_edge_set = set(reusable_edges)
    incoming: dict[str, list[str]] = defaultdict(list)
    for edge in edge_list:
        incoming[edge.target_node_id].append(edge.source_node_id)

    selected: set[str] = set()

    def include_missing_required_upstream(node_id: str) -> None:
        target_node = nodes_by_id[node_id]
        for source_id in incoming[node_id]:
            source_node = nodes_by_id.get(source_id)
            if source_node is None:
                raise BusinessValidationError("工作流连线引用了不存在的节点")
            if (source_id, node_id) in reusable_edge_set:
                continue
            if not should_execute_missing_upstream(source_node, target_node):
                continue
            if source_id in selected:
                continue
            include_missing_required_upstream(source_id)
            selected.add(source_id)

    include_missing_required_upstream(start_node_id)
    selected.add(start_node_id)
    return selected
