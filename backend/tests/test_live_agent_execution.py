from __future__ import annotations

import os
from collections.abc import Iterator
from contextlib import contextmanager
from datetime import timedelta
from pathlib import Path
from uuid import uuid4

import pytest
import sqlalchemy as sa
from alembic.config import Config
from sqlalchemy.engine import URL, make_url
from test_agent_sessions import _create_workspace

from alembic import command
from productflow_backend.application.agent_conversations import reserve_agent_turn
from productflow_backend.application.agent_execution import (
    append_agent_turn_checkpoint,
    claim_agent_turn_execution,
    heartbeat_agent_turn_execution,
    recover_expired_agent_turn_executions,
)
from productflow_backend.application.time import now_utc
from productflow_backend.config import get_settings
from productflow_backend.domain.enums import AgentCheckpointKind, AgentExecutionPhase, AgentTurnStatus
from productflow_backend.domain.errors import ConflictError
from productflow_backend.infrastructure.db.models import AgentTurnCheckpoint, AgentTurnExecution, AgentTurnProjection
from productflow_backend.infrastructure.db.session import get_engine, get_session_factory

LIVE_AGENT_EXECUTION_SWITCH = "PRODUCTFLOW_RUN_LIVE_AGENT_EXECUTION"

pytestmark = [
    pytest.mark.live_dependencies,
    pytest.mark.skipif(
        os.getenv(LIVE_AGENT_EXECUTION_SWITCH) != "1",
        reason=f"set {LIVE_AGENT_EXECUTION_SWITCH}=1 to run the PostgreSQL Agent execution gate",
    ),
]


@contextmanager
def _temporary_postgres_database(base_database_url: str) -> Iterator[URL]:
    base_url = make_url(base_database_url)
    if base_url.get_backend_name() != "postgresql":
        pytest.fail("DATABASE_URL must point to PostgreSQL for the live Agent execution gate", pytrace=False)

    database_name = f"productflow_live_agent_execution_{uuid4().hex}"
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


def test_agent_execution_lease_and_recovery_on_postgresql(
    monkeypatch: pytest.MonkeyPatch,
    tmp_path: Path,
) -> None:
    base_database_url = os.getenv("DATABASE_URL", "").strip()
    if not base_database_url:
        pytest.fail("DATABASE_URL must be provided by the development environment", pytrace=False)

    with _temporary_postgres_database(base_database_url) as database_url:
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
                workspace = _create_workspace(session, key=f"live-agent-execution-{uuid4().hex}")
                reservation = reserve_agent_turn(
                    session,
                    product_id=workspace.product.id,
                    conversation_id=workspace.conversation.id,
                    input_text="验证 PostgreSQL execution lease",
                    input_asset_ids=[],
                    idempotency_key=f"live-execution-turn-{uuid4().hex}",
                )
                lease = claim_agent_turn_execution(
                    session,
                    conversation_id=workspace.conversation.id,
                    task_id=None,
                    idempotency_key=reservation.projection.idempotency_key,
                    harness_turn_id="live-harness-turn",
                    owner_id="live-agent-owner-a",
                    lease_seconds=5,
                )
                same_owner = claim_agent_turn_execution(
                    session,
                    conversation_id=workspace.conversation.id,
                    task_id=None,
                    idempotency_key=reservation.projection.idempotency_key,
                    harness_turn_id="live-harness-turn",
                    owner_id="live-agent-owner-a",
                    lease_seconds=5,
                )
                assert same_owner == lease
                with pytest.raises(ConflictError):
                    claim_agent_turn_execution(
                        session,
                        conversation_id=workspace.conversation.id,
                        task_id=None,
                        idempotency_key=reservation.projection.idempotency_key,
                        harness_turn_id="live-harness-turn",
                        owner_id="live-agent-owner-b",
                        lease_seconds=5,
                    )
                checkpoint = append_agent_turn_checkpoint(
                    session,
                    conversation_id=workspace.conversation.id,
                    execution_id=lease.execution_id,
                    owner_id=lease.owner_id,
                    lease_token=lease.lease_token,
                    sequence=1,
                    kind=AgentCheckpointKind.BEFORE_MODEL_REQUEST,
                    payload={"source": "live-test"},
                )
                refreshed = heartbeat_agent_turn_execution(
                    session,
                    conversation_id=workspace.conversation.id,
                    execution_id=lease.execution_id,
                    owner_id=lease.owner_id,
                    lease_token=lease.lease_token,
                    phase=AgentExecutionPhase.MODEL,
                    lease_seconds=5,
                )
                assert checkpoint.fencing_token == refreshed.fencing_token
                assert refreshed.phase == AgentExecutionPhase.MODEL

                recovery_workspace = _create_workspace(session, key=f"live-agent-recovery-{uuid4().hex}")
                recovery_reservation = reserve_agent_turn(
                    session,
                    product_id=recovery_workspace.product.id,
                    conversation_id=recovery_workspace.conversation.id,
                    input_text="验证过期 execution recovery",
                    input_asset_ids=[],
                    idempotency_key=f"live-recovery-turn-{uuid4().hex}",
                )
                recovery_lease = claim_agent_turn_execution(
                    session,
                    conversation_id=recovery_workspace.conversation.id,
                    task_id=None,
                    idempotency_key=recovery_reservation.projection.idempotency_key,
                    harness_turn_id="live-recovery-harness-turn",
                    owner_id="live-recovery-owner",
                    lease_seconds=5,
                )
                execution = session.get(AgentTurnExecution, recovery_lease.execution_id)
                assert execution is not None
                old_fencing_token = execution.fencing_token
                execution.phase = AgentExecutionPhase.TOOL
                execution.lease_expires_at = now_utc() - timedelta(seconds=1)
                session.commit()

                summary = recover_expired_agent_turn_executions(session)
                assert summary.unknown == 1
                recovered = session.get(AgentTurnProjection, recovery_reservation.projection.id)
                assert recovered is not None
                assert recovered.status == AgentTurnStatus.UNKNOWN
                recovery_checkpoint = session.scalar(
                    sa.select(AgentTurnCheckpoint).where(
                        AgentTurnCheckpoint.execution_id == recovery_lease.execution_id
                    )
                )
                assert recovery_checkpoint is not None
                assert recovery_checkpoint.kind == AgentCheckpointKind.TERMINAL
                assert recovery_checkpoint.fencing_token == old_fencing_token + 1

            _reset_database_state()
