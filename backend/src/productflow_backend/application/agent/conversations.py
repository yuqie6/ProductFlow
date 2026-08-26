"""Agent conversation lookup, creation, and conversation-level status transitions."""

from __future__ import annotations

from sqlalchemy import and_, select
from sqlalchemy.exc import IntegrityError
from sqlalchemy.orm import Session, selectinload

from productflow_backend.application.agent.sessions import new_agent_session
from productflow_backend.application.agent.tasks import update_agent_task_from_turn
from productflow_backend.application.time import now_utc
from productflow_backend.domain.enums import (
    AgentConversationScope,
    AgentConversationStatus,
    AgentTurnStatus,
    WorkflowDraftStatus,
)
from productflow_backend.domain.errors import ConflictError, NotFoundError
from productflow_backend.infrastructure.db.models import (
    AgentConversation,
    AgentTurnProjection,
    LibraryOrganizationDraft,
    Product,
    WorkflowDraft,
    WorkflowDraftLegacyArchiveSeed,
    WorkflowDraftRecipeSeed,
    new_id,
)

PRODUCT_WORKFLOW_SCOPE = AgentConversationScope.PRODUCT_WORKFLOW
GLOBAL_SCOPE = AgentConversationScope.GLOBAL
WORKFLOW_DRAFT_ARTIFACT_NAME = "propose_workflow_draft"


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
    """Retained history helper for Draft-bound conversations."""
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


def attach_agent_workflow_draft_artifact(
    session: Session,
    *,
    product_id: str,
    conversation_id: str,
    projection_id: str,
    harness_turn_id: str,
    artifact_name: str,
    artifact_step_id: str,
    artifact_value: dict,
    commit: bool = True,
) -> AgentTurnProjection:
    """Retired Draft artifact entrypoint kept only for historical callers."""
    from productflow_backend.application.workflow_drafts.service import PRODUCT_WORKFLOW_DRAFT_RETIRED

    del session, product_id, conversation_id, projection_id, harness_turn_id
    del artifact_name, artifact_step_id, artifact_value, commit
    raise ConflictError(PRODUCT_WORKFLOW_DRAFT_RETIRED)


def mark_agent_conversation_completed_for_draft(
    session: Session,
    *,
    product_id: str,
    workflow_draft_id: str,
    commit: bool = True,
) -> AgentConversation | None:
    conversation = session.scalar(
        select(AgentConversation)
        .options(
            selectinload(AgentConversation.workflow_draft).selectinload(WorkflowDraft.current_revision),
        )
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
    projection = session.scalar(
        select(AgentTurnProjection)
        .where(
            AgentTurnProjection.conversation_id == conversation.id,
            AgentTurnProjection.workflow_draft_revision_id == conversation.workflow_draft.current_revision_id,
        )
        .with_for_update()
    )
    if projection is not None and projection.status == AgentTurnStatus.AWAITING_CONFIRMATION:
        finished_at = now_utc()
        projection.status = AgentTurnStatus.SUCCEEDED
        projection.finished_at = finished_at
        projection.updated_at = finished_at
        update_agent_task_from_turn(
            session,
            projection=projection,
            status=AgentTurnStatus.SUCCEEDED,
            error_text=None,
            finished_at=finished_at,
        )
    conversation.status = AgentConversationStatus.COMPLETED
    conversation.updated_at = now_utc()
    if commit:
        session.commit()
    return conversation


__all__ = [
    "GLOBAL_SCOPE",
    "PRODUCT_WORKFLOW_SCOPE",
    "WORKFLOW_DRAFT_ARTIFACT_NAME",
    "agent_conversation_query",
    "attach_agent_workflow_draft_artifact",
    "create_agent_conversation",
    "get_agent_conversation_by_id_or_raise",
    "get_agent_conversation_or_raise",
    "mark_agent_conversation_completed_for_draft",
]
