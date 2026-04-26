from __future__ import annotations

import pytest

from productflow_backend.domain.enums import WorkflowNodeType
from productflow_backend.domain.workflow_rules import (
    WorkflowRuleEdge,
    WorkflowRuleNode,
    selected_node_execution_plan,
    should_execute_missing_upstream,
    source_asset_ids_from_config,
    topological_node_ids,
)


def _node(
    node_id: str,
    node_type: WorkflowNodeType,
    *,
    x: int = 0,
    config_json: dict | None = None,
) -> WorkflowRuleNode:
    return WorkflowRuleNode(id=node_id, node_type=node_type, position_x=x, config_json=config_json)


def test_selected_node_plan_uses_reusable_edges_without_database_session() -> None:
    nodes = [
        _node("context", WorkflowNodeType.PRODUCT_CONTEXT, x=0),
        _node("copy", WorkflowNodeType.COPY_GENERATION, x=100),
        _node("image", WorkflowNodeType.IMAGE_GENERATION, x=200),
        _node("target", WorkflowNodeType.REFERENCE_IMAGE, x=300),
    ]
    edges = [
        WorkflowRuleEdge("context", "copy"),
        WorkflowRuleEdge("copy", "image"),
        WorkflowRuleEdge("image", "target"),
    ]

    plan = selected_node_execution_plan(
        nodes=nodes,
        edges=edges,
        start_node_id="target",
        reusable_edges={("copy", "image")},
    )

    assert plan == {"image", "target"}


def test_missing_upstream_business_rules_do_not_need_orm_models() -> None:
    copy_node = _node("copy", WorkflowNodeType.COPY_GENERATION)
    image_node = _node("image", WorkflowNodeType.IMAGE_GENERATION)
    product_context = _node("context", WorkflowNodeType.PRODUCT_CONTEXT)
    configured_reference = _node(
        "reference",
        WorkflowNodeType.REFERENCE_IMAGE,
        config_json={"source_asset_ids": ["asset-1"]},
    )
    empty_reference = _node("empty-reference", WorkflowNodeType.REFERENCE_IMAGE)

    assert should_execute_missing_upstream(copy_node, image_node) is True
    assert should_execute_missing_upstream(product_context, copy_node) is False
    assert should_execute_missing_upstream(configured_reference, copy_node) is True
    assert should_execute_missing_upstream(empty_reference, copy_node) is False


def test_source_asset_ids_from_config_preserves_legacy_string_shape_without_database_session() -> None:
    assert source_asset_ids_from_config({"source_asset_ids": "asset-1"}) == ("asset-1",)
    assert source_asset_ids_from_config({"source_asset_ids": ["asset-2", 3, "asset-3"]}) == ("asset-2", "asset-3")
    assert source_asset_ids_from_config({"source_asset_id": "asset-4"}) == ("asset-4",)


def test_selected_node_plan_rejects_broken_target_without_database_session() -> None:
    nodes = [_node("copy", WorkflowNodeType.COPY_GENERATION)]
    edges = [WorkflowRuleEdge("copy", "missing-target")]

    with pytest.raises(ValueError, match="工作流连线引用了不存在的节点"):
        selected_node_execution_plan(nodes=nodes, edges=edges, start_node_id="copy")


def test_selected_node_plan_rejects_cycle_without_database_session() -> None:
    nodes = [
        _node("copy", WorkflowNodeType.COPY_GENERATION, x=100),
        _node("image", WorkflowNodeType.IMAGE_GENERATION, x=200),
    ]
    edges = [
        WorkflowRuleEdge("copy", "image"),
        WorkflowRuleEdge("image", "copy"),
    ]

    with pytest.raises(ValueError, match="工作流不能包含循环依赖"):
        selected_node_execution_plan(nodes=nodes, edges=edges, start_node_id="image")


def test_topological_node_ids_rejects_cycle_without_database_session() -> None:
    nodes = [
        _node("copy", WorkflowNodeType.COPY_GENERATION, x=100),
        _node("image", WorkflowNodeType.IMAGE_GENERATION, x=200),
    ]
    edges = [
        WorkflowRuleEdge("copy", "image"),
        WorkflowRuleEdge("image", "copy"),
    ]

    with pytest.raises(ValueError, match="工作流不能包含循环依赖"):
        topological_node_ids(nodes, edges)
