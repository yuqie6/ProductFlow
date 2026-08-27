from __future__ import annotations

from collections.abc import Callable
from typing import Literal

from fastapi.responses import StreamingResponse
from sqlalchemy.orm import Session

from productflow_backend.application.agent.control import (
    answer_agent_question as answer_agent_question_use_case,
)
from productflow_backend.application.agent.control import (
    control_agent_turn as control_agent_turn_use_case,
)
from productflow_backend.application.agent.control import (
    refresh_agent_turn as refresh_agent_turn_use_case,
)
from productflow_backend.application.agent.control import (
    submit_agent_turn as submit_agent_turn_use_case,
)
from productflow_backend.application.agent.effect_reconciliation import reconcile_agent_turn_effect
from productflow_backend.application.agent.event_stream import stream_agent_turn_events
from productflow_backend.application.agent.execution import MAX_AGENT_EVENT_SEQUENCE
from productflow_backend.application.agent.graph_tools import canvas_focus_for_turn, canvas_focus_for_turns
from productflow_backend.application.agent.turn_projection import (
    AGENT_TURN_DEFAULT_PAGE_SIZE,
    AGENT_TURN_MAX_PAGE_SIZE,
    get_agent_turn_or_raise,
    list_agent_turn_page,
)
from productflow_backend.application.agent.turn_status import IN_FLIGHT_TURN_STATUSES
from productflow_backend.application.async_delivery import stage_async_dispatch_for_actor
from productflow_backend.domain.enums import AgentTurnStatus
from productflow_backend.domain.errors import AgentServiceUnavailableError, BusinessValidationError
from productflow_backend.infrastructure.agent_service import (
    AgentServiceClient,
    AgentServiceRequestError,
    get_agent_service_client,
)
from productflow_backend.presentation.schemas.agent_conversations import (
    AgentQuestionAnswerRequest,
    AgentQuestionAnswerResponse,
    AgentTurnEffectReconciliationRequest,
    AgentTurnEffectReconciliationResponse,
    AgentTurnPageResponse,
    AgentTurnResponse,
    StartAgentTurnRequest,
    SubmitAgentTurnResponse,
    serialize_agent_turn,
    serialize_agent_turn_effect_reconciliation,
)

AgentTurnEnqueue = Callable[[Session, str], None]
AgentGatewayFactory = Callable[[], AgentServiceClient | None]
AgentEventCursorParser = Callable[[str | None], int]


def enqueue_agent_turn_sync(session: Session, projection_id: str) -> None:
    stage_async_dispatch_for_actor(session, "run_agent_turn_sync", projection_id)


def list_agent_turns_http(
    session: Session,
    *,
    product_id: str | None,
    conversation_id: str,
    task_id: str | None,
    after: str,
    limit: int,
) -> AgentTurnPageResponse:
    page = list_agent_turn_page(
        session,
        product_id=product_id,
        conversation_id=conversation_id,
        task_id=task_id,
        after=after,
        limit=limit,
    )
    focus_by_turn = canvas_focus_for_turns(session, list(page.items))
    return AgentTurnPageResponse(
        items=[
            serialize_agent_turn(turn, canvas_focus=focus_by_turn.get(turn.id))
            for turn in page.items
        ],
        next_cursor=page.next_cursor,
    )


def submit_agent_turn_http(
    session: Session,
    *,
    product_id: str | None,
    conversation_id: str,
    payload: StartAgentTurnRequest,
    gateway: AgentServiceClient,
    enqueue_sync: AgentTurnEnqueue,
) -> SubmitAgentTurnResponse:
    submission = submit_agent_turn_use_case(
        session,
        product_id=product_id,
        conversation_id=conversation_id,
        input_text=payload.input_text,
        input_asset_ids=payload.asset_ids,
        idempotency_key=payload.idempotency_key,
        task_id=payload.task_id,
        page_context=payload.page_context.model_dump(mode="json") if payload.page_context is not None else None,
        gateway=gateway,
        enqueue_sync=enqueue_sync,
        defer_if_unavailable=True,
    )
    return SubmitAgentTurnResponse(
        created=submission.created,
        turn=serialize_agent_turn(submission.projection),
    )


def get_agent_turn_http(
    session: Session,
    *,
    product_id: str | None,
    conversation_id: str,
    projection_id: str,
    gateway_or_none: AgentGatewayFactory,
    require_library_revision_clear: bool,
) -> AgentTurnResponse:
    projection = get_agent_turn_or_raise(
        session,
        product_id=product_id,
        conversation_id=conversation_id,
        projection_id=projection_id,
    )
    if _agent_turn_needs_refresh(
        projection,
        require_library_revision_clear=require_library_revision_clear,
    ):
        gateway = gateway_or_none()
        if gateway is not None:
            projection = refresh_agent_turn_use_case(
                session,
                product_id=product_id,
                conversation_id=conversation_id,
                projection_id=projection_id,
                gateway=gateway,
                tolerate_transient_unavailable=True,
            )
    return serialize_agent_turn(
        projection,
        canvas_focus=canvas_focus_for_turn(session, projection),
    )


def control_agent_turn_http(
    session: Session,
    *,
    product_id: str | None,
    conversation_id: str,
    projection_id: str,
    command: Literal["cancel", "resume"],
    gateway_or_raise: Callable[[], AgentServiceClient],
    enqueue_sync: AgentTurnEnqueue,
) -> AgentTurnResponse:
    projection = get_agent_turn_or_raise(
        session,
        product_id=product_id,
        conversation_id=conversation_id,
        projection_id=projection_id,
    )
    return serialize_agent_turn(
        control_agent_turn_use_case(
            session,
            product_id=product_id,
            conversation_id=conversation_id,
            projection_id=projection_id,
            command=command,
            gateway=None if projection.harness_turn_id is None else gateway_or_raise(),
            enqueue_sync=enqueue_sync,
        )
    )


def answer_agent_question_http(
    session: Session,
    *,
    product_id: str | None,
    conversation_id: str,
    projection_id: str,
    question_id: str,
    payload: AgentQuestionAnswerRequest,
    gateway_or_none: AgentGatewayFactory,
    enqueue_sync: AgentTurnEnqueue,
) -> AgentQuestionAnswerResponse:
    result = answer_agent_question_use_case(
        session,
        product_id=product_id,
        conversation_id=conversation_id,
        projection_id=projection_id,
        question_id=question_id,
        answer=payload.to_gateway_payload(),
        gateway=gateway_or_none(),
        enqueue_sync=enqueue_sync,
    )
    answered_turn = serialize_agent_turn(result.answered_turn)
    return AgentQuestionAnswerResponse(
        **answered_turn.model_dump(),
        answered_turn=answered_turn,
        continuation_turn=serialize_agent_turn(result.continuation_turn),
    )


def reconcile_agent_turn_effect_http(
    session: Session,
    *,
    product_id: str | None,
    conversation_id: str,
    projection_id: str,
    payload: AgentTurnEffectReconciliationRequest,
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


def stream_agent_turn_events_http(
    session: Session,
    *,
    product_id: str | None,
    conversation_id: str,
    projection_id: str,
    after: int,
    last_event_id: str | None,
    parse_event_cursor: AgentEventCursorParser,
) -> StreamingResponse:
    projection = get_agent_turn_or_raise(
        session,
        product_id=product_id,
        conversation_id=conversation_id,
        projection_id=projection_id,
    )
    cursor = max(after, parse_event_cursor(last_event_id))
    return StreamingResponse(
        stream_agent_turn_events(projection_id=projection.id, after=cursor),
        media_type="text/event-stream",
        headers={
            "Cache-Control": "no-cache, no-transform",
            "Connection": "keep-alive",
            "X-Accel-Buffering": "no",
        },
    )


def _agent_turn_needs_refresh(
    projection,
    *,
    require_library_revision_clear: bool,
) -> bool:
    if projection.status in IN_FLIGHT_TURN_STATUSES:
        return True
    if projection.status != AgentTurnStatus.AWAITING_CONFIRMATION:
        return False
    return not require_library_revision_clear or projection.library_organization_draft_revision_id is None


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


__all__ = [
    "AGENT_TURN_DEFAULT_PAGE_SIZE",
    "AGENT_TURN_MAX_PAGE_SIZE",
    "AgentTurnEnqueue",
    "answer_agent_question_http",
    "control_agent_turn_http",
    "enqueue_agent_turn_sync",
    "get_agent_turn_http",
    "list_agent_turns_http",
    "reconcile_agent_turn_effect_http",
    "stream_agent_turn_events_http",
    "submit_agent_turn_http",
]
