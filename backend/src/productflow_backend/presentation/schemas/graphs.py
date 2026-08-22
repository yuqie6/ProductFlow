from __future__ import annotations

from decimal import Decimal
from typing import Any, Literal

from pydantic import BaseModel, ConfigDict, Field

from productflow_backend.application.product_workflow.graph_queries import (
    GraphEdgeSummary,
    GraphEdgeView,
    GraphGroupView,
    GraphNodeView,
    GraphProjection,
)
from productflow_backend.application.product_workflow.product_sources import ProductSourceSnapshot
from productflow_backend.application.workflow_drafts.contracts import ProductFactDraft
from productflow_backend.domain.enums import (
    GraphArtifactType,
    GraphConfigStatus,
    GraphEdgeDataType,
    GraphEdgeRole,
    GraphNodeType,
    GraphRunScope,
    ProductFactSourceType,
    ProductFactStatus,
    WorkflowNodeStatus,
    WorkflowRunStatus,
)
from productflow_backend.domain.graph_catalog import (
    GraphCatalogDocument,
    GraphConfigControl,
    GraphConfigValueKind,
    graph_catalog_json,
)
from productflow_backend.infrastructure.db.models import (
    Product,
    ProductImageAsset,
    WorkflowGraphNodeRun,
    WorkflowGraphRun,
)
from productflow_backend.presentation.schemas.products import (
    CanonicalProductDetailResponse,
    ProductImageAssetResponse,
    serialize_canonical_product_detail,
    serialize_product_image_asset,
)


class GraphEdgeSummaryResponse(BaseModel):
    model_config = ConfigDict(extra="forbid")

    id: str
    node_id: str
    data_type: GraphEdgeDataType
    role: GraphEdgeRole
    order: int


class GraphNodeResponse(BaseModel):
    model_config = ConfigDict(extra="forbid")

    id: str
    node_type: GraphNodeType
    title: str
    position_x: int
    position_y: int
    config: dict
    source_product: GraphSourceProductResponse | None = None
    product_fact_set: GraphFactSetResponse | None = None
    bound_asset_id: str | None = None
    group_id: str | None = None
    preview_asset_id: str | None = None
    config_status: GraphConfigStatus
    unused: bool
    current_artifact_id: str | None = None
    current_artifact_type: GraphArtifactType | None = None
    current_artifact_payload: dict | None = None
    incoming: list[GraphEdgeSummaryResponse]
    outgoing: list[GraphEdgeSummaryResponse]


class GraphEdgeResponse(BaseModel):
    model_config = ConfigDict(extra="forbid")

    id: str
    source_node_id: str
    target_node_id: str
    data_type: GraphEdgeDataType
    role: GraphEdgeRole
    order: int


class GraphGroupResponse(BaseModel):
    model_config = ConfigDict(extra="forbid")

    id: str
    title: str
    member_ids: list[str]


class GraphProjectionResponse(BaseModel):
    model_config = ConfigDict(extra="forbid")

    id: str
    product_id: str
    title: str
    schema_version: int
    revision: int
    source_draft_revision_id: str | None = None
    last_operation_group_id: str | None = None
    can_undo: bool = False
    can_redo: bool = False
    nodes: list[GraphNodeResponse]
    edges: list[GraphEdgeResponse]
    groups: list[GraphGroupResponse]


class GraphSourceProductResponse(BaseModel):
    model_config = ConfigDict(extra="forbid")

    id: str
    name: str
    category: str | None = None
    price: Decimal | None = None
    source_note: str | None = None


class GraphFactSetResponse(BaseModel):
    model_config = ConfigDict(extra="forbid")

    id: str
    product_id: str
    version: int
    facts: list[ProductFactDraft]


class GraphCatalogInputContractResponse(BaseModel):
    model_config = ConfigDict(extra="forbid")

    data_type: GraphEdgeDataType
    role: GraphEdgeRole
    max_count: int | None = None
    required_to_run: bool


class GraphCatalogVisibleWhenResponse(BaseModel):
    model_config = ConfigDict(extra="forbid")

    field: str
    op: Literal["in"]
    values: list[str]


class GraphCatalogConfigFieldResponse(BaseModel):
    model_config = ConfigDict(extra="forbid")

    key: str
    value_kind: GraphConfigValueKind
    control: GraphConfigControl
    required: bool = False
    label_key: str | None = None
    hint_key: str | None = None
    toggle_label_key: str | None = None
    choices: list[str] = Field(default_factory=list)
    min_value: int | None = None
    max_value: int | None = None
    max_length: int | None = None
    default: Any = None
    panel: str | None = None
    visible_when: GraphCatalogVisibleWhenResponse | None = None
    fields: list[GraphCatalogConfigFieldResponse] = Field(default_factory=list)


class GraphCatalogNodeResponse(BaseModel):
    model_config = ConfigDict(extra="forbid")

    node_type: GraphNodeType
    output_data_type: GraphEdgeDataType
    kind: Literal["source", "processing"]
    accepts: list[GraphCatalogInputContractResponse]
    config_fields: list[GraphCatalogConfigFieldResponse]


class GraphCatalogResponse(BaseModel):
    model_config = ConfigDict(extra="forbid")

    version: int
    nodes: list[GraphCatalogNodeResponse]


class DirectCreateProductResponse(BaseModel):
    model_config = ConfigDict(extra="forbid")

    product: CanonicalProductDetailResponse
    created_assets: list[ProductImageAssetResponse]
    graph: GraphProjectionResponse


class DirectCreateImageTypeRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")

    key: str = Field(min_length=1, max_length=80)
    quantity: int = Field(ge=1, le=6)
    aspect_ratio: str | None = Field(default=None, pattern=r"^[1-9][0-9]{0,2}:[1-9][0-9]{0,2}$")


def serialize_graph_catalog(document: GraphCatalogDocument) -> GraphCatalogResponse:
    return GraphCatalogResponse.model_validate(graph_catalog_json(document))


def serialize_graph_projection(projection: GraphProjection) -> GraphProjectionResponse:
    return GraphProjectionResponse(
        id=projection.id,
        product_id=projection.product_id,
        title=projection.title,
        schema_version=projection.schema_version,
        revision=projection.revision,
        source_draft_revision_id=projection.source_draft_revision_id,
        last_operation_group_id=projection.last_operation_group_id,
        can_undo=projection.can_undo,
        can_redo=projection.can_redo,
        nodes=[_serialize_node(node) for node in projection.nodes],
        edges=[_serialize_edge(edge) for edge in projection.edges],
        groups=[_serialize_group(group) for group in projection.groups],
    )


def serialize_direct_create(
    *,
    product: Product,
    created_assets: list[ProductImageAsset],
    projection: GraphProjection,
) -> DirectCreateProductResponse:
    return DirectCreateProductResponse(
        product=serialize_canonical_product_detail(product),
        created_assets=[serialize_product_image_asset(asset) for asset in created_assets],
        graph=serialize_graph_projection(projection),
    )


def _serialize_node(node: GraphNodeView) -> GraphNodeResponse:
    source_product, product_fact_set = _serialize_product_source(node.product_source)
    return GraphNodeResponse(
        id=node.id,
        node_type=node.node_type,
        title=node.title,
        position_x=node.position_x,
        position_y=node.position_y,
        config=dict(node.config),
        source_product=source_product,
        product_fact_set=product_fact_set,
        bound_asset_id=node.bound_asset_id,
        group_id=node.group_id,
        preview_asset_id=node.preview_asset_id,
        config_status=node.config_status,
        unused=node.unused,
        current_artifact_id=node.current_artifact_id,
        current_artifact_type=node.current_artifact_type,
        current_artifact_payload=dict(node.current_artifact_payload)
        if node.current_artifact_payload is not None
        else None,
        incoming=[_serialize_edge_summary(item) for item in node.incoming],
        outgoing=[_serialize_edge_summary(item) for item in node.outgoing],
    )


def _serialize_product_source(
    snapshot: ProductSourceSnapshot | None,
) -> tuple[GraphSourceProductResponse | None, GraphFactSetResponse | None]:
    if snapshot is None:
        return None, None
    product = snapshot.source_product
    fact_set = snapshot.fact_set_version
    return (
        (
            GraphSourceProductResponse(
                id=product.id,
                name=product.name,
                category=product.category,
                price=product.price,
                source_note=product.source_note,
            )
            if product is not None
            else None
        ),
        (
            GraphFactSetResponse(
                id=fact_set.id,
                product_id=fact_set.product_id,
                version=fact_set.version,
                facts=[_serialize_fact(item) for item in fact_set.facts],
            )
            if fact_set is not None
            else None
        ),
    )


def _serialize_fact(payload: dict[str, Any]) -> ProductFactDraft:
    normalized = {
        "key": payload.get("key") or "unknown",
        "value": payload.get("value"),
        "source_type": payload.get("source_type") or ProductFactSourceType.LEGACY_PRODUCT.value,
        "status": payload.get("status") or ProductFactStatus.CONFIRMED.value,
        "requires_confirmation": bool(payload.get("requires_confirmation", False)),
        "evidence_asset_ids": list(payload.get("evidence_asset_ids") or []),
        "conflicts": list(payload.get("conflicts") or []),
    }
    return ProductFactDraft.model_validate(normalized)


def _serialize_edge(edge: GraphEdgeView) -> GraphEdgeResponse:
    return GraphEdgeResponse(
        id=edge.id,
        source_node_id=edge.source_node_id,
        target_node_id=edge.target_node_id,
        data_type=edge.data_type,
        role=edge.role,
        order=edge.order,
    )


def _serialize_group(group: GraphGroupView) -> GraphGroupResponse:
    return GraphGroupResponse(id=group.id, title=group.title, member_ids=list(group.member_ids))


def _serialize_edge_summary(item: GraphEdgeSummary) -> GraphEdgeSummaryResponse:
    return GraphEdgeSummaryResponse(
        id=item.id,
        node_id=item.node_id,
        data_type=item.data_type,
        role=item.role,
        order=item.order,
    )


class PersistDraftGraphRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")

    expected_draft_version: int = Field(ge=1)


class DraftGraphPersistResponse(BaseModel):
    model_config = ConfigDict(extra="forbid")

    created: bool
    graph: GraphProjectionResponse


class GraphRunRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")

    scope: GraphRunScope = GraphRunScope.GRAPH
    node_id: str | None = None


class GraphNodeRunResponse(BaseModel):
    model_config = ConfigDict(extra="forbid")

    id: str
    node_id: str | None
    status: WorkflowNodeStatus
    sort_order: int
    compiled_context: dict | None = None
    output: dict | None = None
    failure_reason: str | None = None
    started_at: str
    finished_at: str | None = None


class GraphRunResponse(BaseModel):
    model_config = ConfigDict(extra="forbid")

    id: str
    graph_id: str
    status: WorkflowRunStatus
    scope: GraphRunScope
    requested_node_id: str | None = None
    graph_revision: int
    failure_reason: str | None = None
    is_retryable: bool
    node_runs: list[GraphNodeRunResponse]
    started_at: str
    finished_at: str | None = None


class GraphRunListResponse(BaseModel):
    model_config = ConfigDict(extra="forbid")

    items: list[GraphRunResponse]


def serialize_graph_run(run: WorkflowGraphRun) -> GraphRunResponse:
    node_runs = sorted(run.node_runs, key=lambda item: (item.sort_order, item.id))
    return GraphRunResponse(
        id=run.id,
        graph_id=run.graph_id,
        status=WorkflowRunStatus(run.status),
        scope=GraphRunScope(run.run_scope),
        requested_node_id=run.requested_node_id,
        graph_revision=run.graph_revision,
        failure_reason=run.failure_reason,
        is_retryable=run.is_retryable,
        node_runs=[_serialize_node_run(item) for item in node_runs],
        started_at=run.started_at.isoformat(),
        finished_at=run.finished_at.isoformat() if run.finished_at else None,
    )


def _serialize_node_run(node_run: WorkflowGraphNodeRun) -> GraphNodeRunResponse:
    return GraphNodeRunResponse(
        id=node_run.id,
        node_id=node_run.node_id,
        status=WorkflowNodeStatus(node_run.status),
        sort_order=node_run.sort_order,
        compiled_context=node_run.compiled_context_json,
        output=node_run.output_json,
        failure_reason=node_run.failure_reason,
        started_at=node_run.started_at.isoformat(),
        finished_at=node_run.finished_at.isoformat() if node_run.finished_at else None,
    )
