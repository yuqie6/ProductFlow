from __future__ import annotations

from fastapi import APIRouter, Depends, Header, Query
from fastapi.responses import StreamingResponse
from sqlalchemy.orm import Session

from productflow_backend.application.agent.global_drafts import (
    confirm_global_workflow_draft_review,
    get_global_workflow_draft_review,
)
from productflow_backend.application.agent.workflow_run_requests import (
    cancel_agent_workflow_run_request,
    confirm_agent_workflow_run_request,
    get_agent_workflow_run_request,
)
from productflow_backend.application.media_library.drafts import (
    confirm_library_organization_draft_revision,
    get_library_organization_draft_or_raise,
)
from productflow_backend.presentation.deps import get_session, require_admin
from productflow_backend.presentation.routes.agent_turn_http import (
    AGENT_TURN_DEFAULT_PAGE_SIZE,
    AGENT_TURN_MAX_PAGE_SIZE,
    _agent_gateway_or_none,
    _agent_gateway_or_raise,
    _parse_event_cursor,
    answer_agent_question_http,
    control_agent_turn_http,
    get_agent_turn_http,
    list_agent_turns_http,
    reconcile_agent_turn_effect_http,
    stream_agent_turn_events_http,
    submit_agent_turn_http,
)
from productflow_backend.presentation.routes.agent_turn_http import (
    enqueue_agent_turn_sync as _enqueue_agent_turn_sync,
)
from productflow_backend.presentation.schemas.agent_conversations import (
    AgentGlobalWorkflowDraftReviewConfirmRequest,
    AgentGlobalWorkflowDraftReviewResponse,
    AgentQuestionAnswerRequest,
    AgentQuestionAnswerResponse,
    AgentTurnEffectReconciliationRequest,
    AgentTurnEffectReconciliationResponse,
    AgentTurnPageResponse,
    AgentTurnResponse,
    AgentWorkflowRunRequestResponse,
    StartAgentTurnRequest,
    SubmitAgentTurnResponse,
    serialize_agent_workflow_run_request,
)
from productflow_backend.presentation.schemas.library_organization_drafts import (
    ConfirmLibraryOrganizationDraftRequest,
    LibraryOrganizationDraftResponse,
    serialize_library_organization_draft,
)
from productflow_backend.presentation.schemas.workflow_drafts import serialize_workflow_draft

router = APIRouter(
    prefix="/api/v2/agent-conversations",
    tags=["agent-conversations"],
    dependencies=[Depends(require_admin)],
)


enqueue_global_agent_turn_sync = _enqueue_agent_turn_sync


@router.get(
    "/{conversation_id}/library-organization-draft",
    response_model=LibraryOrganizationDraftResponse,
)
def get_global_library_organization_draft_endpoint(
    conversation_id: str,
    session: Session = Depends(get_session),
) -> LibraryOrganizationDraftResponse:
    return serialize_library_organization_draft(
        get_library_organization_draft_or_raise(
            session,
            conversation_id=conversation_id,
        )
    )


@router.post(
    "/{conversation_id}/library-organization-draft/confirm",
    response_model=LibraryOrganizationDraftResponse,
)
def confirm_global_library_organization_draft_endpoint(
    conversation_id: str,
    payload: ConfirmLibraryOrganizationDraftRequest,
    session: Session = Depends(get_session),
) -> LibraryOrganizationDraftResponse:
    draft = get_library_organization_draft_or_raise(
        session,
        conversation_id=conversation_id,
    )
    return serialize_library_organization_draft(
        confirm_library_organization_draft_revision(
            session,
            draft_id=draft.id,
            expected_draft_version=payload.expected_draft_version,
            idempotency_key=payload.idempotency_key,
        )
    )


@router.get(
    "/{conversation_id}/workflow-draft-reviews/{revision_id}",
    response_model=AgentGlobalWorkflowDraftReviewResponse,
)
def get_global_workflow_draft_review_endpoint(
    conversation_id: str,
    revision_id: str,
    session: Session = Depends(get_session),
) -> AgentGlobalWorkflowDraftReviewResponse:
    return _serialize_global_workflow_draft_review(
        get_global_workflow_draft_review(
            session,
            conversation_id=conversation_id,
            revision_id=revision_id,
        )
    )


@router.post(
    "/{conversation_id}/workflow-draft-reviews/{revision_id}/confirm",
    response_model=AgentGlobalWorkflowDraftReviewResponse,
)
def confirm_global_workflow_draft_review_endpoint(
    conversation_id: str,
    revision_id: str,
    payload: AgentGlobalWorkflowDraftReviewConfirmRequest,
    session: Session = Depends(get_session),
) -> AgentGlobalWorkflowDraftReviewResponse:
    return _serialize_global_workflow_draft_review(
        confirm_global_workflow_draft_review(
            session,
            conversation_id=conversation_id,
            revision_id=revision_id,
            expected_draft_version=payload.expected_draft_version,
        )
    )


@router.get(
    "/{conversation_id}/workflow-run-request",
    response_model=AgentWorkflowRunRequestResponse | None,
)
def get_global_workflow_run_request_endpoint(
    conversation_id: str,
    task_id: str | None = Query(default=None, max_length=64),
    session: Session = Depends(get_session),
) -> AgentWorkflowRunRequestResponse | None:
    request = get_agent_workflow_run_request(
        session,
        product_id=None,
        conversation_id=conversation_id,
        task_id=task_id,
    )
    return serialize_agent_workflow_run_request(request) if request is not None else None


@router.post(
    "/{conversation_id}/workflow-run-request/{request_id}/confirm",
    response_model=AgentWorkflowRunRequestResponse,
)
def confirm_global_workflow_run_request_endpoint(
    conversation_id: str,
    request_id: str,
    session: Session = Depends(get_session),
) -> AgentWorkflowRunRequestResponse:
    return serialize_agent_workflow_run_request(
        confirm_agent_workflow_run_request(
            session,
            product_id=None,
            conversation_id=conversation_id,
            request_id=request_id,
        )
    )


@router.post(
    "/{conversation_id}/workflow-run-request/{request_id}/cancel",
    response_model=AgentWorkflowRunRequestResponse,
)
def cancel_global_workflow_run_request_endpoint(
    conversation_id: str,
    request_id: str,
    session: Session = Depends(get_session),
) -> AgentWorkflowRunRequestResponse:
    return serialize_agent_workflow_run_request(
        cancel_agent_workflow_run_request(
            session,
            product_id=None,
            conversation_id=conversation_id,
            request_id=request_id,
        )
    )


@router.get("/{conversation_id}/turns", response_model=AgentTurnPageResponse)
def list_global_agent_turns_endpoint(
    conversation_id: str,
    task_id: str | None = Query(default=None, max_length=64),
    after: str = Query(default="", max_length=4096),
    limit: int = Query(default=AGENT_TURN_DEFAULT_PAGE_SIZE, ge=1, le=AGENT_TURN_MAX_PAGE_SIZE),
    session: Session = Depends(get_session),
) -> AgentTurnPageResponse:
    return list_agent_turns_http(
        session,
        product_id=None,
        conversation_id=conversation_id,
        task_id=task_id,
        after=after,
        limit=limit,
    )


@router.post("/{conversation_id}/turns", response_model=SubmitAgentTurnResponse, status_code=202)
def submit_global_agent_turn_endpoint(
    conversation_id: str,
    payload: StartAgentTurnRequest,
    session: Session = Depends(get_session),
) -> SubmitAgentTurnResponse:
    return submit_agent_turn_http(
        session,
        product_id=None,
        conversation_id=conversation_id,
        payload=payload,
        gateway=_agent_gateway_or_raise(),
        enqueue_sync=enqueue_global_agent_turn_sync,
    )


@router.get("/{conversation_id}/turns/{projection_id}", response_model=AgentTurnResponse)
def get_global_agent_turn_endpoint(
    conversation_id: str,
    projection_id: str,
    session: Session = Depends(get_session),
) -> AgentTurnResponse:
    return get_agent_turn_http(
        session,
        product_id=None,
        conversation_id=conversation_id,
        projection_id=projection_id,
        gateway_or_none=_agent_gateway_or_none,
        require_library_revision_clear=True,
    )


@router.post("/{conversation_id}/turns/{projection_id}/cancel", response_model=AgentTurnResponse)
def cancel_global_agent_turn_endpoint(
    conversation_id: str,
    projection_id: str,
    session: Session = Depends(get_session),
) -> AgentTurnResponse:
    return control_agent_turn_http(
        session,
        product_id=None,
        conversation_id=conversation_id,
        projection_id=projection_id,
        command="cancel",
        gateway_or_raise=_agent_gateway_or_raise,
        enqueue_sync=enqueue_global_agent_turn_sync,
    )


@router.post("/{conversation_id}/turns/{projection_id}/resume", response_model=AgentTurnResponse)
def resume_global_agent_turn_endpoint(
    conversation_id: str,
    projection_id: str,
    session: Session = Depends(get_session),
) -> AgentTurnResponse:
    return control_agent_turn_http(
        session,
        product_id=None,
        conversation_id=conversation_id,
        projection_id=projection_id,
        command="resume",
        gateway_or_raise=_agent_gateway_or_raise,
        enqueue_sync=enqueue_global_agent_turn_sync,
    )


@router.post(
    "/{conversation_id}/turns/{projection_id}/questions/{question_id}/answer",
    response_model=AgentQuestionAnswerResponse,
)
def answer_global_agent_question_endpoint(
    conversation_id: str,
    projection_id: str,
    question_id: str,
    payload: AgentQuestionAnswerRequest,
    session: Session = Depends(get_session),
) -> AgentQuestionAnswerResponse:
    return answer_agent_question_http(
        session,
        product_id=None,
        conversation_id=conversation_id,
        projection_id=projection_id,
        question_id=question_id,
        payload=payload,
        gateway_or_none=_agent_gateway_or_none,
        enqueue_sync=enqueue_global_agent_turn_sync,
    )


@router.post(
    "/{conversation_id}/turns/{projection_id}/effect-reconciliation",
    response_model=AgentTurnEffectReconciliationResponse,
)
def reconcile_global_agent_turn_effect_endpoint(
    conversation_id: str,
    projection_id: str,
    payload: AgentTurnEffectReconciliationRequest,
    session: Session = Depends(get_session),
) -> AgentTurnEffectReconciliationResponse:
    return reconcile_agent_turn_effect_http(
        session,
        product_id=None,
        conversation_id=conversation_id,
        projection_id=projection_id,
        payload=payload,
    )


@router.get("/{conversation_id}/turns/{projection_id}/events")
async def stream_global_agent_turn_events_endpoint(
    conversation_id: str,
    projection_id: str,
    after: int = Query(default=0, ge=0),
    last_event_id: str | None = Header(default=None, alias="Last-Event-ID"),
    session: Session = Depends(get_session),
) -> StreamingResponse:
    return stream_agent_turn_events_http(
        session,
        product_id=None,
        conversation_id=conversation_id,
        projection_id=projection_id,
        after=after,
        last_event_id=last_event_id,
        parse_event_cursor=_parse_event_cursor,
    )


def _serialize_global_workflow_draft_review(review) -> AgentGlobalWorkflowDraftReviewResponse:
    return AgentGlobalWorkflowDraftReviewResponse(
        conversation_id=review.conversation_id,
        product_id=review.product_id,
        product_name=review.product_name,
        product_conversation_id=review.product_conversation_id,
        workflow_draft_id=review.workflow_draft_id,
        draft=serialize_workflow_draft(review.draft),
    )


__all__ = ["router"]
