from __future__ import annotations

import importlib
import os
import sys
from collections.abc import Iterator
from contextlib import contextmanager
from pathlib import Path
from types import ModuleType
from urllib.parse import parse_qsl, urlencode, urlsplit, urlunsplit
from uuid import uuid4

import pytest
from dramatiq.brokers.redis import RedisBroker
from sqlalchemy import create_engine, select, text
from sqlalchemy.engine import URL, make_url

from productflow_backend.application.durable_recovery import recover_unfinished_workflow_runs
from productflow_backend.application.runtime_settings import get_runtime_settings
from productflow_backend.config import get_settings
from productflow_backend.domain.enums import WorkflowNodeStatus, WorkflowNodeType, WorkflowRunStatus
from productflow_backend.infrastructure.db.models import (
    Base,
    Product,
    ProductWorkflow,
    WorkflowNode,
    WorkflowNodeRun,
    WorkflowRun,
)
from productflow_backend.infrastructure.db.session import get_engine, get_session_factory
from productflow_backend.infrastructure.queue import enqueue_workflow_run, get_broker

LIVE_RECOVERY_SWITCH = "PRODUCTFLOW_RUN_LIVE_RECOVERY"
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
                assert workers.run_product_workflow_run.broker is broker
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
        workflow = ProductWorkflow(product=product, title="Live recovery gate workflow")
        node = WorkflowNode(
            workflow=workflow,
            node_type=WorkflowNodeType.PROMPT_GENERATION,
            title="Queued prompt node",
            status=WorkflowNodeStatus.QUEUED,
        )
        workflow_run = WorkflowRun(workflow=workflow, status=WorkflowRunStatus.RUNNING)
        node_run = WorkflowNodeRun(
            workflow_run=workflow_run,
            node=node,
            status=WorkflowNodeStatus.QUEUED,
        )
        session.add(product)
        session.commit()
        run_id = workflow_run.id
        node_run_id = node_run.id

    summary = recover_unfinished_workflow_runs(enqueue=enqueue_workflow_run)

    assert summary.queued_runs == 1
    assert summary.stale_running_runs == 0
    assert summary.enqueued_runs == 1

    with session_factory() as session:
        persisted_run = session.get(WorkflowRun, run_id)
        persisted_node_run = session.get(WorkflowNodeRun, node_run_id)
        active_run_ids = set(
            session.scalars(select(WorkflowRun.id).where(WorkflowRun.status == WorkflowRunStatus.RUNNING)).all()
        )
        assert persisted_run is not None
        assert persisted_run.status == WorkflowRunStatus.RUNNING
        assert persisted_node_run is not None
        assert persisted_node_run.status == WorkflowNodeStatus.QUEUED
        assert active_run_ids == {run_id}

    consumer = broker.consume(workers.run_product_workflow_run.queue_name, prefetch=1, timeout=5_000)
    try:
        message = next(consumer)
        assert message is not None
        assert message.actor_name == "run_product_workflow_run"
        assert message.args == (run_id,)
        assert message.kwargs == {}
        consumer.ack(message)
        broker.join(workers.run_product_workflow_run.queue_name, timeout=5_000)
    finally:
        consumer.close()


def test_runtime_settings_fallback_preserves_postgres_transaction(
    live_recovery_dependencies: tuple[RedisBroker, ModuleType],
) -> None:
    session_factory = get_session_factory()

    with session_factory() as session:
        session.execute(text("SET LOCAL search_path TO pg_catalog"))

        get_runtime_settings(session)

        assert session.scalar(text("SELECT 1")) == 1
