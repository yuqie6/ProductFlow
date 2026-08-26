"""Agent conversation 的查找、创建与 conversation 级状态转换。"""

from __future__ import annotations

from typing import NoReturn

from sqlalchemy import and_, select
from sqlalchemy.orm import Session, selectinload

from productflow_backend.application.workflow_drafts.service import PRODUCT_WORKFLOW_DRAFT_RETIRED
from productflow_backend.domain.enums import AgentConversationScope
from productflow_backend.domain.errors import ConflictError, NotFoundError
from productflow_backend.infrastructure.db.models import (
    AgentConversation,
    LibraryOrganizationDraft,
    Product,
    WorkflowDraft,
)

PRODUCT_WORKFLOW_SCOPE = AgentConversationScope.PRODUCT_WORKFLOW
GLOBAL_SCOPE = AgentConversationScope.GLOBAL


def agent_conversation_query():
    return select(AgentConversation).options(
        selectinload(AgentConversation.session),
        selectinload(AgentConversation.workflow_draft).selectinload(WorkflowDraft.current_revision),
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
) -> NoReturn:
    """商品路径不再用 WorkflowDraft 绑定创建 conversation。"""
    del session, product_id, workflow_draft_id
    raise ConflictError(PRODUCT_WORKFLOW_DRAFT_RETIRED)


__all__ = [
    "GLOBAL_SCOPE",
    "PRODUCT_WORKFLOW_SCOPE",
    "agent_conversation_query",
    "create_agent_conversation",
    "get_agent_conversation_by_id_or_raise",
    "get_agent_conversation_or_raise",
]
