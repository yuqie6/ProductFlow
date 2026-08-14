from __future__ import annotations

from datetime import datetime
from typing import Annotated, Literal

from pydantic import BaseModel, ConfigDict, Field, model_validator

from productflow_backend.application.agent_product_intake import WorkflowIntakeV1, parse_workflow_intake
from productflow_backend.application.product_workflow.folders import WorkflowNodePosition
from productflow_backend.application.product_workflow.v2_canvas_mutations import WorkflowCanvasMutationResult
from productflow_backend.application.product_workflow.v2_node_editing import V2WorkflowNodeDetail
from productflow_backend.application.product_workflow.v2_reference_bindings import V2ReferenceBindingResult
from productflow_backend.application.workflow_drafts.contracts import (
    WORKFLOW_DRAFT_MAX_IMAGES_PER_TYPE,
    WORKFLOW_DRAFT_MAX_REFERENCE_ASSETS,
    WORKFLOW_DRAFT_MAX_TOTAL_IMAGES,
    WORKFLOW_DRAFT_MIN_IMAGE_TYPES,
    WORKFLOW_DRAFT_MIN_IMAGES_PER_TYPE,
    DeliverySpec,
    GenerationSpec,
    ImagePromptPayloadV1,
    WorkflowDraftPayloadV1,
    parse_workflow_draft_payload,
)
from productflow_backend.application.workflow_drafts.materialization import (
    ActiveV2WorkflowSnapshot,
    WorkflowMaterializationResult,
)
from productflow_backend.domain.durable_generation_tasks import WORKFLOW_RUN_GENERATION_TASK_CONTRACT
from productflow_backend.domain.enums import WorkflowDraftStatus, WorkflowNodeStatus, WorkflowRunStatus
from productflow_backend.infrastructure.db.models import (
    ProductWorkflow,
    WorkflowDraft,
    WorkflowDraftRevision,
    WorkflowEdge,
    WorkflowFolder,
    WorkflowNode,
    WorkflowNodeRun,
    WorkflowRun,
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
    expected_draft_version: int = Field(ge=0)


class ConfirmWorkflowDraftRequest(StrictRequestModel):
    expected_draft_version: int = Field(ge=1)


class MaterializeWorkflowDraftRequest(StrictRequestModel):
    expected_draft_version: int = Field(ge=1)
    expected_workflow_revision: int = Field(ge=0)
    idempotency_key: str = Field(min_length=1, max_length=120)


class BindWorkflowReferenceAssetRequest(StrictRequestModel):
    asset_id: str = Field(min_length=1, max_length=36)
    expected_workflow_revision: int = Field(ge=1)
    expected_bound_asset_id: str | None = Field(max_length=36)


class CreateWorkflowFolderRequest(StrictRequestModel):
    title: str = Field(min_length=1, max_length=255)
    node_ids: list[str] = Field(min_length=1)
    expected_edit_version: int = Field(ge=0)


class RenameWorkflowFolderRequest(StrictRequestModel):
    title: str = Field(min_length=1, max_length=255)
    expected_edit_version: int = Field(ge=0)


class SetWorkflowFolderMembersRequest(StrictRequestModel):
    node_ids: list[str]
    expected_edit_version: int = Field(ge=0)


class TranslateWorkflowFolderRequest(StrictRequestModel):
    delta_x: int
    delta_y: int
    expected_edit_version: int = Field(ge=0)


class WorkflowNodePositionRequest(StrictRequestModel):
    node_id: str = Field(min_length=1, max_length=36)
    position_x: int
    position_y: int


class UpdateWorkflowNodeLayoutRequest(StrictRequestModel):
    positions: list[WorkflowNodePositionRequest] = Field(min_length=1)
    expected_edit_version: int = Field(ge=0)


class CreateReferenceWorkflowNodeV2Request(StrictRequestModel):
    expected_edit_version: int = Field(ge=0)
    title: str = Field(min_length=1, max_length=255)
    role: str = Field(min_length=1, max_length=120)
    label: str = Field(min_length=1, max_length=255)
    position_x: int
    position_y: int
    folder_id: str | None = Field(default=None, max_length=36)


class DuplicateWorkflowNodeV2Request(StrictRequestModel):
    expected_edit_version: int = Field(ge=0)


class CreateWorkflowEdgeV2Request(StrictRequestModel):
    expected_edit_version: int = Field(ge=0)
    source_node_id: str = Field(min_length=1, max_length=36)
    target_node_id: str = Field(min_length=1, max_length=36)


class UpdateReferenceWorkflowNodeV2Request(StrictRequestModel):
    node_type: Literal["reference_image"]
    expected_edit_version: int = Field(ge=0)
    title: str = Field(min_length=1, max_length=255)
    role: str = Field(min_length=1, max_length=120)
    label: str = Field(min_length=1, max_length=255)


class UpdatePromptWorkflowNodeV2Request(StrictRequestModel):
    node_type: Literal["prompt_generation"]
    expected_edit_version: int = Field(ge=0)
    expected_prompt_artifact_version_id: str = Field(min_length=1, max_length=36)
    title: str = Field(min_length=1, max_length=255)
    prompt_payload: ImagePromptPayloadV1


class UpdateImageWorkflowNodeV2Request(StrictRequestModel):
    node_type: Literal["image_generation"]
    expected_edit_version: int = Field(ge=0)
    title: str = Field(min_length=1, max_length=255)
    variation_instruction: str | None = Field(default=None, max_length=4000)
    generation_spec: GenerationSpec
    delivery_spec: DeliverySpec | None = None


UpdateWorkflowNodeV2Request = Annotated[
    UpdateReferenceWorkflowNodeV2Request
    | UpdatePromptWorkflowNodeV2Request
    | UpdateImageWorkflowNodeV2Request,
    Field(discriminator="node_type"),
]


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
    visual_system_version_id: str | None
    created_at: datetime


class WorkflowDraftResponse(BaseModel):
    id: str
    product_id: str
    status: WorkflowDraftStatus
    current_revision_id: str | None
    current_revision: WorkflowDraftRevisionResponse | None
    current_version: int
    revisions: list[WorkflowDraftRevisionResponse]
    intake: WorkflowIntakeV1 | None
    final_workflow_id: str | None
    recipe_seed: WorkflowDraftRecipeSeedResponse | None
    limits: WorkflowDraftLimitsResponse
    created_at: datetime
    updated_at: datetime


class WorkflowDraftRecipeSeedResponse(BaseModel):
    id: str
    workflow_draft_id: str
    recipe_version_id: str
    recipe_id: str
    recipe_version: int
    recipe_title: str
    product_id: str
    base_workflow_id: str | None
    base_workflow_revision: int | None
    schema_version: Literal[1]
    created_at: datetime


class WorkflowFolderV2Response(BaseModel):
    id: str
    workflow_id: str
    key: str
    title: str
    order: int
    created_at: datetime
    updated_at: datetime


WorkflowNodeTypeV2Value = Literal[
    "product_context",
    "reference_image",
    "prompt_generation",
    "image_generation",
]
WorkflowRunnableNodeTypeV2Value = Literal["prompt_generation", "image_generation"]


class WorkflowNodeV2Response(BaseModel):
    id: str
    workflow_id: str
    schema_version: Literal[2]
    key: str
    node_type: WorkflowNodeTypeV2Value
    title: str
    position_x: int
    position_y: int
    folder_id: str | None
    bound_image_asset_id: str | None
    current_prompt_artifact_version_id: str | None
    config_json: dict[str, object]
    status: WorkflowNodeStatus
    output_json: dict[str, object] | None
    failure_reason: str | None
    created_at: datetime
    updated_at: datetime


class PromptArtifactVersionV2Response(BaseModel):
    artifact_id: str
    artifact_title: str
    image_type_key: str
    version_id: str
    version: int
    schema_version: Literal[1]
    payload: ImagePromptPayloadV1
    created_at: datetime


class WorkflowNodeDetailV2Response(BaseModel):
    workflow_id: str
    workflow_revision: int
    workflow_edit_version: int
    source_draft_revision_id: str
    visual_system_version_id: str
    node: WorkflowNodeV2Response
    prompt_artifact: PromptArtifactVersionV2Response | None


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
    edit_version: int
    source_draft_revision_id: str
    visual_system_version_id: str
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


class WorkflowCanvasMutationResponse(BaseModel):
    changed: bool
    edit_version: int
    dissolved_folder_ids: list[str]
    workflow: ProductWorkflowV2Response


class WorkflowActualMediaResponse(BaseModel):
    mime_type: Literal["image/png", "image/jpeg", "image/webp"]
    width: int = Field(gt=0)
    height: int = Field(gt=0)
    byte_size: int = Field(gt=0)
    sha256: str = Field(pattern=r"^[0-9a-f]{64}$")


class WorkflowNodeRunV2Response(BaseModel):
    id: str
    schema_version: Literal[2]
    workflow_run_id: str
    node_id: str
    node_type: WorkflowRunnableNodeTypeV2Value
    status: WorkflowNodeStatus
    output_json: dict[str, object] | None
    failure_reason: str | None
    visual_system_version_id: str
    prompt_artifact_version_id: str | None
    generation_record_id: str | None
    result_asset_id: str | None
    requested_spec: GenerationSpec | None
    effective_parameters: dict[str, object] | None
    actual_media: WorkflowActualMediaResponse | None
    compiled_prompt: str | None
    compiled_prompt_hash: str | None
    reference_asset_ids: list[str]
    provider_name: str | None
    provider_model: str | None
    provider_response_id: str | None
    provider_status: str | None
    started_at: datetime
    finished_at: datetime | None


class SubmitWorkflowNodeRunV2Response(BaseModel):
    created: bool
    node_run: WorkflowNodeRunV2Response


class WorkflowNodeRunListV2Response(BaseModel):
    items: list[WorkflowNodeRunV2Response]


class WorkflowRunV2Response(BaseModel):
    id: str
    schema_version: Literal[2]
    workflow_id: str
    status: WorkflowRunStatus
    failure_reason: str | None
    is_retryable: bool
    is_cancelable: bool
    progress_metadata: dict[str, object] | None
    started_at: datetime
    finished_at: datetime | None
    node_runs: list[WorkflowNodeRunV2Response]


class WorkflowRunDetailV2Response(BaseModel):
    workflow_run: WorkflowRunV2Response
    workflow: ProductWorkflowV2Response


class SubmitWorkflowRunV2Response(WorkflowRunDetailV2Response):
    created: bool


class WorkflowRunListV2Response(BaseModel):
    items: list[WorkflowRunV2Response]
    workflow: ProductWorkflowV2Response


class BindWorkflowReferenceAssetResponse(BaseModel):
    changed: bool
    previous_asset_id: str | None
    affected_node_ids: list[str]
    reference_node: WorkflowNodeV2Response


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
        visual_system_version_id=revision.visual_system_version_id,
        created_at=revision.created_at,
    )


def serialize_workflow_draft(draft: WorkflowDraft) -> WorkflowDraftResponse:
    current_revision = draft.current_revision
    seed = draft.recipe_seed
    return WorkflowDraftResponse(
        id=draft.id,
        product_id=draft.product_id,
        status=draft.status,
        current_revision_id=draft.current_revision_id,
        current_revision=(
            serialize_workflow_draft_revision(current_revision)
            if current_revision is not None
            else None
        ),
        current_version=current_revision.version if current_revision is not None else 0,
        revisions=[serialize_workflow_draft_revision(revision) for revision in draft.revisions],
        intake=parse_workflow_intake(
            schema_version=draft.intake_schema_version,
            payload=draft.intake_json,
        ),
        final_workflow_id=draft.final_workflow_id,
        recipe_seed=(
            WorkflowDraftRecipeSeedResponse(
                id=seed.id,
                workflow_draft_id=seed.workflow_draft_id,
                recipe_version_id=seed.recipe_version_id,
                recipe_id=seed.recipe_version.recipe_id,
                recipe_version=seed.recipe_version.version,
                recipe_title=seed.recipe_version.title,
                product_id=seed.product_id,
                base_workflow_id=seed.base_workflow_id,
                base_workflow_revision=seed.base_workflow_revision,
                schema_version=seed.schema_version,
                created_at=seed.created_at,
            )
            if seed is not None
            else None
        ),
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
        node_type=node.node_type.value,
        title=node.title,
        position_x=node.position_x,
        position_y=node.position_y,
        folder_id=node.folder_id,
        bound_image_asset_id=node.bound_image_asset_id,
        current_prompt_artifact_version_id=node.current_prompt_artifact_version_id,
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
        edit_version=workflow.edit_version,
        source_draft_revision_id=workflow.source_draft_revision_id,
        visual_system_version_id=workflow.visual_system_version_id,
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


def serialize_reference_binding(result: V2ReferenceBindingResult) -> BindWorkflowReferenceAssetResponse:
    return BindWorkflowReferenceAssetResponse(
        changed=result.changed,
        previous_asset_id=result.previous_asset_id,
        affected_node_ids=list(result.affected_node_ids),
        reference_node=serialize_workflow_node_v2(result.reference_node),
    )


def serialize_workflow_node_detail_v2(detail: V2WorkflowNodeDetail) -> WorkflowNodeDetailV2Response:
    workflow = detail.workflow
    if workflow.source_draft_revision_id is None or workflow.visual_system_version_id is None:
        raise ValueError("v2 节点详情缺少 workflow lineage")
    prompt = detail.prompt_artifact
    return WorkflowNodeDetailV2Response(
        workflow_id=workflow.id,
        workflow_revision=workflow.revision,
        workflow_edit_version=workflow.edit_version,
        source_draft_revision_id=workflow.source_draft_revision_id,
        visual_system_version_id=workflow.visual_system_version_id,
        node=serialize_workflow_node_v2(detail.node),
        prompt_artifact=(
            PromptArtifactVersionV2Response(
                artifact_id=prompt.artifact.id,
                artifact_title=prompt.artifact.title,
                image_type_key=prompt.artifact.image_type_key,
                version_id=prompt.version.id,
                version=prompt.version.version,
                schema_version=prompt.version.schema_version,
                payload=prompt.payload,
                created_at=prompt.version.created_at,
            )
            if prompt is not None
            else None
        ),
    )


def to_workflow_node_positions(
    items: list[WorkflowNodePositionRequest],
) -> tuple[WorkflowNodePosition, ...]:
    return tuple(
        WorkflowNodePosition(
            node_id=item.node_id,
            position_x=item.position_x,
            position_y=item.position_y,
        )
        for item in items
    )


def serialize_canvas_mutation(
    result: WorkflowCanvasMutationResult,
) -> WorkflowCanvasMutationResponse:
    return WorkflowCanvasMutationResponse(
        changed=result.changed,
        edit_version=result.workflow.edit_version,
        dissolved_folder_ids=list(result.dissolved_folder_ids),
        workflow=serialize_product_workflow_v2(result.workflow),
    )


def serialize_workflow_node_run_v2(node_run: WorkflowNodeRun) -> WorkflowNodeRunV2Response:
    workflow = node_run.workflow_run.workflow
    if workflow.schema_version != 2 or node_run.node.schema_version != 2:
        raise ValueError("v2 node run projection 收到了 schema-v1 运行")
    if workflow.visual_system_version_id is None:
        raise ValueError("v2 node run projection 缺少 VisualSystemVersion")

    prompt_version = node_run.prompt_artifact_version
    generation = node_run.image_generation_record
    prompt_artifact_version_id = (
        prompt_version.id
        if prompt_version is not None
        else generation.prompt_artifact_version_id
        if generation is not None
        else node_run.node.current_prompt_artifact_version_id
    )
    if generation is not None:
        reference_asset_ids = [reference.asset_id for reference in generation.references]
        provider_name = generation.provider_name
        provider_model = generation.provider_model
        provider_response_id = generation.provider_response_id
    elif prompt_version is not None:
        reference_asset_ids = [reference.asset_id for reference in prompt_version.references]
        provider_name = prompt_version.provider_name
        provider_model = prompt_version.provider_model
        provider_response_id = prompt_version.provider_response_id
    else:
        reference_asset_ids = []
        provider_name = None
        provider_model = None
        provider_response_id = None

    return WorkflowNodeRunV2Response(
        id=node_run.id,
        schema_version=2,
        workflow_run_id=node_run.workflow_run_id,
        node_id=node_run.node_id,
        node_type=node_run.node.node_type.value,
        status=node_run.status,
        output_json=node_run.output_json,
        failure_reason=node_run.failure_reason,
        visual_system_version_id=workflow.visual_system_version_id,
        prompt_artifact_version_id=prompt_artifact_version_id,
        generation_record_id=generation.id if generation is not None else None,
        result_asset_id=generation.result_asset_id if generation is not None else None,
        requested_spec=generation.requested_spec_json if generation is not None else None,
        effective_parameters=generation.effective_parameters_json if generation is not None else None,
        actual_media=generation.actual_media_json if generation is not None else None,
        compiled_prompt=generation.compiled_prompt if generation is not None else None,
        compiled_prompt_hash=generation.compiled_prompt_hash if generation is not None else None,
        reference_asset_ids=reference_asset_ids,
        provider_name=provider_name,
        provider_model=provider_model,
        provider_response_id=provider_response_id,
        provider_status=generation.provider_status if generation is not None else None,
        started_at=node_run.started_at,
        finished_at=node_run.finished_at,
    )


def serialize_workflow_run_v2(run: WorkflowRun) -> WorkflowRunV2Response:
    if run.workflow.schema_version != 2:
        raise ValueError("v2 workflow run projection 收到了 schema-v1 运行")
    return WorkflowRunV2Response(
        id=run.id,
        schema_version=2,
        workflow_id=run.workflow_id,
        status=run.status,
        failure_reason=run.failure_reason,
        is_retryable=run.status == WorkflowRunStatus.FAILED and run.is_retryable,
        is_cancelable=WORKFLOW_RUN_GENERATION_TASK_CONTRACT.is_active(run.status),
        progress_metadata=run.progress_metadata,
        started_at=run.started_at,
        finished_at=run.finished_at,
        node_runs=[
            serialize_workflow_node_run_v2(node_run)
            for node_run in sorted(run.node_runs, key=lambda item: (item.started_at, item.id))
        ],
    )


__all__ = [
    "ActiveProductWorkflowV2Response",
    "AppendWorkflowDraftRevisionRequest",
    "BindWorkflowReferenceAssetRequest",
    "BindWorkflowReferenceAssetResponse",
    "CreateReferenceWorkflowNodeV2Request",
    "CreateWorkflowEdgeV2Request",
    "CreateWorkflowFolderRequest",
    "ConfirmWorkflowDraftRequest",
    "CreateWorkflowDraftRequest",
    "DuplicateWorkflowNodeV2Request",
    "MaterializeWorkflowDraftRequest",
    "RenameWorkflowFolderRequest",
    "SetWorkflowFolderMembersRequest",
    "SubmitWorkflowNodeRunV2Response",
    "SubmitWorkflowRunV2Response",
    "UpdateWorkflowNodeV2Request",
    "WorkflowDraftResponse",
    "TranslateWorkflowFolderRequest",
    "UpdateWorkflowNodeLayoutRequest",
    "WorkflowCanvasMutationResponse",
    "WorkflowMaterializationResponse",
    "WorkflowNodeRunV2Response",
    "WorkflowNodeRunListV2Response",
    "WorkflowRunDetailV2Response",
    "WorkflowRunListV2Response",
    "WorkflowRunV2Response",
    "WorkflowNodeDetailV2Response",
    "serialize_workflow_node_detail_v2",
    "serialize_active_v2_workflow",
    "serialize_materialization",
    "serialize_canvas_mutation",
    "serialize_reference_binding",
    "serialize_workflow_draft",
    "serialize_workflow_node_run_v2",
    "serialize_workflow_run_v2",
    "to_workflow_node_positions",
]
