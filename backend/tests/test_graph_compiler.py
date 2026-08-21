from __future__ import annotations

import pytest

from productflow_backend.application.product_workflow.graph_apply import EMPTY_GRAPH, apply_workflow_change_set
from productflow_backend.application.product_workflow.graph_contracts import UpdateNodeConfigOp, WorkflowChangeSet
from productflow_backend.application.product_workflow.graph_compiler import (
    GraphRuntimeArtifacts,
    GraphSourceRecord,
    compile_image_runtime,
    compile_prompt_runtime,
    select_run_node_ids,
    strip_v3_prompt_payload,
)
from productflow_backend.application.product_workflow.graph_template import (
    DirectCreateImageType,
    build_direct_create_template,
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


def test_prompt_compiler_ignores_unused_image_assets() -> None:
    graph = _template_graph()
    prompt = next(node for node in graph.nodes if node.node_type == GraphNodeType.PROMPT_GENERATION)
    runtime = compile_prompt_runtime(
        graph,
        prompt.id,
        _sources_for(graph, facts=({"key": "product_name", "value": "刀架"},)),
    )
    assert runtime.reference_images == ()
    assert runtime.visual_system == {"name": "测试视觉规范"}
    assert runtime.product_facts[0]["key"] == "product_name"
    assert all(item.asset_id != "asset-a" for item in runtime.reference_images)


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
    assert runtime.reference_images == ()
    assert runtime.generation_spec["aspect_ratio"] == "1:1"


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
    assert types.count(GraphNodeType.PROMPT_GENERATION) == 1
    assert types.count(GraphNodeType.IMAGE_GENERATION) == 1
    assert types[0] == GraphNodeType.PROMPT_GENERATION


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
    assert prompt_runtime.prompt_config == {}
