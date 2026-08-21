from __future__ import annotations

import hashlib
import json
from dataclasses import dataclass, field
from typing import Any

from productflow_backend.application.product_workflow.graph_apply import (
    AppliedGraph,
    AppliedGraphEdge,
    AppliedGraphGroup,
    AppliedGraphNode,
)
from productflow_backend.application.product_workflow.graph_visual import (
    apply_visual_overlay,
    visual_overlay_from_config,
)
from productflow_backend.application.workflow_drafts.contracts import GenerationSpec
from productflow_backend.domain.enums import (
    GraphArtifactType,
    GraphEdgeRole,
    GraphNodeType,
    GraphRunScope,
)
from productflow_backend.domain.errors import BusinessValidationError
from productflow_backend.domain.graph_catalog import PROCESSING_NODE_TYPES, run_required_inputs
from productflow_backend.domain.graph_rules import GraphRuleEdge, GraphRuleNode, node_config_status

GRAPH_SNAPSHOT_SCHEMA_VERSION = 1
V3_PROMPT_STRIPPED_KEYS = frozenset(
    {
        "images",
        "fact_keys",
        "evidence_asset_ids",
        "prompt_plan_key",
        "image_plan_key",
        "prompt_plan_keys",
        "image_plan_keys",
    }
)


@dataclass(frozen=True, slots=True)
class GraphSourceRecord:
    facts: tuple[dict[str, Any], ...] = ()
    brief: dict[str, Any] | None = None
    visual_payload: dict[str, Any] | None = None
    visual_system_version_id: str | None = None
    bound_asset_id: str | None = None
    bound_asset_label: str | None = None
    bound_asset_mime_type: str | None = None
    current_artifact_id: str | None = None
    current_artifact_type: GraphArtifactType | None = None
    current_artifact_payload: dict[str, Any] | None = None
    current_output_asset_id: str | None = None
    current_input_digest: str | None = None


@dataclass(frozen=True, slots=True)
class CompiledReference:
    edge_id: str
    source_node_id: str
    asset_id: str
    label: str
    mime_type: str | None = None
    order: int = 0


@dataclass(frozen=True, slots=True)
class PromptRuntimeInput:
    node_id: str
    image_type_key: str | None
    product_facts: tuple[dict[str, Any], ...]
    briefs: tuple[dict[str, Any], ...]
    prompt_config: dict[str, Any]
    reference_images: tuple[CompiledReference, ...]
    visual_system: dict[str, Any] | None
    visual_system_version_id: str | None
    incoming_edge_ids: tuple[str, ...]
    input_digest: str


@dataclass(frozen=True, slots=True)
class ImageRuntimeInput:
    node_id: str
    prompt_payload: dict[str, Any]
    prompt_artifact_id: str
    prompt_edge_id: str
    reference_images: tuple[CompiledReference, ...]
    visual_system: dict[str, Any] | None
    visual_system_version_id: str | None
    visual_overlay: dict[str, Any] | None
    generation_spec: dict[str, Any]
    variation_instruction: str | None
    incoming_edge_ids: tuple[str, ...]
    input_digest: str


@dataclass(frozen=True, slots=True)
class GraphRuntimeArtifacts:
    payloads: dict[str, dict[str, Any]] = field(default_factory=dict)
    artifact_ids: dict[str, str] = field(default_factory=dict)
    output_asset_ids: dict[str, str] = field(default_factory=dict)

    def with_prompt(self, node_id: str, *, artifact_id: str, payload: dict[str, Any]) -> GraphRuntimeArtifacts:
        payloads = dict(self.payloads)
        artifact_ids = dict(self.artifact_ids)
        payloads[node_id] = payload
        artifact_ids[node_id] = artifact_id
        return GraphRuntimeArtifacts(payloads, artifact_ids, dict(self.output_asset_ids))

    def with_image(self, node_id: str, *, artifact_id: str, asset_id: str) -> GraphRuntimeArtifacts:
        artifact_ids = dict(self.artifact_ids)
        output_asset_ids = dict(self.output_asset_ids)
        artifact_ids[node_id] = artifact_id
        output_asset_ids[node_id] = asset_id
        return GraphRuntimeArtifacts(dict(self.payloads), artifact_ids, output_asset_ids)


def incoming_edges(graph: AppliedGraph, node_id: str) -> tuple[AppliedGraphEdge, ...]:
    return tuple(
        sorted(
            (edge for edge in graph.edges if edge.target_node_id == node_id),
            key=lambda edge: (edge.role.value, edge.order, edge.id),
        )
    )


def strip_v3_prompt_payload(payload: dict[str, Any]) -> dict[str, Any]:
    return {key: value for key, value in payload.items() if key not in V3_PROMPT_STRIPPED_KEYS}


def compile_prompt_runtime(
    graph: AppliedGraph,
    node_id: str,
    sources: dict[str, GraphSourceRecord],
    artifacts: GraphRuntimeArtifacts | None = None,
) -> PromptRuntimeInput:
    node = graph.node(node_id)
    if node.node_type != GraphNodeType.PROMPT_GENERATION:
        raise BusinessValidationError("只有提示词生成节点可以编译为 PromptRuntimeInput")
    _reject_incomplete_required_edges(graph, node)
    edges = incoming_edges(graph, node_id)
    facts: list[dict[str, Any]] = []
    briefs: list[dict[str, Any]] = []
    references: list[CompiledReference] = []
    visual_payload = None
    visual_version_id = None
    for edge in edges:
        source = graph.node(edge.source_node_id)
        record = sources.get(source.id, GraphSourceRecord())
        if edge.role == GraphEdgeRole.FACTS:
            facts.extend(record.facts)
        elif edge.role == GraphEdgeRole.BRIEF:
            if record.brief:
                briefs.append(dict(record.brief))
        elif edge.role == GraphEdgeRole.REFERENCE:
            references.append(_compile_reference(graph, edge, sources, artifacts))
        elif edge.role == GraphEdgeRole.VISUAL_GUIDANCE:
            visual_payload, visual_version_id = _compile_visual(source, record)
    image_type_key = node.config.get("image_type_key")
    prompt_raw = node.config.get("prompt")
    prompt_config = dict(prompt_raw) if isinstance(prompt_raw, dict) else {}
    incoming_ids = tuple(edge.id for edge in edges)
    digest = _input_digest(
        {
            "node_id": node_id,
            "config": node.config,
            "facts": facts,
            "briefs": briefs,
            "references": [reference.asset_id for reference in references],
            "visual_system_version_id": visual_version_id,
            "incoming_edge_ids": incoming_ids,
        }
    )
    return PromptRuntimeInput(
        node_id=node_id,
        image_type_key=image_type_key if isinstance(image_type_key, str) else None,
        product_facts=tuple(facts),
        briefs=tuple(briefs),
        prompt_config=prompt_config,
        reference_images=tuple(references),
        visual_system=visual_payload,
        visual_system_version_id=visual_version_id,
        incoming_edge_ids=incoming_ids,
        input_digest=digest,
    )


def compile_image_runtime(
    graph: AppliedGraph,
    node_id: str,
    sources: dict[str, GraphSourceRecord],
    artifacts: GraphRuntimeArtifacts | None = None,
) -> ImageRuntimeInput:
    node = graph.node(node_id)
    if node.node_type != GraphNodeType.IMAGE_GENERATION:
        raise BusinessValidationError("只有图片生成节点可以编译为 ImageRuntimeInput")
    _reject_incomplete_required_edges(graph, node)
    edges = incoming_edges(graph, node_id)
    prompt_edge = next((edge for edge in edges if edge.role == GraphEdgeRole.PROMPT), None)
    if prompt_edge is None:
        raise BusinessValidationError("图片生成节点缺少 prompt 边，不能运行")
    prompt_source = graph.node(prompt_edge.source_node_id)
    prompt_payload, prompt_artifact_id = _prompt_artifact(prompt_source.id, sources, artifacts)
    references: list[CompiledReference] = []
    visual_payload = None
    visual_version_id = None
    for edge in edges:
        if edge.role == GraphEdgeRole.REFERENCE:
            references.append(_compile_reference(graph, edge, sources, artifacts))
        elif edge.role == GraphEdgeRole.VISUAL_GUIDANCE:
            source = graph.node(edge.source_node_id)
            visual_payload, visual_version_id = _compile_visual(source, sources.get(source.id, GraphSourceRecord()))
    generation_spec = node.config.get("generation_spec")
    try:
        spec_payload = GenerationSpec.model_validate(generation_spec).model_dump(mode="json")
    except Exception as exc:
        raise BusinessValidationError("图片生成节点缺少有效 GenerationSpec") from exc
    variation = node.config.get("variation_instruction")
    overlay = visual_overlay_from_config(node.config)
    incoming_ids = tuple(edge.id for edge in edges)
    digest = _input_digest(
        {
            "node_id": node_id,
            "config": node.config,
            "prompt_artifact_id": prompt_artifact_id,
            "references": [reference.asset_id for reference in references],
            "visual_system_version_id": visual_version_id,
            "incoming_edge_ids": incoming_ids,
        }
    )
    return ImageRuntimeInput(
        node_id=node_id,
        prompt_payload=prompt_payload,
        prompt_artifact_id=prompt_artifact_id,
        prompt_edge_id=prompt_edge.id,
        reference_images=tuple(references),
        visual_system=visual_payload,
        visual_system_version_id=visual_version_id,
        visual_overlay=overlay,
        generation_spec=spec_payload,
        variation_instruction=variation if isinstance(variation, str) else None,
        incoming_edge_ids=incoming_ids,
        input_digest=digest,
    )


def select_run_node_ids(
    graph: AppliedGraph,
    *,
    scope: GraphRunScope,
    target_node_id: str | None,
    artifacts: GraphRuntimeArtifacts | None = None,
) -> tuple[str, ...]:
    processing_ids = [node.id for node in graph.nodes if node.node_type in PROCESSING_NODE_TYPES]
    if scope == GraphRunScope.GRAPH:
        selected = [node_id for node_id in processing_ids if _has_required_edges(graph, node_id)]
    else:
        if target_node_id is None:
            raise BusinessValidationError("节点运行范围必须指定目标节点")
        target = graph.node(target_node_id)
        if target.node_type not in PROCESSING_NODE_TYPES:
            raise BusinessValidationError("只能运行提示词生成或图片生成节点")
        if not _has_required_edges(graph, target_node_id):
            raise BusinessValidationError("目标节点缺少运行所需的输入边")
        ancestors = _processing_ancestors(graph, target_node_id)
        if scope == GraphRunScope.TO_NODE:
            selected = [node_id for node_id in ancestors if _has_required_edges(graph, node_id)]
            selected.append(target_node_id)
        else:
            selected = [
                node_id
                for node_id in ancestors
                if _has_required_edges(graph, node_id) and not _has_output(node_id, artifacts)
            ]
            selected.append(target_node_id)
    ordered = _topo_order(graph, selected)
    if not ordered:
        raise BusinessValidationError("没有可运行的处理节点")
    return tuple(ordered)


def snapshot_graph(
    graph: AppliedGraph,
    sources: dict[str, GraphSourceRecord],
) -> dict[str, Any]:
    return {
        "schema_version": GRAPH_SNAPSHOT_SCHEMA_VERSION,
        "revision": graph.revision,
        "nodes": [
            {
                "id": node.id,
                "node_type": node.node_type.value,
                "title": node.title,
                "position_x": node.position_x,
                "position_y": node.position_y,
                "config": dict(node.config),
                "bound_asset_id": node.bound_asset_id,
                "group_id": node.group_id,
            }
            for node in graph.nodes
        ],
        "edges": [
            {
                "id": edge.id,
                "source_node_id": edge.source_node_id,
                "target_node_id": edge.target_node_id,
                "data_type": edge.data_type.value,
                "role": edge.role.value,
                "order": edge.order,
            }
            for edge in graph.edges
        ],
        "groups": [{"id": group.id, "title": group.title} for group in graph.groups],
        "sources": {node_id: _source_snapshot(record) for node_id, record in sources.items()},
    }


def applied_graph_from_snapshot(payload: dict[str, Any]) -> AppliedGraph:
    from productflow_backend.domain.enums import GraphEdgeDataType, GraphEdgeRole

    return AppliedGraph(
        revision=int(payload["revision"]),
        nodes=tuple(
            AppliedGraphNode(
                id=node["id"],
                node_type=GraphNodeType(node["node_type"]),
                title=node["title"],
                position_x=node["position_x"],
                position_y=node["position_y"],
                config=dict(node.get("config") or {}),
                bound_asset_id=node.get("bound_asset_id"),
                group_id=node.get("group_id"),
            )
            for node in payload["nodes"]
        ),
        edges=tuple(
            AppliedGraphEdge(
                id=edge["id"],
                source_node_id=edge["source_node_id"],
                target_node_id=edge["target_node_id"],
                data_type=GraphEdgeDataType(edge["data_type"]),
                role=GraphEdgeRole(edge["role"]),
                order=edge["order"],
            )
            for edge in payload["edges"]
        ),
        groups=tuple(AppliedGraphGroup(id=group["id"], title=group["title"]) for group in payload.get("groups") or ()),
    )


def sources_from_snapshot(payload: dict[str, Any]) -> dict[str, GraphSourceRecord]:
    sources: dict[str, GraphSourceRecord] = {}
    for node_id, record in (payload.get("sources") or {}).items():
        artifact_type = record.get("current_artifact_type")
        sources[node_id] = GraphSourceRecord(
            facts=tuple(record.get("facts") or ()),
            brief=record.get("brief"),
            visual_payload=record.get("visual_payload"),
            visual_system_version_id=record.get("visual_system_version_id"),
            bound_asset_id=record.get("bound_asset_id"),
            bound_asset_label=record.get("bound_asset_label"),
            bound_asset_mime_type=record.get("bound_asset_mime_type"),
            current_artifact_id=record.get("current_artifact_id"),
            current_artifact_type=GraphArtifactType(artifact_type) if artifact_type else None,
            current_artifact_payload=record.get("current_artifact_payload"),
            current_output_asset_id=record.get("current_output_asset_id"),
            current_input_digest=record.get("current_input_digest"),
        )
    return sources


def artifacts_from_sources(sources: dict[str, GraphSourceRecord]) -> GraphRuntimeArtifacts:
    payloads = {
        node_id: dict(record.current_artifact_payload)
        for node_id, record in sources.items()
        if record.current_artifact_payload is not None
    }
    artifact_ids = {
        node_id: record.current_artifact_id
        for node_id, record in sources.items()
        if record.current_artifact_id is not None
    }
    output_asset_ids = {
        node_id: record.current_output_asset_id
        for node_id, record in sources.items()
        if record.current_output_asset_id is not None
    }
    return GraphRuntimeArtifacts(payloads, artifact_ids, output_asset_ids)


def _compile_reference(
    graph: AppliedGraph,
    edge: AppliedGraphEdge,
    sources: dict[str, GraphSourceRecord],
    artifacts: GraphRuntimeArtifacts | None,
) -> CompiledReference:
    source = graph.node(edge.source_node_id)
    record = sources.get(source.id, GraphSourceRecord())
    asset_id = None
    label = source.title
    mime_type = record.bound_asset_mime_type
    if source.node_type == GraphNodeType.IMAGE_ASSET:
        asset_id = record.bound_asset_id or source.bound_asset_id
        label = record.bound_asset_label or source.title
    elif source.node_type == GraphNodeType.IMAGE_GENERATION:
        if artifacts is not None:
            asset_id = artifacts.output_asset_ids.get(source.id)
        if asset_id is None:
            asset_id = record.current_output_asset_id
        if asset_id is None:
            raise BusinessValidationError("上游图片生成节点尚无当前输出，不能作为参考")
    if not asset_id:
        raise BusinessValidationError("参考输入缺少已绑定的图片资产")
    return CompiledReference(
        edge_id=edge.id,
        source_node_id=source.id,
        asset_id=asset_id,
        label=label,
        mime_type=mime_type,
        order=edge.order,
    )


def _compile_visual(
    source: AppliedGraphNode,
    record: GraphSourceRecord,
) -> tuple[dict[str, Any] | None, str | None]:
    if source.node_type != GraphNodeType.VISUAL_SYSTEM:
        raise BusinessValidationError("visual_guidance 边的源必须是视觉规范节点")
    version_id = record.visual_system_version_id or source.config.get("visual_system_version_id")
    if not isinstance(version_id, str) or not version_id.strip() or record.visual_payload is None:
        return None, None
    payload = dict(record.visual_payload)
    overlay = visual_overlay_from_config(source.config)
    if overlay:
        payload = apply_visual_overlay(payload, overlay)
    return payload, version_id


def _prompt_artifact(
    node_id: str,
    sources: dict[str, GraphSourceRecord],
    artifacts: GraphRuntimeArtifacts | None,
) -> tuple[dict[str, Any], str]:
    if artifacts is not None and node_id in artifacts.payloads and node_id in artifacts.artifact_ids:
        return dict(artifacts.payloads[node_id]), artifacts.artifact_ids[node_id]
    record = sources.get(node_id, GraphSourceRecord())
    if record.current_artifact_payload is None or record.current_artifact_id is None:
        raise BusinessValidationError("上游提示词尚未生成")
    return dict(record.current_artifact_payload), record.current_artifact_id


def _reject_incomplete_required_edges(graph: AppliedGraph, node: AppliedGraphNode) -> None:
    status = node_config_status(
        GraphRuleNode(node.id, node.node_type, node.config, node.bound_asset_id),
        (
            GraphRuleEdge(edge.id, edge.source_node_id, edge.target_node_id, edge.data_type, edge.role, edge.order)
            for edge in incoming_edges(graph, node.id)
        ),
    )
    required = run_required_inputs(node.node_type)
    if required and status.value == "incomplete":
        raise BusinessValidationError("节点缺少运行所需的输入边")


def _has_required_edges(graph: AppliedGraph, node_id: str) -> bool:
    node = graph.node(node_id)
    try:
        _reject_incomplete_required_edges(graph, node)
    except BusinessValidationError:
        return False
    return True


def _has_output(node_id: str, artifacts: GraphRuntimeArtifacts | None) -> bool:
    if artifacts is None:
        return False
    return node_id in artifacts.artifact_ids or node_id in artifacts.payloads or node_id in artifacts.output_asset_ids


def _processing_ancestors(graph: AppliedGraph, node_id: str) -> list[str]:
    seen: set[str] = set()
    ordered: list[str] = []
    stack = [edge.source_node_id for edge in incoming_edges(graph, node_id)]
    while stack:
        current_id = stack.pop()
        if current_id in seen:
            continue
        seen.add(current_id)
        node = graph.node(current_id)
        if node.node_type in PROCESSING_NODE_TYPES:
            ordered.append(current_id)
        stack.extend(edge.source_node_id for edge in incoming_edges(graph, current_id))
    return ordered


def _topo_order(graph: AppliedGraph, selected: list[str]) -> list[str]:
    selected_set = set(selected)
    incoming_count = {node_id: 0 for node_id in selected_set}
    outgoing: dict[str, list[str]] = {node_id: [] for node_id in selected_set}
    for edge in graph.edges:
        if edge.source_node_id in selected_set and edge.target_node_id in selected_set:
            outgoing[edge.source_node_id].append(edge.target_node_id)
            incoming_count[edge.target_node_id] += 1
    ready = sorted(node_id for node_id, count in incoming_count.items() if count == 0)
    ordered: list[str] = []
    while ready:
        node_id = ready.pop(0)
        ordered.append(node_id)
        for target in sorted(outgoing[node_id]):
            incoming_count[target] -= 1
            if incoming_count[target] == 0:
                ready.append(target)
                ready.sort()
    return ordered


def _input_digest(payload: dict[str, Any]) -> str:
    encoded = json.dumps(payload, ensure_ascii=False, sort_keys=True, default=str)
    return hashlib.sha256(encoded.encode("utf-8")).hexdigest()


def _source_snapshot(record: GraphSourceRecord) -> dict[str, Any]:
    return {
        "facts": list(record.facts),
        "brief": record.brief,
        "visual_payload": record.visual_payload,
        "visual_system_version_id": record.visual_system_version_id,
        "bound_asset_id": record.bound_asset_id,
        "bound_asset_label": record.bound_asset_label,
        "bound_asset_mime_type": record.bound_asset_mime_type,
        "current_artifact_id": record.current_artifact_id,
        "current_artifact_type": record.current_artifact_type.value if record.current_artifact_type else None,
        "current_artifact_payload": record.current_artifact_payload,
        "current_output_asset_id": record.current_output_asset_id,
        "current_input_digest": record.current_input_digest,
    }
