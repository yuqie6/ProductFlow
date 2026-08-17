from __future__ import annotations

from datetime import UTC, datetime, timedelta

import pytest
from sqlalchemy import select

from productflow_backend.application.async_delivery import (
    claim_async_dispatch_for_consumption,
    delivery_key_for_actor,
    mark_async_dispatch_consumed,
    mark_async_dispatch_failed,
    requeue_async_dispatch,
    run_async_dispatcher_once,
    stage_async_dispatch,
)
from productflow_backend.domain.enums import AsyncDispatchStatus
from productflow_backend.infrastructure.db.models import AsyncDispatch


def test_stage_async_dispatch_is_idempotent_by_delivery_key(db_session) -> None:
    first = stage_async_dispatch(
        db_session,
        delivery_key="workflow_run:run-1",
        actor_name="run_product_workflow_run",
        aggregate_id="run-1",
        payload={"scope": "workflow"},
    )
    second = stage_async_dispatch(
        db_session,
        delivery_key="workflow_run:run-1",
        actor_name="run_product_workflow_run",
        aggregate_id="run-1",
    )
    db_session.commit()

    assert first.id == second.id
    assert first.status == AsyncDispatchStatus.PENDING
    assert first.aggregate_id == "run-1"
    assert db_session.scalar(select(AsyncDispatch).where(AsyncDispatch.id == first.id)) is not None


def test_actor_delivery_key_is_shared_by_stage_and_requeue(db_session) -> None:
    key = delivery_key_for_actor("run_image_session_generation_task", "task-1")
    staged = stage_async_dispatch(
        db_session,
        delivery_key=key,
        actor_name="run_image_session_generation_task",
        aggregate_id="task-1",
    )
    db_session.commit()

    requeued = requeue_async_dispatch(
        db_session,
        delivery_key=delivery_key_for_actor("run_image_session_generation_task", "task-1"),
        actor_name="run_image_session_generation_task",
        aggregate_id="task-1",
    )
    db_session.commit()

    assert requeued.id == staged.id
    assert db_session.scalar(select(AsyncDispatch).where(AsyncDispatch.aggregate_id == "task-1")).id == staged.id


def test_dispatcher_sends_pending_and_marks_sent(db_session) -> None:
    dispatch = stage_async_dispatch(
        db_session,
        delivery_key="delivery:job-1",
        actor_name="run_delivery_rendition_job",
        aggregate_id="job-1",
    )
    db_session.commit()

    sent: list[tuple[str, str]] = []
    observed_status: list[AsyncDispatchStatus] = []

    def enqueue(dispatch_id: str, aggregate_id: str) -> None:
        db_session.expire_all()
        observed = db_session.get(AsyncDispatch, dispatch_id)
        assert observed is not None
        observed_status.append(observed.status)
        sent.append((dispatch_id, aggregate_id))

    summary = run_async_dispatcher_once(enqueue=enqueue)

    assert summary.sent == 1
    assert observed_status == [AsyncDispatchStatus.SENT]
    assert sent == [(dispatch.id, "job-1")]
    db_session.expire_all()
    persisted = db_session.get(AsyncDispatch, dispatch.id)
    assert persisted is not None
    assert persisted.status == AsyncDispatchStatus.SENT
    assert persisted.sent_at is not None


def test_dispatcher_enqueue_failure_preserves_sent_for_reconciliation(db_session) -> None:
    dispatch = stage_async_dispatch(
        db_session,
        delivery_key="delivery:job-fail",
        actor_name="run_delivery_rendition_job",
        aggregate_id="job-fail",
    )
    db_session.commit()

    def fail_enqueue(dispatch_id: str, aggregate_id: str) -> None:
        raise RuntimeError("redis down")

    run_async_dispatcher_once(
        enqueue=fail_enqueue,
        max_attempts=1,
        backoff_seconds=1,
    )
    db_session.expire_all()
    persisted = db_session.get(AsyncDispatch, dispatch.id)
    assert persisted is not None
    assert persisted.status == AsyncDispatchStatus.SENT
    assert persisted.last_error == "redis down"


def test_dead_dispatch_requires_explicit_requeue(db_session) -> None:
    dispatch = stage_async_dispatch(
        db_session,
        delivery_key="delivery:dead-retry",
        actor_name="run_delivery_rendition_job",
        aggregate_id="dead-retry",
    )
    db_session.commit()
    old = datetime.now(UTC)

    run_async_dispatcher_once(
        enqueue=lambda dispatch_id, aggregate_id: None,
        now=old,
        max_attempts=1,
    )
    token = claim_async_dispatch_for_consumption(
        db_session,
        dispatch_id=dispatch.id,
        aggregate_id="dead-retry",
    )
    assert token is not None
    assert mark_async_dispatch_failed(
        db_session,
        dispatch_id=dispatch.id,
        aggregate_id="dead-retry",
        lease_token=token,
        error="target failed",
        max_attempts=1,
        backoff_seconds=0,
    ) is True
    db_session.commit()
    db_session.expire_all()
    assert db_session.get(AsyncDispatch, dispatch.id).status == AsyncDispatchStatus.DEAD

    stage_async_dispatch(
        db_session,
        delivery_key="delivery:dead-retry",
        actor_name="run_delivery_rendition_job",
        aggregate_id="dead-retry",
    )
    db_session.commit()
    db_session.expire_all()
    assert db_session.get(AsyncDispatch, dispatch.id).status == AsyncDispatchStatus.DEAD

    sent: list[tuple[str, str]] = []
    run_async_dispatcher_once(
        enqueue=lambda dispatch_id, aggregate_id: sent.append((dispatch_id, aggregate_id)),
        now=old + timedelta(minutes=2),
        max_attempts=1,
    )
    db_session.expire_all()
    assert db_session.get(AsyncDispatch, dispatch.id).status == AsyncDispatchStatus.DEAD
    assert sent == []

    requeue_async_dispatch(
        db_session,
        delivery_key="delivery:dead-retry",
        actor_name="run_delivery_rendition_job",
        aggregate_id="dead-retry",
        available_at=old + timedelta(minutes=2),
    )
    db_session.commit()
    run_async_dispatcher_once(
        enqueue=lambda dispatch_id, aggregate_id: sent.append((dispatch_id, aggregate_id)),
        now=old + timedelta(minutes=2),
        max_attempts=1,
    )
    assert sent == [(dispatch.id, "dead-retry")]


def test_enqueue_ambiguity_keeps_sent_for_reconciliation(db_session) -> None:
    dispatch = stage_async_dispatch(
        db_session,
        delivery_key="delivery:ambiguous-publish",
        actor_name="run_delivery_rendition_job",
        aggregate_id="ambiguous-publish",
    )
    db_session.commit()

    def broker_accepts_then_times_out(dispatch_id: str, aggregate_id: str) -> None:
        del dispatch_id, aggregate_id
        raise TimeoutError("broker publish acknowledgement timed out")

    run_async_dispatcher_once(
        enqueue=broker_accepts_then_times_out,
        max_attempts=1,
    )

    db_session.expire_all()
    persisted = db_session.get(AsyncDispatch, dispatch.id)
    assert persisted is not None
    assert persisted.status == AsyncDispatchStatus.SENT
    assert persisted.lease_token is None
    assert persisted.last_error == "broker publish acknowledgement timed out"


def test_mark_consumed_is_atomic_and_idempotent(db_session) -> None:
    dispatch = stage_async_dispatch(
        db_session,
        delivery_key="agent:projection-1",
        actor_name="run_agent_turn_sync",
        aggregate_id="projection-1",
    )
    db_session.commit()
    run_async_dispatcher_once(
        enqueue=lambda dispatch_id, aggregate_id: None,
    )
    db_session.expire_all()
    persisted = db_session.get(AsyncDispatch, dispatch.id)
    assert persisted is not None
    assert persisted.status == AsyncDispatchStatus.SENT

    assert mark_async_dispatch_consumed(
        db_session,
        dispatch_id=dispatch.id,
        aggregate_id="projection-1",
    ) is True
    db_session.commit()
    db_session.expire_all()
    persisted = db_session.get(AsyncDispatch, dispatch.id)
    assert persisted is not None
    assert persisted.status == AsyncDispatchStatus.CONSUMED

    assert mark_async_dispatch_consumed(
        db_session,
        dispatch_id=dispatch.id,
        aggregate_id="projection-1",
    ) is False
    db_session.rollback()


def test_mark_failed_releases_consumer_lease_for_bounded_retry(db_session) -> None:
    dispatch = stage_async_dispatch(
        db_session,
        delivery_key="delivery:consumer-failure",
        actor_name="run_agent_turn_sync",
        aggregate_id="consumer-failure",
    )
    db_session.commit()
    run_async_dispatcher_once(enqueue=lambda dispatch_id, aggregate_id: None)
    token = claim_async_dispatch_for_consumption(
        db_session,
        dispatch_id=dispatch.id,
        aggregate_id="consumer-failure",
    )
    assert token is not None

    assert mark_async_dispatch_failed(
        db_session,
        dispatch_id=dispatch.id,
        aggregate_id="consumer-failure",
        lease_token=token,
        error="target failed",
        max_attempts=3,
        backoff_seconds=0,
    ) is True
    db_session.commit()
    db_session.expire_all()
    persisted = db_session.get(AsyncDispatch, dispatch.id)
    assert persisted is not None
    assert persisted.status == AsyncDispatchStatus.PENDING
    assert persisted.lease_token is None
    assert persisted.last_error == "target failed"


def test_worker_target_failure_requeues_dispatch(monkeypatch, db_session) -> None:
    from productflow_backend import workers

    dispatch = stage_async_dispatch(
        db_session,
        delivery_key="delivery:worker-failure",
        actor_name="run_agent_turn_sync",
        aggregate_id="worker-failure",
    )
    db_session.commit()
    run_async_dispatcher_once(enqueue=lambda dispatch_id, aggregate_id: None)
    monkeypatch.setattr(
        workers,
        "_execute_async_dispatch_target",
        lambda actor_name, aggregate_id: (_ for _ in ()).throw(RuntimeError("target failed")),
    )

    with pytest.raises(RuntimeError, match="target failed"):
        workers.execute_async_dispatch(dispatch.id, "worker-failure")

    db_session.expire_all()
    persisted = db_session.get(AsyncDispatch, dispatch.id)
    assert persisted is not None
    assert persisted.status == AsyncDispatchStatus.PENDING
    assert persisted.lease_token is None


def test_stage_async_dispatch_resets_consumed_to_pending(db_session) -> None:
    dispatch = stage_async_dispatch(
        db_session,
        delivery_key="delivery:retry-after-consumed",
        actor_name="run_delivery_rendition_job",
        aggregate_id="job-retry",
    )
    db_session.commit()
    run_async_dispatcher_once(
        enqueue=lambda dispatch_id, aggregate_id: None,
    )
    db_session.expire_all()
    persisted = db_session.get(AsyncDispatch, dispatch.id)
    assert persisted is not None
    assert persisted.status == AsyncDispatchStatus.SENT
    assert mark_async_dispatch_consumed(
        db_session,
        dispatch_id=dispatch.id,
        aggregate_id="job-retry",
    ) is True
    db_session.commit()
    db_session.expire_all()

    restaged = stage_async_dispatch(
        db_session,
        delivery_key="delivery:retry-after-consumed",
        actor_name="run_delivery_rendition_job",
        aggregate_id="job-retry",
    )
    db_session.commit()
    db_session.expire_all()
    persisted = db_session.get(AsyncDispatch, restaged.id)
    assert persisted is not None
    assert persisted.status == AsyncDispatchStatus.PENDING
    assert persisted.attempts == 0
    assert persisted.consumed_at is None


def test_requeue_does_not_revoke_active_consumer_lease(db_session) -> None:
    dispatch = stage_async_dispatch(
        db_session,
        delivery_key="delivery:active-lease",
        actor_name="run_agent_turn_sync",
        aggregate_id="active-lease",
    )
    db_session.commit()
    run_async_dispatcher_once(enqueue=lambda dispatch_id, aggregate_id: None)
    lease_token = claim_async_dispatch_for_consumption(
        db_session,
        dispatch_id=dispatch.id,
        aggregate_id="active-lease",
    )
    assert lease_token is not None

    requeue_async_dispatch(
        db_session,
        delivery_key="delivery:active-lease",
        actor_name="run_agent_turn_sync",
        aggregate_id="active-lease",
    )
    db_session.commit()
    db_session.expire_all()
    persisted = db_session.get(AsyncDispatch, dispatch.id)
    assert persisted is not None
    assert persisted.status == AsyncDispatchStatus.SENT
    assert persisted.lease_token == lease_token


def test_stale_sent_reconciliation_skips_active_consumer_lease(db_session) -> None:
    dispatch = stage_async_dispatch(
        db_session,
        delivery_key="delivery:long-running",
        actor_name="run_image_session_generation_task",
        aggregate_id="long-running",
    )
    db_session.commit()
    sent_at = datetime.now(UTC)
    run_async_dispatcher_once(
        enqueue=lambda dispatch_id, aggregate_id: None,
        now=sent_at,
    )
    db_session.expire_all()
    persisted = db_session.get(AsyncDispatch, dispatch.id)
    assert persisted is not None
    assert persisted.status == AsyncDispatchStatus.SENT

    lease_token = claim_async_dispatch_for_consumption(
        db_session,
        dispatch_id=dispatch.id,
        aggregate_id="long-running",
        now=sent_at,
        lease_seconds=3600,
    )
    assert lease_token is not None

    run_async_dispatcher_once(
        enqueue=lambda dispatch_id, aggregate_id: (_ for _ in ()).throw(AssertionError("active work duplicated")),
        now=sent_at + timedelta(minutes=6),
    )
    db_session.expire_all()
    persisted = db_session.get(AsyncDispatch, dispatch.id)
    assert persisted is not None
    assert persisted.status == AsyncDispatchStatus.SENT
    assert persisted.lease_token == lease_token
    assert mark_async_dispatch_consumed(
        db_session,
        dispatch_id=dispatch.id,
        aggregate_id="long-running",
        lease_token=lease_token,
    ) is True
    db_session.commit()


def test_requeue_async_dispatch_resets_sent_to_pending(db_session) -> None:
    dispatch = stage_async_dispatch(
        db_session,
        delivery_key="agent:projection-poll",
        actor_name="run_agent_turn_sync",
        aggregate_id="projection-poll",
    )
    db_session.commit()
    run_async_dispatcher_once(
        enqueue=lambda dispatch_id, aggregate_id: None,
    )
    db_session.expire_all()
    persisted = db_session.get(AsyncDispatch, dispatch.id)
    assert persisted is not None
    assert persisted.status == AsyncDispatchStatus.SENT

    future = datetime.now(UTC) + timedelta(seconds=30)
    requeued = requeue_async_dispatch(
        db_session,
        delivery_key="agent:projection-poll",
        actor_name="run_agent_turn_sync",
        aggregate_id="projection-poll",
        available_at=future,
    )
    db_session.commit()
    db_session.expire_all()
    persisted = db_session.get(AsyncDispatch, requeued.id)
    assert persisted is not None
    assert persisted.status == AsyncDispatchStatus.PENDING
    assert persisted.sent_at is None
    assert persisted.available_at is not None


def test_expired_lease_is_reconciled_to_pending(db_session) -> None:
    old = datetime.now(UTC) - timedelta(hours=1)
    dispatch = stage_async_dispatch(
        db_session,
        delivery_key="workflow:run-lease",
        actor_name="run_product_workflow_run",
        aggregate_id="run-lease",
    )
    dispatch.lease_token = "stale-token"
    dispatch.lease_expires_at = old
    dispatch.available_at = old
    db_session.commit()

    sent: list[tuple[str, str]] = []
    run_async_dispatcher_once(
        enqueue=lambda dispatch_id, aggregate_id: sent.append((dispatch_id, aggregate_id)),
        now=datetime.now(UTC),
    )

    assert sent == [(dispatch.id, "run-lease")]
    db_session.expire_all()
    persisted = db_session.get(AsyncDispatch, dispatch.id)
    assert persisted is not None
    assert persisted.status == AsyncDispatchStatus.SENT
    assert persisted.lease_token is None
