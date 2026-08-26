from __future__ import annotations

import importlib
import os
import signal
import socket
import subprocess
import sys
import time
from collections.abc import Iterator
from contextlib import contextmanager
from datetime import UTC, datetime, timedelta
from pathlib import Path
from threading import Event
from types import ModuleType
from urllib.parse import parse_qsl, urlencode, urlsplit, urlunsplit
from uuid import uuid4

import dramatiq
import pytest
from dramatiq.brokers.redis import RedisBroker
from dramatiq.message import Message
from dramatiq.worker import Worker
from sqlalchemy import create_engine, select, text
from sqlalchemy.engine import URL, make_url

from productflow_backend.application.agent.sessions import create_agent_session
from productflow_backend.application.agent.sync import recover_unfinished_agent_turn_syncs
from productflow_backend.application.agent.turn_projection import reserve_agent_turn
from productflow_backend.application.async_delivery import (
    claim_async_dispatch_for_consumption,
    mark_async_dispatch_consumed,
    run_async_dispatcher_once,
    stage_async_dispatch_for_actor,
)
from productflow_backend.application.durable_recovery import recover_unfinished_workflow_runs
from productflow_backend.config import get_settings
from productflow_backend.domain.enums import (
    AsyncDispatchStatus,
    GraphNodeType,
    GraphRunScope,
    WorkflowNodeStatus,
    WorkflowRunStatus,
)
from productflow_backend.infrastructure.db.models import (
    AgentTurnProjection,
    AsyncDispatch,
    Base,
    Product,
    WorkflowGraph,
    WorkflowGraphNode,
    WorkflowGraphNodeRun,
    WorkflowGraphRun,
)
from productflow_backend.infrastructure.db.session import get_engine, get_session_factory
from productflow_backend.infrastructure.queue import enqueue_async_dispatch, enqueue_graph_run, get_broker
from productflow_backend.infrastructure.runtime_settings import get_runtime_settings

LIVE_RECOVERY_SWITCH = "PRODUCTFLOW_RUN_LIVE_RECOVERY"
LIVE_REDIS_RESTART_SWITCH = "PRODUCTFLOW_RUN_LIVE_AGENT_REDIS_RESTART"
LIVE_REDIS_CONNECTION_SWITCH = "PRODUCTFLOW_RUN_LIVE_AGENT_REDIS_CONNECTION"
LIVE_DISPATCHER_WATCH_SWITCH = "PRODUCTFLOW_RUN_LIVE_AGENT_DISPATCHER_WATCH"
WORKERS_MODULE = "productflow_backend.workers"
REDIS_TEST_DATABASE = 15

pytestmark = [
    pytest.mark.live_dependencies,
    pytest.mark.skipif(
        os.getenv(LIVE_RECOVERY_SWITCH) != "1",
        reason=f"set {LIVE_RECOVERY_SWITCH}=1 to run the PostgreSQL/Redis recovery gate",
    ),
]


def _required_environment(name: str) -> str:
    value = os.getenv(name, "").strip()
    if not value:
        pytest.fail(f"{name} must be provided by the development environment", pytrace=False)
    return value


def _redis_url_for_database(base_url: str, database: int) -> str:
    parsed = urlsplit(base_url)
    if parsed.scheme not in {"redis", "rediss"}:
        pytest.fail("REDIS_URL must use redis:// or rediss:// for the live recovery gate", pytrace=False)
    query = urlencode([(key, value) for key, value in parse_qsl(parsed.query) if key != "db"])
    return urlunsplit(parsed._replace(path=f"/{database}", query=query))


def _unused_tcp_port() -> int:
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as listener:
        listener.bind(("127.0.0.1", 0))
        return int(listener.getsockname()[1])


def _compose(*arguments: str) -> None:
    repo_root = Path(__file__).resolve().parents[2]
    subprocess.run(
        ["bash", "scripts/with_dev_env.sh", "docker", "compose", *arguments],
        cwd=repo_root,
        check=True,
        timeout=90,
        capture_output=True,
        text=True,
    )


def _restart_redis_service() -> None:
    try:
        _compose("restart", "productflow-redis")
    finally:
        _compose("up", "-d", "--wait", "productflow-redis")


@contextmanager
def _temporary_postgres_database(base_database_url: str) -> Iterator[URL]:
    try:
        base_url = make_url(base_database_url)
    except Exception:
        pytest.fail("DATABASE_URL must be a valid SQLAlchemy URL", pytrace=False)
    if base_url.get_backend_name() != "postgresql":
        pytest.fail("DATABASE_URL must point to PostgreSQL for the live recovery gate", pytrace=False)

    database_name = f"productflow_live_recovery_{uuid4().hex}"
    maintenance_engine = create_engine(
        base_url.set(database="postgres"),
        future=True,
        isolation_level="AUTOCOMMIT",
        pool_pre_ping=True,
    )
    quoted_database_name = maintenance_engine.dialect.identifier_preparer.quote(database_name)
    created = False
    try:
        with maintenance_engine.connect() as connection:
            connection.exec_driver_sql(f"CREATE DATABASE {quoted_database_name}")
        created = True
        yield base_url.set(database=database_name)
    finally:
        try:
            if created:
                with maintenance_engine.connect() as connection:
                    connection.execute(
                        text(
                            "SELECT pg_terminate_backend(pid) "
                            "FROM pg_stat_activity "
                            "WHERE datname = :database_name AND pid <> pg_backend_pid()"
                        ),
                        {"database_name": database_name},
                    ).all()
                    connection.exec_driver_sql(f"DROP DATABASE IF EXISTS {quoted_database_name}")
                    database_exists = connection.scalar(
                        text("SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = :database_name)"),
                        {"database_name": database_name},
                    )
                    assert database_exists is False
        finally:
            maintenance_engine.dispose()


def _reset_runtime_state() -> None:
    sys.modules.pop(WORKERS_MODULE, None)
    cached_engine = get_engine() if get_engine.cache_info().currsize else None
    cached_broker = get_broker() if get_broker.cache_info().currsize else None
    get_session_factory.cache_clear()
    try:
        if cached_broker is not None:
            cached_broker.client.connection_pool.disconnect()
    finally:
        get_broker.cache_clear()
        try:
            if cached_engine is not None:
                cached_engine.dispose()
        finally:
            get_engine.cache_clear()
            get_settings.cache_clear()
            dramatiq_broker_module = importlib.import_module("dramatiq.broker")
            dramatiq_broker_module.global_broker = None


@pytest.fixture()
def live_recovery_dependencies(
    monkeypatch: pytest.MonkeyPatch,
    tmp_path: Path,
) -> Iterator[tuple[RedisBroker, ModuleType]]:
    base_database_url = _required_environment("DATABASE_URL")
    base_redis_url = _required_environment("REDIS_URL")
    isolated_redis_url = _redis_url_for_database(base_redis_url, REDIS_TEST_DATABASE)

    with _temporary_postgres_database(base_database_url) as temporary_database_url:
        with monkeypatch.context() as environment:
            environment.setenv("DATABASE_URL", temporary_database_url.render_as_string(hide_password=False))
            environment.setenv("REDIS_URL", isolated_redis_url)
            environment.setenv("LOG_DIR", str(tmp_path / "logs"))
            environment.setenv("STORAGE_ROOT", str(tmp_path / "storage"))
            _reset_runtime_state()

            broker: RedisBroker | None = None
            try:
                engine = get_engine()
                Base.metadata.create_all(engine)

                broker = get_broker()
                assert broker.client.connection_pool.connection_kwargs["db"] == REDIS_TEST_DATABASE
                broker.client.flushdb()

                workers = importlib.import_module(WORKERS_MODULE)
                assert workers.run_workflow_graph_run.broker is broker
                yield broker, workers
            finally:
                try:
                    if broker is not None:
                        broker.client.flushdb()
                        assert broker.client.dbsize() == 0
                finally:
                    _reset_runtime_state()

        _reset_runtime_state()


def test_recover_queued_workflow_run_through_postgres_and_redis(
    live_recovery_dependencies: tuple[RedisBroker, ModuleType],
) -> None:
    broker, workers = live_recovery_dependencies
    session_factory = get_session_factory()

    with session_factory() as session:
        product = Product(name="Live recovery gate product")
        graph = WorkflowGraph(
            product=product,
            title="Live recovery gate workflow",
            revision=1,
            schema_version=3,
            active=True,
        )
        session.add(product)
        session.flush()
        node = WorkflowGraphNode(
            graph_id=graph.id,
            node_type=GraphNodeType.PROMPT_GENERATION,
            title="Queued prompt node",
        )
        session.add(node)
        session.flush()
        graph_run = WorkflowGraphRun(
            graph_id=graph.id,
            status=WorkflowRunStatus.RUNNING,
            run_scope=GraphRunScope.GRAPH,
            graph_revision=1,
            snapshot_json={"revision": 1, "nodes": [], "edges": [], "groups": []},
        )
        session.add(graph_run)
        session.flush()
        node_run = WorkflowGraphNodeRun(
            graph_run_id=graph_run.id,
            node_id=node.id,
            status=WorkflowNodeStatus.QUEUED,
            sort_order=0,
        )
        session.add(node_run)
        session.commit()
        run_id = graph_run.id
        node_run_id = node_run.id

    summary = recover_unfinished_workflow_runs(enqueue=enqueue_graph_run)

    assert summary.queued_runs == 1
    assert summary.stale_running_runs == 0
    assert summary.enqueued_runs == 1

    with session_factory() as session:
        persisted_run = session.get(WorkflowGraphRun, run_id)
        persisted_node_run = session.get(WorkflowGraphNodeRun, node_run_id)
        active_run_ids = set(
            session.scalars(
                select(WorkflowGraphRun.id).where(WorkflowGraphRun.status == WorkflowRunStatus.RUNNING)
            ).all()
        )
        assert persisted_run is not None
        assert persisted_run.status == WorkflowRunStatus.RUNNING
        assert persisted_node_run is not None
        assert persisted_node_run.status == WorkflowNodeStatus.QUEUED
        assert active_run_ids == {run_id}

    consumer = broker.consume(workers.run_workflow_graph_run.queue_name, prefetch=1, timeout=5_000)
    try:
        message = next(consumer)
        assert message is not None
        assert message.actor_name == "run_workflow_graph_run"
        assert message.args == (run_id,)
        assert message.kwargs == {}
        consumer.ack(message)
        broker.join(workers.run_workflow_graph_run.queue_name, timeout=5_000)
    finally:
        consumer.close()


def test_recover_queued_agent_turn_sync_through_postgres_and_redis(
    live_recovery_dependencies: tuple[RedisBroker, ModuleType],
) -> None:
    broker, workers = live_recovery_dependencies
    session_factory = get_session_factory()

    with session_factory() as session:
        agent_session = create_agent_session(session, title="Live Agent recovery gate")
        conversation = agent_session.conversations[0]
        reservation = reserve_agent_turn(
            session,
            product_id=None,
            conversation_id=conversation.id,
            input_text="验证 Agent Turn durable recovery 投递",
            input_asset_ids=[],
            idempotency_key=f"live-agent-recovery-turn-{uuid4().hex}",
        )
        projection_id = reservation.projection.id

    recovery = recover_unfinished_agent_turn_syncs(
        stage_dispatch=lambda session, target_id: stage_async_dispatch_for_actor(
            session,
            "run_agent_turn_sync",
            target_id,
        )
    )

    assert recovery.pending_turns == 1
    assert recovery.enqueued_turns == 1
    with session_factory() as session:
        dispatch = session.scalar(
            select(AsyncDispatch).where(
                AsyncDispatch.actor_name == "run_agent_turn_sync",
                AsyncDispatch.aggregate_id == projection_id,
            )
        )
        projection = session.get(AgentTurnProjection, projection_id)
        assert dispatch is not None
        assert projection is not None
        assert dispatch.status.value == "pending"
        assert projection.status.value == "queued"
        dispatch_id = dispatch.id

    dispatched = run_async_dispatcher_once(enqueue=enqueue_async_dispatch, limit=10)

    assert dispatched.sent == 1
    consumer = broker.consume(workers.run_async_dispatch.queue_name, prefetch=1, timeout=5_000)
    try:
        message = next(consumer)
        assert message is not None
        assert message.actor_name == "run_async_dispatch"
        assert message.args == (dispatch_id, projection_id)
        assert message.kwargs == {}
        consumer.ack(message)
        broker.join(workers.run_async_dispatch.queue_name, timeout=5_000)
    finally:
        consumer.close()

    with session_factory() as session:
        persisted = session.get(AsyncDispatch, dispatch_id)
        assert persisted is not None
        assert persisted.status.value == "sent"


def test_agent_poll_delay_survives_consumer_handoff_before_redis_redelivery(
    live_recovery_dependencies: tuple[RedisBroker, ModuleType],
) -> None:
    broker, workers = live_recovery_dependencies
    session_factory = get_session_factory()
    aggregate_id = "live-agent-poll-delay-handoff"

    with session_factory() as session:
        dispatch = stage_async_dispatch_for_actor(session, "run_agent_turn_sync", aggregate_id)
        session.commit()
        dispatch_id = dispatch.id

    first_dispatch = run_async_dispatcher_once(enqueue=enqueue_async_dispatch, limit=10)
    assert first_dispatch.sent == 1

    consumer = broker.consume(workers.run_async_dispatch.queue_name, prefetch=1, timeout=5_000)
    try:
        message = next(consumer)
        assert message is not None
        assert message.actor_name == "run_async_dispatch"
        assert message.args == (dispatch_id, aggregate_id)

        with session_factory() as session:
            lease_token = claim_async_dispatch_for_consumption(
                session,
                dispatch_id=dispatch_id,
                aggregate_id=aggregate_id,
            )
        assert lease_token is not None

        scheduled_after = datetime.now(UTC)
        with session_factory() as session:
            scheduled = stage_async_dispatch_for_actor(
                session,
                "run_agent_turn_sync",
                aggregate_id,
                delay_ms=5_000,
            )
            session.commit()
            assert scheduled.status == AsyncDispatchStatus.SENT
            assert scheduled.lease_token == lease_token
            assert scheduled.available_at > scheduled_after

        with session_factory() as session:
            assert mark_async_dispatch_consumed(
                session,
                dispatch_id=dispatch_id,
                aggregate_id=aggregate_id,
                lease_token=lease_token,
            )
            session.commit()

        # The resident business-state scan stages the same pollable projection
        # every cycle. It must reopen CONSUMED without replacing the target's
        # durable future timestamp with the scan time.
        with session_factory() as session:
            restaged = stage_async_dispatch_for_actor(
                session,
                "run_agent_turn_sync",
                aggregate_id,
            )
            session.commit()
            assert restaged.status == AsyncDispatchStatus.PENDING
            next_poll_at = restaged.available_at

        early = run_async_dispatcher_once(
            enqueue=enqueue_async_dispatch,
            now=next_poll_at - timedelta(milliseconds=1),
            limit=10,
        )
        assert early.sent == 0

        due = run_async_dispatcher_once(
            enqueue=enqueue_async_dispatch,
            now=next_poll_at,
            limit=10,
        )
        assert due.sent == 1

        consumer.ack(message)
        next_message = next(consumer)
        assert next_message is not None
        assert next_message.actor_name == "run_async_dispatch"
        assert next_message.args == (dispatch_id, aggregate_id)
        consumer.ack(next_message)
        broker.join(workers.run_async_dispatch.queue_name, timeout=5_000)
    finally:
        consumer.close()


def test_redis_publish_failure_reconciles_from_postgres_before_resend(
    live_recovery_dependencies: tuple[RedisBroker, ModuleType],
) -> None:
    broker, workers = live_recovery_dependencies
    session_factory = get_session_factory()
    aggregate_id = "live-redis-outage-agent-turn"

    with session_factory() as session:
        dispatch = stage_async_dispatch_for_actor(session, "run_agent_turn_sync", aggregate_id)
        session.commit()
        dispatch_id = dispatch.id

    unavailable_broker = RedisBroker(url=f"redis://127.0.0.1:{_unused_tcp_port()}/{REDIS_TEST_DATABASE}")
    try:
        failed = run_async_dispatcher_once(
            enqueue=lambda current_dispatch_id, current_aggregate_id: unavailable_broker.enqueue(
                Message(
                    queue_name=workers.run_async_dispatch.queue_name,
                    actor_name="run_async_dispatch",
                    args=(current_dispatch_id, current_aggregate_id),
                    kwargs={},
                    options={},
                )
            ),
            limit=10,
        )
    finally:
        unavailable_broker.client.connection_pool.disconnect()

    assert failed.pending == 1
    assert failed.sent == 0
    with session_factory() as session:
        persisted = session.get(AsyncDispatch, dispatch_id)
        assert persisted is not None
        assert persisted.status.value == "sent"
        assert persisted.attempts == 1
        assert persisted.last_error
        assert persisted.sent_at is not None
        stale_at = persisted.sent_at + timedelta(minutes=6)

    recovered = run_async_dispatcher_once(
        enqueue=enqueue_async_dispatch,
        now=stale_at,
        limit=10,
    )
    assert recovered.reconciled >= 1
    assert recovered.sent == 1

    with session_factory() as session:
        persisted = session.get(AsyncDispatch, dispatch_id)
        assert persisted is not None
        assert persisted.status.value == "sent"
        assert persisted.attempts == 2

    consumer = broker.consume(workers.run_async_dispatch.queue_name, prefetch=1, timeout=5_000)
    try:
        message = next(consumer)
        assert message is not None
        assert message.actor_name == "run_async_dispatch"
        assert message.args == (dispatch_id, aggregate_id)
        consumer.ack(message)
        broker.join(workers.run_async_dispatch.queue_name, timeout=5_000)
    finally:
        consumer.close()


def test_redis_server_restart_preserves_async_dispatch_delivery(
    live_recovery_dependencies: tuple[RedisBroker, ModuleType],
) -> None:
    if os.getenv(LIVE_REDIS_RESTART_SWITCH) != "1":
        pytest.skip(f"set {LIVE_REDIS_RESTART_SWITCH}=1 to run the Redis server restart gate")

    broker, workers = live_recovery_dependencies
    session_factory = get_session_factory()
    aggregate_id = "live-redis-server-restart-agent-turn"

    with session_factory() as session:
        dispatch = stage_async_dispatch_for_actor(session, "run_agent_turn_sync", aggregate_id)
        session.commit()
        dispatch_id = dispatch.id

    sent = run_async_dispatcher_once(enqueue=enqueue_async_dispatch, limit=10)
    assert sent.sent == 1
    with session_factory() as session:
        persisted = session.get(AsyncDispatch, dispatch_id)
        assert persisted is not None
        assert persisted.status.value == "sent"
        assert persisted.attempts == 1

    _restart_redis_service()
    assert broker.client.ping() is True

    consumer = broker.consume(workers.run_async_dispatch.queue_name, prefetch=1, timeout=5_000)
    try:
        message = next(consumer)
        assert message is not None
        assert message.actor_name == "run_async_dispatch"
        assert message.args == (dispatch_id, aggregate_id)
        consumer.ack(message)
        broker.join(workers.run_async_dispatch.queue_name, timeout=5_000)
    finally:
        consumer.close()

    with session_factory() as session:
        lease_token = claim_async_dispatch_for_consumption(
            session,
            dispatch_id=dispatch_id,
            aggregate_id=aggregate_id,
        )
        assert lease_token is not None
        assert mark_async_dispatch_consumed(
            session,
            dispatch_id=dispatch_id,
            aggregate_id=aggregate_id,
            lease_token=lease_token,
        )
        session.commit()
        persisted = session.get(AsyncDispatch, dispatch_id)
        assert persisted is not None
        assert persisted.status.value == "consumed"


def test_dramatiq_worker_reconnects_after_redis_connection_interruption(
    live_recovery_dependencies: tuple[RedisBroker, ModuleType],
) -> None:
    if os.getenv(LIVE_REDIS_CONNECTION_SWITCH) != "1":
        pytest.skip(f"set {LIVE_REDIS_CONNECTION_SWITCH}=1 to run the Redis connection interruption gate")

    broker, _ = live_recovery_dependencies
    before_restart = Event()
    after_restart = Event()
    received: list[str] = []

    @dramatiq.actor(queue_name="default", max_retries=0)
    def reconnect_probe(marker: str) -> None:
        received.append(marker)
        if marker == "before-restart":
            before_restart.set()
        elif marker == "after-restart":
            after_restart.set()

    worker = Worker(broker, worker_threads=1, worker_timeout=100)
    worker.start()
    try:
        reconnect_probe.send("before-restart")
        assert before_restart.wait(timeout=10), f"worker did not consume the pre-restart message: {received!r}"

        _restart_redis_service()

        reconnect_probe.send("after-restart")
        assert after_restart.wait(timeout=15), f"worker did not recover after Redis restart: {received!r}"
    finally:
        worker.stop(timeout=15_000)

    assert received == ["before-restart", "after-restart"]


def test_resident_dispatcher_reconciles_and_republishes_stale_dispatch(
    live_recovery_dependencies: tuple[RedisBroker, ModuleType],
) -> None:
    if os.getenv(LIVE_DISPATCHER_WATCH_SWITCH) != "1":
        pytest.skip(f"set {LIVE_DISPATCHER_WATCH_SWITCH}=1 to run the resident dispatcher gate")

    broker, workers = live_recovery_dependencies
    session_factory = get_session_factory()
    aggregate_id = "live-resident-dispatcher-agent-turn"
    stale_sent_at = datetime.now(UTC) - timedelta(minutes=6)

    with session_factory() as session:
        dispatch = stage_async_dispatch_for_actor(session, "run_agent_turn_sync", aggregate_id)
        dispatch.status = AsyncDispatchStatus.SENT
        dispatch.attempts = 1
        dispatch.sent_at = stale_sent_at
        dispatch.lease_token = None
        dispatch.lease_expires_at = None
        dispatch.updated_at = stale_sent_at
        session.commit()
        dispatch_id = dispatch.id

    consumer = broker.consume(workers.run_async_dispatch.queue_name, prefetch=1, timeout=1_000)
    backend_dir = Path(__file__).resolve().parents[1]
    child = subprocess.Popen(
        [
            sys.executable,
            "-m",
            "productflow_backend.commands.run_async_dispatcher",
            "--watch",
            "--interval",
            "0.1",
            "--limit",
            "10",
        ],
        cwd=backend_dir,
        env=os.environ.copy(),
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
    )
    try:
        message = None
        deadline = time.monotonic() + 20
        while message is None and time.monotonic() < deadline:
            message = next(consumer)
            if message is None and child.poll() is not None:
                stdout, stderr = child.communicate(timeout=2)
                pytest.fail(
                    "resident dispatcher exited before publishing a recovered dispatch: "
                    f"returncode={child.returncode} stdout={stdout[-4000:]!r} stderr={stderr[-4000:]!r}"
                )
        if message is None:
            child.send_signal(signal.SIGTERM)
            child.wait(timeout=10)
            stdout, stderr = child.communicate(timeout=2)
            pytest.fail(
                "resident dispatcher did not publish a recovered dispatch: "
                f"returncode={child.returncode} stdout={stdout[-4000:]!r} stderr={stderr[-4000:]!r}"
            )
        assert message.actor_name == "run_async_dispatch"
        assert message.args == (dispatch_id, aggregate_id)
        consumer.ack(message)
        with session_factory() as session:
            persisted = session.get(AsyncDispatch, dispatch_id)
            assert persisted is not None
            assert persisted.status == AsyncDispatchStatus.SENT
            assert persisted.attempts == 2
        child.send_signal(signal.SIGTERM)
        child.wait(timeout=10)
        stdout, stderr = child.communicate(timeout=2)
        assert child.returncode == 0, f"resident dispatcher failed: stdout={stdout!r} stderr={stderr!r}"
    finally:
        if child.poll() is None:
            child.kill()
            child.wait(timeout=10)
        consumer.close()


def test_worker_termination_reconciles_consumer_lease_before_redelivery(
    live_recovery_dependencies: tuple[RedisBroker, ModuleType],
) -> None:
    broker, workers = live_recovery_dependencies
    session_factory = get_session_factory()
    aggregate_id = "live-worker-termination-agent-turn"

    with session_factory() as session:
        dispatch = stage_async_dispatch_for_actor(session, "run_agent_turn_sync", aggregate_id)
        session.commit()
        dispatch_id = dispatch.id

    sent = run_async_dispatcher_once(enqueue=enqueue_async_dispatch, limit=10)
    assert sent.sent == 1

    consumer = broker.consume(workers.run_async_dispatch.queue_name, prefetch=1, timeout=5_000)
    message = next(consumer)
    assert message is not None
    assert message.actor_name == "run_async_dispatch"
    assert message.args == (dispatch_id, aggregate_id)

    child_script = """
import sys
import time

import productflow_backend.workers as workers


def hold_target(_actor_name, _aggregate_id):
    time.sleep(120)


workers._execute_async_dispatch_target = hold_target
workers.execute_async_dispatch(sys.argv[1], sys.argv[2])
"""
    child = subprocess.Popen(
        [sys.executable, "-c", child_script, dispatch_id, aggregate_id],
        cwd=Path(__file__).resolve().parents[1],
        env=os.environ.copy(),
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
    )
    old_lease_token: str | None = None
    old_lease_expires_at = None
    try:
        deadline = time.monotonic() + 10
        while time.monotonic() < deadline:
            with session_factory() as session:
                persisted = session.get(AsyncDispatch, dispatch_id)
                if persisted is not None and persisted.lease_token is not None:
                    old_lease_token = persisted.lease_token
                    old_lease_expires_at = persisted.lease_expires_at
                    break
            if child.poll() is not None:
                stdout, stderr = child.communicate(timeout=2)
                pytest.fail(f"worker child exited before claiming dispatch: stdout={stdout!r} stderr={stderr!r}")
            time.sleep(0.05)
        assert old_lease_token is not None
        assert old_lease_expires_at is not None
        child.kill()
        child.wait(timeout=10)
    finally:
        if child.poll() is None:
            child.kill()
            child.wait(timeout=10)
        consumer.ack(message)
        broker.join(workers.run_async_dispatch.queue_name, timeout=5_000)
        consumer.close()

    with session_factory() as session:
        persisted = session.get(AsyncDispatch, dispatch_id)
        assert persisted is not None
        assert persisted.status.value == "sent"
        assert persisted.lease_token == old_lease_token

    recovered = run_async_dispatcher_once(
        enqueue=enqueue_async_dispatch,
        now=old_lease_expires_at + timedelta(seconds=1),
        limit=10,
    )
    assert recovered.reconciled >= 1
    assert recovered.sent == 1

    with session_factory() as session:
        assert (
            mark_async_dispatch_consumed(
                session,
                dispatch_id=dispatch_id,
                aggregate_id=aggregate_id,
                lease_token=old_lease_token,
            )
            is False
        )
        session.rollback()
        persisted = session.get(AsyncDispatch, dispatch_id)
        assert persisted is not None
        assert persisted.status.value == "sent"
        assert persisted.attempts == 2

    recovered_consumer = broker.consume(workers.run_async_dispatch.queue_name, prefetch=1, timeout=5_000)
    recovered_message = next(recovered_consumer)
    assert recovered_message is not None
    assert recovered_message.actor_name == "run_async_dispatch"
    assert recovered_message.args == (dispatch_id, aggregate_id)
    recovered_consumer.ack(recovered_message)
    broker.join(workers.run_async_dispatch.queue_name, timeout=5_000)
    recovered_consumer.close()

    with session_factory() as session:
        new_lease_token = claim_async_dispatch_for_consumption(
            session,
            dispatch_id=dispatch_id,
            aggregate_id=aggregate_id,
        )
        assert new_lease_token is not None
        assert mark_async_dispatch_consumed(
            session,
            dispatch_id=dispatch_id,
            aggregate_id=aggregate_id,
            lease_token=new_lease_token,
        )
        session.commit()
        persisted = session.get(AsyncDispatch, dispatch_id)
        assert persisted is not None
        assert persisted.status.value == "consumed"


def test_runtime_settings_fallback_preserves_postgres_transaction(
    live_recovery_dependencies: tuple[RedisBroker, ModuleType],
) -> None:
    session_factory = get_session_factory()

    with session_factory() as session:
        session.execute(text("SET LOCAL search_path TO pg_catalog"))

        get_runtime_settings(session)

        assert session.scalar(text("SELECT 1")) == 1
