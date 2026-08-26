from __future__ import annotations

import hashlib
import logging
from collections.abc import Callable
from dataclasses import dataclass
from typing import Any, Literal

from sqlalchemy import select
from sqlalchemy.orm import Session, selectinload

from productflow_backend.application.agent.conversations import get_agent_conversation_or_raise
from productflow_backend.application.agent.execution import (
    is_stale_queued_execution_snapshot,
    validate_agent_execution_fence,
)
from productflow_backend.application.agent.global_draft_contracts import GLOBAL_AGENT_DRAFT_ARTIFACT_NAME
from productflow_backend.application.agent.global_drafts import attach_agent_global_draft_artifact
from productflow_backend.application.agent.turn_projection import (
    bind_harness_turn,
    cancel_unbound_agent_turn,
    expected_harness_run_id,
    get_agent_turn_or_raise,
    is_confirmed_workflow_draft_turn,
    lock_agent_turn_or_raise,
    project_agent_turn_state,
    record_agent_turn_start_error,
    reserve_agent_turn,
    set_agent_turn_resume_required,
)
from productflow_backend.application.agent.turn_status import TERMINAL_TURN_STATUSES
from productflow_backend.application.agent.workflow_run_requests import (
    attach_agent_workflow_run_request,
    get_agent_workflow_run_request_by_source_step,
)
from productflow_backend.application.media_library.drafts import (
    LIBRARY_ORGANIZATION_DRAFT_ARTIFACT_NAME,
    append_library_organization_draft_revision,
)
from productflow_backend.application.time import now_utc
from productflow_backend.domain.enums import (
    AgentConversationScope,
    AgentToolStepKind,
    AgentToolStepStatus,
    AgentTurnStatus,
)
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
from productflow_backend.infrastructure.db.models import (
    AgentTurnProjection,
    LibraryOrganizationDraft,
    LibraryOrganizationDraftRevision,
)

AgentControlCommand = Literal["cancel", "resume"]
logger = logging.getLogger(__name__)


@dataclass(frozen=True, slots=True)
class AgentTurnSubmission:
    projection: AgentTurnProjection
    created: bool


@dataclass(frozen=True, slots=True)
class AgentQuestionAnswerResult:
    answered_turn: AgentTurnProjection
    continuation_turn: AgentTurnProjection


def submit_agent_turn(
    session: Session,
    *,
    product_id: str | None,
    conversation_id: str,
    input_text: str,
    input_asset_ids: list[str],
    idempotency_key: str,
    task_id: str | None = None,
    page_context: dict[str, Any] | None = None,
    gateway: AgentServiceClient | None,
    enqueue_sync: Callable[[Session, str], None],
    defer_if_unavailable: bool = False,
) -> AgentTurnSubmission:
    reservation = reserve_agent_turn(
        session,
        product_id=product_id,
        conversation_id=conversation_id,
        input_text=input_text,
        input_asset_ids=input_asset_ids,
        idempotency_key=idempotency_key,
        task_id=task_id,
        page_context=page_context,
    )
    projection = reservation.projection
    if projection.harness_turn_id is None:
        if gateway is None:
            if not defer_if_unavailable:
                raise AgentServiceUnavailableError("Agent 服务暂时不可用")
            record_agent_turn_start_error(
                session,
                product_id=product_id,
                conversation_id=conversation_id,
                projection_id=projection.id,
                safe_error="Agent 服务暂时不可用",
            )
        else:
            try:
                state = gateway.start_turn(
                    conversation_id=conversation_id,
                    task_id=projection.task_id,
                    input_text=projection.input_text,
                    asset_ids=list(projection.input_asset_ids_json),
                    idempotency_key=projection.idempotency_key,
                    page_context=_agent_page_context_payload(projection),
                )
                conversation = get_agent_conversation_or_raise(
                    session,
                    product_id=product_id,
                    conversation_id=conversation_id,
                )
                _validate_agent_state_scope(expected_harness_run_id(conversation, projection), state)
                projection = bind_harness_turn(
                    session,
                    product_id=product_id,
                    conversation_id=conversation_id,
                    projection_id=projection.id,
                    harness_turn_id=state.turn_id,
                    status=state.status,
                    commit=False,
                )
                projection = synchronize_agent_turn_state(
                    session,
                    product_id=product_id,
                    conversation_id=conversation_id,
                    projection_id=projection.id,
                    state=state,
                    commit=False,
                )
            except AgentServiceRequestError as exc:
                record_agent_turn_start_error(
                    session,
                    product_id=product_id,
                    conversation_id=conversation_id,
                    projection_id=projection.id,
                    safe_error=_safe_agent_sync_error(exc),
                )
                if not defer_if_unavailable or not _is_transient_agent_service_error(exc):
                    _raise_agent_service_business_error(exc)

    try:
        enqueue_sync(session, projection.id)
        session.commit()
    except Exception as exc:  # noqa: BLE001
        session.rollback()
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
    product_id: str | None,
    conversation_id: str,
    projection_id: str,
    gateway: AgentServiceClient,
    tolerate_transient_unavailable: bool = False,
) -> AgentTurnProjection:
    projection = get_agent_turn_or_raise(
        session,
        product_id=product_id,
        conversation_id=conversation_id,
        projection_id=projection_id,
    )
    if projection.harness_turn_id is None:
        return projection
    try:
        state = gateway.get_turn(
            conversation_id=conversation_id,
            turn_id=projection.harness_turn_id,
            task_id=projection.task_id,
        )
    except AgentServiceRequestError as exc:
        if tolerate_transient_unavailable and _is_transient_agent_service_error(exc):
            record_agent_turn_start_error(
                session,
                product_id=product_id,
                conversation_id=conversation_id,
                projection_id=projection.id,
                safe_error=_safe_agent_sync_error(exc),
            )
            return get_agent_turn_or_raise(
                session,
                product_id=product_id,
                conversation_id=conversation_id,
                projection_id=projection.id,
            )
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
    product_id: str | None,
    conversation_id: str,
    projection_id: str,
    command: AgentControlCommand,
    gateway: AgentServiceClient | None,
    enqueue_sync: Callable[[Session, str], None],
) -> AgentTurnProjection:
    projection = get_agent_turn_or_raise(
        session,
        product_id=product_id,
        conversation_id=conversation_id,
        projection_id=projection_id,
    )
    if projection.harness_turn_id is None:
        if command == "cancel":
            return cancel_unbound_agent_turn(
                session,
                product_id=product_id,
                conversation_id=conversation_id,
                projection_id=projection.id,
            )
        raise ConflictError("Agent Turn 尚未绑定 runtime Turn")
    if gateway is None:
        raise AgentServiceUnavailableError("Agent 服务暂时不可用")
    try:
        if command == "cancel":
            state = gateway.cancel_turn(
                conversation_id=conversation_id,
                turn_id=projection.harness_turn_id,
                task_id=projection.task_id,
            )
        else:
            state = gateway.resume_turn(
                conversation_id=conversation_id,
                turn_id=projection.harness_turn_id,
                task_id=projection.task_id,
            )
    except AgentServiceRequestError as exc:
        _raise_agent_service_business_error(exc)
    projection = synchronize_agent_turn_state(
        session,
        product_id=product_id,
        conversation_id=conversation_id,
        projection_id=projection.id,
        state=state,
        commit=False,
    )
    projection = set_agent_turn_resume_required(
        session,
        product_id=product_id,
        conversation_id=conversation_id,
        projection_id=projection.id,
        required=False,
        commit=False,
    )
    try:
        enqueue_sync(session, projection.id)
        session.commit()
    except Exception as exc:  # noqa: BLE001
        session.rollback()
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
    product_id: str | None,
    conversation_id: str,
    projection_id: str,
    question_id: str,
    answer: dict[str, Any],
    gateway: AgentServiceClient | None,
    enqueue_sync: Callable[[Session, str], None],
) -> AgentQuestionAnswerResult:
    projection = lock_agent_turn_or_raise(
        session,
        product_id=product_id,
        conversation_id=conversation_id,
        projection_id=projection_id,
    )
    continuation_key = _question_continuation_key(projection.id, question_id)
    continuation = None
    created_continuation = False
    question: dict[str, Any] | None = None
    if projection.continuation_turn_id is not None:
        continuation = session.get(AgentTurnProjection, projection.continuation_turn_id)
        if continuation is None:
            raise ConflictError("Agent question continuation Turn 不存在")
        if projection.question_json is None or projection.question_json.get("id") != question_id:
            raise ConflictError("当前 Agent 问题不存在或已经过期")
        if projection.question_answer_json != answer:
            raise ConflictError("当前问题已经使用其他答案创建 continuation Turn")
    else:
        if projection.status != AgentTurnStatus.REQUIRES_INPUT:
            raise ConflictError("当前 Agent Turn 没有可回答的问题")
        question = _validate_question_for_answer(projection.question_json, question_id, answer)
        continuation_input = _question_continuation_input(question, answer)
        reservation = reserve_agent_turn(
            session,
            product_id=product_id,
            conversation_id=conversation_id,
            input_text=continuation_input,
            input_asset_ids=list(projection.input_asset_ids_json),
            idempotency_key=continuation_key,
            task_id=projection.task_id,
            ignore_turn_id=projection.id,
        )
        continuation = reservation.projection
        created_continuation = reservation.created
        projection.question_answer_json = dict(answer)
        projection.continuation_turn_id = continuation.id
        projection.resume_required = False
        projection.sync_error = None
        projection.updated_at = now_utc()
        session.flush()

        # Stop a live waiter when possible. If the Agent process is unavailable,
        # the persisted continuation remains queued and the old wait is left for
        # lease recovery to reconcile.
        if projection.harness_turn_id is not None and gateway is not None:
            try:
                state = gateway.cancel_turn(
                    conversation_id=conversation_id,
                    turn_id=projection.harness_turn_id,
                    task_id=projection.task_id,
                )
                synchronize_agent_turn_state(
                    session,
                    product_id=product_id,
                    conversation_id=conversation_id,
                    projection_id=projection.id,
                    state=state,
                    commit=False,
                )
                projection.question_json = dict(question)
                projection.updated_at = now_utc()
            except AgentServiceRequestError as exc:
                projection.sync_error = (
                    "问题答案已持久化；原等待 Turn 尚未确认取消，等待执行恢复对账"
                    if exc.status_code is None or exc.status_code >= 500
                    else "原问题 Turn 已不可用，继续执行由 continuation Turn 接管"
                )
            except ConflictError:
                # A restarted Agent may return a snapshot fenced by the expired
                # execution. The durable question and continuation remain the
                # ProductFlow authority; stale cancellation state is advisory.
                projection.sync_error = "问题答案已持久化；原等待 Turn 状态已过期，continuation Turn 接管"
        elif projection.harness_turn_id is not None:
            projection.sync_error = "问题答案已持久化；Agent 服务不可用，continuation Turn 等待恢复入队"

    if continuation is None:
        raise ConflictError("Agent question continuation Turn 创建失败")
    if created_continuation or continuation.harness_turn_id is None or not _is_terminal_turn(continuation.status):
        continuation = submit_agent_turn(
            session,
            product_id=product_id,
            conversation_id=conversation_id,
            input_text=continuation.input_text,
            input_asset_ids=list(continuation.input_asset_ids_json),
            idempotency_key=continuation.idempotency_key,
            task_id=continuation.task_id,
            gateway=gateway,
            enqueue_sync=enqueue_sync,
            defer_if_unavailable=True,
        ).projection
    session.refresh(projection)
    session.refresh(continuation)
    return AgentQuestionAnswerResult(
        answered_turn=projection,
        continuation_turn=continuation,
    )


def _question_continuation_key(projection_id: str, question_id: str) -> str:
    digest = hashlib.sha256(question_id.encode("utf-8")).hexdigest()
    return f"question-continuation:{projection_id}:{digest}"


def _validate_question_for_answer(
    question: dict[str, Any] | None,
    question_id: str,
    answer: dict[str, Any],
) -> dict[str, Any]:
    if question is None or question.get("id") != question_id:
        raise ConflictError("当前 Agent 问题不存在或已经过期")
    if "option" in answer:
        option = answer.get("option")
        options = question.get("options")
        if (
            isinstance(option, bool)
            or not isinstance(option, int)
            or not isinstance(options, list)
            or option < 0
            or option >= len(options)
        ):
            raise BusinessValidationError("Agent 问题选项无效")
    elif not isinstance(answer.get("text"), str) or not answer["text"].strip():
        raise BusinessValidationError("Agent 问题文本回答不能为空")
    return question


def _question_continuation_input(question: dict[str, Any], answer: dict[str, Any]) -> str:
    question_text = str(question.get("question", "")).strip()
    if "option" in answer:
        option = int(answer["option"])
        options = question.get("options")
        selected = options[option] if isinstance(options, list) and option < len(options) else None
        label = selected.get("label") if isinstance(selected, dict) else None
        answer_text = f"选择第 {option + 1} 项" + (f"（{label}）" if isinstance(label, str) and label else "")
    else:
        answer_text = str(answer.get("text", "")).strip()
    return (
        "继续当前 Agent 任务。"
        f"针对问题“{question_text}”，用户回答：{answer_text}。"
        "请基于这个回答继续执行，并再次通过 ProductFlow 工具确认业务事实。"
    )


def _is_terminal_turn(status: AgentTurnStatus) -> bool:
    return status in TERMINAL_TURN_STATUSES


def synchronize_agent_turn_state(
    session: Session,
    *,
    product_id: str | None,
    conversation_id: str,
    projection_id: str,
    state: AgentServiceTurnState,
    commit: bool = True,
) -> AgentTurnProjection:
    conversation = get_agent_conversation_or_raise(
        session,
        product_id=product_id,
        conversation_id=conversation_id,
    )
    projection = get_agent_turn_or_raise(
        session,
        product_id=product_id,
        conversation_id=conversation_id,
        projection_id=projection_id,
    )
    _validate_agent_state_scope(expected_harness_run_id(conversation, projection), state)
    validate_agent_execution_fence(session, projection=projection, state=state)
    if is_stale_queued_execution_snapshot(session, projection=projection, state=state):
        return projection
    pending_workflow_run_request = _find_pending_workflow_run_request(
        session,
        conversation=conversation,
        projection=projection,
        state=state,
    )
    effective_status = state.status
    if (
        conversation.scope_type == AgentConversationScope.GLOBAL
        and state.status == AgentTurnStatus.SUCCEEDED
        and state.artifact is not None
    ):
        effective_status = AgentTurnStatus.AWAITING_CONFIRMATION
    elif pending_workflow_run_request is not None and state.status == AgentTurnStatus.SUCCEEDED:
        effective_status = AgentTurnStatus.AWAITING_CONFIRMATION
    stale_confirmed_workflow_turn = (
        effective_status == AgentTurnStatus.AWAITING_CONFIRMATION
        and is_confirmed_workflow_draft_turn(projection)
    )
    projection = project_agent_turn_state(
        session,
        product_id=product_id,
        conversation_id=conversation_id,
        projection_id=projection_id,
        harness_turn_id=state.turn_id,
        status=effective_status,
        output_text=state.output or None,
        error_text=_safe_agent_turn_error(state),
        question_json=state.question.model_dump(mode="json") if state.question is not None else None,
        tool_steps_json=(
            [tool_step.model_dump(mode="json", exclude_none=True) for tool_step in state.tool_steps]
            if state.tool_steps is not None
            else None
        ),
        finished_at=state.finished_at,
        commit=commit,
    )
    if stale_confirmed_workflow_turn:
        effective_status = projection.status
    if effective_status == AgentTurnStatus.AWAITING_CONFIRMATION:
        if state.artifact is None and pending_workflow_run_request is None:
            raise ConflictError("Agent Turn 待确认状态缺少 required artifact")
        if state.artifact is not None and conversation.scope_type == AgentConversationScope.GLOBAL:
            if product_id is not None:
                raise ConflictError("全局 Agent Turn 不应包含商品作用域")
            if state.artifact.name == GLOBAL_AGENT_DRAFT_ARTIFACT_NAME:
                projection = attach_agent_global_draft_artifact(
                    session,
                    conversation_id=conversation_id,
                    projection_id=projection.id,
                    harness_turn_id=state.turn_id,
                    artifact_name=state.artifact.name,
                    artifact_step_id=state.artifact.step_id,
                    artifact_value=state.artifact.value,
                    commit=commit,
                )
            else:
                # Keep already journaled turns from the pre-global-draft
                # contract recoverable during the transition.
                projection = attach_agent_library_organization_draft_artifact(
                    session,
                    conversation_id=conversation_id,
                    projection_id=projection.id,
                    harness_turn_id=state.turn_id,
                    artifact_name=state.artifact.name,
                    artifact_step_id=state.artifact.step_id,
                    artifact_value=state.artifact.value,
                    commit=commit,
                )
        elif state.artifact is not None:
            from productflow_backend.application.workflow_drafts.service import PRODUCT_WORKFLOW_DRAFT_RETIRED

            raise ConflictError(PRODUCT_WORKFLOW_DRAFT_RETIRED)
        if pending_workflow_run_request is not None:
            projection = attach_agent_workflow_run_request(
                session,
                product_id=product_id,
                conversation_id=conversation_id,
                projection_id=projection.id,
                request_id=pending_workflow_run_request.id,
                commit=commit,
            )
    elif state.artifact is not None and not stale_confirmed_workflow_turn:
        raise ConflictError("Agent Turn 在非待确认状态返回了 artifact")
    return projection


def _find_pending_workflow_run_request(
    session: Session,
    *,
    conversation,
    projection: AgentTurnProjection,
    state: AgentServiceTurnState,
):
    if not state.tool_steps:
        return None
    for tool_step in reversed(state.tool_steps):
        if (
            tool_step.kind == AgentToolStepKind.REQUEST_WORKFLOW_RUN
            and tool_step.status == AgentToolStepStatus.SUCCEEDED
        ):
            return get_agent_workflow_run_request_by_source_step(
                session,
                conversation_id=conversation.id,
                task_id=projection.task_id,
                source_step_id=tool_step.step_id,
            )
    return None


def attach_agent_library_organization_draft_artifact(
    session: Session,
    *,
    conversation_id: str,
    projection_id: str,
    harness_turn_id: str,
    artifact_name: str,
    artifact_step_id: str,
    artifact_value: dict[str, Any],
    commit: bool = True,
) -> AgentTurnProjection:
    if artifact_name != LIBRARY_ORGANIZATION_DRAFT_ARTIFACT_NAME:
        raise BusinessValidationError("Agent 返回了不受支持的素材整理 required artifact")
    normalized_step_id = artifact_step_id.strip()
    if not normalized_step_id or len(normalized_step_id) > 120:
        raise BusinessValidationError("Agent artifact step ID 无效")
    conversation = get_agent_conversation_or_raise(
        session,
        product_id=None,
        conversation_id=conversation_id,
    )
    projection = get_agent_turn_or_raise(
        session,
        product_id=None,
        conversation_id=conversation_id,
        projection_id=projection_id,
    )
    if projection.status != AgentTurnStatus.AWAITING_CONFIRMATION:
        raise ConflictError("Agent Turn 尚未进入待确认状态")
    if projection.harness_turn_id not in {None, harness_turn_id}:
        raise ConflictError("Agent turn projection 已绑定其他 harness Turn")
    if projection.workflow_draft_revision_id is not None:
        raise ConflictError("全局 Agent Turn 不能同时绑定 WorkflowDraft revision")

    draft = session.scalar(
        select(LibraryOrganizationDraft)
        .options(selectinload(LibraryOrganizationDraft.current_revision))
        .where(LibraryOrganizationDraft.conversation_id == conversation.id)
        .with_for_update()
    )
    expected_version = draft.current_revision.version if draft is not None and draft.current_revision is not None else 0
    draft = append_library_organization_draft_revision(
        session,
        conversation_id=conversation_id,
        expected_draft_version=expected_version,
        payload=artifact_value,
        source_turn_id=harness_turn_id,
        source_artifact_step_id=normalized_step_id,
        commit=commit,
    )
    revision = session.scalar(
        select(LibraryOrganizationDraftRevision).where(
            LibraryOrganizationDraftRevision.draft_id == draft.id,
            LibraryOrganizationDraftRevision.source_turn_id == harness_turn_id,
            LibraryOrganizationDraftRevision.source_artifact_step_id == normalized_step_id,
        )
    )
    if revision is None:
        raise ConflictError("Agent artifact 未能同步为素材整理 Draft revision")
    if projection.library_organization_draft_revision_id not in {None, revision.id}:
        raise ConflictError("Agent turn projection 已关联其他素材整理 Draft revision")
    projection.harness_turn_id = harness_turn_id
    projection.artifact_name = artifact_name
    projection.artifact_step_id = normalized_step_id
    projection.library_organization_draft_revision_id = revision.id
    projection.sync_error = None
    projection.updated_at = now_utc()
    if commit:
        session.commit()
        session.refresh(projection)
    return projection


def retry_unbound_agent_turn_start(
    session: Session,
    *,
    projection: AgentTurnProjection,
    gateway: AgentServiceClient,
    commit: bool = True,
) -> AgentTurnProjection:
    conversation = projection.conversation
    try:
        state = gateway.start_turn(
            conversation_id=conversation.id,
            task_id=projection.task_id,
            input_text=projection.input_text,
            asset_ids=list(projection.input_asset_ids_json),
            idempotency_key=projection.idempotency_key,
            page_context=_agent_page_context_payload(projection),
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
    _validate_agent_state_scope(expected_harness_run_id(conversation, projection), state)
    projection = bind_harness_turn(
        session,
        product_id=conversation.product_id,
        conversation_id=conversation.id,
        projection_id=projection.id,
        harness_turn_id=state.turn_id,
        status=state.status,
        commit=commit,
    )
    return synchronize_agent_turn_state(
        session,
        product_id=conversation.product_id,
        conversation_id=conversation.id,
        projection_id=projection.id,
        state=state,
        commit=commit,
    )


def adopt_queued_agent_turn_start(
    session: Session,
    *,
    projection: AgentTurnProjection,
    gateway: AgentServiceClient,
    commit: bool = True,
) -> AgentTurnProjection:
    """Materialize a safe pre-model Turn on another Agent service instance."""

    conversation = projection.conversation
    if projection.status != AgentTurnStatus.QUEUED or projection.harness_turn_id is None:
        raise ConflictError("只有已绑定且仍处于 queued 的 Agent Turn 可以 handoff")
    previous_turn_id = projection.harness_turn_id
    try:
        state = gateway.start_turn(
            conversation_id=conversation.id,
            task_id=projection.task_id,
            input_text=projection.input_text,
            asset_ids=list(projection.input_asset_ids_json),
            idempotency_key=projection.idempotency_key,
            page_context=_agent_page_context_payload(projection),
            turn_id=previous_turn_id,
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
    _validate_agent_state_scope(expected_harness_run_id(conversation, projection), state)
    if state.turn_id != previous_turn_id:
        raise ConflictError("Agent handoff 返回了不同的 harness Turn")
    return synchronize_agent_turn_state(
        session,
        product_id=conversation.product_id,
        conversation_id=conversation.id,
        projection_id=projection.id,
        state=state,
        commit=commit,
    )


def _validate_agent_state_scope(expected_run_id: str, state: AgentServiceTurnState) -> None:
    if state.run_id != expected_run_id or not state.turn_id:
        raise AgentServiceUnavailableError("Agent 服务返回了作用域不匹配的 Turn")


def _agent_page_context_payload(projection: AgentTurnProjection) -> dict[str, Any] | None:
    snapshot = projection.page_context_snapshot
    if snapshot is None:
        return None
    return {
        "snapshot_id": snapshot.id,
        "route": snapshot.route,
        "page_type": snapshot.page_type,
        "product_id": snapshot.product_id,
        "workflow_id": snapshot.workflow_id,
        "selected_asset_ids": list(snapshot.selected_asset_ids_json),
        "visible_asset_ids": list(snapshot.visible_asset_ids_json),
        "filters": dict(snapshot.filters_json),
        "workflow_revision": snapshot.workflow_revision,
        "library_revision": snapshot.library_revision,
        "digest": snapshot.digest,
        "captured_at": snapshot.captured_at.isoformat(),
    }


def _safe_agent_sync_error(exc: AgentServiceRequestError) -> str:
    if exc.status_code is not None and 400 <= exc.status_code < 500:
        return f"Agent 请求被拒绝: {exc.code}"
    return "Agent 服务暂时不可用"


def _is_transient_agent_service_error(exc: AgentServiceRequestError) -> bool:
    return exc.status_code is None or exc.status_code >= 500


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


def cancel_agent_task_run(session: Session, *, task_id: str):
    """Cancel a Task, including its current Turn or workflow-run request."""

    from productflow_backend.application.agent.tasks import cancel_agent_task, get_agent_task_or_raise
    from productflow_backend.application.agent.turn_status import TASK_BLOCKING_TURN_STATUSES
    from productflow_backend.application.agent.workflow_run_requests import cancel_agent_workflow_run_request
    from productflow_backend.application.async_delivery import stage_async_dispatch_for_actor
    from productflow_backend.domain.enums import AgentTaskStatus
    from productflow_backend.infrastructure.agent_service import get_agent_service_client

    task = get_agent_task_or_raise(session, task_id)
    if task.status in {
        AgentTaskStatus.SUCCEEDED,
        AgentTaskStatus.FAILED,
        AgentTaskStatus.CANCELED,
        AgentTaskStatus.UNKNOWN,
    }:
        return task
    projection = None
    if task.current_turn_id is not None and task.conversation_id is not None:
        projection = get_agent_turn_or_raise(
            session,
            product_id=task.product_id,
            conversation_id=task.conversation_id,
            projection_id=task.current_turn_id,
        )
    if (
        projection is not None
        and projection.workflow_run_request_id is not None
        and task.product_id is not None
        and task.conversation_id is not None
    ):
        cancel_agent_workflow_run_request(
            session,
            product_id=task.product_id,
            conversation_id=task.conversation_id,
            request_id=projection.workflow_run_request_id,
        )
        return get_agent_task_or_raise(session, task_id)
    if (
        projection is not None
        and projection.harness_turn_id is not None
        and projection.status in TASK_BLOCKING_TURN_STATUSES
    ):
        control_agent_turn(
            session,
            product_id=task.product_id,
            conversation_id=task.conversation_id,
            projection_id=projection.id,
            command="cancel",
            gateway=get_agent_service_client(),
            enqueue_sync=lambda sess, pid: stage_async_dispatch_for_actor(sess, "run_agent_turn_sync", pid),
        )
        return get_agent_task_or_raise(session, task_id)
    return cancel_agent_task(session, task_id=task_id)


__all__ = [
    "AgentQuestionAnswerResult",
    "AgentTurnSubmission",
    "adopt_queued_agent_turn_start",
    "attach_agent_library_organization_draft_artifact",
    "answer_agent_question",
    "control_agent_turn",
    "cancel_agent_task_run",
    "refresh_agent_turn",
    "retry_unbound_agent_turn_start",
    "submit_agent_turn",
    "synchronize_agent_turn_state",
]
