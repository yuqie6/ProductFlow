from __future__ import annotations

import os
import subprocess
from collections.abc import Iterator
from contextlib import contextmanager
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import pytest
import sqlalchemy as sa
from alembic.config import Config
from sqlalchemy.engine import URL, make_url

from alembic import command
from productflow_backend.application.agent.execution import (
    claim_agent_turn_execution,
    recover_expired_agent_turn_executions,
)
from productflow_backend.application.agent.sessions import create_agent_session
from productflow_backend.application.agent.turn_projection import reserve_agent_turn
from productflow_backend.application.time import now_utc
from productflow_backend.config import get_settings
from productflow_backend.domain.enums import AgentExecutionPhase, AgentTurnStatus
from productflow_backend.infrastructure.db.models import AgentTurnExecution, AgentTurnProjection
from productflow_backend.infrastructure.db.session import get_engine, get_session_factory

LIVE_AGENT_POSTGRES_RESTART_SWITCH = "PRODUCTFLOW_RUN_LIVE_AGENT_POSTGRES_RESTART"

pytestmark = [
    pytest.mark.live_dependencies,
    pytest.mark.skipif(
        os.getenv(LIVE_AGENT_POSTGRES_RESTART_SWITCH) != "1",
        reason=f"set {LIVE_AGENT_POSTGRES_RESTART_SWITCH}=1 to run the PostgreSQL server restart gate",
    ),
]


@contextmanager
def _temporary_postgres_database(base_database_url: str) -> Iterator[URL]:
    base_url = make_url(base_database_url)
    if base_url.get_backend_name() != "postgresql":
        pytest.fail("DATABASE_URL must point to PostgreSQL for the server restart gate", pytrace=False)

    database_name = f"productflow_live_agent_postgres_restart_{uuid4().hex}"
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
    if cached_engine is not None:
        cached_engine.dispose()
    get_engine.cache_clear()
    get_settings.cache_clear()


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


def _restart_postgres_service() -> None:
    try:
        _compose("restart", "productflow-postgres")
    finally:
        _compose("up", "-d", "--wait", "productflow-postgres")


def test_postgres_server_restart_preserves_agent_execution_recovery(
    monkeypatch: pytest.MonkeyPatch,
    tmp_path: Path,
) -> None:
    base_database_url = os.getenv("DATABASE_URL", "").strip()
    if not base_database_url:
        pytest.fail("DATABASE_URL must be provided by the development environment", pytrace=False)

    with _temporary_postgres_database(base_database_url) as database_url:
        try:
            with monkeypatch.context() as environment:
                environment.setenv("DATABASE_URL", database_url.render_as_string(hide_password=False))
                environment.setenv("STORAGE_ROOT", str(tmp_path / "storage"))
                _reset_database_state()

                backend_dir = Path(__file__).resolve().parents[1]
                config = Config(str(backend_dir / "alembic.ini"))
                config.set_main_option("script_location", str(backend_dir / "alembic"))
                command.upgrade(config, "head")

                session_factory = get_session_factory()
                with session_factory() as session:
                    agent_session = create_agent_session(session, title="PostgreSQL restart recovery")
                    conversation = agent_session.conversations[0]
                    reservation = reserve_agent_turn(
                        session,
                        product_id=None,
                        conversation_id=conversation.id,
                        input_text="验证 PostgreSQL server restart 后的 Agent recovery",
                        input_asset_ids=[],
                        idempotency_key=f"live-postgres-restart-{uuid4().hex}",
                    )
                    lease = claim_agent_turn_execution(
                        session,
                        conversation_id=conversation.id,
                        task_id=None,
                        idempotency_key=reservation.projection.idempotency_key,
                        harness_turn_id="live-postgres-restart-turn",
                        owner_id="live-postgres-restart-owner",
                        lease_seconds=60,
                    )
                    execution = session.get(AgentTurnExecution, lease.execution_id)
                    assert execution is not None
                    old_fencing_token = execution.fencing_token
                    execution.phase = AgentExecutionPhase.MODEL
                    execution.lease_expires_at = now_utc() - timedelta(seconds=1)
                    session.commit()
                    projection_id = reservation.projection.id

                # 把活连接还回 SQLAlchemy 池，让下一次 session 必须证明
                # pool_pre_ping 能丢掉被重启杀掉的连接。
                with session_factory() as session:
                    assert session.scalar(sa.text("SELECT 1")) == 1

                _restart_postgres_service()

                with session_factory() as session:
                    assert session.scalar(sa.text("SELECT 1")) == 1
                    projection = session.get(AgentTurnProjection, projection_id)
                    assert projection is not None
                    assert projection.status == AgentTurnStatus.QUEUED

                    recovery = recover_expired_agent_turn_executions(session)
                    assert recovery.unknown == 1
                    session.refresh(projection)
                    assert projection.status == AgentTurnStatus.UNKNOWN

                    recovered_execution = session.get(AgentTurnExecution, lease.execution_id)
                    assert recovered_execution is not None
                    assert recovered_execution.owner_id is None
                    assert recovered_execution.fencing_token == old_fencing_token + 1
        finally:
            _reset_database_state()
