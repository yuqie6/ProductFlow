from __future__ import annotations

from fastapi import APIRouter, Depends, Header, Query
from fastapi.responses import StreamingResponse
from sqlalchemy.orm import Session

from productflow_backend.application.agent_control import (
    answer_agent_question,
    control_agent_turn,
    refresh_agent_turn,
    submit_agent_turn,
)
from productflow_backend.application.agent_conversations import (
    AGENT_TURN_DEFAULT_PAGE_SIZE,
    AGENT_TURN_MAX_PAGE_SIZE,
    get_agent_turn_or_raise,
    list_agent_turn_page,
)
from productflow_backend.application.agent_effect_reconciliation import reconcile_agent_turn_effect
from productflow_backend.application.agent_event_stream import stream_agent_turn_events
from productflow_backend.application.agent_workflow_run_requests import (
    cancel_agent_workflow_run_request,
    confirm_agent_workflow_run_request,
    get_agent_workflow_run_request,
)
from productflow_backend.application.async_delivery import stage_async_dispatch_for_actor
from productflow_backend.application.global_agent_drafts import (
    confirm_global_workflow_draft_review,
    get_global_workflow_draft_review,
)
from productflow_backend.application.media_library.drafts import (
    confirm_library_organization_draft_revision,
    get_library_organization_draft_or_raise,
)
from productflow_backend.domain.enums import AgentTurnStatus
from productflow_backend.domain.errors import AgentServiceUnavailableError
from productflow_backend.infrastructure.agent_service import (
    AgentServiceClient,
    AgentServiceRequestError,
    get_agent_service_client,
)
from productflow_backend.presentation.deps import get_session, require_admin
from productflow_backend.presentation.routes.agent_conversations import _parse_event_cursor
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
    serialize_agent_turn,
    serialize_agent_turn_effect_reconciliation,
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


def enqueue_global_agent_turn_sync(session: Session, projection_id: str) -> None:
    stage_async_dispatch_for_actor(session, "run_agent_turn_sync", projection_id)


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


_REFRESHABLE_AGENT_TURN_STATUSES = {
    AgentTurnStatus.QUEUED,
    AgentTurnStatus.RUNNING,
    AgentTurnStatus.CANCEL_REQUESTED,
}


@router.get("/{conversation_id}/turns", response_model=AgentTurnPageResponse)
def list_global_agent_turns_endpoint(
    conversation_id: str,
    task_id: str | None = Query(default=None, max_length=64),
    after: str = Query(default="", max_length=4096),
    limit: int = Query(default=AGENT_TURN_DEFAULT_PAGE_SIZE, ge=1, le=AGENT_TURN_MAX_PAGE_SIZE),
    session: Session = Depends(get_session),
) -> AgentTurnPageResponse:
    page = list_agent_turn_page(
        session,
        product_id=None,
        conversation_id=conversation_id,
        task_id=task_id,
        after=after,
        limit=limit,
    )
    return AgentTurnPageResponse(
        items=[serialize_agent_turn(turn) for turn in page.items],
        next_cursor=page.next_cursor,
    )


@router.post("/{conversation_id}/turns", response_model=SubmitAgentTurnResponse, status_code=202)
def submit_global_agent_turn_endpoint(
    conversation_id: str,
    payload: StartAgentTurnRequest,
    session: Session = Depends(get_session),
) -> SubmitAgentTurnResponse:
    submission = submit_agent_turn(
        session,
        product_id=None,
        conversation_id=conversation_id,
        input_text=payload.input_text,
        input_asset_ids=payload.asset_ids,
        idempotency_key=payload.idempotency_key,
        task_id=payload.task_id,
        page_context=payload.page_context.model_dump(mode="json") if payload.page_context is not None else None,
        gateway=_agent_gateway_or_raise(),
        enqueue_sync=enqueue_global_agent_turn_sync,
    )
    return SubmitAgentTurnResponse(created=submission.created, turn=serialize_agent_turn(submission.projection))


@router.get("/{conversation_id}/turns/{projection_id}", response_model=AgentTurnResponse)
def get_global_agent_turn_endpoint(
    conversation_id: str,
    projection_id: str,
    session: Session = Depends(get_session),
) -> AgentTurnResponse:
    projection = get_agent_turn_or_raise(
        session,
        product_id=None,
        conversation_id=conversation_id,
        projection_id=projection_id,
    )
    if projection.status in _REFRESHABLE_AGENT_TURN_STATUSES or (
        projection.status == AgentTurnStatus.AWAITING_CONFIRMATION
        and projection.workflow_draft_revision_id is None
        and projection.library_organization_draft_revision_id is None
    ):
        gateway = _agent_gateway_or_none()
        if gateway is not None:
            projection = refresh_agent_turn(
                session,
                product_id=None,
                conversation_id=conversation_id,
                projection_id=projection_id,
                gateway=gateway,
                tolerate_transient_unavailable=True,
            )
    return serialize_agent_turn(projection)


@router.post("/{conversation_id}/turns/{projection_id}/cancel", response_model=AgentTurnResponse)
def cancel_global_agent_turn_endpoint(
    conversation_id: str,
    projection_id: str,
    session: Session = Depends(get_session),
) -> AgentTurnResponse:
    projection = get_agent_turn_or_raise(
        session,
        product_id=None,
        conversation_id=conversation_id,
        projection_id=projection_id,
    )
    return serialize_agent_turn(
        control_agent_turn(
            session,
            product_id=None,
            conversation_id=conversation_id,
            projection_id=projection_id,
            command="cancel",
            gateway=None if projection.harness_turn_id is None else _agent_gateway_or_raise(),
            enqueue_sync=enqueue_global_agent_turn_sync,
        )
    )


@router.post("/{conversation_id}/turns/{projection_id}/resume", response_model=AgentTurnResponse)
def resume_global_agent_turn_endpoint(
    conversation_id: str,
    projection_id: str,
    session: Session = Depends(get_session),
) -> AgentTurnResponse:
    projection = get_agent_turn_or_raise(
        session,
        product_id=None,
        conversation_id=conversation_id,
        projection_id=projection_id,
    )
    return serialize_agent_turn(
        control_agent_turn(
            session,
            product_id=None,
            conversation_id=conversation_id,
            projection_id=projection_id,
            command="resume",
            gateway=None if projection.harness_turn_id is None else _agent_gateway_or_raise(),
            enqueue_sync=enqueue_global_agent_turn_sync,
        )
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
    result = answer_agent_question(
        session,
        product_id=None,
        conversation_id=conversation_id,
        projection_id=projection_id,
        question_id=question_id,
        answer=payload.to_gateway_payload(),
        gateway=_agent_gateway_or_none(),
        enqueue_sync=enqueue_global_agent_turn_sync,
    )
    answered_turn = serialize_agent_turn(result.answered_turn)
    return AgentQuestionAnswerResponse(
        **answered_turn.model_dump(),
        answered_turn=answered_turn,
        continuation_turn=serialize_agent_turn(result.continuation_turn),
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
    return serialize_agent_turn_effect_reconciliation(
        reconcile_agent_turn_effect(
            session,
            product_id=None,
            conversation_id=conversation_id,
            projection_id=projection_id,
            tool_call_id=payload.tool_call_id,
        )
    )


@router.get("/{conversation_id}/turns/{projection_id}/events")
async def stream_global_agent_turn_events_endpoint(
    conversation_id: str,
    projection_id: str,
    after: int = Query(default=0, ge=0),
    last_event_id: str | None = Header(default=None, alias="Last-Event-ID"),
    session: Session = Depends(get_session),
) -> StreamingResponse:
    projection = get_agent_turn_or_raise(
        session,
        product_id=None,
        conversation_id=conversation_id,
        projection_id=projection_id,
    )
    cursor = max(after, _parse_event_cursor(last_event_id))
    return StreamingResponse(
        stream_agent_turn_events(projection_id=projection.id, after=cursor),
        media_type="text/event-stream",
        headers={
            "Cache-Control": "no-cache, no-transform",
            "Connection": "keep-alive",
            "X-Accel-Buffering": "no",
        },
    )


def _agent_gateway_or_raise() -> AgentServiceClient:
    try:
        return get_agent_service_client()
    except AgentServiceRequestError as exc:
        raise AgentServiceUnavailableError("Agent 服务尚未配置或暂时不可用") from exc


def _agent_gateway_or_none() -> AgentServiceClient | None:
    try:
        return get_agent_service_client()
    except AgentServiceRequestError:
        return None


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
