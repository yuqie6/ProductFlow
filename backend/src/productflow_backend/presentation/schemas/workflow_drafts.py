from __future__ import annotations

from datetime import datetime
from typing import Literal

from pydantic import BaseModel, ConfigDict, Field, model_validator

from productflow_backend.application.workflow_drafts.contracts import (
    WORKFLOW_DRAFT_MAX_IMAGES_PER_TYPE,
    WORKFLOW_DRAFT_MAX_REFERENCE_ASSETS,
    WORKFLOW_DRAFT_MAX_TOTAL_IMAGES,
    WORKFLOW_DRAFT_MIN_IMAGE_TYPES,
    WORKFLOW_DRAFT_MIN_IMAGES_PER_TYPE,
    WorkflowDraftPayloadV1,
    parse_workflow_draft_payload,
)
from productflow_backend.application.workflow_drafts.materialization import (
    ActiveV2WorkflowSnapshot,
    WorkflowMaterializationResult,
)
from productflow_backend.domain.enums import WorkflowDraftStatus, WorkflowNodeStatus, WorkflowNodeType
from productflow_backend.infrastructure.db.models import (
    ProductWorkflow,
    WorkflowDraft,
    WorkflowDraftRevision,
    WorkflowEdge,
    WorkflowFolder,
    WorkflowNode,
)


class StrictRequestModel(BaseModel):
    model_config = ConfigDict(extra="forbid")


class WorkflowDraftLimitsResponse(BaseModel):
    min_image_types: int = WORKFLOW_DRAFT_MIN_IMAGE_TYPES
    min_images_per_type: int = WORKFLOW_DRAFT_MIN_IMAGES_PER_TYPE
    max_images_per_type: int = WORKFLOW_DRAFT_MAX_IMAGES_PER_TYPE
    max_total_images: int = WORKFLOW_DRAFT_MAX_TOTAL_IMAGES
    max_reference_assets: int = WORKFLOW_DRAFT_MAX_REFERENCE_ASSETS


class CreateWorkflowDraftRequest(StrictRequestModel):
    payload: WorkflowDraftPayloadV1
    ready_for_confirmation: bool = False
    source_turn_id: str | None = Field(default=None, min_length=1, max_length=120)
    source_artifact_step_id: str | None = Field(default=None, min_length=1, max_length=120)

    @model_validator(mode="after")
    def validate_origin_pair(self) -> CreateWorkflowDraftRequest:
        if (self.source_turn_id is None) != (self.source_artifact_step_id is None):
            raise ValueError("source_turn_id 和 source_artifact_step_id 必须同时提供")
        return self


class AppendWorkflowDraftRevisionRequest(CreateWorkflowDraftRequest):
    expected_draft_version: int = Field(ge=1)


class ConfirmWorkflowDraftRequest(StrictRequestModel):
    expected_draft_version: int = Field(ge=1)


class MaterializeWorkflowDraftRequest(StrictRequestModel):
    expected_draft_version: int = Field(ge=1)
    expected_workflow_revision: int = Field(ge=0)
    idempotency_key: str = Field(min_length=1, max_length=120)


class WorkflowDraftRevisionResponse(BaseModel):
    id: str
    draft_id: str
    version: int
    schema_version: Literal[1]
    payload: WorkflowDraftPayloadV1
    payload_hash: str
    source_turn_id: str | None
    source_artifact_step_id: str | None
    confirmed_at: datetime | None
    fact_set_version_id: str | None
    created_at: datetime


class WorkflowDraftResponse(BaseModel):
    id: str
    product_id: str
    status: WorkflowDraftStatus
    current_revision_id: str
    current_revision: WorkflowDraftRevisionResponse
    revisions: list[WorkflowDraftRevisionResponse]
    final_workflow_id: str | None
    limits: WorkflowDraftLimitsResponse
    created_at: datetime
    updated_at: datetime


class WorkflowFolderV2Response(BaseModel):
    id: str
    workflow_id: str
    key: str
    title: str
    order: int
    position_x: int
    position_y: int
    width: int
    height: int
    config_json: dict[str, object]
    created_at: datetime
    updated_at: datetime


class WorkflowNodeV2Response(BaseModel):
    id: str
    workflow_id: str
    schema_version: Literal[2]
    key: str
    node_type: WorkflowNodeType
    title: str
    position_x: int
    position_y: int
    folder_id: str | None
    bound_image_asset_id: str | None
    config_json: dict[str, object]
    status: WorkflowNodeStatus
    output_json: dict[str, object] | None
    failure_reason: str | None
    created_at: datetime
    updated_at: datetime


class WorkflowEdgeV2Response(BaseModel):
    id: str
    workflow_id: str
    key: str
    source_node_id: str
    target_node_id: str
    source_handle: str | None
    target_handle: str | None
    created_at: datetime


class ProductWorkflowV2Response(BaseModel):
    id: str
    product_id: str
    title: str
    active: bool
    schema_version: Literal[2]
    revision: int
    source_draft_revision_id: str
    materialization_id: str
    folders: list[WorkflowFolderV2Response]
    nodes: list[WorkflowNodeV2Response]
    edges: list[WorkflowEdgeV2Response]
    created_at: datetime
    updated_at: datetime


class ActiveProductWorkflowV2Response(BaseModel):
    latest_revision: int
    workflow: ProductWorkflowV2Response | None


class WorkflowMaterializationResponse(BaseModel):
    id: str
    created: bool
    workflow: ProductWorkflowV2Response
    reveal_events_url: str


def serialize_workflow_draft_revision(revision: WorkflowDraftRevision) -> WorkflowDraftRevisionResponse:
    return WorkflowDraftRevisionResponse(
        id=revision.id,
        draft_id=revision.draft_id,
        version=revision.version,
        schema_version=revision.schema_version,
        payload=parse_workflow_draft_payload(revision.payload_json),
        payload_hash=revision.payload_hash,
        source_turn_id=revision.source_turn_id,
        source_artifact_step_id=revision.source_artifact_step_id,
        confirmed_at=revision.confirmed_at,
        fact_set_version_id=revision.fact_set_version.id if revision.fact_set_version is not None else None,
        created_at=revision.created_at,
    )


def serialize_workflow_draft(draft: WorkflowDraft) -> WorkflowDraftResponse:
    if draft.current_revision is None or draft.current_revision_id is None:
        raise ValueError("WorkflowDraft 缺少 current revision")
    return WorkflowDraftResponse(
        id=draft.id,
        product_id=draft.product_id,
        status=draft.status,
        current_revision_id=draft.current_revision_id,
        current_revision=serialize_workflow_draft_revision(draft.current_revision),
        revisions=[serialize_workflow_draft_revision(revision) for revision in draft.revisions],
        final_workflow_id=draft.final_workflow_id,
        limits=WorkflowDraftLimitsResponse(),
        created_at=draft.created_at,
        updated_at=draft.updated_at,
    )


def serialize_workflow_folder_v2(folder: WorkflowFolder) -> WorkflowFolderV2Response:
    return WorkflowFolderV2Response(
        id=folder.id,
        workflow_id=folder.workflow_id,
        key=folder.folder_key,
        title=folder.title,
        order=folder.sort_order,
        position_x=folder.position_x,
        position_y=folder.position_y,
        width=folder.width,
        height=folder.height,
        config_json=folder.config_json or {},
        created_at=folder.created_at,
        updated_at=folder.updated_at,
    )


def serialize_workflow_node_v2(node: WorkflowNode) -> WorkflowNodeV2Response:
    if node.schema_version != 2 or node.node_key is None:
        raise ValueError("v2 workflow projection 收到了非 v2 节点")
    return WorkflowNodeV2Response(
        id=node.id,
        workflow_id=node.workflow_id,
        schema_version=node.schema_version,
        key=node.node_key,
        node_type=node.node_type,
        title=node.title,
        position_x=node.position_x,
        position_y=node.position_y,
        folder_id=node.folder_id,
        bound_image_asset_id=node.bound_image_asset_id,
        config_json=node.config_json or {},
        status=node.status,
        output_json=node.output_json,
        failure_reason=node.failure_reason,
        created_at=node.created_at,
        updated_at=node.updated_at,
    )


def serialize_workflow_edge_v2(edge: WorkflowEdge) -> WorkflowEdgeV2Response:
    if edge.edge_key is None:
        raise ValueError("v2 workflow projection 收到了缺少 key 的连线")
    return WorkflowEdgeV2Response(
        id=edge.id,
        workflow_id=edge.workflow_id,
        key=edge.edge_key,
        source_node_id=edge.source_node_id,
        target_node_id=edge.target_node_id,
        source_handle=edge.source_handle,
        target_handle=edge.target_handle,
        created_at=edge.created_at,
    )


def serialize_product_workflow_v2(workflow: ProductWorkflow) -> ProductWorkflowV2Response:
    if (
        workflow.schema_version != 2
        or workflow.source_draft_revision_id is None
        or workflow.materialization is None
    ):
        raise ValueError("v2 workflow projection 收到了不完整的 lineage")
    return ProductWorkflowV2Response(
        id=workflow.id,
        product_id=workflow.product_id,
        title=workflow.title,
        active=workflow.active,
        schema_version=workflow.schema_version,
        revision=workflow.revision,
        source_draft_revision_id=workflow.source_draft_revision_id,
        materialization_id=workflow.materialization.id,
        folders=[serialize_workflow_folder_v2(folder) for folder in workflow.folders],
        nodes=[serialize_workflow_node_v2(node) for node in workflow.nodes],
        edges=[serialize_workflow_edge_v2(edge) for edge in workflow.edges],
        created_at=workflow.created_at,
        updated_at=workflow.updated_at,
    )


def serialize_active_v2_workflow(snapshot: ActiveV2WorkflowSnapshot) -> ActiveProductWorkflowV2Response:
    return ActiveProductWorkflowV2Response(
        latest_revision=snapshot.latest_revision,
        workflow=serialize_product_workflow_v2(snapshot.workflow) if snapshot.workflow is not None else None,
    )


def serialize_materialization(result: WorkflowMaterializationResult) -> WorkflowMaterializationResponse:
    return WorkflowMaterializationResponse(
        id=result.materialization.id,
        created=result.created,
        workflow=serialize_product_workflow_v2(result.workflow),
        reveal_events_url=f"/api/v2/workflow-materializations/{result.materialization.id}/reveal-events",
    )


__all__ = [
    "ActiveProductWorkflowV2Response",
    "AppendWorkflowDraftRevisionRequest",
    "ConfirmWorkflowDraftRequest",
    "CreateWorkflowDraftRequest",
    "MaterializeWorkflowDraftRequest",
    "WorkflowDraftResponse",
    "WorkflowMaterializationResponse",
    "serialize_active_v2_workflow",
    "serialize_materialization",
    "serialize_workflow_draft",
]
