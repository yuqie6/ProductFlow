from __future__ import annotations

from fastapi import APIRouter, Depends, Header, Query, status
from fastapi.responses import StreamingResponse
from sqlalchemy.orm import Session

from productflow_backend.application.agent.control import (
    answer_agent_question,
    control_agent_turn,
    refresh_agent_turn,
    submit_agent_turn,
)
from productflow_backend.application.agent.conversations import (
    AGENT_TURN_DEFAULT_PAGE_SIZE,
    AGENT_TURN_MAX_PAGE_SIZE,
    create_agent_conversation,
    get_agent_conversation_or_raise,
    get_agent_turn_or_raise,
    list_agent_turn_page,
)
from productflow_backend.application.agent.effect_reconciliation import reconcile_agent_turn_effect
from productflow_backend.application.agent.event_stream import stream_agent_turn_events
from productflow_backend.application.agent.execution import MAX_AGENT_EVENT_SEQUENCE
from productflow_backend.application.agent.workflow_run_requests import (
    cancel_agent_workflow_run_request as cancel_agent_workflow_run_request_use_case,
)
from productflow_backend.application.agent.workflow_run_requests import (
    confirm_agent_workflow_run_request as confirm_agent_workflow_run_request_use_case,
)
from productflow_backend.application.agent.workflow_run_requests import (
    get_agent_workflow_run_request as get_agent_workflow_run_request_use_case,
)
from productflow_backend.application.async_delivery import stage_async_dispatch_for_actor
from productflow_backend.domain.enums import AgentTurnStatus
from productflow_backend.domain.errors import AgentServiceUnavailableError, BusinessValidationError
from productflow_backend.infrastructure.agent_service import (
    AgentServiceClient,
    AgentServiceRequestError,
    get_agent_service_client,
)
from productflow_backend.presentation.deps import get_session, require_admin
from productflow_backend.presentation.schemas.agent_conversations import (
    AgentConversationResponse,
    AgentQuestionAnswerRequest,
    AgentQuestionAnswerResponse,
    AgentTurnEffectReconciliationRequest,
    AgentTurnEffectReconciliationResponse,
    AgentTurnPageResponse,
    AgentTurnResponse,
    AgentWorkflowRunRequestResponse,
    CreateAgentConversationRequest,
    StartAgentTurnRequest,
    SubmitAgentTurnResponse,
    serialize_agent_conversation,
    serialize_agent_turn,
    serialize_agent_turn_effect_reconciliation,
    serialize_agent_workflow_run_request,
)

router = APIRouter(
    prefix="/api/v2/products/{product_id}/agent-conversations",
    tags=["agent-conversations"],
    dependencies=[Depends(require_admin)],
)


def enqueue_agent_turn_sync(session: Session, projection_id: str) -> None:
    stage_async_dispatch_for_actor(session, "run_agent_turn_sync", projection_id)


_REFRESHABLE_AGENT_TURN_STATUSES = {
    AgentTurnStatus.QUEUED,
    AgentTurnStatus.RUNNING,
    AgentTurnStatus.CANCEL_REQUESTED,
}


@router.post("", response_model=AgentConversationResponse, status_code=status.HTTP_201_CREATED)
def create_agent_conversation_endpoint(
    product_id: str,
    payload: CreateAgentConversationRequest,
    session: Session = Depends(get_session),
) -> AgentConversationResponse:
    return serialize_agent_conversation(
        create_agent_conversation(
            session,
            product_id=product_id,
            workflow_draft_id=payload.workflow_draft_id,
        )
    )


@router.get("/{conversation_id}", response_model=AgentConversationResponse)
def get_agent_conversation_endpoint(
    product_id: str,
    conversation_id: str,
    session: Session = Depends(get_session),
) -> AgentConversationResponse:
    return serialize_agent_conversation(
        get_agent_conversation_or_raise(
            session,
            product_id=product_id,
            conversation_id=conversation_id,
        )
    )


@router.get(
    "/{conversation_id}/workflow-run-request",
    response_model=AgentWorkflowRunRequestResponse | None,
)
def get_agent_workflow_run_request_endpoint(
    product_id: str,
    conversation_id: str,
    session: Session = Depends(get_session),
) -> AgentWorkflowRunRequestResponse | None:
    request = get_agent_workflow_run_request_use_case(
        session,
        product_id=product_id,
        conversation_id=conversation_id,
    )
    return serialize_agent_workflow_run_request(request) if request is not None else None


@router.post(
    "/{conversation_id}/workflow-run-request/{request_id}/confirm",
    response_model=AgentWorkflowRunRequestResponse,
)
def confirm_agent_workflow_run_request_endpoint(
    product_id: str,
    conversation_id: str,
    request_id: str,
    session: Session = Depends(get_session),
) -> AgentWorkflowRunRequestResponse:
    return serialize_agent_workflow_run_request(
        confirm_agent_workflow_run_request_use_case(
            session,
            product_id=product_id,
            conversation_id=conversation_id,
            request_id=request_id,
        )
    )


@router.post(
    "/{conversation_id}/workflow-run-request/{request_id}/cancel",
    response_model=AgentWorkflowRunRequestResponse,
)
def cancel_agent_workflow_run_request_endpoint(
    product_id: str,
    conversation_id: str,
    request_id: str,
    session: Session = Depends(get_session),
) -> AgentWorkflowRunRequestResponse:
    return serialize_agent_workflow_run_request(
        cancel_agent_workflow_run_request_use_case(
            session,
            product_id=product_id,
            conversation_id=conversation_id,
            request_id=request_id,
        )
    )


@router.get("/{conversation_id}/turns", response_model=AgentTurnPageResponse)
def list_agent_turns_endpoint(
    product_id: str,
    conversation_id: str,
    task_id: str | None = Query(default=None, max_length=64),
    after: str = Query(default="", max_length=4096),
    limit: int = Query(default=AGENT_TURN_DEFAULT_PAGE_SIZE, ge=1, le=AGENT_TURN_MAX_PAGE_SIZE),
    session: Session = Depends(get_session),
) -> AgentTurnPageResponse:
    page = list_agent_turn_page(
        session,
        product_id=product_id,
        conversation_id=conversation_id,
        task_id=task_id,
        after=after,
        limit=limit,
    )
    return AgentTurnPageResponse(
        items=[serialize_agent_turn(turn) for turn in page.items],
        next_cursor=page.next_cursor,
    )


@router.post(
    "/{conversation_id}/turns",
    response_model=SubmitAgentTurnResponse,
    status_code=status.HTTP_202_ACCEPTED,
)
def submit_agent_turn_endpoint(
    product_id: str,
    conversation_id: str,
    payload: StartAgentTurnRequest,
    session: Session = Depends(get_session),
) -> SubmitAgentTurnResponse:
    submission = submit_agent_turn(
        session,
        product_id=product_id,
        conversation_id=conversation_id,
        input_text=payload.input_text,
        input_asset_ids=payload.asset_ids,
        idempotency_key=payload.idempotency_key,
        task_id=payload.task_id,
        page_context=payload.page_context.model_dump(mode="json") if payload.page_context is not None else None,
        gateway=_agent_gateway_or_raise(),
        enqueue_sync=enqueue_agent_turn_sync,
    )
    return SubmitAgentTurnResponse(
        created=submission.created,
        turn=serialize_agent_turn(submission.projection),
    )


@router.get("/{conversation_id}/turns/{projection_id}", response_model=AgentTurnResponse)
def get_agent_turn_endpoint(
    product_id: str,
    conversation_id: str,
    projection_id: str,
    session: Session = Depends(get_session),
) -> AgentTurnResponse:
    projection = get_agent_turn_or_raise(
        session,
        product_id=product_id,
        conversation_id=conversation_id,
        projection_id=projection_id,
    )
    if projection.status in _REFRESHABLE_AGENT_TURN_STATUSES or (
        projection.status == AgentTurnStatus.AWAITING_CONFIRMATION
        and projection.workflow_draft_revision_id is None
    ):
        gateway = _agent_gateway_or_none()
        if gateway is not None:
            projection = refresh_agent_turn(
                session,
                product_id=product_id,
                conversation_id=conversation_id,
                projection_id=projection_id,
                gateway=gateway,
                tolerate_transient_unavailable=True,
            )
    return serialize_agent_turn(projection)


@router.post("/{conversation_id}/turns/{projection_id}/cancel", response_model=AgentTurnResponse)
def cancel_agent_turn_endpoint(
    product_id: str,
    conversation_id: str,
    projection_id: str,
    session: Session = Depends(get_session),
) -> AgentTurnResponse:
    projection = get_agent_turn_or_raise(
        session,
        product_id=product_id,
        conversation_id=conversation_id,
        projection_id=projection_id,
    )
    return serialize_agent_turn(
        control_agent_turn(
            session,
            product_id=product_id,
            conversation_id=conversation_id,
            projection_id=projection_id,
            command="cancel",
            gateway=None if projection.harness_turn_id is None else _agent_gateway_or_raise(),
            enqueue_sync=enqueue_agent_turn_sync,
        )
    )


@router.post("/{conversation_id}/turns/{projection_id}/resume", response_model=AgentTurnResponse)
def resume_agent_turn_endpoint(
    product_id: str,
    conversation_id: str,
    projection_id: str,
    session: Session = Depends(get_session),
) -> AgentTurnResponse:
    projection = get_agent_turn_or_raise(
        session,
        product_id=product_id,
        conversation_id=conversation_id,
        projection_id=projection_id,
    )
    return serialize_agent_turn(
        control_agent_turn(
            session,
            product_id=product_id,
            conversation_id=conversation_id,
            projection_id=projection_id,
            command="resume",
            gateway=None if projection.harness_turn_id is None else _agent_gateway_or_raise(),
            enqueue_sync=enqueue_agent_turn_sync,
        )
    )


@router.post(
    "/{conversation_id}/turns/{projection_id}/questions/{question_id}/answer",
    response_model=AgentQuestionAnswerResponse,
)
def answer_agent_question_endpoint(
    product_id: str,
    conversation_id: str,
    projection_id: str,
    question_id: str,
    payload: AgentQuestionAnswerRequest,
    session: Session = Depends(get_session),
) -> AgentQuestionAnswerResponse:
    result = answer_agent_question(
        session,
        product_id=product_id,
        conversation_id=conversation_id,
        projection_id=projection_id,
        question_id=question_id,
        answer=payload.to_gateway_payload(),
        gateway=_agent_gateway_or_none(),
        enqueue_sync=enqueue_agent_turn_sync,
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
def reconcile_agent_turn_effect_endpoint(
    product_id: str,
    conversation_id: str,
    projection_id: str,
    payload: AgentTurnEffectReconciliationRequest,
    session: Session = Depends(get_session),
) -> AgentTurnEffectReconciliationResponse:
    return serialize_agent_turn_effect_reconciliation(
        reconcile_agent_turn_effect(
            session,
            product_id=product_id,
            conversation_id=conversation_id,
            projection_id=projection_id,
            tool_call_id=payload.tool_call_id,
        )
    )


@router.get("/{conversation_id}/turns/{projection_id}/events")
async def stream_agent_turn_events_endpoint(
    product_id: str,
    conversation_id: str,
    projection_id: str,
    after: int = Query(default=0, ge=0),
    last_event_id: str | None = Header(default=None, alias="Last-Event-ID"),
    session: Session = Depends(get_session),
) -> StreamingResponse:
    projection = get_agent_turn_or_raise(
        session,
        product_id=product_id,
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


def _parse_event_cursor(value: str | None) -> int:
    if value is None or not value.strip():
        return 0
    try:
        cursor = int(value)
    except ValueError as exc:
        raise BusinessValidationError("Last-Event-ID 无效") from exc
    if cursor < 0 or cursor > MAX_AGENT_EVENT_SEQUENCE:
        raise BusinessValidationError("Last-Event-ID 无效")
    return cursor


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
