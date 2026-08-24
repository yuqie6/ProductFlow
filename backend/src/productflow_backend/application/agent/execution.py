"""Agent Turn 可恢复执行。PostgreSQL 持有 lease、event 与 checkpoint；Node.js session/event files 不是业务权威。"""

from __future__ import annotations

import json
from dataclasses import dataclass
from datetime import datetime, timedelta
from typing import Any

from sqlalchemy import func, select
from sqlalchemy.exc import IntegrityError
from sqlalchemy.orm import Session, selectinload

from productflow_backend.application.agent.conversations import project_agent_turn_state
from productflow_backend.application.time import now_utc
from productflow_backend.domain.enums import AgentCheckpointKind, AgentExecutionPhase, AgentTurnStatus
from productflow_backend.domain.errors import BusinessValidationError, ConflictError, NotFoundError
from productflow_backend.infrastructure.db.models import (
    AgentTurnCheckpoint,
    AgentTurnEvent,
    AgentTurnExecution,
    AgentTurnProjection,
    new_id,
)

DEFAULT_AGENT_EXECUTION_LEASE_SECONDS = 60
MAX_AGENT_CHECKPOINT_PAYLOAD_BYTES = 64 * 1024
MAX_AGENT_CHECKPOINT_SEQUENCE = 10_000
MAX_AGENT_EVENT_PAYLOAD_BYTES = 128 * 1024
MAX_AGENT_EVENT_SEQUENCE = 100_000
RESTART_UNKNOWN_ERROR = "Agent execution lease expired before this Turn reached a provable terminal state"
_AGENT_EFFECT_RESULTS = {"applied", "failed", "unknown"}

_AGENT_EVENT_KINDS = {
    "turn.queued",
    "turn.started",
    "text.delta",
    "tool.step",
    "question.required",
    "question.answered",
    "turn.resume_requested",
    "turn.cancel_requested",
    "turn.requires_input",
    "artifact.proposed",
    "turn.awaiting_confirmation",
    "turn.succeeded",
    "turn.failed",
    "turn.canceled",
    "turn.unknown",
}

_TERMINAL_AGENT_EVENT_KINDS = {
    "turn.succeeded",
    "turn.failed",
    "turn.canceled",
    "turn.unknown",
    "turn.awaiting_confirmation",
}

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
    requires_input: int = 0
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


@dataclass(frozen=True, slots=True)
class AgentTurnEventReceipt:
    id: str
    projection_id: str
    execution_id: str
    sequence: int
    schema_version: int
    kind: str
    created_at: datetime


def append_agent_turn_event(
    session: Session,
    *,
    conversation_id: str,
    execution_id: str,
    owner_id: str,
    lease_token: str,
    sequence: int,
    schema_version: int,
    run_id: str,
    turn_id: str,
    kind: str,
    payload: dict[str, Any],
    created_at: datetime,
) -> AgentTurnEventReceipt:
    """按 sequence 追加投影事件。相同 sequence 必须内容一致。本函数 commit。"""
    normalized_run_id = run_id.strip()
    normalized_turn_id = turn_id.strip()
    normalized_kind = kind.strip()
    if schema_version != 1:
        raise BusinessValidationError("Agent event schema version 不受支持")
    if not 1 <= sequence <= MAX_AGENT_EVENT_SEQUENCE:
        raise BusinessValidationError("Agent event sequence 无效")
    if not normalized_run_id or len(normalized_run_id) > 120:
        raise BusinessValidationError("Agent event run ID 无效")
    if not normalized_turn_id or len(normalized_turn_id) > 120:
        raise BusinessValidationError("Agent event turn ID 无效")
    if normalized_kind not in _AGENT_EVENT_KINDS:
        raise BusinessValidationError("Agent event kind 不受支持")
    try:
        encoded = json.dumps(
            payload,
            ensure_ascii=False,
            sort_keys=True,
            separators=(",", ":"),
            allow_nan=False,
        ).encode()
    except (TypeError, ValueError) as exc:
        raise BusinessValidationError("Agent event payload 不是有效 JSON") from exc
    if len(encoded) > MAX_AGENT_EVENT_PAYLOAD_BYTES:
        raise BusinessValidationError("Agent event payload 超过大小限制")

    projection = session.scalar(
        select(AgentTurnProjection)
        .join(
            AgentTurnExecution,
            AgentTurnExecution.turn_projection_id == AgentTurnProjection.id,
        )
        .where(
            AgentTurnProjection.conversation_id == conversation_id,
            AgentTurnExecution.id == execution_id,
        )
        .with_for_update()
    )
    if projection is None:
        raise NotFoundError("Agent Turn projection 不存在")
    if projection.harness_turn_id != normalized_turn_id:
        raise ConflictError("Agent event turn ID 与 projection 不匹配")
    if projection.conversation.harness_run_id != normalized_run_id:
        raise ConflictError("Agent event run ID 与 conversation 不匹配")

    existing = session.scalar(
        select(AgentTurnEvent)
        .where(
            AgentTurnEvent.turn_projection_id == projection.id,
            AgentTurnEvent.sequence == sequence,
        )
        .with_for_update()
    )
    if existing is not None:
        if (
            existing.schema_version != schema_version
            or existing.run_id != normalized_run_id
            or existing.turn_id != normalized_turn_id
            or existing.kind != normalized_kind
            or existing.payload_json != payload
        ):
            raise ConflictError("Agent event sequence 已绑定不同内容")
        # 相同 sequence 的幂等回放也由本函数提交，避免调用方误以为未落库。
        session.commit()
        return _event_receipt(existing)

    execution = session.scalar(
        select(AgentTurnExecution)
        .where(
            AgentTurnExecution.id == execution_id,
            AgentTurnExecution.turn_projection_id == projection.id,
        )
        .with_for_update()
    )
    if execution is None:
        raise NotFoundError("Agent execution 不存在")
    if execution.harness_turn_id != normalized_turn_id:
        raise ConflictError("Agent execution turn ID 与 event 不匹配")
    _require_active_lease(execution, owner_id=owner_id, lease_token=lease_token)
    last_sequence = session.scalar(
        select(func.max(AgentTurnEvent.sequence)).where(AgentTurnEvent.turn_projection_id == projection.id)
    ) or 0
    if sequence != last_sequence + 1:
        raise ConflictError("Agent event sequence 必须连续提交")

    normalized_created_at = created_at if created_at.tzinfo is not None else created_at.replace(tzinfo=now_utc().tzinfo)
    event = AgentTurnEvent(
        turn_projection_id=projection.id,
        execution_id=execution.id,
        run_id=normalized_run_id,
        turn_id=normalized_turn_id,
        schema_version=schema_version,
        sequence=sequence,
        attempt=execution.attempt,
        fencing_token=execution.fencing_token,
        kind=normalized_kind,
        payload_json=dict(payload),
        created_at=normalized_created_at,
    )
    session.add(event)
    try:
        session.commit()
    except IntegrityError as exc:
        session.rollback()
        raise ConflictError("Agent event 与其他 writer 冲突") from exc
    session.refresh(event)
    return _event_receipt(event)


def list_agent_turn_events(
    session: Session,
    *,
    projection_id: str,
    after: int,
    limit: int = 100,
) -> list[AgentTurnEvent]:
    if after < 0 or after > MAX_AGENT_EVENT_SEQUENCE:
        raise BusinessValidationError("Agent event cursor 无效")
    if not 1 <= limit <= 500:
        raise BusinessValidationError("Agent event limit 无效")
    return list(
        session.scalars(
            select(AgentTurnEvent)
            .where(
                AgentTurnEvent.turn_projection_id == projection_id,
                AgentTurnEvent.sequence > after,
            )
            .order_by(AgentTurnEvent.sequence)
            .limit(limit)
        ).all()
    )


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
    """多实例 claim：同一 owner 未过期则续用；过期且已离开 CLAIMED 必须先对账，不能直接重试。本函数 commit。"""
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
        # 心跳窗口内重复 claim 必须回放同一 fencing，不能另开 attempt。
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
        # 已进入工具/模型阶段的过期 lease 可能有未证明副作用，禁止直接抢走。
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
    """按 attempt 连续追加 checkpoint。TOOL_EFFECT_RESULT 只允许 applied/failed/unknown。本函数 commit。"""
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
    if kind == AgentCheckpointKind.TOOL_EFFECT_RESULT:
        result = payload.get("result")
        if not isinstance(result, str) or result not in _AGENT_EFFECT_RESULTS:
            raise BusinessValidationError("Agent effect result 必须是 applied、failed 或 unknown")

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
    """持有人续租并推进 phase。过期或 fencing 失效则冲突。本函数 commit。"""
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
    """持有人释放。TERMINAL 清空 owner；其它 phase 只把 lease 立刻过期，交给恢复扫描。本函数 commit。"""
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
        # 非终态释放仍保留 phase，只让 lease 立刻过期，恢复扫描才能接手。
        execution.lease_expires_at = now
    session.commit()
    return True


def recover_expired_agent_turn_executions(
    session: Session,
    *,
    now: datetime | None = None,
) -> AgentExecutionRecoverySummary:
    """过期 lease 恢复：无 checkpoint 的 CLAIMED queued 可重入队；WAITING_INPUT 可还原问题；无法证明终态则标 unknown。"""
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
    requires_input = 0
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
            # 尚未产生副作用证据，重入队是安全的。
            _clear_expired_lease(execution, resolved_now)
            requeued += 1
            continue
        question = _recoverable_waiting_question(
            session=session,
            projection=projection,
            execution=execution,
            latest_checkpoint=latest_checkpoint,
        )
        if question is not None:
            project_agent_turn_state(
                session,
                product_id=projection.conversation.product_id,
                conversation_id=projection.conversation_id,
                projection_id=projection.id,
                harness_turn_id=projection.harness_turn_id or execution.harness_turn_id,
                status=AgentTurnStatus.REQUIRES_INPUT,
                output_text=projection.output_text,
                error_text=None,
                question_json=question,
                tool_steps_json=projection.tool_steps_json,
                finished_at=None,
                commit=False,
            )
            projection.resume_required = False
            _record_recovery_question_events(
                session,
                execution=execution,
                projection=projection,
                question=question,
                created_at=resolved_now,
            )
            _clear_expired_lease(execution, resolved_now, terminal=True)
            requires_input += 1
            continue
        # 过期时无法证明模型/工具结果，不能标 failed。
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
        _record_recovery_terminal_event(
            session,
            execution=execution,
            projection=projection,
            created_at=resolved_now,
        )
        _clear_expired_lease(execution, resolved_now, terminal=True)
        unknown += 1
    if executions:
        session.commit()
    return AgentExecutionRecoverySummary(
        requeued=requeued,
        requires_input=requires_input,
        unknown=unknown,
    )


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


def _event_receipt(event: AgentTurnEvent) -> AgentTurnEventReceipt:
    if event.execution_id is None:
        raise ConflictError("Agent event 缺少 execution 绑定")
    return AgentTurnEventReceipt(
        id=event.id,
        projection_id=event.turn_projection_id,
        execution_id=event.execution_id,
        sequence=event.sequence,
        schema_version=event.schema_version,
        kind=event.kind,
        created_at=event.created_at,
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
    """把仍显示 running 的步骤改成 unknown：lease 过期不能证明该 effect 成败。"""
    result: list[dict[str, Any]] = []
    for step in value or []:
        copied = dict(step)
        if copied.get("status") == "running":
            copied["status"] = "unknown"
        result.append(copied)
    return result


def _recoverable_waiting_question(
    *,
    session: Session,
    projection: AgentTurnProjection,
    execution: AgentTurnExecution,
    latest_checkpoint: AgentTurnCheckpoint | None,
) -> dict[str, Any] | None:
    # 只有 WAITING_INPUT + QUESTION_REQUIRED checkpoint、且无终态 event 才能还原为 requires_input。
    if execution.phase != AgentExecutionPhase.WAITING_INPUT:
        return None
    if latest_checkpoint is None or latest_checkpoint.kind != AgentCheckpointKind.QUESTION_REQUIRED:
        return None
    if session.scalar(
        select(AgentTurnEvent.id)
        .where(
            AgentTurnEvent.turn_projection_id == projection.id,
            AgentTurnEvent.kind.in_(_TERMINAL_AGENT_EVENT_KINDS),
        )
        .limit(1)
    ) is not None:
        return None
    payload_question = latest_checkpoint.payload_json.get("question")
    if not isinstance(payload_question, dict):
        return None
    question_id = payload_question.get("id")
    question_text = payload_question.get("question")
    if (
        not isinstance(question_id, str)
        or not question_id.strip()
        or not isinstance(question_text, str)
        or not question_text.strip()
    ):
        return None
    projection_question = projection.question_json
    if projection_question is not None:
        if not isinstance(projection_question, dict) or projection_question.get("id") != question_id:
            return None
        return dict(projection_question)
    return dict(payload_question)


def _record_recovery_question_events(
    session: Session,
    *,
    execution: AgentTurnExecution,
    projection: AgentTurnProjection,
    question: dict[str, Any],
    created_at: datetime,
) -> None:
    if projection.harness_turn_id is None:
        return
    existing_kinds = set(
        session.scalars(
            select(AgentTurnEvent.kind).where(
                AgentTurnEvent.turn_projection_id == projection.id,
                AgentTurnEvent.kind.in_({"turn.requires_input", "question.required"}),
            )
        ).all()
    )
    last_sequence = session.scalar(
        select(func.max(AgentTurnEvent.sequence)).where(
            AgentTurnEvent.turn_projection_id == projection.id,
        )
    ) or 0
    events = (
        (
            "turn.requires_input",
            {"status": AgentTurnStatus.REQUIRES_INPUT.value, "question": question},
        ),
        ("question.required", question),
    )
    for kind, payload in events:
        if kind in existing_kinds:
            continue
        last_sequence += 1
        if last_sequence > MAX_AGENT_EVENT_SEQUENCE:
            return
        session.add(
            AgentTurnEvent(
                turn_projection_id=projection.id,
                execution_id=execution.id,
                run_id=projection.conversation.harness_run_id,
                turn_id=projection.harness_turn_id,
                schema_version=1,
                sequence=last_sequence,
                attempt=execution.attempt,
                fencing_token=execution.fencing_token,
                kind=kind,
                payload_json=payload,
                created_at=created_at,
            )
        )


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


def _record_recovery_terminal_event(
    session: Session,
    *,
    execution: AgentTurnExecution,
    projection: AgentTurnProjection,
    created_at: datetime,
) -> None:
    last_sequence = session.scalar(
        select(func.max(AgentTurnEvent.sequence)).where(
            AgentTurnEvent.turn_projection_id == projection.id,
        )
    ) or 0
    sequence = last_sequence + 1
    if sequence > MAX_AGENT_EVENT_SEQUENCE or projection.harness_turn_id is None:
        return
    session.add(
        AgentTurnEvent(
            turn_projection_id=projection.id,
            execution_id=execution.id,
            run_id=projection.conversation.harness_run_id,
            turn_id=projection.harness_turn_id,
            schema_version=1,
            sequence=sequence,
            attempt=execution.attempt,
            fencing_token=execution.fencing_token,
            kind="turn.unknown",
            payload_json={
                "status": AgentTurnStatus.UNKNOWN.value,
                "error": RESTART_UNKNOWN_ERROR,
            },
            created_at=created_at,
        )
    )


__all__ = [
    "AgentCheckpointReceipt",
    "AgentExecutionLease",
    "AgentExecutionRecoverySummary",
    "AgentTurnEventReceipt",
    "DEFAULT_AGENT_EXECUTION_LEASE_SECONDS",
    "MAX_AGENT_EVENT_PAYLOAD_BYTES",
    "MAX_AGENT_EVENT_SEQUENCE",
    "RESTART_UNKNOWN_ERROR",
    "append_agent_turn_event",
    "claim_agent_turn_execution",
    "heartbeat_agent_turn_execution",
    "list_agent_turn_events",
    "recover_expired_agent_turn_executions",
    "release_agent_turn_execution",
]
