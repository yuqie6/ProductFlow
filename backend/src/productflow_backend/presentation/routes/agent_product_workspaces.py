from __future__ import annotations

from fastapi import APIRouter, Depends, File, Form, Header, HTTPException, UploadFile, status
from sqlalchemy.orm import Session

from productflow_backend.application.agent_product_intake import parse_agent_product_selection
from productflow_backend.application.agent_product_workspaces import create_agent_product_workspace
from productflow_backend.application.workflow_drafts.contracts import WORKFLOW_DRAFT_MAX_REFERENCE_ASSETS
from productflow_backend.presentation.deps import get_session, require_admin
from productflow_backend.presentation.schemas.agent_conversations import serialize_agent_conversation
from productflow_backend.presentation.schemas.agent_product_workspaces import (
    AgentProductWorkspaceCreateResponse,
    AgentProductWorkspaceOptionsResponse,
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


@router.post("", response_model=AgentProductWorkspaceCreateResponse, status_code=status.HTTP_201_CREATED)
async def create_agent_product_workspace_endpoint(
    name: str = Form(...),
    selection: str = Form(...),
    images: list[UploadFile] = File(...),
    idempotency_key: str = Header(alias="Idempotency-Key", min_length=1, max_length=200),
    session: Session = Depends(get_session),
) -> AgentProductWorkspaceCreateResponse:
    if not images:
        raise HTTPException(status_code=400, detail="至少上传一张商品参考图")
    if len(images) > WORKFLOW_DRAFT_MAX_REFERENCE_ASSETS:
        raise HTTPException(
            status_code=400,
            detail=f"商品参考图最多上传 {WORKFLOW_DRAFT_MAX_REFERENCE_ASSETS} 张",
        )
    validate_reference_image_count(len(images))
    parsed_selection = parse_agent_product_selection(selection)
    image_payloads: list[tuple[bytes, str, str]] = []
    for image in images:
        validated = await read_validated_image_upload(image, fallback_filename="reference.bin")
        image_payloads.append((validated.content, validated.filename, validated.mime_type))

    creation = create_agent_product_workspace(
        session,
        name=name,
        selection=parsed_selection,
        image_uploads=image_payloads,
        idempotency_key=idempotency_key,
    )
    return AgentProductWorkspaceCreateResponse(
        product=serialize_canonical_product_detail(creation.product),
        created_assets=[serialize_product_image_asset(asset) for asset in creation.created_assets],
        workflow_draft=serialize_workflow_draft(creation.workflow_draft),
        conversation=serialize_agent_conversation(creation.conversation),
    )


__all__ = ["router"]
