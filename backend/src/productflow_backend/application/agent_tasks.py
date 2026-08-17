from __future__ import annotations

import hashlib
import json
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Any

from sqlalchemy import select
from sqlalchemy.orm import Session, selectinload

from productflow_backend.application.agent_sessions import get_agent_session_or_raise
from productflow_backend.application.time import now_utc
from productflow_backend.domain.enums import AgentConversationStatus, AgentTaskStatus, AgentTurnStatus
from productflow_backend.domain.errors import BusinessValidationError, ConflictError, NotFoundError
from productflow_backend.infrastructure.db.models import (
    AgentConversation,
    AgentPageContextSnapshot,
    AgentSession,
    AgentTask,
    AgentTurnProjection,
    new_id,
)

AGENT_TASK_TITLE_MAX_LENGTH = 160
AGENT_TASK_GOAL_MAX_LENGTH = 20_000
AGENT_TASK_LIST_DEFAULT_LIMIT = 50
AGENT_TASK_LIST_MAX_LIMIT = 100
AGENT_CONTEXT_ROUTE_MAX_LENGTH = 512
AGENT_CONTEXT_PAGE_TYPE_MAX_LENGTH = 80
AGENT_CONTEXT_MAX_ASSET_IDS = 100
AGENT_CONTEXT_MAX_FILTERS = 20

_ACTIVE_TASK_STATUSES = {
    AgentTaskStatus.QUEUED,
    AgentTaskStatus.RUNNING,
    AgentTaskStatus.WAITING_USER,
    AgentTaskStatus.AWAITING_CONFIRMATION,
}
_TERMINAL_TASK_STATUSES = {
    AgentTaskStatus.SUCCEEDED,
    AgentTaskStatus.FAILED,
    AgentTaskStatus.CANCELED,
    AgentTaskStatus.UNKNOWN,
}
_ACTIVE_TURN_STATUSES = {
    AgentTurnStatus.QUEUED,
    AgentTurnStatus.RUNNING,
    AgentTurnStatus.REQUIRES_INPUT,
    AgentTurnStatus.AWAITING_CONFIRMATION,
    AgentTurnStatus.CANCEL_REQUESTED,
}


@dataclass(frozen=True, slots=True)
class AgentTaskPage:
    items: list[AgentTask]


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
        workflow_draft_id=conversation.workflow_draft_id if conversation is not None else None,
        harness_run_id=task_id,
        title=normalized_title,
        goal=normalized_goal,
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
    session.commit()
    return get_agent_task_or_raise(session, task.id)


def get_agent_task_or_raise(session: Session, task_id: str) -> AgentTask:
    task = session.scalar(
        select(AgentTask)
        .options(
            selectinload(AgentTask.session),
            selectinload(AgentTask.conversation),
        )
        .where(AgentTask.id == task_id)
    )
    if task is None:
        raise NotFoundError("Agent Task 不存在")
    return task


def list_agent_tasks(
    session: Session,
    *,
    session_id: str | None = None,
    include_terminal: bool = True,
    limit: int = AGENT_TASK_LIST_DEFAULT_LIMIT,
) -> AgentTaskPage:
    if not 1 <= limit <= AGENT_TASK_LIST_MAX_LIMIT:
        raise BusinessValidationError(
            f"Agent Task 列表 limit 必须在 1 到 {AGENT_TASK_LIST_MAX_LIMIT} 之间"
        )
    statement = select(AgentTask).options(
        selectinload(AgentTask.session),
        selectinload(AgentTask.conversation),
    )
    if session_id is not None:
        statement = statement.where(AgentTask.session_id == session_id)
    if not include_terminal:
        statement = statement.where(AgentTask.status.not_in(_TERMINAL_TASK_STATUSES))
    rows = list(
        session.scalars(
            statement.order_by(AgentTask.updated_at.desc(), AgentTask.id.desc()).limit(limit)
        ).all()
    )
    return AgentTaskPage(items=rows)


def rename_agent_task(session: Session, *, task_id: str, title: str) -> AgentTask:
    task = _get_task_for_update(session, task_id)
    task.title = _normalize_task_title(title)
    task.updated_at = now_utc()
    session.commit()
    return get_agent_task_or_raise(session, task.id)


def cancel_agent_task(session: Session, *, task_id: str) -> AgentTask:
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
        session.commit()
    return get_agent_task_or_raise(session, task.id)


def task_contract(session: Session, task_id: str) -> tuple[AgentTask, AgentConversation]:
    task = session.scalar(
        select(AgentTask)
        .options(
            selectinload(AgentTask.conversation).selectinload(AgentConversation.workflow_draft),
        )
        .where(AgentTask.id == task_id)
    )
    if task is None:
        raise NotFoundError("Agent Task 不存在")
    if task.conversation is None:
        raise ConflictError("当前 Agent Task 尚未绑定可执行的商品工作区")
    if task.status == AgentTaskStatus.CANCELED:
        raise ConflictError("已取消的 Agent Task 不能继续运行")
    return task, task.conversation


def ensure_task_for_turn(
    session: Session,
    *,
    conversation: AgentConversation,
    task_id: str | None,
) -> AgentTask | None:
    if conversation.session_id is None:
        raise ConflictError("Agent conversation 尚未绑定 Session")
    agent_session = get_agent_session_or_raise(session, conversation.session_id)
    if task_id is None:
        return None

    task = _get_task_for_update(session, task_id)
    if task.session_id != agent_session.id or task.conversation_id != conversation.id:
        raise ConflictError("Agent Task 与当前商品工作区不匹配")
    if task.status == AgentTaskStatus.CANCELED:
        raise ConflictError("已取消的 Agent Task 不能继续提交 Turn")
    if task.status == AgentTaskStatus.PAUSED:
        raise ConflictError("已暂停的 Agent Task 需要恢复后才能提交 Turn")
    if task.current_turn_id is not None:
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
    if projection.task_id is None:
        return None
    task = session.get(AgentTask, projection.task_id, with_for_update=True)
    if task is None:
        raise ConflictError("Agent Turn 绑定的 Task 不存在")
    now = now_utc()
    task.current_turn_id = projection.id
    task.updated_at = now
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
        task.status = AgentTaskStatus.SUCCEEDED
        task.waiting_reason = None
        task.failure_reason = None
        task.finished_at = finished_at or now
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
    elif status == AgentTurnStatus.UNKNOWN:
        task.status = AgentTaskStatus.UNKNOWN
        task.waiting_reason = None
        task.failure_reason = error_text
        task.finished_at = finished_at or now
    return task


def _get_task_for_update(session: Session, task_id: str) -> AgentTask:
    task = session.scalar(select(AgentTask).where(AgentTask.id == task_id).with_for_update())
    if task is None:
        raise NotFoundError("Agent Task 不存在")
    return task


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
    "AGENT_TASK_LIST_DEFAULT_LIMIT",
    "AGENT_TASK_LIST_MAX_LIMIT",
    "AGENT_TASK_TITLE_MAX_LENGTH",
    "AgentTaskPage",
    "create_agent_task",
    "create_page_context_snapshot",
    "cancel_agent_task",
    "ensure_task_for_turn",
    "get_agent_task_or_raise",
    "list_agent_tasks",
    "new_agent_task",
    "normalize_page_context",
    "page_context_digest",
    "rename_agent_task",
    "task_contract",
    "update_agent_task_from_turn",
]
