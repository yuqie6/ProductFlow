from __future__ import annotations

import json
from dataclasses import dataclass
from datetime import datetime, timedelta
from typing import Any

from sqlalchemy import select
from sqlalchemy.exc import IntegrityError
from sqlalchemy.orm import Session, selectinload

from productflow_backend.application.agent_conversations import project_agent_turn_state
from productflow_backend.application.time import now_utc
from productflow_backend.domain.enums import AgentCheckpointKind, AgentExecutionPhase, AgentTurnStatus
from productflow_backend.domain.errors import BusinessValidationError, ConflictError, NotFoundError
from productflow_backend.infrastructure.db.models import (
    AgentTurnCheckpoint,
    AgentTurnExecution,
    AgentTurnProjection,
    new_id,
)

DEFAULT_AGENT_EXECUTION_LEASE_SECONDS = 60
MAX_AGENT_CHECKPOINT_PAYLOAD_BYTES = 64 * 1024
MAX_AGENT_CHECKPOINT_SEQUENCE = 10_000
RESTART_UNKNOWN_ERROR = "Agent execution lease expired before this Turn reached a provable terminal state"

_ACTIVE_TURN_STATUSES = {
    AgentTurnStatus.QUEUED,
    AgentTurnStatus.RUNNING,
    AgentTurnStatus.REQUIRES_INPUT,
    AgentTurnStatus.CANCEL_REQUESTED,
}


@dataclass(frozen=True, slots=True)
class AgentExecutionLease:
    execution_id: str
    projection_id: str
    harness_turn_id: str
    owner_id: str
    lease_token: str
    attempt: int
    fencing_token: int
    phase: AgentExecutionPhase
    lease_expires_at: datetime


@dataclass(frozen=True, slots=True)
class AgentExecutionRecoverySummary:
    requeued: int = 0
    unknown: int = 0


@dataclass(frozen=True, slots=True)
class AgentCheckpointReceipt:
    id: str
    projection_id: str
    execution_id: str
    attempt: int
    fencing_token: int
    sequence: int
    kind: AgentCheckpointKind
    created_at: datetime


def claim_agent_turn_execution(
    session: Session,
    *,
    conversation_id: str,
    task_id: str | None,
    idempotency_key: str,
    harness_turn_id: str,
    owner_id: str,
    lease_seconds: int = DEFAULT_AGENT_EXECUTION_LEASE_SECONDS,
) -> AgentExecutionLease:
    normalized_key = idempotency_key.strip()
    normalized_turn_id = harness_turn_id.strip()
    normalized_owner_id = owner_id.strip()
    if not normalized_key or len(normalized_key) > 200:
        raise BusinessValidationError("Agent execution idempotency key 无效")
    if not normalized_turn_id or len(normalized_turn_id) > 120:
        raise BusinessValidationError("Agent execution harness turn ID 无效")
    if not normalized_owner_id or len(normalized_owner_id) > 120:
        raise BusinessValidationError("Agent execution owner ID 无效")
    if not 5 <= lease_seconds <= 600:
        raise BusinessValidationError("Agent execution lease 时长无效")

    statement = (
        select(AgentTurnProjection)
        .options(selectinload(AgentTurnProjection.conversation))
        .where(
            AgentTurnProjection.conversation_id == conversation_id,
            AgentTurnProjection.idempotency_key == normalized_key,
        )
        .with_for_update()
    )
    if task_id is None:
        statement = statement.where(AgentTurnProjection.task_id.is_(None))
    else:
        statement = statement.where(AgentTurnProjection.task_id == task_id)
    projection = session.scalar(statement)
    if projection is None:
        raise NotFoundError("Agent Turn projection 不存在")
    if projection.harness_turn_id not in {None, normalized_turn_id}:
        raise ConflictError("Agent Turn projection 已绑定其他 harness Turn")
    if projection.status in {
        AgentTurnStatus.AWAITING_CONFIRMATION,
        AgentTurnStatus.SUCCEEDED,
        AgentTurnStatus.FAILED,
        AgentTurnStatus.CANCELED,
        AgentTurnStatus.UNKNOWN,
    }:
        raise ConflictError("Agent Turn 已进入终态，不能重新 claim")

    execution = session.scalar(
        select(AgentTurnExecution)
        .where(AgentTurnExecution.turn_projection_id == projection.id)
        .with_for_update()
    )
    if execution is None:
        execution = AgentTurnExecution(
            turn_projection_id=projection.id,
            harness_turn_id=normalized_turn_id,
            attempt=0,
            fencing_token=0,
            phase=AgentExecutionPhase.CLAIMED,
        )
        session.add(execution)
        session.flush()
    elif execution.harness_turn_id != normalized_turn_id:
        raise ConflictError("Agent execution 已绑定其他 harness Turn")

    now = now_utc()
    active_until = execution.lease_expires_at
    if active_until is not None and active_until.tzinfo is None:
        active_until = active_until.replace(tzinfo=now.tzinfo)
    if (
        execution.owner_id == normalized_owner_id
        and execution.lease_token is not None
        and active_until is not None
        and active_until > now
    ):
        return _lease_from_execution(execution, normalized_owner_id)
    if (
        execution.owner_id is not None
        and execution.lease_token is not None
        and active_until is not None
        and active_until > now
        and execution.owner_id != normalized_owner_id
    ):
        raise ConflictError("Agent Turn 已被其他 Agent worker claim")
    if (
        execution.owner_id is not None
        and execution.lease_token is not None
        and active_until is not None
        and active_until <= now
        and execution.phase != AgentExecutionPhase.CLAIMED
    ):
        raise ConflictError("Agent Turn execution 已过期，必须先完成副作用对账")
    if (
        execution.owner_id is None
        and execution.lease_token is None
        and execution.phase not in {AgentExecutionPhase.CLAIMED}
    ):
        raise ConflictError("Agent Turn execution 已越过安全重试边界")

    projection.harness_turn_id = normalized_turn_id
    execution.owner_id = normalized_owner_id
    execution.lease_token = new_id()
    execution.lease_expires_at = now + timedelta(seconds=lease_seconds)
    execution.last_heartbeat_at = now
    execution.released_at = None
    execution.attempt += 1
    execution.fencing_token += 1
    execution.phase = AgentExecutionPhase.CLAIMED
    try:
        session.commit()
    except IntegrityError as exc:
        session.rollback()
        raise ConflictError("Agent Turn execution 与其他 claim 冲突") from exc
    session.refresh(execution)
    return _lease_from_execution(execution, normalized_owner_id)


def append_agent_turn_checkpoint(
    session: Session,
    *,
    conversation_id: str,
    execution_id: str,
    owner_id: str,
    lease_token: str,
    sequence: int,
    kind: AgentCheckpointKind,
    payload: dict[str, Any],
) -> AgentCheckpointReceipt:
    if not 1 <= sequence <= MAX_AGENT_CHECKPOINT_SEQUENCE:
        raise BusinessValidationError("Agent checkpoint sequence 无效")
    try:
        encoded = json.dumps(
            payload,
            ensure_ascii=False,
            sort_keys=True,
            separators=(",", ":"),
            allow_nan=False,
        ).encode()
    except (TypeError, ValueError) as exc:
        raise BusinessValidationError("Agent checkpoint payload 不是有效 JSON") from exc
    if len(encoded) > MAX_AGENT_CHECKPOINT_PAYLOAD_BYTES:
        raise BusinessValidationError("Agent checkpoint payload 超过大小限制")

    execution = session.scalar(
        select(AgentTurnExecution)
        .join(AgentTurnProjection)
        .where(
            AgentTurnExecution.id == execution_id,
            AgentTurnProjection.conversation_id == conversation_id,
        )
        .with_for_update()
    )
    if execution is None:
        raise NotFoundError("Agent execution 不存在")
    _require_active_lease(execution, owner_id=owner_id, lease_token=lease_token)
    existing = session.scalar(
        select(AgentTurnCheckpoint).where(
            AgentTurnCheckpoint.execution_id == execution.id,
            AgentTurnCheckpoint.attempt == execution.attempt,
            AgentTurnCheckpoint.sequence == sequence,
        )
    )
    if existing is not None:
        if (
            existing.kind != kind
            or existing.fencing_token != execution.fencing_token
            or existing.payload_json != payload
        ):
            raise ConflictError("Agent checkpoint sequence 已绑定不同内容")
        return _checkpoint_receipt(existing)
    if sequence != execution.last_checkpoint_sequence + 1:
        raise ConflictError("Agent checkpoint sequence 必须连续提交")

    now = now_utc()
    checkpoint = AgentTurnCheckpoint(
        turn_projection_id=execution.turn_projection_id,
        execution_id=execution.id,
        attempt=execution.attempt,
        fencing_token=execution.fencing_token,
        sequence=sequence,
        kind=kind,
        payload_json=dict(payload),
        created_at=now,
    )
    session.add(checkpoint)
    execution.last_checkpoint_sequence = sequence
    execution.last_checkpoint_at = now
    try:
        session.commit()
    except IntegrityError as exc:
        session.rollback()
        raise ConflictError("Agent checkpoint 与其他 writer 冲突") from exc
    session.refresh(checkpoint)
    return _checkpoint_receipt(checkpoint)


def heartbeat_agent_turn_execution(
    session: Session,
    *,
    conversation_id: str,
    execution_id: str,
    owner_id: str,
    lease_token: str,
    phase: AgentExecutionPhase,
    lease_seconds: int = DEFAULT_AGENT_EXECUTION_LEASE_SECONDS,
) -> AgentExecutionLease:
    normalized_owner_id = owner_id.strip()
    normalized_token = lease_token.strip()
    if not normalized_owner_id or not normalized_token:
        raise BusinessValidationError("Agent execution lease identity 无效")
    if not 5 <= lease_seconds <= 600:
        raise BusinessValidationError("Agent execution lease 时长无效")
    execution = session.scalar(
        select(AgentTurnExecution)
        .join(AgentTurnProjection)
        .where(
            AgentTurnExecution.id == execution_id,
            AgentTurnProjection.conversation_id == conversation_id,
        )
        .with_for_update()
    )
    if execution is None:
        raise NotFoundError("Agent execution 不存在")
    now = now_utc()
    _require_active_lease(execution, owner_id=normalized_owner_id, lease_token=normalized_token, now=now)
    execution.phase = phase
    execution.last_heartbeat_at = now
    execution.lease_expires_at = now + timedelta(seconds=lease_seconds)
    session.commit()
    session.refresh(execution)
    return _lease_from_execution(execution, normalized_owner_id)


def release_agent_turn_execution(
    session: Session,
    *,
    conversation_id: str,
    execution_id: str,
    owner_id: str,
    lease_token: str,
    phase: AgentExecutionPhase = AgentExecutionPhase.TERMINAL,
) -> bool:
    execution = session.scalar(
        select(AgentTurnExecution)
        .join(AgentTurnProjection)
        .where(
            AgentTurnExecution.id == execution_id,
            AgentTurnProjection.conversation_id == conversation_id,
        )
        .with_for_update()
    )
    if execution is None:
        return False
    if execution.owner_id != owner_id or execution.lease_token != lease_token:
        session.rollback()
        return False
    now = now_utc()
    execution.phase = phase
    execution.released_at = now
    execution.last_heartbeat_at = now
    if phase == AgentExecutionPhase.TERMINAL:
        execution.owner_id = None
        execution.lease_token = None
        execution.lease_expires_at = None
    else:
        execution.lease_expires_at = now
    session.commit()
    return True


def recover_expired_agent_turn_executions(
    session: Session,
    *,
    now: datetime | None = None,
) -> AgentExecutionRecoverySummary:
    resolved_now = now or now_utc()
    executions = list(
        session.scalars(
            select(AgentTurnExecution)
            .join(AgentTurnProjection)
            .options(selectinload(AgentTurnExecution.turn_projection).selectinload(AgentTurnProjection.conversation))
            .where(
                AgentTurnExecution.owner_id.is_not(None),
                AgentTurnExecution.lease_expires_at.is_not(None),
                AgentTurnExecution.lease_expires_at <= resolved_now,
            )
            .with_for_update()
        ).all()
    )
    requeued = 0
    unknown = 0
    for execution in executions:
        projection = execution.turn_projection
        # A recovered worker must not be able to publish a terminal snapshot
        # with the fencing token from the expired lease.
        execution.fencing_token += 1
        if projection.status not in _ACTIVE_TURN_STATUSES:
            _clear_expired_lease(execution, resolved_now, terminal=True)
            continue
        latest_checkpoint = session.scalar(
            select(AgentTurnCheckpoint)
            .where(AgentTurnCheckpoint.execution_id == execution.id)
            .order_by(AgentTurnCheckpoint.sequence.desc())
            .limit(1)
        )
        if (
            projection.status == AgentTurnStatus.QUEUED
            and execution.phase == AgentExecutionPhase.CLAIMED
            and execution.last_checkpoint_sequence == 0
            and latest_checkpoint is None
        ):
            _clear_expired_lease(execution, resolved_now)
            requeued += 1
            continue
        tool_steps = _unknown_running_tool_steps(projection.tool_steps_json)
        project_agent_turn_state(
            session,
            product_id=projection.conversation.product_id,
            conversation_id=projection.conversation_id,
            projection_id=projection.id,
            harness_turn_id=projection.harness_turn_id or execution.harness_turn_id,
            status=AgentTurnStatus.UNKNOWN,
            output_text=projection.output_text,
            error_text=RESTART_UNKNOWN_ERROR,
            question_json=None,
            tool_steps_json=tool_steps,
            finished_at=resolved_now,
            commit=False,
        )
        projection.resume_required = False
        _record_recovery_terminal_checkpoint(
            session,
            execution=execution,
            latest_checkpoint=latest_checkpoint,
            created_at=resolved_now,
        )
        _clear_expired_lease(execution, resolved_now, terminal=True)
        unknown += 1
    if executions:
        session.commit()
    return AgentExecutionRecoverySummary(requeued=requeued, unknown=unknown)


def _require_active_lease(
    execution: AgentTurnExecution,
    *,
    owner_id: str,
    lease_token: str,
    now: datetime | None = None,
) -> None:
    resolved_now = now or now_utc()
    expires = execution.lease_expires_at
    if expires is not None and expires.tzinfo is None:
        expires = expires.replace(tzinfo=resolved_now.tzinfo)
    if (
        execution.owner_id != owner_id
        or execution.lease_token != lease_token
        or expires is None
        or expires <= resolved_now
    ):
        raise ConflictError("Agent execution lease 已失效")


def _checkpoint_receipt(checkpoint: AgentTurnCheckpoint) -> AgentCheckpointReceipt:
    return AgentCheckpointReceipt(
        id=checkpoint.id,
        projection_id=checkpoint.turn_projection_id,
        execution_id=checkpoint.execution_id,
        attempt=checkpoint.attempt,
        fencing_token=checkpoint.fencing_token,
        sequence=checkpoint.sequence,
        kind=checkpoint.kind,
        created_at=checkpoint.created_at,
    )


def _lease_from_execution(execution: AgentTurnExecution, owner_id: str) -> AgentExecutionLease:
    if execution.lease_token is None or execution.lease_expires_at is None or execution.owner_id != owner_id:
        raise ConflictError("Agent execution lease 不可用")
    return AgentExecutionLease(
        execution_id=execution.id,
        projection_id=execution.turn_projection_id,
        harness_turn_id=execution.harness_turn_id,
        owner_id=owner_id,
        lease_token=execution.lease_token,
        attempt=execution.attempt,
        fencing_token=execution.fencing_token,
        phase=execution.phase,
        lease_expires_at=execution.lease_expires_at,
    )


def _clear_expired_lease(
    execution: AgentTurnExecution,
    now: datetime,
    *,
    terminal: bool = False,
) -> None:
    execution.owner_id = None
    execution.lease_token = None
    execution.lease_expires_at = None
    execution.released_at = now
    execution.last_heartbeat_at = now
    if terminal:
        execution.phase = AgentExecutionPhase.TERMINAL


def _unknown_running_tool_steps(value: list[dict[str, Any]]) -> list[dict[str, Any]]:
    result: list[dict[str, Any]] = []
    for step in value or []:
        copied = dict(step)
        if copied.get("status") == "running":
            copied["status"] = "unknown"
        result.append(copied)
    return result


def _record_recovery_terminal_checkpoint(
    session: Session,
    *,
    execution: AgentTurnExecution,
    latest_checkpoint: AgentTurnCheckpoint | None,
    created_at: datetime,
) -> None:
    if latest_checkpoint is not None and latest_checkpoint.kind == AgentCheckpointKind.TERMINAL:
        return
    sequence = execution.last_checkpoint_sequence + 1
    if sequence > MAX_AGENT_CHECKPOINT_SEQUENCE:
        return
    checkpoint = AgentTurnCheckpoint(
        turn_projection_id=execution.turn_projection_id,
        execution_id=execution.id,
        attempt=execution.attempt,
        fencing_token=execution.fencing_token,
        sequence=sequence,
        kind=AgentCheckpointKind.TERMINAL,
        payload_json={
            "status": AgentTurnStatus.UNKNOWN.value,
            "error": RESTART_UNKNOWN_ERROR,
            "recovery": "expired_execution_lease",
            "phase": execution.phase.value,
        },
        created_at=created_at,
    )
    session.add(checkpoint)
    execution.last_checkpoint_sequence = sequence
    execution.last_checkpoint_at = created_at


__all__ = [
    "AgentCheckpointReceipt",
    "AgentExecutionLease",
    "AgentExecutionRecoverySummary",
    "DEFAULT_AGENT_EXECUTION_LEASE_SECONDS",
    "RESTART_UNKNOWN_ERROR",
    "claim_agent_turn_execution",
    "heartbeat_agent_turn_execution",
    "recover_expired_agent_turn_executions",
    "release_agent_turn_execution",
]
