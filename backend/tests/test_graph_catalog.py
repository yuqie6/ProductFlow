from __future__ import annotations

import pytest
from fastapi.testclient import TestClient
from helpers import _login

from productflow_backend.application.product_workflow.graph_apply import EMPTY_GRAPH, apply_workflow_change_set
from productflow_backend.application.product_workflow.graph_contracts import (
    ConnectNodesOp,
    CreateNodeOp,
    WorkflowChangeSet,
)
from productflow_backend.domain.enums import GraphConfigStatus, GraphEdgeRole, GraphNodeType
from productflow_backend.domain.errors import BusinessValidationError
from productflow_backend.domain.graph_catalog import (
    GRAPH_CATALOG_VERSION,
    allowed_config_keys,
    graph_catalog_document,
    graph_input_contract,
    graph_node_output_type,
    validate_node_config,
)
from productflow_backend.domain.graph_rules import (
    GraphRuleEdge,
    GraphRuleNode,
    node_config_status,
    topological_graph_node_ids,
)
from productflow_backend.presentation.api import create_app


def test_catalog_rejects_facts_into_image_generation() -> None:
    assert graph_input_contract(GraphNodeType.PRODUCT_SOURCE, GraphNodeType.IMAGE_GENERATION) is None
    assert graph_node_output_type(GraphNodeType.IMAGE_GENERATION).value == "image_asset"


def test_catalog_document_covers_every_node_type_and_acceptance() -> None:
    document = graph_catalog_document()
    assert document.version == GRAPH_CATALOG_VERSION
    assert [node.node_type for node in document.nodes] == list(GraphNodeType)
    prompt = next(node for node in document.nodes if node.node_type == GraphNodeType.PROMPT_GENERATION)
    image = next(node for node in document.nodes if node.node_type == GraphNodeType.IMAGE_GENERATION)
    source = next(node for node in document.nodes if node.node_type == GraphNodeType.PRODUCT_SOURCE)
    assert prompt.kind == "processing"
    assert source.kind == "source"
    assert source.accepts == ()
    assert {item.role for item in prompt.accepts} == {
        GraphEdgeRole.FACTS,
        GraphEdgeRole.REFERENCE,
        GraphEdgeRole.BRIEF,
        GraphEdgeRole.VISUAL_GUIDANCE,
    }
    visual = next(item for item in prompt.accepts if item.role == GraphEdgeRole.VISUAL_GUIDANCE)
    assert visual.max_count == 1
    required = next(item for item in image.accepts if item.role == GraphEdgeRole.PROMPT)
    assert required.max_count == 1
    assert required.required_to_run is True
    assert all(item.data_type != graph_node_output_type(GraphNodeType.PRODUCT_SOURCE) for item in image.accepts)
    assert {field.key for field in image.config_fields} == {
        "image_type_key",
        "generation_spec",
        "delivery_spec",
        "variation_instruction",
        "visual_overlay",
        "visual_overrides",
    }
    assert {field.key for field in prompt.config_fields} == {"image_type_key", "prompt"}
    assert allowed_config_keys(GraphNodeType.PRODUCT_SOURCE) == {"source_product_id", "fact_set_version_id"}


def test_node_catalog_http_projects_domain_document(configured_env) -> None:
    client = TestClient(create_app())
    _login(client)
    response = client.get("/api/v3/node-catalog")
    assert response.status_code == 200, response.text
    payload = response.json()
    document = graph_catalog_document()
    assert payload["version"] == document.version
    assert [node["node_type"] for node in payload["nodes"]] == [node.node_type.value for node in document.nodes]
    image = next(node for node in payload["nodes"] if node["node_type"] == "image_generation")
    prompt_input = next(item for item in image["accepts"] if item["role"] == "prompt")
    assert prompt_input["max_count"] == 1
    assert prompt_input["required_to_run"] is True
    assert prompt_input["data_type"] == "prompt"
    assert not any(item["data_type"] == "product_facts" for item in image["accepts"])
    assert {field["key"] for field in image["config_fields"]} == {
        "image_type_key",
        "generation_spec",
        "delivery_spec",
        "variation_instruction",
        "visual_overlay",
        "visual_overrides",
    }


def test_catalog_rejects_unregistered_and_topology_config_keys() -> None:
    validate_node_config(GraphNodeType.IMAGE_GENERATION, {"generation_spec": {"aspect_ratio": "1:1"}})
    with pytest.raises(BusinessValidationError, match="拓扑字段"):
        validate_node_config(GraphNodeType.IMAGE_GENERATION, {"prompt_plan_key": "hero-1"})
    with pytest.raises(BusinessValidationError, match="未登记字段"):
        apply_workflow_change_set(
            EMPTY_GRAPH,
            WorkflowChangeSet(
                base_graph_revision=0,
                summary="未知配置字段",
                operations=[
                    CreateNodeOp(
                        client_ref="image",
                        node_type=GraphNodeType.IMAGE_GENERATION,
                        title="图",
                        config={"unknown_field": True},
                    )
                ],
            ),
        )


def test_incomplete_image_generation_without_prompt_is_legal_graph() -> None:
    nodes = [
        GraphRuleNode("product", GraphNodeType.PRODUCT_SOURCE),
        GraphRuleNode("image", GraphNodeType.IMAGE_GENERATION),
    ]
    ordered = topological_graph_node_ids(nodes, [])
    assert set(ordered) == {"product", "image"}
    assert node_config_status(nodes[1], []) == GraphConfigStatus.INCOMPLETE


def test_visual_system_is_ready_with_inline_overlay() -> None:
    empty = GraphRuleNode("visual", GraphNodeType.VISUAL_SYSTEM, config={})
    assert node_config_status(empty, []) == GraphConfigStatus.INCOMPLETE
    overlay = GraphRuleNode(
        "visual",
        GraphNodeType.VISUAL_SYSTEM,
        config={"visual_overlay": {"style": ["干净白底"]}},
    )
    assert node_config_status(overlay, []) == GraphConfigStatus.READY


def test_product_source_distinguishes_legacy_fallback_from_explicit_unbound_config() -> None:
    legacy = GraphRuleNode("legacy-product", GraphNodeType.PRODUCT_SOURCE, config={})
    unbound = GraphRuleNode(
        "new-product",
        GraphNodeType.PRODUCT_SOURCE,
        config={"source_product_id": None, "fact_set_version_id": None},
    )
    bound = GraphRuleNode(
        "bound-product",
        GraphNodeType.PRODUCT_SOURCE,
        config={"source_product_id": "product-1", "fact_set_version_id": None},
    )
    assert node_config_status(legacy, []) == GraphConfigStatus.READY
    assert node_config_status(unbound, []) == GraphConfigStatus.INCOMPLETE
    assert node_config_status(bound, []) == GraphConfigStatus.READY


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
