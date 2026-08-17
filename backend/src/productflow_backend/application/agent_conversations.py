from __future__ import annotations

import base64
import hashlib
import json
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Any

from sqlalchemy import and_, or_, select
from sqlalchemy.exc import IntegrityError
from sqlalchemy.orm import Session, selectinload

from productflow_backend.application.agent_sessions import new_agent_session
from productflow_backend.application.agent_tasks import (
    create_page_context_snapshot,
    ensure_task_for_turn,
    normalize_page_context,
    update_agent_task_from_turn,
)
from productflow_backend.application.time import now_utc
from productflow_backend.application.workflow_drafts.service import (
    append_workflow_draft_revision,
    parse_workflow_draft_payload_or_raise,
    validate_workflow_draft_for_confirmation,
    workflow_draft_query,
)
from productflow_backend.domain.enums import (
    AgentConversationScope,
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
    LibraryOrganizationDraft,
    Product,
    ProductImageAsset,
    WorkflowDraft,
    WorkflowDraftLegacyArchiveSeed,
    WorkflowDraftRecipeSeed,
    WorkflowDraftRevision,
    new_id,
)

AGENT_MAX_INPUT_ASSETS = 6
AGENT_MAX_INPUT_TEXT_CHARS = 20_000
AGENT_MAX_IDEMPOTENCY_KEY_BYTES = 200
AGENT_TURN_CURSOR_VERSION = 1
AGENT_TURN_DEFAULT_PAGE_SIZE = 20
AGENT_TURN_MAX_PAGE_SIZE = 50
WORKFLOW_DRAFT_ARTIFACT_NAME = "propose_workflow_draft"

PRODUCT_WORKFLOW_SCOPE = AgentConversationScope.PRODUCT_WORKFLOW
GLOBAL_SCOPE = AgentConversationScope.GLOBAL

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


def agent_conversation_query():
    return select(AgentConversation).options(
        selectinload(AgentConversation.session),
        selectinload(AgentConversation.workflow_draft).selectinload(WorkflowDraft.current_revision),
        selectinload(AgentConversation.workflow_draft)
        .selectinload(WorkflowDraft.recipe_seed)
        .selectinload(WorkflowDraftRecipeSeed.recipe_version),
        selectinload(AgentConversation.workflow_draft)
        .selectinload(WorkflowDraft.legacy_archive_seed)
        .selectinload(WorkflowDraftLegacyArchiveSeed.workflow_archive),
        selectinload(AgentConversation.workflow_draft)
        .selectinload(WorkflowDraft.legacy_archive_seed)
        .selectinload(WorkflowDraftLegacyArchiveSeed.canvas_agent_archive),
        selectinload(AgentConversation.workflow_draft)
        .selectinload(WorkflowDraft.legacy_archive_seed)
        .selectinload(WorkflowDraftLegacyArchiveSeed.user_template_archive),
        selectinload(AgentConversation.library_organization_draft).selectinload(
            LibraryOrganizationDraft.current_revision
        ),
    )


def get_agent_conversation_or_raise(
    session: Session,
    *,
    product_id: str | None,
    conversation_id: str,
) -> AgentConversation:
    scope_filter = (
        AgentConversation.scope_type == GLOBAL_SCOPE
        if product_id is None
        else and_(
            AgentConversation.scope_type == PRODUCT_WORKFLOW_SCOPE,
            AgentConversation.product_id == product_id,
        )
    )
    conversation = session.scalar(
        agent_conversation_query().where(
            AgentConversation.id == conversation_id,
            scope_filter,
        )
    )
    if conversation is None:
        if product_id is not None and session.get(Product, product_id) is None:
            raise NotFoundError("商品不存在")
        raise NotFoundError("Agent conversation 不存在")
    return conversation


def get_agent_conversation_by_id_or_raise(session: Session, conversation_id: str) -> AgentConversation:
    conversation = session.scalar(
        agent_conversation_query().where(AgentConversation.id == conversation_id)
    )
    if conversation is None:
        raise NotFoundError("Agent conversation 不存在")
    return conversation


def create_agent_conversation(
    session: Session,
    *,
    product_id: str,
    workflow_draft_id: str,
) -> AgentConversation:
    product = session.get(Product, product_id)
    if product is None:
        raise NotFoundError("商品不存在")
    draft = session.scalar(
        select(WorkflowDraft).where(
            WorkflowDraft.id == workflow_draft_id,
            WorkflowDraft.product_id == product_id,
        )
    )
    if draft is None:
        raise NotFoundError("WorkflowDraft 不存在")
    existing = session.scalar(
        agent_conversation_query().where(AgentConversation.workflow_draft_id == workflow_draft_id)
    )
    if existing is not None:
        return existing
    if draft.status not in {WorkflowDraftStatus.COLLECTING, WorkflowDraftStatus.AWAITING_CONFIRMATION}:
        raise ConflictError("当前 WorkflowDraft 状态不允许创建 Agent conversation")

    conversation_id = new_id()
    agent_session = new_agent_session(title=product.name)
    session.add(agent_session)
    session.flush()
    conversation = AgentConversation(
        id=conversation_id,
        scope_type=PRODUCT_WORKFLOW_SCOPE,
        session_id=agent_session.id,
        product_id=product_id,
        workflow_draft_id=workflow_draft_id,
        harness_run_id=conversation_id,
        status=AgentConversationStatus.COLLECTING,
    )
    session.add(conversation)
    try:
        session.commit()
    except IntegrityError:
        session.rollback()
        existing = session.scalar(
            agent_conversation_query().where(AgentConversation.workflow_draft_id == workflow_draft_id)
        )
        if existing is not None:
            return existing
        raise
    return get_agent_conversation_or_raise(
        session,
        product_id=product_id,
        conversation_id=conversation.id,
    )


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
) -> AgentTurnReservation:
    normalized_text = _normalize_input_text(input_text)
    normalized_asset_ids = _normalize_input_asset_ids(input_asset_ids)
    normalized_key = _normalize_idempotency_key(idempotency_key)
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
        session.commit()
        return AgentTurnReservation(projection=existing, created=False)

    task = ensure_task_for_turn(
        session,
        conversation=conversation,
        task_id=task_id,
    )

    if conversation.scope_type == PRODUCT_WORKFLOW_SCOPE:
        draft = session.scalar(
            workflow_draft_query().where(WorkflowDraft.id == conversation.workflow_draft_id)
        )
        if draft is None:
            raise ConflictError("Agent conversation 绑定的 WorkflowDraft 不存在")
        if (
            draft.intake_json is None
            and draft.current_revision_id is None
            and draft.recipe_seed is None
            and draft.legacy_archive_seed is None
        ):
            raise ConflictError("请先完成商品图片需求和参考图，再启动 Agent Turn")

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
    projection.status = status
    projection.output_text = _bounded_optional_text(output_text, limit=100_000)
    projection.error_text = _bounded_optional_text(error_text, limit=4_000)
    projection.question_json = dict(question_json) if question_json is not None else None
    if tool_steps_json is not None:
        projection.tool_steps_json = [dict(item) for item in tool_steps_json]
    projection.sync_error = None
    projection.finished_at = finished_at
    projection.updated_at = now_utc()
    _apply_conversation_status(projection.conversation, status)
    update_agent_task_from_turn(
        session,
        projection=projection,
        status=status,
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


def attach_agent_workflow_draft_artifact(
    session: Session,
    *,
    product_id: str,
    conversation_id: str,
    projection_id: str,
    harness_turn_id: str,
    artifact_name: str,
    artifact_step_id: str,
    artifact_value: dict[str, Any],
    commit: bool = True,
) -> AgentTurnProjection:
    if artifact_name != WORKFLOW_DRAFT_ARTIFACT_NAME:
        raise BusinessValidationError("Agent 返回了不受支持的 required artifact")
    normalized_step_id = artifact_step_id.strip()
    if not normalized_step_id or len(normalized_step_id) > 120:
        raise BusinessValidationError("Agent artifact step ID 无效")
    projection = _get_agent_turn_for_update(
        session,
        product_id=product_id,
        conversation_id=conversation_id,
        projection_id=projection_id,
    )
    normalized_turn_id = _validate_harness_turn_binding(projection, harness_turn_id)
    if projection.status != AgentTurnStatus.AWAITING_CONFIRMATION:
        raise ConflictError("Agent Turn 尚未进入待确认状态")
    if projection.artifact_step_id is not None and projection.artifact_step_id != normalized_step_id:
        raise ConflictError("Agent turn projection 已绑定其他 artifact step")
    conversation = projection.conversation
    draft = conversation.workflow_draft
    expected_version = draft.current_revision.version if draft.current_revision is not None else 0
    draft_id = draft.id
    artifact = parse_workflow_draft_payload_or_raise(artifact_value)
    validate_workflow_draft_for_confirmation(
        session,
        product_id=product_id,
        artifact=artifact,
    )
    append_workflow_draft_revision(
        session,
        product_id=product_id,
        draft_id=draft_id,
        expected_draft_version=expected_version,
        payload=artifact,
        ready_for_confirmation=True,
        source_turn_id=normalized_turn_id,
        source_artifact_step_id=normalized_step_id,
        commit=commit,
    )

    revision = session.scalar(
        select(WorkflowDraftRevision).where(
            WorkflowDraftRevision.draft_id == draft_id,
            WorkflowDraftRevision.source_turn_id == normalized_turn_id,
            WorkflowDraftRevision.source_artifact_step_id == normalized_step_id,
        )
    )
    if revision is None:
        raise ConflictError("Agent artifact 未能同步为 WorkflowDraft revision")
    projection = _get_agent_turn_for_update(
        session,
        product_id=product_id,
        conversation_id=conversation_id,
        projection_id=projection_id,
    )
    if projection.workflow_draft_revision_id not in {None, revision.id}:
        raise ConflictError("Agent turn projection 已关联其他 WorkflowDraft revision")
    projection.artifact_name = artifact_name
    projection.artifact_step_id = normalized_step_id
    projection.workflow_draft_revision_id = revision.id
    projection.sync_error = None
    projection.updated_at = now_utc()
    projection.conversation.status = AgentConversationStatus.AWAITING_CONFIRMATION
    projection.conversation.updated_at = now_utc()
    if commit:
        session.commit()
        session.refresh(projection)
    return projection


def mark_agent_conversation_completed_for_draft(
    session: Session,
    *,
    product_id: str,
    workflow_draft_id: str,
) -> AgentConversation | None:
    conversation = session.scalar(
        select(AgentConversation)
        .options(selectinload(AgentConversation.workflow_draft))
        .where(
            AgentConversation.product_id == product_id,
            AgentConversation.workflow_draft_id == workflow_draft_id,
        )
        .with_for_update()
    )
    if conversation is None:
        return None
    if conversation.workflow_draft.status not in {
        WorkflowDraftStatus.CONFIRMED,
        WorkflowDraftStatus.MATERIALIZING,
        WorkflowDraftStatus.READY,
    }:
        raise ConflictError("WorkflowDraft 尚未确认")
    conversation.status = AgentConversationStatus.COMPLETED
    conversation.updated_at = now_utc()
    session.commit()
    return conversation


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


def _normalize_idempotency_key(value: str) -> str:
    normalized = value.strip()
    if not normalized:
        raise BusinessValidationError("idempotency key 不能为空")
    if len(normalized.encode("utf-8")) > AGENT_MAX_IDEMPOTENCY_KEY_BYTES:
        raise BusinessValidationError(
            f"idempotency key 不能超过 {AGENT_MAX_IDEMPOTENCY_KEY_BYTES} bytes"
        )
    return normalized


def _turn_request_hash(
    *,
    conversation_id: str,
    task_id: str | None,
    input_text: str,
    input_asset_ids: list[str],
) -> str:
    payload = {
        "schema_version": 1,
        "conversation_id": conversation_id,
        "task_id": task_id,
        "input_text": input_text,
        "input_asset_ids": input_asset_ids,
    }
    encoded = json.dumps(payload, ensure_ascii=False, sort_keys=True, separators=(",", ":")).encode()
    return hashlib.sha256(encoded).hexdigest()


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
            conversation.status = (
                AgentConversationStatus.AWAITING_CONFIRMATION
                if conversation.workflow_draft.status == WorkflowDraftStatus.AWAITING_CONFIRMATION
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
    "WORKFLOW_DRAFT_ARTIFACT_NAME",
    "AgentTurnPage",
    "AgentTurnReservation",
    "agent_conversation_query",
    "attach_agent_workflow_draft_artifact",
    "bind_harness_turn",
    "create_agent_conversation",
    "get_agent_conversation_by_id_or_raise",
    "get_agent_conversation_or_raise",
    "get_agent_turn_or_raise",
    "list_agent_turn_page",
    "mark_agent_conversation_completed_for_draft",
    "project_agent_turn_state",
    "record_agent_turn_start_error",
    "reserve_agent_turn",
    "set_agent_turn_resume_required",
]
