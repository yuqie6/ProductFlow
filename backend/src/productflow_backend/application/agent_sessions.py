from __future__ import annotations

from datetime import UTC, datetime

from sqlalchemy import func, select
from sqlalchemy.orm import Session, selectinload

from productflow_backend.application.time import now_utc
from productflow_backend.domain.enums import AgentSessionStatus
from productflow_backend.domain.errors import BusinessValidationError, NotFoundError
from productflow_backend.infrastructure.db.models import AgentConversation, AgentSession

AGENT_SESSION_TITLE_MAX_LENGTH = 160
AGENT_SESSION_LIST_MAX_ITEMS = 100


def agent_session_query():
    return select(AgentSession).options(
        selectinload(AgentSession.conversations).selectinload(AgentConversation.product),
    )


def normalize_agent_session_title(value: str) -> str:
    normalized = value.strip()
    if not normalized:
        raise BusinessValidationError("Agent Session 名称不能为空")
    if len(normalized) > AGENT_SESSION_TITLE_MAX_LENGTH:
        raise BusinessValidationError(
            f"Agent Session 名称不能超过 {AGENT_SESSION_TITLE_MAX_LENGTH} 个字符"
        )
    return normalized


def new_agent_session(*, title: str) -> AgentSession:
    normalized = title.strip()
    if not normalized:
        raise BusinessValidationError("Agent Session 名称不能为空")
    return AgentSession(title=normalized[:AGENT_SESSION_TITLE_MAX_LENGTH])


def get_agent_session_or_raise(
    session: Session,
    session_id: str,
) -> AgentSession:
    agent_session = session.scalar(agent_session_query().where(AgentSession.id == session_id))
    if agent_session is None:
        raise NotFoundError("Agent Session 不存在")
    return agent_session


def list_agent_sessions(
    session: Session,
    *,
    include_archived: bool = False,
) -> list[AgentSession]:
    latest_conversation_at = (
        select(func.max(AgentConversation.updated_at))
        .where(AgentConversation.session_id == AgentSession.id)
        .correlate(AgentSession)
        .scalar_subquery()
    )
    statement = agent_session_query().order_by(
        func.coalesce(latest_conversation_at, AgentSession.updated_at).desc(),
        AgentSession.id.desc(),
    )
    if not include_archived:
        statement = statement.where(AgentSession.status == AgentSessionStatus.ACTIVE)
    return list(session.scalars(statement.limit(AGENT_SESSION_LIST_MAX_ITEMS)).unique().all())


def create_agent_session(
    session: Session,
    *,
    title: str,
) -> AgentSession:
    agent_session = new_agent_session(title=title)
    session.add(agent_session)
    session.commit()
    return get_agent_session_or_raise(session, agent_session.id)


def rename_agent_session(
    session: Session,
    *,
    session_id: str,
    title: str,
) -> AgentSession:
    agent_session = get_agent_session_or_raise(session, session_id)
    agent_session.title = normalize_agent_session_title(title)
    agent_session.updated_at = now_utc()
    session.commit()
    return get_agent_session_or_raise(session, session_id)


def archive_agent_session(
    session: Session,
    *,
    session_id: str,
) -> AgentSession:
    agent_session = get_agent_session_or_raise(session, session_id)
    if agent_session.status != AgentSessionStatus.ARCHIVED:
        agent_session.status = AgentSessionStatus.ARCHIVED
        agent_session.archived_at = datetime.now(UTC)
        agent_session.updated_at = now_utc()
        session.commit()
    return get_agent_session_or_raise(session, session_id)


__all__ = [
    "AGENT_SESSION_LIST_MAX_ITEMS",
    "AGENT_SESSION_TITLE_MAX_LENGTH",
    "archive_agent_session",
    "agent_session_query",
    "create_agent_session",
    "get_agent_session_or_raise",
    "list_agent_sessions",
    "new_agent_session",
    "normalize_agent_session_title",
    "rename_agent_session",
]
