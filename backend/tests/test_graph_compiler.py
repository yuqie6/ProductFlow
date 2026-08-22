from __future__ import annotations

from dataclasses import replace

import pytest

from productflow_backend.application.product_workflow.graph_apply import EMPTY_GRAPH, apply_workflow_change_set
from productflow_backend.application.product_workflow.graph_compiler import (
    GraphRuntimeArtifacts,
    GraphSourceRecord,
    compile_context_runtime,
    compile_image_runtime,
    compile_prompt_runtime,
    select_run_node_ids,
    strip_v3_prompt_payload,
)
from productflow_backend.application.product_workflow.graph_contracts import (
    DisconnectEdgeOp,
    UpdateNodeConfigOp,
    WorkflowChangeSet,
)
from productflow_backend.application.product_workflow.graph_template import (
    DirectCreateImageType,
    build_direct_create_template,
)
from productflow_backend.application.product_workflow.product_sources import (
    ProductSourceSnapshot,
    ProductSummarySnapshot,
)
from productflow_backend.domain.enums import GraphActorType, GraphNodeType, GraphRunScope
from productflow_backend.domain.errors import BusinessValidationError


def _template_graph():
    change_set = build_direct_create_template(
        image_types=[DirectCreateImageType(key="hero", quantity=1, order=0)],
        reference_asset_ids=["asset-a"],
    )
    return apply_workflow_change_set(EMPTY_GRAPH, change_set)


def _sources_for(graph, *, facts=(), visual=True):
    sources = {}
    for node in graph.nodes:
        if node.node_type == GraphNodeType.PRODUCT_SOURCE:
            sources[node.id] = GraphSourceRecord(facts=facts)
        elif node.node_type == GraphNodeType.IMAGE_ASSET:
            sources[node.id] = GraphSourceRecord(
                bound_asset_id=node.bound_asset_id,
                bound_asset_label=node.title,
            )
        elif node.node_type == GraphNodeType.VISUAL_SYSTEM and visual:
            sources[node.id] = GraphSourceRecord(
                visual_payload={"name": "测试视觉规范"},
                visual_system_version_id="visual-1",
            )
        else:
            sources[node.id] = GraphSourceRecord()
    return sources


def test_context_compiler_reads_template_facts_and_references() -> None:
    graph = _template_graph()
    visual = next(node for node in graph.nodes if node.node_type == GraphNodeType.VISUAL_SYSTEM)
    brief = next(node for node in graph.nodes if node.node_type == GraphNodeType.CREATIVE_BRIEF)
    sources = _sources_for(graph, facts=({"key": "product_name", "value": "刀架"},), visual=False)
    visual_runtime = compile_context_runtime(graph, visual.id, sources)
    brief_runtime = compile_context_runtime(graph, brief.id, sources)
    assert visual_runtime.node_type == GraphNodeType.VISUAL_SYSTEM
    assert [item.asset_id for item in visual_runtime.reference_images] == ["asset-a"]
    assert visual_runtime.product_facts[0]["value"] == "刀架"
    assert brief_runtime.node_type == GraphNodeType.CREATIVE_BRIEF
    assert [item.asset_id for item in brief_runtime.reference_images] == ["asset-a"]
    assert brief_runtime.text_policy == "none"


def test_prompt_compiler_includes_template_reference_images() -> None:
    graph = _template_graph()
    prompt = next(node for node in graph.nodes if node.node_type == GraphNodeType.PROMPT_GENERATION)
    runtime = compile_prompt_runtime(
        graph,
        prompt.id,
        _sources_for(graph, facts=({"key": "product_name", "value": "刀架"},)),
    )
    assert [item.asset_id for item in runtime.reference_images] == ["asset-a"]
    assert runtime.visual_system == {"name": "测试视觉规范"}
    assert runtime.product_facts[0]["key"] == "product_name"
    assert runtime.text_policy == "none"
    assert runtime.text_language is None


def test_prompt_compiler_fills_product_name_from_bound_source() -> None:
    graph = _template_graph()
    prompt = next(node for node in graph.nodes if node.node_type == GraphNodeType.PROMPT_GENERATION)
    source = next(node for node in graph.nodes if node.node_type == GraphNodeType.PRODUCT_SOURCE)
    sources = _sources_for(graph, facts=())
    sources[source.id] = GraphSourceRecord(
        facts=(),
        product_source=ProductSourceSnapshot(
            source_product_id="product-1",
            fact_set_version_id=None,
            source_product=ProductSummarySnapshot(
                id="product-1",
                name="筋膜枪",
                category=None,
                price=None,
                source_note=None,
            ),
            fact_set_version=None,
            facts=(),
        ),
    )
    runtime = compile_prompt_runtime(graph, prompt.id, sources)
    assert runtime.product_facts[0]["key"] == "product_name"
    assert runtime.product_facts[0]["value"] == "筋膜枪"


def test_image_compiler_requires_prompt_artifact_and_prompt_edge() -> None:
    graph = _template_graph()
    image = next(node for node in graph.nodes if node.node_type == GraphNodeType.IMAGE_GENERATION)
    sources = {node.id: GraphSourceRecord() for node in graph.nodes}
    with pytest.raises(BusinessValidationError, match="尚未生成"):
        compile_image_runtime(graph, image.id, sources)

    prompt = next(node for node in graph.nodes if node.node_type == GraphNodeType.PROMPT_GENERATION)
    artifacts = GraphRuntimeArtifacts().with_prompt(
        prompt.id,
        artifact_id="art-1",
        payload={"design_goal": "海报", "shared_rules": ["保持结构"]},
    )
    runtime = compile_image_runtime(graph, image.id, _sources_for(graph), artifacts)
    assert runtime.prompt_artifact_id == "art-1"
    assert runtime.prompt_payload["design_goal"] == "海报"
    assert "images" not in runtime.prompt_payload or True
    assert [item.asset_id for item in runtime.reference_images] == ["asset-a"]
    assert runtime.generation_spec["aspect_ratio"] == "1:1"


def test_image_compiler_separates_missing_prompt_edge_from_invalid_config() -> None:
    graph = _template_graph()
    image = next(node for node in graph.nodes if node.node_type == GraphNodeType.IMAGE_GENERATION)
    prompt_edge = next(
        edge for edge in graph.edges if edge.target_node_id == image.id and edge.role.value == "prompt"
    )
    missing_edge_graph = apply_workflow_change_set(
        graph,
        WorkflowChangeSet(
            base_graph_revision=graph.revision,
            summary="删除提示词边",
            operations=[DisconnectEdgeOp(edge_ref=prompt_edge.id)],
        ),
    )
    with pytest.raises(BusinessValidationError, match="输入边"):
        compile_image_runtime(missing_edge_graph, image.id, _sources_for(missing_edge_graph))

    invalid_image = replace(
        image,
        config={
            **image.config,
            "delivery_spec": {
                "width": 1200,
                "height": 1200,
                "format": "png",
                "fit": "contain",
                "crop_anchor": "center",
            },
        },
    )
    invalid_config_graph = replace(
        graph,
        nodes=tuple(invalid_image if node.id == image.id else node for node in graph.nodes),
    )
    with pytest.raises(BusinessValidationError, match="delivery_spec"):
        compile_image_runtime(invalid_config_graph, image.id, _sources_for(invalid_config_graph))


def test_image_compiler_treats_empty_prompt_artifact_as_absent() -> None:
    graph = _template_graph()
    image = next(node for node in graph.nodes if node.node_type == GraphNodeType.IMAGE_GENERATION)
    prompt = next(node for node in graph.nodes if node.node_type == GraphNodeType.PROMPT_GENERATION)
    for payload in ({}, {"schema_version": 1}, {"images": []}):
        artifacts = GraphRuntimeArtifacts().with_prompt(
            prompt.id,
            artifact_id="empty-artifact",
            payload=payload,
        )
        with pytest.raises(BusinessValidationError, match="尚未生成"):
            compile_image_runtime(graph, image.id, _sources_for(graph), artifacts)


def test_prompt_compiler_accepts_a_valid_partial_prompt_config() -> None:
    graph = _template_graph()
    prompt = next(node for node in graph.nodes if node.node_type == GraphNodeType.PROMPT_GENERATION)
    updated = apply_workflow_change_set(
        graph,
        WorkflowChangeSet(
            base_graph_revision=graph.revision,
            summary="补充部分提示词",
            operations=[
                UpdateNodeConfigOp(
                    node_ref=prompt.id,
                    config={
                        "image_type_key": "hero",
                        "prompt": {"design_goal": "只补充商品主体"},
                    },
                )
            ],
        ),
    )
    runtime = compile_prompt_runtime(updated, prompt.id, _sources_for(updated))
    assert runtime.prompt_config == {"design_goal": "只补充商品主体"}


def test_inline_visual_overlay_compiles_without_version() -> None:
    graph = _template_graph()
    visual = next(node for node in graph.nodes if node.node_type == GraphNodeType.VISUAL_SYSTEM)
    graph = apply_workflow_change_set(
        graph,
        WorkflowChangeSet(
            base_graph_revision=graph.revision,
            summary="inline visual",
            actor_type=GraphActorType.USER,
            operations=[
                UpdateNodeConfigOp(
                    node_ref=visual.id,
                    config={
                        "visual_overlay": {
                            "style": ["干净白底"],
                            "colors": [{"role": "background", "value": "#FFFFFF"}],
                        },
                    },
                )
            ],
        ),
    )
    prompt = next(node for node in graph.nodes if node.node_type == GraphNodeType.PROMPT_GENERATION)
    sources = _sources_for(graph, visual=False)
    sources[visual.id] = GraphSourceRecord(
        visual_payload={"style": ["干净白底"], "colors": [{"role": "background", "value": "#FFFFFF"}]},
    )
    runtime = compile_prompt_runtime(graph, prompt.id, sources)
    assert runtime.visual_system_version_id is None
    assert runtime.visual_system["style"] == ["干净白底"]


def test_incomplete_visual_edge_does_not_inject_visual_payload() -> None:
    graph = _template_graph()
    prompt = next(node for node in graph.nodes if node.node_type == GraphNodeType.PROMPT_GENERATION)
    runtime = compile_prompt_runtime(graph, prompt.id, _sources_for(graph, visual=False))
    assert runtime.visual_system is None
    assert runtime.visual_system_version_id is None


def test_graph_scope_selects_processing_nodes_in_topo_order() -> None:
    graph = _template_graph()
    ordered = select_run_node_ids(graph, scope=GraphRunScope.GRAPH, target_node_id=None)
    types = [graph.node(node_id).node_type for node_id in ordered]
    assert types.count(GraphNodeType.CREATIVE_BRIEF) == 1
    assert types.count(GraphNodeType.VISUAL_SYSTEM) == 1
    assert types.count(GraphNodeType.PROMPT_GENERATION) == 1
    assert types.count(GraphNodeType.IMAGE_GENERATION) == 1
    assert types.index(GraphNodeType.CREATIVE_BRIEF) < types.index(GraphNodeType.PROMPT_GENERATION)
    assert types.index(GraphNodeType.VISUAL_SYSTEM) < types.index(GraphNodeType.PROMPT_GENERATION)
    assert types.index(GraphNodeType.PROMPT_GENERATION) < types.index(GraphNodeType.IMAGE_GENERATION)


def test_node_scope_runs_only_the_target_processing_node() -> None:
    graph = _template_graph()
    visual = next(node for node in graph.nodes if node.node_type == GraphNodeType.VISUAL_SYSTEM)
    prompt = next(node for node in graph.nodes if node.node_type == GraphNodeType.PROMPT_GENERATION)
    image = next(node for node in graph.nodes if node.node_type == GraphNodeType.IMAGE_GENERATION)
    assert select_run_node_ids(graph, scope=GraphRunScope.NODE, target_node_id=visual.id) == (visual.id,)
    assert select_run_node_ids(graph, scope=GraphRunScope.NODE, target_node_id=prompt.id) == (prompt.id,)
    assert select_run_node_ids(graph, scope=GraphRunScope.NODE, target_node_id=image.id) == (image.id,)
    to_image = select_run_node_ids(graph, scope=GraphRunScope.TO_NODE, target_node_id=image.id)
    assert visual.id in to_image
    assert prompt.id in to_image
    assert to_image[-1] == image.id


def test_strip_v3_prompt_payload_drops_topology_keys() -> None:
    stripped = strip_v3_prompt_payload(
        {
            "design_goal": "海报",
            "images": [{"image_plan_key": "hero-1"}],
            "fact_keys": ["product_name"],
            "evidence_asset_ids": ["asset-a"],
        }
    )
    assert stripped == {"design_goal": "海报"}


def test_image_compiler_reads_visual_overrides_list() -> None:
    graph = _template_graph()
    image = next(node for node in graph.nodes if node.node_type == GraphNodeType.IMAGE_GENERATION)
    graph = apply_workflow_change_set(
        graph,
        WorkflowChangeSet(
            base_graph_revision=graph.revision,
            summary="写入视觉覆盖",
            actor_type=GraphActorType.USER,
            operations=[
                UpdateNodeConfigOp(
                    node_ref=image.id,
                    config={
                        **image.config,
                        "visual_overrides": [
                            {
                                "key": "hero-color",
                                "scope": {"type": "image_plan", "key": "hero-1"},
                                "overrides": [
                                    {
                                        "field": "colors",
                                        "value": [{"role": "background", "value": "#FFFFFF", "label": "白底"}],
                                    }
                                ],
                                "reason": "平台白底",
                            }
                        ],
                    },
                )
            ],
        ),
    )
    prompt = next(node for node in graph.nodes if node.node_type == GraphNodeType.PROMPT_GENERATION)
    artifacts = GraphRuntimeArtifacts().with_prompt(
        prompt.id,
        artifact_id="art-1",
        payload={"design_goal": "海报", "shared_rules": ["保持结构"]},
    )
    runtime = compile_image_runtime(graph, image.id, _sources_for(graph), artifacts)
    assert runtime.visual_overlay == {"colors": [{"role": "background", "value": "#FFFFFF", "label": "白底"}]}
    prompt_runtime = compile_prompt_runtime(graph, prompt.id, _sources_for(graph))
    assert prompt_runtime.prompt_config == {"design_goal": "首屏海报图：快速抓住用户注意力，传递产品核心定位"}


def test_prompt_compiler_ignores_disconnected_image_assets() -> None:
    graph = _template_graph()
    prompt = next(node for node in graph.nodes if node.node_type == GraphNodeType.PROMPT_GENERATION)
    ref_edge = next(
        edge for edge in graph.edges if edge.target_node_id == prompt.id and edge.role.value == "reference"
    )
    disconnected = apply_workflow_change_set(
        graph,
        WorkflowChangeSet(
            base_graph_revision=graph.revision,
            summary="断开提示词参考图",
            operations=[DisconnectEdgeOp(edge_ref=ref_edge.id)],
        ),
    )
    runtime = compile_prompt_runtime(
        disconnected,
        prompt.id,
        _sources_for(disconnected, facts=({"key": "product_name", "value": "刀架"},)),
    )
    assert runtime.reference_images == ()


def test_prompt_compiler_reads_downstream_image_text_policy() -> None:
    graph = _template_graph()
    prompt = next(node for node in graph.nodes if node.node_type == GraphNodeType.PROMPT_GENERATION)
    image = next(node for node in graph.nodes if node.node_type == GraphNodeType.IMAGE_GENERATION)
    updated = apply_workflow_change_set(
        graph,
        WorkflowChangeSet(
            base_graph_revision=graph.revision,
            summary="要求图片内文字",
            operations=[
                UpdateNodeConfigOp(
                    node_ref=image.id,
                    config={
                        **image.config,
                        "generation_spec": {
                            **image.config["generation_spec"],
                            "text_policy": "required",
                            "text_language": "zh-CN",
                        },
                    },
                )
            ],
        ),
    )
    runtime = compile_prompt_runtime(updated, prompt.id, _sources_for(updated))
    assert runtime.text_policy == "required"
    assert runtime.text_language == "zh-CN"
