from __future__ import annotations

from dataclasses import dataclass

from sqlalchemy import select
from sqlalchemy.exc import IntegrityError
from sqlalchemy.orm import Session

from productflow_backend.application.agent.conversations import (
    agent_conversation_query,
    get_agent_conversation_or_raise,
)
from productflow_backend.application.agent.product_intake import (
    WORKFLOW_INTAKE_SCHEMA_VERSION,
    AgentProductSelectionV1,
    WorkflowIntakeV1,
    agent_product_draft_workspace_request_hash,
    agent_product_intake_from_assets_request_hash,
    agent_product_intake_request_hash,
    agent_product_workspace_request_hash,
    agent_workbench_attach_request_hash,
    normalize_agent_product_idempotency_key,
    parse_workflow_intake,
    workflow_intake_from_selection,
    workflow_intake_payload,
)
from productflow_backend.application.agent.sessions import get_agent_session_or_raise, new_agent_session
from productflow_backend.application.agent.tasks import (
    AGENT_TASK_TITLE_MAX_LENGTH,
    new_agent_task,
    refresh_agent_session_summary,
)
from productflow_backend.application.product_facts import product_metadata_facts, stage_product_fact_set
from productflow_backend.application.product_images.assets import get_product_image_assets_by_ids
from productflow_backend.application.product_workflow.graph_commands import get_active_workflow_graph
from productflow_backend.application.products import (
    normalize_product_name,
    stage_canonical_product,
    stage_canonical_product_assets,
    stage_canonical_product_with_assets,
)
from productflow_backend.application.storage_compensation import compensate_storage_writes
from productflow_backend.application.time import now_utc
from productflow_backend.application.workflow_drafts.service import workflow_draft_query
from productflow_backend.domain.enums import (
    AgentConversationScope,
    AgentConversationStatus,
    AgentSessionStatus,
    AgentTaskStatus,
    MediaVerificationStatus,
    WorkflowDraftStatus,
)
from productflow_backend.domain.errors import BusinessValidationError, ConflictError, NotFoundError
from productflow_backend.infrastructure.db.models import (
    AgentConversation,
    AgentTask,
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


@dataclass(frozen=True, slots=True)
class AgentProductWorkspaceReconcileResult:
    state: str
    creation: AgentProductWorkspaceCreation | None = None
    detail: str | None = None


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
        _stage_product_identity_facts(session, product)
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


def reconcile_agent_product_draft_workspace_from_global_conversation(
    session: Session,
    *,
    global_conversation_id: str,
    name: str,
    idempotency_key: str,
) -> AgentProductWorkspaceReconcileResult:
    """Read the durable workspace fact without issuing another create command."""
    global_conversation = get_agent_conversation_or_raise(
        session,
        product_id=None,
        conversation_id=global_conversation_id,
    )
    if global_conversation.session_id is None:
        raise ConflictError("全局 Agent conversation 没有关联 Session")
    normalized_name = normalize_product_name(name)
    normalized_key = normalize_agent_product_idempotency_key(idempotency_key)
    request_hash = agent_product_draft_workspace_request_hash(
        normalized_product_name=normalized_name,
        agent_session_id=global_conversation.session_id,
    )
    existing = _conversation_by_creation_key(session, normalized_key)
    if existing is None:
        return AgentProductWorkspaceReconcileResult(state="not_applied")
    if existing.creation_request_hash != request_hash:
        return AgentProductWorkspaceReconcileResult(
            state="conflict",
            detail="同一 Idempotency-Key 已对应不同的 Agent 商品创建请求",
        )
    try:
        creation = _load_idempotent_workspace(
            session,
            conversation=existing,
            request_hash=request_hash,
            created=False,
        )
    except ConflictError as exc:
        return AgentProductWorkspaceReconcileResult(
            state="unknown",
            detail=f"商品工作区记录存在，但聚合无法完整读取: {exc}",
        )
    return AgentProductWorkspaceReconcileResult(state="applied", creation=creation)


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
            _stage_product_identity_facts(session, canonical.product)
            intake = workflow_intake_from_selection(
                selection,
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


def attach_agent_workspace_to_product(
    session: Session,
    *,
    product_id: str,
    idempotency_key: str,
    agent_session_id: str | None = None,
) -> AgentProductWorkspaceCreation:
    """Create a product-scoped Agent conversation for an existing v3 graph product."""
    product = session.scalar(select(Product).where(Product.id == product_id).with_for_update())
    if product is None:
        raise NotFoundError("商品不存在")
    existing_for_product = session.scalar(
        agent_conversation_query()
        .where(AgentConversation.product_id == product_id)
        .order_by(AgentConversation.created_at.desc(), AgentConversation.id.desc())
        .limit(1)
    )
    if existing_for_product is not None:
        return _load_workspace(session, conversation_id=existing_for_product.id, created=False)
    if get_active_workflow_graph(session, product_id=product_id) is None:
        raise ConflictError("当前商品还没有可执行的工作流")

    normalized_key = normalize_agent_product_idempotency_key(idempotency_key)
    normalized_session_id = _normalize_agent_session_id(agent_session_id)
    request_hash = agent_workbench_attach_request_hash(
        product_id=product_id,
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
        _stage_workspace_records(
            session,
            product=product,
            creation_idempotency_key=normalized_key,
            creation_request_hash=request_hash,
            intake=None,
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
    except Exception:
        session.rollback()
        raise

    persisted = _conversation_by_creation_key(session, normalized_key)
    if persisted is None:
        raise ConflictError("Agent 工作区创建结果无法重新读取")
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


@dataclass(frozen=True, slots=True)
class _IntakeFinalizationLock:
    conversation: AgentConversation
    draft: WorkflowDraft
    product: Product
    onboarding_task: AgentTask | None
    replay: AgentProductWorkspaceCreation | None


def _lock_intake_finalization(
    session: Session,
    *,
    conversation_id: str,
    normalized_key: str,
    request_hash: str,
    task_id: str | None,
) -> _IntakeFinalizationLock:
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
        return _IntakeFinalizationLock(
            conversation=conversation,
            draft=draft,
            product=product,
            onboarding_task=onboarding_task,
            replay=_load_workspace(session, conversation_id=conversation.id, created=False),
        )
    if conversation.intake_request_hash is not None:
        raise ConflictError("Agent 商品输入幂等记录不完整")
    if draft.intake_json is not None or draft.intake_schema_version is not None:
        raise ConflictError("Agent 商品输入已经确认")
    if conversation.status == AgentConversationStatus.AWAITING_CONFIRMATION:
        raise ConflictError("当前 Agent conversation 状态不允许确认商品输入")
    if draft.status != WorkflowDraftStatus.COLLECTING:
        raise ConflictError("当前 WorkflowDraft 状态不允许确认商品输入")
    if draft.current_revision_id is not None or draft.revisions:
        raise ConflictError("已经开始生成的 WorkflowDraft 不能再确认商品输入")
    if draft.recipe_seed is not None:
        raise ConflictError("带重建种子的 WorkflowDraft 不能确认商品创建输入")
    if get_active_workflow_graph(session, product_id=product.id) is not None:
        raise ConflictError("商品已有可运行的工作流，不能再提交创建输入")
    return _IntakeFinalizationLock(
        conversation=conversation,
        draft=draft,
        product=product,
        onboarding_task=onboarding_task,
        replay=None,
    )


def _write_intake_and_commit(
    session: Session,
    *,
    conversation_id: str,
    conversation: AgentConversation,
    draft: WorkflowDraft,
    onboarding_task: AgentTask | None,
    intake: WorkflowIntakeV1,
    normalized_key: str,
    request_hash: str,
) -> AgentProductWorkspaceCreation:
    draft.intake_schema_version = WORKFLOW_INTAKE_SCHEMA_VERSION
    draft.intake_json = workflow_intake_payload(intake)
    draft.updated_at = now_utc()
    conversation.intake_idempotency_key = normalized_key
    conversation.intake_request_hash = request_hash
    conversation.status = AgentConversationStatus.COLLECTING
    conversation.updated_at = now_utc()
    if onboarding_task is not None:
        now = now_utc()
        onboarding_task.status = AgentTaskStatus.SUCCEEDED
        onboarding_task.waiting_reason = None
        onboarding_task.failure_reason = None
        onboarding_task.finished_at = now
        onboarding_task.updated_at = now
    refresh_agent_session_summary(session, conversation.session_id)
    try:
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
    """Atomically bind newly uploaded references and immutable intake to a version-zero draft."""
    normalized_key = normalize_agent_product_idempotency_key(idempotency_key)
    request_hash = agent_product_intake_request_hash(
        selection=selection,
        image_uploads=image_uploads,
    )
    locked = _lock_intake_finalization(
        session,
        conversation_id=conversation_id,
        normalized_key=normalized_key,
        request_hash=request_hash,
        task_id=task_id,
    )
    if locked.replay is not None:
        return locked.replay

    storage = storage or LocalStorage()
    with compensate_storage_writes(session) as storage_writes:
        assets = stage_canonical_product_assets(
            session,
            product=locked.product,
            image_uploads=image_uploads,
            storage=storage,
            storage_writes=storage_writes,
        )
        intake = workflow_intake_from_selection(selection, reference_asset_ids=[asset.id for asset in assets])
        return _write_intake_and_commit(
            session,
            conversation_id=conversation_id,
            conversation=locked.conversation,
            draft=locked.draft,
            onboarding_task=locked.onboarding_task,
            intake=intake,
            normalized_key=normalized_key,
            request_hash=request_hash,
        )


def finalize_agent_product_workspace_intake_from_assets(
    session: Session,
    *,
    conversation_id: str,
    selection: AgentProductSelectionV1,
    reference_asset_ids: list[str],
    idempotency_key: str,
    task_id: str | None = None,
) -> AgentProductWorkspaceCreation:
    """Bind already-uploaded product images as immutable intake from the Agent conversation."""
    normalized_key = normalize_agent_product_idempotency_key(idempotency_key)
    request_hash = agent_product_intake_from_assets_request_hash(
        selection=selection,
        reference_asset_ids=reference_asset_ids,
    )
    locked = _lock_intake_finalization(
        session,
        conversation_id=conversation_id,
        normalized_key=normalized_key,
        request_hash=request_hash,
        task_id=task_id,
    )
    if locked.replay is not None:
        return locked.replay

    assets = get_product_image_assets_by_ids(
        session,
        product_id=locked.product.id,
        asset_ids=reference_asset_ids,
    )
    if any(asset.media_object.verification_status != MediaVerificationStatus.VERIFIED for asset in assets):
        raise BusinessValidationError("参考图必须通过核验")
    intake = workflow_intake_from_selection(selection, reference_asset_ids=list(reference_asset_ids))
    return _write_intake_and_commit(
        session,
        conversation_id=conversation_id,
        conversation=locked.conversation,
        draft=locked.draft,
        onboarding_task=locked.onboarding_task,
        intake=intake,
        normalized_key=normalized_key,
        request_hash=request_hash,
    )


def reconcile_agent_product_intake_from_assets(
    session: Session,
    *,
    conversation_id: str,
    selection: AgentProductSelectionV1,
    reference_asset_ids: list[str],
    idempotency_key: str,
) -> AgentProductWorkspaceReconcileResult:
    conversation = session.scalar(
        select(AgentConversation).where(AgentConversation.id == conversation_id)
    )
    if conversation is None:
        raise NotFoundError("Agent 商品工作空间不存在")
    normalized_key = normalize_agent_product_idempotency_key(idempotency_key)
    request_hash = agent_product_intake_from_assets_request_hash(
        selection=selection,
        reference_asset_ids=reference_asset_ids,
    )
    if conversation.intake_idempotency_key is None:
        return AgentProductWorkspaceReconcileResult(state="not_applied")
    if (
        conversation.intake_idempotency_key != normalized_key
        or conversation.intake_request_hash != request_hash
    ):
        return AgentProductWorkspaceReconcileResult(
            state="conflict",
            detail="同一 Idempotency-Key 已对应不同的商品输入",
        )
    try:
        creation = _load_workspace(session, conversation_id=conversation.id, created=False)
    except ConflictError as exc:
        return AgentProductWorkspaceReconcileResult(
            state="unknown",
            detail=f"商品输入记录存在，但聚合无法完整读取: {exc}",
        )
    if creation.workflow_draft.intake_json is None:
        return AgentProductWorkspaceReconcileResult(
            state="unknown",
            detail="商品输入幂等记录与 WorkflowDraft 不一致",
        )
    return AgentProductWorkspaceReconcileResult(state="applied", creation=creation)


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
        intake_json=(workflow_intake_payload(intake) if intake is not None else None),
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


def _stage_product_identity_facts(session: Session, product: Product) -> None:
    if product.current_fact_set_version_id:
        return
    facts = product_metadata_facts(product)
    if not facts:
        return
    stage_product_fact_set(session, product=product, facts=facts)


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
    "AgentProductWorkspaceReconcileResult",
    "PRODUCT_ONBOARDING_TASK_WAITING_REASON",
    "attach_agent_workspace_to_product",
    "create_agent_product_draft_workspace",
    "create_agent_product_draft_workspace_from_global_conversation",
    "create_agent_product_workspace",
    "finalize_agent_product_workspace_intake",
    "finalize_agent_product_workspace_intake_from_assets",
    "get_agent_product_workspace",
    "reconcile_agent_product_draft_workspace_from_global_conversation",
    "reconcile_agent_product_intake_from_assets",
]
