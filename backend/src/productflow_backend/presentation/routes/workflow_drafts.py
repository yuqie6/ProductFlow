from __future__ import annotations

from fastapi import APIRouter, Depends, status
from sqlalchemy.orm import Session

from productflow_backend.application.workflow_drafts.confirmation import confirm_product_workflow_draft
from productflow_backend.application.workflow_drafts.service import (
    append_workflow_draft_revision,
    create_workflow_draft,
    get_workflow_draft_or_raise,
)
from productflow_backend.presentation.deps import get_session, require_admin
from productflow_backend.presentation.schemas.workflow_drafts import (
    AppendWorkflowDraftRevisionRequest,
    ConfirmWorkflowDraftRequest,
    CreateWorkflowDraftRequest,
    WorkflowDraftResponse,
    serialize_workflow_draft,
)

router = APIRouter(prefix="/api/v2", tags=["workflow-drafts"], dependencies=[Depends(require_admin)])


@router.post(
    "/products/{product_id}/workflow-drafts",
    response_model=WorkflowDraftResponse,
    status_code=status.HTTP_201_CREATED,
)
def create_workflow_draft_endpoint(
    product_id: str,
    payload: CreateWorkflowDraftRequest,
    session: Session = Depends(get_session),
) -> WorkflowDraftResponse:
    draft = create_workflow_draft(
        session,
        product_id=product_id,
        payload=payload.payload,
        ready_for_confirmation=payload.ready_for_confirmation,
        source_turn_id=payload.source_turn_id,
        source_artifact_step_id=payload.source_artifact_step_id,
    )
    return serialize_workflow_draft(draft)


@router.get(
    "/products/{product_id}/workflow-drafts/{draft_id}",
    response_model=WorkflowDraftResponse,
)
def get_workflow_draft_endpoint(
    product_id: str,
    draft_id: str,
    session: Session = Depends(get_session),
) -> WorkflowDraftResponse:
    return serialize_workflow_draft(
        get_workflow_draft_or_raise(session, product_id=product_id, draft_id=draft_id)
    )


@router.post(
    "/products/{product_id}/workflow-drafts/{draft_id}/revisions",
    response_model=WorkflowDraftResponse,
    status_code=status.HTTP_201_CREATED,
)
def append_workflow_draft_revision_endpoint(
    product_id: str,
    draft_id: str,
    payload: AppendWorkflowDraftRevisionRequest,
    session: Session = Depends(get_session),
) -> WorkflowDraftResponse:
    draft = append_workflow_draft_revision(
        session,
        product_id=product_id,
        draft_id=draft_id,
        expected_draft_version=payload.expected_draft_version,
        payload=payload.payload,
        ready_for_confirmation=payload.ready_for_confirmation,
        source_turn_id=payload.source_turn_id,
        source_artifact_step_id=payload.source_artifact_step_id,
    )
    return serialize_workflow_draft(draft)


@router.post(
    "/products/{product_id}/workflow-drafts/{draft_id}/confirm",
    response_model=WorkflowDraftResponse,
)
def confirm_workflow_draft_endpoint(
    product_id: str,
    draft_id: str,
    payload: ConfirmWorkflowDraftRequest,
    session: Session = Depends(get_session),
) -> WorkflowDraftResponse:
    draft = confirm_product_workflow_draft(
        session,
        product_id=product_id,
        draft_id=draft_id,
        expected_draft_version=payload.expected_draft_version,
    )
    return serialize_workflow_draft(draft)
