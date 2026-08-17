from __future__ import annotations

import logging
from collections.abc import Callable
from dataclasses import dataclass

from sqlalchemy import or_, select
from sqlalchemy.orm import Session, selectinload

from productflow_backend.application.agent_control import (
    retry_unbound_agent_turn_start,
    synchronize_agent_turn_state,
)
from productflow_backend.application.agent_conversations import record_agent_turn_start_error
from productflow_backend.config import get_settings
from productflow_backend.domain.enums import AgentTurnStatus
from productflow_backend.domain.errors import BusinessError
from productflow_backend.infrastructure.agent_service import (
    AgentServiceClient,
    AgentServiceRequestError,
    get_agent_service_client,
)
from productflow_backend.infrastructure.db.models import (
    AgentConversation,
    AgentTurnProjection,
    WorkflowDraft,
)
from productflow_backend.infrastructure.db.session import get_session_factory

logger = logging.getLogger(__name__)

_POLLABLE_AGENT_TURN_STATUSES = {
    AgentTurnStatus.QUEUED,
    AgentTurnStatus.RUNNING,
    AgentTurnStatus.CANCEL_REQUESTED,
}


@dataclass(frozen=True, slots=True)
class AgentTurnRecoverySummary:
    pending_turns: int = 0
    enqueued_turns: int = 0


def execute_agent_turn_sync(
    projection_id: str,
    *,
    gateway: AgentServiceClient | None = None,
    enqueue_later: Callable[[Session, str, int], None],
) -> None:
    session = get_session_factory()()
    try:
        projection = session.scalar(
            select(AgentTurnProjection)
            .options(
                selectinload(AgentTurnProjection.conversation)
                .selectinload(AgentConversation.workflow_draft)
                .selectinload(WorkflowDraft.current_revision)
            )
            .where(AgentTurnProjection.id == projection_id)
        )
        if projection is None or projection.resume_required:
            return
        client = gateway or get_agent_service_client()
        conversation = projection.conversation
        if projection.harness_turn_id is None:
            projection = retry_unbound_agent_turn_start(
                session,
                projection=projection,
                gateway=client,
                commit=False,
            )
        else:
            state = client.get_turn(
                conversation_id=conversation.id,
                turn_id=projection.harness_turn_id,
            )
            projection = synchronize_agent_turn_state(
                session,
                product_id=conversation.product_id,
                conversation_id=conversation.id,
                projection_id=projection.id,
                state=state,
                commit=False,
            )
        if projection.status in _POLLABLE_AGENT_TURN_STATUSES and not projection.resume_required:
            enqueue_later(session, projection.id, _poll_delay_ms())
        session.commit()
    except AgentServiceRequestError as exc:
        session.rollback()
        projection = session.get(AgentTurnProjection, projection_id)
        if projection is not None:
            conversation = projection.conversation
            record_agent_turn_start_error(
                session,
                product_id=conversation.product_id,
                conversation_id=conversation.id,
                projection_id=projection.id,
                safe_error=(
                    f"Agent 请求被拒绝: {exc.code}"
                    if exc.status_code is not None and exc.status_code < 500
                    else "Agent 服务暂时不可用"
                ),
            )
            if exc.status_code is None or exc.status_code >= 500:
                enqueue_later(session, projection.id, _poll_delay_ms())
                session.commit()
    except BusinessError as exc:
        session.rollback()
        projection = session.get(AgentTurnProjection, projection_id)
        if projection is not None:
            conversation = projection.conversation
            record_agent_turn_start_error(
                session,
                product_id=conversation.product_id,
                conversation_id=conversation.id,
                projection_id=projection.id,
                safe_error=str(exc),
            )
    except Exception:
        session.rollback()
        logger.exception("Agent Turn 投影同步失败: projection_id=%s", projection_id)
        raise
    finally:
        session.close()


def recover_unfinished_agent_turn_syncs(
    *,
    enqueue: Callable[[str], None] | None = None,
    stage_dispatch: Callable[[Session, str], None] | None = None,
) -> AgentTurnRecoverySummary:
    session = get_session_factory()()
    try:
        projection_ids = list(
            session.scalars(
                select(AgentTurnProjection.id)
                .where(
                    AgentTurnProjection.resume_required.is_(False),
                    or_(
                        AgentTurnProjection.status.in_(_POLLABLE_AGENT_TURN_STATUSES),
                        (
                            (AgentTurnProjection.status == AgentTurnStatus.AWAITING_CONFIRMATION)
                            & AgentTurnProjection.workflow_draft_revision_id.is_(None)
                        ),
                    ),
                )
                .order_by(AgentTurnProjection.created_at.asc(), AgentTurnProjection.id.asc())
            ).all()
        )
        if stage_dispatch is not None:
            for projection_id in projection_ids:
                stage_dispatch(session, projection_id)
            session.commit()
    except Exception:
        session.rollback()
        logger.exception("读取待恢复 Agent Turn 投影失败")
        raise
    finally:
        session.close()

    enqueued = len(projection_ids) if stage_dispatch is not None else 0
    if stage_dispatch is None:
        if enqueue is None:
            raise ValueError("enqueue or stage_dispatch is required")
        for projection_id in projection_ids:
            try:
                enqueue(projection_id)
                enqueued += 1
            except Exception:
                logger.exception("恢复 Agent Turn 同步任务入队失败: projection_id=%s", projection_id)
    return AgentTurnRecoverySummary(
        pending_turns=len(projection_ids),
        enqueued_turns=enqueued,
    )


def _poll_delay_ms() -> int:
    return max(1, int(get_settings().agent_turn_sync_poll_seconds * 1000))


__all__ = [
    "AgentTurnRecoverySummary",
    "execute_agent_turn_sync",
    "recover_unfinished_agent_turn_syncs",
]
