"""Agent Turn 投影的查找、预留、绑定与状态转换。"""

from __future__ import annotations

import base64
import json
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Any

from sqlalchemy import and_, or_, select
from sqlalchemy.exc import IntegrityError
from sqlalchemy.orm import Session, selectinload

from productflow_backend.application.agent.conversations import (
    GLOBAL_SCOPE,
    PRODUCT_WORKFLOW_SCOPE,
    get_agent_conversation_or_raise,
)
from productflow_backend.application.agent.idempotency import (
    DEFAULT_IDEMPOTENCY_KEY_MAX_BYTES,
    canonical_json_request_hash,
    normalize_idempotency_key,
)
from productflow_backend.application.agent.tasks import (
    create_page_context_snapshot,
    ensure_task_for_turn,
    normalize_page_context,
    update_agent_task_from_turn,
)
from productflow_backend.application.agent.turn_status import TERMINAL_TURN_STATUSES
from productflow_backend.application.time import now_utc
from productflow_backend.domain.enums import (
    AgentConversationStatus,
    AgentTurnStatus,
    LibraryOrganizationDraftStatus,
    MediaVerificationStatus,
    WorkflowDraftStatus,
)
from productflow_backend.domain.errors import BusinessValidationError, ConflictError, NotFoundError
from productflow_backend.infrastructure.db.models import (
    AgentConversation,
    AgentTask,
    AgentTurnProjection,
    ProductImageAsset,
    WorkflowDraft,
)

AGENT_MAX_INPUT_ASSETS = 6
AGENT_MAX_INPUT_TEXT_CHARS = 20_000
AGENT_MAX_IDEMPOTENCY_KEY_BYTES = DEFAULT_IDEMPOTENCY_KEY_MAX_BYTES
AGENT_TURN_CURSOR_VERSION = 1
AGENT_TURN_DEFAULT_PAGE_SIZE = 20
AGENT_TURN_MAX_PAGE_SIZE = 50

_STARTABLE_CONVERSATION_STATUSES = {
    AgentConversationStatus.COLLECTING,
    AgentConversationStatus.AWAITING_CONFIRMATION,
    AgentConversationStatus.FAILED,
    AgentConversationStatus.CANCELED,
    AgentConversationStatus.UNKNOWN,
    AgentConversationStatus.COMPLETED,
}


@dataclass(frozen=True, slots=True)
class AgentTurnReservation:
    projection: AgentTurnProjection
    created: bool


@dataclass(frozen=True, slots=True)
class AgentTurnPage:
    items: list[AgentTurnProjection]
    next_cursor: str | None


@dataclass(frozen=True, slots=True)
class _AgentTurnCursor:
    conversation_id: str
    created_at: datetime
    projection_id: str


def expected_harness_run_id(conversation: AgentConversation, projection: AgentTurnProjection) -> str:
    """Task Turn 用 Task 的 harness_run_id；否则用 Conversation 的 run。"""
    task = projection.task
    if task is not None:
        return task.harness_run_id
    return conversation.harness_run_id


def list_agent_turn_page(
    session: Session,
    *,
    product_id: str | None,
    conversation_id: str,
    task_id: str | None = None,
    after: str = "",
    limit: int = AGENT_TURN_DEFAULT_PAGE_SIZE,
) -> AgentTurnPage:
    get_agent_conversation_or_raise(
        session,
        product_id=product_id,
        conversation_id=conversation_id,
    )
    if not 1 <= limit <= AGENT_TURN_MAX_PAGE_SIZE:
        raise BusinessValidationError(
            f"Agent Turn 分页 limit 必须在 1 到 {AGENT_TURN_MAX_PAGE_SIZE} 之间"
        )
    cursor = _decode_agent_turn_cursor(after) if after.strip() else None
    if cursor is not None and cursor.conversation_id != conversation_id:
        raise BusinessValidationError("Agent Turn 分页 cursor 与当前 conversation 不匹配")
    if task_id is not None:
        task = session.get(AgentTask, task_id)
        if task is None or task.conversation_id != conversation_id:
            raise ConflictError("Agent Task 与当前 conversation 不匹配")

    statement = (
        select(AgentTurnProjection)
        .options(
            selectinload(AgentTurnProjection.conversation),
            selectinload(AgentTurnProjection.task),
        )
        .where(AgentTurnProjection.conversation_id == conversation_id)
        .order_by(AgentTurnProjection.created_at.desc(), AgentTurnProjection.id.desc())
    )
    if task_id is not None:
        statement = statement.where(AgentTurnProjection.task_id == task_id)
    if cursor is not None:
        statement = statement.where(
            or_(
                AgentTurnProjection.created_at < cursor.created_at,
                and_(
                    AgentTurnProjection.created_at == cursor.created_at,
                    AgentTurnProjection.id < cursor.projection_id,
                ),
            )
        )
    rows = list(session.scalars(statement.limit(limit + 1)).all())
    has_more = len(rows) > limit
    selected = rows[:limit]
    next_cursor = None
    if has_more and selected:
        oldest = selected[-1]
        next_cursor = _encode_agent_turn_cursor(
            _AgentTurnCursor(
                conversation_id=conversation_id,
                created_at=_normalize_cursor_datetime(oldest.created_at),
                projection_id=oldest.id,
            )
        )
    return AgentTurnPage(items=list(reversed(selected)), next_cursor=next_cursor)


def _encode_agent_turn_cursor(cursor: _AgentTurnCursor) -> str:
    encoded = json.dumps(
        {
            "v": AGENT_TURN_CURSOR_VERSION,
            "conversation_id": cursor.conversation_id,
            "created_at": _normalize_cursor_datetime(cursor.created_at).isoformat(),
            "id": cursor.projection_id,
        },
        sort_keys=True,
        separators=(",", ":"),
    ).encode()
    return base64.urlsafe_b64encode(encoded).decode().rstrip("=")


def _decode_agent_turn_cursor(value: str) -> _AgentTurnCursor:
    normalized = value.strip()
    if len(normalized) > 4096:
        raise BusinessValidationError("Agent Turn 分页 cursor 无效")
    try:
        padded = normalized + "=" * (-len(normalized) % 4)
        raw = base64.b64decode(padded, altchars=b"-_", validate=True)
        decoded = json.loads(raw)
        if not isinstance(decoded, dict) or set(decoded) != {"v", "conversation_id", "created_at", "id"}:
            raise ValueError
        if decoded["v"] != AGENT_TURN_CURSOR_VERSION:
            raise ValueError
        if not all(
            isinstance(decoded[field], str) and decoded[field]
            for field in ("conversation_id", "created_at", "id")
        ):
            raise ValueError
        created_at = datetime.fromisoformat(decoded["created_at"])
        if created_at.tzinfo is None:
            raise ValueError
        return _AgentTurnCursor(
            conversation_id=decoded["conversation_id"],
            created_at=_normalize_cursor_datetime(created_at),
            projection_id=decoded["id"],
        )
    except (TypeError, ValueError, UnicodeDecodeError, json.JSONDecodeError) as exc:
        raise BusinessValidationError("Agent Turn 分页 cursor 无效") from exc


def _normalize_cursor_datetime(value: datetime) -> datetime:
    normalized = value if value.tzinfo is not None else value.replace(tzinfo=UTC)
    return normalized.astimezone(UTC)


def get_agent_turn_or_raise(
    session: Session,
    *,
    product_id: str | None,
    conversation_id: str,
    projection_id: str,
) -> AgentTurnProjection:
    projection = session.scalar(
        select(AgentTurnProjection)
        .options(
            selectinload(AgentTurnProjection.conversation),
            selectinload(AgentTurnProjection.task),
        )
        .join(AgentConversation, AgentConversation.id == AgentTurnProjection.conversation_id)
        .where(
            AgentTurnProjection.id == projection_id,
            AgentTurnProjection.conversation_id == conversation_id,
            (
                AgentConversation.scope_type == GLOBAL_SCOPE
                if product_id is None
                else and_(
                    AgentConversation.scope_type == PRODUCT_WORKFLOW_SCOPE,
                    AgentConversation.product_id == product_id,
                )
            ),
        )
    )
    if projection is None:
        get_agent_conversation_or_raise(
            session,
            product_id=product_id,
            conversation_id=conversation_id,
        )
        raise NotFoundError("Agent turn 不存在")
    return projection


def lock_agent_turn_or_raise(
    session: Session,
    *,
    product_id: str | None,
    conversation_id: str,
    projection_id: str,
) -> AgentTurnProjection:
    return _get_agent_turn_for_update(
        session,
        product_id=product_id,
        conversation_id=conversation_id,
        projection_id=projection_id,
    )


def reserve_agent_turn(
    session: Session,
    *,
    product_id: str | None,
    conversation_id: str,
    input_text: str,
    input_asset_ids: list[str],
    idempotency_key: str,
    task_id: str | None = None,
    page_context: dict[str, Any] | None = None,
    ignore_turn_id: str | None = None,
) -> AgentTurnReservation:
    """预留 Turn。同 key 同 hash 回放已有行。本函数 commit。"""
    normalized_text = _normalize_input_text(input_text)
    normalized_asset_ids = _normalize_input_asset_ids(input_asset_ids)
    normalized_key = normalize_idempotency_key(
        idempotency_key,
        max_bytes=AGENT_MAX_IDEMPOTENCY_KEY_BYTES,
    )
    normalized_context = normalize_page_context(page_context) if page_context is not None else None
    request_hash = _turn_request_hash(
        conversation_id=conversation_id,
        task_id=task_id,
        input_text=normalized_text,
        input_asset_ids=normalized_asset_ids,
    )

    conversation = session.scalar(
        select(AgentConversation)
        .where(
            AgentConversation.id == conversation_id,
            (
                AgentConversation.scope_type == GLOBAL_SCOPE
                if product_id is None
                else and_(
                    AgentConversation.scope_type == PRODUCT_WORKFLOW_SCOPE,
                    AgentConversation.product_id == product_id,
                )
            ),
        )
        .with_for_update()
    )
    if conversation is None:
        get_agent_conversation_or_raise(
            session,
            product_id=product_id,
            conversation_id=conversation_id,
        )
        raise AssertionError("unreachable")
    if conversation.status not in _STARTABLE_CONVERSATION_STATUSES:
        raise ConflictError("当前 Agent conversation 状态不允许创建 Turn")

    existing = session.scalar(
        select(AgentTurnProjection).where(
            AgentTurnProjection.conversation_id == conversation_id,
            AgentTurnProjection.idempotency_key == normalized_key,
        )
    )
    if existing is not None:
        if existing.request_hash != request_hash:
            raise ConflictError("同一 idempotency key 不能提交不同的 Agent Turn 请求")
        # 同 key 同 hash 回放也 commit，避免调用方误以为未落库。
        session.commit()
        return AgentTurnReservation(projection=existing, created=False)

    if conversation.scope_type == GLOBAL_SCOPE and conversation.session is not None:
        from productflow_backend.application.agent.sessions import auto_name_agent_session

        auto_name_agent_session(conversation.session, input_text=normalized_text)

    task = ensure_task_for_turn(
        session,
        conversation=conversation,
        task_id=task_id,
        ignore_turn_id=ignore_turn_id,
    )

    if conversation.scope_type == PRODUCT_WORKFLOW_SCOPE:
        if product_id is None:
            raise ConflictError("商品工作流 Agent conversation 缺少商品作用域")
        _validate_product_assets(
            session,
            product_id=product_id,
            asset_ids=normalized_asset_ids,
        )
    projection = AgentTurnProjection(
        conversation_id=conversation_id,
        task_id=task.id if task is not None else None,
        idempotency_key=normalized_key,
        request_hash=request_hash,
        input_text=normalized_text,
        input_asset_ids_json=normalized_asset_ids,
        status=AgentTurnStatus.QUEUED,
    )
    conversation.status = AgentConversationStatus.COLLECTING
    conversation.updated_at = now_utc()
    session.add(projection)
    try:
        session.flush()
        snapshot = None
        if normalized_context is not None:
            snapshot = create_page_context_snapshot(
                session,
                task=task,
                turn_id=projection.id,
                page_context=normalized_context,
            )
        if snapshot is not None:
            projection.page_context_snapshot_id = snapshot.id
        if task is not None:
            task.current_turn_id = projection.id
        session.commit()
    except IntegrityError:
        session.rollback()
        existing = session.scalar(
            select(AgentTurnProjection).where(
                AgentTurnProjection.conversation_id == conversation_id,
                AgentTurnProjection.idempotency_key == normalized_key,
            )
        )
        if existing is not None:
            if existing.request_hash != request_hash:
                raise ConflictError("同一 idempotency key 不能提交不同的 Agent Turn 请求") from None
            return AgentTurnReservation(projection=existing, created=False)
        raise
    session.refresh(projection)
    return AgentTurnReservation(projection=projection, created=True)


def bind_harness_turn(
    session: Session,
    *,
    product_id: str | None,
    conversation_id: str,
    projection_id: str,
    harness_turn_id: str,
    status: AgentTurnStatus,
    commit: bool = True,
) -> AgentTurnProjection:
    normalized_turn_id = harness_turn_id.strip()
    if not normalized_turn_id or len(normalized_turn_id) > 120:
        raise BusinessValidationError("harness turn ID 无效")
    projection = _get_agent_turn_for_update(
        session,
        product_id=product_id,
        conversation_id=conversation_id,
        projection_id=projection_id,
    )
    if projection.harness_turn_id is not None and projection.harness_turn_id != normalized_turn_id:
        raise ConflictError("Agent turn projection 已绑定其他 harness Turn")
    projection.harness_turn_id = normalized_turn_id
    projection.status = status
    projection.updated_at = now_utc()
    _apply_conversation_status(projection.conversation, status)
    update_agent_task_from_turn(
        session,
        projection=projection,
        status=status,
        error_text=None,
        finished_at=None,
    )
    try:
        if commit:
            session.commit()
    except IntegrityError as exc:
        session.rollback()
        raise ConflictError("harness Turn 已绑定其他 Agent turn projection") from exc
    if commit:
        session.refresh(projection)
    return projection


def cancel_unbound_agent_turn(
    session: Session,
    *,
    product_id: str | None,
    conversation_id: str,
    projection_id: str,
    commit: bool = True,
) -> AgentTurnProjection:
    """在 Agent runtime 接受前取消已预留、尚未绑定的 Turn。commit=False 时由调用方持有事务。"""
    projection = _get_agent_turn_for_update(
        session,
        product_id=product_id,
        conversation_id=conversation_id,
        projection_id=projection_id,
    )
    if projection.harness_turn_id is not None:
        raise ConflictError("Agent Turn 已绑定 runtime Turn")
    if projection.status in TERMINAL_TURN_STATUSES:
        return projection
    if projection.status not in {AgentTurnStatus.QUEUED, AgentTurnStatus.CANCEL_REQUESTED}:
        raise ConflictError("当前 Agent Turn 状态不允许在未绑定 runtime 时取消")

    finished_at = now_utc()
    projection.status = AgentTurnStatus.CANCELED
    projection.resume_required = False
    projection.error_text = None
    projection.question_json = None
    projection.sync_error = None
    projection.finished_at = finished_at
    projection.updated_at = finished_at
    _apply_conversation_status(projection.conversation, AgentTurnStatus.CANCELED)
    update_agent_task_from_turn(
        session,
        projection=projection,
        status=AgentTurnStatus.CANCELED,
        error_text=None,
        finished_at=finished_at,
    )
    if commit:
        session.commit()
        session.refresh(projection)
    return projection


def record_agent_turn_start_error(
    session: Session,
    *,
    product_id: str | None,
    conversation_id: str,
    projection_id: str,
    safe_error: str,
) -> AgentTurnProjection:
    projection = _get_agent_turn_for_update(
        session,
        product_id=product_id,
        conversation_id=conversation_id,
        projection_id=projection_id,
    )
    projection.sync_error = _bounded_optional_text(safe_error, limit=2_000)
    projection.updated_at = now_utc()
    session.commit()
    session.refresh(projection)
    return projection


def project_agent_turn_state(
    session: Session,
    *,
    product_id: str | None,
    conversation_id: str,
    projection_id: str,
    harness_turn_id: str,
    status: AgentTurnStatus,
    output_text: str | None,
    error_text: str | None,
    question_json: dict[str, Any] | None,
    finished_at: datetime | None,
    tool_steps_json: list[dict[str, Any]] | None = None,
    commit: bool = True,
) -> AgentTurnProjection:
    projection = _get_agent_turn_for_update(
        session,
        product_id=product_id,
        conversation_id=conversation_id,
        projection_id=projection_id,
    )
    _validate_harness_turn_binding(projection, harness_turn_id)
    # Draft 已确认后，迟到的 awaiting_confirmation 不能盖住 SUCCEEDED。
    stale_confirmed_workflow_turn = (
        status == AgentTurnStatus.AWAITING_CONFIRMATION
        and is_confirmed_workflow_draft_turn(projection)
    )
    projected_status = AgentTurnStatus.SUCCEEDED if stale_confirmed_workflow_turn else status
    projection.status = projected_status
    projection.output_text = _bounded_optional_text(output_text, limit=100_000)
    projection.error_text = _bounded_optional_text(error_text, limit=4_000)
    projection.question_json = dict(question_json) if question_json is not None else None
    if tool_steps_json is not None:
        projection.tool_steps_json = [dict(item) for item in tool_steps_json]
    projection.sync_error = None
    projection.finished_at = finished_at
    projection.updated_at = now_utc()
    preserve_newer_conversation_status = (
        stale_confirmed_workflow_turn
        and projection.conversation.status
        not in {
            AgentConversationStatus.AWAITING_CONFIRMATION,
            AgentConversationStatus.COMPLETED,
        }
    )
    if not preserve_newer_conversation_status:
        _apply_conversation_status(projection.conversation, projected_status)
    update_agent_task_from_turn(
        session,
        projection=projection,
        status=projected_status,
        error_text=projection.error_text,
        finished_at=finished_at,
    )
    if commit:
        session.commit()
        session.refresh(projection)
    return projection


def set_agent_turn_resume_required(
    session: Session,
    *,
    product_id: str | None,
    conversation_id: str,
    projection_id: str,
    required: bool,
    commit: bool = True,
) -> AgentTurnProjection:
    projection = _get_agent_turn_for_update(
        session,
        product_id=product_id,
        conversation_id=conversation_id,
        projection_id=projection_id,
    )
    projection.resume_required = required
    projection.updated_at = now_utc()
    if commit:
        session.commit()
        session.refresh(projection)
    return projection


def _get_agent_turn_for_update(
    session: Session,
    *,
    product_id: str | None,
    conversation_id: str,
    projection_id: str,
) -> AgentTurnProjection:
    projection = session.scalar(
        select(AgentTurnProjection)
        .join(AgentConversation, AgentConversation.id == AgentTurnProjection.conversation_id)
        .options(
            selectinload(AgentTurnProjection.conversation)
            .selectinload(AgentConversation.workflow_draft)
            .selectinload(WorkflowDraft.current_revision),
            selectinload(AgentTurnProjection.task),
        )
        .where(
            AgentTurnProjection.id == projection_id,
            AgentTurnProjection.conversation_id == conversation_id,
            (
                AgentConversation.scope_type == GLOBAL_SCOPE
                if product_id is None
                else and_(
                    AgentConversation.scope_type == PRODUCT_WORKFLOW_SCOPE,
                    AgentConversation.product_id == product_id,
                )
            ),
        )
        .with_for_update()
    )
    if projection is None:
        get_agent_conversation_or_raise(
            session,
            product_id=product_id,
            conversation_id=conversation_id,
        )
        raise NotFoundError("Agent turn 不存在")
    return projection


def _validate_product_assets(session: Session, *, product_id: str, asset_ids: list[str]) -> None:
    if not asset_ids:
        return
    assets = list(
        session.scalars(
            select(ProductImageAsset)
            .options(selectinload(ProductImageAsset.media_object))
            .where(ProductImageAsset.id.in_(asset_ids), ProductImageAsset.product_id == product_id)
        ).all()
    )
    if len(assets) != len(asset_ids):
        raise BusinessValidationError("参考图片必须属于当前商品")
    if any(asset.media_object.verification_status != MediaVerificationStatus.VERIFIED for asset in assets):
        raise BusinessValidationError("参考图片必须已通过媒体核验")


def _normalize_input_text(value: str) -> str:
    normalized = value.strip()
    if not normalized:
        raise BusinessValidationError("Agent Turn 输入不能为空")
    if len(normalized) > AGENT_MAX_INPUT_TEXT_CHARS:
        raise BusinessValidationError(f"Agent Turn 输入不能超过 {AGENT_MAX_INPUT_TEXT_CHARS} 个字符")
    return normalized


def _normalize_input_asset_ids(values: list[str]) -> list[str]:
    normalized = [value.strip() for value in values]
    if any(not value for value in normalized):
        raise BusinessValidationError("参考图片 ID 不能为空")
    if len(normalized) > AGENT_MAX_INPUT_ASSETS:
        raise BusinessValidationError(f"单个 Agent Turn 最多选择 {AGENT_MAX_INPUT_ASSETS} 张参考图片")
    if len(set(normalized)) != len(normalized):
        raise BusinessValidationError("同一 Agent Turn 不能重复选择参考图片")
    return normalized


def _turn_request_hash(
    *,
    conversation_id: str,
    task_id: str | None,
    input_text: str,
    input_asset_ids: list[str],
) -> str:
    return canonical_json_request_hash(
        {
            "schema_version": 1,
            "conversation_id": conversation_id,
            "task_id": task_id,
            "input_text": input_text,
            "input_asset_ids": input_asset_ids,
        }
    )


def _validate_harness_turn_binding(projection: AgentTurnProjection, harness_turn_id: str) -> str:
    normalized = harness_turn_id.strip()
    if not normalized or projection.harness_turn_id != normalized:
        raise ConflictError("harness Turn 与 Agent turn projection 不匹配")
    return normalized


def _apply_conversation_status(
    conversation: AgentConversation,
    turn_status: AgentTurnStatus,
) -> None:
    if turn_status == AgentTurnStatus.AWAITING_CONFIRMATION:
        conversation.status = AgentConversationStatus.AWAITING_CONFIRMATION
    elif turn_status == AgentTurnStatus.SUCCEEDED:
        if conversation.scope_type == GLOBAL_SCOPE:
            pending_library_draft = conversation.library_organization_draft
            conversation.status = (
                AgentConversationStatus.AWAITING_CONFIRMATION
                if pending_library_draft is not None
                and pending_library_draft.status == LibraryOrganizationDraftStatus.AWAITING_CONFIRMATION
                else AgentConversationStatus.COMPLETED
            )
        else:
            draft = conversation.workflow_draft
            conversation.status = (
                AgentConversationStatus.AWAITING_CONFIRMATION
                if draft is not None and draft.status == WorkflowDraftStatus.AWAITING_CONFIRMATION
                else AgentConversationStatus.COMPLETED
            )
    elif turn_status == AgentTurnStatus.FAILED:
        conversation.status = AgentConversationStatus.FAILED
    elif turn_status == AgentTurnStatus.CANCELED:
        conversation.status = AgentConversationStatus.CANCELED
    elif turn_status == AgentTurnStatus.UNKNOWN:
        conversation.status = AgentConversationStatus.UNKNOWN
    else:
        conversation.status = AgentConversationStatus.COLLECTING
    conversation.updated_at = now_utc()


def is_confirmed_workflow_draft_turn(projection: AgentTurnProjection) -> bool:
    revision = projection.workflow_draft_revision
    draft = revision.draft if revision is not None else None
    return (
        draft is not None
        and draft.status
        in {
            WorkflowDraftStatus.CONFIRMED,
            WorkflowDraftStatus.MATERIALIZING,
            WorkflowDraftStatus.READY,
        }
    )


def _bounded_optional_text(value: str | None, *, limit: int) -> str | None:
    if value is None:
        return None
    return value[:limit]


__all__ = [
    "AGENT_MAX_IDEMPOTENCY_KEY_BYTES",
    "AGENT_MAX_INPUT_ASSETS",
    "AGENT_MAX_INPUT_TEXT_CHARS",
    "AGENT_TURN_DEFAULT_PAGE_SIZE",
    "AGENT_TURN_MAX_PAGE_SIZE",
    "AgentTurnPage",
    "AgentTurnReservation",
    "bind_harness_turn",
    "cancel_unbound_agent_turn",
    "expected_harness_run_id",
    "get_agent_turn_or_raise",
    "is_confirmed_workflow_draft_turn",
    "list_agent_turn_page",
    "lock_agent_turn_or_raise",
    "project_agent_turn_state",
    "record_agent_turn_start_error",
    "reserve_agent_turn",
    "set_agent_turn_resume_required",
]
