from __future__ import annotations

import os
from collections.abc import Iterator
from concurrent.futures import ThreadPoolExecutor
from contextlib import contextmanager
from pathlib import Path
from threading import Barrier
from uuid import uuid4

import pytest
import sqlalchemy as sa
from sqlalchemy.engine import URL, make_url

from productflow_backend.application.async_delivery import (
    run_async_dispatcher_once,
    stage_async_dispatch,
)
from productflow_backend.config import get_settings
from productflow_backend.domain.enums import AsyncDispatchStatus
from productflow_backend.infrastructure.db.models import AsyncDispatch, Base
from productflow_backend.infrastructure.db.session import get_engine, get_session_factory

LIVE_ASYNC_DELIVERY_SWITCH = "PRODUCTFLOW_RUN_LIVE_ASYNC_DELIVERY"

pytestmark = [
    pytest.mark.live_dependencies,
    pytest.mark.skipif(
        os.getenv(LIVE_ASYNC_DELIVERY_SWITCH) != "1",
        reason=f"set {LIVE_ASYNC_DELIVERY_SWITCH}=1 to run the PostgreSQL async-delivery gate",
    ),
]


def _required_environment(name: str) -> str:
    value = os.getenv(name, "").strip()
    if not value:
        pytest.fail(f"{name} must be provided by the development environment", pytrace=False)
    return value


@contextmanager
def _temporary_postgres_database(base_database_url: str) -> Iterator[URL]:
    try:
        base_url = make_url(base_database_url)
    except Exception:
        pytest.fail("DATABASE_URL must be a valid SQLAlchemy URL", pytrace=False)
    if base_url.get_backend_name() != "postgresql":
        pytest.fail("DATABASE_URL must point to PostgreSQL for the live async-delivery gate", pytrace=False)

    database_name = f"productflow_live_async_delivery_{uuid4().hex}"
    maintenance_engine = sa.create_engine(
        base_url.set(database="postgres"),
        future=True,
        isolation_level="AUTOCOMMIT",
        pool_pre_ping=True,
    )
    quoted_name = maintenance_engine.dialect.identifier_preparer.quote(database_name)
    created = False
    try:
        with maintenance_engine.connect() as connection:
            connection.exec_driver_sql(f"CREATE DATABASE {quoted_name} TEMPLATE template0")
        created = True
        yield base_url.set(database=database_name)
    finally:
        try:
            if created:
                with maintenance_engine.connect() as connection:
                    connection.execute(
                        sa.text(
                            "SELECT pg_terminate_backend(pid) FROM pg_stat_activity "
                            "WHERE datname = :database_name AND pid <> pg_backend_pid()"
                        ),
                        {"database_name": database_name},
                    ).all()
                    connection.exec_driver_sql(f"DROP DATABASE IF EXISTS {quoted_name}")
        finally:
            maintenance_engine.dispose()


def _reset_database_state() -> None:
    cached_engine = get_engine() if get_engine.cache_info().currsize else None
    get_session_factory.cache_clear()
    try:
        if cached_engine is not None:
            cached_engine.dispose()
    finally:
        get_engine.cache_clear()
        get_settings.cache_clear()


@pytest.fixture()
def live_async_delivery_database(monkeypatch: pytest.MonkeyPatch, tmp_path: Path) -> Iterator[Path]:
    with _temporary_postgres_database(_required_environment("DATABASE_URL")) as database_url:
        with monkeypatch.context() as environment:
            environment.setenv("DATABASE_URL", database_url.render_as_string(hide_password=False))
            environment.setenv("STORAGE_ROOT", str(tmp_path / "storage"))
            environment.setenv("LOG_DIR", str(tmp_path / "logs"))
            _reset_database_state()
            try:
                Base.metadata.create_all(get_engine())
                yield tmp_path / "storage"
            finally:
                _reset_database_state()
        _reset_database_state()


def test_postgres_dispatcher_skip_locked_sends_each_pending_once(
    live_async_delivery_database: Path,
) -> None:
    del live_async_delivery_database
    session_factory = get_session_factory()
    with session_factory() as session:
        dispatch = stage_async_dispatch(
            session,
            delivery_key="delivery:live-skip-locked",
            actor_name="run_delivery_rendition_job",
            aggregate_id="job-live",
        )
        session.commit()
        dispatch_id = dispatch.id

    sent: list[tuple[str, str]] = []
    barrier = Barrier(2)

    def run_dispatcher() -> None:
        barrier.wait(timeout=5)
        run_async_dispatcher_once(
            enqueue=lambda did, aid: sent.append((did, aid)),
            limit=10,
        )

    with ThreadPoolExecutor(max_workers=2) as executor:
        futures = [executor.submit(run_dispatcher), executor.submit(run_dispatcher)]
        for future in futures:
            future.result(timeout=10)

    assert len(sent) == 1
    assert sent[0][0] == dispatch_id
    with session_factory() as session:
        persisted = session.get(AsyncDispatch, dispatch_id)
        assert persisted is not None
        assert persisted.status == AsyncDispatchStatus.SENT
