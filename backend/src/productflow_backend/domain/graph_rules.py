"""schema-v3 图规则：允许不完整 DAG，只拒绝环、悬空引用和基数冲突。"""

from __future__ import annotations

from collections import deque
from collections.abc import Iterable
from dataclasses import dataclass

from productflow_backend.domain.enums import (
    GraphConfigStatus,
    GraphEdgeDataType,
    GraphEdgeRole,
    GraphNodeType,
)
from productflow_backend.domain.errors import BusinessValidationError
from productflow_backend.domain.graph_catalog import (
    GraphInputContract,
    graph_input_contract,
    graph_node_output_type,
    normalize_node_config,
    require_graph_connection,
    run_required_inputs,
)


@dataclass(frozen=True, slots=True)
class GraphRuleNode:
    id: str
    node_type: GraphNodeType
    config: dict[str, object] | None = None
    bound_asset_id: str | None = None


@dataclass(frozen=True, slots=True)
class GraphRuleEdge:
    id: str
    source_node_id: str
    target_node_id: str
    data_type: GraphEdgeDataType
    role: GraphEdgeRole
    order: int = 0


def topological_graph_node_ids(nodes: Iterable[GraphRuleNode], edges: Iterable[GraphRuleEdge]) -> list[str]:
    """允许不完整 DAG；缺边节点仍可排序，只拒绝环和悬空引用。"""

    nodes_by_id = {node.id: node for node in nodes}
    incoming_count = {node_id: 0 for node_id in nodes_by_id}
    outgoing: dict[str, list[str]] = {node_id: [] for node_id in nodes_by_id}
    for edge in edges:
        if edge.source_node_id not in nodes_by_id or edge.target_node_id not in nodes_by_id:
            raise BusinessValidationError("工作流连线引用了不存在的节点")
        outgoing[edge.source_node_id].append(edge.target_node_id)
        incoming_count[edge.target_node_id] += 1

    queue = deque(sorted(node_id for node_id, count in incoming_count.items() if count == 0))
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


def validate_graph_edge(
    *,
    source: GraphRuleNode,
    target: GraphRuleNode,
    existing_target_edges: Iterable[GraphRuleEdge],
) -> GraphInputContract:
    """连线类型与基数上限归 Catalog；此处只应用合同。"""

    if source.id == target.id:
        raise BusinessValidationError("节点不能连接自身")
    contract = require_graph_connection(source.node_type, target.node_type)
    same_role_count = sum(
        1 for edge in existing_target_edges if edge.data_type == contract.data_type and edge.role == contract.role
    )
    if contract.max_count is not None and same_role_count >= contract.max_count:
        raise BusinessValidationError("目标节点该类输入已达到上限")
    return contract


def typed_edge_from_nodes(
    source: GraphRuleNode,
    target: GraphRuleNode,
) -> tuple[GraphEdgeDataType, GraphEdgeRole]:
    contract = graph_input_contract(source.node_type, target.node_type)
    if contract is None:
        raise BusinessValidationError("节点类型不兼容，不能创建连线")
    return graph_node_output_type(source.node_type), contract.role


def node_config_status(node: GraphRuleNode, incoming: Iterable[GraphRuleEdge]) -> GraphConfigStatus:
    """就绪性只看自身配置和传入的 incoming 边，不扫描图上其他节点。"""

    incoming_list = list(incoming)
    if node_config_error(node) is not None:
        return GraphConfigStatus.INCOMPLETE
    if node.node_type == GraphNodeType.PRODUCT_SOURCE:
        # 新节点必须显式做出绑定决策。缺失的键留给遗留读取，由图运行时所有者解析。
        config = node.config or {}
        if "source_product_id" in config:
            source_id = config.get("source_product_id")
            if not isinstance(source_id, str) or not source_id.strip():
                return GraphConfigStatus.INCOMPLETE
            fact_set_id = config.get("fact_set_version_id")
            if fact_set_id is not None and (not isinstance(fact_set_id, str) or not fact_set_id.strip()):
                return GraphConfigStatus.INCOMPLETE
    # 绑定是 image_asset 的资产身份，不等于下游 reference 边。
    if node.node_type == GraphNodeType.IMAGE_ASSET and not node.bound_asset_id:
        return GraphConfigStatus.INCOMPLETE
    if node.node_type == GraphNodeType.VISUAL_SYSTEM and not _has_visual_system_config(node.config):
        return GraphConfigStatus.INCOMPLETE
    if missing_required_inputs(node, incoming_list):
        return GraphConfigStatus.INCOMPLETE
    return GraphConfigStatus.READY


def node_config_error(node: GraphRuleNode) -> str | None:
    try:
        normalized = normalize_node_config(node.node_type, node.config)
    except BusinessValidationError as exc:
        return str(exc)
    if node.node_type == GraphNodeType.IMAGE_GENERATION and "generation_spec" not in normalized:
        return "图片生成节点缺少有效 GenerationSpec"
    return None


def missing_required_inputs(
    node: GraphRuleNode,
    incoming: Iterable[GraphRuleEdge],
) -> tuple[GraphInputContract, ...]:
    """运行必需边只对照传入的 incoming；断开边即失去该输入。"""

    incoming_list = list(incoming)
    return tuple(
        contract
        for contract in run_required_inputs(node.node_type)
        if not any(edge.data_type == contract.data_type and edge.role == contract.role for edge in incoming_list)
    )


def _has_visual_system_config(config: dict[str, object] | None) -> bool:
    payload = config or {}
    version_id = payload.get("visual_system_version_id")
    if isinstance(version_id, str) and version_id.strip():
        return True
    overlay = payload.get("visual_overlay")
    if isinstance(overlay, dict) and overlay:
        return True
    overrides = payload.get("visual_overrides")
    return isinstance(overrides, list) and bool(overrides)
