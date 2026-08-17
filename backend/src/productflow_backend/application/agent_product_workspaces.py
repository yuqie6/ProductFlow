from __future__ import annotations

from dataclasses import dataclass

from sqlalchemy import select
from sqlalchemy.exc import IntegrityError
from sqlalchemy.orm import Session

from productflow_backend.application.agent_conversations import (
    agent_conversation_query,
    get_agent_conversation_or_raise,
)
from productflow_backend.application.agent_product_intake import (
    WORKFLOW_INTAKE_SCHEMA_VERSION,
    AgentProductSelectionV1,
    WorkflowIntakeV1,
    agent_product_draft_workspace_request_hash,
    agent_product_intake_request_hash,
    agent_product_workspace_request_hash,
    normalize_agent_product_idempotency_key,
    parse_workflow_intake,
)
from productflow_backend.application.agent_sessions import get_agent_session_or_raise, new_agent_session
from productflow_backend.application.agent_tasks import (
    AGENT_TASK_TITLE_MAX_LENGTH,
    new_agent_task,
    refresh_agent_session_summary,
)
from productflow_backend.application.media_assets import get_product_image_assets_by_ids
from productflow_backend.application.storage_compensation import compensate_storage_writes
from productflow_backend.application.time import now_utc
from productflow_backend.application.use_cases import (
    normalize_product_name,
    stage_canonical_product,
    stage_canonical_product_assets,
    stage_canonical_product_with_assets,
)
from productflow_backend.application.workflow_drafts.service import workflow_draft_query
from productflow_backend.domain.enums import (
    AgentConversationScope,
    AgentConversationStatus,
    AgentSessionStatus,
    AgentTaskStatus,
    WorkflowDraftStatus,
)
from productflow_backend.domain.errors import ConflictError, NotFoundError
from productflow_backend.infrastructure.db.models import (
    AgentConversation,
    AgentTask,
    AgentTurnProjection,
    Product,
    ProductImageAsset,
    WorkflowDraft,
    new_id,
)
from productflow_backend.infrastructure.storage import LocalStorage


@dataclass(frozen=True, slots=True)
class AgentProductWorkspaceCreation:
    product: Product
    created_assets: list[ProductImageAsset]
    workflow_draft: WorkflowDraft
    conversation: AgentConversation
    onboarding_task_id: str | None
    created: bool


PRODUCT_ONBOARDING_TASK_WAITING_REASON = "product_onboarding_intake"


def create_agent_product_draft_workspace(
    session: Session,
    *,
    name: str,
    idempotency_key: str,
    agent_session_id: str | None = None,
) -> AgentProductWorkspaceCreation:
    """Create the durable workspace identity before collecting bounded intake."""
    normalized_name = normalize_product_name(name)
    normalized_key = normalize_agent_product_idempotency_key(idempotency_key)
    normalized_session_id = _normalize_agent_session_id(agent_session_id)
    request_hash = agent_product_draft_workspace_request_hash(
        normalized_product_name=normalized_name,
        agent_session_id=normalized_session_id,
    )

    existing = _conversation_by_creation_key(session, normalized_key)
    if existing is not None:
        return _load_idempotent_workspace(
            session,
            conversation=existing,
            request_hash=request_hash,
            created=False,
        )

    try:
        product = stage_canonical_product(
            session,
            name=normalized_name,
            category=None,
            price=None,
            source_note=None,
        )
        _stage_workspace_records(
            session,
            product=product,
            creation_idempotency_key=normalized_key,
            creation_request_hash=request_hash,
            intake=None,
            agent_session_id=normalized_session_id,
            create_onboarding_task=True,
        )
        session.commit()
    except IntegrityError:
        session.rollback()
        existing = _conversation_by_creation_key(session, normalized_key)
        if existing is None:
            raise
        return _load_idempotent_workspace(
            session,
            conversation=existing,
            request_hash=request_hash,
            created=False,
        )
    except Exception:
        session.rollback()
        raise

    persisted = _conversation_by_creation_key(session, normalized_key)
    if persisted is None:
        raise ConflictError("Agent 商品草稿创建结果无法重新读取")
    return _load_idempotent_workspace(
        session,
        conversation=persisted,
        request_hash=request_hash,
        created=True,
    )


def create_agent_product_draft_workspace_from_global_conversation(
    session: Session,
    *,
    global_conversation_id: str,
    name: str,
    idempotency_key: str,
) -> AgentProductWorkspaceCreation:
    """Start product onboarding from a global conversation without merging run histories."""
    global_conversation = get_agent_conversation_or_raise(
        session,
        product_id=None,
        conversation_id=global_conversation_id,
    )
    if global_conversation.session_id is None:
        raise ConflictError("全局 Agent conversation 没有关联 Session")
    return create_agent_product_draft_workspace(
        session,
        name=name,
        idempotency_key=idempotency_key,
        agent_session_id=global_conversation.session_id,
    )


def create_agent_product_workspace(
    session: Session,
    *,
    name: str,
    selection: AgentProductSelectionV1,
    image_uploads: list[tuple[bytes, str, str]],
    idempotency_key: str,
    agent_session_id: str | None = None,
    storage: LocalStorage | None = None,
) -> AgentProductWorkspaceCreation:
    """Keep the original one-request creation contract for existing callers."""
    normalized_name = normalize_product_name(name)
    normalized_key = normalize_agent_product_idempotency_key(idempotency_key)
    normalized_session_id = _normalize_agent_session_id(agent_session_id)
    request_hash = agent_product_workspace_request_hash(
        normalized_product_name=normalized_name,
        selection=selection,
        image_uploads=image_uploads,
        agent_session_id=normalized_session_id,
    )

    existing = _conversation_by_creation_key(session, normalized_key)
    if existing is not None:
        return _load_idempotent_workspace(
            session,
            conversation=existing,
            request_hash=request_hash,
            created=False,
        )

    storage = storage or LocalStorage()
    try:
        with compensate_storage_writes(session) as storage_writes:
            canonical = stage_canonical_product_with_assets(
                session,
                name=normalized_name,
                category=None,
                price=None,
                source_note=None,
                image_uploads=image_uploads,
                storage=storage,
                storage_writes=storage_writes,
            )
            intake = WorkflowIntakeV1(
                schema_version=WORKFLOW_INTAKE_SCHEMA_VERSION,
                image_types=selection.image_types,
                reference_asset_ids=[asset.id for asset in canonical.created_assets],
            )
            _stage_workspace_records(
                session,
                product=canonical.product,
                creation_idempotency_key=normalized_key,
                creation_request_hash=request_hash,
                intake=intake,
                agent_session_id=normalized_session_id,
                create_onboarding_task=False,
            )
            session.commit()
    except IntegrityError:
        session.rollback()
        existing = _conversation_by_creation_key(session, normalized_key)
        if existing is None:
            raise
        return _load_idempotent_workspace(
            session,
            conversation=existing,
            request_hash=request_hash,
            created=False,
        )

    persisted = _conversation_by_creation_key(session, normalized_key)
    if persisted is None:
        raise ConflictError("Agent 商品创建结果无法重新读取")
    return _load_idempotent_workspace(
        session,
        conversation=persisted,
        request_hash=request_hash,
        created=True,
    )


def get_agent_product_workspace(
    session: Session,
    *,
    conversation_id: str,
) -> AgentProductWorkspaceCreation:
    conversation = session.scalar(
        agent_conversation_query().where(AgentConversation.id == conversation_id)
    )
    if conversation is None:
        raise NotFoundError("Agent 商品工作空间不存在")
    return _load_workspace(session, conversation_id=conversation.id, created=False)


def finalize_agent_product_workspace_intake(
    session: Session,
    *,
    conversation_id: str,
    selection: AgentProductSelectionV1,
    image_uploads: list[tuple[bytes, str, str]],
    idempotency_key: str,
    task_id: str | None = None,
    storage: LocalStorage | None = None,
) -> AgentProductWorkspaceCreation:
    """Atomically bind verified references and immutable intake to a version-zero draft."""
    normalized_key = normalize_agent_product_idempotency_key(idempotency_key)
    request_hash = agent_product_intake_request_hash(
        selection=selection,
        image_uploads=image_uploads,
    )

    conversation = session.scalar(
        select(AgentConversation)
        .where(AgentConversation.id == conversation_id)
        .with_for_update()
    )
    if conversation is None:
        raise NotFoundError("Agent 商品工作空间不存在")
    draft = session.scalar(
        workflow_draft_query()
        .where(WorkflowDraft.id == conversation.workflow_draft_id)
        .with_for_update()
    )
    product = session.scalar(
        select(Product).where(Product.id == conversation.product_id).with_for_update()
    )
    if draft is None or product is None:
        raise ConflictError("Agent 商品工作空间聚合不完整")

    onboarding_task = _get_onboarding_task_for_update(
        session,
        conversation_id=conversation.id,
        task_id=task_id,
    )
    if onboarding_task is not None and onboarding_task.status == AgentTaskStatus.CANCELED:
        raise ConflictError("商品创建 Task 已取消，不能继续提交商品输入")

    if conversation.intake_idempotency_key is not None:
        if (
            conversation.intake_idempotency_key != normalized_key
            or conversation.intake_request_hash != request_hash
        ):
            raise ConflictError("Agent 商品输入已经确认，不能提交不同请求")
        if draft.intake_json is None:
            raise ConflictError("Agent 商品输入幂等记录与 WorkflowDraft 不一致")
        session.commit()
        return _load_workspace(session, conversation_id=conversation.id, created=False)
    if conversation.intake_request_hash is not None:
        raise ConflictError("Agent 商品输入幂等记录不完整")
    if draft.intake_json is not None or draft.intake_schema_version is not None:
        raise ConflictError("Agent 商品输入已经确认")
    if conversation.status != AgentConversationStatus.COLLECTING:
        raise ConflictError("当前 Agent conversation 状态不允许确认商品输入")
    if draft.status != WorkflowDraftStatus.COLLECTING:
        raise ConflictError("当前 WorkflowDraft 状态不允许确认商品输入")
    if draft.current_revision_id is not None or draft.revisions:
        raise ConflictError("已经开始生成的 WorkflowDraft 不能再确认商品输入")
    if draft.recipe_seed is not None:
        raise ConflictError("带重建种子的 WorkflowDraft 不能确认商品创建输入")
    has_turn = session.scalar(
        select(AgentTurnProjection.id)
        .where(AgentTurnProjection.conversation_id == conversation.id)
        .limit(1)
    )
    if has_turn is not None:
        raise ConflictError("已经开始 Agent 对话的商品不能再确认创建输入")

    storage = storage or LocalStorage()
    try:
        with compensate_storage_writes(session) as storage_writes:
            assets = stage_canonical_product_assets(
                session,
                product=product,
                image_uploads=image_uploads,
                storage=storage,
                storage_writes=storage_writes,
            )
            intake = WorkflowIntakeV1(
                schema_version=WORKFLOW_INTAKE_SCHEMA_VERSION,
                image_types=selection.image_types,
                reference_asset_ids=[asset.id for asset in assets],
            )
            draft.intake_schema_version = WORKFLOW_INTAKE_SCHEMA_VERSION
            draft.intake_json = intake.model_dump(mode="json")
            draft.updated_at = now_utc()
            conversation.intake_idempotency_key = normalized_key
            conversation.intake_request_hash = request_hash
            conversation.updated_at = now_utc()
            if onboarding_task is not None:
                now = now_utc()
                onboarding_task.status = AgentTaskStatus.SUCCEEDED
                onboarding_task.waiting_reason = None
                onboarding_task.failure_reason = None
                onboarding_task.finished_at = now
                onboarding_task.updated_at = now
            refresh_agent_session_summary(session, conversation.session_id)
            session.commit()
    except IntegrityError:
        session.rollback()
        persisted = session.scalar(
            agent_conversation_query().where(AgentConversation.id == conversation_id)
        )
        if (
            persisted is not None
            and persisted.intake_idempotency_key == normalized_key
            and persisted.intake_request_hash == request_hash
            and persisted.workflow_draft.intake_json is not None
        ):
            return _load_workspace(session, conversation_id=persisted.id, created=False)
        raise

    return _load_workspace(session, conversation_id=conversation.id, created=True)


def _stage_workspace_records(
    session: Session,
    *,
    product: Product,
    creation_idempotency_key: str,
    creation_request_hash: str,
    intake: WorkflowIntakeV1 | None,
    agent_session_id: str | None,
    create_onboarding_task: bool,
) -> tuple[WorkflowDraft, AgentConversation]:
    draft = WorkflowDraft(
        product_id=product.id,
        status=WorkflowDraftStatus.COLLECTING,
        intake_schema_version=(WORKFLOW_INTAKE_SCHEMA_VERSION if intake is not None else None),
        intake_json=(intake.model_dump(mode="json") if intake is not None else None),
    )
    session.add(draft)
    session.flush()

    conversation_id = new_id()
    if agent_session_id is None:
        agent_session = new_agent_session(title=product.name)
        session.add(agent_session)
        session.flush()
    else:
        agent_session = get_agent_session_or_raise(session, agent_session_id)
        if agent_session.status != AgentSessionStatus.ACTIVE:
            raise ConflictError("已归档的 Agent Session 不能创建商品工作区")

    global_conversation = session.scalar(
        select(AgentConversation).where(
            AgentConversation.session_id == agent_session.id,
            AgentConversation.scope_type == AgentConversationScope.GLOBAL,
        )
    )
    if global_conversation is None:
        session.add(
            AgentConversation(
                id=new_id(),
                scope_type=AgentConversationScope.GLOBAL,
                session_id=agent_session.id,
                harness_run_id=new_id(),
            )
        )
    conversation = AgentConversation(
        id=conversation_id,
        session_id=agent_session.id,
        product_id=product.id,
        workflow_draft_id=draft.id,
        harness_run_id=conversation_id,
        status=AgentConversationStatus.COLLECTING,
        creation_idempotency_key=creation_idempotency_key,
        creation_request_hash=creation_request_hash,
    )
    session.add(conversation)
    session.flush()
    if create_onboarding_task:
        title_prefix = "创建商品："
        task = new_agent_task(
            session_id=agent_session.id,
            title=(title_prefix + product.name)[:AGENT_TASK_TITLE_MAX_LENGTH],
            goal=f"完成商品“{product.name}”创建，收集参考图和图片需求并初始化商品工作区",
            conversation=conversation,
        )
        task.status = AgentTaskStatus.WAITING_USER
        task.waiting_reason = PRODUCT_ONBOARDING_TASK_WAITING_REASON
        session.add(task)
    refresh_agent_session_summary(session, agent_session.id)
    return draft, conversation


def _normalize_agent_session_id(value: str | None) -> str | None:
    if value is None:
        return None
    normalized = value.strip()
    return normalized or None


def _conversation_by_creation_key(session: Session, idempotency_key: str) -> AgentConversation | None:
    return session.scalar(
        agent_conversation_query().where(AgentConversation.creation_idempotency_key == idempotency_key)
    )


def _load_idempotent_workspace(
    session: Session,
    *,
    conversation: AgentConversation,
    request_hash: str,
    created: bool,
) -> AgentProductWorkspaceCreation:
    if conversation.creation_request_hash != request_hash:
        raise ConflictError("相同 Idempotency-Key 不能创建不同的 Agent 商品")
    return _load_workspace(session, conversation_id=conversation.id, created=created)


def _load_workspace(
    session: Session,
    *,
    conversation_id: str,
    created: bool,
) -> AgentProductWorkspaceCreation:
    session.expire_all()
    conversation = session.scalar(
        agent_conversation_query().where(AgentConversation.id == conversation_id)
    )
    if conversation is None:
        raise ConflictError("Agent 商品工作空间无法重新读取")
    draft = session.scalar(
        workflow_draft_query().where(WorkflowDraft.id == conversation.workflow_draft_id)
    )
    product = session.scalar(select(Product).where(Product.id == conversation.product_id))
    if draft is None or product is None:
        raise ConflictError("Agent 商品工作空间聚合不完整")
    intake = parse_workflow_intake(
        schema_version=draft.intake_schema_version,
        payload=draft.intake_json,
    )
    assets = (
        get_product_image_assets_by_ids(
            session,
            product_id=product.id,
            asset_ids=list(intake.reference_asset_ids),
        )
        if intake is not None
        else []
    )
    onboarding_task = session.scalar(
        select(AgentTask)
        .where(
            AgentTask.conversation_id == conversation.id,
            AgentTask.waiting_reason == PRODUCT_ONBOARDING_TASK_WAITING_REASON,
        )
        .order_by(AgentTask.created_at.desc(), AgentTask.id.desc())
    )
    return AgentProductWorkspaceCreation(
        product=product,
        created_assets=assets,
        workflow_draft=draft,
        conversation=conversation,
        onboarding_task_id=onboarding_task.id if onboarding_task is not None else None,
        created=created,
    )


def _get_onboarding_task_for_update(
    session: Session,
    *,
    conversation_id: str,
    task_id: str | None,
) -> AgentTask | None:
    statement = select(AgentTask).where(AgentTask.conversation_id == conversation_id).with_for_update()
    if task_id is not None:
        task = session.scalar(statement.where(AgentTask.id == task_id))
        if task is None:
            raise ConflictError("商品创建 Task 与当前商品工作区不匹配")
        return task
    return session.scalar(
        statement.where(
            AgentTask.status == AgentTaskStatus.WAITING_USER,
            AgentTask.waiting_reason == PRODUCT_ONBOARDING_TASK_WAITING_REASON,
        )
    )


__all__ = [
    "AgentProductWorkspaceCreation",
    "PRODUCT_ONBOARDING_TASK_WAITING_REASON",
    "create_agent_product_draft_workspace",
    "create_agent_product_draft_workspace_from_global_conversation",
    "create_agent_product_workspace",
    "finalize_agent_product_workspace_intake",
    "get_agent_product_workspace",
]
