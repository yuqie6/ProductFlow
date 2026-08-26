"""商品工作区：Session 下的 product conversation 与 live graph。创建幂等靠 key+request hash。"""

from __future__ import annotations

from dataclasses import dataclass

from sqlalchemy import select
from sqlalchemy.exc import IntegrityError
from sqlalchemy.orm import Session

from productflow_backend.application.agent.conversations import (
    agent_conversation_query,
    get_agent_conversation_or_raise,
)
from productflow_backend.application.agent.sessions import get_agent_session_or_raise, new_agent_session
from productflow_backend.application.agent.tasks import refresh_agent_session_summary
from productflow_backend.application.product_facts import product_metadata_facts, stage_product_fact_set
from productflow_backend.application.product_images.assets import get_product_image_assets_by_ids
from productflow_backend.application.product_intake import (
    AgentProductSelectionV1,
    WorkflowIntakeV1,
    agent_product_draft_workspace_request_hash,
    agent_product_intake_from_assets_request_hash,
    agent_product_intake_request_hash,
    agent_product_workspace_request_hash,
    agent_workbench_attach_request_hash,
    normalize_agent_product_idempotency_key,
    parse_product_intake,
    workflow_intake_from_selection,
    write_product_intake,
)
from productflow_backend.application.product_workflow.graph_commands import (
    get_active_workflow_graph,
    load_applied_graph,
    stage_apply_graph_change_set,
    stage_new_workflow_graph,
)
from productflow_backend.application.product_workflow.graph_template import (
    DirectCreateImageType,
    build_direct_create_template,
    build_product_source_create_graph,
    template_for_existing_product_source,
)
from productflow_backend.application.products import (
    normalize_product_name,
    stage_canonical_product,
    stage_canonical_product_assets,
    stage_canonical_product_with_assets,
)
from productflow_backend.application.storage_compensation import compensate_storage_writes
from productflow_backend.application.time import now_utc
from productflow_backend.domain.enums import (
    AgentConversationStatus,
    AgentSessionStatus,
    GraphNodeType,
    MediaVerificationStatus,
)
from productflow_backend.domain.errors import BusinessValidationError, ConflictError, NotFoundError
from productflow_backend.infrastructure.db.models import (
    AgentConversation,
    Product,
    ProductImageAsset,
    new_id,
)
from productflow_backend.infrastructure.storage import LocalStorage


@dataclass(frozen=True, slots=True)
class AgentProductWorkspaceCreation:
    product: Product
    created_assets: list[ProductImageAsset]
    conversation: AgentConversation
    created: bool

    @property
    def intake(self) -> WorkflowIntakeV1 | None:
        return parse_product_intake(self.product)

    @property
    def intake_finalized(self) -> bool:
        return self.product.intake_json is not None


@dataclass(frozen=True, slots=True)
class AgentProductWorkspaceReconcileResult:
    state: str
    creation: AgentProductWorkspaceCreation | None = None
    detail: str | None = None


def create_agent_product_draft_workspace(
    session: Session,
    *,
    name: str,
    idempotency_key: str,
    agent_session_id: str | None = None,
) -> AgentProductWorkspaceCreation:
    """创建可落库的工作区身份；有界 intake 尚未写入。本函数 commit。"""
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
            created_assets=[],
            fork_global_session=True,
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
    """从全局 conversation 开商品画布 Session，不合并两边的 run 历史。"""
    get_agent_conversation_or_raise(
        session,
        product_id=None,
        conversation_id=global_conversation_id,
    )
    return create_agent_product_draft_workspace(
        session,
        name=name,
        idempotency_key=idempotency_key,
        agent_session_id=None,
    )


def reconcile_agent_product_draft_workspace_from_global_conversation(
    session: Session,
    *,
    global_conversation_id: str,
    name: str,
    idempotency_key: str,
) -> AgentProductWorkspaceReconcileResult:
    """只读已落库的工作区事实，不重放 create。记录在但聚合读不齐则 unknown。"""
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
        agent_session_id=None,
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
    """保留现有调用方的一次性创建合同。本函数 commit。"""
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
                created_assets=canonical.created_assets,
                fork_global_session=True,
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
    force_new: bool = False,
) -> AgentProductWorkspaceCreation:
    """为已有 v3 graph 商品创建 product-scoped Agent conversation。本函数 commit。"""
    product = session.scalar(select(Product).where(Product.id == product_id).with_for_update())
    if product is None:
        raise NotFoundError("商品不存在")
    existing_for_product = session.scalar(
        agent_conversation_query()
        .where(AgentConversation.product_id == product_id)
        .order_by(AgentConversation.created_at.desc(), AgentConversation.id.desc())
        .limit(1)
    )
    if existing_for_product is not None and not force_new:
        if agent_session_id is None or existing_for_product.session_id == agent_session_id:
            return _load_workspace(session, conversation_id=existing_for_product.id, created=False)
        session_match = session.scalar(
            agent_conversation_query().where(
                AgentConversation.product_id == product_id,
                AgentConversation.session_id == agent_session_id,
            )
        )
        if session_match is not None:
            return _load_workspace(session, conversation_id=session_match.id, created=False)
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
            created_assets=[],
            fork_global_session=False,
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
    product: Product
    replay: AgentProductWorkspaceCreation | None


def _lock_intake_finalization(
    session: Session,
    *,
    conversation_id: str,
    normalized_key: str,
    request_hash: str,
    task_id: str | None,
) -> _IntakeFinalizationLock:
    del task_id
    conversation = session.scalar(
        select(AgentConversation)
        .where(AgentConversation.id == conversation_id)
        .with_for_update()
    )
    if conversation is None:
        raise NotFoundError("Agent 商品工作空间不存在")
    product = session.scalar(
        select(Product).where(Product.id == conversation.product_id).with_for_update()
    )
    if product is None:
        raise ConflictError("Agent 商品工作空间聚合不完整")

    if conversation.intake_idempotency_key is not None:
        if (
            conversation.intake_idempotency_key != normalized_key
            or conversation.intake_request_hash != request_hash
        ):
            raise ConflictError("Agent 商品输入已经确认，不能提交不同请求")
        if product.intake_json is None:
            raise ConflictError("Agent 商品输入幂等记录与商品 intake 不一致")
        # 幂等回放在此 commit，调用方不得再写 intake。
        session.commit()
        return _IntakeFinalizationLock(
            conversation=conversation,
            product=product,
            replay=_load_workspace(session, conversation_id=conversation.id, created=False),
        )
    if conversation.intake_request_hash is not None:
        raise ConflictError("Agent 商品输入幂等记录不完整")
    if product.intake_json is not None or product.intake_schema_version is not None:
        raise ConflictError("Agent 商品输入已经确认")
    if conversation.status == AgentConversationStatus.AWAITING_CONFIRMATION:
        raise ConflictError("当前 Agent conversation 状态不允许确认商品输入")
    return _IntakeFinalizationLock(
        conversation=conversation,
        product=product,
        replay=None,
    )


def _write_intake_and_commit(
    session: Session,
    *,
    conversation_id: str,
    conversation: AgentConversation,
    product: Product,
    intake: WorkflowIntakeV1,
    normalized_key: str,
    request_hash: str,
) -> AgentProductWorkspaceCreation:
    write_product_intake(product, intake)
    product.updated_at = now_utc()
    conversation.intake_idempotency_key = normalized_key
    conversation.intake_request_hash = request_hash
    conversation.status = AgentConversationStatus.COLLECTING
    conversation.updated_at = now_utc()
    refresh_agent_session_summary(session, conversation.session_id)
    _expand_birth_graph_from_intake(session, product=product, intake=intake)
    try:
        session.commit()
    except IntegrityError:
        session.rollback()
        persisted = session.scalar(
            agent_conversation_query().where(AgentConversation.id == conversation_id)
        )
        persisted_product = (
            session.scalar(select(Product).where(Product.id == persisted.product_id))
            if persisted is not None
            else None
        )
        if (
            persisted is not None
            and persisted.intake_idempotency_key == normalized_key
            and persisted.intake_request_hash == request_hash
            and persisted_product is not None
            and persisted_product.intake_json is not None
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
    source_note: str | None = None,
    storage: LocalStorage | None = None,
) -> AgentProductWorkspaceCreation:
    """原子绑定新上传参考图与不可变 intake。本函数 commit。"""
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
        if source_note is not None:
            locked.product.source_note = source_note.strip() or None
        return _write_intake_and_commit(
            session,
            conversation_id=conversation_id,
            conversation=locked.conversation,
            product=locked.product,
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
    """把已上传商品图绑定为不可变 intake。本函数 commit。"""
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
        product=locked.product,
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
    """对账 intake，不重放 finalize。记录在但聚合读不齐则 unknown。"""
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
    if creation.product.intake_json is None:
        return AgentProductWorkspaceReconcileResult(
            state="unknown",
            detail="商品输入幂等记录与商品 intake 不一致",
        )
    return AgentProductWorkspaceReconcileResult(state="applied", creation=creation)


def _intake_direct_create_types(intake: WorkflowIntakeV1) -> list[DirectCreateImageType]:
    return [
        DirectCreateImageType(key=item.key, quantity=item.quantity, order=item.order)
        for item in intake.image_types
    ]


def _intake_delivery_spec_json(intake: WorkflowIntakeV1) -> dict[str, object] | None:
    if intake.delivery_spec is None:
        return None
    return intake.delivery_spec.model_dump(mode="json")


def _expand_birth_graph_from_intake(
    session: Session,
    *,
    product: Product,
    intake: WorkflowIntakeV1,
) -> None:
    """名称-only 出生图在 intake 落定后扩成与直接创建相同的模板。已有其它节点则不动。"""

    if not intake.image_types or not intake.reference_asset_ids:
        return
    graph = get_active_workflow_graph(session, product_id=product.id)
    if graph is None:
        _stage_live_graph_for_workspace(
            session,
            product=product,
            intake=intake,
            created_assets=[],
        )
        return
    applied = load_applied_graph(session, graph)
    product_sources = [node for node in applied.nodes if node.node_type == GraphNodeType.PRODUCT_SOURCE]
    if len(product_sources) != 1 or any(node.node_type != GraphNodeType.PRODUCT_SOURCE for node in applied.nodes):
        return
    change_set = template_for_existing_product_source(
        product_source_node_id=product_sources[0].id,
        base_graph_revision=applied.revision,
        image_types=_intake_direct_create_types(intake),
        reference_asset_ids=list(intake.reference_asset_ids),
        product_title=product.name,
        source_product_id=product.id,
        fact_set_version_id=product.current_fact_set_version_id,
        source_note=product.source_note,
        delivery_spec=_intake_delivery_spec_json(intake),
    )
    stage_apply_graph_change_set(
        session,
        product_id=product.id,
        graph_id=graph.id,
        change_set=change_set,
    )


def _stage_live_graph_for_workspace(
    session: Session,
    *,
    product: Product,
    intake: WorkflowIntakeV1 | None,
    created_assets: list[ProductImageAsset],
) -> None:
    del created_assets
    if get_active_workflow_graph(session, product_id=product.id) is not None:
        return
    fact_set_version_id = product.current_fact_set_version_id
    if intake is not None and intake.image_types and intake.reference_asset_ids:
        change_set = build_direct_create_template(
            image_types=_intake_direct_create_types(intake),
            reference_asset_ids=list(intake.reference_asset_ids),
            product_title=product.name,
            source_product_id=product.id,
            fact_set_version_id=fact_set_version_id,
            source_note=product.source_note,
            delivery_spec=_intake_delivery_spec_json(intake),
        )
    else:
        change_set = build_product_source_create_graph(
            product_title=product.name,
            source_product_id=product.id,
            fact_set_version_id=fact_set_version_id,
        )
    stage_new_workflow_graph(
        session,
        product_id=product.id,
        change_set=change_set,
        title=product.name,
    )


def _require_canvas_session(agent_session, *, product_id: str) -> None:
    if agent_session.status != AgentSessionStatus.ACTIVE:
        raise ConflictError("已归档的 Agent Session 不能创建商品工作区")
    if agent_session.product_id is None:
        raise ConflictError("全局 Agent Session 不能作为画布会话")
    if agent_session.product_id != product_id:
        raise ConflictError("Agent Session 不属于当前商品")


def _stage_workspace_records(
    session: Session,
    *,
    product: Product,
    creation_idempotency_key: str,
    creation_request_hash: str,
    intake: WorkflowIntakeV1 | None,
    agent_session_id: str | None,
    created_assets: list[ProductImageAsset],
    fork_global_session: bool = False,
) -> AgentConversation:
    """只 flush 工作区行。调用方持有事务；Session 归属商品，product conversation 才是 Turn 投影。"""
    if intake is not None:
        write_product_intake(product, intake)
    _stage_live_graph_for_workspace(
        session,
        product=product,
        intake=intake,
        created_assets=created_assets,
    )

    conversation_id = new_id()
    if agent_session_id is None:
        agent_session = new_agent_session(title=product.name, product_id=product.id)
        session.add(agent_session)
        session.flush()
    else:
        agent_session = get_agent_session_or_raise(session, agent_session_id)
        if agent_session.product_id is None:
            if not fork_global_session:
                raise ConflictError("全局 Agent Session 不能作为画布会话")
            agent_session = new_agent_session(title=product.name, product_id=product.id)
            session.add(agent_session)
            session.flush()
        else:
            _require_canvas_session(agent_session, product_id=product.id)

    conversation = AgentConversation(
        id=conversation_id,
        session_id=agent_session.id,
        product_id=product.id,
        workflow_draft_id=None,
        harness_run_id=conversation_id,
        status=AgentConversationStatus.COLLECTING,
        creation_idempotency_key=creation_idempotency_key,
        creation_request_hash=creation_request_hash,
    )
    session.add(conversation)
    session.flush()
    refresh_agent_session_summary(session, agent_session.id)
    return conversation


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
    product = session.scalar(select(Product).where(Product.id == conversation.product_id))
    if product is None:
        raise ConflictError("Agent 商品工作空间聚合不完整")
    intake = parse_product_intake(product)
    assets = (
        get_product_image_assets_by_ids(
            session,
            product_id=product.id,
            asset_ids=list(intake.reference_asset_ids),
        )
        if intake is not None
        else []
    )
    return AgentProductWorkspaceCreation(
        product=product,
        created_assets=assets,
        conversation=conversation,
        created=created,
    )


__all__ = [
    "AgentProductWorkspaceCreation",
    "AgentProductWorkspaceReconcileResult",
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
