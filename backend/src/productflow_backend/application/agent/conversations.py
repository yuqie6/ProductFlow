"""Agent conversation 的查找与 conversation 级状态转换。"""

from __future__ import annotations

from sqlalchemy import and_, select
from sqlalchemy.orm import Session, selectinload

from productflow_backend.domain.enums import AgentConversationScope
from productflow_backend.domain.errors import NotFoundError
from productflow_backend.infrastructure.db.models import (
    AgentConversation,
    LibraryOrganizationDraft,
    Product,
)

PRODUCT_WORKFLOW_SCOPE = AgentConversationScope.PRODUCT_WORKFLOW
GLOBAL_SCOPE = AgentConversationScope.GLOBAL


def agent_conversation_query():
    return select(AgentConversation).options(
        selectinload(AgentConversation.session),
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


__all__ = [
    "GLOBAL_SCOPE",
    "PRODUCT_WORKFLOW_SCOPE",
    "agent_conversation_query",
    "get_agent_conversation_by_id_or_raise",
    "get_agent_conversation_or_raise",
]
