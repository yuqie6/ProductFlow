from __future__ import annotations

from urllib.parse import quote

from fastapi import APIRouter, Depends, Header, Query, Response
from sqlalchemy.orm import Session

from productflow_backend.application.agent.execution import (
    append_agent_turn_checkpoint,
    append_agent_turn_event,
    claim_agent_turn_execution,
    heartbeat_agent_turn_execution,
    release_agent_turn_execution,
)
from productflow_backend.application.agent.product_workspaces import (
    create_agent_product_draft_workspace_from_global_conversation,
    reconcile_agent_product_draft_workspace_from_global_conversation,
)
from productflow_backend.application.agent.tools import (
    AGENT_ASSET_LIST_DEFAULT_LIMIT,
    AGENT_ASSET_LIST_MAX_LIMIT,
    AGENT_GLOBAL_PRODUCT_LIST_MAX_LIMIT,
    apply_agent_asset_move,
    apply_agent_asset_rename,
    apply_agent_folder_create,
    apply_agent_folder_rename,
    get_agent_contract,
    get_agent_global_workflow_context,
    get_agent_product_context,
    get_agent_runtime_context,
    inspect_agent_global_media_assets,
    inspect_agent_global_products,
    inspect_agent_product_assets,
    list_agent_global_media_assets,
    list_agent_global_products,
    list_agent_product_assets,
    prepare_agent_asset_move,
    prepare_agent_asset_rename,
    prepare_agent_folder_create,
    prepare_agent_folder_rename,
    read_agent_global_media_asset_content,
    read_agent_product_asset_content,
    reconcile_agent_asset_move,
    reconcile_agent_asset_rename,
    reconcile_agent_folder_create,
    reconcile_agent_folder_rename,
    validate_agent_global_draft,
    validate_agent_library_organization_draft,
    validate_agent_workflow_draft,
)
from productflow_backend.application.agent.workflow_run_requests import (
    create_agent_global_workflow_run_request as create_agent_global_workflow_run_request_use_case,
)
from productflow_backend.application.agent.workflow_run_requests import (
    create_agent_workflow_run_request as create_agent_workflow_run_request_use_case,
)
from productflow_backend.application.agent.workflow_run_requests import (
    prepare_agent_global_workflow_run_request as prepare_agent_global_workflow_run_request_use_case,
)
from productflow_backend.application.agent.workflow_run_requests import (
    prepare_agent_workflow_run_request as prepare_agent_workflow_run_request_use_case,
)
from productflow_backend.application.agent.workflow_run_requests import (
    reconcile_agent_global_workflow_run_request as reconcile_agent_global_workflow_run_request_use_case,
)
from productflow_backend.application.agent.workflow_run_requests import (
    reconcile_agent_workflow_run_request as reconcile_agent_workflow_run_request_use_case,
)
from productflow_backend.application.agent.workflow_runs import (
    inspect_agent_global_workflow_runs,
    list_agent_workflow_runs,
)
from productflow_backend.application.legacy_archive_rebuilds import (
    AGENT_LEGACY_ARCHIVE_LIST_DEFAULT_LIMIT,
    AGENT_LEGACY_ARCHIVE_LIST_MAX_LIMIT,
    inspect_agent_legacy_archive,
    list_agent_legacy_archives,
)
from productflow_backend.application.legacy_archives import LegacyArchiveKind
from productflow_backend.application.product_images.mutations import GalleryAssetMove
from productflow_backend.application.product_images.queries import GalleryAssetSort, GalleryDirectoryKind
from productflow_backend.domain.errors import ConflictError
from productflow_backend.presentation.deps import get_session, require_agent_service
from productflow_backend.presentation.schemas.agent_conversations import (
    AgentAssetListResponse,
    AgentAssetMetadataResponse,
    AgentAssetMovePreparedRequest,
    AgentAssetMoveReconcileResponse,
    AgentAssetMoveResultResponse,
    AgentAssetRenamePreparedRequest,
    AgentAssetRenamePreparedResponse,
    AgentAssetRenameReconcileResponse,
    AgentAssetRenameResultResponse,
    AgentContractResponse,
    AgentFolderCreatePreparedRequest,
    AgentFolderCreateReconcileResponse,
    AgentFolderCreateResultResponse,
    AgentFolderRenamePreparedRequest,
    AgentFolderRenameReconcileResponse,
    AgentFolderRenameResultResponse,
    AgentGlobalProductListResponse,
    AgentGlobalProductResponse,
    AgentGlobalWorkflowRunRequestCreateRequest,
    AgentLegacyArchiveInspectResponse,
    AgentLegacyArchiveListResponse,
    AgentProductWorkspaceLaunchRequest,
    AgentProductWorkspaceLaunchResponse,
    AgentProductWorkspaceReconcileResponse,
    AgentRuntimeContextResponse,
    AgentTurnCheckpointRequest,
    AgentTurnCheckpointResponse,
    AgentTurnEventRequest,
    AgentTurnEventResponse,
    AgentTurnExecutionClaimRequest,
    AgentTurnExecutionHeartbeatRequest,
    AgentTurnExecutionLeaseResponse,
    AgentTurnExecutionReleaseRequest,
    AgentTurnExecutionReleaseResponse,
    AgentWorkflowDraftValidationRequest,
    AgentWorkflowDraftValidationResponse,
    AgentWorkflowRunListResponse,
    AgentWorkflowRunRequestCreateRequest,
    AgentWorkflowRunRequestPreparedResponse,
    AgentWorkflowRunRequestReconcileResponse,
    AgentWorkflowRunRequestResponse,
    InspectAgentAssetsRequest,
    InspectAgentAssetsResponse,
    InspectAgentLegacyArchiveRequest,
    InspectAgentProductsRequest,
    InspectAgentProductsResponse,
    InspectAgentWorkflowRunsRequest,
    InspectAgentWorkflowRunsResponse,
    PrepareAgentAssetMoveRequest,
    PrepareAgentAssetRenameRequest,
    PrepareAgentFolderCreateRequest,
    PrepareAgentFolderRenameRequest,
    PrepareAgentGlobalWorkflowRunRequest,
    PrepareAgentWorkflowRunRequest,
    serialize_agent_workflow_run_request,
)
from productflow_backend.presentation.schemas.graphs import serialize_graph_run

router = APIRouter(
    prefix="/api/internal/v1/agent-conversations",
    tags=["agent-internal"],
    dependencies=[Depends(require_agent_service)],
)


def _serialize_agent_product_workspace_launch(
    *,
    global_conversation_id: str,
    creation,
) -> AgentProductWorkspaceLaunchResponse:
    session_id = creation.conversation.session_id
    workflow_draft_id = creation.conversation.workflow_draft_id
    task_id = creation.onboarding_task_id
    if session_id is None or workflow_draft_id is None or task_id is None:
        raise ConflictError("Agent 商品工作区缺少 Session 或 WorkflowDraft")
    navigation_path = (
        "/products/new?workspace="
        + quote(creation.conversation.id, safe="")
        + "&agent_session_id="
        + quote(session_id, safe="")
        + "&agent_task_id="
        + quote(task_id, safe="")
    )
    return AgentProductWorkspaceLaunchResponse(
        created=creation.created,
        session_id=session_id,
        global_conversation_id=global_conversation_id,
        product_conversation_id=creation.conversation.id,
        product_id=creation.product.id,
        product_name=creation.product.name,
        workflow_draft_id=workflow_draft_id,
        task_id=task_id,
        intake_finalized=creation.workflow_draft.intake_json is not None,
        navigation_path=navigation_path,
    )


@router.get("/{conversation_id}/contract", response_model=AgentContractResponse)
def get_agent_contract_endpoint(
    conversation_id: str,
    session: Session = Depends(get_session),
) -> AgentContractResponse:
    return AgentContractResponse.model_validate(get_agent_contract(session, conversation_id))


@router.post(
    "/{conversation_id}/workflow-draft/validate",
    response_model=AgentWorkflowDraftValidationResponse,
)
def validate_agent_workflow_draft_endpoint(
    conversation_id: str,
    payload: AgentWorkflowDraftValidationRequest,
    session: Session = Depends(get_session),
) -> AgentWorkflowDraftValidationResponse:
    validate_agent_workflow_draft(
        session,
        conversation_id=conversation_id,
        value=payload.value,
    )
    return AgentWorkflowDraftValidationResponse()


@router.post(
    "/{conversation_id}/library-organization-draft/validate",
    response_model=AgentWorkflowDraftValidationResponse,
)
def validate_agent_library_organization_draft_endpoint(
    conversation_id: str,
    payload: AgentWorkflowDraftValidationRequest,
    session: Session = Depends(get_session),
) -> AgentWorkflowDraftValidationResponse:
    validate_agent_library_organization_draft(
        session,
        conversation_id=conversation_id,
        value=payload.value,
    )
    return AgentWorkflowDraftValidationResponse()


@router.post(
    "/{conversation_id}/global-draft/validate",
    response_model=AgentWorkflowDraftValidationResponse,
)
def validate_agent_global_draft_endpoint(
    conversation_id: str,
    payload: AgentWorkflowDraftValidationRequest,
    session: Session = Depends(get_session),
) -> AgentWorkflowDraftValidationResponse:
    validate_agent_global_draft(
        session,
        conversation_id=conversation_id,
        value=payload.value,
    )
    return AgentWorkflowDraftValidationResponse()


@router.get("/{conversation_id}/product-context")
def get_agent_product_context_endpoint(
    conversation_id: str,
    session: Session = Depends(get_session),
) -> dict:
    return get_agent_product_context(session, conversation_id)


@router.get("/{conversation_id}/global-workflow-context")
def get_agent_global_workflow_context_endpoint(
    conversation_id: str,
    product_id: str = Query(min_length=1, max_length=64),
    session: Session = Depends(get_session),
) -> dict:
    return get_agent_global_workflow_context(
        session,
        conversation_id=conversation_id,
        product_id=product_id,
    )


@router.get("/{conversation_id}/runtime-context", response_model=AgentRuntimeContextResponse)
def get_agent_runtime_context_endpoint(
    conversation_id: str,
    task_id: str | None = Query(default=None, max_length=64),
    session: Session = Depends(get_session),
) -> AgentRuntimeContextResponse:
    return AgentRuntimeContextResponse.model_validate(
        get_agent_runtime_context(
            session,
            conversation_id=conversation_id,
            task_id=task_id,
        )
    )


@router.post(
    "/{conversation_id}/turn-executions/claim",
    response_model=AgentTurnExecutionLeaseResponse,
)
def claim_agent_turn_execution_endpoint(
    conversation_id: str,
    payload: AgentTurnExecutionClaimRequest,
    session: Session = Depends(get_session),
) -> AgentTurnExecutionLeaseResponse:
    lease = claim_agent_turn_execution(
        session,
        conversation_id=conversation_id,
        task_id=payload.task_id,
        idempotency_key=payload.idempotency_key,
        harness_turn_id=payload.harness_turn_id,
        owner_id=payload.owner_id,
    )
    return AgentTurnExecutionLeaseResponse(
        execution_id=lease.execution_id,
        projection_id=lease.projection_id,
        harness_turn_id=lease.harness_turn_id,
        owner_id=lease.owner_id,
        lease_token=lease.lease_token,
        attempt=lease.attempt,
        fencing_token=lease.fencing_token,
        phase=lease.phase,
        lease_expires_at=lease.lease_expires_at,
    )


@router.post(
    "/{conversation_id}/turn-executions/{execution_id}/heartbeat",
    response_model=AgentTurnExecutionLeaseResponse,
)
def heartbeat_agent_turn_execution_endpoint(
    conversation_id: str,
    execution_id: str,
    payload: AgentTurnExecutionHeartbeatRequest,
    session: Session = Depends(get_session),
) -> AgentTurnExecutionLeaseResponse:
    lease = heartbeat_agent_turn_execution(
        session,
        conversation_id=conversation_id,
        execution_id=execution_id,
        owner_id=payload.owner_id,
        lease_token=payload.lease_token,
        phase=payload.phase,
    )
    return AgentTurnExecutionLeaseResponse(
        execution_id=lease.execution_id,
        projection_id=lease.projection_id,
        harness_turn_id=lease.harness_turn_id,
        owner_id=lease.owner_id,
        lease_token=lease.lease_token,
        attempt=lease.attempt,
        fencing_token=lease.fencing_token,
        phase=lease.phase,
        lease_expires_at=lease.lease_expires_at,
    )


@router.post(
    "/{conversation_id}/turn-executions/{execution_id}/checkpoints",
    response_model=AgentTurnCheckpointResponse,
)
def append_agent_turn_checkpoint_endpoint(
    conversation_id: str,
    execution_id: str,
    payload: AgentTurnCheckpointRequest,
    session: Session = Depends(get_session),
) -> AgentTurnCheckpointResponse:
    checkpoint = append_agent_turn_checkpoint(
        session,
        conversation_id=conversation_id,
        execution_id=execution_id,
        owner_id=payload.owner_id,
        lease_token=payload.lease_token,
        sequence=payload.sequence,
        kind=payload.kind,
        payload=payload.payload,
    )
    return AgentTurnCheckpointResponse(
        id=checkpoint.id,
        projection_id=checkpoint.projection_id,
        execution_id=checkpoint.execution_id,
        attempt=checkpoint.attempt,
        fencing_token=checkpoint.fencing_token,
        sequence=checkpoint.sequence,
        kind=checkpoint.kind,
        created_at=checkpoint.created_at,
    )


@router.post(
    "/{conversation_id}/turn-executions/{execution_id}/release",
    response_model=AgentTurnExecutionReleaseResponse,
)
def release_agent_turn_execution_endpoint(
    conversation_id: str,
    execution_id: str,
    payload: AgentTurnExecutionReleaseRequest,
    session: Session = Depends(get_session),
) -> AgentTurnExecutionReleaseResponse:
    released = release_agent_turn_execution(
        session,
        conversation_id=conversation_id,
        execution_id=execution_id,
        owner_id=payload.owner_id,
        lease_token=payload.lease_token,
        phase=payload.phase,
    )
    return AgentTurnExecutionReleaseResponse(released=released)


@router.post(
    "/{conversation_id}/turn-executions/{execution_id}/events",
    response_model=AgentTurnEventResponse,
)
def append_agent_turn_event_endpoint(
    conversation_id: str,
    execution_id: str,
    payload: AgentTurnEventRequest,
    session: Session = Depends(get_session),
) -> AgentTurnEventResponse:
    event = append_agent_turn_event(
        session,
        conversation_id=conversation_id,
        execution_id=execution_id,
        owner_id=payload.owner_id,
        lease_token=payload.lease_token,
        sequence=payload.sequence,
        schema_version=payload.schema_version,
        run_id=payload.run_id,
        turn_id=payload.turn_id,
        kind=payload.kind,
        payload=payload.payload,
        created_at=payload.created_at,
    )
    return AgentTurnEventResponse(
        id=event.id,
        projection_id=event.projection_id,
        execution_id=event.execution_id,
        sequence=event.sequence,
        schema_version=event.schema_version,
        kind=event.kind,
        created_at=event.created_at,
    )


@router.get("/{conversation_id}/workflow-runs", response_model=AgentWorkflowRunListResponse)
def list_agent_workflow_runs_endpoint(
    conversation_id: str,
    limit: int = Query(default=20, ge=1, le=20),
    session: Session = Depends(get_session),
) -> AgentWorkflowRunListResponse:
    page = list_agent_workflow_runs(session, conversation_id=conversation_id, limit=limit)
    items = [serialize_graph_run(run) for run in page.graph_runs]
    return AgentWorkflowRunListResponse(
        workflow_id=page.workflow_id,
        workflow_revision=page.workflow_revision,
        items=items,
    )


@router.post(
    "/{conversation_id}/workflow-run-requests/prepare",
    response_model=AgentWorkflowRunRequestPreparedResponse,
)
def prepare_agent_workflow_run_request_endpoint(
    conversation_id: str,
    payload: PrepareAgentWorkflowRunRequest,
    session: Session = Depends(get_session),
) -> AgentWorkflowRunRequestPreparedResponse:
    prepared = prepare_agent_workflow_run_request_use_case(
        session,
        conversation_id=conversation_id,
        expected_workflow_revision=payload.expected_workflow_revision,
        task_id=payload.task_id,
        source_run_id=payload.source_run_id,
    )
    return AgentWorkflowRunRequestPreparedResponse(
        product_id=prepared.product_id,
        workflow_id=prepared.workflow_id,
        workflow_title=prepared.workflow_title,
        workflow_revision=prepared.workflow_revision,
        runnable_node_count=prepared.runnable_node_count,
        task_id=prepared.task_id,
        source_run_id=prepared.source_run_id,
    )


@router.post(
    "/{conversation_id}/global-workflow-run-requests/prepare",
    response_model=AgentWorkflowRunRequestPreparedResponse,
)
def prepare_agent_global_workflow_run_request_endpoint(
    conversation_id: str,
    payload: PrepareAgentGlobalWorkflowRunRequest,
    session: Session = Depends(get_session),
) -> AgentWorkflowRunRequestPreparedResponse:
    prepared = prepare_agent_global_workflow_run_request_use_case(
        session,
        conversation_id=conversation_id,
        product_id=payload.product_id,
        workflow_id=payload.workflow_id,
        expected_workflow_revision=payload.expected_workflow_revision,
        task_id=payload.task_id,
        source_run_id=payload.source_run_id,
    )
    return AgentWorkflowRunRequestPreparedResponse(
        product_id=prepared.product_id,
        workflow_id=prepared.workflow_id,
        workflow_title=prepared.workflow_title,
        workflow_revision=prepared.workflow_revision,
        runnable_node_count=prepared.runnable_node_count,
        task_id=prepared.task_id,
        source_run_id=prepared.source_run_id,
    )


@router.post(
    "/{conversation_id}/workflow-run-requests",
    response_model=AgentWorkflowRunRequestResponse,
)
def create_agent_workflow_run_request_endpoint(
    conversation_id: str,
    payload: AgentWorkflowRunRequestCreateRequest,
    idempotency_key: str = Header(alias="Idempotency-Key", min_length=1, max_length=200),
    session: Session = Depends(get_session),
) -> AgentWorkflowRunRequestResponse:
    request = create_agent_workflow_run_request_use_case(
        session,
        conversation_id=conversation_id,
        expected_workflow_revision=payload.expected_workflow_revision,
        workflow_id=payload.workflow_id,
        source_step_id=payload.source_step_id,
        idempotency_key=idempotency_key,
        task_id=payload.task_id,
        source_run_id=payload.source_run_id,
    )
    return serialize_agent_workflow_run_request(request)


@router.post(
    "/{conversation_id}/global-workflow-run-requests",
    response_model=AgentWorkflowRunRequestResponse,
)
def create_agent_global_workflow_run_request_endpoint(
    conversation_id: str,
    payload: AgentGlobalWorkflowRunRequestCreateRequest,
    idempotency_key: str = Header(alias="Idempotency-Key", min_length=1, max_length=200),
    session: Session = Depends(get_session),
) -> AgentWorkflowRunRequestResponse:
    request = create_agent_global_workflow_run_request_use_case(
        session,
        conversation_id=conversation_id,
        product_id=payload.product_id,
        expected_workflow_revision=payload.expected_workflow_revision,
        workflow_id=payload.workflow_id,
        source_step_id=payload.source_step_id,
        idempotency_key=idempotency_key,
        task_id=payload.task_id,
        source_run_id=payload.source_run_id,
    )
    return serialize_agent_workflow_run_request(request)


@router.post(
    "/{conversation_id}/workflow-run-requests/reconcile",
    response_model=AgentWorkflowRunRequestReconcileResponse,
)
def reconcile_agent_workflow_run_request_endpoint(
    conversation_id: str,
    payload: AgentWorkflowRunRequestCreateRequest,
    idempotency_key: str = Header(alias="Idempotency-Key", min_length=1, max_length=200),
    session: Session = Depends(get_session),
) -> AgentWorkflowRunRequestReconcileResponse:
    result = reconcile_agent_workflow_run_request_use_case(
        session,
        conversation_id=conversation_id,
        expected_workflow_revision=payload.expected_workflow_revision,
        workflow_id=payload.workflow_id,
        source_step_id=payload.source_step_id,
        idempotency_key=idempotency_key,
        task_id=payload.task_id,
        source_run_id=payload.source_run_id,
    )
    return AgentWorkflowRunRequestReconcileResponse(
        state=result.state,
        result=serialize_agent_workflow_run_request(result.request) if result.request is not None else None,
        detail=result.detail,
    )


@router.post(
    "/{conversation_id}/global-workflow-run-requests/reconcile",
    response_model=AgentWorkflowRunRequestReconcileResponse,
)
def reconcile_agent_global_workflow_run_request_endpoint(
    conversation_id: str,
    payload: AgentGlobalWorkflowRunRequestCreateRequest,
    idempotency_key: str = Header(alias="Idempotency-Key", min_length=1, max_length=200),
    session: Session = Depends(get_session),
) -> AgentWorkflowRunRequestReconcileResponse:
    result = reconcile_agent_global_workflow_run_request_use_case(
        session,
        conversation_id=conversation_id,
        product_id=payload.product_id,
        expected_workflow_revision=payload.expected_workflow_revision,
        workflow_id=payload.workflow_id,
        source_step_id=payload.source_step_id,
        idempotency_key=idempotency_key,
        task_id=payload.task_id,
        source_run_id=payload.source_run_id,
    )
    return AgentWorkflowRunRequestReconcileResponse(
        state=result.state,
        result=serialize_agent_workflow_run_request(result.request) if result.request is not None else None,
        detail=result.detail,
    )


@router.get("/{conversation_id}/assets", response_model=AgentAssetListResponse)
def list_agent_product_assets_endpoint(
    conversation_id: str,
    directory_kind: GalleryDirectoryKind = Query(default=GalleryDirectoryKind.ALL),
    directory_key: str | None = Query(default=None, max_length=120),
    query: str = Query(default="", max_length=255),
    sort: GalleryAssetSort = Query(default=GalleryAssetSort.CREATED_DESC),
    after: str = Query(default="", max_length=1024),
    limit: int = Query(default=AGENT_ASSET_LIST_DEFAULT_LIMIT, ge=1, le=AGENT_ASSET_LIST_MAX_LIMIT),
    session: Session = Depends(get_session),
) -> AgentAssetListResponse:
    page = list_agent_product_assets(
        session,
        conversation_id=conversation_id,
        directory_kind=directory_kind,
        directory_key=directory_key,
        query=query,
        sort=sort,
        after=after,
        limit=limit,
    )
    return AgentAssetListResponse(
        items=[AgentAssetMetadataResponse.model_validate(item) for item in page.items],
        next_cursor=page.next_cursor,
    )


@router.post("/{conversation_id}/assets/inspect", response_model=InspectAgentAssetsResponse)
def inspect_agent_product_assets_endpoint(
    conversation_id: str,
    payload: InspectAgentAssetsRequest,
    session: Session = Depends(get_session),
) -> InspectAgentAssetsResponse:
    items = inspect_agent_product_assets(
        session,
        conversation_id=conversation_id,
        asset_ids=payload.asset_ids,
    )
    return InspectAgentAssetsResponse(items=[AgentAssetMetadataResponse.model_validate(item) for item in items])


@router.get("/{conversation_id}/media-library", response_model=AgentAssetListResponse)
def list_agent_global_media_assets_endpoint(
    conversation_id: str,
    query: str = Query(default="", max_length=255),
    cursor: str | None = Query(default=None, max_length=4096),
    limit: int = Query(default=AGENT_ASSET_LIST_DEFAULT_LIMIT, ge=1, le=AGENT_ASSET_LIST_MAX_LIMIT),
    session: Session = Depends(get_session),
) -> AgentAssetListResponse:
    page = list_agent_global_media_assets(
        session,
        conversation_id=conversation_id,
        query=query,
        cursor=cursor,
        limit=limit,
    )
    return AgentAssetListResponse(
        items=[AgentAssetMetadataResponse.model_validate(item) for item in page.items],
        next_cursor=page.next_cursor,
    )


@router.post("/{conversation_id}/media-library/inspect", response_model=InspectAgentAssetsResponse)
def inspect_agent_global_media_assets_endpoint(
    conversation_id: str,
    payload: InspectAgentAssetsRequest,
    session: Session = Depends(get_session),
) -> InspectAgentAssetsResponse:
    items = inspect_agent_global_media_assets(
        session,
        conversation_id=conversation_id,
        asset_ids=payload.asset_ids,
    )
    return InspectAgentAssetsResponse(items=[AgentAssetMetadataResponse.model_validate(item) for item in items])


@router.get(
    "/{conversation_id}/products",
    response_model=AgentGlobalProductListResponse,
)
def list_agent_global_products_endpoint(
    conversation_id: str,
    query: str = Query(default="", max_length=255),
    cursor: str | None = Query(default=None, max_length=4096),
    limit: int = Query(default=AGENT_ASSET_LIST_DEFAULT_LIMIT, ge=1, le=AGENT_GLOBAL_PRODUCT_LIST_MAX_LIMIT),
    session: Session = Depends(get_session),
) -> AgentGlobalProductListResponse:
    page = list_agent_global_products(
        session,
        conversation_id=conversation_id,
        query=query,
        cursor=cursor,
        limit=limit,
    )
    return AgentGlobalProductListResponse(
        items=[AgentGlobalProductResponse.model_validate(item) for item in page.items],
        next_cursor=page.next_cursor,
    )


@router.post(
    "/{conversation_id}/products/inspect",
    response_model=InspectAgentProductsResponse,
)
def inspect_agent_global_products_endpoint(
    conversation_id: str,
    payload: InspectAgentProductsRequest,
    session: Session = Depends(get_session),
) -> InspectAgentProductsResponse:
    items = inspect_agent_global_products(
        session,
        conversation_id=conversation_id,
        product_ids=payload.product_ids,
    )
    return InspectAgentProductsResponse(
        items=[AgentGlobalProductResponse.model_validate(item) for item in items]
    )


@router.post(
    "/{conversation_id}/product-workspaces",
    response_model=AgentProductWorkspaceLaunchResponse,
    status_code=201,
)
def create_agent_product_workspace_from_global_conversation_endpoint(
    conversation_id: str,
    payload: AgentProductWorkspaceLaunchRequest,
    idempotency_key: str = Header(alias="Idempotency-Key", min_length=1, max_length=200),
    session: Session = Depends(get_session),
) -> AgentProductWorkspaceLaunchResponse:
    creation = create_agent_product_draft_workspace_from_global_conversation(
        session,
        global_conversation_id=conversation_id,
        name=payload.name,
        idempotency_key=idempotency_key,
    )
    return _serialize_agent_product_workspace_launch(
        global_conversation_id=conversation_id,
        creation=creation,
    )


@router.post(
    "/{conversation_id}/product-workspaces/reconcile",
    response_model=AgentProductWorkspaceReconcileResponse,
)
def reconcile_agent_product_workspace_from_global_conversation_endpoint(
    conversation_id: str,
    payload: AgentProductWorkspaceLaunchRequest,
    idempotency_key: str = Header(alias="Idempotency-Key", min_length=1, max_length=200),
    session: Session = Depends(get_session),
) -> AgentProductWorkspaceReconcileResponse:
    reconciled = reconcile_agent_product_draft_workspace_from_global_conversation(
        session,
        global_conversation_id=conversation_id,
        name=payload.name,
        idempotency_key=idempotency_key,
    )
    return AgentProductWorkspaceReconcileResponse(
        state=reconciled.state,
        result=(
            _serialize_agent_product_workspace_launch(
                global_conversation_id=conversation_id,
                creation=reconciled.creation,
            )
            if reconciled.creation is not None
            else None
        ),
        detail=reconciled.detail,
    )


@router.post(
    "/{conversation_id}/workflow-runs/inspect",
    response_model=InspectAgentWorkflowRunsResponse,
)
def inspect_agent_global_workflow_runs_endpoint(
    conversation_id: str,
    payload: InspectAgentWorkflowRunsRequest,
    session: Session = Depends(get_session),
) -> InspectAgentWorkflowRunsResponse:
    items = inspect_agent_global_workflow_runs(
        session,
        conversation_id=conversation_id,
        workflow_ids=payload.workflow_ids,
        limit=payload.limit,
    )
    return InspectAgentWorkflowRunsResponse.model_validate({"items": items})


@router.get(
    "/{conversation_id}/legacy-archives",
    response_model=AgentLegacyArchiveListResponse,
)
def list_agent_legacy_archives_endpoint(
    conversation_id: str,
    kind: LegacyArchiveKind = Query(),
    query: str = Query(default="", max_length=255),
    after: str = Query(default="", max_length=4096),
    limit: int = Query(
        default=AGENT_LEGACY_ARCHIVE_LIST_DEFAULT_LIMIT,
        ge=1,
        le=AGENT_LEGACY_ARCHIVE_LIST_MAX_LIMIT,
    ),
    session: Session = Depends(get_session),
) -> AgentLegacyArchiveListResponse:
    return AgentLegacyArchiveListResponse.model_validate(
        list_agent_legacy_archives(
            session,
            conversation_id=conversation_id,
            kind=kind,
            query=query,
            after=after,
            limit=limit,
        )
    )


@router.post(
    "/{conversation_id}/legacy-archives/inspect",
    response_model=AgentLegacyArchiveInspectResponse,
)
def inspect_agent_legacy_archive_endpoint(
    conversation_id: str,
    payload: InspectAgentLegacyArchiveRequest,
    session: Session = Depends(get_session),
) -> AgentLegacyArchiveInspectResponse:
    return AgentLegacyArchiveInspectResponse.model_validate(
        inspect_agent_legacy_archive(
            session,
            conversation_id=conversation_id,
            kind=payload.kind,
            archive_id=payload.archive_id,
            section=payload.section,
            offset=payload.offset,
            limit=payload.limit,
        )
    )


@router.get("/{conversation_id}/assets/{asset_id}/content")
def read_agent_product_asset_content_endpoint(
    conversation_id: str,
    asset_id: str,
    session: Session = Depends(get_session),
) -> Response:
    result = read_agent_product_asset_content(
        session,
        conversation_id=conversation_id,
        asset_id=asset_id,
    )
    return Response(
        content=result.content,
        media_type=result.media_type,
        headers={
            "Cache-Control": "private, no-store",
            "Content-Length": str(len(result.content)),
            "X-Content-Type-Options": "nosniff",
        },
    )


@router.get("/{conversation_id}/media-library/{asset_id}/content")
def read_agent_global_media_asset_content_endpoint(
    conversation_id: str,
    asset_id: str,
    session: Session = Depends(get_session),
) -> Response:
    result = read_agent_global_media_asset_content(
        session,
        conversation_id=conversation_id,
        asset_id=asset_id,
    )
    return Response(
        content=result.content,
        media_type=result.media_type,
        headers={
            "Cache-Control": "private, no-store",
            "Content-Length": str(len(result.content)),
            "X-Content-Type-Options": "nosniff",
        },
    )


@router.post(
    "/{conversation_id}/asset-renames/prepare",
    response_model=AgentAssetRenamePreparedResponse,
)
def prepare_agent_asset_rename_endpoint(
    conversation_id: str,
    payload: PrepareAgentAssetRenameRequest,
    session: Session = Depends(get_session),
) -> AgentAssetRenamePreparedResponse:
    prepared = prepare_agent_asset_rename(
        session,
        conversation_id=conversation_id,
        asset_id=payload.asset_id,
        target_display_name=payload.target_display_name,
    )
    return AgentAssetRenamePreparedResponse(
        asset_id=prepared.asset_id,
        expected_display_name=prepared.expected_display_name,
        target_display_name=prepared.target_display_name,
    )


@router.post("/{conversation_id}/asset-renames", response_model=AgentAssetRenameResultResponse)
def apply_agent_asset_rename_endpoint(
    conversation_id: str,
    payload: AgentAssetRenamePreparedRequest,
    idempotency_key: str = Header(alias="Idempotency-Key", min_length=1, max_length=200),
    session: Session = Depends(get_session),
) -> AgentAssetRenameResultResponse:
    result = apply_agent_asset_rename(
        session,
        conversation_id=conversation_id,
        idempotency_key=idempotency_key,
        asset_id=payload.asset_id,
        expected_display_name=payload.expected_display_name,
        target_display_name=payload.target_display_name,
    )
    return AgentAssetRenameResultResponse.model_validate(result)


@router.post(
    "/{conversation_id}/asset-renames/reconcile",
    response_model=AgentAssetRenameReconcileResponse,
)
def reconcile_agent_asset_rename_endpoint(
    conversation_id: str,
    payload: AgentAssetRenamePreparedRequest,
    idempotency_key: str = Header(alias="Idempotency-Key", min_length=1, max_length=200),
    session: Session = Depends(get_session),
) -> AgentAssetRenameReconcileResponse:
    result = reconcile_agent_asset_rename(
        session,
        conversation_id=conversation_id,
        idempotency_key=idempotency_key,
        asset_id=payload.asset_id,
        expected_display_name=payload.expected_display_name,
        target_display_name=payload.target_display_name,
    )
    return AgentAssetRenameReconcileResponse(
        state=result.state,
        result=(
            AgentAssetRenameResultResponse.model_validate(result.result)
            if result.result is not None
            else None
        ),
        detail=result.detail,
    )


@router.post(
    "/{conversation_id}/folder-creates/prepare",
    response_model=AgentFolderCreatePreparedRequest,
)
def prepare_agent_folder_create_endpoint(
    conversation_id: str,
    payload: PrepareAgentFolderCreateRequest,
    session: Session = Depends(get_session),
) -> AgentFolderCreatePreparedRequest:
    prepared = prepare_agent_folder_create(
        session,
        conversation_id=conversation_id,
        name=payload.name,
    )
    return AgentFolderCreatePreparedRequest(folder_id=prepared.folder_id, name=prepared.name)


@router.post("/{conversation_id}/folder-creates", response_model=AgentFolderCreateResultResponse)
def apply_agent_folder_create_endpoint(
    conversation_id: str,
    payload: AgentFolderCreatePreparedRequest,
    idempotency_key: str = Header(alias="Idempotency-Key", min_length=1, max_length=200),
    session: Session = Depends(get_session),
) -> AgentFolderCreateResultResponse:
    return AgentFolderCreateResultResponse.model_validate(
        apply_agent_folder_create(
            session,
            conversation_id=conversation_id,
            idempotency_key=idempotency_key,
            folder_id=payload.folder_id,
            name=payload.name,
        )
    )


@router.post(
    "/{conversation_id}/folder-creates/reconcile",
    response_model=AgentFolderCreateReconcileResponse,
)
def reconcile_agent_folder_create_endpoint(
    conversation_id: str,
    payload: AgentFolderCreatePreparedRequest,
    idempotency_key: str = Header(alias="Idempotency-Key", min_length=1, max_length=200),
    session: Session = Depends(get_session),
) -> AgentFolderCreateReconcileResponse:
    reconciled = reconcile_agent_folder_create(
        session,
        conversation_id=conversation_id,
        idempotency_key=idempotency_key,
        folder_id=payload.folder_id,
        name=payload.name,
    )
    return AgentFolderCreateReconcileResponse(
        state=reconciled.state,
        result=(
            AgentFolderCreateResultResponse.model_validate(reconciled.result)
            if reconciled.result is not None
            else None
        ),
        detail=reconciled.detail,
    )


@router.post(
    "/{conversation_id}/folder-renames/prepare",
    response_model=AgentFolderRenamePreparedRequest,
)
def prepare_agent_folder_rename_endpoint(
    conversation_id: str,
    payload: PrepareAgentFolderRenameRequest,
    session: Session = Depends(get_session),
) -> AgentFolderRenamePreparedRequest:
    prepared = prepare_agent_folder_rename(
        session,
        conversation_id=conversation_id,
        folder_id=payload.folder_id,
        target_name=payload.target_name,
    )
    return AgentFolderRenamePreparedRequest(
        folder_id=prepared.folder_id,
        expected_name=prepared.expected_name,
        target_name=prepared.target_name,
    )


@router.post("/{conversation_id}/folder-renames", response_model=AgentFolderRenameResultResponse)
def apply_agent_folder_rename_endpoint(
    conversation_id: str,
    payload: AgentFolderRenamePreparedRequest,
    idempotency_key: str = Header(alias="Idempotency-Key", min_length=1, max_length=200),
    session: Session = Depends(get_session),
) -> AgentFolderRenameResultResponse:
    return AgentFolderRenameResultResponse.model_validate(
        apply_agent_folder_rename(
            session,
            conversation_id=conversation_id,
            idempotency_key=idempotency_key,
            folder_id=payload.folder_id,
            expected_name=payload.expected_name,
            target_name=payload.target_name,
        )
    )


@router.post(
    "/{conversation_id}/folder-renames/reconcile",
    response_model=AgentFolderRenameReconcileResponse,
)
def reconcile_agent_folder_rename_endpoint(
    conversation_id: str,
    payload: AgentFolderRenamePreparedRequest,
    idempotency_key: str = Header(alias="Idempotency-Key", min_length=1, max_length=200),
    session: Session = Depends(get_session),
) -> AgentFolderRenameReconcileResponse:
    reconciled = reconcile_agent_folder_rename(
        session,
        conversation_id=conversation_id,
        idempotency_key=idempotency_key,
        folder_id=payload.folder_id,
        expected_name=payload.expected_name,
        target_name=payload.target_name,
    )
    return AgentFolderRenameReconcileResponse(
        state=reconciled.state,
        result=(
            AgentFolderRenameResultResponse.model_validate(reconciled.result)
            if reconciled.result is not None
            else None
        ),
        detail=reconciled.detail,
    )


@router.post(
    "/{conversation_id}/asset-moves/prepare",
    response_model=AgentAssetMovePreparedRequest,
)
def prepare_agent_asset_move_endpoint(
    conversation_id: str,
    payload: PrepareAgentAssetMoveRequest,
    session: Session = Depends(get_session),
) -> AgentAssetMovePreparedRequest:
    prepared = prepare_agent_asset_move(
        session,
        conversation_id=conversation_id,
        asset_ids=payload.asset_ids,
        target_folder_id=payload.target_folder_id,
    )
    return AgentAssetMovePreparedRequest(
        moves=[
            {"asset_id": move.asset_id, "expected_folder_id": move.expected_folder_id}
            for move in prepared.moves
        ],
        target_folder_id=prepared.target_folder_id,
    )


@router.post("/{conversation_id}/asset-moves", response_model=AgentAssetMoveResultResponse)
def apply_agent_asset_move_endpoint(
    conversation_id: str,
    payload: AgentAssetMovePreparedRequest,
    idempotency_key: str = Header(alias="Idempotency-Key", min_length=1, max_length=200),
    session: Session = Depends(get_session),
) -> AgentAssetMoveResultResponse:
    return AgentAssetMoveResultResponse.model_validate(
        apply_agent_asset_move(
            session,
            conversation_id=conversation_id,
            idempotency_key=idempotency_key,
            moves=[
                GalleryAssetMove(
                    asset_id=move.asset_id,
                    expected_folder_id=move.expected_folder_id,
                )
                for move in payload.moves
            ],
            target_folder_id=payload.target_folder_id,
        )
    )


@router.post(
    "/{conversation_id}/asset-moves/reconcile",
    response_model=AgentAssetMoveReconcileResponse,
)
def reconcile_agent_asset_move_endpoint(
    conversation_id: str,
    payload: AgentAssetMovePreparedRequest,
    idempotency_key: str = Header(alias="Idempotency-Key", min_length=1, max_length=200),
    session: Session = Depends(get_session),
) -> AgentAssetMoveReconcileResponse:
    reconciled = reconcile_agent_asset_move(
        session,
        conversation_id=conversation_id,
        idempotency_key=idempotency_key,
        moves=[
            GalleryAssetMove(
                asset_id=move.asset_id,
                expected_folder_id=move.expected_folder_id,
            )
            for move in payload.moves
        ],
        target_folder_id=payload.target_folder_id,
    )
    return AgentAssetMoveReconcileResponse(
        state=reconciled.state,
        result=(
            AgentAssetMoveResultResponse.model_validate(reconciled.result)
            if reconciled.result is not None
            else None
        ),
        detail=reconciled.detail,
    )
