from __future__ import annotations

import pytest

from productflow_backend.application.product_workflow.graph_apply import EMPTY_GRAPH, apply_workflow_change_set
from productflow_backend.application.product_workflow.graph_contracts import FORBIDDEN_GRAPH_TOPOLOGY_KEYS
from productflow_backend.application.product_workflow.graph_template import (
    DirectCreateImageType,
    build_direct_create_template,
)
from productflow_backend.domain.enums import GraphConfigStatus, GraphNodeType
from productflow_backend.domain.errors import BusinessValidationError


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
    assert asset_outgoing == []
    assert graph.config_status("image-asset-1") == GraphConfigStatus.READY
    assert graph.config_status("image-hero-1") == GraphConfigStatus.READY
    assert graph.config_status("visual-system") == GraphConfigStatus.INCOMPLETE
    assert graph.config_status("prompt-hero") == GraphConfigStatus.READY

    for node in graph.nodes:
        assert FORBIDDEN_GRAPH_TOPOLOGY_KEYS.isdisjoint(node.config)


def test_direct_create_template_enforces_image_quantity_cap() -> None:
    with pytest.raises(BusinessValidationError, match="30"):
        build_direct_create_template(
            image_types=[
                DirectCreateImageType(key=key, quantity=6, order=index)
                for index, key in enumerate(("hero", "detail", "scene", "sku", "selling_point", "packaging"))
            ],
            reference_asset_ids=["asset-a"],
        )
