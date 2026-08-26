"""纯内存应用 ChangeSet：Catalog 校验 config，graph_rules 拒绝环/悬空/基数冲突。"""

from __future__ import annotations

from collections import defaultdict
from dataclasses import dataclass, replace

from productflow_backend.application.product_workflow.graph_contracts import (
    ConnectNodesOp,
    CreateGroupOp,
    CreateNodeOp,
    DeleteNodeOp,
    DisconnectEdgeOp,
    DissolveGroupOp,
    GraphOperation,
    MoveNodesOp,
    MoveNodesToGroupOp,
    RenameGroupOp,
    RenameNodeOp,
    UpdateNodeConfigOp,
    WorkflowChangeSet,
)
from productflow_backend.domain.artifact_contracts import PRODUCT_INTAKE_MAX_TOTAL_IMAGES
from productflow_backend.domain.enums import GraphConfigStatus, GraphEdgeDataType, GraphEdgeRole, GraphNodeType
from productflow_backend.domain.errors import BusinessValidationError, ConflictError
from productflow_backend.domain.graph_catalog import graph_node_output_type, normalize_node_config
from productflow_backend.domain.graph_rules import (
    GraphRuleEdge,
    GraphRuleNode,
    node_config_status,
    topological_graph_node_ids,
    validate_graph_edge,
)


@dataclass(frozen=True, slots=True)
class AppliedGraphNode:
    id: str
    node_type: GraphNodeType
    title: str
    position_x: int
    position_y: int
    config: dict[str, object]
    bound_asset_id: str | None = None
    group_id: str | None = None


@dataclass(frozen=True, slots=True)
class AppliedGraphEdge:
    id: str
    source_node_id: str
    target_node_id: str
    data_type: GraphEdgeDataType
    role: GraphEdgeRole
    order: int = 0


@dataclass(frozen=True, slots=True)
class AppliedGraphGroup:
    """画布一层视觉分组，无执行状态或端口。"""

    id: str
    title: str


@dataclass(frozen=True, slots=True)
class AppliedGraph:
    revision: int
    nodes: tuple[AppliedGraphNode, ...]
    edges: tuple[AppliedGraphEdge, ...]
    groups: tuple[AppliedGraphGroup, ...]

    def node(self, node_id: str) -> AppliedGraphNode:
        for node in self.nodes:
            if node.id == node_id:
                return node
        raise BusinessValidationError("图节点不存在")

    def group(self, group_id: str) -> AppliedGraphGroup:
        for group in self.groups:
            if group.id == group_id:
                return group
        raise BusinessValidationError("图分组不存在")

    def incoming(self, node_id: str) -> tuple[AppliedGraphEdge, ...]:
        return tuple(edge for edge in self.edges if edge.target_node_id == node_id)

    def config_status(self, node_id: str) -> GraphConfigStatus:
        node = self.node(node_id)
        return node_config_status(
            GraphRuleNode(node.id, node.node_type, node.config, node.bound_asset_id),
            (
                GraphRuleEdge(edge.id, edge.source_node_id, edge.target_node_id, edge.data_type, edge.role, edge.order)
                for edge in self.incoming(node_id)
            ),
        )


EMPTY_GRAPH = AppliedGraph(revision=0, nodes=(), edges=(), groups=())


def apply_workflow_change_set(graph: AppliedGraph, change_set: WorkflowChangeSet) -> AppliedGraph:
    """顺序应用操作并校验完整结果。不写库；持久化由 Graph Command 负责。"""

    if change_set.base_graph_revision != graph.revision:
        raise ConflictError("图 revision 已变化，请刷新后重试")

    nodes = {node.id: node for node in graph.nodes}
    edges = {edge.id: edge for edge in graph.edges}
    groups = {group.id: group for group in graph.groups}
    aliases = {node.id: node.id for node in graph.nodes}
    aliases.update({group.id: group.id for group in graph.groups})
    aliases.update({edge.id: edge.id for edge in graph.edges})

    def resolve(ref: str) -> str:
        if ref in aliases:
            return aliases[ref]
        raise BusinessValidationError("ChangeSet 引用了不存在的图对象")

    for operation in change_set.operations:
        if isinstance(operation, CreateGroupOp):
            if operation.client_ref in aliases:
                raise BusinessValidationError("ChangeSet client_ref 与已有对象冲突")
            groups[operation.client_ref] = AppliedGraphGroup(operation.client_ref, operation.title)
            aliases[operation.client_ref] = operation.client_ref
            for member_ref in operation.member_refs:
                member_id = resolve(member_ref)
                nodes[member_id] = replace(nodes[member_id], group_id=operation.client_ref)
            continue
        if isinstance(operation, CreateNodeOp):
            if operation.client_ref in aliases:
                raise BusinessValidationError("ChangeSet client_ref 与已有对象冲突")
            config = normalize_node_config(operation.node_type, operation.config)
            group_id = resolve(operation.group_ref) if operation.group_ref else None
            nodes[operation.client_ref] = AppliedGraphNode(
                id=operation.client_ref,
                node_type=operation.node_type,
                title=operation.title,
                position_x=operation.position_x,
                position_y=operation.position_y,
                config=config,
                bound_asset_id=operation.bound_asset_id,
                group_id=group_id,
            )
            aliases[operation.client_ref] = operation.client_ref
            continue
        if isinstance(operation, ConnectNodesOp):
            source = nodes[resolve(operation.source_ref)]
            target = nodes[resolve(operation.target_ref)]
            existing = [edge for edge in edges.values() if edge.target_node_id == target.id]
            contract = validate_graph_edge(
                source=GraphRuleNode(source.id, source.node_type, source.config, source.bound_asset_id),
                target=GraphRuleNode(target.id, target.node_type, target.config, target.bound_asset_id),
                existing_target_edges=(
                    GraphRuleEdge(
                        edge.id,
                        edge.source_node_id,
                        edge.target_node_id,
                        edge.data_type,
                        edge.role,
                        edge.order,
                    )
                    for edge in existing
                ),
            )
            if any(
                edge.source_node_id == source.id and edge.target_node_id == target.id and edge.role == contract.role
                for edge in existing
            ):
                raise BusinessValidationError("相同输入连线已存在")
            if operation.client_ref in aliases:
                raise BusinessValidationError("ChangeSet client_ref 与已有对象冲突")
            edges[operation.client_ref] = AppliedGraphEdge(
                id=operation.client_ref,
                source_node_id=source.id,
                target_node_id=target.id,
                data_type=graph_node_output_type(source.node_type),
                role=contract.role,
                order=operation.order,
            )
            aliases[operation.client_ref] = operation.client_ref
            continue
        if isinstance(operation, DisconnectEdgeOp):
            edge_id = resolve(operation.edge_ref)
            edges.pop(edge_id)
            aliases.pop(edge_id, None)
            continue
        if isinstance(operation, DeleteNodeOp):
            node_id = resolve(operation.node_ref)
            incident = [
                edge_id
                for edge_id, edge in edges.items()
                if node_id in {edge.source_node_id, edge.target_node_id}
            ]
            for edge_id in incident:
                edges.pop(edge_id)
                aliases.pop(edge_id, None)
            nodes.pop(node_id)
            aliases.pop(node_id, None)
            continue
        if isinstance(operation, RenameNodeOp):
            node_id = resolve(operation.node_ref)
            nodes[node_id] = replace(nodes[node_id], title=operation.title)
            continue
        if isinstance(operation, UpdateNodeConfigOp):
            node_id = resolve(operation.node_ref)
            node = nodes[node_id]
            # Catalog 规范化 config；未登记键和退休 plan key 在这里被拒绝。
            config = normalize_node_config(node.node_type, operation.config)
            bound = (
                operation.bound_asset_id
                if "bound_asset_id" in operation.model_fields_set
                else node.bound_asset_id
            )
            nodes[node_id] = replace(node, config=config, bound_asset_id=bound)
            continue
        if isinstance(operation, MoveNodesOp):
            for node_ref, position_x, position_y in operation.nodes:
                node_id = resolve(node_ref)
                nodes[node_id] = replace(nodes[node_id], position_x=position_x, position_y=position_y)
            continue
        if isinstance(operation, MoveNodesToGroupOp):
            group_id = resolve(operation.group_ref) if operation.group_ref is not None else None
            for node_ref in operation.node_refs:
                node_id = resolve(node_ref)
                nodes[node_id] = replace(nodes[node_id], group_id=group_id)
            continue
        if isinstance(operation, RenameGroupOp):
            group_id = resolve(operation.group_ref)
            groups[group_id] = AppliedGraphGroup(group_id, operation.title)
            continue
        if isinstance(operation, DissolveGroupOp):
            group_id = resolve(operation.group_ref)
            groups.pop(group_id)
            aliases.pop(group_id, None)
            for node_id, node in list(nodes.items()):
                if node.group_id == group_id:
                    nodes[node_id] = replace(node, group_id=None)
            continue
        raise BusinessValidationError("不支持的 Graph 操作")

    image_generation_count = sum(node.node_type == GraphNodeType.IMAGE_GENERATION for node in nodes.values())
    if image_generation_count > PRODUCT_INTAKE_MAX_TOTAL_IMAGES:
        raise BusinessValidationError(f"图片生成总数不能超过 {PRODUCT_INTAKE_MAX_TOTAL_IMAGES}")
    # 绑定只属于 image_asset；下游 reference 边是另一条关系。
    for node in nodes.values():
        if node.bound_asset_id and node.node_type != GraphNodeType.IMAGE_ASSET:
            raise BusinessValidationError("只有图片素材节点可以绑定商品图片")

    rule_nodes = [GraphRuleNode(node.id, node.node_type, node.config, node.bound_asset_id) for node in nodes.values()]
    rule_edges = [
        GraphRuleEdge(edge.id, edge.source_node_id, edge.target_node_id, edge.data_type, edge.role, edge.order)
        for edge in edges.values()
    ]
    topological_graph_node_ids(rule_nodes, rule_edges)
    return AppliedGraph(
        revision=graph.revision + 1,
        nodes=tuple(nodes[node_id] for node_id in sorted(nodes)),
        edges=tuple(edges[edge_id] for edge_id in sorted(edges)),
        groups=tuple(groups[group_id] for group_id in sorted(groups)),
    )


def invert_applied_graph(before: AppliedGraph, after: AppliedGraph) -> list[GraphOperation]:
    """Return operations that take `after` back to `before` topology (revision is not restored)."""

    operations: list[GraphOperation] = []
    before_node_ids = {node.id for node in before.nodes}
    after_node_ids = {node.id for node in after.nodes}
    before_edge_ids = {edge.id for edge in before.edges}
    after_edge_ids = {edge.id for edge in after.edges}
    before_group_ids = {group.id for group in before.groups}
    after_group_ids = {group.id for group in after.groups}

    for edge in after.edges:
        if edge.id not in before_edge_ids:
            operations.append(DisconnectEdgeOp(edge_ref=edge.id))
    for node in after.nodes:
        if node.id not in before_node_ids:
            operations.append(DeleteNodeOp(node_ref=node.id))
    for group in after.groups:
        if group.id not in before_group_ids:
            operations.append(DissolveGroupOp(group_ref=group.id))

    for group in before.groups:
        if group.id not in after_group_ids:
            operations.append(CreateGroupOp(client_ref=group.id, title=group.title, member_refs=()))
        elif after.group(group.id).title != group.title:
            operations.append(RenameGroupOp(group_ref=group.id, title=group.title))

    membership_moves: dict[str | None, list[str]] = defaultdict(list)
    position_moves: list[tuple[str, int, int]] = []
    for node in after.nodes:
        if node.id not in before_node_ids:
            continue
        previous = before.node(node.id)
        if node.group_id != previous.group_id:
            membership_moves[previous.group_id].append(node.id)
        if (node.position_x, node.position_y) != (previous.position_x, previous.position_y):
            position_moves.append((node.id, previous.position_x, previous.position_y))
        if node.title != previous.title:
            operations.append(RenameNodeOp(node_ref=node.id, title=previous.title))
        if node.config != previous.config or node.bound_asset_id != previous.bound_asset_id:
            operations.append(
                UpdateNodeConfigOp(
                    node_ref=node.id,
                    config=dict(previous.config),
                    bound_asset_id=previous.bound_asset_id,
                )
            )
    for group_id, node_ids in membership_moves.items():
        operations.append(MoveNodesToGroupOp(group_ref=group_id, node_refs=tuple(node_ids)))
    if position_moves:
        operations.append(MoveNodesOp(nodes=position_moves))

    for node in before.nodes:
        if node.id in after_node_ids:
            continue
        operations.append(
            CreateNodeOp(
                client_ref=node.id,
                node_type=node.node_type,
                title=node.title,
                position_x=node.position_x,
                position_y=node.position_y,
                config=dict(node.config),
                bound_asset_id=node.bound_asset_id,
                group_ref=node.group_id,
            )
        )
    for edge in before.edges:
        if edge.id in after_edge_ids:
            continue
        operations.append(
            ConnectNodesOp(
                client_ref=edge.id,
                source_ref=edge.source_node_id,
                target_ref=edge.target_node_id,
                order=edge.order,
            )
        )
    return operations
