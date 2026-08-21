"""schema-v3 Node Catalog：输出类型、可接受输入、基数、运行必需性和可编辑配置字段。"""

from __future__ import annotations

from dataclasses import dataclass
from typing import Literal

from productflow_backend.domain.enums import GraphEdgeDataType, GraphEdgeRole, GraphNodeType
from productflow_backend.domain.errors import BusinessValidationError

GraphNodeKind = Literal["source", "processing"]
GraphConfigValueKind = Literal["string", "string_or_null", "string_list", "object", "object_or_null"]

GRAPH_CATALOG_VERSION = 2

FORBIDDEN_GRAPH_CONFIG_KEYS = frozenset(
    {
        "prompt_plan_key",
        "image_plan_key",
        "prompt_plan_keys",
        "image_plan_keys",
    }
)


@dataclass(frozen=True, slots=True)
class GraphInputContract:
    data_type: GraphEdgeDataType
    role: GraphEdgeRole
    max_count: int | None
    required_to_run: bool


@dataclass(frozen=True, slots=True)
class GraphConfigFieldDocument:
    key: str
    value_kind: GraphConfigValueKind
    required: bool = False


_OUTPUT_TYPE: dict[GraphNodeType, GraphEdgeDataType] = {
    GraphNodeType.PRODUCT_SOURCE: GraphEdgeDataType.PRODUCT_FACTS,
    GraphNodeType.IMAGE_ASSET: GraphEdgeDataType.IMAGE_ASSET,
    GraphNodeType.CREATIVE_BRIEF: GraphEdgeDataType.CREATIVE_BRIEF,
    GraphNodeType.VISUAL_SYSTEM: GraphEdgeDataType.VISUAL_SYSTEM,
    GraphNodeType.PROMPT_GENERATION: GraphEdgeDataType.PROMPT,
    GraphNodeType.IMAGE_GENERATION: GraphEdgeDataType.IMAGE_ASSET,
}

_ACCEPTANCE: dict[tuple[GraphEdgeDataType, GraphNodeType], GraphInputContract] = {
    (GraphEdgeDataType.PRODUCT_FACTS, GraphNodeType.PROMPT_GENERATION): GraphInputContract(
        GraphEdgeDataType.PRODUCT_FACTS, GraphEdgeRole.FACTS, None, False
    ),
    (GraphEdgeDataType.IMAGE_ASSET, GraphNodeType.PROMPT_GENERATION): GraphInputContract(
        GraphEdgeDataType.IMAGE_ASSET, GraphEdgeRole.REFERENCE, None, False
    ),
    (GraphEdgeDataType.CREATIVE_BRIEF, GraphNodeType.PROMPT_GENERATION): GraphInputContract(
        GraphEdgeDataType.CREATIVE_BRIEF, GraphEdgeRole.BRIEF, None, False
    ),
    (GraphEdgeDataType.VISUAL_SYSTEM, GraphNodeType.PROMPT_GENERATION): GraphInputContract(
        GraphEdgeDataType.VISUAL_SYSTEM, GraphEdgeRole.VISUAL_GUIDANCE, 1, False
    ),
    (GraphEdgeDataType.IMAGE_ASSET, GraphNodeType.IMAGE_GENERATION): GraphInputContract(
        GraphEdgeDataType.IMAGE_ASSET, GraphEdgeRole.REFERENCE, None, False
    ),
    (GraphEdgeDataType.VISUAL_SYSTEM, GraphNodeType.IMAGE_GENERATION): GraphInputContract(
        GraphEdgeDataType.VISUAL_SYSTEM, GraphEdgeRole.VISUAL_GUIDANCE, 1, False
    ),
    (GraphEdgeDataType.PROMPT, GraphNodeType.IMAGE_GENERATION): GraphInputContract(
        GraphEdgeDataType.PROMPT, GraphEdgeRole.PROMPT, 1, True
    ),
}

_CONFIG_FIELDS: dict[GraphNodeType, tuple[GraphConfigFieldDocument, ...]] = {
    GraphNodeType.PRODUCT_SOURCE: (
        GraphConfigFieldDocument("source_product_id", "string_or_null"),
        GraphConfigFieldDocument("fact_set_version_id", "string_or_null"),
    ),
    GraphNodeType.IMAGE_ASSET: (
        GraphConfigFieldDocument("role", "string_or_null"),
        GraphConfigFieldDocument("label", "string_or_null"),
    ),
    GraphNodeType.CREATIVE_BRIEF: (
        GraphConfigFieldDocument("goal", "string"),
        GraphConfigFieldDocument("title", "string"),
        GraphConfigFieldDocument("design_goals", "string_list"),
        GraphConfigFieldDocument("required_copy", "string_list"),
        GraphConfigFieldDocument("prohibitions", "string_list"),
    ),
    GraphNodeType.VISUAL_SYSTEM: (
        GraphConfigFieldDocument("visual_system_version_id", "string_or_null"),
        GraphConfigFieldDocument("visual_overlay", "object_or_null"),
        GraphConfigFieldDocument("visual_overrides", "object"),
    ),
    GraphNodeType.PROMPT_GENERATION: (
        GraphConfigFieldDocument("image_type_key", "string"),
        GraphConfigFieldDocument("prompt", "object"),
    ),
    GraphNodeType.IMAGE_GENERATION: (
        GraphConfigFieldDocument("image_type_key", "string"),
        GraphConfigFieldDocument("generation_spec", "object"),
        GraphConfigFieldDocument("delivery_spec", "object_or_null"),
        GraphConfigFieldDocument("variation_instruction", "string_or_null"),
        GraphConfigFieldDocument("visual_overlay", "object_or_null"),
        GraphConfigFieldDocument("visual_overrides", "object"),
    ),
}

PROCESSING_NODE_TYPES = frozenset({GraphNodeType.PROMPT_GENERATION, GraphNodeType.IMAGE_GENERATION})
SOURCE_NODE_TYPES = frozenset(
    {
        GraphNodeType.PRODUCT_SOURCE,
        GraphNodeType.IMAGE_ASSET,
        GraphNodeType.CREATIVE_BRIEF,
        GraphNodeType.VISUAL_SYSTEM,
    }
)


@dataclass(frozen=True, slots=True)
class GraphCatalogNodeDocument:
    node_type: GraphNodeType
    output_data_type: GraphEdgeDataType
    kind: GraphNodeKind
    accepts: tuple[GraphInputContract, ...]
    config_fields: tuple[GraphConfigFieldDocument, ...]


@dataclass(frozen=True, slots=True)
class GraphCatalogDocument:
    version: int
    nodes: tuple[GraphCatalogNodeDocument, ...]


def graph_node_kind(node_type: GraphNodeType) -> GraphNodeKind:
    return "processing" if node_type in PROCESSING_NODE_TYPES else "source"


def node_config_fields(node_type: GraphNodeType) -> tuple[GraphConfigFieldDocument, ...]:
    return _CONFIG_FIELDS[node_type]


def allowed_config_keys(node_type: GraphNodeType) -> frozenset[str]:
    return frozenset(field.key for field in node_config_fields(node_type))


def validate_node_config(node_type: GraphNodeType, config: dict[str, object] | None) -> None:
    payload = config or {}
    illegal = FORBIDDEN_GRAPH_CONFIG_KEYS.intersection(payload)
    if illegal:
        raise BusinessValidationError(f"节点配置不能包含拓扑字段: {', '.join(sorted(illegal))}")
    unknown = sorted(key for key in payload if key not in allowed_config_keys(node_type))
    if unknown:
        raise BusinessValidationError(f"节点配置包含未登记字段: {', '.join(unknown)}")


def graph_catalog_document() -> GraphCatalogDocument:
    return GraphCatalogDocument(
        version=GRAPH_CATALOG_VERSION,
        nodes=tuple(
            GraphCatalogNodeDocument(
                node_type=node_type,
                output_data_type=graph_node_output_type(node_type),
                kind=graph_node_kind(node_type),
                accepts=accepted_inputs(node_type),
                config_fields=node_config_fields(node_type),
            )
            for node_type in GraphNodeType
        ),
    )


def graph_node_output_type(node_type: GraphNodeType) -> GraphEdgeDataType:
    return _OUTPUT_TYPE[node_type]


def graph_input_contract(
    source_type: GraphNodeType,
    target_type: GraphNodeType,
) -> GraphInputContract | None:
    return _ACCEPTANCE.get((graph_node_output_type(source_type), target_type))


def require_graph_connection(
    source_type: GraphNodeType,
    target_type: GraphNodeType,
) -> GraphInputContract:
    contract = graph_input_contract(source_type, target_type)
    if contract is None:
        raise BusinessValidationError("节点类型不兼容，不能创建连线")
    return contract


def accepted_inputs(node_type: GraphNodeType) -> tuple[GraphInputContract, ...]:
    return tuple(contract for (data_type, target), contract in _ACCEPTANCE.items() if target == node_type)


def run_required_inputs(node_type: GraphNodeType) -> tuple[GraphInputContract, ...]:
    return tuple(contract for contract in accepted_inputs(node_type) if contract.required_to_run)
