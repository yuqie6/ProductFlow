from __future__ import annotations

import logging
from collections.abc import Callable
from dataclasses import dataclass

from sqlalchemy import or_, select
from sqlalchemy.orm import Session, selectinload

from productflow_backend.application.agent.control import (
    adopt_queued_agent_turn_start,
    retry_unbound_agent_turn_start,
    synchronize_agent_turn_state,
)
from productflow_backend.application.agent.conversations import record_agent_turn_start_error, reserve_agent_turn
from productflow_backend.application.agent.execution import recover_expired_agent_turn_executions
from productflow_backend.application.agent.tasks import initial_agent_task_turn_idempotency_key
from productflow_backend.config import get_settings
from productflow_backend.domain.enums import (
    AgentConversationScope,
    AgentExecutionPhase,
    AgentTaskStatus,
    AgentTurnStatus,
    AsyncDispatchStatus,
)
from productflow_backend.domain.errors import BusinessError
from productflow_backend.infrastructure.agent_service import (
    AgentServiceClient,
    AgentServiceRequestError,
    get_agent_service_client,
)
from productflow_backend.infrastructure.db.models import (
    AgentConversation,
    AgentTask,
    AgentTurnExecution,
    AgentTurnProjection,
    AsyncDispatch,
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
    recovered_task_turns: int = 0


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
            execution = session.scalar(
                select(AgentTurnExecution).where(AgentTurnExecution.turn_projection_id == projection.id)
            )
            try:
                state = client.get_turn(
                    conversation_id=conversation.id,
                    turn_id=projection.harness_turn_id,
                    task_id=projection.task_id,
                )
            except AgentServiceRequestError as exc:
                if not (
                    exc.status_code == 404
                    and projection.status == AgentTurnStatus.QUEUED
                    and execution is not None
                    and execution.owner_id is None
                    and execution.phase == AgentExecutionPhase.CLAIMED
                ):
                    raise
                projection = adopt_queued_agent_turn_start(
                    session,
                    projection=projection,
                    gateway=client,
                    # The other Agent instance starts draining immediately
                    # after start_turn returns. Commit its ProductFlow
                    # projection before asking that instance for more state,
                    # otherwise its first durable event can wait on this
                    # transaction's projection lock.
                    commit=True,
                )
                state = client.get_turn(
                    conversation_id=conversation.id,
                    turn_id=projection.harness_turn_id or "",
                    task_id=projection.task_id,
                )
            if projection.status == AgentTurnStatus.QUEUED and state.status == AgentTurnStatus.QUEUED:
                if (
                    execution is not None
                    and execution.owner_id is None
                    and execution.phase == AgentExecutionPhase.CLAIMED
                ):
                    state = client.resume_turn(
                        conversation_id=conversation.id,
                        turn_id=projection.harness_turn_id,
                        task_id=projection.task_id,
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
    stage_dispatch: Callable[[Session, str], AsyncDispatch | None] | None = None,
) -> AgentTurnRecoverySummary:
    session = get_session_factory()()
    staged_projection_ids: list[str] = []
    try:
        recover_expired_agent_turn_executions(session)
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
        recovered_task_projection_ids, recovered_task_turns = _recover_queued_task_turns(session)
        projection_ids.extend(recovered_task_projection_ids)
        if stage_dispatch is not None:
            active_dispatch_projection_ids = set(
                session.scalars(
                    select(AsyncDispatch.aggregate_id).where(
                        AsyncDispatch.actor_name == "run_agent_turn_sync",
                        AsyncDispatch.aggregate_id.in_(projection_ids),
                        AsyncDispatch.status.in_(
                            (AsyncDispatchStatus.PENDING, AsyncDispatchStatus.SENT)
                        ),
                    )
                ).all()
            ) if projection_ids else set()
            for projection_id in projection_ids:
                if projection_id in active_dispatch_projection_ids:
                    continue
                dispatch = stage_dispatch(session, projection_id)
                if dispatch is None or dispatch.status != AsyncDispatchStatus.DEAD:
                    staged_projection_ids.append(projection_id)
            session.commit()
    except Exception:
        session.rollback()
        logger.exception("读取待恢复 Agent Turn 投影失败")
        raise
    finally:
        session.close()

    enqueued = len(staged_projection_ids) if stage_dispatch is not None else 0
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
        recovered_task_turns=recovered_task_turns,
    )


def _recover_queued_task_turns(session: Session) -> tuple[list[str], int]:
    tasks = list(
        session.scalars(
            select(AgentTask)
            .options(selectinload(AgentTask.conversation))
            .where(
                AgentTask.status == AgentTaskStatus.QUEUED,
                AgentTask.current_turn_id.is_(None),
                AgentTask.conversation_id.is_not(None),
            )
            .order_by(AgentTask.created_at.asc(), AgentTask.id.asc())
        ).all()
    )
    projection_ids: list[str] = []
    recovered_task_turns = 0
    for task in tasks:
        conversation = task.conversation
        if conversation is None:
            continue
        product_id = (
            conversation.product_id
            if conversation.scope_type == AgentConversationScope.PRODUCT_WORKFLOW
            else None
        )
        try:
            reservation = reserve_agent_turn(
                session,
                product_id=product_id,
                conversation_id=conversation.id,
                input_text=task.goal,
                input_asset_ids=[],
                idempotency_key=initial_agent_task_turn_idempotency_key(
                    conversation_id=conversation.id,
                    task_id=task.id,
                ),
                task_id=task.id,
            )
        except BusinessError as exc:
            session.rollback()
            logger.warning(
                "跳过尚未满足启动条件的 Agent Task 首轮恢复: task_id=%s detail=%s",
                task.id,
                exc,
            )
            continue
        projection_ids.append(reservation.projection.id)
        if reservation.created:
            recovered_task_turns += 1
    return projection_ids, recovered_task_turns


def _poll_delay_ms() -> int:
    return max(1, int(get_settings().agent_turn_sync_poll_seconds * 1000))


__all__ = [
    "AgentTurnRecoverySummary",
    "execute_agent_turn_sync",
    "recover_unfinished_agent_turn_syncs",
]
