from __future__ import annotations

import logging
from collections.abc import Callable
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta
from typing import Any

from sqlalchemy import func, select, update
from sqlalchemy.orm import Session

from productflow_backend.application.time import now_utc
from productflow_backend.domain.enums import AsyncDispatchStatus
from productflow_backend.domain.errors import ConflictError
from productflow_backend.infrastructure.db.models import AsyncDispatch, new_id
from productflow_backend.infrastructure.db.session import get_session_factory

logger = logging.getLogger(__name__)

DEFAULT_DISPATCH_LEASE_SECONDS = 60
DEFAULT_DISPATCH_MAX_ATTEMPTS = 10
DEFAULT_DISPATCH_BACKOFF_SECONDS = 2
DEFAULT_SENT_RECONCILE_AFTER = timedelta(minutes=5)
DEFAULT_DISPATCH_CONSUMER_LEASE_SECONDS = 10 * 60


@dataclass(slots=True)
class AsyncDispatchSummary:
    pending: int = 0
    sent: int = 0
    reconciled: int = 0
    dead: int = 0


def delivery_key_for_actor(actor_name: str, aggregate_id: str) -> str:
    """Return the stable idempotency key shared by submission and recovery paths."""
    return f"{actor_name}:{aggregate_id}"


def _validate_existing_dispatch_identity(
    existing: AsyncDispatch,
    *,
    actor_name: str,
    aggregate_id: str,
    payload: dict[str, Any] | None,
) -> None:
    if existing.actor_name != actor_name or existing.aggregate_id != aggregate_id:
        raise ConflictError("同一 delivery key 不能复用到不同的异步目标")
    if payload is not None and existing.payload_json not in (None, payload):
        raise ConflictError("同一 delivery key 不能复用到不同的异步 payload")


def stage_async_dispatch(
    session: Session,
    *,
    delivery_key: str,
    actor_name: str,
    aggregate_id: str,
    payload: dict[str, Any] | None = None,
    available_at: datetime | None = None,
) -> AsyncDispatch:
    """Create or return the durable dispatch row without committing.

    Caller owns the transaction; this helper only flushes so the dispatch id is available.
    """
    existing = session.scalar(
        select(AsyncDispatch).where(AsyncDispatch.delivery_key == delivery_key)
    )
    if existing is not None:
        _validate_existing_dispatch_identity(
            existing,
            actor_name=actor_name,
            aggregate_id=aggregate_id,
            payload=payload,
        )
        if payload is not None and existing.payload_json is None:
            existing.payload_json = payload
        if existing.status == AsyncDispatchStatus.CONSUMED:
            now = now_utc()
            existing.status = AsyncDispatchStatus.PENDING
            existing.available_at = available_at or now
            existing.lease_token = None
            existing.lease_expires_at = None
            existing.attempts = 0
            existing.last_error = None
            existing.sent_at = None
            existing.consumed_at = None
            existing.updated_at = now
            session.flush()
        return existing
    dispatch = AsyncDispatch(
        delivery_key=delivery_key,
        actor_name=actor_name,
        aggregate_id=aggregate_id,
        payload_json=payload,
        status=AsyncDispatchStatus.PENDING,
        available_at=available_at or now_utc(),
        attempts=0,
    )
    session.add(dispatch)
    session.flush()
    return dispatch


def requeue_async_dispatch(
    session: Session,
    *,
    delivery_key: str,
    actor_name: str,
    aggregate_id: str,
    payload: dict[str, Any] | None = None,
    available_at: datetime | None = None,
    allow_active_lease: bool = False,
) -> AsyncDispatch:
    """Create or reset a dispatch row to pending for recovery/retry paths."""
    existing = session.scalar(
        select(AsyncDispatch).where(AsyncDispatch.delivery_key == delivery_key)
    )
    if existing is None:
        return stage_async_dispatch(
            session,
            delivery_key=delivery_key,
            actor_name=actor_name,
            aggregate_id=aggregate_id,
            payload=payload,
            available_at=available_at,
        )
    _validate_existing_dispatch_identity(
        existing,
        actor_name=actor_name,
        aggregate_id=aggregate_id,
        payload=payload,
    )
    if payload is not None and existing.payload_json is None:
        existing.payload_json = payload
    now = now_utc()
    lease_expires_at = existing.lease_expires_at
    if lease_expires_at is not None and lease_expires_at.tzinfo is None:
        lease_expires_at = lease_expires_at.replace(tzinfo=UTC)
    if (
        existing.status == AsyncDispatchStatus.SENT
        and existing.lease_token is not None
        and (lease_expires_at is None or lease_expires_at > now)
        and not allow_active_lease
    ):
        return existing
    if existing.status == AsyncDispatchStatus.PENDING:
        return existing
    existing.status = AsyncDispatchStatus.PENDING
    existing.available_at = available_at or now
    existing.lease_token = None
    existing.lease_expires_at = None
    existing.attempts = 0
    existing.last_error = None
    existing.sent_at = None
    existing.consumed_at = None
    existing.updated_at = now
    session.flush()
    return existing


def stage_async_dispatch_for_actor(
    session: Session,
    actor_name: str,
    aggregate_id: str,
    *,
    delay_ms: int | None = None,
) -> AsyncDispatch:
    available_at = now_utc() + timedelta(milliseconds=delay_ms) if delay_ms is not None else None
    return stage_async_dispatch(
        session,
        delivery_key=delivery_key_for_actor(actor_name, aggregate_id),
        actor_name=actor_name,
        aggregate_id=aggregate_id,
        available_at=available_at,
    )


def enqueue_async_dispatch_for_actor(
    actor_name: str,
    aggregate_id: str,
    *,
    delay_ms: int | None = None,
    allow_active_lease: bool = False,
) -> None:
    """Open a session and create/requeue a dispatch row for recovery callbacks."""
    session = get_session_factory()()
    try:
        available_at = (
            now_utc() + timedelta(milliseconds=delay_ms) if delay_ms is not None else None
        )
        requeue_async_dispatch(
            session,
            delivery_key=delivery_key_for_actor(actor_name, aggregate_id),
            actor_name=actor_name,
            aggregate_id=aggregate_id,
            available_at=available_at,
            allow_active_lease=allow_active_lease,
        )
        session.commit()
    finally:
        session.close()


def _claim_pending_dispatches(
    session: Session,
    *,
    now: datetime,
    limit: int,
    lease_seconds: int,
) -> list[AsyncDispatch]:
    statement = (
        select(AsyncDispatch)
        .where(
            AsyncDispatch.status == AsyncDispatchStatus.PENDING,
            AsyncDispatch.available_at <= now,
            (AsyncDispatch.lease_expires_at.is_(None)) | (AsyncDispatch.lease_expires_at <= now),
        )
        .order_by(AsyncDispatch.available_at.asc(), AsyncDispatch.id.asc())
        .limit(limit)
    )
    if session.get_bind().dialect.name == "postgresql":
        statement = statement.with_for_update(skip_locked=True)
    else:
        statement = statement.with_for_update()
    dispatches = list(session.scalars(statement).all())
    lease_expires_at = now + timedelta(seconds=lease_seconds)
    for dispatch in dispatches:
        dispatch.lease_token = new_id()
        dispatch.lease_expires_at = lease_expires_at
        dispatch.attempts += 1
        dispatch.updated_at = now
    session.flush()
    return dispatches


def _send_claimed_dispatch(
    session: Session,
    *,
    dispatch: AsyncDispatch,
    enqueue: Callable[[str, str], None],
    now: datetime,
) -> bool:
    # Publish SENT before touching the broker so a fast worker can claim the message.
    # If the process dies before enqueue, stale reconciliation returns this row to pending.
    dispatch.status = AsyncDispatchStatus.SENT
    dispatch.sent_at = now
    dispatch.lease_token = None
    dispatch.lease_expires_at = None
    dispatch.last_error = None
    dispatch.updated_at = now
    session.commit()

    try:
        enqueue(dispatch.id, dispatch.aggregate_id)
    except Exception as exc:  # noqa: BLE001
        logger.exception("异步投递入队失败: dispatch_id=%s aggregate_id=%s", dispatch.id, dispatch.aggregate_id)
        # The broker may have accepted the message before the client observed an
        # error. Keep SENT so stale reconciliation, rather than this exception,
        # decides when an ambiguous publish can be retried.
        session.execute(
            update(AsyncDispatch)
            .where(
                AsyncDispatch.id == dispatch.id,
                AsyncDispatch.status == AsyncDispatchStatus.SENT,
                AsyncDispatch.lease_token.is_(None),
            )
            .values(last_error=str(exc)[:1000], updated_at=now)
            .execution_options(synchronize_session=False)
        )
        session.commit()
        return False
    return True


def _reconcile_expired_leases(session: Session, *, now: datetime) -> int:
    result = session.execute(
        update(AsyncDispatch)
        .where(
            AsyncDispatch.status == AsyncDispatchStatus.PENDING,
            AsyncDispatch.lease_expires_at.is_not(None),
            AsyncDispatch.lease_expires_at <= now,
        )
        .values(
            lease_token=None,
            lease_expires_at=None,
            available_at=now,
            updated_at=now,
        )
        .execution_options(synchronize_session=False)
    )
    return result.rowcount or 0


def _reconcile_stale_sent(
    session: Session,
    *,
    now: datetime,
    sent_after: timedelta = DEFAULT_SENT_RECONCILE_AFTER,
    max_attempts: int = DEFAULT_DISPATCH_MAX_ATTEMPTS,
) -> int:
    cutoff = now - sent_after
    rows = list(
        session.scalars(
            select(AsyncDispatch)
            .where(
                AsyncDispatch.status == AsyncDispatchStatus.SENT,
                AsyncDispatch.sent_at.is_not(None),
                AsyncDispatch.sent_at <= cutoff,
                (AsyncDispatch.lease_token.is_(None))
                | (AsyncDispatch.lease_expires_at.is_not(None) & (AsyncDispatch.lease_expires_at <= now)),
            )
            .with_for_update()
        ).all()
    )
    reconciled = 0
    for dispatch in rows:
        dispatch.lease_token = None
        dispatch.lease_expires_at = None
        dispatch.sent_at = None
        dispatch.updated_at = now
        if dispatch.attempts >= max_attempts:
            dispatch.status = AsyncDispatchStatus.DEAD
        else:
            dispatch.status = AsyncDispatchStatus.PENDING
            dispatch.available_at = now
        reconciled += 1
    return reconciled


def claim_async_dispatch_for_consumption(
    session: Session,
    *,
    dispatch_id: str,
    aggregate_id: str,
    now: datetime | None = None,
    lease_seconds: int = DEFAULT_DISPATCH_CONSUMER_LEASE_SECONDS,
) -> str | None:
    """Atomically claim a sent dispatch for one worker before running its target."""
    resolved_now = now or now_utc()
    token = new_id()
    result = session.execute(
        update(AsyncDispatch)
        .where(
            AsyncDispatch.id == dispatch_id,
            AsyncDispatch.aggregate_id == aggregate_id,
            AsyncDispatch.status == AsyncDispatchStatus.SENT,
            AsyncDispatch.lease_token.is_(None),
        )
        .values(
            lease_token=token,
            lease_expires_at=resolved_now + timedelta(seconds=lease_seconds),
            updated_at=resolved_now,
        )
        .execution_options(synchronize_session=False)
    )
    if result.rowcount != 1:
        session.rollback()
        return None
    session.commit()
    return token


def run_async_dispatcher_once(
    *,
    enqueue: Callable[[str, str], None],
    limit: int = 100,
    now: datetime | None = None,
    lease_seconds: int = DEFAULT_DISPATCH_LEASE_SECONDS,
    max_attempts: int = DEFAULT_DISPATCH_MAX_ATTEMPTS,
    backoff_seconds: int = DEFAULT_DISPATCH_BACKOFF_SECONDS,
) -> AsyncDispatchSummary:
    resolved_now = now or now_utc()
    session = get_session_factory()()
    summary = AsyncDispatchSummary()
    try:
        summary.reconciled = _reconcile_expired_leases(session, now=resolved_now)
        summary.reconciled += _reconcile_stale_sent(
            session,
            now=resolved_now,
            max_attempts=max_attempts,
        )
        session.commit()

        dispatches = _claim_pending_dispatches(
            session,
            now=resolved_now,
            limit=limit,
            lease_seconds=lease_seconds,
        )
        summary.pending = len(dispatches)
        for dispatch in dispatches:
            if _send_claimed_dispatch(
                session,
                dispatch=dispatch,
                enqueue=enqueue,
                now=resolved_now,
            ):
                summary.sent += 1
        summary.dead = session.scalar(
            select(func.count())
            .select_from(AsyncDispatch)
            .where(AsyncDispatch.status == AsyncDispatchStatus.DEAD)
        ) or 0
    except Exception:
        session.rollback()
        logger.exception("异步投递调度器运行失败")
        raise
    finally:
        session.close()
    return summary


def mark_async_dispatch_failed(
    session: Session,
    *,
    dispatch_id: str,
    aggregate_id: str,
    lease_token: str,
    error: str,
    max_attempts: int = DEFAULT_DISPATCH_MAX_ATTEMPTS,
    backoff_seconds: int = DEFAULT_DISPATCH_BACKOFF_SECONDS,
    now: datetime | None = None,
) -> bool:
    """Release a consumer lease after target failure for bounded retry/dead-letter handling."""
    resolved_now = now or now_utc()
    attempts = session.scalar(
        select(AsyncDispatch.attempts).where(
            AsyncDispatch.id == dispatch_id,
            AsyncDispatch.aggregate_id == aggregate_id,
            AsyncDispatch.status == AsyncDispatchStatus.SENT,
            AsyncDispatch.lease_token == lease_token,
        )
    )
    if attempts is None:
        return False
    next_status = AsyncDispatchStatus.DEAD if attempts >= max_attempts else AsyncDispatchStatus.PENDING
    values: dict[str, Any] = {
        "status": next_status,
        "lease_token": None,
        "lease_expires_at": None,
        "sent_at": None,
        "consumed_at": None,
        "last_error": error[:1000],
        "updated_at": resolved_now,
    }
    if next_status == AsyncDispatchStatus.PENDING:
        values["available_at"] = resolved_now + timedelta(seconds=backoff_seconds)
    result = session.execute(
        update(AsyncDispatch)
        .where(
            AsyncDispatch.id == dispatch_id,
            AsyncDispatch.aggregate_id == aggregate_id,
            AsyncDispatch.status == AsyncDispatchStatus.SENT,
            AsyncDispatch.lease_token == lease_token,
        )
        .values(**values)
        .execution_options(synchronize_session=False)
    )
    return result.rowcount == 1


def mark_async_dispatch_consumed(
    session: Session,
    *,
    dispatch_id: str,
    aggregate_id: str,
    lease_token: str | None = None,
    now: datetime | None = None,
) -> bool:
    """Atomically mark a sent dispatch consumed by its owning worker."""
    resolved_now = now or now_utc()
    lease_predicate = (
        AsyncDispatch.lease_token == lease_token
        if lease_token is not None
        else AsyncDispatch.lease_token.is_(None)
    )
    result = session.execute(
        update(AsyncDispatch)
        .where(
            AsyncDispatch.id == dispatch_id,
            AsyncDispatch.aggregate_id == aggregate_id,
            AsyncDispatch.status == AsyncDispatchStatus.SENT,
            lease_predicate,
        )
        .values(
            status=AsyncDispatchStatus.CONSUMED,
            lease_token=None,
            lease_expires_at=None,
            consumed_at=resolved_now,
            updated_at=resolved_now,
        )
        .execution_options(synchronize_session=False)
    )
    return result.rowcount == 1
