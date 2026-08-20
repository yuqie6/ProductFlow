from __future__ import annotations

from fastapi import APIRouter, Depends, File, Form, Header, HTTPException, UploadFile, status
from sqlalchemy.orm import Session

from productflow_backend.application.agent.product_intake import parse_agent_product_selection
from productflow_backend.application.agent.product_workspaces import (
    AgentProductWorkspaceCreation,
    create_agent_product_draft_workspace,
    create_agent_product_workspace,
    finalize_agent_product_workspace_intake,
    get_agent_product_workspace,
)
from productflow_backend.application.workflow_drafts.contracts import WORKFLOW_DRAFT_MAX_REFERENCE_ASSETS
from productflow_backend.presentation.deps import get_session, require_admin
from productflow_backend.presentation.schemas.agent_conversations import serialize_agent_conversation
from productflow_backend.presentation.schemas.agent_product_workspaces import (
    AgentProductDraftWorkspaceRequest,
    AgentProductWorkspaceCreateResponse,
    AgentProductWorkspaceOptionsResponse,
    AgentProductWorkspaceSnapshotResponse,
    serialize_agent_product_workspace_options,
)
from productflow_backend.presentation.schemas.products import (
    serialize_canonical_product_detail,
    serialize_product_image_asset,
)
from productflow_backend.presentation.schemas.workflow_drafts import serialize_workflow_draft
from productflow_backend.presentation.upload_validation import (
    read_validated_image_upload,
    validate_reference_image_count,
)

router = APIRouter(
    prefix="/api/v2/agent-product-workspaces",
    tags=["agent-product-workspaces"],
    dependencies=[Depends(require_admin)],
)


@router.get("/options", response_model=AgentProductWorkspaceOptionsResponse)
def get_agent_product_workspace_options_endpoint() -> AgentProductWorkspaceOptionsResponse:
    return serialize_agent_product_workspace_options()


@router.post(
    "/drafts",
    response_model=AgentProductWorkspaceSnapshotResponse,
    status_code=status.HTTP_201_CREATED,
)
def create_agent_product_draft_workspace_endpoint(
    payload: AgentProductDraftWorkspaceRequest,
    idempotency_key: str = Header(alias="Idempotency-Key", min_length=1, max_length=200),
    session: Session = Depends(get_session),
) -> AgentProductWorkspaceSnapshotResponse:
    return _serialize_workspace_snapshot(
        create_agent_product_draft_workspace(
            session,
            name=payload.name,
            idempotency_key=idempotency_key,
            agent_session_id=payload.agent_session_id,
        )
    )


@router.get("/{conversation_id}", response_model=AgentProductWorkspaceSnapshotResponse)
def get_agent_product_workspace_endpoint(
    conversation_id: str,
    session: Session = Depends(get_session),
) -> AgentProductWorkspaceSnapshotResponse:
    return _serialize_workspace_snapshot(
        get_agent_product_workspace(session, conversation_id=conversation_id)
    )


@router.post("/{conversation_id}/intake", response_model=AgentProductWorkspaceSnapshotResponse)
async def finalize_agent_product_workspace_intake_endpoint(
    conversation_id: str,
    selection: str = Form(...),
    images: list[UploadFile] = File(...),
    task_id: str | None = Form(default=None, max_length=64),
    idempotency_key: str = Header(alias="Idempotency-Key", min_length=1, max_length=200),
    session: Session = Depends(get_session),
) -> AgentProductWorkspaceSnapshotResponse:
    image_payloads = await _read_workspace_images(images)
    creation = finalize_agent_product_workspace_intake(
        session,
        conversation_id=conversation_id,
        selection=parse_agent_product_selection(selection),
        image_uploads=image_payloads,
        idempotency_key=idempotency_key,
        task_id=task_id,
    )
    return _serialize_workspace_snapshot(creation)


@router.post("", response_model=AgentProductWorkspaceCreateResponse, status_code=status.HTTP_201_CREATED)
async def create_agent_product_workspace_endpoint(
    name: str = Form(...),
    selection: str = Form(...),
    images: list[UploadFile] = File(...),
    agent_session_id: str | None = Form(default=None),
    idempotency_key: str = Header(alias="Idempotency-Key", min_length=1, max_length=200),
    session: Session = Depends(get_session),
) -> AgentProductWorkspaceCreateResponse:
    parsed_selection = parse_agent_product_selection(selection)
    image_payloads = await _read_workspace_images(images)

    creation = create_agent_product_workspace(
        session,
        name=name,
        selection=parsed_selection,
        image_uploads=image_payloads,
        idempotency_key=idempotency_key,
        agent_session_id=agent_session_id,
    )
    return AgentProductWorkspaceCreateResponse(
        task_id=creation.onboarding_task_id,
        product=serialize_canonical_product_detail(creation.product),
        created_assets=[serialize_product_image_asset(asset) for asset in creation.created_assets],
        workflow_draft=serialize_workflow_draft(creation.workflow_draft),
        conversation=serialize_agent_conversation(creation.conversation),
    )


async def _read_workspace_images(images: list[UploadFile]) -> list[tuple[bytes, str, str]]:
    if not images:
        raise HTTPException(status_code=400, detail="至少上传一张商品参考图")
    if len(images) > WORKFLOW_DRAFT_MAX_REFERENCE_ASSETS:
        raise HTTPException(
            status_code=400,
            detail=f"商品参考图最多上传 {WORKFLOW_DRAFT_MAX_REFERENCE_ASSETS} 张",
        )
    validate_reference_image_count(len(images))
    image_payloads: list[tuple[bytes, str, str]] = []
    for image in images:
        validated = await read_validated_image_upload(image, fallback_filename="reference.bin")
        image_payloads.append((validated.content, validated.filename, validated.mime_type))
    return image_payloads


def _serialize_workspace_snapshot(
    creation: AgentProductWorkspaceCreation,
) -> AgentProductWorkspaceSnapshotResponse:
    return AgentProductWorkspaceSnapshotResponse(
        task_id=creation.onboarding_task_id,
        created=creation.created,
        intake_finalized=creation.workflow_draft.intake_json is not None,
        product=serialize_canonical_product_detail(creation.product),
        created_assets=[serialize_product_image_asset(asset) for asset in creation.created_assets],
        workflow_draft=serialize_workflow_draft(creation.workflow_draft),
        conversation=serialize_agent_conversation(creation.conversation),
    )


__all__ = ["router"]
