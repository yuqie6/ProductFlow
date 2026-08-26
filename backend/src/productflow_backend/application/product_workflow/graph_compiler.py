"""schema-v3 运行时编译：只收集目标节点 incoming 边上的 facts/references/briefs/visual，不扫全图。"""

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
from productflow_backend.application.product_workflow.product_sources import (
    ProductSourceSnapshot,
    merge_runtime_facts,
    product_source_snapshot_from_dict,
    product_source_snapshot_to_dict,
)
from productflow_backend.domain.enums import (
    GraphArtifactType,
    GraphEdgeRole,
    GraphNodeType,
    GraphRunScope,
)
from productflow_backend.domain.errors import BusinessValidationError
from productflow_backend.domain.graph_catalog import (
    PROCESSING_NODE_TYPES,
    catalog_visual_overlay,
    normalize_node_config,
)
from productflow_backend.domain.graph_rules import (
    GraphRuleEdge,
    GraphRuleNode,
    missing_required_inputs,
    node_config_error,
)
from productflow_backend.domain.image_type_catalog import (
    agent_product_image_type_option,
    image_type_family,
    image_type_generation_job,
)

# 运行快照合同版本，不是 workflow_graphs.schema_version。
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
V3_PROMPT_EMPTY_KEYS = frozenset({"schema_version", "visual_variant_key"})


@dataclass(frozen=True, slots=True)
class GraphSourceRecord:
    facts: tuple[dict[str, Any], ...] = ()
    product_source: ProductSourceSnapshot | None = None
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
    role: str = "reference"


@dataclass(frozen=True, slots=True)
class ContextRuntimeInput:
    node_id: str
    node_type: GraphNodeType
    product_facts: tuple[dict[str, Any], ...]
    reference_images: tuple[CompiledReference, ...]
    current_config: dict[str, Any]
    incoming_edge_ids: tuple[str, ...]
    input_digest: str
    text_policy: str = "none"
    text_language: str | None = None
    image_types: tuple[dict[str, Any], ...] = ()


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
    visual_overlay: dict[str, Any] | None = None
    text_policy: str = "none"
    text_language: str | None = None


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
    image_type_key: str | None = None


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


def _downstream_text_intent(graph: AppliedGraph, prompt_node_id: str) -> tuple[str, str | None]:
    intents: list[tuple[str, str | None]] = []
    for edge in graph.edges:
        if edge.source_node_id != prompt_node_id or edge.role != GraphEdgeRole.PROMPT:
            continue
        target = graph.node(edge.target_node_id)
        if target.node_type != GraphNodeType.IMAGE_GENERATION:
            continue
        spec = target.config.get("generation_spec")
        if not isinstance(spec, dict):
            intents.append(("none", None))
            continue
        policy = spec.get("text_policy")
        if policy not in {"none", "allow", "required"}:
            policy = "none"
        language = spec.get("text_language")
        if not isinstance(language, str) or not language.strip() or policy == "none":
            language = None
        else:
            language = language.strip()
        intents.append((policy, language))
    if not intents:
        return "none", None
    policies = {item[0] for item in intents}
    if "required" in policies:
        language = next((lang for policy, lang in intents if policy == "required" and lang), None)
        return "required", language
    if policies == {"none"}:
        return "none", None
    language = next((lang for policy, lang in intents if policy != "none" and lang), None)
    return "allow", language


def _graph_text_intent(graph: AppliedGraph) -> tuple[str, str | None]:
    intents: list[tuple[str, str | None]] = []
    for node in graph.nodes:
        if node.node_type != GraphNodeType.IMAGE_GENERATION:
            continue
        spec = node.config.get("generation_spec")
        if not isinstance(spec, dict):
            intents.append(("none", None))
            continue
        policy = spec.get("text_policy")
        if policy not in {"none", "allow", "required"}:
            policy = "none"
        language = spec.get("text_language")
        if not isinstance(language, str) or not language.strip() or policy == "none":
            language = None
        else:
            language = language.strip()
        intents.append((policy, language))
    if not intents:
        return "none", None
    policies = {item[0] for item in intents}
    if "required" in policies:
        language = next((lang for policy, lang in intents if policy == "required" and lang), None)
        return "required", language
    if policies == {"none"}:
        return "none", None
    language = next((lang for policy, lang in intents if policy != "none" and lang), None)
    return "allow", language


def strip_v3_prompt_payload(payload: dict[str, Any]) -> dict[str, Any]:
    return {key: value for key, value in payload.items() if key not in V3_PROMPT_STRIPPED_KEYS}


def compile_prompt_runtime(
    graph: AppliedGraph,
    node_id: str,
    sources: dict[str, GraphSourceRecord],
    artifacts: GraphRuntimeArtifacts | None = None,
) -> PromptRuntimeInput:
    """只编译 prompt 节点 incoming 边上的 facts/brief/reference/visual；断开边即失去该输入。"""

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
    visual_overlay = None
    for edge in edges:
        source = graph.node(edge.source_node_id)
        record = sources.get(source.id, GraphSourceRecord())
        if edge.role == GraphEdgeRole.FACTS:
            facts.extend(merge_runtime_facts(record.facts, record.product_source))
        elif edge.role == GraphEdgeRole.BRIEF:
            if record.brief:
                briefs.append(dict(record.brief))
        elif edge.role == GraphEdgeRole.REFERENCE:
            references.append(_compile_reference(graph, edge, sources, artifacts))
        elif edge.role == GraphEdgeRole.VISUAL_GUIDANCE:
            visual_payload, visual_version_id, visual_overlay = _compile_visual(source, record)
    image_type_key = node.config.get("image_type_key")
    prompt_raw = node.config.get("prompt")
    prompt_config = dict(prompt_raw) if isinstance(prompt_raw, dict) else {}
    text_policy, text_language = _downstream_text_intent(graph, node_id)
    incoming_ids = tuple(edge.id for edge in edges)
    digest = _input_digest(
        {
            "node_id": node_id,
            "config": _request_config_for_digest(node.node_type, node.config),
            "facts": facts,
            "briefs": briefs,
            "references": [reference.asset_id for reference in references],
            "visual_system": visual_payload,
            "visual_system_version_id": visual_version_id,
            "visual_overlay": visual_overlay,
            "text_policy": text_policy,
            "text_language": text_language,
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
        visual_overlay=visual_overlay,
        incoming_edge_ids=incoming_ids,
        input_digest=digest,
        text_policy=text_policy,
        text_language=text_language,
    )


def _planned_image_types(graph: AppliedGraph) -> tuple[dict[str, Any], ...]:
    items: list[dict[str, Any]] = []
    seen: set[str] = set()
    for node in graph.nodes:
        if node.node_type != GraphNodeType.IMAGE_GENERATION:
            continue
        key = node.config.get("image_type_key")
        if not isinstance(key, str) or not key or key in seen:
            continue
        seen.add(key)
        option = agent_product_image_type_option(key)
        items.append(
            {
                "key": key,
                "title": option.title if option else key,
                "description": option.description if option else "",
                "family": image_type_family(key),
                "generation_job": image_type_generation_job(key),
            }
        )
    return tuple(items)


def compile_context_runtime(
    graph: AppliedGraph,
    node_id: str,
    sources: dict[str, GraphSourceRecord],
    artifacts: GraphRuntimeArtifacts | None = None,
) -> ContextRuntimeInput:
    """只编译 brief / visual_system 节点 incoming 边上的 facts 与 reference。"""

    node = graph.node(node_id)
    if node.node_type not in {GraphNodeType.CREATIVE_BRIEF, GraphNodeType.VISUAL_SYSTEM}:
        raise BusinessValidationError("只有视觉规范或创作要求节点可以编译为 ContextRuntimeInput")
    _reject_incomplete_required_edges(graph, node)
    edges = incoming_edges(graph, node_id)
    facts: list[dict[str, Any]] = []
    references: list[CompiledReference] = []
    for edge in edges:
        record = sources.get(edge.source_node_id, GraphSourceRecord())
        if edge.role == GraphEdgeRole.FACTS:
            facts.extend(merge_runtime_facts(record.facts, record.product_source))
        elif edge.role == GraphEdgeRole.REFERENCE:
            references.append(_compile_reference(graph, edge, sources, artifacts))
    text_policy, text_language = _graph_text_intent(graph)
    incoming_ids = tuple(edge.id for edge in edges)
    current_config = dict(node.config)
    if node.node_type == GraphNodeType.VISUAL_SYSTEM:
        overlay_raw = current_config.get("visual_overlay")
        overlay = catalog_visual_overlay(overlay_raw if isinstance(overlay_raw, dict) else None)
        if overlay:
            current_config = {**current_config, "visual_overlay": overlay}
    image_types = _planned_image_types(graph)
    digest = _input_digest(
        {
            "node_id": node_id,
            "node_type": node.node_type.value,
            "config": _request_config_for_digest(node.node_type, node.config),
            "facts": facts,
            "references": [reference.asset_id for reference in references],
            "text_policy": text_policy,
            "text_language": text_language,
            "incoming_edge_ids": incoming_ids,
            "image_types": list(image_types),
        }
    )
    return ContextRuntimeInput(
        node_id=node_id,
        node_type=node.node_type,
        product_facts=tuple(facts),
        reference_images=tuple(references),
        current_config=current_config,
        incoming_edge_ids=incoming_ids,
        input_digest=digest,
        text_policy=text_policy,
        text_language=text_language,
        image_types=image_types,
    )


def compile_image_runtime(
    graph: AppliedGraph,
    node_id: str,
    sources: dict[str, GraphSourceRecord],
    artifacts: GraphRuntimeArtifacts | None = None,
) -> ImageRuntimeInput:
    """只编译 image 节点 incoming 边上的 prompt/reference/visual；不扫图上其他节点。"""

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
            visual_payload, visual_version_id, _ = _compile_visual(
                source,
                sources.get(source.id, GraphSourceRecord()),
            )
    normalized_config = normalize_node_config(node.node_type, node.config)
    spec_payload = normalized_config["generation_spec"]
    variation = normalized_config.get("variation_instruction")
    overlay = visual_overlay_from_config(normalized_config)
    incoming_ids = tuple(edge.id for edge in edges)
    image_type_key = node.config.get("image_type_key")
    digest = _input_digest(
        {
            "node_id": node_id,
            "config": normalized_config,
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
        image_type_key=image_type_key if isinstance(image_type_key, str) else None,
    )


def select_run_node_ids(
    graph: AppliedGraph,
    *,
    scope: GraphRunScope,
    target_node_id: str | None,
    artifacts: GraphRuntimeArtifacts | None = None,
) -> tuple[str, ...]:
    """GRAPH 入队可运行处理节点；NODE 只跑目标节点，不顺带下游 image 节点。"""

    processing_ids = [node.id for node in graph.nodes if node.node_type in PROCESSING_NODE_TYPES]
    # 选节点不看已有 artifact；GRAPH 范围的 skip 在执行层按 digest 决定。
    del artifacts
    if scope == GraphRunScope.GRAPH:
        selected = [node_id for node_id in processing_ids if _has_required_edges(graph, node_id)]
    else:
        if target_node_id is None:
            raise BusinessValidationError("节点运行范围必须指定目标节点")
        target = graph.node(target_node_id)
        if target.node_type not in PROCESSING_NODE_TYPES:
            raise BusinessValidationError("只能运行视觉规范、创作要求、提示词生成或图片生成节点")
        try:
            _reject_incomplete_required_edges(graph, target)
        except BusinessValidationError as exc:
            raise BusinessValidationError(f"目标节点不可运行: {exc}") from exc
        ancestors = _processing_ancestors(graph, target_node_id)
        if scope == GraphRunScope.TO_NODE:
            selected = [node_id for node_id in ancestors if _has_required_edges(graph, node_id)]
            selected.append(target_node_id)
        else:
            # NODE 范围只入队目标节点：跑内容节点不会顺带跑下游 image_generation。
            selected = [target_node_id]
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


def graph_snapshot_node_title(snapshot: dict[str, Any], node_id: str | None) -> str | None:
    if not node_id:
        return None
    for node in snapshot.get("nodes") or ():
        if not isinstance(node, dict) or node.get("id") != node_id:
            continue
        title = node.get("title")
        if isinstance(title, str) and title.strip():
            return title
        return None
    return None


def graph_snapshot_input_trace(snapshot: dict[str, Any], node_id: str | None) -> list[dict[str, Any]]:
    if not node_id:
        return []
    titles = {
        node.get("id"): node.get("title")
        for node in snapshot.get("nodes") or ()
        if isinstance(node, dict)
    }
    entries: list[dict[str, Any]] = []
    for edge in snapshot.get("edges") or ():
        if not isinstance(edge, dict) or edge.get("target_node_id") != node_id:
            continue
        source_id = edge.get("source_node_id")
        title = titles.get(source_id)
        order = edge.get("order") or 0
        entries.append(
            {
                "edge_id": str(edge.get("id") or ""),
                "source_node_id": str(source_id) if source_id else None,
                "source_title": title if isinstance(title, str) and title.strip() else None,
                "role": str(edge.get("role") or ""),
                "order": int(order) if isinstance(order, int) else 0,
            }
        )
    entries.sort(key=lambda item: (item["order"], item["edge_id"]))
    return entries


def graph_runtime_input_trace(
    graph: AppliedGraph,
    node_id: str,
    sources: dict[str, GraphSourceRecord],
    artifacts: GraphRuntimeArtifacts | None = None,
) -> list[dict[str, Any]]:
    """投影一次节点执行实际消费的不可变身份。"""

    entries: list[dict[str, Any]] = []
    for edge in incoming_edges(graph, node_id):
        source = graph.node(edge.source_node_id)
        record = sources.get(source.id, GraphSourceRecord())
        artifact_id = (
            artifacts.artifact_ids.get(source.id)
            if artifacts is not None
            else None
        ) or record.current_artifact_id
        artifact_type = record.current_artifact_type.value if record.current_artifact_type else None
        if artifact_id is not None and artifact_type is None:
            artifact_type = {
                GraphNodeType.CREATIVE_BRIEF: GraphArtifactType.CREATIVE_BRIEF.value,
                GraphNodeType.VISUAL_SYSTEM: GraphArtifactType.VISUAL_SYSTEM.value,
                GraphNodeType.PROMPT_GENERATION: GraphArtifactType.PROMPT.value,
                GraphNodeType.IMAGE_GENERATION: GraphArtifactType.IMAGE.value,
            }.get(source.node_type)

        asset_id: str | None = None
        if source.node_type == GraphNodeType.IMAGE_ASSET:
            asset_id = record.bound_asset_id or source.bound_asset_id
        elif source.node_type == GraphNodeType.IMAGE_GENERATION:
            asset_id = (
                artifacts.output_asset_ids.get(source.id)
                if artifacts is not None
                else None
            ) or record.current_output_asset_id

        version_id: str | None = None
        if source.node_type == GraphNodeType.PRODUCT_SOURCE and record.product_source is not None:
            version_id = record.product_source.fact_set_version_id
        elif source.node_type == GraphNodeType.VISUAL_SYSTEM:
            version_id = record.visual_system_version_id

        entries.append(
            {
                "edge_id": edge.id,
                "source_node_id": source.id,
                "source_title": source.title,
                "role": edge.role.value,
                "order": edge.order,
                "artifact_id": artifact_id,
                "artifact_type": artifact_type,
                "asset_id": asset_id,
                "version_id": version_id,
            }
        )
    entries.sort(key=lambda item: (item["order"], item["edge_id"]))
    return entries


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
            product_source=product_source_snapshot_from_dict(record.get("product_source")),
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
    """reference 边消费上游当前资产 id；image_asset 的 bound_asset_id 不是这条边本身。"""

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
        role=_asset_role(source),
    )


def _asset_role(source: AppliedGraphNode) -> str:
    raw = source.config.get("role")
    if isinstance(raw, str) and raw.strip():
        return raw.strip()
    return "reference"


def _compile_visual(
    source: AppliedGraphNode,
    record: GraphSourceRecord,
) -> tuple[dict[str, Any] | None, str | None, dict[str, Any] | None]:
    if source.node_type != GraphNodeType.VISUAL_SYSTEM:
        raise BusinessValidationError("visual_guidance 边的源必须是视觉规范节点")
    raw_version = record.visual_system_version_id or source.config.get("visual_system_version_id")
    version_id = raw_version.strip() if isinstance(raw_version, str) and raw_version.strip() else None
    overlay = visual_overlay_from_config(source.config)
    if record.visual_payload is None:
        return (dict(overlay), version_id, dict(overlay)) if overlay else (None, None, None)
    payload = dict(record.visual_payload)
    if overlay:
        payload = apply_visual_overlay(payload, overlay)
    return payload, version_id, dict(overlay) if overlay else None


def _prompt_artifact(
    node_id: str,
    sources: dict[str, GraphSourceRecord],
    artifacts: GraphRuntimeArtifacts | None,
) -> tuple[dict[str, Any], str]:
    if artifacts is not None and node_id in artifacts.payloads and node_id in artifacts.artifact_ids:
        payload = strip_v3_prompt_payload(dict(artifacts.payloads[node_id]))
        if _has_prompt_payload(payload):
            return dict(artifacts.payloads[node_id]), artifacts.artifact_ids[node_id]
        raise BusinessValidationError("上游提示词尚未生成")
    record = sources.get(node_id, GraphSourceRecord())
    if record.current_artifact_payload is None or record.current_artifact_id is None:
        raise BusinessValidationError("上游提示词尚未生成")
    if not _has_prompt_payload(strip_v3_prompt_payload(dict(record.current_artifact_payload))):
        raise BusinessValidationError("上游提示词尚未生成")
    return dict(record.current_artifact_payload), record.current_artifact_id


def _has_prompt_payload(payload: dict[str, Any]) -> bool:
    return any(
        key not in V3_PROMPT_EMPTY_KEYS and value not in (None, "", [], {})
        for key, value in payload.items()
    )


def _reject_incomplete_required_edges(graph: AppliedGraph, node: AppliedGraphNode) -> None:
    rule_node = GraphRuleNode(node.id, node.node_type, node.config, node.bound_asset_id)
    config_error = node_config_error(rule_node)
    if config_error is not None:
        if config_error.startswith(("图片生成节点", "generation_spec", "delivery_spec")):
            raise BusinessValidationError(config_error)
        raise BusinessValidationError(f"节点配置无效: {config_error}")
    incoming = tuple(
        GraphRuleEdge(edge.id, edge.source_node_id, edge.target_node_id, edge.data_type, edge.role, edge.order)
        for edge in incoming_edges(graph, node.id)
    )
    if missing_required_inputs(rule_node, incoming):
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


_SELF_OUTPUT_CONFIG_KEYS: dict[GraphNodeType, frozenset[str]] = {
    GraphNodeType.VISUAL_SYSTEM: frozenset({"visual_overlay"}),
    GraphNodeType.CREATIVE_BRIEF: frozenset({"goal", "design_goals", "required_copy", "prohibitions"}),
    GraphNodeType.PROMPT_GENERATION: frozenset({"prompt"}),
}


def _request_config_for_digest(node_type: GraphNodeType, config: dict[str, Any]) -> dict[str, Any]:
    """digest 排除本节点会回写的输出字段，避免刚生成就把自己标成 STALE。"""

    excluded = _SELF_OUTPUT_CONFIG_KEYS.get(node_type, frozenset())
    return {key: value for key, value in config.items() if key not in excluded}


def _input_digest(payload: dict[str, Any]) -> str:
    encoded = json.dumps(payload, ensure_ascii=False, sort_keys=True, default=str)
    return hashlib.sha256(encoded.encode("utf-8")).hexdigest()


def _source_snapshot(record: GraphSourceRecord) -> dict[str, Any]:
    return {
        "facts": list(record.facts),
        "product_source": product_source_snapshot_to_dict(record.product_source),
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
