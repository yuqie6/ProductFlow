"""AsyncDispatch 投递账本。PostgreSQL 是权威；SENT 表示已交给 broker，不等于 CONSUMED。"""

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
    """提交和恢复共用的稳定幂等键。"""
    return f"{actor_name}:{aggregate_id}"


def _as_aware_utc(value: datetime) -> datetime:
    if value.tzinfo is None:
        return value.replace(tzinfo=UTC)
    return value


def _validate_existing_dispatch_identity(
    existing: AsyncDispatch,
    *,
    actor_name: str,
    aggregate_id: str,
    payload: dict[str, Any] | None,
) -> None:
    """同一 delivery_key 不能改绑 actor、aggregate 或 payload。"""
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
    """创建或返回耐久投递行，不 commit。

    调用方拥有事务；本 helper 只 flush，好让调用方拿到 dispatch id。
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
        now = now_utc()
        if existing.status == AsyncDispatchStatus.CONSUMED:
            existing.status = AsyncDispatchStatus.PENDING
            # 可轮询目标可能在 worker 把本行标成 CONSUMED 之前就写下一次投递时间。
            # 常驻恢复可能先看到 CONSUMED 行、broker 还没发出下一封，不能用 now 盖掉那个未来时间。
            existing.available_at = available_at or existing.available_at or now
            existing.lease_token = None
            existing.lease_expires_at = None
            existing.attempts = 0
            existing.last_error = None
            existing.sent_at = None
            existing.consumed_at = None
            existing.updated_at = now
            session.flush()
        elif (
            existing.status == AsyncDispatchStatus.SENT
            and available_at is not None
            and existing.lease_token is not None
        ):
            lease_expires_at = existing.lease_expires_at
            if lease_expires_at is None or _as_aware_utc(lease_expires_at) > now:
                # 当前 SENT 仍被消费者持有。保留这条 lease，只把下一次轮询时间写进账本。
                existing.available_at = available_at
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
    """为恢复或重试创建投递行，或把已有行重置为 pending。"""
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
    """按 actor+aggregate 暂存 PENDING 行，不 commit。调用方持有事务。"""
    available_at = now_utc() + timedelta(milliseconds=delay_ms) if delay_ms is not None else None
    return stage_async_dispatch(
        session,
        delivery_key=delivery_key_for_actor(actor_name, aggregate_id),
        actor_name=actor_name,
        aggregate_id=aggregate_id,
        available_at=available_at,
    )


def recover_async_dispatch_for_actor(
    session: Session,
    actor_name: str,
    aggregate_id: str,
    *,
    delay_ms: int | None = None,
) -> AsyncDispatch:
    """为 actor 暂存一次投递，并尝试恢复一条过期发布造成的死信。

    普通 stage 不会复活 DEAD：死信是明确的重试边界。Agent Turn 恢复只允许
    复活第一条默认绑定、且没有记录错误的死信——这证明是过期 SENT 对账，
    不是目标失败——并保留已有 attempts。
    """
    available_at = now_utc() + timedelta(milliseconds=delay_ms) if delay_ms is not None else None
    dispatch = stage_async_dispatch(
        session,
        delivery_key=delivery_key_for_actor(actor_name, aggregate_id),
        actor_name=actor_name,
        aggregate_id=aggregate_id,
        available_at=available_at,
    )
    if (
        dispatch.status != AsyncDispatchStatus.DEAD
        or dispatch.attempts != DEFAULT_DISPATCH_MAX_ATTEMPTS
        or dispatch.last_error is not None
    ):
        return dispatch
    return _recover_stale_dead_dispatch(
        session,
        dispatch_id=dispatch.id,
        available_at=available_at,
    )


def _recover_stale_dead_dispatch(
    session: Session,
    *,
    dispatch_id: str,
    available_at: datetime | None = None,
) -> AsyncDispatch:
    """有条件地重开一条过期发布死信，不重置 attempts。"""
    # SENT 可能在过期发布对账时变成 DEAD，当时并没有执行错误。只恢复这第一条死信边界，
    # 保留投递次数，让下一次 claim 是 attempt N+1。目标失败和更晚的死信保持 DEAD，
    # 直到运维显式再投；否则常驻扫描会抹掉重试上限。
    now = now_utc()
    session.execute(
        update(AsyncDispatch)
        .where(
            AsyncDispatch.id == dispatch_id,
            AsyncDispatch.status == AsyncDispatchStatus.DEAD,
            AsyncDispatch.attempts == DEFAULT_DISPATCH_MAX_ATTEMPTS,
            AsyncDispatch.last_error.is_(None),
        )
        .values(
            status=AsyncDispatchStatus.PENDING,
            available_at=available_at or now,
            lease_token=None,
            lease_expires_at=None,
            sent_at=None,
            consumed_at=None,
            updated_at=now,
        )
        .execution_options(synchronize_session=False)
    )
    dispatch = session.get(AsyncDispatch, dispatch_id)
    if dispatch is None:
        raise RuntimeError(f"async dispatch disappeared during dead recovery: {dispatch_id}")
    session.expire(dispatch)
    session.refresh(dispatch)
    return dispatch


def enqueue_async_dispatch_for_actor(
    actor_name: str,
    aggregate_id: str,
    *,
    delay_ms: int | None = None,
    allow_active_lease: bool = False,
) -> None:
    """打开 Session，为恢复回调创建或再排队一条投递行。"""
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
    """领取到期 PENDING。只 flush lease，由调用方决定何时标 SENT。"""
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
    # 先把行写成 SENT 再碰 broker，这样跑得快的 worker 可以立刻 claim。
    # 若进程在 enqueue 前死掉，过期对账会把这行退回 pending。
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
        # broker 可能已经收下消息，客户端却看到错误。保持 SENT，由过期对账而不是这个异常
        # 决定模糊发布何时可以重试。
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
            # 目标可能已经写下一次轮询时间，而当前 SENT 行仍有消费 lease。
            # 消费者若在标 CONSUMED 前死掉，对账必须保留计划延迟，不能立刻再投。
            if _as_aware_utc(dispatch.available_at) <= now:
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
    """从 SENT 抢消费 lease。抢不到说明另一 worker 正在跑或行已不是 SENT。本函数 commit。"""
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
    """调度一轮：对账过期 lease / 陈旧 SENT，再 claim PENDING 并先标 SENT 再 enqueue。"""
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
    """目标失败后释放消费 lease，走有界重试或死信。"""
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
    """把 SENT 标 CONSUMED。SENT 只表示已交给 broker，消费完成才是 CONSUMED。"""
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
