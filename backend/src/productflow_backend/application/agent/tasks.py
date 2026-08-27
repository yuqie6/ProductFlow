"""AgentTask 在 Session 下保存一条业务目标与一次 task-specific run。

兼容列仍叫 harness_run_id；page context 不改写 goal。
"""

from __future__ import annotations

import base64
import hashlib
import json
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Any

from sqlalchemy import and_, func, or_, select
from sqlalchemy.orm import Session, selectinload

from productflow_backend.application.agent.sessions import get_agent_session_or_raise
from productflow_backend.application.agent.turn_status import TASK_BLOCKING_TURN_STATUSES
from productflow_backend.application.async_delivery import stage_async_dispatch_for_actor
from productflow_backend.application.time import now_utc
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
    AgentPageContextSnapshot,
    AgentSession,
    AgentTask,
    AgentTurnProjection,
    AgentWorkflowRunRequest,
    new_id,
)

AGENT_TASK_TITLE_MAX_LENGTH = 160
AGENT_TASK_GOAL_MAX_LENGTH = 20_000
AGENT_TASK_SUMMARY_MAX_LENGTH = 2_000
AGENT_TASK_LIST_DEFAULT_LIMIT = 50
AGENT_TASK_LIST_MAX_LIMIT = 100
AGENT_TASK_CURSOR_VERSION = 1
AGENT_CONTEXT_ROUTE_MAX_LENGTH = 512
AGENT_CONTEXT_PAGE_TYPE_MAX_LENGTH = 80
AGENT_CONTEXT_MAX_ASSET_IDS = 100
AGENT_CONTEXT_MAX_FILTERS = 20


def initial_agent_task_turn_idempotency_key(*, conversation_id: str, task_id: str) -> str:
    """UI 与恢复路径共用的 Task 首轮 Turn idempotency key。"""
    return f"initial:{conversation_id}:{task_id}"

_ACTIVE_TASK_STATUSES = {
    AgentTaskStatus.QUEUED,
    AgentTaskStatus.RUNNING,
    AgentTaskStatus.WAITING_USER,
    AgentTaskStatus.AWAITING_CONFIRMATION,
}
_INCOMPLETE_TASK_STATUSES = _ACTIVE_TASK_STATUSES | {AgentTaskStatus.PAUSED}
_TERMINAL_TASK_STATUSES = {
    AgentTaskStatus.SUCCEEDED,
    AgentTaskStatus.FAILED,
    AgentTaskStatus.CANCELED,
    AgentTaskStatus.UNKNOWN,
}
_USER_OWNED_TASK_STATUSES = {
    AgentTaskStatus.SUCCEEDED,
    AgentTaskStatus.CANCELED,
    AgentTaskStatus.PAUSED,
}
_BUSY_HARNESS_TURN_STATUSES = {
    AgentTurnStatus.QUEUED,
    AgentTurnStatus.RUNNING,
    AgentTurnStatus.CANCEL_REQUESTED,
}
_ACTIVE_TURN_STATUSES = TASK_BLOCKING_TURN_STATUSES


@dataclass(frozen=True, slots=True)
class AgentTaskPage:
    items: list[AgentTask]
    next_cursor: str | None = None


@dataclass(frozen=True, slots=True)
class AgentTaskResumeResult:
    task: AgentTask
    projection_id: str | None = None


@dataclass(frozen=True, slots=True)
class _AgentTaskCursor:
    session_id: str | None
    include_terminal: bool
    updated_at: datetime
    task_id: str


def new_agent_task(
    *,
    session_id: str,
    title: str,
    goal: str,
    conversation: AgentConversation | None = None,
) -> AgentTask:
    normalized_title = _normalize_task_title(title)
    normalized_goal = _normalize_task_goal(goal)
    task_id = new_id()
    return AgentTask(
        id=task_id,
        session_id=session_id,
        conversation_id=conversation.id if conversation is not None else None,
        product_id=conversation.product_id if conversation is not None else None,
        harness_run_id=task_id,  # 这条 Task 自己的 run，不是 Session transcript。
        title=normalized_title,
        goal=normalized_goal,
        summary=normalized_goal[:AGENT_TASK_SUMMARY_MAX_LENGTH],
        status=AgentTaskStatus.QUEUED,
    )


def create_agent_task(
    session: Session,
    *,
    session_id: str,
    title: str,
    goal: str,
    conversation_id: str | None = None,
) -> AgentTask:
    """在 Session 下创建一条业务目标。本函数 commit。"""
    agent_session = get_agent_session_or_raise(session, session_id)
    conversation = _conversation_for_task(session, conversation_id)
    _validate_task_session(agent_session, conversation)
    task = new_agent_task(
        session_id=agent_session.id,
        title=title,
        goal=goal,
        conversation=conversation,
    )
    session.add(task)
    refresh_agent_session_summary(session, agent_session.id)
    session.commit()
    return get_agent_task_or_raise(session, task.id)


def get_agent_task_or_raise(session: Session, task_id: str) -> AgentTask:
    task = session.scalar(
        select(AgentTask)
        .options(
            selectinload(AgentTask.session),
            selectinload(AgentTask.conversation),
            selectinload(AgentTask.workflow_run_requests).selectinload(
                AgentWorkflowRunRequest.graph_run,
            ),
        )
        .where(AgentTask.id == task_id)
    )
    if task is None:
        raise NotFoundError("Agent Task 不存在")
    if _synchronize_task_workflow_run(task, session):
        # 已确认运行后 Task 跟随 graph run；读取路径也会提交这次投影。
        refresh_agent_session_summary(session, task.session_id)
        session.commit()
        return get_agent_task_or_raise(session, task_id)
    return task


def list_agent_tasks(
    session: Session,
    *,
    session_id: str | None = None,
    include_terminal: bool = True,
    limit: int = AGENT_TASK_LIST_DEFAULT_LIMIT,
    after: str = "",
) -> AgentTaskPage:
    if not 1 <= limit <= AGENT_TASK_LIST_MAX_LIMIT:
        raise BusinessValidationError(
            f"Agent Task 列表 limit 必须在 1 到 {AGENT_TASK_LIST_MAX_LIMIT} 之间"
        )
    cursor = _decode_agent_task_cursor(after) if after.strip() else None
    if cursor is not None:
        if cursor.session_id != session_id or cursor.include_terminal != include_terminal:
            raise BusinessValidationError("Agent Task 分页 cursor 与当前查询条件不匹配")
    statement = select(AgentTask).options(
        selectinload(AgentTask.session),
        selectinload(AgentTask.conversation),
        selectinload(AgentTask.workflow_run_requests).selectinload(
            AgentWorkflowRunRequest.graph_run,
        ),
    )
    if session_id is not None:
        statement = statement.where(AgentTask.session_id == session_id)
    if not include_terminal:
        statement = statement.where(AgentTask.status.not_in(_TERMINAL_TASK_STATUSES))
    if cursor is not None:
        statement = statement.where(
            or_(
                AgentTask.updated_at < cursor.updated_at,
                and_(
                    AgentTask.updated_at == cursor.updated_at,
                    AgentTask.id < cursor.task_id,
                ),
            )
        )
    rows = list(
        session.scalars(
            statement.order_by(AgentTask.updated_at.desc(), AgentTask.id.desc()).limit(limit + 1)
        ).all()
    )
    has_more = len(rows) > limit
    rows = rows[:limit]
    changed = False
    for task in rows:
        synchronized = _synchronize_task_workflow_run(task, session)
        if synchronized:
            refresh_agent_session_summary(session, task.session_id)
        changed = synchronized or changed
    if changed:
        session.commit()
    next_cursor = None
    if has_more and rows:
        oldest = rows[-1]
        next_cursor = _encode_agent_task_cursor(
            _AgentTaskCursor(
                session_id=session_id,
                include_terminal=include_terminal,
                updated_at=_normalize_task_cursor_datetime(oldest.updated_at),
                task_id=oldest.id,
            )
        )
    return AgentTaskPage(items=rows, next_cursor=next_cursor)


def rename_agent_task(session: Session, *, task_id: str, title: str) -> AgentTask:
    task = _get_task_for_update(session, task_id)
    task.title = _normalize_task_title(title)
    task.updated_at = now_utc()
    refresh_agent_session_summary(session, task.session_id)
    session.commit()
    return get_agent_task_or_raise(session, task.id)


def cancel_agent_task(session: Session, *, task_id: str) -> AgentTask:
    """本地取消 Task 与当前投影。不把 Node.js transcript 当作权威。本函数 commit。"""
    task = _get_task_for_update(session, task_id)
    if task.status not in _TERMINAL_TASK_STATUSES:
        now = now_utc()
        task.status = AgentTaskStatus.CANCELED
        task.waiting_reason = None
        task.canceled_at = now
        task.finished_at = now
        task.updated_at = now
        if task.current_turn_id is not None:
            projection = session.get(AgentTurnProjection, task.current_turn_id)
            if projection is not None and projection.status in _ACTIVE_TURN_STATUSES:
                projection.status = AgentTurnStatus.CANCELED
                projection.finished_at = now
                projection.updated_at = now
                if projection.conversation is not None:
                    projection.conversation.status = AgentConversationStatus.CANCELED
                    projection.conversation.updated_at = now
        refresh_agent_session_summary(session, task.session_id)
        session.commit()
    return get_agent_task_or_raise(session, task.id)


def complete_agent_task(session: Session, *, task_id: str) -> AgentTask:
    """用户标记 Goal 完成。Turn 或 WorkflowGraphRun 成功不能代替这一步。本函数 commit。"""
    task = _get_task_for_update(session, task_id)
    if task.status in _TERMINAL_TASK_STATUSES:
        raise ConflictError("已结束的 Agent Task 不能再标记完成")
    if _task_has_busy_harness_turn(session, task):
        raise ConflictError("运行中的 Agent Task 需要先等待结束或取消，才能标记 Goal 完成")
    now = now_utc()
    task.status = AgentTaskStatus.SUCCEEDED
    task.waiting_reason = None
    task.failure_reason = None
    task.finished_at = now
    task.updated_at = now
    task.summary = _bounded_summary(task.goal or "Goal 已完成")
    refresh_agent_session_summary(session, task.session_id)
    session.commit()
    return get_agent_task_or_raise(session, task.id)


def pause_agent_task(session: Session, *, task_id: str) -> AgentTask:
    """只暂停等待用户/确认的 Task。运行中的 harness Turn 不能中断后保持暂停。"""
    task = _get_task_for_update(session, task_id)
    if task.status in _TERMINAL_TASK_STATUSES:
        return get_agent_task_or_raise(session, task.id)
    projection = session.get(AgentTurnProjection, task.current_turn_id) if task.current_turn_id else None
    if task.status == AgentTaskStatus.PAUSED:
        return get_agent_task_or_raise(session, task.id)
    if _task_has_busy_harness_turn(session, task, projection=projection):
        raise ConflictError("运行中的 Agent Task 需要先取消，当前 harness 不支持中断后保持任务暂停")
    if task.status not in {
        AgentTaskStatus.QUEUED,
        AgentTaskStatus.WAITING_USER,
        AgentTaskStatus.AWAITING_CONFIRMATION,
    }:
        raise ConflictError("当前 Agent Task 状态不允许暂停")
    task.status = AgentTaskStatus.PAUSED
    task.waiting_reason = "user_paused"
    task.updated_at = now_utc()
    refresh_agent_session_summary(session, task.session_id)
    session.commit()
    return get_agent_task_or_raise(session, task.id)


def resume_agent_task(session: Session, *, task_id: str) -> AgentTaskResumeResult:
    """恢复暂停 Task。无当前 Turn 时复用首轮 idempotency key，避免与恢复扫描双写。"""
    task = _get_task_for_update(session, task_id)
    if task.status != AgentTaskStatus.PAUSED:
        return AgentTaskResumeResult(task=get_agent_task_or_raise(session, task.id))
    projection = session.get(AgentTurnProjection, task.current_turn_id) if task.current_turn_id else None
    if projection is not None:
        if projection.status == AgentTurnStatus.REQUIRES_INPUT:
            task.status = AgentTaskStatus.WAITING_USER
            task.waiting_reason = "requires_input"
        elif projection.status == AgentTurnStatus.AWAITING_CONFIRMATION:
            task.status = AgentTaskStatus.AWAITING_CONFIRMATION
            task.waiting_reason = "awaiting_confirmation"
        else:
            raise ConflictError("暂停的 Agent Task 当前 Turn 状态已变化，请刷新后处理")
        task.updated_at = now_utc()
        refresh_agent_session_summary(session, task.session_id)
        session.commit()
        return AgentTaskResumeResult(task=get_agent_task_or_raise(session, task.id))

    if task.conversation_id is None:
        raise ConflictError("没有绑定 conversation 的 Agent Task 不能恢复执行")
    task.status = AgentTaskStatus.QUEUED
    task.waiting_reason = None
    task.failure_reason = None
    task.finished_at = None
    task.canceled_at = None
    task.updated_at = now_utc()
    # 恢复扫描与显式 resume 共用此 key，worker 已并发恢复时重试不能再造一条 first Turn。
    from productflow_backend.application.agent.turn_projection import reserve_agent_turn

    conversation = session.get(AgentConversation, task.conversation_id)
    if conversation is None:
        raise ConflictError("Agent Task 绑定的 conversation 不存在")
    reservation = reserve_agent_turn(
        session,
        product_id=(
            conversation.product_id
            if conversation.scope_type == AgentConversationScope.PRODUCT_WORKFLOW
            else None
        ),
        conversation_id=conversation.id,
        input_text=task.goal,
        input_asset_ids=[],
        idempotency_key=initial_agent_task_turn_idempotency_key(
            conversation_id=conversation.id,
            task_id=task.id,
        ),
        task_id=task.id,
    )
    refresh_agent_session_summary(session, task.session_id)
    stage_async_dispatch_for_actor(session, "run_agent_turn_sync", reservation.projection.id)
    session.commit()
    return AgentTaskResumeResult(
        task=get_agent_task_or_raise(session, task.id),
        projection_id=reservation.projection.id,
    )


def task_contract(session: Session, task_id: str) -> tuple[AgentTask, AgentConversation]:
    """Task 可执行合同：必须已绑定 conversation，且未取消。"""
    task = session.scalar(
        select(AgentTask)
        .options(
            selectinload(AgentTask.conversation),
        )
        .where(AgentTask.id == task_id)
    )
    if task is None:
        raise NotFoundError("Agent Task 不存在")
    if task.conversation is None:
        raise ConflictError("当前 Agent Task 尚未绑定可执行的 Agent conversation")
    if task.status == AgentTaskStatus.CANCELED:
        raise ConflictError("已取消的 Agent Task 不能继续运行")
    return task, task.conversation


def ensure_task_for_turn(
    session: Session,
    *,
    conversation: AgentConversation,
    task_id: str | None,
    ignore_turn_id: str | None = None,
) -> AgentTask | None:
    """Turn 必须挂同一 Session/conversation 的未取消 Task，且不能叠未结束 Turn。

    `ignore_turn_id` 用于提问续跑：正在回答的 REQUIRES_INPUT Turn 仍挂在 Task 上，
    但不能挡住它自己的 continuation。
    """
    if conversation.session_id is None:
        raise ConflictError("Agent conversation 尚未绑定 Session")
    agent_session = get_agent_session_or_raise(session, conversation.session_id)
    if task_id is None:
        return None

    task = _get_task_for_update(session, task_id)
    if task.session_id != agent_session.id or task.conversation_id != conversation.id:
        raise ConflictError("Agent Task 与当前 Agent conversation 不匹配")
    if task.status in _TERMINAL_TASK_STATUSES:
        raise ConflictError("已结束的 Agent Task 不能继续提交 Turn")
    if task.status == AgentTaskStatus.PAUSED:
        raise ConflictError("已暂停的 Agent Task 需要恢复后才能提交 Turn")
    if task.current_turn_id is not None and task.current_turn_id != ignore_turn_id:
        current_turn = session.get(AgentTurnProjection, task.current_turn_id)
        if current_turn is not None and current_turn.status in _ACTIVE_TURN_STATUSES:
            raise ConflictError("当前 Agent Task 仍有未结束的 Turn")
    return task


def create_page_context_snapshot(
    session: Session,
    *,
    task: AgentTask | None,
    turn_id: str,
    page_context: dict[str, Any] | None,
) -> AgentPageContextSnapshot | None:
    """记录本轮页面氛围。route 变化只影响后续 Turn 上下文，不改写 Task goal。"""
    if page_context is None:
        return None
    normalized = normalize_page_context(page_context)
    digest = page_context_digest(normalized)
    snapshot = AgentPageContextSnapshot(
        task_id=task.id if task is not None else None,
        turn_id=turn_id,
        route=normalized["route"],
        page_type=normalized["page_type"],
        product_id=normalized["product_id"],
        workflow_id=normalized["workflow_id"],
        selected_asset_ids_json=normalized["selected_asset_ids"],
        visible_asset_ids_json=normalized["visible_asset_ids"],
        filters_json=normalized["filters"],
        workflow_revision=normalized["workflow_revision"],
        library_revision=normalized["library_revision"],
        digest=digest,
        captured_at=normalized["captured_at"],
    )
    session.add(snapshot)
    session.flush()
    return snapshot


def normalize_page_context(value: dict[str, Any]) -> dict[str, Any]:
    route = _bounded_string(value.get("route"), AGENT_CONTEXT_ROUTE_MAX_LENGTH, "页面 route")
    page_type = _bounded_string(value.get("page_type"), AGENT_CONTEXT_PAGE_TYPE_MAX_LENGTH, "页面类型")
    product_id = _optional_identifier(value.get("product_id"), "product_id")
    workflow_id = _optional_identifier(value.get("workflow_id"), "workflow_id")
    selected_asset_ids = _bounded_identifiers(value.get("selected_asset_ids", []), "selected_asset_ids")
    visible_asset_ids = _bounded_identifiers(value.get("visible_asset_ids", []), "visible_asset_ids")
    filters = value.get("filters", {})
    if not isinstance(filters, dict) or len(filters) > AGENT_CONTEXT_MAX_FILTERS:
        raise BusinessValidationError("页面 filters 必须是有界对象")
    normalized_filters: dict[str, str] = {}
    for key, item in filters.items():
        normalized_key = _bounded_string(key, 80, "页面 filter key")
        normalized_filters[normalized_key] = _bounded_string(item, 255, "页面 filter value")
    workflow_revision = _optional_non_negative_int(value.get("workflow_revision"), "workflow_revision")
    library_revision = _optional_non_negative_int(value.get("library_revision"), "library_revision")
    captured_at = _parse_captured_at(value.get("captured_at"))
    return {
        "route": route,
        "page_type": page_type,
        "product_id": product_id,
        "workflow_id": workflow_id,
        "selected_asset_ids": selected_asset_ids,
        "visible_asset_ids": visible_asset_ids,
        "filters": normalized_filters,
        "workflow_revision": workflow_revision,
        "library_revision": library_revision,
        "captured_at": captured_at,
    }


def page_context_digest(value: dict[str, Any]) -> str:
    encoded = json.dumps(
        {**value, "captured_at": value["captured_at"].isoformat()},
        ensure_ascii=False,
        sort_keys=True,
        separators=(",", ":"),
    ).encode()
    return hashlib.sha256(encoded).hexdigest()


def update_agent_task_from_turn(
    session: Session,
    *,
    projection: AgentTurnProjection,
    status: AgentTurnStatus,
    error_text: str | None,
    finished_at: datetime | None,
) -> AgentTask | None:
    """用 Turn 投影推进 Task。unknown 原样保留，不降成 failed。"""
    if projection.task_id is None:
        return None
    task = session.get(AgentTask, projection.task_id, with_for_update=True)
    if task is None:
        raise ConflictError("Agent Turn 绑定的 Task 不存在")
    if task.status in {AgentTaskStatus.SUCCEEDED, AgentTaskStatus.CANCELED}:
        return task
    now = now_utc()
    task.current_turn_id = projection.id
    task.updated_at = now
    if task.status == AgentTaskStatus.PAUSED and status in {
        AgentTurnStatus.REQUIRES_INPUT,
        AgentTurnStatus.AWAITING_CONFIRMATION,
    }:
        task.summary = _agent_task_turn_summary(projection, status=status, error_text=error_text)
        refresh_agent_session_summary(session, task.session_id)
        return task
    if status in {AgentTurnStatus.QUEUED, AgentTurnStatus.RUNNING, AgentTurnStatus.CANCEL_REQUESTED}:
        task.status = AgentTaskStatus.RUNNING if status != AgentTurnStatus.QUEUED else AgentTaskStatus.QUEUED
        task.waiting_reason = None
        task.failure_reason = None
        task.started_at = task.started_at or now
        task.finished_at = None
        task.canceled_at = None
    elif status == AgentTurnStatus.REQUIRES_INPUT:
        task.status = AgentTaskStatus.WAITING_USER
        task.waiting_reason = "requires_input"
        task.finished_at = None
    elif status == AgentTurnStatus.AWAITING_CONFIRMATION:
        task.status = AgentTaskStatus.AWAITING_CONFIRMATION
        task.waiting_reason = "awaiting_confirmation"
        task.finished_at = None
    elif status == AgentTurnStatus.SUCCEEDED:
        if _task_is_product_goal(session, task):
            task.status = AgentTaskStatus.WAITING_USER
            task.waiting_reason = "goal_loop"
            task.failure_reason = None
            task.finished_at = None
        else:
            task.status = AgentTaskStatus.SUCCEEDED
            task.waiting_reason = None
            task.failure_reason = None
            task.finished_at = finished_at or now
    elif status in {AgentTurnStatus.FAILED, AgentTurnStatus.CANCELED, AgentTurnStatus.UNKNOWN}:
        if _task_is_product_goal(session, task):
            task.status = AgentTaskStatus.WAITING_USER
            task.waiting_reason = "goal_loop"
            task.failure_reason = None
            task.finished_at = None
        elif status == AgentTurnStatus.FAILED:
            task.status = AgentTaskStatus.FAILED
            task.waiting_reason = None
            task.failure_reason = error_text
            task.finished_at = finished_at or now
        elif status == AgentTurnStatus.CANCELED:
            task.status = AgentTaskStatus.CANCELED
            task.waiting_reason = None
            task.canceled_at = finished_at or now
            task.finished_at = finished_at or now
        else:
            # provider/tool 结果无法证明时保持 unknown。
            task.status = AgentTaskStatus.UNKNOWN
            task.waiting_reason = None
            task.failure_reason = error_text
            task.finished_at = finished_at or now
    task.summary = _agent_task_turn_summary(projection, status=status, error_text=error_text)
    refresh_agent_session_summary(session, task.session_id)
    return task


def _synchronize_task_workflow_run(task: AgentTask, session: Session | None = None) -> bool:
    """已确认运行请求后，request 跟随 graph run。用户完成/取消/暂停不被跑图结果改写。"""
    request = task.workflow_run_requests[0] if task.workflow_run_requests else None
    if request is None:
        return False
    run = request.graph_run
    if run is None:
        return False

    finished_at = run.finished_at or now_utc()
    request_changed = False
    if run.status == WorkflowRunStatus.RUNNING:
        if request.status != AgentWorkflowRunRequestStatus.CONFIRMED:
            request.status = AgentWorkflowRunRequestStatus.CONFIRMED
            request_changed = True
    elif run.status == WorkflowRunStatus.SUCCEEDED:
        if request.status != AgentWorkflowRunRequestStatus.SUCCEEDED:
            request.status = AgentWorkflowRunRequestStatus.SUCCEEDED
            request.failure_reason = None
            request.finished_at = finished_at
            request_changed = True
    elif run.status == WorkflowRunStatus.FAILED:
        if (
            request.status != AgentWorkflowRunRequestStatus.FAILED
            or request.failure_reason != run.failure_reason
        ):
            request.status = AgentWorkflowRunRequestStatus.FAILED
            request.failure_reason = run.failure_reason
            request.finished_at = finished_at
            request_changed = True
    elif run.status == WorkflowRunStatus.CANCELLED:
        failure_reason = run.failure_reason or "工作流运行已取消"
        if (
            request.status != AgentWorkflowRunRequestStatus.CANCELLED
            or request.failure_reason != failure_reason
        ):
            request.status = AgentWorkflowRunRequestStatus.CANCELLED
            request.failure_reason = failure_reason
            request.finished_at = finished_at
            request_changed = True
    else:
        return False

    task_changed = apply_graph_run_status_to_task(task, run, session)
    changed = request_changed or task_changed
    if changed:
        now = now_utc()
        request.updated_at = now
        if task_changed:
            task.updated_at = now
    return changed


def apply_graph_run_status_to_task(
    task: AgentTask | None,
    run: Any,
    session: Session | None = None,
) -> bool:
    """把 graph run 投影到 Task。用户完成/取消/暂停和商品 Goal 循环不被跑图终态改写。"""
    if task is None or task.status in _USER_OWNED_TASK_STATUSES:
        return False
    keep_goal = _task_is_product_goal(session, task)
    finished_at = run.finished_at or now_utc()
    if run.status == WorkflowRunStatus.RUNNING:
        expected_status = AgentTaskStatus.RUNNING
        expected_waiting = "workflow_run_running"
        expected_failure = None
        expected_finished = None
        expected_summary = "工作流运行中"
        clear_canceled_at = True
        started_at = task.started_at or run.started_at
    elif run.status == WorkflowRunStatus.SUCCEEDED:
        expected_status = AgentTaskStatus.WAITING_USER if keep_goal else AgentTaskStatus.SUCCEEDED
        expected_waiting = "goal_loop" if keep_goal else None
        expected_failure = None
        expected_finished = None if keep_goal else finished_at
        expected_summary = "工作流运行已完成，Goal 未结束" if keep_goal else "工作流运行已完成"
        clear_canceled_at = False
        started_at = task.started_at
    elif run.status == WorkflowRunStatus.FAILED:
        expected_status = AgentTaskStatus.WAITING_USER if keep_goal else AgentTaskStatus.FAILED
        expected_waiting = "goal_loop" if keep_goal else None
        expected_failure = None if keep_goal else run.failure_reason
        expected_finished = None if keep_goal else finished_at
        expected_summary = _bounded_summary(
            f"工作流运行失败：{run.failure_reason or '未知原因'}。Goal 未结束"
            if keep_goal
            else f"工作流运行失败：{run.failure_reason or '未知原因'}"
        )
        clear_canceled_at = False
        started_at = task.started_at
    elif run.status == WorkflowRunStatus.CANCELLED:
        failure_reason = run.failure_reason or "工作流运行已取消"
        expected_status = AgentTaskStatus.WAITING_USER if keep_goal else AgentTaskStatus.CANCELED
        expected_waiting = "goal_loop" if keep_goal else None
        expected_failure = None if keep_goal else failure_reason
        expected_finished = None if keep_goal else finished_at
        expected_summary = "工作流运行已取消，Goal 未结束" if keep_goal else "工作流运行已取消"
        clear_canceled_at = False
        started_at = task.started_at
    else:
        return False

    changed = (
        task.status != expected_status
        or task.waiting_reason != expected_waiting
        or task.failure_reason != expected_failure
        or task.finished_at != expected_finished
        or task.summary != expected_summary
        or task.started_at != started_at
    )
    task.status = expected_status
    task.waiting_reason = expected_waiting
    task.failure_reason = expected_failure
    task.finished_at = expected_finished
    task.summary = expected_summary
    task.started_at = started_at
    if clear_canceled_at:
        if task.canceled_at is not None:
            changed = True
        task.canceled_at = None
    elif not keep_goal and run.status == WorkflowRunStatus.CANCELLED and task.canceled_at != finished_at:
        task.canceled_at = finished_at
        changed = True
    return changed


def _agent_task_turn_summary(
    projection: AgentTurnProjection,
    *,
    status: AgentTurnStatus,
    error_text: str | None,
) -> str:
    if status == AgentTurnStatus.REQUIRES_INPUT:
        question = projection.question_json or {}
        question_text = question.get("question") if isinstance(question, dict) else None
        if isinstance(question_text, str) and question_text.strip():
            return _bounded_summary(f"等待回答：{question_text}")
        return "等待回答"
    if status == AgentTurnStatus.AWAITING_CONFIRMATION:
        if projection.artifact_name:
            return _bounded_summary("等待确认：Agent Draft")
        return "等待确认"
    if error_text:
        return _bounded_summary(f"执行失败：{error_text}")
    if projection.output_text:
        return _bounded_summary(projection.output_text)
    for step in reversed(projection.tool_steps_json or []):
        summary = step.get("summary") if isinstance(step, dict) else None
        if isinstance(summary, str) and summary.strip():
            return _bounded_summary(summary)
    return _bounded_summary(f"Agent Turn 状态：{status.value}")


def _bounded_summary(value: str) -> str:
    normalized = " ".join(value.split())
    return normalized[:AGENT_TASK_SUMMARY_MAX_LENGTH]


def refresh_agent_session_summary(session: Session, session_id: str) -> None:
    agent_session = session.get(AgentSession, session_id)
    if agent_session is None:
        return
    session.flush()
    task_filter = AgentTask.session_id == session_id
    total = session.scalar(select(func.count(AgentTask.id)).where(task_filter)) or 0
    active = session.scalar(
        select(func.count(AgentTask.id)).where(
            task_filter,
            AgentTask.status.in_(_INCOMPLETE_TASK_STATUSES),
        )
    ) or 0
    recent = list(
        session.execute(
            select(AgentTask.title, AgentTask.status)
            .where(task_filter)
            .order_by(AgentTask.updated_at.desc(), AgentTask.id.desc())
            .limit(8)
        ).all()
    )
    if not recent:
        summary = "暂无 Agent Task"
    else:
        recent_text = "；".join(f"{title}（{status.value}）" for title, status in recent)
        summary = f"任务 {total} 个，未完成 {active} 个。最近任务：{recent_text}"
    bounded = _bounded_summary(summary)
    if agent_session.summary != bounded:
        agent_session.summary = bounded
        agent_session.updated_at = now_utc()


def _encode_agent_task_cursor(cursor: _AgentTaskCursor) -> str:
    encoded = json.dumps(
        {
            "v": AGENT_TASK_CURSOR_VERSION,
            "session_id": cursor.session_id,
            "include_terminal": cursor.include_terminal,
            "updated_at": _normalize_task_cursor_datetime(cursor.updated_at).isoformat(),
            "id": cursor.task_id,
        },
        sort_keys=True,
        separators=(",", ":"),
    ).encode()
    return base64.urlsafe_b64encode(encoded).decode().rstrip("=")


def _decode_agent_task_cursor(value: str) -> _AgentTaskCursor:
    normalized = value.strip()
    if len(normalized) > 4096:
        raise BusinessValidationError("Agent Task 分页 cursor 无效")
    try:
        padded = normalized + "=" * (-len(normalized) % 4)
        raw = base64.b64decode(padded, altchars=b"-_", validate=True)
        decoded = json.loads(raw)
        if not isinstance(decoded, dict) or set(decoded) != {
            "v", "session_id", "include_terminal", "updated_at", "id"
        }:
            raise ValueError
        if decoded["v"] != AGENT_TASK_CURSOR_VERSION:
            raise ValueError
        session_id = decoded["session_id"]
        if session_id is not None and (not isinstance(session_id, str) or not session_id):
            raise ValueError
        if not isinstance(decoded["include_terminal"], bool):
            raise ValueError
        if not all(
            isinstance(decoded[field], str) and decoded[field]
            for field in ("updated_at", "id")
        ):
            raise ValueError
        updated_at = datetime.fromisoformat(decoded["updated_at"])
        if updated_at.tzinfo is None:
            raise ValueError
        return _AgentTaskCursor(
            session_id=session_id,
            include_terminal=decoded["include_terminal"],
            updated_at=_normalize_task_cursor_datetime(updated_at),
            task_id=decoded["id"],
        )
    except (TypeError, ValueError, UnicodeDecodeError, json.JSONDecodeError) as exc:
        raise BusinessValidationError("Agent Task 分页 cursor 无效") from exc


def _normalize_task_cursor_datetime(value: datetime) -> datetime:
    normalized = value if value.tzinfo is not None else value.replace(tzinfo=UTC)
    return normalized.astimezone(UTC)


def _get_task_for_update(session: Session, task_id: str) -> AgentTask:
    task = session.scalar(select(AgentTask).where(AgentTask.id == task_id).with_for_update())
    if task is None:
        raise NotFoundError("Agent Task 不存在")
    return task


def _task_is_product_goal(session: Session | None, task: AgentTask) -> bool:
    conversation = task.conversation
    if conversation is None and session is not None and task.conversation_id is not None:
        conversation = session.get(AgentConversation, task.conversation_id)
    return (
        conversation is not None
        and conversation.scope_type == AgentConversationScope.PRODUCT_WORKFLOW
    )


def _task_has_busy_harness_turn(
    session: Session,
    task: AgentTask,
    *,
    projection: AgentTurnProjection | None = None,
) -> bool:
    if task.status == AgentTaskStatus.RUNNING:
        return True
    current = projection
    if current is None and task.current_turn_id is not None:
        current = session.get(AgentTurnProjection, task.current_turn_id)
    return current is not None and current.status in _BUSY_HARNESS_TURN_STATUSES


def _conversation_for_task(session: Session, conversation_id: str | None) -> AgentConversation | None:
    if conversation_id is None:
        return None
    conversation = session.scalar(
        select(AgentConversation).where(AgentConversation.id == conversation_id)
    )
    if conversation is None:
        raise NotFoundError("Agent conversation 不存在")
    return conversation


def _validate_task_session(agent_session: AgentSession, conversation: AgentConversation | None) -> None:
    if conversation is not None and conversation.session_id != agent_session.id:
        raise ConflictError("Agent conversation 不属于当前 Session")


def _normalize_task_title(value: str) -> str:
    normalized = value.strip()
    if not normalized:
        raise BusinessValidationError("Agent Task 标题不能为空")
    if len(normalized) > AGENT_TASK_TITLE_MAX_LENGTH:
        raise BusinessValidationError(f"Agent Task 标题不能超过 {AGENT_TASK_TITLE_MAX_LENGTH} 个字符")
    return normalized


def _normalize_task_goal(value: str) -> str:
    normalized = value.strip()
    if not normalized:
        raise BusinessValidationError("Agent Task 目标不能为空")
    if len(normalized) > AGENT_TASK_GOAL_MAX_LENGTH:
        raise BusinessValidationError(f"Agent Task 目标不能超过 {AGENT_TASK_GOAL_MAX_LENGTH} 个字符")
    return normalized


def _bounded_string(value: Any, limit: int, label: str) -> str:
    if not isinstance(value, str) or not value.strip():
        raise BusinessValidationError(f"{label}不能为空")
    normalized = value.strip()
    if len(normalized) > limit:
        raise BusinessValidationError(f"{label}不能超过 {limit} 个字符")
    return normalized


def _optional_identifier(value: Any, label: str) -> str | None:
    if value is None:
        return None
    return _bounded_string(value, 64, label)


def _bounded_identifiers(value: Any, label: str) -> list[str]:
    if not isinstance(value, list) or len(value) > AGENT_CONTEXT_MAX_ASSET_IDS:
        raise BusinessValidationError(f"页面 {label} 必须是有界 ID 列表")
    normalized = [_bounded_string(item, 64, label) for item in value]
    if len(set(normalized)) != len(normalized):
        raise BusinessValidationError(f"页面 {label} 不能包含重复 ID")
    return normalized


def _optional_non_negative_int(value: Any, label: str) -> int | None:
    if value is None:
        return None
    if not isinstance(value, int) or isinstance(value, bool) or value < 0:
        raise BusinessValidationError(f"{label} 必须是非负整数")
    return value


def _parse_captured_at(value: Any) -> datetime:
    if isinstance(value, datetime):
        parsed = value
    elif isinstance(value, str):
        try:
            parsed = datetime.fromisoformat(value)
        except ValueError as exc:
            raise BusinessValidationError("页面 captured_at 无效") from exc
    else:
        raise BusinessValidationError("页面 captured_at 必须是时间")
    if parsed.tzinfo is None:
        raise BusinessValidationError("页面 captured_at 必须包含时区")
    return parsed.astimezone(UTC)


__all__ = [
    "AGENT_CONTEXT_MAX_ASSET_IDS",
    "AGENT_TASK_GOAL_MAX_LENGTH",
    "AGENT_TASK_SUMMARY_MAX_LENGTH",
    "AGENT_TASK_CURSOR_VERSION",
    "AGENT_TASK_LIST_DEFAULT_LIMIT",
    "AGENT_TASK_LIST_MAX_LIMIT",
    "AGENT_TASK_TITLE_MAX_LENGTH",
    "AgentTaskPage",
    "AgentTaskResumeResult",
    "apply_graph_run_status_to_task",
    "create_agent_task",
    "create_page_context_snapshot",
    "cancel_agent_task",
    "complete_agent_task",
    "pause_agent_task",
    "resume_agent_task",
    "ensure_task_for_turn",
    "get_agent_task_or_raise",
    "initial_agent_task_turn_idempotency_key",
    "list_agent_tasks",
    "new_agent_task",
    "normalize_page_context",
    "page_context_digest",
    "refresh_agent_session_summary",
    "rename_agent_task",
    "task_contract",
    "update_agent_task_from_turn",
]
