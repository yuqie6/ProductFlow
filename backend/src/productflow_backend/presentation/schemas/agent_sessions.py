from __future__ import annotations

from datetime import datetime

from pydantic import BaseModel, ConfigDict, Field

from productflow_backend.domain.enums import AgentConversationStatus, AgentSessionStatus
from productflow_backend.infrastructure.db.models import AgentSession


class StrictAgentSessionRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")


class CreateAgentSessionRequest(StrictAgentSessionRequest):
    title: str = Field(min_length=1, max_length=160)


class RenameAgentSessionRequest(StrictAgentSessionRequest):
    title: str = Field(min_length=1, max_length=160)


class AgentSessionConversationResponse(BaseModel):
    conversation_id: str
    product_id: str
    product_name: str
    conversation_status: AgentConversationStatus
    updated_at: datetime


class AgentSessionResponse(BaseModel):
    id: str
    title: str
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
            product_id=conversation.product_id,
            product_name=conversation.product.name,
            conversation_status=conversation.status,
            updated_at=conversation.updated_at,
        )
        for conversation in agent_session.conversations[:20]
        if conversation.product is not None
    ]
    return AgentSessionResponse(
        id=agent_session.id,
        title=agent_session.title,
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
    "CreateAgentSessionRequest",
    "RenameAgentSessionRequest",
    "serialize_agent_session",
]
