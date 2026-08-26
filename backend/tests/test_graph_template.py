from __future__ import annotations

import pytest

from productflow_backend.application.product_workflow.graph_apply import EMPTY_GRAPH, apply_workflow_change_set
from productflow_backend.application.product_workflow.graph_contracts import FORBIDDEN_GRAPH_TOPOLOGY_KEYS, CreateNodeOp
from productflow_backend.application.product_workflow.graph_template import (
    DirectCreateImageType,
    build_direct_create_template,
    build_product_source_create_graph,
    template_for_existing_product_source,
)
from productflow_backend.domain.enums import GraphConfigStatus, GraphNodeType
from productflow_backend.domain.errors import BusinessValidationError
from productflow_backend.domain.image_type_catalog import LISTING_LOOK_RULE, image_type_prompt_goal


def test_direct_create_template_builds_incomplete_runnable_structure() -> None:
    change_set = build_direct_create_template(
        image_types=[
            DirectCreateImageType(key="hero", quantity=2, order=0, title="首屏海报图"),
            DirectCreateImageType(key="detail", quantity=1, order=1, title="细节展示图"),
        ],
        reference_asset_ids=["asset-a", "asset-b"],
    )
    graph = apply_workflow_change_set(EMPTY_GRAPH, change_set)

    types = {node.id: node.node_type for node in graph.nodes}
    assert types["product-source"] == GraphNodeType.PRODUCT_SOURCE
    assert types["visual-system"] == GraphNodeType.VISUAL_SYSTEM
    assert types["creative-brief"] == GraphNodeType.CREATIVE_BRIEF
    assert types["image-asset-1"] == GraphNodeType.IMAGE_ASSET
    assert types["prompt-hero"] == GraphNodeType.PROMPT_GENERATION
    assert types["image-hero-1"] == GraphNodeType.IMAGE_GENERATION
    assert types["image-hero-2"] == GraphNodeType.IMAGE_GENERATION
    assert types["image-detail-1"] == GraphNodeType.IMAGE_GENERATION
    assert graph.nodes.__len__() == 10

    asset_outgoing = [edge for edge in graph.edges if edge.source_node_id.startswith("image-asset-")]
    image_ids = {node.id for node in graph.nodes if node.node_type == GraphNodeType.IMAGE_GENERATION}
    prompt_ids = {node.id for node in graph.nodes if node.node_type == GraphNodeType.PROMPT_GENERATION}
    context_ids = {"visual-system", "creative-brief"}
    assert {edge.target_node_id for edge in asset_outgoing} == image_ids | prompt_ids | context_ids
    assert any(
        edge.source_node_id == "product-source" and edge.target_node_id == "visual-system" for edge in graph.edges
    )
    assert any(
        edge.source_node_id == "product-source" and edge.target_node_id == "creative-brief" for edge in graph.edges
    )
    hero_prompt = graph.node("prompt-hero")
    assert hero_prompt.config["prompt"]["design_goal"] == image_type_prompt_goal("hero")
    assert graph.config_status("image-asset-1") == GraphConfigStatus.READY
    assert graph.config_status("image-hero-1") == GraphConfigStatus.READY
    assert graph.config_status("visual-system") == GraphConfigStatus.INCOMPLETE
    assert graph.config_status("prompt-hero") == GraphConfigStatus.READY
    assert graph.node("creative-brief").config == {}
    assert graph.node("image-hero-1").config["generation_spec"]["text_policy"] == "none"
    assert graph.node("image-hero-1").config["generation_spec"]["aspect_ratio"] == "1:1"

    for node in graph.nodes:
        assert FORBIDDEN_GRAPH_TOPOLOGY_KEYS.isdisjoint(node.config)
    assert {group.id for group in graph.groups} == {"shot-hero", "shot-detail"}
    assert graph.node("prompt-hero").group_id == "shot-hero"
    assert graph.node("image-hero-1").group_id == "shot-hero"
    assert graph.node("image-hero-2").group_id == "shot-hero"
    assert graph.node("image-asset-1").config["role"] == "product_identity"


def test_direct_create_template_enforces_image_quantity_cap() -> None:
    with pytest.raises(BusinessValidationError, match="30"):
        build_direct_create_template(
            image_types=[
                DirectCreateImageType(key=key, quantity=6, order=index)
                for index, key in enumerate(("hero", "detail", "scene", "sku", "selling_point", "packaging"))
            ],
            reference_asset_ids=["asset-a"],
        )


def test_direct_create_template_writes_brief_and_generation_spec() -> None:
    change_set = build_direct_create_template(
        image_types=[DirectCreateImageType(key="hero", quantity=1, order=0, title="首屏海报图")],
        reference_asset_ids=["asset-a"],
        source_note="无线洗地机，面向注重清洁效率的都市白领，画面干净专业",
        generation_spec={
            "aspect_ratio": "3:4",
            "text_policy": "required",
            "text_language": "zh-CN",
        },
    )
    graph = apply_workflow_change_set(EMPTY_GRAPH, change_set)
    brief = graph.node("creative-brief")
    image = graph.node("image-hero-1")
    assert brief.config["goal"] == LISTING_LOOK_RULE
    assert brief.config["design_goals"] == ["商品与受众资料：无线洗地机，面向注重清洁效率的都市白领，画面干净专业"]
    assert "不要极简大留白、浅灰空棚、杂志静物" in brief.config["prohibitions"]
    assert "不要爆炸贴、满屏色块、牛皮癣标签" in brief.config["prohibitions"]
    assert image.config["generation_spec"]["aspect_ratio"] == "3:4"
    assert image.config["generation_spec"]["text_policy"] == "required"
    assert image.config["generation_spec"]["text_language"] == "zh-CN"
    assert image.config["generation_spec"]["resolution_tier"] == "high"


def test_direct_create_template_applies_per_type_aspect_ratio() -> None:
    change_set = build_direct_create_template(
        image_types=[
            DirectCreateImageType(key="hero", quantity=1, order=0, aspect_ratio="3:4"),
            DirectCreateImageType(key="detail", quantity=1, order=1, aspect_ratio="1:1"),
        ],
        reference_asset_ids=["asset-a"],
        generation_spec={
            "text_policy": "required",
            "text_language": "zh-CN",
        },
    )
    graph = apply_workflow_change_set(EMPTY_GRAPH, change_set)
    hero = graph.node("image-hero-1").config["generation_spec"]
    detail = graph.node("image-detail-1").config["generation_spec"]
    assert hero["aspect_ratio"] == "3:4"
    assert detail["aspect_ratio"] == "1:1"
    assert hero["text_policy"] == "required"
    assert detail["text_policy"] == "required"
    assert hero["text_language"] == "zh-CN"


def test_direct_create_template_builds_shot_groups_and_evidence_placeholders() -> None:
    change_set = build_direct_create_template(
        image_types=[
            DirectCreateImageType(key="hero", quantity=2, order=0, title="首屏海报图"),
            DirectCreateImageType(key="certification", quantity=2, order=1, title="资质认证图"),
        ],
        reference_asset_ids=["asset-a", "asset-b"],
    )
    graph = apply_workflow_change_set(EMPTY_GRAPH, change_set)
    prompt_ids = [node.id for node in graph.nodes if node.node_type == GraphNodeType.PROMPT_GENERATION]
    image_ids = [node.id for node in graph.nodes if node.node_type == GraphNodeType.IMAGE_GENERATION]
    evidence = graph.node("evidence-certification")
    identity_ids = {"image-asset-1", "image-asset-2"}
    assert prompt_ids == ["prompt-hero"]
    assert sorted(image_ids) == ["image-hero-1", "image-hero-2"]
    assert graph.node("prompt-hero").group_id == "shot-hero"
    assert graph.node("image-hero-1").group_id == "shot-hero"
    assert graph.node("image-hero-2").group_id == "shot-hero"
    assert evidence.node_type == GraphNodeType.IMAGE_ASSET
    assert evidence.bound_asset_id is None
    assert evidence.config["role"] == "evidence"
    assert evidence.group_id is None
    outgoing_targets = {edge.target_node_id for edge in graph.edges if edge.source_node_id in identity_ids}
    assert evidence.id not in outgoing_targets
    assert {"visual-system", "creative-brief", "prompt-hero", "image-hero-1", "image-hero-2"} <= outgoing_targets


def test_direct_create_template_rejects_required_text_without_language() -> None:
    with pytest.raises(BusinessValidationError, match="出图设定无效"):
        build_direct_create_template(
            image_types=[DirectCreateImageType(key="hero", quantity=1, order=0)],
            reference_asset_ids=["asset-a"],
            generation_spec={"text_policy": "required"},
        )


def test_template_for_existing_product_source_reuses_birth_node() -> None:
    birth = apply_workflow_change_set(
        EMPTY_GRAPH,
        build_product_source_create_graph(product_title="待确认商品", source_product_id="prod-1"),
    )
    change_set = template_for_existing_product_source(
        product_source_node_id="product-source",
        base_graph_revision=birth.revision,
        image_types=[DirectCreateImageType(key="hero", quantity=1, order=0, title="首屏海报图")],
        reference_asset_ids=["asset-a"],
        product_title="待确认商品",
        source_product_id="prod-1",
    )

    assert not any(
        isinstance(operation, CreateNodeOp) and operation.client_ref == "product-source"
        for operation in change_set.operations
    )
    assert change_set.base_graph_revision == birth.revision

    expanded = apply_workflow_change_set(birth, change_set)
    assert expanded.node("product-source").node_type == GraphNodeType.PRODUCT_SOURCE
    types = {node.node_type for node in expanded.nodes}
    assert GraphNodeType.VISUAL_SYSTEM in types
    assert GraphNodeType.CREATIVE_BRIEF in types
    assert GraphNodeType.IMAGE_ASSET in types
    assert GraphNodeType.PROMPT_GENERATION in types
    assert GraphNodeType.IMAGE_GENERATION in types
    assert any(
        edge.source_node_id == "product-source" and edge.target_node_id == "visual-system"
        for edge in expanded.edges
    )
