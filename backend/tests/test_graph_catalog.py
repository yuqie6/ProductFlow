from __future__ import annotations

import pytest

from productflow_backend.application.product_workflow.graph_apply import EMPTY_GRAPH, apply_workflow_change_set
from productflow_backend.application.product_workflow.graph_contracts import (
    ConnectNodesOp,
    CreateNodeOp,
    WorkflowChangeSet,
)
from productflow_backend.domain.enums import GraphConfigStatus, GraphEdgeRole, GraphNodeType
from productflow_backend.domain.errors import BusinessValidationError
from productflow_backend.domain.graph_catalog import graph_input_contract, graph_node_output_type
from productflow_backend.domain.graph_rules import (
    GraphRuleEdge,
    GraphRuleNode,
    node_config_status,
    topological_graph_node_ids,
)


def test_catalog_rejects_facts_into_image_generation() -> None:
    assert graph_input_contract(GraphNodeType.PRODUCT_SOURCE, GraphNodeType.IMAGE_GENERATION) is None
    assert graph_node_output_type(GraphNodeType.IMAGE_GENERATION).value == "image_asset"


def test_incomplete_image_generation_without_prompt_is_legal_graph() -> None:
    nodes = [
        GraphRuleNode("product", GraphNodeType.PRODUCT_SOURCE),
        GraphRuleNode("image", GraphNodeType.IMAGE_GENERATION),
    ]
    ordered = topological_graph_node_ids(nodes, [])
    assert set(ordered) == {"product", "image"}
    assert node_config_status(nodes[1], []) == GraphConfigStatus.INCOMPLETE


def test_unbound_image_asset_is_incomplete_and_unused_without_edges() -> None:
    node = GraphRuleNode("asset", GraphNodeType.IMAGE_ASSET, bound_asset_id=None)
    assert node_config_status(node, []) == GraphConfigStatus.INCOMPLETE
    bound = GraphRuleNode("asset", GraphNodeType.IMAGE_ASSET, bound_asset_id="asset-1")
    assert node_config_status(bound, []) == GraphConfigStatus.READY


def test_image_generation_ready_only_with_prompt_edge() -> None:
    image = GraphRuleNode("image", GraphNodeType.IMAGE_GENERATION)
    prompt_edge = GraphRuleEdge(
        "e1",
        "prompt",
        "image",
        graph_node_output_type(GraphNodeType.PROMPT_GENERATION),
        GraphEdgeRole.PROMPT,
    )
    assert node_config_status(image, [prompt_edge]) == GraphConfigStatus.READY


def test_visual_system_cardinality_is_one() -> None:
    change_set = WorkflowChangeSet(
        base_graph_revision=0,
        summary="too many visual edges",
        operations=[
            CreateNodeOp(client_ref="visual-a", node_type=GraphNodeType.VISUAL_SYSTEM, title="A"),
            CreateNodeOp(client_ref="visual-b", node_type=GraphNodeType.VISUAL_SYSTEM, title="B"),
            CreateNodeOp(client_ref="prompt", node_type=GraphNodeType.PROMPT_GENERATION, title="提示词"),
            ConnectNodesOp(client_ref="e1", source_ref="visual-a", target_ref="prompt"),
            ConnectNodesOp(client_ref="e2", source_ref="visual-b", target_ref="prompt"),
        ],
    )
    with pytest.raises(BusinessValidationError, match="上限"):
        apply_workflow_change_set(EMPTY_GRAPH, change_set)


def test_cycle_is_rejected() -> None:
    change_set = WorkflowChangeSet(
        base_graph_revision=0,
        summary="cycle",
        operations=[
            CreateNodeOp(client_ref="a", node_type=GraphNodeType.IMAGE_GENERATION, title="A"),
            CreateNodeOp(client_ref="b", node_type=GraphNodeType.IMAGE_GENERATION, title="B"),
            ConnectNodesOp(client_ref="e1", source_ref="a", target_ref="b"),
            ConnectNodesOp(client_ref="e2", source_ref="b", target_ref="a"),
        ],
    )
    with pytest.raises(BusinessValidationError, match="循环"):
        apply_workflow_change_set(EMPTY_GRAPH, change_set)
