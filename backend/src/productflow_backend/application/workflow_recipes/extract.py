"""从 live schema-v3 图抽出可复用配方：去掉商品身份、绑定资产和生成结果。"""

from __future__ import annotations

from typing import Any, Literal

from productflow_backend.application.product_workflow.graph_apply import AppliedGraph, AppliedGraphNode
from productflow_backend.application.workflow_recipes.contracts import (
    RECIPE_IDENTITY_CONFIG_KEYS,
    RECIPE_STRIPPED_PROMPT_KEYS,
    RecipeGraphEdge,
    RecipeGraphGroup,
    RecipeGraphNode,
    RecipePayload,
)
from productflow_backend.domain.errors import BusinessValidationError
from productflow_backend.domain.graph_catalog import FORBIDDEN_GRAPH_CONFIG_KEYS

RecipeSourceType = Literal["workflow", "group", "selection"]


def extract_recipe_payload(
    graph: AppliedGraph,
    *,
    source_type: RecipeSourceType,
    group_id: str | None = None,
    node_ids: list[str] | None = None,
) -> RecipePayload:
    """只保留选区内的结构与可复用 config；不写入目标商品 live 图。"""

    selected_ids = _selected_node_ids(graph, source_type=source_type, group_id=group_id, node_ids=node_ids or [])
    nodes = [node for node in graph.nodes if node.id in selected_ids]
    if not nodes:
        raise BusinessValidationError("配方至少需要一个节点")
    selected = set(selected_ids)
    edges = [
        edge
        for edge in graph.edges
        if edge.source_node_id in selected and edge.target_node_id in selected
    ]
    groups = []
    for group in graph.groups:
        members = [node.id for node in nodes if node.group_id == group.id]
        if not members:
            continue
        if source_type == "selection" and any(
            node.id not in selected for node in graph.nodes if node.group_id == group.id
        ):
            continue
        groups.append(
            RecipeGraphGroup(
                key=_recipe_key(group.id),
                title=group.title,
                member_keys=tuple(_recipe_key(node_id) for node_id in members),
            )
        )
    included_group_keys = {group.key for group in groups}
    return RecipePayload(
        nodes=[
            RecipeGraphNode(
                key=_recipe_key(node.id),
                node_type=node.node_type,
                title=node.title,
                position_x=node.position_x,
                position_y=node.position_y,
                group_key=(
                    _recipe_key(node.group_id)
                    if node.group_id and _recipe_key(node.group_id) in included_group_keys
                    else None
                ),
                config=_reusable_node_config(node),
            )
            for node in nodes
        ],
        edges=[
            RecipeGraphEdge(
                key=_recipe_key(edge.id),
                source_node_key=_recipe_key(edge.source_node_id),
                target_node_key=_recipe_key(edge.target_node_id),
                data_type=edge.data_type,
                role=edge.role,
                order=edge.order,
            )
            for edge in edges
        ],
        groups=groups,
    )


def _selected_node_ids(
    graph: AppliedGraph,
    *,
    source_type: RecipeSourceType,
    group_id: str | None,
    node_ids: list[str],
) -> list[str]:
    known = {node.id for node in graph.nodes}
    if source_type == "workflow":
        if group_id is not None or node_ids:
            raise BusinessValidationError("完整工作流来源不能指定 group_id 或 node_ids")
        return [node.id for node in graph.nodes]
    if source_type == "group":
        if group_id is None or node_ids:
            raise BusinessValidationError("分组来源必须且只能指定 group_id")
        graph.group(group_id)
        members = [node.id for node in graph.nodes if node.group_id == group_id]
        if not members:
            raise BusinessValidationError("分组里没有可保存的节点")
        return members
    if group_id is not None or not node_ids:
        raise BusinessValidationError("多选来源必须且只能指定非空 node_ids")
    if len(node_ids) != len(set(node_ids)):
        raise BusinessValidationError("node_ids 不能重复")
    missing = [node_id for node_id in node_ids if node_id not in known]
    if missing:
        raise BusinessValidationError("选区包含图上不存在的节点")
    return list(dict.fromkeys(node_ids))


def _reusable_node_config(node: AppliedGraphNode) -> dict[str, Any]:
    return _sanitize_config_value(dict(node.config))


def _sanitize_config_value(value: Any) -> Any:
    # 配方不得保存商品身份、绑定资产或退休 plan key。
    if isinstance(value, dict):
        cleaned: dict[str, Any] = {}
        for key, item in value.items():
            if key in RECIPE_STRIPPED_PROMPT_KEYS or key in FORBIDDEN_GRAPH_CONFIG_KEYS:
                continue
            if key in RECIPE_IDENTITY_CONFIG_KEYS:
                cleaned[key] = None
                continue
            cleaned[key] = _sanitize_config_value(item)
        return cleaned
    if isinstance(value, list):
        return [_sanitize_config_value(item) for item in value]
    return value


def _recipe_key(value: str) -> str:
    return value.strip().lower()


__all__ = ["RecipeSourceType", "extract_recipe_payload"]
