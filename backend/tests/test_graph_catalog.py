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
    catalog_visual_overlay,
    graph_catalog_document,
    graph_catalog_json,
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
from productflow_backend.presentation.schemas.graphs import serialize_graph_catalog


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
    visual_node = next(node for node in document.nodes if node.node_type == GraphNodeType.VISUAL_SYSTEM)
    brief_node = next(node for node in document.nodes if node.node_type == GraphNodeType.CREATIVE_BRIEF)
    assert visual_node.kind == "processing"
    assert brief_node.kind == "processing"
    assert {item.role for item in visual_node.accepts} == {GraphEdgeRole.FACTS, GraphEdgeRole.REFERENCE}
    assert {item.role for item in brief_node.accepts} == {GraphEdgeRole.FACTS, GraphEdgeRole.REFERENCE}
    assert {item.role for item in prompt.accepts} == {
        GraphEdgeRole.FACTS,
        GraphEdgeRole.REFERENCE,
        GraphEdgeRole.BRIEF,
        GraphEdgeRole.VISUAL_GUIDANCE,
    }
    visual = next(item for item in prompt.accepts if item.role == GraphEdgeRole.VISUAL_GUIDANCE)
    assert visual.max_count == 1
    required = {item.role: item.required_to_run for item in image.accepts}
    assert required[GraphEdgeRole.PROMPT] is True
    assert required[GraphEdgeRole.REFERENCE] is False
    prompt_required = next(item for item in image.accepts if item.role == GraphEdgeRole.PROMPT)
    assert prompt_required.max_count == 1
    assert all(item.data_type != graph_node_output_type(GraphNodeType.PRODUCT_SOURCE) for item in image.accepts)
    assert {field.key for field in image.config_fields} == {
        "image_type_key",
        "generation_spec",
        "delivery_spec",
        "variation_instruction",
        "visual_overlay",
        "visual_overrides",
    }
    generation = next(field for field in image.config_fields if field.key == "generation_spec")
    assert generation.control == "group"
    assert {child.key for child in generation.fields} == {
        "aspect_ratio",
        "resolution_tier",
        "quality_intent",
        "reference_fidelity",
        "background_intent",
        "text_policy",
        "text_language",
    }
    aspect = next(child for child in generation.fields if child.key == "aspect_ratio")
    assert aspect.control == "aspect_ratio"
    assert aspect.label_key == "agentWorkbench.nodeEditor.aspectRatio"
    prompt_object = next(field for field in prompt.config_fields if field.key == "prompt")
    assert {"design_goal", "product_fidelity", "composition"} <= {child.key for child in prompt_object.fields}
    visual_object = next(field for field in next(
        node for node in document.nodes if node.node_type == GraphNodeType.VISUAL_SYSTEM
    ).config_fields if field.key == "visual_overlay")
    colors = next(field for field in visual_object.fields if field.key == "colors")
    assert colors.value_kind == "object_list"
    assert {child.key for child in colors.fields} == {"role", "value", "label"}
    image_overlay = next(field for field in image.config_fields if field.key == "visual_overlay")
    assert image_overlay.control == "group"
    assert next(child for child in image_overlay.fields if child.key == "colors").value_kind == "object_list"
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
    reference_input = next(item for item in image["accepts"] if item["role"] == "reference")
    assert prompt_input["max_count"] == 1
    assert prompt_input["required_to_run"] is True
    assert prompt_input["data_type"] == "prompt"
    assert reference_input["required_to_run"] is False
    assert not any(item["data_type"] == "product_facts" for item in image["accepts"])
    assert {field["key"] for field in image["config_fields"]} == {
        "image_type_key",
        "generation_spec",
        "delivery_spec",
        "variation_instruction",
        "visual_overlay",
        "visual_overrides",
    }
    generation = next(field for field in image["config_fields"] if field["key"] == "generation_spec")
    aspect = next(child for child in generation["fields"] if child["key"] == "aspect_ratio")
    assert aspect["control"] == "aspect_ratio"
    assert aspect["label_key"] == "agentWorkbench.nodeEditor.aspectRatio"
    assert aspect["panel"] == "basic"


def test_catalog_json_is_the_http_and_agent_document() -> None:
    document = graph_catalog_document()
    payload = graph_catalog_json(document)
    assert payload == serialize_graph_catalog(document).model_dump(mode="json")
    image = next(node for node in payload["nodes"] if node["node_type"] == "image_generation")
    generation = next(field for field in image["config_fields"] if field["key"] == "generation_spec")
    assert generation["default"]["aspect_ratio"] == "1:1"


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


def test_catalog_recursively_validates_present_config_shapes_and_constraints() -> None:
    with pytest.raises(BusinessValidationError, match="未登记字段"):
        validate_node_config(
            GraphNodeType.PROMPT_GENERATION,
            {"prompt": {"text": {"unknown_field": True}}},
        )
    with pytest.raises(BusinessValidationError, match="value_kind"):
        validate_node_config(GraphNodeType.PROMPT_GENERATION, {"prompt": {"composition": "not-an-object"}})
    with pytest.raises(BusinessValidationError, match="choices"):
        validate_node_config(
            GraphNodeType.PROMPT_GENERATION,
            {"prompt": {"product_fidelity": {"picture_in_picture": "sometimes"}}},
        )
    with pytest.raises(BusinessValidationError, match="不能小于"):
        validate_node_config(
            GraphNodeType.PROMPT_GENERATION,
            {"prompt": {"composition": {"product_share_percent": 0}}},
        )
    with pytest.raises(BusinessValidationError, match="max_length"):
        validate_node_config(
            GraphNodeType.IMAGE_GENERATION,
            {"variation_instruction": "x" * 4001},
        )
    with pytest.raises(BusinessValidationError, match="未登记字段"):
        validate_node_config(
            GraphNodeType.IMAGE_GENERATION,
            {"visual_overlay": {"colors": [{"role": "background", "unknown": "#FFFFFF"}]}},
        )


def test_catalog_visual_overlay_keeps_inspector_fields_only() -> None:
    assert catalog_visual_overlay(
        {"style": ["干净白底"], "typography": {"title_font": "Inter"}, "spacing": {"min_edge_whitespace_percent": 10}}
    ) == {"style": ["干净白底"]}
    assert catalog_visual_overlay({"typography": {"title_font": "Inter"}}) is None


def test_hidden_json_fields_are_opaque_at_the_catalog_boundary() -> None:
    validate_node_config(
        GraphNodeType.IMAGE_GENERATION,
        {
            "visual_overrides": [
                {
                    "scope": {"type": "image_plan", "key": "hero-1"},
                    "overrides": [{"field": "colors", "value": [{"arbitrary": {"nested": True}}]}],
                }
            ]
        },
    )
    validate_node_config(
        GraphNodeType.VISUAL_SYSTEM,
        {"visual_overrides": [{"future_field": {"shape": ["is", "opaque"]}}]},
    )


def test_generation_and_delivery_specs_normalize_without_full_draft_validation() -> None:
    graph = apply_workflow_change_set(
        EMPTY_GRAPH,
        WorkflowChangeSet(
            base_graph_revision=0,
            summary="部分规格",
            operations=[
                CreateNodeOp(
                    client_ref="image",
                    node_type=GraphNodeType.IMAGE_GENERATION,
                    title="图",
                    config={
                        "generation_spec": {"aspect_ratio": "4:3"},
                        "delivery_spec": None,
                    },
                )
            ],
        ),
    )
    image = graph.node("image")
    assert image.config["generation_spec"] == {
        "aspect_ratio": "4:3",
        "resolution_tier": "high",
        "quality_intent": "high",
        "reference_fidelity": "high",
        "background_intent": "auto",
        "text_policy": "none",
        "text_language": None,
    }
    assert image.config["delivery_spec"] is None

    with pytest.raises(BusinessValidationError, match="generation_spec"):
        apply_workflow_change_set(
            EMPTY_GRAPH,
            WorkflowChangeSet(
                base_graph_revision=0,
                summary="文字规则冲突",
                operations=[
                    CreateNodeOp(
                        client_ref="image",
                        node_type=GraphNodeType.IMAGE_GENERATION,
                        title="图",
                        config={
                            "generation_spec": {
                                "aspect_ratio": "1:1",
                                "text_policy": "required",
                            }
                        },
                    )
                ],
            ),
        )
    with pytest.raises(BusinessValidationError, match="generation_spec"):
        apply_workflow_change_set(
            EMPTY_GRAPH,
            WorkflowChangeSet(
                base_graph_revision=0,
                summary="禁止文字规则冲突",
                operations=[
                    CreateNodeOp(
                        client_ref="image",
                        node_type=GraphNodeType.IMAGE_GENERATION,
                        title="图",
                        config={
                            "generation_spec": {
                                "aspect_ratio": "1:1",
                                "text_policy": "none",
                                "text_language": "zh-CN",
                            }
                        },
                    )
                ],
            ),
        )
    with pytest.raises(BusinessValidationError, match="delivery_spec"):
        apply_workflow_change_set(
            EMPTY_GRAPH,
            WorkflowChangeSet(
                base_graph_revision=0,
                summary="交付 fit 冲突",
                operations=[
                    CreateNodeOp(
                        client_ref="image",
                        node_type=GraphNodeType.IMAGE_GENERATION,
                        title="图",
                        config={
                            "generation_spec": {"aspect_ratio": "1:1"},
                            "delivery_spec": {
                                "width": 1200,
                                "height": 1200,
                                "format": "png",
                                "fit": "contain",
                                "crop_anchor": "center",
                            },
                        },
                    )
                ],
            ),
        )
    with pytest.raises(BusinessValidationError, match="delivery_spec"):
        apply_workflow_change_set(
            EMPTY_GRAPH,
            WorkflowChangeSet(
                base_graph_revision=0,
                summary="交付像素上限",
                operations=[
                    CreateNodeOp(
                        client_ref="image",
                        node_type=GraphNodeType.IMAGE_GENERATION,
                        title="图",
                        config={
                            "generation_spec": {"aspect_ratio": "1:1"},
                            "delivery_spec": {
                                "width": 16384,
                                "height": 4097,
                                "format": "webp",
                                "fit": "cover",
                                "crop_anchor": "center",
                            },
                        },
                    )
                ],
            ),
        )


def test_catalog_defaults_are_not_shared_between_documents_or_json_payloads() -> None:
    first_document = graph_catalog_document()
    first_image = next(node for node in first_document.nodes if node.node_type == GraphNodeType.IMAGE_GENERATION)
    first_generation = next(field for field in first_image.config_fields if field.key == "generation_spec")
    assert isinstance(first_generation.default, dict)
    first_generation.default["aspect_ratio"] = "9:16"

    second_document = graph_catalog_document()
    second_image = next(node for node in second_document.nodes if node.node_type == GraphNodeType.IMAGE_GENERATION)
    second_generation = next(field for field in second_image.config_fields if field.key == "generation_spec")
    assert second_generation.default["aspect_ratio"] == "1:1"

    first_json = graph_catalog_json()
    first_json_image = next(node for node in first_json["nodes"] if node["node_type"] == "image_generation")
    first_json_generation = next(
        field for field in first_json_image["config_fields"] if field["key"] == "generation_spec"
    )
    first_json_generation["default"]["aspect_ratio"] = "2:3"
    second_json = graph_catalog_json()
    second_json_image = next(node for node in second_json["nodes"] if node["node_type"] == "image_generation")
    second_json_generation = next(
        field for field in second_json_image["config_fields"] if field["key"] == "generation_spec"
    )
    assert second_json_generation["default"]["aspect_ratio"] == "1:1"


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


def test_image_generation_ready_with_prompt_and_reference_edges() -> None:
    image = GraphRuleNode(
        "image",
        GraphNodeType.IMAGE_GENERATION,
        config={"generation_spec": {"aspect_ratio": "1:1"}, "delivery_spec": None},
    )
    prompt_edge = GraphRuleEdge(
        "e1",
        "prompt",
        "image",
        graph_node_output_type(GraphNodeType.PROMPT_GENERATION),
        GraphEdgeRole.PROMPT,
    )
    reference_edge = GraphRuleEdge(
        "e2",
        "asset",
        "image",
        graph_node_output_type(GraphNodeType.IMAGE_ASSET),
        GraphEdgeRole.REFERENCE,
    )
    assert node_config_status(image, [prompt_edge]) == GraphConfigStatus.READY
    assert node_config_status(image, [prompt_edge, reference_edge]) == GraphConfigStatus.READY
    assert node_config_status(
        GraphRuleNode(
            "missing-spec",
            GraphNodeType.IMAGE_GENERATION,
            config={"delivery_spec": None},
        ),
        [prompt_edge, reference_edge],
    ) == GraphConfigStatus.INCOMPLETE
    assert node_config_status(
        GraphRuleNode(
            "bad-spec",
            GraphNodeType.IMAGE_GENERATION,
            config={
                "generation_spec": {
                    "aspect_ratio": "1:1",
                    "text_policy": "required",
                },
                "delivery_spec": None,
            },
        ),
        [prompt_edge, reference_edge],
    ) == GraphConfigStatus.INCOMPLETE


def test_prompt_partial_config_is_ready_without_required_inputs() -> None:
    prompt = GraphRuleNode(
        "prompt",
        GraphNodeType.PROMPT_GENERATION,
        config={"prompt": {"design_goal": "只填了目标"}},
    )
    assert node_config_status(prompt, []) == GraphConfigStatus.READY


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
