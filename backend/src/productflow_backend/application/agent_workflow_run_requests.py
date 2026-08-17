from __future__ import annotations

import hashlib
import json
from dataclasses import dataclass
from typing import Any

from sqlalchemy import select
from sqlalchemy.exc import IntegrityError
from sqlalchemy.orm import Session, selectinload

from productflow_backend.application.agent_conversations import get_agent_conversation_by_id_or_raise
from productflow_backend.application.agent_tasks import get_agent_task_or_raise
from productflow_backend.application.product_workflow.run_state import WORKFLOW_CANCELLED_REASON
from productflow_backend.application.product_workflow.v2_runs import (
    cancel_v2_workflow_run,
    submit_v2_workflow_run,
    validate_v2_workflow_run,
)
from productflow_backend.application.time import now_utc
from productflow_backend.application.workflow_drafts.materialization import get_active_v2_workflow_snapshot
from productflow_backend.domain.enums import (
    AgentConversationScope,
    AgentConversationStatus,
    AgentTaskStatus,
    AgentTurnStatus,
    AgentWorkflowRunRequestStatus,
    WorkflowRunStatus,
)
from productflow_backend.domain.errors import BusinessValidationError, ConflictError, NotFoundError
from productflow_backend.infrastructure.db.models import (
    AgentConversation,
    AgentTask,
    AgentTurnProjection,
    AgentWorkflowRunRequest,
    ProductWorkflow,
    new_id,
)

AGENT_WORKFLOW_RUN_REQUEST_MAX_KEY_BYTES = 200
AGENT_WORKFLOW_RUN_REQUEST_MAX_STEP_ID_LENGTH = 120


@dataclass(frozen=True, slots=True)
class AgentWorkflowRunRequestPreparation:
    product_id: str
    workflow_id: str
    workflow_title: str
    workflow_revision: int
    runnable_node_count: int
    task_id: str | None


@dataclass(frozen=True, slots=True)
class AgentWorkflowRunRequestReconcileResult:
    state: str
    request: AgentWorkflowRunRequest | None = None
    detail: str | None = None


def prepare_agent_workflow_run_request(
    session: Session,
    *,
    conversation_id: str,
    expected_workflow_revision: int,
    task_id: str | None = None,
) -> AgentWorkflowRunRequestPreparation:
    conversation = _get_product_conversation(session, conversation_id)
    workflow, ordered_node_ids = _prepare_current_workflow(
        session,
        product_id=conversation.product_id or "",
        expected_workflow_revision=expected_workflow_revision,
    )
    _validate_task_scope(
        session,
        conversation=conversation,
        task_id=task_id,
    )
    return AgentWorkflowRunRequestPreparation(
        product_id=conversation.product_id or "",
        workflow_id=workflow.id,
        workflow_title=workflow.title,
        workflow_revision=workflow.revision,
        runnable_node_count=len(ordered_node_ids),
        task_id=task_id,
    )


def create_agent_workflow_run_request(
    session: Session,
    *,
    conversation_id: str,
    expected_workflow_revision: int,
    workflow_id: str,
    source_step_id: str,
    idempotency_key: str,
    task_id: str | None = None,
) -> AgentWorkflowRunRequest:
    normalized_key = _normalize_idempotency_key(idempotency_key)
    normalized_step_id = _normalize_source_step_id(source_step_id)
    normalized_workflow_id = _normalize_required_id(workflow_id, "workflow_id")
    request_hash = _request_hash(
        conversation_id=conversation_id,
        task_id=task_id,
        workflow_id=normalized_workflow_id,
        expected_workflow_revision=expected_workflow_revision,
        source_step_id=normalized_step_id,
    )
    conversation = _get_product_conversation(session, conversation_id, lock=True)
    existing = session.scalar(
        select(AgentWorkflowRunRequest)
        .options(
            selectinload(AgentWorkflowRunRequest.workflow),
            selectinload(AgentWorkflowRunRequest.workflow_run),
            selectinload(AgentWorkflowRunRequest.task),
        )
        .where(
            AgentWorkflowRunRequest.conversation_id == conversation.id,
            AgentWorkflowRunRequest.idempotency_key == normalized_key,
        )
    )
    if existing is not None:
        if existing.request_hash != request_hash:
            raise ConflictError("同一 idempotency key 不能提交不同的工作流执行请求")
        session.commit()
        return _load_request(session, existing.id)

    preparation = prepare_agent_workflow_run_request(
        session,
        conversation_id=conversation_id,
        expected_workflow_revision=expected_workflow_revision,
        task_id=task_id,
    )
    if preparation.workflow_id != normalized_workflow_id:
        raise ConflictError("工作流执行请求的 workflow_id 与当前 active 工作流不一致")
    request = AgentWorkflowRunRequest(
        id=new_id(),
        conversation_id=conversation.id,
        task_id=task_id,
        product_id=preparation.product_id,
        workflow_id=preparation.workflow_id,
        expected_workflow_revision=preparation.workflow_revision,
        idempotency_key=normalized_key,
        request_hash=request_hash,
        source_step_id=normalized_step_id,
        status=AgentWorkflowRunRequestStatus.AWAITING_CONFIRMATION,
    )
    session.add(request)
    _mark_request_waiting(conversation, _task_for_request(session, task_id))
    try:
        session.commit()
    except IntegrityError:
        session.rollback()
        replay = session.scalar(
            select(AgentWorkflowRunRequest).where(
                AgentWorkflowRunRequest.conversation_id == conversation.id,
                AgentWorkflowRunRequest.idempotency_key == normalized_key,
            )
        )
        if replay is not None and replay.request_hash == request_hash:
            return _load_request(session, replay.id)
        raise
    return _load_request(session, request.id)


def reconcile_agent_workflow_run_request(
    session: Session,
    *,
    conversation_id: str,
    expected_workflow_revision: int,
    workflow_id: str,
    source_step_id: str,
    idempotency_key: str,
    task_id: str | None = None,
) -> AgentWorkflowRunRequestReconcileResult:
    normalized_key = _normalize_idempotency_key(idempotency_key)
    normalized_step_id = _normalize_source_step_id(source_step_id)
    normalized_workflow_id = _normalize_required_id(workflow_id, "workflow_id")
    request_hash = _request_hash(
        conversation_id=conversation_id,
        task_id=task_id,
        workflow_id=normalized_workflow_id,
        expected_workflow_revision=expected_workflow_revision,
        source_step_id=normalized_step_id,
    )
    _get_product_conversation(session, conversation_id)
    request = session.scalar(
        select(AgentWorkflowRunRequest).where(
            AgentWorkflowRunRequest.conversation_id == conversation_id,
            AgentWorkflowRunRequest.idempotency_key == normalized_key,
        )
    )
    if request is None:
        return AgentWorkflowRunRequestReconcileResult(state="not_applied")
    if request.request_hash != request_hash:
        return AgentWorkflowRunRequestReconcileResult(
            state="conflict",
            detail="同一 idempotency key 已对应其他工作流执行请求",
        )
    return AgentWorkflowRunRequestReconcileResult(
        state="applied",
        request=_load_request(session, request.id),
    )


def get_agent_workflow_run_request(
    session: Session,
    *,
    product_id: str,
    conversation_id: str,
    request_id: str | None = None,
) -> AgentWorkflowRunRequest | None:
    conversation = _get_product_conversation(session, conversation_id)
    if conversation.product_id != product_id:
        raise NotFoundError("Agent workflow run request 不存在")
    statement = (
        select(AgentWorkflowRunRequest)
        .options(
            selectinload(AgentWorkflowRunRequest.workflow),
            selectinload(AgentWorkflowRunRequest.workflow_run),
            selectinload(AgentWorkflowRunRequest.task),
        )
        .where(AgentWorkflowRunRequest.conversation_id == conversation_id)
    )
    if request_id is not None:
        statement = statement.where(AgentWorkflowRunRequest.id == request_id)
    else:
        statement = statement.order_by(
            AgentWorkflowRunRequest.created_at.desc(),
            AgentWorkflowRunRequest.id.desc(),
        ).limit(1)
    request = session.scalar(statement)
    if request is None:
        return None
    changed = _sync_request_from_workflow_run(session, request)
    if changed:
        session.commit()
        request = _load_request(session, request.id)
    return request


def get_agent_workflow_run_request_by_source_step(
    session: Session,
    *,
    conversation_id: str,
    task_id: str | None,
    source_step_id: str,
) -> AgentWorkflowRunRequest | None:
    statement = select(AgentWorkflowRunRequest).where(
        AgentWorkflowRunRequest.conversation_id == conversation_id,
        AgentWorkflowRunRequest.source_step_id == source_step_id,
    )
    if task_id is not None:
        statement = statement.where(AgentWorkflowRunRequest.task_id == task_id)
    return session.scalar(statement)


def attach_agent_workflow_run_request(
    session: Session,
    *,
    product_id: str,
    conversation_id: str,
    projection_id: str,
    request_id: str,
    commit: bool = True,
) -> AgentTurnProjection:
    projection = session.scalar(
        select(AgentTurnProjection)
        .join(AgentConversation, AgentConversation.id == AgentTurnProjection.conversation_id)
        .where(
            AgentTurnProjection.id == projection_id,
            AgentTurnProjection.conversation_id == conversation_id,
            AgentConversation.product_id == product_id,
            AgentConversation.scope_type == AgentConversationScope.PRODUCT_WORKFLOW,
        )
        .with_for_update()
    )
    if projection is None:
        raise NotFoundError("Agent Turn 不存在")
    request = session.scalar(
        select(AgentWorkflowRunRequest).where(
            AgentWorkflowRunRequest.id == request_id,
            AgentWorkflowRunRequest.conversation_id == conversation_id,
        )
    )
    if request is None:
        raise ConflictError("Agent workflow run request 不存在")
    if projection.workflow_run_request_id not in {None, request.id}:
        raise ConflictError("Agent Turn 已关联其他工作流执行请求")
    projection.workflow_run_request_id = request.id
    projection.status = projection.status.AWAITING_CONFIRMATION
    projection.updated_at = now_utc()
    if commit:
        session.commit()
        session.refresh(projection)
    return projection


def confirm_agent_workflow_run_request(
    session: Session,
    *,
    product_id: str,
    conversation_id: str,
    request_id: str,
) -> AgentWorkflowRunRequest:
    request = _load_request_for_update(
        session,
        product_id=product_id,
        conversation_id=conversation_id,
        request_id=request_id,
    )
    _sync_request_from_workflow_run(session, request)
    if request.status == AgentWorkflowRunRequestStatus.CANCELLED:
        raise ConflictError("已取消的工作流执行请求不能确认")
    if request.workflow_run_id is not None:
        session.commit()
        return _load_request(session, request.id)
    if request.status != AgentWorkflowRunRequestStatus.AWAITING_CONFIRMATION:
        raise ConflictError("当前工作流执行请求不在待确认状态")

    workflow, _ = validate_v2_workflow_run(
        session,
        product_id=request.product_id,
        workflow_id=request.workflow_id,
        lock=True,
    )
    if workflow.revision != request.expected_workflow_revision:
        raise ConflictError("工作流已经发生变化，请重新让 Agent 检查后再确认")
    submission = submit_v2_workflow_run(
        session,
        product_id=request.product_id,
        workflow_id=request.workflow_id,
        commit=False,
        run_metadata={
            "requested_by": "agent",
            "agent_workflow_run_request_id": request.id,
            "agent_task_id": request.task_id,
        },
    )
    request.workflow_run_id = submission.run.id
    request.status = AgentWorkflowRunRequestStatus.CONFIRMED
    request.confirmed_at = now_utc()
    request.failure_reason = None
    request.updated_at = now_utc()
    _mark_turn_succeeded(request.turn_projection)
    _mark_task_running(request.task)
    request.conversation.status = AgentConversationStatus.COMPLETED
    request.conversation.updated_at = now_utc()
    session.commit()
    return _load_request(session, request.id)


def cancel_agent_workflow_run_request(
    session: Session,
    *,
    product_id: str,
    conversation_id: str,
    request_id: str,
) -> AgentWorkflowRunRequest:
    request = _load_request_for_update(
        session,
        product_id=product_id,
        conversation_id=conversation_id,
        request_id=request_id,
    )
    _sync_request_from_workflow_run(session, request)
    if request.status == AgentWorkflowRunRequestStatus.CANCELLED:
        session.commit()
        return _load_request(session, request.id)
    if request.workflow_run_id is None:
        request.status = AgentWorkflowRunRequestStatus.CANCELLED
        request.failure_reason = WORKFLOW_CANCELLED_REASON
        request.finished_at = now_utc()
        request.updated_at = now_utc()
        _mark_turn_cancelled(request.turn_projection)
        _mark_task_cancelled(request.task)
        request.conversation.status = AgentConversationStatus.CANCELED
        request.conversation.updated_at = now_utc()
        session.commit()
        return _load_request(session, request.id)
    if request.workflow_run is None:
        raise ConflictError("工作流执行请求关联的运行记录不存在")
    if request.workflow_run.status == WorkflowRunStatus.CANCELLED:
        _sync_request_from_workflow_run(session, request)
        session.commit()
        return _load_request(session, request.id)
    if request.workflow_run.status != WorkflowRunStatus.RUNNING:
        raise ConflictError("已结束的工作流运行不能取消")
    cancel_v2_workflow_run(
        session,
        product_id=request.product_id,
        workflow_id=request.workflow_id,
        run_id=request.workflow_run_id,
    )
    request = _load_request_for_update(
        session,
        product_id=product_id,
        conversation_id=conversation_id,
        request_id=request_id,
    )
    _sync_request_from_workflow_run(session, request)
    session.commit()
    return _load_request(session, request.id)


def _get_product_conversation(
    session: Session,
    conversation_id: str,
    *,
    lock: bool = False,
) -> AgentConversation:
    conversation = get_agent_conversation_by_id_or_raise(session, conversation_id)
    if conversation.scope_type != AgentConversationScope.PRODUCT_WORKFLOW or conversation.product_id is None:
        raise ConflictError("只有商品工作流 Agent conversation 可以请求执行工作流")
    if lock:
        locked = session.scalar(
            select(AgentConversation)
            .where(AgentConversation.id == conversation_id)
            .with_for_update()
        )
        if locked is not None:
            conversation = locked
    return conversation


def _prepare_current_workflow(
    session: Session,
    *,
    product_id: str,
    expected_workflow_revision: int,
) -> tuple[ProductWorkflow, tuple[str, ...]]:
    if expected_workflow_revision <= 0:
        raise BusinessValidationError("expected_workflow_revision 必须大于 0")
    snapshot = get_active_v2_workflow_snapshot(session, product_id=product_id)
    if snapshot.workflow is None:
        raise ConflictError("当前商品还没有可执行的 active schema-v2 工作流")
    if snapshot.workflow.revision != expected_workflow_revision:
        raise ConflictError("工作流 revision 已变化，请重新读取当前工作流")
    return validate_v2_workflow_run(
        session,
        product_id=product_id,
        workflow_id=snapshot.workflow.id,
        lock=False,
    )


def _validate_task_scope(
    session: Session,
    *,
    conversation: AgentConversation,
    task_id: str | None,
) -> AgentTask | None:
    if task_id is None:
        return None
    task = get_agent_task_or_raise(session, task_id)
    if (
        task.session_id != conversation.session_id
        or task.conversation_id != conversation.id
        or task.product_id != conversation.product_id
    ):
        raise ConflictError("Agent Task 与当前 Agent conversation 不匹配")
    return task


def _task_for_request(session: Session, task_id: str | None) -> AgentTask | None:
    return session.get(AgentTask, task_id) if task_id is not None else None


def _mark_request_waiting(conversation: AgentConversation, task: AgentTask | None) -> None:
    now = now_utc()
    conversation.status = AgentConversationStatus.AWAITING_CONFIRMATION
    conversation.updated_at = now
    if task is not None:
        task.status = AgentTaskStatus.AWAITING_CONFIRMATION
        task.waiting_reason = "workflow_run_confirmation"
        task.failure_reason = None
        task.finished_at = None
        task.updated_at = now


def _mark_task_running(task: AgentTask | None) -> None:
    if task is None:
        return
    now = now_utc()
    task.status = AgentTaskStatus.RUNNING
    task.waiting_reason = "workflow_run_running"
    task.failure_reason = None
    task.started_at = task.started_at or now
    task.finished_at = None
    task.updated_at = now


def _mark_task_cancelled(task: AgentTask | None) -> None:
    if task is None:
        return
    now = now_utc()
    task.status = AgentTaskStatus.CANCELED
    task.waiting_reason = None
    task.canceled_at = now
    task.finished_at = now
    task.updated_at = now


def _sync_request_from_workflow_run(session: Session, request: AgentWorkflowRunRequest) -> bool:
    run = request.workflow_run
    if run is None:
        return False
    now = now_utc()
    changed = False
    if run.status == WorkflowRunStatus.RUNNING:
        if request.status != AgentWorkflowRunRequestStatus.CONFIRMED:
            request.status = AgentWorkflowRunRequestStatus.CONFIRMED
            changed = True
        if request.task is not None:
            _mark_task_running(request.task)
    elif run.status == WorkflowRunStatus.SUCCEEDED:
        if request.status != AgentWorkflowRunRequestStatus.SUCCEEDED:
            request.status = AgentWorkflowRunRequestStatus.SUCCEEDED
            request.finished_at = run.finished_at or now
            changed = True
        _mark_task_finished(request.task, AgentTaskStatus.SUCCEEDED, None, run.finished_at or now)
    elif run.status == WorkflowRunStatus.FAILED:
        if request.status != AgentWorkflowRunRequestStatus.FAILED or request.failure_reason != run.failure_reason:
            request.status = AgentWorkflowRunRequestStatus.FAILED
            request.failure_reason = run.failure_reason
            request.finished_at = run.finished_at or now
            changed = True
        _mark_task_finished(request.task, AgentTaskStatus.FAILED, run.failure_reason, run.finished_at or now)
    elif run.status == WorkflowRunStatus.CANCELLED:
        if request.status != AgentWorkflowRunRequestStatus.CANCELLED:
            request.status = AgentWorkflowRunRequestStatus.CANCELLED
            request.failure_reason = run.failure_reason or WORKFLOW_CANCELLED_REASON
            request.finished_at = run.finished_at or now
            changed = True
        _mark_task_finished(request.task, AgentTaskStatus.CANCELED, request.failure_reason, run.finished_at or now)
    if changed:
        request.updated_at = now
    return changed


def _mark_turn_succeeded(projection: AgentTurnProjection | None) -> None:
    if projection is None:
        return
    now = now_utc()
    projection.status = AgentTurnStatus.SUCCEEDED
    projection.finished_at = projection.finished_at or now
    projection.updated_at = now


def _mark_turn_cancelled(projection: AgentTurnProjection | None) -> None:
    if projection is None:
        return
    now = now_utc()
    projection.status = AgentTurnStatus.CANCELED
    projection.finished_at = projection.finished_at or now
    projection.updated_at = now


def _mark_task_finished(
    task: AgentTask | None,
    status: AgentTaskStatus,
    failure_reason: str | None,
    finished_at: Any,
) -> None:
    if task is None:
        return
    task.status = status
    task.waiting_reason = None
    task.failure_reason = failure_reason
    task.finished_at = finished_at
    task.updated_at = now_utc()


def _load_request(session: Session, request_id: str) -> AgentWorkflowRunRequest:
    request = session.scalar(
        select(AgentWorkflowRunRequest)
        .options(
            selectinload(AgentWorkflowRunRequest.conversation),
            selectinload(AgentWorkflowRunRequest.workflow),
            selectinload(AgentWorkflowRunRequest.workflow_run),
            selectinload(AgentWorkflowRunRequest.task),
            selectinload(AgentWorkflowRunRequest.turn_projection),
        )
        .where(AgentWorkflowRunRequest.id == request_id)
        .execution_options(populate_existing=True)
    )
    if request is None:
        raise NotFoundError("Agent workflow run request 不存在")
    return request


def _load_request_for_update(
    session: Session,
    *,
    product_id: str,
    conversation_id: str,
    request_id: str,
) -> AgentWorkflowRunRequest:
    request = session.scalar(
        select(AgentWorkflowRunRequest)
        .options(
            selectinload(AgentWorkflowRunRequest.conversation),
            selectinload(AgentWorkflowRunRequest.workflow),
            selectinload(AgentWorkflowRunRequest.workflow_run),
            selectinload(AgentWorkflowRunRequest.task),
            selectinload(AgentWorkflowRunRequest.turn_projection),
        )
        .where(
            AgentWorkflowRunRequest.id == request_id,
            AgentWorkflowRunRequest.conversation_id == conversation_id,
            AgentWorkflowRunRequest.product_id == product_id,
        )
        .with_for_update()
        .execution_options(populate_existing=True)
    )
    if request is None:
        raise NotFoundError("Agent workflow run request 不存在")
    return request


def _normalize_idempotency_key(value: str) -> str:
    normalized = value.strip()
    if not normalized:
        raise BusinessValidationError("工作流执行请求 idempotency key 不能为空")
    if len(normalized.encode("utf-8")) > AGENT_WORKFLOW_RUN_REQUEST_MAX_KEY_BYTES:
        raise BusinessValidationError("工作流执行请求 idempotency key 过长")
    return normalized


def _normalize_source_step_id(value: str) -> str:
    normalized = value.strip()
    if not normalized or len(normalized) > AGENT_WORKFLOW_RUN_REQUEST_MAX_STEP_ID_LENGTH:
        raise BusinessValidationError("工作流执行请求 source_step_id 无效")
    return normalized


def _normalize_required_id(value: str, field_name: str) -> str:
    normalized = value.strip()
    if not normalized or len(normalized) > 64:
        raise BusinessValidationError(f"{field_name} 无效")
    return normalized


def _request_hash(
    *,
    conversation_id: str,
    task_id: str | None,
    workflow_id: str,
    expected_workflow_revision: int,
    source_step_id: str,
) -> str:
    payload = {
        "schema_version": 1,
        "conversation_id": conversation_id,
        "task_id": task_id,
        "workflow_id": workflow_id,
        "expected_workflow_revision": expected_workflow_revision,
        "source_step_id": source_step_id,
    }
    encoded = json.dumps(payload, ensure_ascii=False, sort_keys=True, separators=(",", ":")).encode()
    return hashlib.sha256(encoded).hexdigest()


__all__ = [
    "AgentWorkflowRunRequestPreparation",
    "AgentWorkflowRunRequestReconcileResult",
    "attach_agent_workflow_run_request",
    "cancel_agent_workflow_run_request",
    "confirm_agent_workflow_run_request",
    "create_agent_workflow_run_request",
    "get_agent_workflow_run_request",
    "get_agent_workflow_run_request_by_source_step",
    "prepare_agent_workflow_run_request",
    "reconcile_agent_workflow_run_request",
]
