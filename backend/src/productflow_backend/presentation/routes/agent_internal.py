from __future__ import annotations

from fastapi import APIRouter, Depends, Header, Query, Response
from sqlalchemy.orm import Session

from productflow_backend.application.agent_tools import (
    AGENT_ASSET_LIST_DEFAULT_LIMIT,
    AGENT_ASSET_LIST_MAX_LIMIT,
    apply_agent_asset_rename,
    get_agent_contract,
    get_agent_product_context,
    inspect_agent_product_assets,
    list_agent_product_assets,
    prepare_agent_asset_rename,
    read_agent_product_asset_content,
    reconcile_agent_asset_rename,
)
from productflow_backend.presentation.deps import get_session, require_agent_service
from productflow_backend.presentation.schemas.agent_conversations import (
    AgentAssetListResponse,
    AgentAssetMetadataResponse,
    AgentAssetRenamePreparedRequest,
    AgentAssetRenamePreparedResponse,
    AgentAssetRenameReconcileResponse,
    AgentAssetRenameResultResponse,
    AgentContractResponse,
    InspectAgentAssetsRequest,
    InspectAgentAssetsResponse,
    PrepareAgentAssetRenameRequest,
)

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


@router.get("/{conversation_id}/product-context")
def get_agent_product_context_endpoint(
    conversation_id: str,
    session: Session = Depends(get_session),
) -> dict:
    return get_agent_product_context(session, conversation_id)


@router.get("/{conversation_id}/assets", response_model=AgentAssetListResponse)
def list_agent_product_assets_endpoint(
    conversation_id: str,
    query: str = Query(default="", max_length=255),
    after: str = Query(default="", max_length=1024),
    limit: int = Query(default=AGENT_ASSET_LIST_DEFAULT_LIMIT, ge=1, le=AGENT_ASSET_LIST_MAX_LIMIT),
    session: Session = Depends(get_session),
) -> AgentAssetListResponse:
    page = list_agent_product_assets(
        session,
        conversation_id=conversation_id,
        query=query,
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
    return InspectAgentAssetsResponse(
        items=[AgentAssetMetadataResponse.model_validate(item) for item in items]
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
