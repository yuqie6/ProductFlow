"""AgentSession 是长期会话容器。Turn runtime 仍走 conversation 投影，不把 Session 当 transcript 权威。"""

from __future__ import annotations

import re
from datetime import UTC, datetime

from sqlalchemy import case, func, select
from sqlalchemy.orm import Session, selectinload

from productflow_backend.application.time import now_utc
from productflow_backend.domain.enums import AgentConversationScope, AgentSessionStatus
from productflow_backend.domain.errors import BusinessValidationError, ConflictError, NotFoundError
from productflow_backend.infrastructure.db.models import AgentConversation, AgentSession, new_id

AGENT_SESSION_TITLE_MAX_LENGTH = 160
AGENT_SESSION_LIST_MAX_ITEMS = 100
AGENT_SESSION_DEFAULT_TITLE = "新会话"


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


def new_agent_session(*, title: str | None = None, product_id: str | None = None) -> AgentSession:
    if title is None:
        return AgentSession(title=AGENT_SESSION_DEFAULT_TITLE, product_id=product_id)
    normalized = title.strip()
    if not normalized:
        raise BusinessValidationError("Agent Session 名称不能为空")
    return AgentSession(title=normalized[:AGENT_SESSION_TITLE_MAX_LENGTH], product_id=product_id)


def derive_agent_session_title(input_text: str) -> str:
    """从首条用户任务中提取稳定、短的临时 Session 标题。"""
    normalized = " ".join(input_text.split())
    if not normalized:
        return AGENT_SESSION_DEFAULT_TITLE
    first_sentence = re.split(r"[\r\n。！？!?；;]", normalized, maxsplit=1)[0]
    candidate = re.sub(r"^#+\s*", "", first_sentence).strip(" ，,：:。！？!?；;")
    return (candidate or normalized)[:AGENT_SESSION_TITLE_MAX_LENGTH]


def auto_name_agent_session(agent_session: AgentSession, *, input_text: str) -> bool:
    if agent_session.title != AGENT_SESSION_DEFAULT_TITLE:
        return False
    next_title = derive_agent_session_title(input_text)
    if next_title == AGENT_SESSION_DEFAULT_TITLE:
        return False
    agent_session.title = next_title
    agent_session.updated_at = now_utc()
    return True


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
    product_id: str | None = None,
) -> list[AgentSession]:
    latest_conversation_at = (
        select(func.max(AgentConversation.updated_at))
        .where(AgentConversation.session_id == AgentSession.id)
        .correlate(AgentSession)
        .scalar_subquery()
    )
    latest_activity_at = case(
        (latest_conversation_at.is_(None), AgentSession.updated_at),
        (latest_conversation_at > AgentSession.updated_at, latest_conversation_at),
        else_=AgentSession.updated_at,
    )
    statement = agent_session_query().order_by(
        latest_activity_at.desc(),
        AgentSession.id.desc(),
    )
    if product_id is None:
        statement = statement.where(AgentSession.product_id.is_(None))
    else:
        statement = statement.where(AgentSession.product_id == product_id)
    if not include_archived:
        statement = statement.where(AgentSession.status == AgentSessionStatus.ACTIVE)
    return list(session.scalars(statement.limit(AGENT_SESSION_LIST_MAX_ITEMS)).unique().all())


def create_agent_session(
    session: Session,
    *,
    title: str | None = None,
    product_id: str | None = None,
) -> AgentSession:
    """创建长期 Session。全局 Session 带 GLOBAL conversation；画布 Session 必须带 product_id。本函数 commit。"""
    if product_id is not None:
        raise BusinessValidationError("画布 Session 请通过商品工作台创建")
    agent_session = new_agent_session(title=title, product_id=None)
    session.add(agent_session)
    session.flush()
    session.add(
        AgentConversation(
            id=new_id(),
            scope_type=AgentConversationScope.GLOBAL,
            session_id=agent_session.id,
            harness_run_id=new_id(),
        )
    )
    session.commit()
    return get_agent_session_or_raise(session, agent_session.id)


def ensure_global_agent_conversation(
    session: Session,
    *,
    session_id: str,
) -> AgentConversation:
    """旧 Session 首次进入全局 Dock 时补建 GLOBAL conversation。本函数 commit。"""
    agent_session = get_agent_session_or_raise(session, session_id)
    existing = session.scalar(
        select(AgentConversation).where(
            AgentConversation.session_id == agent_session.id,
            AgentConversation.scope_type == AgentConversationScope.GLOBAL,
        )
    )
    if existing is not None:
        return existing
    if agent_session.status == AgentSessionStatus.ARCHIVED:
        raise ConflictError("已归档的 Agent Session 不能创建全局 conversation")
    conversation = AgentConversation(
        id=new_id(),
        scope_type=AgentConversationScope.GLOBAL,
        session_id=agent_session.id,
        harness_run_id=new_id(),
    )
    session.add(conversation)
    session.commit()
    return session.get(AgentConversation, conversation.id) or conversation


def ensure_global_agent_conversations(session: Session) -> None:
    """给 global scope 出现前创建的全局 Session 惰性补 GLOBAL conversation。有写入时 commit。"""
    sessions = list(session.scalars(select(AgentSession).where(AgentSession.product_id.is_(None))).all())
    changed = False
    for agent_session in sessions:
        existing = session.scalar(
            select(AgentConversation.id).where(
                AgentConversation.session_id == agent_session.id,
                AgentConversation.scope_type == AgentConversationScope.GLOBAL,
            )
        )
        if existing is not None or agent_session.status == AgentSessionStatus.ARCHIVED:
            continue
        session.add(
            AgentConversation(
                id=new_id(),
                scope_type=AgentConversationScope.GLOBAL,
                session_id=agent_session.id,
                harness_run_id=new_id(),
            )
        )
        changed = True
    if changed:
        session.commit()


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
    "AGENT_SESSION_DEFAULT_TITLE",
    "AGENT_SESSION_TITLE_MAX_LENGTH",
    "archive_agent_session",
    "agent_session_query",
    "auto_name_agent_session",
    "create_agent_session",
    "derive_agent_session_title",
    "ensure_global_agent_conversation",
    "ensure_global_agent_conversations",
    "get_agent_session_or_raise",
    "list_agent_sessions",
    "new_agent_session",
    "normalize_agent_session_title",
    "rename_agent_session",
]
