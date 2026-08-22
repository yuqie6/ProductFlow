from __future__ import annotations

from workflow_draft_helpers import make_workflow_draft_payload

from productflow_backend.application.product_workflow.draft_graph_adapter import build_draft_initial_graph_change_set
from productflow_backend.application.product_workflow.graph_apply import EMPTY_GRAPH, apply_workflow_change_set
from productflow_backend.application.product_workflow.graph_contracts import FORBIDDEN_GRAPH_TOPOLOGY_KEYS
from productflow_backend.application.workflow_drafts.contracts import WorkflowDraftPayloadV1
from productflow_backend.domain.enums import GraphConfigStatus, GraphEdgeRole, GraphNodeType


def test_draft_adapter_maps_fixture_without_plan_keys() -> None:
    payload = WorkflowDraftPayloadV1.model_validate(make_workflow_draft_payload())
    change_set = build_draft_initial_graph_change_set(payload, draft_revision_id="rev-1")
    graph = apply_workflow_change_set(EMPTY_GRAPH, change_set)

    types = {node.id: node.node_type for node in graph.nodes}
    assert types["product-context"] == GraphNodeType.PRODUCT_SOURCE
    assert types["product-reference-node"] == GraphNodeType.IMAGE_ASSET
    assert types["hero-prompt-node"] == GraphNodeType.PROMPT_GENERATION
    assert types["hero-image-1-node"] == GraphNodeType.IMAGE_GENERATION
    assert types["visual-system"] == GraphNodeType.VISUAL_SYSTEM
    assert types["creative-brief"] == GraphNodeType.CREATIVE_BRIEF
    assert {group.id for group in graph.groups} == {"hero-folder"}
    assert graph.node("hero-prompt-node").group_id == "hero-folder"

    prompt_config = graph.node("hero-prompt-node").config
    assert "images" not in prompt_config.get("prompt", {})
    assert "fact_keys" not in prompt_config.get("prompt", {})
    assert "evidence_asset_ids" not in prompt_config.get("prompt", {})
    for node in graph.nodes:
        assert FORBIDDEN_GRAPH_TOPOLOGY_KEYS.isdisjoint(node.config)

    reference_edges = [
        edge
        for edge in graph.edges
        if edge.source_node_id == "product-reference-node" and edge.role == GraphEdgeRole.REFERENCE
    ]
    assert {edge.target_node_id for edge in reference_edges} == {
        "hero-prompt-node",
        "hero-image-1-node",
        "hero-image-2-node",
    }
    assert graph.config_status("hero-image-1-node") == GraphConfigStatus.READY


def test_draft_adapter_drops_facts_edges_into_image_nodes() -> None:
    payload = make_workflow_draft_payload()
    payload["edges"].append(
        {
            "key": "context-to-image-1",
            "source_node_key": "product-context",
            "target_node_key": "hero-image-1-node",
            "source_handle": "facts",
            "target_handle": "facts",
        }
    )
    mapped = WorkflowDraftPayloadV1.model_validate(payload)
    graph = apply_workflow_change_set(
        EMPTY_GRAPH,
        build_draft_initial_graph_change_set(mapped, draft_revision_id="rev-1"),
    )
    assert not any(
        edge.source_node_id == "product-context" and edge.target_node_id == "hero-image-1-node" for edge in graph.edges
    )


def test_draft_adapter_keeps_explicit_reference_edges_without_duplicates() -> None:
    payload = make_workflow_draft_payload()
    payload["edges"].append(
        {
            "key": "reference-to-image-1",
            "source_node_key": "product-reference-node",
            "target_node_key": "hero-image-1-node",
            "source_handle": "asset",
            "target_handle": "reference",
        }
    )
    graph = apply_workflow_change_set(
        EMPTY_GRAPH,
        build_draft_initial_graph_change_set(
            WorkflowDraftPayloadV1.model_validate(payload),
            draft_revision_id="rev-1",
        ),
    )
    image_reference_edges = [
        edge
        for edge in graph.edges
        if edge.source_node_id == "product-reference-node"
        and edge.target_node_id == "hero-image-1-node"
        and edge.role == GraphEdgeRole.REFERENCE
    ]
    assert len(image_reference_edges) == 1


def test_draft_adapter_maps_visual_exceptions_to_overlay() -> None:
    payload = make_workflow_draft_payload()
    payload["visual_exceptions"] = [
        {
            "key": "workflow-color",
            "scope": {"type": "workflow"},
            "overrides": [
                {
                    "field": "colors",
                    "value": [{"role": "background", "value": "#111111", "label": "工作流背景"}],
                }
            ],
            "reason": "全图压暗背景",
        },
        {
            "key": "hero-1-color",
            "scope": {"type": "image_plan", "key": "hero-1"},
            "overrides": [
                {
                    "field": "colors",
                    "value": [{"role": "background", "value": "#FFFFFF", "label": "纯白背景"}],
                }
            ],
            "reason": "平台白底图要求",
        },
    ]
    graph = apply_workflow_change_set(
        EMPTY_GRAPH,
        build_draft_initial_graph_change_set(
            WorkflowDraftPayloadV1.model_validate(payload),
            draft_revision_id="rev-1",
        ),
    )
    visual = graph.node("visual-system")
    assert visual.config["visual_overlay"]["colors"][0]["value"] == "#111111"
    image = graph.node("hero-image-1-node")
    assert image.config["visual_overlay"]["colors"][0]["value"] == "#FFFFFF"
    brief = graph.node("creative-brief")
    assert brief.config["goal"] == brief.config["design_goals"][0]


def test_draft_adapter_keeps_non_catalog_exceptions_out_of_visual_overlay() -> None:
    payload = make_workflow_draft_payload()
    payload["visual_exceptions"] = [
        {
            "key": "workflow-type",
            "scope": {"type": "workflow"},
            "overrides": [
                {
                    "field": "typography",
                    "value": {
                        "title_font": "思源黑体 Bold",
                        "body_font": "思源黑体 Regular",
                        "scale": {"headline": 3, "subtitle": 1.8, "body": 1},
                    },
                },
                {"field": "style", "value": ["干净白底"]},
            ],
            "reason": "全图字体和风格",
        }
    ]
    graph = apply_workflow_change_set(
        EMPTY_GRAPH,
        build_draft_initial_graph_change_set(
            WorkflowDraftPayloadV1.model_validate(payload),
            draft_revision_id="rev-1",
        ),
    )
    visual = graph.node("visual-system")
    assert visual.config["visual_overlay"] == {"style": ["干净白底"]}
    assert visual.config["visual_overrides"][0]["overrides"][0]["field"] == "typography"
