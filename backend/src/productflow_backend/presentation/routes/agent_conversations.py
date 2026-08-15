from __future__ import annotations

from collections.abc import AsyncIterator

from fastapi import APIRouter, Depends, Header, Query, status
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
    create_agent_conversation,
    get_agent_conversation_or_raise,
    get_agent_turn_or_raise,
    list_agent_turn_page,
)
from productflow_backend.domain.enums import AgentTurnStatus
from productflow_backend.domain.errors import AgentServiceUnavailableError, BusinessValidationError, ConflictError
from productflow_backend.infrastructure.agent_service import (
    AgentServiceClient,
    AgentServiceRequestError,
    get_agent_service_client,
)
from productflow_backend.infrastructure.queue import enqueue_agent_turn_sync
from productflow_backend.presentation.deps import get_session, require_admin
from productflow_backend.presentation.schemas.agent_conversations import (
    AgentConversationResponse,
    AgentQuestionAnswerRequest,
    AgentTurnPageResponse,
    AgentTurnResponse,
    CreateAgentConversationRequest,
    StartAgentTurnRequest,
    SubmitAgentTurnResponse,
    serialize_agent_conversation,
    serialize_agent_turn,
)

router = APIRouter(
    prefix="/api/v2/products/{product_id}/agent-conversations",
    tags=["agent-conversations"],
    dependencies=[Depends(require_admin)],
)

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


@router.get("/{conversation_id}/turns", response_model=AgentTurnPageResponse)
def list_agent_turns_endpoint(
    product_id: str,
    conversation_id: str,
    after: str = Query(default="", max_length=4096),
    limit: int = Query(default=AGENT_TURN_DEFAULT_PAGE_SIZE, ge=1, le=AGENT_TURN_MAX_PAGE_SIZE),
    session: Session = Depends(get_session),
) -> AgentTurnPageResponse:
    page = list_agent_turn_page(
        session,
        product_id=product_id,
        conversation_id=conversation_id,
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
    if (
        projection.status in _REFRESHABLE_AGENT_TURN_STATUSES
        and projection.sync_error is None
    ) or (
        projection.status == AgentTurnStatus.AWAITING_CONFIRMATION
        and projection.workflow_draft_revision_id is None
    ):
        projection = refresh_agent_turn(
            session,
            product_id=product_id,
            conversation_id=conversation_id,
            projection_id=projection_id,
            gateway=_agent_gateway_or_raise(),
        )
    return serialize_agent_turn(projection)


@router.post("/{conversation_id}/turns/{projection_id}/cancel", response_model=AgentTurnResponse)
def cancel_agent_turn_endpoint(
    product_id: str,
    conversation_id: str,
    projection_id: str,
    session: Session = Depends(get_session),
) -> AgentTurnResponse:
    return serialize_agent_turn(
        control_agent_turn(
            session,
            product_id=product_id,
            conversation_id=conversation_id,
            projection_id=projection_id,
            command="cancel",
            gateway=_agent_gateway_or_raise(),
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
    return serialize_agent_turn(
        control_agent_turn(
            session,
            product_id=product_id,
            conversation_id=conversation_id,
            projection_id=projection_id,
            command="resume",
            gateway=_agent_gateway_or_raise(),
            enqueue_sync=enqueue_agent_turn_sync,
        )
    )


@router.post(
    "/{conversation_id}/turns/{projection_id}/questions/{question_id}/answer",
    response_model=AgentTurnResponse,
)
def answer_agent_question_endpoint(
    product_id: str,
    conversation_id: str,
    projection_id: str,
    question_id: str,
    payload: AgentQuestionAnswerRequest,
    session: Session = Depends(get_session),
) -> AgentTurnResponse:
    return serialize_agent_turn(
        answer_agent_question(
            session,
            product_id=product_id,
            conversation_id=conversation_id,
            projection_id=projection_id,
            question_id=question_id,
            answer=payload.to_gateway_payload(),
            gateway=_agent_gateway_or_raise(),
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
    if projection.harness_turn_id is None:
        raise ConflictError("Agent turn projection 尚未绑定 harness Turn")
    cursor = max(after, _parse_event_cursor(last_event_id))
    gateway = _agent_gateway_or_raise()
    try:
        gateway.get_turn(
            conversation_id=conversation_id,
            turn_id=projection.harness_turn_id,
        )
    except AgentServiceRequestError as exc:
        if exc.status_code in {404, 409}:
            raise ConflictError("Agent Turn 当前不可订阅") from exc
        raise AgentServiceUnavailableError("Agent 服务暂时不可用") from exc

    async def upstream_events() -> AsyncIterator[bytes]:
        try:
            async for chunk in gateway.stream_turn_events(
                conversation_id=conversation_id,
                turn_id=projection.harness_turn_id or "",
                after=cursor,
            ):
                yield chunk
        except AgentServiceRequestError:
            return

    return StreamingResponse(
        upstream_events(),
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
    if cursor < 0:
        raise BusinessValidationError("Last-Event-ID 无效")
    return cursor


def _agent_gateway_or_raise() -> AgentServiceClient:
    try:
        return get_agent_service_client()
    except AgentServiceRequestError as exc:
        raise AgentServiceUnavailableError("Agent 服务尚未配置或暂时不可用") from exc
