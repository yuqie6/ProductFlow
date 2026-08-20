from __future__ import annotations

from datetime import datetime

from pydantic import BaseModel, ConfigDict, Field

from productflow_backend.domain.enums import AgentConversationScope, AgentConversationStatus, AgentSessionStatus
from productflow_backend.infrastructure.db.models import AgentSession


class StrictAgentSessionRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")


class RenameAgentSessionRequest(StrictAgentSessionRequest):
    title: str = Field(min_length=1, max_length=160)


class AgentSessionConversationResponse(BaseModel):
    conversation_id: str
    scope_type: AgentConversationScope
    product_id: str | None
    product_name: str
    conversation_status: AgentConversationStatus
    updated_at: datetime


class AgentSessionResponse(BaseModel):
    id: str
    title: str
    summary: str | None
    status: AgentSessionStatus
    archived_at: datetime | None
    conversation_count: int
    conversations: list[AgentSessionConversationResponse]
    created_at: datetime
    updated_at: datetime


class AgentSessionListResponse(BaseModel):
    items: list[AgentSessionResponse]


def serialize_agent_session(agent_session: AgentSession) -> AgentSessionResponse:
    conversations = [
        AgentSessionConversationResponse(
            conversation_id=conversation.id,
            scope_type=conversation.scope_type,
            product_id=conversation.product_id,
            product_name=conversation.product.name if conversation.product is not None else "全局 Agent",
            conversation_status=conversation.status,
            updated_at=conversation.updated_at,
        )
        for conversation in sorted(
            agent_session.conversations,
            key=lambda conversation: (
                conversation.scope_type == AgentConversationScope.GLOBAL,
                -conversation.updated_at.timestamp(),
                conversation.id,
            ),
        )[:20]
    ]
    return AgentSessionResponse(
        id=agent_session.id,
        title=agent_session.title,
        summary=agent_session.summary,
        status=agent_session.status,
        archived_at=agent_session.archived_at,
        conversation_count=len(agent_session.conversations),
        conversations=conversations,
        created_at=agent_session.created_at,
        updated_at=agent_session.updated_at,
    )


__all__ = [
    "AgentSessionConversationResponse",
    "AgentSessionListResponse",
    "AgentSessionResponse",
    "RenameAgentSessionRequest",
    "serialize_agent_session",
]
