from __future__ import annotations

from collections import defaultdict, deque
from collections.abc import Iterable
from dataclasses import dataclass

from productflow_backend.domain.enums import WorkflowNodeType
from productflow_backend.domain.errors import BusinessValidationError


@dataclass(frozen=True, slots=True)
class WorkflowRuleNode:
    """Small DB-free node shape used by workflow graph business rules."""

    id: str
    node_type: WorkflowNodeType
    position_x: int = 0
    config_json: dict[str, object] | None = None


@dataclass(frozen=True, slots=True)
class WorkflowRuleEdge:
    """Small DB-free edge shape used by workflow graph business rules."""

    source_node_id: str
    target_node_id: str


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
        raise BusinessValidationError("工作流不能包含循环依赖")
    return ordered


def ready_workflow_node_ids(
    *,
    nodes: Iterable[WorkflowRuleNode],
    edges: Iterable[WorkflowRuleEdge],
    run_node_ids: Iterable[str],
    queued_node_ids: Iterable[str],
    succeeded_node_ids: Iterable[str],
) -> list[str]:
    """Return queued run nodes whose in-run dependencies have succeeded."""

    nodes_by_id = {node.id: node for node in nodes}
    edge_list = list(edges)
    ordered_ids = topological_node_ids(nodes_by_id.values(), edge_list)
    run_node_id_set = set(run_node_ids)
    queued_node_id_set = set(queued_node_ids)
    succeeded_node_id_set = set(succeeded_node_ids)

    incoming: dict[str, list[str]] = defaultdict(list)
    for edge in edge_list:
        incoming[edge.target_node_id].append(edge.source_node_id)

    ready: list[str] = []
    for node_id in ordered_ids:
        if node_id not in run_node_id_set or node_id not in queued_node_id_set:
            continue
        if all(
            source_id not in run_node_id_set or source_id in succeeded_node_id_set
            for source_id in incoming[node_id]
        ):
            ready.append(node_id)
    return ready
