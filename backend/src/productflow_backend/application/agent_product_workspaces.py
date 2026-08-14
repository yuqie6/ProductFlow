from __future__ import annotations

from dataclasses import dataclass

from pydantic import ValidationError
from sqlalchemy import select
from sqlalchemy.exc import IntegrityError
from sqlalchemy.orm import Session

from productflow_backend.application.agent_conversations import agent_conversation_query
from productflow_backend.application.agent_product_intake import (
    WORKFLOW_INTAKE_SCHEMA_VERSION,
    AgentProductSelectionV1,
    WorkflowIntakeV1,
    agent_product_workspace_request_hash,
    normalize_agent_product_idempotency_key,
)
from productflow_backend.application.media_assets import get_product_image_assets_by_ids
from productflow_backend.application.storage_compensation import compensate_storage_writes
from productflow_backend.application.use_cases import (
    normalize_product_name,
    stage_canonical_product_with_assets,
)
from productflow_backend.application.workflow_drafts.service import workflow_draft_query
from productflow_backend.domain.enums import AgentConversationStatus, WorkflowDraftStatus
from productflow_backend.domain.errors import ConflictError
from productflow_backend.infrastructure.db.models import (
    AgentConversation,
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
    created: bool


def create_agent_product_workspace(
    session: Session,
    *,
    name: str,
    selection: AgentProductSelectionV1,
    image_uploads: list[tuple[bytes, str, str]],
    idempotency_key: str,
    storage: LocalStorage | None = None,
) -> AgentProductWorkspaceCreation:
    normalized_name = normalize_product_name(name)
    normalized_key = normalize_agent_product_idempotency_key(idempotency_key)
    request_hash = agent_product_workspace_request_hash(
        normalized_product_name=normalized_name,
        selection=selection,
        image_uploads=image_uploads,
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
            draft = WorkflowDraft(
                product_id=canonical.product.id,
                status=WorkflowDraftStatus.COLLECTING,
                intake_schema_version=WORKFLOW_INTAKE_SCHEMA_VERSION,
                intake_json=intake.model_dump(mode="json"),
            )
            session.add(draft)
            session.flush()

            conversation_id = new_id()
            conversation = AgentConversation(
                id=conversation_id,
                product_id=canonical.product.id,
                workflow_draft_id=draft.id,
                harness_run_id=conversation_id,
                status=AgentConversationStatus.COLLECTING,
                creation_idempotency_key=normalized_key,
                creation_request_hash=request_hash,
            )
            session.add(conversation)
            session.commit()
    except IntegrityError:
        existing = _conversation_by_creation_key(session, normalized_key)
        if existing is None:
            raise
        return _load_idempotent_workspace(
            session,
            conversation=existing,
            request_hash=request_hash,
            created=False,
        )

    session.expire_all()
    persisted = _conversation_by_creation_key(session, normalized_key)
    if persisted is None:
        raise ConflictError("Agent 商品创建结果无法重新读取")
    return _load_idempotent_workspace(
        session,
        conversation=persisted,
        request_hash=request_hash,
        created=True,
    )


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
    draft = session.scalar(
        workflow_draft_query().where(WorkflowDraft.id == conversation.workflow_draft_id)
    )
    product = session.scalar(select(Product).where(Product.id == conversation.product_id))
    if draft is None or product is None:
        raise ConflictError("Agent 商品创建聚合不完整")
    if draft.intake_schema_version != WORKFLOW_INTAKE_SCHEMA_VERSION or draft.intake_json is None:
        raise ConflictError("Agent 商品创建 intake 缺失或版本不受支持")
    try:
        intake = WorkflowIntakeV1.model_validate(draft.intake_json)
    except ValidationError as exc:
        raise ConflictError("Agent 商品创建 intake 不符合 schema version 1") from exc
    assets = get_product_image_assets_by_ids(
        session,
        product_id=product.id,
        asset_ids=list(intake.reference_asset_ids),
    )
    return AgentProductWorkspaceCreation(
        product=product,
        created_assets=assets,
        workflow_draft=draft,
        conversation=conversation,
        created=created,
    )


__all__ = ["AgentProductWorkspaceCreation", "create_agent_product_workspace"]
