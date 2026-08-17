from __future__ import annotations

from fastapi import APIRouter, Depends, Header, Query, Response
from sqlalchemy.orm import Session

from productflow_backend.application.agent_tools import (
    AGENT_ASSET_LIST_DEFAULT_LIMIT,
    AGENT_ASSET_LIST_MAX_LIMIT,
    apply_agent_asset_move,
    apply_agent_asset_rename,
    apply_agent_folder_create,
    apply_agent_folder_rename,
    get_agent_contract,
    get_agent_product_context,
    inspect_agent_global_media_assets,
    inspect_agent_product_assets,
    list_agent_global_media_assets,
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
    validate_agent_workflow_draft,
)
from productflow_backend.application.agent_workflow_runs import list_agent_workflow_runs
from productflow_backend.application.gallery_assets import GalleryAssetSort, GalleryDirectoryKind
from productflow_backend.application.gallery_mutations import GalleryAssetMove
from productflow_backend.application.legacy_archive_rebuilds import (
    AGENT_LEGACY_ARCHIVE_LIST_DEFAULT_LIMIT,
    AGENT_LEGACY_ARCHIVE_LIST_MAX_LIMIT,
    inspect_agent_legacy_archive,
    list_agent_legacy_archives,
)
from productflow_backend.application.legacy_archives import LegacyArchiveKind
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
    AgentLegacyArchiveInspectResponse,
    AgentLegacyArchiveListResponse,
    AgentWorkflowDraftValidationRequest,
    AgentWorkflowDraftValidationResponse,
    AgentWorkflowRunListResponse,
    InspectAgentAssetsRequest,
    InspectAgentAssetsResponse,
    InspectAgentLegacyArchiveRequest,
    PrepareAgentAssetMoveRequest,
    PrepareAgentAssetRenameRequest,
    PrepareAgentFolderCreateRequest,
    PrepareAgentFolderRenameRequest,
)
from productflow_backend.presentation.schemas.workflow_drafts import serialize_workflow_run_v2

router = APIRouter(
    prefix="/api/internal/v1/agent-conversations",
    tags=["agent-internal"],
    dependencies=[Depends(require_agent_service)],
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


@router.get("/{conversation_id}/product-context")
def get_agent_product_context_endpoint(
    conversation_id: str,
    session: Session = Depends(get_session),
) -> dict:
    return get_agent_product_context(session, conversation_id)


@router.get("/{conversation_id}/workflow-runs", response_model=AgentWorkflowRunListResponse)
def list_agent_workflow_runs_endpoint(
    conversation_id: str,
    limit: int = Query(default=20, ge=1, le=20),
    session: Session = Depends(get_session),
) -> AgentWorkflowRunListResponse:
    page = list_agent_workflow_runs(session, conversation_id=conversation_id, limit=limit)
    return AgentWorkflowRunListResponse(
        workflow_id=page.workflow_id,
        workflow_revision=page.workflow_revision,
        items=[serialize_workflow_run_v2(run) for run in page.runs],
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
