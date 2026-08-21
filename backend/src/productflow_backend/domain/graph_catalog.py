"""schema-v3 Node Catalog：输出类型、可接受输入、基数和运行必需性。"""

from __future__ import annotations

from dataclasses import dataclass
from typing import Literal

from productflow_backend.domain.enums import GraphEdgeDataType, GraphEdgeRole, GraphNodeType
from productflow_backend.domain.errors import BusinessValidationError

GraphNodeKind = Literal["source", "processing"]

GRAPH_CATALOG_VERSION = 1


@dataclass(frozen=True, slots=True)
class GraphInputContract:
    data_type: GraphEdgeDataType
    role: GraphEdgeRole
    max_count: int | None
    required_to_run: bool


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


@dataclass(frozen=True, slots=True)
class GraphCatalogDocument:
    version: int
    nodes: tuple[GraphCatalogNodeDocument, ...]


def graph_node_kind(node_type: GraphNodeType) -> GraphNodeKind:
    return "processing" if node_type in PROCESSING_NODE_TYPES else "source"


def graph_catalog_document() -> GraphCatalogDocument:
    return GraphCatalogDocument(
        version=GRAPH_CATALOG_VERSION,
        nodes=tuple(
            GraphCatalogNodeDocument(
                node_type=node_type,
                output_data_type=graph_node_output_type(node_type),
                kind=graph_node_kind(node_type),
                accepts=accepted_inputs(node_type),
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
