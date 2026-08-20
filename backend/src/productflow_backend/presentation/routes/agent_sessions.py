from __future__ import annotations

from fastapi import APIRouter, Depends, Query, status
from sqlalchemy.orm import Session

from productflow_backend.application.agent.sessions import (
    AGENT_SESSION_LIST_MAX_ITEMS,
    archive_agent_session,
    create_agent_session,
    ensure_global_agent_conversations,
    list_agent_sessions,
    rename_agent_session,
)
from productflow_backend.presentation.deps import get_session, require_admin
from productflow_backend.presentation.schemas.agent_sessions import (
    AgentSessionListResponse,
    AgentSessionResponse,
    RenameAgentSessionRequest,
    serialize_agent_session,
)

router = APIRouter(
    prefix="/api/v2/agent-sessions",
    tags=["agent-sessions"],
    dependencies=[Depends(require_admin)],
)


@router.get("", response_model=AgentSessionListResponse)
def list_agent_sessions_endpoint(
    include_archived: bool = Query(default=False),
    session: Session = Depends(get_session),
) -> AgentSessionListResponse:
    ensure_global_agent_conversations(session)
    return AgentSessionListResponse(
        items=[
            serialize_agent_session(agent_session)
            for agent_session in list_agent_sessions(session, include_archived=include_archived)
        ][:AGENT_SESSION_LIST_MAX_ITEMS]
    )


@router.post("", response_model=AgentSessionResponse, status_code=status.HTTP_201_CREATED)
def create_agent_session_endpoint(
    session: Session = Depends(get_session),
) -> AgentSessionResponse:
    return serialize_agent_session(create_agent_session(session))


@router.patch("/{session_id}", response_model=AgentSessionResponse)
def rename_agent_session_endpoint(
    session_id: str,
    payload: RenameAgentSessionRequest,
    session: Session = Depends(get_session),
) -> AgentSessionResponse:
    return serialize_agent_session(
        rename_agent_session(session, session_id=session_id, title=payload.title)
    )


@router.post("/{session_id}/archive", response_model=AgentSessionResponse)
def archive_agent_session_endpoint(
    session_id: str,
    session: Session = Depends(get_session),
) -> AgentSessionResponse:
    return serialize_agent_session(archive_agent_session(session, session_id=session_id))


__all__ = ["router"]
