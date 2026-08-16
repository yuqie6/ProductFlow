from __future__ import annotations

import logging
from collections.abc import Callable
from dataclasses import dataclass
from typing import Any, Literal

from sqlalchemy.orm import Session

from productflow_backend.application.agent_conversations import (
    attach_agent_workflow_draft_artifact,
    bind_harness_turn,
    get_agent_conversation_or_raise,
    get_agent_turn_or_raise,
    project_agent_turn_state,
    record_agent_turn_start_error,
    reserve_agent_turn,
    set_agent_turn_resume_required,
)
from productflow_backend.domain.enums import AgentTurnStatus
from productflow_backend.domain.errors import (
    AgentServiceUnavailableError,
    BusinessValidationError,
    ConflictError,
)
from productflow_backend.infrastructure.agent_service import (
    AgentServiceClient,
    AgentServiceRequestError,
    AgentServiceTurnState,
)
from productflow_backend.infrastructure.db.models import AgentTurnProjection

AgentControlCommand = Literal["cancel", "resume"]
logger = logging.getLogger(__name__)


@dataclass(frozen=True, slots=True)
class AgentTurnSubmission:
    projection: AgentTurnProjection
    created: bool


def submit_agent_turn(
    session: Session,
    *,
    product_id: str,
    conversation_id: str,
    input_text: str,
    input_asset_ids: list[str],
    idempotency_key: str,
    gateway: AgentServiceClient,
    enqueue_sync: Callable[[str], None],
) -> AgentTurnSubmission:
    reservation = reserve_agent_turn(
        session,
        product_id=product_id,
        conversation_id=conversation_id,
        input_text=input_text,
        input_asset_ids=input_asset_ids,
        idempotency_key=idempotency_key,
    )
    projection = reservation.projection
    if projection.harness_turn_id is None:
        try:
            state = gateway.start_turn(
                conversation_id=conversation_id,
                input_text=projection.input_text,
                asset_ids=list(projection.input_asset_ids_json),
                idempotency_key=projection.id,
            )
            conversation = get_agent_conversation_or_raise(
                session,
                product_id=product_id,
                conversation_id=conversation_id,
            )
            _validate_agent_state_scope(conversation.harness_run_id, state)
            projection = bind_harness_turn(
                session,
                product_id=product_id,
                conversation_id=conversation_id,
                projection_id=projection.id,
                harness_turn_id=state.turn_id,
                status=state.status,
            )
            projection = synchronize_agent_turn_state(
                session,
                product_id=product_id,
                conversation_id=conversation_id,
                projection_id=projection.id,
                state=state,
            )
        except AgentServiceRequestError as exc:
            record_agent_turn_start_error(
                session,
                product_id=product_id,
                conversation_id=conversation_id,
                projection_id=projection.id,
                safe_error=_safe_agent_sync_error(exc),
            )
            _raise_agent_service_business_error(exc)

    try:
        enqueue_sync(projection.id)
    except Exception as exc:  # noqa: BLE001
        record_agent_turn_start_error(
            session,
            product_id=product_id,
            conversation_id=conversation_id,
            projection_id=projection.id,
            safe_error="Agent Turn 已创建，但后台同步任务暂时无法入队",
        )
        raise AgentServiceUnavailableError(
            "Agent Turn 已创建，但状态同步暂时不可用；请重试当前请求"
        ) from exc
    return AgentTurnSubmission(projection=projection, created=reservation.created)


def refresh_agent_turn(
    session: Session,
    *,
    product_id: str,
    conversation_id: str,
    projection_id: str,
    gateway: AgentServiceClient,
) -> AgentTurnProjection:
    projection = get_agent_turn_or_raise(
        session,
        product_id=product_id,
        conversation_id=conversation_id,
        projection_id=projection_id,
    )
    if projection.harness_turn_id is None:
        raise ConflictError("Agent turn projection 尚未绑定 harness Turn")
    try:
        state = gateway.get_turn(
            conversation_id=conversation_id,
            turn_id=projection.harness_turn_id,
        )
    except AgentServiceRequestError as exc:
        _raise_agent_service_business_error(exc)
    return synchronize_agent_turn_state(
        session,
        product_id=product_id,
        conversation_id=conversation_id,
        projection_id=projection.id,
        state=state,
    )


def control_agent_turn(
    session: Session,
    *,
    product_id: str,
    conversation_id: str,
    projection_id: str,
    command: AgentControlCommand,
    gateway: AgentServiceClient,
    enqueue_sync: Callable[[str], None],
) -> AgentTurnProjection:
    projection = get_agent_turn_or_raise(
        session,
        product_id=product_id,
        conversation_id=conversation_id,
        projection_id=projection_id,
    )
    if projection.harness_turn_id is None:
        raise ConflictError("Agent turn projection 尚未绑定 harness Turn")
    try:
        if command == "cancel":
            state = gateway.cancel_turn(
                conversation_id=conversation_id,
                turn_id=projection.harness_turn_id,
            )
        else:
            state = gateway.resume_turn(
                conversation_id=conversation_id,
                turn_id=projection.harness_turn_id,
            )
    except AgentServiceRequestError as exc:
        _raise_agent_service_business_error(exc)
    projection = synchronize_agent_turn_state(
        session,
        product_id=product_id,
        conversation_id=conversation_id,
        projection_id=projection.id,
        state=state,
    )
    projection = set_agent_turn_resume_required(
        session,
        product_id=product_id,
        conversation_id=conversation_id,
        projection_id=projection.id,
        required=False,
    )
    try:
        enqueue_sync(projection.id)
    except Exception as exc:  # noqa: BLE001
        record_agent_turn_start_error(
            session,
            product_id=product_id,
            conversation_id=conversation_id,
            projection_id=projection.id,
            safe_error="Agent Turn 状态已更新，但后台同步任务暂时无法入队",
        )
        raise AgentServiceUnavailableError(
            "Agent Turn 状态已更新，但后台同步暂时不可用；请重试当前请求"
        ) from exc
    return projection


def answer_agent_question(
    session: Session,
    *,
    product_id: str,
    conversation_id: str,
    projection_id: str,
    question_id: str,
    answer: dict[str, Any],
    gateway: AgentServiceClient,
) -> AgentTurnProjection:
    projection = get_agent_turn_or_raise(
        session,
        product_id=product_id,
        conversation_id=conversation_id,
        projection_id=projection_id,
    )
    if projection.harness_turn_id is None:
        raise ConflictError("Agent turn projection 尚未绑定 harness Turn")
    try:
        state = gateway.answer_question(
            conversation_id=conversation_id,
            turn_id=projection.harness_turn_id,
            question_id=question_id,
            answer=answer,
        )
    except AgentServiceRequestError as exc:
        _raise_agent_service_business_error(exc)
    projection = synchronize_agent_turn_state(
        session,
        product_id=product_id,
        conversation_id=conversation_id,
        projection_id=projection.id,
        state=state,
    )
    return set_agent_turn_resume_required(
        session,
        product_id=product_id,
        conversation_id=conversation_id,
        projection_id=projection.id,
        required=True,
    )


def synchronize_agent_turn_state(
    session: Session,
    *,
    product_id: str,
    conversation_id: str,
    projection_id: str,
    state: AgentServiceTurnState,
) -> AgentTurnProjection:
    conversation = get_agent_conversation_or_raise(
        session,
        product_id=product_id,
        conversation_id=conversation_id,
    )
    _validate_agent_state_scope(conversation.harness_run_id, state)
    projection = project_agent_turn_state(
        session,
        product_id=product_id,
        conversation_id=conversation_id,
        projection_id=projection_id,
        harness_turn_id=state.turn_id,
        status=state.status,
        output_text=state.output or None,
        error_text=_safe_agent_turn_error(state),
        question_json=state.question.model_dump(mode="json") if state.question is not None else None,
        tool_steps_json=(
            [tool_step.model_dump(mode="json") for tool_step in state.tool_steps]
            if state.tool_steps is not None
            else None
        ),
        finished_at=state.finished_at,
    )
    if state.status == AgentTurnStatus.AWAITING_CONFIRMATION:
        if state.artifact is None:
            raise ConflictError("Agent Turn 待确认状态缺少 required artifact")
        projection = attach_agent_workflow_draft_artifact(
            session,
            product_id=product_id,
            conversation_id=conversation_id,
            projection_id=projection.id,
            harness_turn_id=state.turn_id,
            artifact_name=state.artifact.name,
            artifact_step_id=state.artifact.step_id,
            artifact_value=state.artifact.value,
        )
    elif state.artifact is not None:
        raise ConflictError("Agent Turn 在非待确认状态返回了 required artifact")
    return projection


def retry_unbound_agent_turn_start(
    session: Session,
    *,
    projection: AgentTurnProjection,
    gateway: AgentServiceClient,
) -> AgentTurnProjection:
    conversation = projection.conversation
    try:
        state = gateway.start_turn(
            conversation_id=conversation.id,
            input_text=projection.input_text,
            asset_ids=list(projection.input_asset_ids_json),
            idempotency_key=projection.id,
        )
    except AgentServiceRequestError as exc:
        record_agent_turn_start_error(
            session,
            product_id=conversation.product_id,
            conversation_id=conversation.id,
            projection_id=projection.id,
            safe_error=_safe_agent_sync_error(exc),
        )
        raise
    _validate_agent_state_scope(conversation.harness_run_id, state)
    projection = bind_harness_turn(
        session,
        product_id=conversation.product_id,
        conversation_id=conversation.id,
        projection_id=projection.id,
        harness_turn_id=state.turn_id,
        status=state.status,
    )
    return synchronize_agent_turn_state(
        session,
        product_id=conversation.product_id,
        conversation_id=conversation.id,
        projection_id=projection.id,
        state=state,
    )


def _validate_agent_state_scope(expected_run_id: str, state: AgentServiceTurnState) -> None:
    if state.run_id != expected_run_id or not state.turn_id:
        raise AgentServiceUnavailableError("Agent 服务返回了作用域不匹配的 Turn")


def _safe_agent_sync_error(exc: AgentServiceRequestError) -> str:
    if exc.status_code is not None and 400 <= exc.status_code < 500:
        return f"Agent 请求被拒绝: {exc.code}"
    return "Agent 服务暂时不可用"


def _safe_agent_turn_error(state: AgentServiceTurnState) -> str | None:
    if not state.error:
        return None
    if state.status == AgentTurnStatus.FAILED:
        logger.warning(
            "Agent Turn failed: run_id=%s turn_id=%s",
            state.run_id,
            state.turn_id,
        )
        return "Agent 生成失败，请重试；持续失败请检查 Agent 供应商配置"
    if state.status == AgentTurnStatus.UNKNOWN:
        logger.warning(
            "Agent Turn entered unknown state: run_id=%s turn_id=%s",
            state.run_id,
            state.turn_id,
        )
        return "Agent 执行状态不明确，请稍后重试"
    return state.error


def _raise_agent_service_business_error(exc: AgentServiceRequestError) -> None:
    if exc.status_code == 400:
        raise BusinessValidationError("Agent 服务拒绝了当前请求") from exc
    if exc.status_code in {404}:
        raise ConflictError("Agent 服务中不存在对应 Turn") from exc
    if exc.status_code == 409:
        raise ConflictError("Agent Turn 当前状态与请求冲突") from exc
    raise AgentServiceUnavailableError("Agent 服务暂时不可用") from exc


__all__ = [
    "AgentTurnSubmission",
    "answer_agent_question",
    "control_agent_turn",
    "refresh_agent_turn",
    "retry_unbound_agent_turn_start",
    "submit_agent_turn",
    "synchronize_agent_turn_state",
]
