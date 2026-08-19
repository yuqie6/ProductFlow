from __future__ import annotations

import hashlib
import logging
from collections.abc import Callable
from dataclasses import dataclass
from typing import Any, Literal

from sqlalchemy import select
from sqlalchemy.orm import Session, selectinload

from productflow_backend.application.agent_conversations import (
    attach_agent_workflow_draft_artifact,
    bind_harness_turn,
    cancel_unbound_agent_turn,
    get_agent_conversation_or_raise,
    get_agent_turn_or_raise,
    lock_agent_turn_or_raise,
    project_agent_turn_state,
    record_agent_turn_start_error,
    reserve_agent_turn,
    set_agent_turn_resume_required,
)
from productflow_backend.application.agent_workflow_run_requests import (
    attach_agent_workflow_run_request,
    get_agent_workflow_run_request_by_source_step,
)
from productflow_backend.application.global_agent_draft_contracts import GLOBAL_AGENT_DRAFT_ARTIFACT_NAME
from productflow_backend.application.global_agent_drafts import attach_agent_global_draft_artifact
from productflow_backend.application.media_library.drafts import (
    LIBRARY_ORGANIZATION_DRAFT_ARTIFACT_NAME,
    append_library_organization_draft_revision,
)
from productflow_backend.application.time import now_utc
from productflow_backend.domain.enums import (
    AgentConversationScope,
    AgentExecutionPhase,
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
    AgentTurnExecution,
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
    gateway: AgentServiceClient,
    enqueue_sync: Callable[[Session, str], None],
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
        try:
            state = gateway.start_turn(
                conversation_id=conversation_id,
                task_id=projection.task_id,
                input_text=projection.input_text,
                asset_ids=list(projection.input_asset_ids_json),
                idempotency_key=projection.id,
                page_context=_agent_page_context_payload(projection),
            )
            conversation = get_agent_conversation_or_raise(
                session,
                product_id=product_id,
                conversation_id=conversation_id,
            )
            _validate_agent_state_scope(_expected_harness_run_id(conversation, projection), state)
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
) -> AgentTurnProjection:
    projection = get_agent_turn_or_raise(
        session,
        product_id=product_id,
        conversation_id=conversation_id,
        projection_id=projection_id,
    )
    if projection.harness_turn_id is None:
        raise ConflictError("Agent Turn 尚未绑定 runtime Turn")
    try:
        state = gateway.get_turn(
            conversation_id=conversation_id,
            turn_id=projection.harness_turn_id,
            task_id=projection.task_id,
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
    gateway: AgentServiceClient,
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
        if projection.harness_turn_id is not None:
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
    return status in {
        AgentTurnStatus.AWAITING_CONFIRMATION,
        AgentTurnStatus.SUCCEEDED,
        AgentTurnStatus.FAILED,
        AgentTurnStatus.CANCELED,
        AgentTurnStatus.UNKNOWN,
    }


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
    _validate_agent_state_scope(_expected_harness_run_id(conversation, projection), state)
    _validate_agent_execution_fence(session, projection=projection, state=state)
    if _is_stale_queued_execution_snapshot(session, projection=projection, state=state):
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
            [tool_step.model_dump(mode="json") for tool_step in state.tool_steps]
            if state.tool_steps is not None
            else None
        ),
        finished_at=state.finished_at,
        commit=commit,
    )
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
            if conversation.workflow_draft_id is None or product_id is None:
                raise ConflictError("商品工作流 Agent Turn 缺少 WorkflowDraft artifact 作用域")
            projection = attach_agent_workflow_draft_artifact(
                session,
                product_id=product_id,
                conversation_id=conversation_id,
                projection_id=projection.id,
                harness_turn_id=state.turn_id,
                artifact_name=state.artifact.name,
                artifact_step_id=state.artifact.step_id,
                artifact_value=state.artifact.value,
                commit=commit,
            )
        if pending_workflow_run_request is not None:
            projection = attach_agent_workflow_run_request(
                session,
                product_id=product_id,
                conversation_id=conversation_id,
                projection_id=projection.id,
                request_id=pending_workflow_run_request.id,
                commit=commit,
            )
    elif state.artifact is not None:
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
            idempotency_key=projection.id,
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
    _validate_agent_state_scope(_expected_harness_run_id(conversation, projection), state)
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


def _validate_agent_execution_fence(
    session: Session,
    *,
    projection: AgentTurnProjection,
    state: AgentServiceTurnState,
) -> None:
    execution = session.scalar(
        select(AgentTurnExecution).where(AgentTurnExecution.turn_projection_id == projection.id)
    )
    if execution is None:
        if state.execution_attempt is not None or state.execution_fencing_token is not None:
            raise ConflictError("Agent Turn 返回了不存在的 execution lease")
        return
    if execution.phase == AgentExecutionPhase.TERMINAL and state.status not in {
        AgentTurnStatus.SUCCEEDED,
        AgentTurnStatus.FAILED,
        AgentTurnStatus.CANCELED,
        AgentTurnStatus.UNKNOWN,
        AgentTurnStatus.AWAITING_CONFIRMATION,
    }:
        raise ConflictError("Agent execution 已进入终态，不能回写活动状态")
    if state.execution_attempt is None or state.execution_fencing_token is None:
        if state.status == AgentTurnStatus.QUEUED:
            return
        raise ConflictError("Agent Turn 缺少 execution fencing 信息")
    if (
        state.execution_attempt != execution.attempt
        or state.execution_fencing_token != execution.fencing_token
    ):
        raise ConflictError("Agent Turn execution fencing token 已过期")


def _is_stale_queued_execution_snapshot(
    session: Session,
    *,
    projection: AgentTurnProjection,
    state: AgentServiceTurnState,
) -> bool:
    if state.status != AgentTurnStatus.QUEUED:
        return False
    if state.execution_attempt is not None or state.execution_fencing_token is not None:
        return False
    execution = session.scalar(
        select(AgentTurnExecution).where(AgentTurnExecution.turn_projection_id == projection.id)
    )
    return execution is not None and (
        projection.status != AgentTurnStatus.QUEUED
        or execution.phase != AgentExecutionPhase.CLAIMED
    )


def _validate_agent_state_scope(expected_run_id: str, state: AgentServiceTurnState) -> None:
    if state.run_id != expected_run_id or not state.turn_id:
        raise AgentServiceUnavailableError("Agent 服务返回了作用域不匹配的 Turn")


def _expected_harness_run_id(conversation, projection: AgentTurnProjection) -> str:
    return projection.task.harness_run_id if projection.task is not None else conversation.harness_run_id


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
    "AgentQuestionAnswerResult",
    "AgentTurnSubmission",
    "attach_agent_library_organization_draft_artifact",
    "answer_agent_question",
    "control_agent_turn",
    "refresh_agent_turn",
    "retry_unbound_agent_turn_start",
    "submit_agent_turn",
    "synchronize_agent_turn_state",
]
