from __future__ import annotations

from pydantic import BaseModel, ConfigDict, Field

from productflow_backend.application.product_workflow.graph_queries import (
    GraphEdgeSummary,
    GraphEdgeView,
    GraphGroupView,
    GraphNodeView,
    GraphProjection,
)
from productflow_backend.domain.enums import (
    GraphConfigStatus,
    GraphEdgeDataType,
    GraphEdgeRole,
    GraphNodeType,
    GraphRunScope,
    WorkflowNodeStatus,
    WorkflowRunStatus,
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
    bound_asset_id: str | None = None
    group_id: str | None = None
    preview_asset_id: str | None = None
    config_status: GraphConfigStatus
    unused: bool
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
    nodes: list[GraphNodeResponse]
    edges: list[GraphEdgeResponse]
    groups: list[GraphGroupResponse]


class DirectCreateProductResponse(BaseModel):
    model_config = ConfigDict(extra="forbid")

    product: CanonicalProductDetailResponse
    created_assets: list[ProductImageAssetResponse]
    graph: GraphProjectionResponse


class DirectCreateImageTypeRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")

    key: str = Field(min_length=1, max_length=80)
    quantity: int = Field(ge=1, le=6)


def serialize_graph_projection(projection: GraphProjection) -> GraphProjectionResponse:
    return GraphProjectionResponse(
        id=projection.id,
        product_id=projection.product_id,
        title=projection.title,
        schema_version=projection.schema_version,
        revision=projection.revision,
        source_draft_revision_id=projection.source_draft_revision_id,
        last_operation_group_id=projection.last_operation_group_id,
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
    return GraphNodeResponse(
        id=node.id,
        node_type=node.node_type,
        title=node.title,
        position_x=node.position_x,
        position_y=node.position_y,
        config=dict(node.config),
        bound_asset_id=node.bound_asset_id,
        group_id=node.group_id,
        preview_asset_id=node.preview_asset_id,
        config_status=node.config_status,
        unused=node.unused,
        incoming=[_serialize_edge_summary(item) for item in node.incoming],
        outgoing=[_serialize_edge_summary(item) for item in node.outgoing],
    )


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
    node_id: str
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
