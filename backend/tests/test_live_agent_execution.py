from __future__ import annotations

import json
import os
import shutil
import socket
import subprocess
import sys
import threading
import time
from collections.abc import Iterator
from concurrent.futures import ThreadPoolExecutor
from contextlib import contextmanager
from datetime import timedelta
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from threading import Barrier
from urllib.parse import urlsplit
from uuid import uuid4

import httpx
import pytest
import sqlalchemy as sa
import uvicorn
from alembic.config import Config
from sqlalchemy.engine import URL, make_url
from test_agent_sessions import _create_workspace

from alembic import command
from productflow_backend.application.agent_control import answer_agent_question
from productflow_backend.application.agent_conversations import project_agent_turn_state, reserve_agent_turn
from productflow_backend.application.agent_execution import (
    append_agent_turn_checkpoint,
    append_agent_turn_event,
    claim_agent_turn_execution,
    heartbeat_agent_turn_execution,
    recover_expired_agent_turn_executions,
    release_agent_turn_execution,
)
from productflow_backend.application.agent_sessions import create_agent_session
from productflow_backend.application.agent_sync import execute_agent_turn_sync
from productflow_backend.application.async_delivery import run_async_dispatcher_once, stage_async_dispatch_for_actor
from productflow_backend.application.time import now_utc
from productflow_backend.config import get_settings
from productflow_backend.domain.enums import AgentCheckpointKind, AgentExecutionPhase, AgentTurnStatus
from productflow_backend.domain.errors import ConflictError
from productflow_backend.infrastructure.agent_service import AgentServiceClient
from productflow_backend.infrastructure.db.models import (
    AgentTurnCheckpoint,
    AgentTurnEvent,
    AgentTurnExecution,
    AgentTurnProjection,
    AsyncDispatch,
)
from productflow_backend.infrastructure.db.session import get_engine, get_session_factory
from productflow_backend.infrastructure.provider_config import (
    AGENT_PURPOSE,
    CAPABILITY_TEXT_RESPONSES,
    create_provider_profile,
    update_provider_binding,
)
from productflow_backend.presentation.api import create_app
from productflow_backend.presentation.routes import agent_internal as agent_internal_routes

LIVE_AGENT_EXECUTION_SWITCH = "PRODUCTFLOW_RUN_LIVE_AGENT_EXECUTION"
LIVE_AGENT_QUESTION_POSTGRES_RESTART_SWITCH = "PRODUCTFLOW_RUN_LIVE_AGENT_QUESTION_POSTGRES_RESTART"
LIVE_AGENT_WORKER_EFFECTS_SWITCH = "PRODUCTFLOW_RUN_LIVE_AGENT_WORKER_EFFECTS"

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


def _terminate_postgres_connections(database_url: URL) -> int:
    database_name = database_url.database
    if not database_name:
        pytest.fail("temporary PostgreSQL database name is required", pytrace=False)
    maintenance_engine = sa.create_engine(
        database_url.set(database="postgres"),
        future=True,
        isolation_level="AUTOCOMMIT",
        pool_pre_ping=True,
    )
    try:
        with maintenance_engine.connect() as connection:
            terminated = connection.execute(
                sa.text(
                    "SELECT pg_terminate_backend(pid) FROM pg_stat_activity "
                    "WHERE datname = :database_name AND pid <> pg_backend_pid()"
                ),
                {"database_name": database_name},
            ).all()
            return len(terminated)
    finally:
        maintenance_engine.dispose()


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

                concurrent_workspace = _create_workspace(session, key=f"live-agent-concurrent-{uuid4().hex}")
                concurrent_reservation = reserve_agent_turn(
                    session,
                    product_id=concurrent_workspace.product.id,
                    conversation_id=concurrent_workspace.conversation.id,
                    input_text="验证 PostgreSQL 并发 execution claim",
                    input_asset_ids=[],
                    idempotency_key=f"live-concurrent-turn-{uuid4().hex}",
                )
                concurrent_projection_id = concurrent_reservation.projection.id
                concurrent_key = concurrent_reservation.projection.idempotency_key
                concurrent_conversation_id = concurrent_workspace.conversation.id
                concurrent_run_id = concurrent_workspace.conversation.harness_run_id
                barrier = Barrier(2)

                def concurrent_claim(owner_id: str) -> tuple[str, str, str | None]:
                    with session_factory() as contender:
                        barrier.wait(timeout=10)
                        try:
                            lease = claim_agent_turn_execution(
                                contender,
                                conversation_id=concurrent_conversation_id,
                                task_id=None,
                                idempotency_key=concurrent_key,
                                harness_turn_id="live-concurrent-harness-turn",
                                owner_id=owner_id,
                                lease_seconds=5,
                            )
                        except ConflictError as exc:
                            return ("conflict", owner_id, str(exc))
                        return ("claimed", owner_id, lease.lease_token)

                with ThreadPoolExecutor(max_workers=2, thread_name_prefix="live-agent-claim") as executor:
                    outcomes = list(
                        executor.map(
                            concurrent_claim,
                            ["live-concurrent-owner-a", "live-concurrent-owner-b"],
                        )
                    )
                assert sorted(outcome[0] for outcome in outcomes) == ["claimed", "conflict"]
                claimed_outcome = next(outcome for outcome in outcomes if outcome[0] == "claimed")
                assert claimed_outcome[2]
                with session_factory() as verification:
                    concurrent_execution = verification.scalar(
                        sa.select(AgentTurnExecution).where(
                            AgentTurnExecution.turn_projection_id == concurrent_projection_id
                        )
                    )
                    assert concurrent_execution is not None
                    assert concurrent_execution.owner_id == claimed_outcome[1]
                    assert concurrent_execution.attempt == 1
                    assert concurrent_execution.fencing_token == 1
                    assert concurrent_execution.harness_turn_id == "live-concurrent-harness-turn"
                    concurrent_execution_id = concurrent_execution.id
                    concurrent_owner_id = concurrent_execution.owner_id
                    concurrent_lease_token = concurrent_execution.lease_token
                assert concurrent_owner_id is not None
                assert concurrent_lease_token is not None
                event_barrier = Barrier(2)

                def concurrent_event(writer: str) -> tuple[str, str, str | None]:
                    with session_factory() as contender:
                        event_barrier.wait(timeout=10)
                        try:
                            append_agent_turn_event(
                                contender,
                                conversation_id=concurrent_conversation_id,
                                execution_id=concurrent_execution_id,
                                owner_id=concurrent_owner_id,
                                lease_token=concurrent_lease_token,
                                sequence=1,
                                schema_version=1,
                                run_id=concurrent_run_id,
                                turn_id="live-concurrent-harness-turn",
                                kind="turn.started",
                                payload={"status": "running", "writer": writer},
                                created_at=now_utc(),
                            )
                        except ConflictError as exc:
                            return ("conflict", writer, str(exc))
                        return ("appended", writer, None)

                with ThreadPoolExecutor(max_workers=2, thread_name_prefix="live-agent-event") as executor:
                    event_outcomes = list(executor.map(concurrent_event, ["writer-a", "writer-b"]))
                assert sorted(outcome[0] for outcome in event_outcomes) == ["appended", "conflict"]
                with session_factory() as verification:
                    concurrent_events = list(
                        verification.scalars(
                            sa.select(AgentTurnEvent).where(
                                AgentTurnEvent.turn_projection_id == concurrent_projection_id,
                                AgentTurnEvent.sequence == 1,
                            )
                        ).all()
                    )
                    assert len(concurrent_events) == 1
                    assert concurrent_events[0].payload_json["writer"] in {"writer-a", "writer-b"}

                queued_event = append_agent_turn_event(
                    session,
                    conversation_id=workspace.conversation.id,
                    execution_id=lease.execution_id,
                    owner_id=lease.owner_id,
                    lease_token=lease.lease_token,
                    sequence=1,
                    schema_version=1,
                    run_id=workspace.conversation.harness_run_id,
                    turn_id=lease.harness_turn_id,
                    kind="turn.queued",
                    payload={"status": "queued"},
                    created_at=now_utc(),
                )
                started_event = append_agent_turn_event(
                    session,
                    conversation_id=workspace.conversation.id,
                    execution_id=lease.execution_id,
                    owner_id=lease.owner_id,
                    lease_token=lease.lease_token,
                    sequence=2,
                    schema_version=1,
                    run_id=workspace.conversation.harness_run_id,
                    turn_id=lease.harness_turn_id,
                    kind="turn.started",
                    payload={"status": "running"},
                    created_at=now_utc(),
                )
                assert queued_event.sequence == 1
                assert started_event.sequence == 2
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

                cancel_workspace = _create_workspace(session, key=f"live-agent-cancel-{uuid4().hex}")
                cancel_reservation = reserve_agent_turn(
                    session,
                    product_id=cancel_workspace.product.id,
                    conversation_id=cancel_workspace.conversation.id,
                    input_text="验证 queued Turn 取消事件",
                    input_asset_ids=[],
                    idempotency_key=f"live-cancel-turn-{uuid4().hex}",
                )
                cancel_lease = claim_agent_turn_execution(
                    session,
                    conversation_id=cancel_workspace.conversation.id,
                    task_id=None,
                    idempotency_key=cancel_reservation.projection.idempotency_key,
                    harness_turn_id="live-cancel-harness-turn",
                    owner_id="live-cancel-owner",
                    lease_seconds=5,
                )
                append_agent_turn_event(
                    session,
                    conversation_id=cancel_workspace.conversation.id,
                    execution_id=cancel_lease.execution_id,
                    owner_id=cancel_lease.owner_id,
                    lease_token=cancel_lease.lease_token,
                    sequence=1,
                    schema_version=1,
                    run_id=cancel_workspace.conversation.harness_run_id,
                    turn_id=cancel_lease.harness_turn_id,
                    kind="turn.queued",
                    payload={"status": "queued"},
                    created_at=now_utc(),
                )
                canceled_event = append_agent_turn_event(
                    session,
                    conversation_id=cancel_workspace.conversation.id,
                    execution_id=cancel_lease.execution_id,
                    owner_id=cancel_lease.owner_id,
                    lease_token=cancel_lease.lease_token,
                    sequence=2,
                    schema_version=1,
                    run_id=cancel_workspace.conversation.harness_run_id,
                    turn_id=cancel_lease.harness_turn_id,
                    kind="turn.canceled",
                    payload={"status": "canceled", "output": ""},
                    created_at=now_utc(),
                )
                canceled_checkpoint = append_agent_turn_checkpoint(
                    session,
                    conversation_id=cancel_workspace.conversation.id,
                    execution_id=cancel_lease.execution_id,
                    owner_id=cancel_lease.owner_id,
                    lease_token=cancel_lease.lease_token,
                    sequence=1,
                    kind=AgentCheckpointKind.TERMINAL,
                    payload={"status": "canceled"},
                )
                assert canceled_event.sequence == 2
                assert canceled_checkpoint.kind == AgentCheckpointKind.TERMINAL
                assert release_agent_turn_execution(
                    session,
                    conversation_id=cancel_workspace.conversation.id,
                    execution_id=cancel_lease.execution_id,
                    owner_id=cancel_lease.owner_id,
                    lease_token=cancel_lease.lease_token,
                    phase=AgentExecutionPhase.TERMINAL,
                )

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
                recovery_event = session.scalar(
                    sa.select(AgentTurnEvent)
                    .where(AgentTurnEvent.turn_projection_id == recovery_reservation.projection.id)
                    .order_by(AgentTurnEvent.sequence.desc())
                )
                assert recovery_event is not None
                assert recovery_event.kind == "turn.unknown"
                assert recovery_event.fencing_token == old_fencing_token + 1

            _reset_database_state()


def test_agent_execution_handoffs_between_real_agent_processes_on_postgresql(
    monkeypatch: pytest.MonkeyPatch,
    tmp_path: Path,
) -> None:
    base_database_url = os.getenv("DATABASE_URL", "").strip()
    if not base_database_url:
        pytest.fail("DATABASE_URL must be provided by the development environment", pytrace=False)

    node = shutil.which("node")
    if node is None:
        pytest.fail("node is required for the real Agent process handoff gate", pytrace=False)

    token = "live-agent-process-handoff-token-0123456789"
    api_port = _free_tcp_port()
    provider_port = _free_tcp_port()
    productflow_url = f"http://127.0.0.1:{api_port}"
    provider_url = f"http://127.0.0.1:{provider_port}/v1"
    claim_entered = threading.Event()
    allow_claim_response = threading.Event()
    api_server: uvicorn.Server | None = None
    api_thread: threading.Thread | None = None
    provider_server: ThreadingHTTPServer | None = None
    provider_thread: threading.Thread | None = None
    first_process: subprocess.Popen[str] | None = None
    second_process: subprocess.Popen[str] | None = None
    process_output: dict[str, list[str]] = {"first": [], "second": []}
    start_thread: threading.Thread | None = None

    with _temporary_postgres_database(base_database_url) as database_url:
        try:
            with monkeypatch.context() as environment:
                environment.setenv("DATABASE_URL", database_url.render_as_string(hide_password=False))
                environment.setenv("REDIS_URL", os.getenv("REDIS_URL", "redis://127.0.0.1:6379/9"))
                environment.setenv("STORAGE_ROOT", str(tmp_path / "storage"))
                environment.setenv("ADMIN_ACCESS_KEY", "live-agent-process-admin-key")
                environment.setenv("SESSION_SECRET", "live-agent-process-session-secret-0123456789")
                environment.setenv("AGENT_SERVICE_INTERNAL_TOKEN", token)
                environment.setenv("AGENT_SERVICE_BASE_URL", productflow_url)
                _reset_database_state()
                backend_dir = Path(__file__).resolve().parents[1]
                config = Config(str(backend_dir / "alembic.ini"))
                config.set_main_option("script_location", str(backend_dir / "alembic"))
                command.upgrade(config, "head")

                provider_server = ThreadingHTTPServer(("127.0.0.1", provider_port), _FailingProviderHandler)
                provider_thread = threading.Thread(
                    target=provider_server.serve_forever,
                    name="live-agent-provider",
                    daemon=True,
                )
                provider_thread.start()

                session_factory = get_session_factory()
                with session_factory() as session:
                    profile = create_provider_profile(
                        session,
                        name="live-agent-process-provider",
                        base_url=provider_url,
                        api_key="live-agent-provider-key",
                        capabilities=[CAPABILITY_TEXT_RESPONSES],
                        default_models={"agent_model": "live-failing-model"},
                    )
                    update_provider_binding(
                        session,
                        purpose=AGENT_PURPOSE,
                        provider_kind="openai",
                        provider_profile_id=profile.id,
                        model_settings={"model": "live-failing-model"},
                        config={},
                    )
                    workspace = _create_workspace(session, key=f"live-agent-process-{uuid4().hex}")
                    reservation = reserve_agent_turn(
                        session,
                        product_id=workspace.product.id,
                        conversation_id=workspace.conversation.id,
                        input_text="验证真实 Agent 进程 handoff",
                        input_asset_ids=[],
                        idempotency_key=f"live-process-handoff-{uuid4().hex}",
                    )
                    projection_id = reservation.projection.id
                    idempotency_key = reservation.projection.idempotency_key
                    conversation_id = workspace.conversation.id

                original_claim = agent_internal_routes.claim_agent_turn_execution

                def block_claim_response(*args, **kwargs):
                    lease = original_claim(*args, **kwargs)
                    claim_entered.set()
                    if not allow_claim_response.wait(timeout=15):
                        raise RuntimeError("test claim response release timed out")
                    return lease

                monkeypatch.setattr(
                    agent_internal_routes,
                    "claim_agent_turn_execution",
                    block_claim_response,
                )
                app = create_app()
                api_config = uvicorn.Config(
                    app,
                    host="127.0.0.1",
                    port=api_port,
                    log_level="error",
                )
                api_server = uvicorn.Server(api_config)
                api_thread = threading.Thread(target=api_server.run, name="live-agent-productflow", daemon=True)
                api_thread.start()
                _wait_for_http_health(productflow_url + "/healthz")

                agent_dir = Path(__file__).resolve().parents[2] / "agent-service"
                first_process, first_port = _spawn_live_agent_process(
                    node=node,
                    agent_dir=agent_dir,
                    data_root=tmp_path / "agent-a",
                    productflow_url=productflow_url,
                    token=token,
                    output=process_output["first"],
                )
                _wait_for_http_health(_agent_health_url(first_port))
                start_thread = threading.Thread(
                    target=_start_agent_turn,
                    args=(
                        first_port,
                        conversation_id,
                        {
                            "input_text": "验证真实 Agent 进程 handoff",
                            "asset_ids": [],
                            "idempotency_key": idempotency_key,
                        },
                    ),
                    name="live-agent-start",
                    daemon=True,
                )
                start_thread.start()

                assert claim_entered.wait(timeout=15), "first Agent process did not reach the committed claim window"
                with session_factory() as session:
                    claimed_projection = session.get(AgentTurnProjection, projection_id)
                    assert claimed_projection is not None
                    assert claimed_projection.harness_turn_id
                    harness_turn_id = claimed_projection.harness_turn_id
                    claimed_execution = session.scalar(
                        sa.select(AgentTurnExecution).where(
                            AgentTurnExecution.turn_projection_id == projection_id
                        )
                    )
                    assert claimed_execution is not None
                    assert claimed_execution.owner_id
                    first_owner_id = claimed_execution.owner_id
                    assert claimed_execution.lease_token
                    first_lease_token = claimed_execution.lease_token
                    assert claimed_execution.attempt == 1
                    assert claimed_execution.fencing_token == 1
                    assert claimed_execution.phase == AgentExecutionPhase.CLAIMED

                first_process.kill()
                first_process.wait(timeout=15)
                allow_claim_response.set()
                if start_thread is not None:
                    start_thread.join(timeout=15)
                    assert not start_thread.is_alive()

                with session_factory() as session:
                    recovery = recover_expired_agent_turn_executions(
                        session,
                        now=now_utc() + timedelta(minutes=2),
                    )
                    assert recovery.requeued == 1
                    recovered_projection = session.get(AgentTurnProjection, projection_id)
                    assert recovered_projection is not None
                    assert recovered_projection.status == AgentTurnStatus.QUEUED
                    assert recovered_projection.harness_turn_id == harness_turn_id
                    recovered_execution = session.scalar(
                        sa.select(AgentTurnExecution).where(
                            AgentTurnExecution.turn_projection_id == projection_id
                        )
                    )
                    assert recovered_execution is not None
                    assert recovered_execution.owner_id is None
                    assert recovered_execution.phase == AgentExecutionPhase.CLAIMED
                    assert recovered_execution.attempt == 1
                    assert recovered_execution.fencing_token == 2

                second_process, second_port = _spawn_live_agent_process(
                    node=node,
                    agent_dir=agent_dir,
                    data_root=tmp_path / "agent-b",
                    productflow_url=productflow_url,
                    token=token,
                    output=process_output["second"],
                )
                _wait_for_http_health(_agent_health_url(second_port))
                second_client = AgentServiceClient(
                    base_url=f"http://127.0.0.1:{second_port}",
                    internal_token=token,
                    connect_timeout_seconds=2,
                    read_timeout_seconds=10,
                )
                execute_agent_turn_sync(
                    projection_id,
                    gateway=second_client,
                    enqueue_later=lambda *_args: None,
                )

                def second_attempt_observed() -> bool:
                    with session_factory() as session:
                        execution = session.scalar(
                            sa.select(AgentTurnExecution).where(
                                AgentTurnExecution.turn_projection_id == projection_id
                            )
                        )
                        checkpoint = session.scalar(
                            sa.select(AgentTurnCheckpoint)
                            .where(
                                AgentTurnCheckpoint.turn_projection_id == projection_id,
                                AgentTurnCheckpoint.attempt == 2,
                                AgentTurnCheckpoint.kind == AgentCheckpointKind.BEFORE_MODEL_REQUEST,
                            )
                            .limit(1)
                        )
                        return execution is not None and execution.attempt == 2 and checkpoint is not None

                try:
                    _wait_for_condition(second_attempt_observed, timeout=20)
                except AssertionError as exc:
                    with session_factory() as session:
                        diagnostic_execution = session.scalar(
                            sa.select(AgentTurnExecution).where(
                                AgentTurnExecution.turn_projection_id == projection_id
                            )
                        )
                        diagnostic_checkpoints = list(
                            session.scalars(
                                sa.select(AgentTurnCheckpoint)
                                .where(AgentTurnCheckpoint.turn_projection_id == projection_id)
                                .order_by(AgentTurnCheckpoint.attempt, AgentTurnCheckpoint.sequence)
                            ).all()
                        )
                    diagnostic_state = second_client.get_turn(
                        conversation_id=conversation_id,
                        turn_id=harness_turn_id,
                    )
                    diagnostic_health = httpx.get(_agent_health_url(second_port), timeout=2).json()
                    raise AssertionError(
                        "real Agent handoff did not reach attempt 2: "
                        f"execution={diagnostic_execution!r} "
                        f"checkpoints={diagnostic_checkpoints!r} "
                        f"state={diagnostic_state!r} health={diagnostic_health!r} "
                        f"second_output={''.join(process_output['second'])}"
                    ) from exc
                with session_factory() as stale_session:
                    with pytest.raises(ConflictError):
                        heartbeat_agent_turn_execution(
                            stale_session,
                            conversation_id=conversation_id,
                            execution_id=claimed_execution.id,
                            owner_id=first_owner_id,
                            lease_token=first_lease_token,
                            phase=AgentExecutionPhase.MODEL,
                            lease_seconds=5,
                        )
                    with pytest.raises(ConflictError):
                        append_agent_turn_event(
                            stale_session,
                            conversation_id=conversation_id,
                            execution_id=claimed_execution.id,
                            owner_id=first_owner_id,
                            lease_token=first_lease_token,
                            sequence=99,
                            schema_version=1,
                            run_id=workspace.conversation.harness_run_id,
                            turn_id=harness_turn_id,
                            kind="turn.started",
                            payload={"status": "running"},
                            created_at=now_utc(),
                        )
                    with pytest.raises(ConflictError):
                        append_agent_turn_checkpoint(
                            stale_session,
                            conversation_id=conversation_id,
                            execution_id=claimed_execution.id,
                            owner_id=first_owner_id,
                            lease_token=first_lease_token,
                            sequence=99,
                            kind=AgentCheckpointKind.BEFORE_MODEL_REQUEST,
                            payload={"source": "stale-owner"},
                        )
                    assert (
                        release_agent_turn_execution(
                            stale_session,
                            conversation_id=conversation_id,
                            execution_id=claimed_execution.id,
                            owner_id=first_owner_id,
                            lease_token=first_lease_token,
                            phase=AgentExecutionPhase.TERMINAL,
                        )
                        is False
                    )
                with session_factory() as session:
                    final_projection = session.get(AgentTurnProjection, projection_id)
                    assert final_projection is not None
                    assert final_projection.harness_turn_id == harness_turn_id
                    final_execution = session.scalar(
                        sa.select(AgentTurnExecution).where(
                            AgentTurnExecution.turn_projection_id == projection_id
                        )
                    )
                    assert final_execution is not None
                    assert final_execution.attempt == 2
                    assert final_execution.fencing_token >= 3
                    assert final_execution.owner_id != first_owner_id
                    second_checkpoint = session.scalar(
                        sa.select(AgentTurnCheckpoint)
                        .where(
                            AgentTurnCheckpoint.turn_projection_id == projection_id,
                            AgentTurnCheckpoint.attempt == 2,
                            AgentTurnCheckpoint.kind == AgentCheckpointKind.BEFORE_MODEL_REQUEST,
                        )
                        .limit(1)
                    )
                    assert second_checkpoint is not None
                    assert second_checkpoint.fencing_token == final_execution.fencing_token

        finally:
            if first_process is not None and first_process.poll() is None:
                first_process.kill()
                first_process.wait(timeout=15)
            if second_process is not None and second_process.poll() is None:
                second_process.kill()
                second_process.wait(timeout=15)
            allow_claim_response.set()
            if api_server is not None:
                api_server.should_exit = True
            if api_thread is not None:
                api_thread.join(timeout=15)
            if provider_server is not None:
                provider_server.shutdown()
                provider_server.server_close()
            if provider_thread is not None:
                provider_thread.join(timeout=15)
            _reset_database_state()


def test_agent_question_continues_after_agent_process_restart_on_postgresql(
    monkeypatch: pytest.MonkeyPatch,
    tmp_path: Path,
) -> None:
    base_database_url = os.getenv("DATABASE_URL", "").strip()
    if not base_database_url:
        pytest.fail("DATABASE_URL must be provided by the development environment", pytrace=False)

    node = shutil.which("node")
    if node is None:
        pytest.fail("node is required for the real Agent question continuation gate", pytrace=False)

    token = "live-agent-question-continuation-token-0123456789"
    api_port = _free_tcp_port()
    provider_port = _free_tcp_port()
    productflow_url = f"http://127.0.0.1:{api_port}"
    provider_url = f"http://127.0.0.1:{provider_port}/v1"
    api_server: uvicorn.Server | None = None
    api_thread: threading.Thread | None = None
    provider_server: ThreadingHTTPServer | None = None
    provider_thread: threading.Thread | None = None
    first_process: subprocess.Popen[str] | None = None
    second_process: subprocess.Popen[str] | None = None
    process_output: dict[str, list[str]] = {"first": [], "second": []}

    with _temporary_postgres_database(base_database_url) as database_url:
        try:
            with monkeypatch.context() as environment:
                environment.setenv("DATABASE_URL", database_url.render_as_string(hide_password=False))
                environment.setenv("REDIS_URL", os.getenv("REDIS_URL", "redis://127.0.0.1:6379/9"))
                environment.setenv("STORAGE_ROOT", str(tmp_path / "storage"))
                environment.setenv("ADMIN_ACCESS_KEY", "live-agent-question-admin-key")
                environment.setenv("SESSION_SECRET", "live-agent-question-session-secret-0123456789")
                environment.setenv("AGENT_SERVICE_INTERNAL_TOKEN", token)
                environment.setenv("AGENT_SERVICE_BASE_URL", productflow_url)
                _reset_database_state()
                backend_dir = Path(__file__).resolve().parents[1]
                config = Config(str(backend_dir / "alembic.ini"))
                config.set_main_option("script_location", str(backend_dir / "alembic"))
                command.upgrade(config, "head")

                _QuestionThenTextProviderHandler.request_count = 0
                provider_server = ThreadingHTTPServer(("127.0.0.1", provider_port), _QuestionThenTextProviderHandler)
                provider_thread = threading.Thread(
                    target=provider_server.serve_forever,
                    name="live-agent-question-provider",
                    daemon=True,
                )
                provider_thread.start()

                session_factory = get_session_factory()
                with session_factory() as session:
                    profile = create_provider_profile(
                        session,
                        name="live-agent-question-provider",
                        base_url=provider_url,
                        api_key="live-agent-question-provider-key",
                        capabilities=[CAPABILITY_TEXT_RESPONSES],
                        default_models={"agent_model": "live-question-model"},
                    )
                    update_provider_binding(
                        session,
                        purpose=AGENT_PURPOSE,
                        provider_kind="openai",
                        provider_profile_id=profile.id,
                        model_settings={"model": "live-question-model"},
                        config={},
                    )
                    agent_session = create_agent_session(
                        session,
                        title=f"live-agent-question-{uuid4().hex}",
                    )
                    conversation = agent_session.conversations[0]
                    reservation = reserve_agent_turn(
                        session,
                        product_id=None,
                        conversation_id=conversation.id,
                        input_text="需要先询问图片文字语言",
                        input_asset_ids=[],
                        idempotency_key=f"live-question-turn-{uuid4().hex}",
                    )
                    projection_id = reservation.projection.id
                    idempotency_key = reservation.projection.idempotency_key
                    conversation_id = conversation.id

                app = create_app()
                api_config = uvicorn.Config(
                    app,
                    host="127.0.0.1",
                    port=api_port,
                    log_level="error",
                )
                api_server = uvicorn.Server(api_config)
                api_thread = threading.Thread(
                    target=api_server.run,
                    name="live-agent-question-productflow",
                    daemon=True,
                )
                api_thread.start()
                _wait_for_http_health(productflow_url + "/healthz")

                agent_dir = Path(__file__).resolve().parents[2] / "agent-service"
                first_process, first_port = _spawn_live_agent_process(
                    node=node,
                    agent_dir=agent_dir,
                    data_root=tmp_path / "agent-question-a",
                    productflow_url=productflow_url,
                    token=token,
                    provider_base_url=provider_url,
                    output=process_output["first"],
                )
                _wait_for_http_health(_agent_health_url(first_port))
                started = httpx.post(
                    f"http://127.0.0.1:{first_port}/internal/v1/conversations/{conversation_id}/turns",
                    headers={"Authorization": f"Bearer {token}"},
                    json={
                        "input_text": "需要先询问图片文字语言",
                        "asset_ids": [],
                        "idempotency_key": idempotency_key,
                    },
                    timeout=20,
                )
                assert started.status_code == 202, started.text
                harness_turn_id = started.json()["turn_id"]

                def question_event_observed() -> bool:
                    with session_factory() as session:
                        return session.scalar(
                            sa.select(AgentTurnEvent.id).where(
                                AgentTurnEvent.turn_projection_id == projection_id,
                                AgentTurnEvent.kind == "question.required",
                            )
                        ) is not None

                try:
                    _wait_for_condition(question_event_observed, timeout=20)
                except AssertionError as exc:
                    local_state = httpx.get(
                        f"http://127.0.0.1:{first_port}/internal/v1/conversations/{conversation_id}/turns/{harness_turn_id}",
                        headers={"Authorization": f"Bearer {token}"},
                        timeout=2,
                    )
                    raise AssertionError(
                        "Agent question event was not published: "
                        f"provider_requests={_QuestionThenTextProviderHandler.request_count} "
                        f"first_state={local_state.status_code}:{local_state.text} "
                        f"first_output={''.join(process_output['first'])}"
                    ) from exc
                first_client = AgentServiceClient(
                    base_url=f"http://127.0.0.1:{first_port}",
                    internal_token=token,
                    connect_timeout_seconds=2,
                    read_timeout_seconds=10,
                )
                execute_agent_turn_sync(
                    projection_id,
                    gateway=first_client,
                    enqueue_later=lambda *_args: None,
                )

                with session_factory() as session:
                    waiting_projection = session.get(AgentTurnProjection, projection_id)
                    assert waiting_projection is not None
                    assert waiting_projection.status == AgentTurnStatus.REQUIRES_INPUT
                    assert waiting_projection.harness_turn_id == harness_turn_id
                    assert waiting_projection.question_json is not None
                    question_id = waiting_projection.question_json["id"]
                    waiting_execution = session.scalar(
                        sa.select(AgentTurnExecution).where(
                            AgentTurnExecution.turn_projection_id == projection_id
                        )
                    )
                    assert waiting_execution is not None
                    assert waiting_execution.phase == AgentExecutionPhase.WAITING_INPUT
                    first_owner_id = waiting_execution.owner_id

                first_process.kill()
                first_process.wait(timeout=15)

                if os.getenv(LIVE_AGENT_QUESTION_POSTGRES_RESTART_SWITCH) == "1":
                    _restart_postgres_service()

                with session_factory() as session:
                    recovery = recover_expired_agent_turn_executions(
                        session,
                        now=now_utc() + timedelta(minutes=2),
                    )
                    assert recovery.requires_input == 1
                    recovered_execution = session.scalar(
                        sa.select(AgentTurnExecution).where(
                            AgentTurnExecution.turn_projection_id == projection_id
                        )
                    )
                    assert recovered_execution is not None
                    assert recovered_execution.owner_id is None
                    assert recovered_execution.phase == AgentExecutionPhase.TERMINAL
                    assert recovered_execution.fencing_token >= 2

                if os.getenv(LIVE_AGENT_QUESTION_POSTGRES_RESTART_SWITCH) != "1":
                    assert _terminate_postgres_connections(database_url) >= 1

                second_process, second_port = _spawn_live_agent_process(
                    node=node,
                    agent_dir=agent_dir,
                    data_root=tmp_path / "agent-question-b",
                    productflow_url=productflow_url,
                    token=token,
                    provider_base_url=provider_url,
                    output=process_output["second"],
                )
                _wait_for_http_health(_agent_health_url(second_port))
                second_client = AgentServiceClient(
                    base_url=f"http://127.0.0.1:{second_port}",
                    internal_token=token,
                    connect_timeout_seconds=2,
                    read_timeout_seconds=10,
                )

                with session_factory() as session:
                    answered = answer_agent_question(
                        session,
                        product_id=None,
                        conversation_id=conversation_id,
                        projection_id=projection_id,
                        question_id=question_id,
                        answer={"option": 0},
                        gateway=second_client,
                        enqueue_sync=lambda _session, _projection_id: None,
                    )
                    continuation_id = answered.continuation_turn.id
                    assert answered.answered_turn.question_answer_json == {"option": 0}
                    assert answered.continuation_turn.harness_turn_id
                    assert answered.continuation_turn.harness_turn_id != harness_turn_id

                continuation_harness_id: str | None = None
                for _attempt in range(300):
                    with session_factory() as session:
                        continuation = session.get(AgentTurnProjection, continuation_id)
                        if continuation is not None:
                            continuation_harness_id = continuation.harness_turn_id
                    if continuation_harness_id:
                        local_state = second_client.get_turn(
                            conversation_id=conversation_id,
                            turn_id=continuation_harness_id,
                        )
                        if local_state.status in {
                            AgentTurnStatus.SUCCEEDED,
                            AgentTurnStatus.FAILED,
                            AgentTurnStatus.UNKNOWN,
                            AgentTurnStatus.AWAITING_CONFIRMATION,
                        }:
                            break
                    time.sleep(0.1)
                assert continuation_harness_id
                execute_agent_turn_sync(
                    continuation_id,
                    gateway=second_client,
                    enqueue_later=lambda *_args: None,
                )

                with session_factory() as session:
                    answered_projection = session.get(AgentTurnProjection, projection_id)
                    continuation_projection = session.get(AgentTurnProjection, continuation_id)
                    assert answered_projection is not None
                    assert continuation_projection is not None
                    assert answered_projection.status == AgentTurnStatus.REQUIRES_INPUT
                    assert answered_projection.question_answer_json == {"option": 0}
                    assert answered_projection.continuation_turn_id == continuation_id
                    assert answered_projection.sync_error is not None
                    assert continuation_projection.status == AgentTurnStatus.SUCCEEDED
                    assert "选择第 1 项" in continuation_projection.input_text
                    continuation_execution = session.scalar(
                        sa.select(AgentTurnExecution).where(
                            AgentTurnExecution.turn_projection_id == continuation_id
                        )
                    )
                    assert continuation_execution is not None
                    assert continuation_execution.owner_id is None
                    assert continuation_execution.phase == AgentExecutionPhase.TERMINAL
                    assert continuation_execution.attempt == 1
                    assert first_owner_id != continuation_execution.owner_id
                assert _QuestionThenTextProviderHandler.request_count == 2

        finally:
            if first_process is not None and first_process.poll() is None:
                first_process.kill()
                first_process.wait(timeout=15)
            if second_process is not None and second_process.poll() is None:
                second_process.kill()
                second_process.wait(timeout=15)
            if api_server is not None:
                api_server.should_exit = True
            if api_thread is not None:
                api_thread.join(timeout=15)
            if provider_server is not None:
                provider_server.shutdown()
                provider_server.server_close()
            if provider_thread is not None:
                provider_thread.join(timeout=15)
            _reset_database_state()


def test_agent_sync_worker_termination_replays_same_harness_turn_once(
    monkeypatch: pytest.MonkeyPatch,
    tmp_path: Path,
) -> None:
    if os.getenv(LIVE_AGENT_WORKER_EFFECTS_SWITCH) != "1":
        pytest.skip(f"set {LIVE_AGENT_WORKER_EFFECTS_SWITCH}=1 to run the Agent worker effect gate")

    base_database_url = os.getenv("DATABASE_URL", "").strip()
    if not base_database_url:
        pytest.fail("DATABASE_URL must be provided by the development environment", pytrace=False)

    node = shutil.which("node")
    if node is None:
        pytest.fail("node is required for the real Agent worker effect gate", pytrace=False)

    token = "live-agent-worker-effect-token-0123456789"
    api_port = _free_tcp_port()
    provider_port = _free_tcp_port()
    productflow_url = f"http://127.0.0.1:{api_port}"
    provider_url = f"http://127.0.0.1:{provider_port}/v1"
    api_server: uvicorn.Server | None = None
    api_thread: threading.Thread | None = None
    provider_server: ThreadingHTTPServer | None = None
    provider_thread: threading.Thread | None = None
    first_worker: subprocess.Popen[str] | None = None
    second_worker: subprocess.Popen[str] | None = None
    agent_process: subprocess.Popen[str] | None = None
    worker_output: dict[str, str] = {}
    agent_output: list[str] = []

    class _CountingFailingProviderHandler(_FailingProviderHandler):
        request_count = 0
        request_lock = threading.Lock()

        def do_POST(self) -> None:
            with self.request_lock:
                type(self).request_count += 1
            super().do_POST()

    with _temporary_postgres_database(base_database_url) as database_url:
        try:
            with monkeypatch.context() as environment:
                environment.setenv("DATABASE_URL", database_url.render_as_string(hide_password=False))
                environment.setenv("REDIS_URL", os.getenv("REDIS_URL", "redis://127.0.0.1:6379/9"))
                environment.setenv("STORAGE_ROOT", str(tmp_path / "storage"))
                environment.setenv("ADMIN_ACCESS_KEY", "live-agent-worker-effect-admin-key")
                environment.setenv("SESSION_SECRET", "live-agent-worker-effect-session-secret-0123456789")
                environment.setenv("AGENT_SERVICE_INTERNAL_TOKEN", token)
                environment.setenv("AGENT_SERVICE_BASE_URL", productflow_url)
                _reset_database_state()

                backend_dir = Path(__file__).resolve().parents[1]
                config = Config(str(backend_dir / "alembic.ini"))
                config.set_main_option("script_location", str(backend_dir / "alembic"))
                command.upgrade(config, "head")

                provider_server = ThreadingHTTPServer(
                    ("127.0.0.1", provider_port),
                    _CountingFailingProviderHandler,
                )
                provider_thread = threading.Thread(
                    target=provider_server.serve_forever,
                    name="live-agent-worker-effect-provider",
                    daemon=True,
                )
                provider_thread.start()

                session_factory = get_session_factory()
                with session_factory() as session:
                    profile = create_provider_profile(
                        session,
                        name="live-agent-worker-effect-provider",
                        base_url=provider_url,
                        api_key="live-agent-worker-effect-provider-key",
                        capabilities=[CAPABILITY_TEXT_RESPONSES],
                        default_models={"agent_model": "live-worker-effect-model"},
                    )
                    update_provider_binding(
                        session,
                        purpose=AGENT_PURPOSE,
                        provider_kind="openai",
                        provider_profile_id=profile.id,
                        model_settings={"model": "live-worker-effect-model"},
                        config={},
                    )
                    workspace = _create_workspace(session, key=f"live-agent-worker-effect-{uuid4().hex}")
                    reservation = reserve_agent_turn(
                        session,
                        product_id=workspace.product.id,
                        conversation_id=workspace.conversation.id,
                        input_text="验证 Agent sync worker 的重复投递幂等性",
                        input_asset_ids=[],
                        idempotency_key=f"live-agent-worker-effect-turn-{uuid4().hex}",
                    )
                    dispatch = stage_async_dispatch_for_actor(
                        session,
                        "run_agent_turn_sync",
                        reservation.projection.id,
                    )
                    session.commit()
                    projection_id = reservation.projection.id
                    dispatch_id = dispatch.id
                    aggregate_id = reservation.projection.id
                    run_id = workspace.conversation.harness_run_id

                app = create_app()
                api_config = uvicorn.Config(
                    app,
                    host="127.0.0.1",
                    port=api_port,
                    log_level="error",
                )
                api_server = uvicorn.Server(api_config)
                api_thread = threading.Thread(
                    target=api_server.run,
                    name="live-agent-worker-effect-productflow",
                    daemon=True,
                )
                api_thread.start()
                _wait_for_http_health(productflow_url + "/healthz")

                agent_dir = Path(__file__).resolve().parents[2] / "agent-service"
                agent_data_root = tmp_path / "agent-worker-effect"
                agent_process, agent_port = _spawn_live_agent_process(
                    node=node,
                    agent_dir=agent_dir,
                    data_root=agent_data_root,
                    productflow_url=productflow_url,
                    token=token,
                    provider_base_url=provider_url,
                    output=agent_output,
                )
                _wait_for_http_health(_agent_health_url(agent_port))

                sent = run_async_dispatcher_once(enqueue=lambda *_args: None, limit=10)
                assert sent.sent == 1

                worker_environment = {
                    **os.environ,
                    "AGENT_SERVICE_BASE_URL": f"http://127.0.0.1:{agent_port}",
                    "AGENT_SERVICE_INTERNAL_TOKEN": token,
                }
                backend_dir = Path(__file__).resolve().parents[1]
                hold_after_target = """
import sys
import time

import productflow_backend.workers as workers


original_target = workers._execute_async_dispatch_target


def hold_after_target(actor_name, aggregate_id):
    original_target(actor_name, aggregate_id)
    time.sleep(120)


workers._execute_async_dispatch_target = hold_after_target
workers.execute_async_dispatch(sys.argv[1], sys.argv[2])
"""
                first_worker = subprocess.Popen(
                    [sys.executable, "-c", hold_after_target, dispatch_id, aggregate_id],
                    cwd=backend_dir,
                    env=worker_environment,
                    stdout=subprocess.PIPE,
                    stderr=subprocess.STDOUT,
                    text=True,
                )

                old_lease_token: str | None = None
                old_lease_expires_at = None
                harness_turn_id: str | None = None
                deadline = time.monotonic() + 20
                while time.monotonic() < deadline:
                    with session_factory() as session:
                        persisted_dispatch = session.get(AsyncDispatch, dispatch_id)
                        persisted_projection = session.get(AgentTurnProjection, projection_id)
                        if (
                            persisted_dispatch is not None
                            and persisted_dispatch.lease_token is not None
                            and persisted_projection is not None
                            and persisted_projection.harness_turn_id is not None
                        ):
                            old_lease_token = persisted_dispatch.lease_token
                            old_lease_expires_at = persisted_dispatch.lease_expires_at
                            harness_turn_id = persisted_projection.harness_turn_id
                            break
                    if first_worker.poll() is not None:
                        output, _ = first_worker.communicate(timeout=2)
                        pytest.fail(f"worker exited before applying Agent sync effect: {output!r}")
                    time.sleep(0.05)

                assert old_lease_token is not None
                assert old_lease_expires_at is not None
                assert harness_turn_id is not None
                first_worker.kill()
                worker_output["first"] = first_worker.communicate(timeout=10)[0]
                first_worker = None

                with session_factory() as session:
                    persisted_dispatch = session.get(AsyncDispatch, dispatch_id)
                    assert persisted_dispatch is not None
                    assert persisted_dispatch.status.value == "sent"
                    assert persisted_dispatch.lease_token == old_lease_token

                recovered = run_async_dispatcher_once(
                    enqueue=lambda *_args: None,
                    now=old_lease_expires_at + timedelta(seconds=1),
                    limit=10,
                )
                assert recovered.reconciled >= 1
                assert recovered.sent == 1

                replay_worker = """
import sys

import productflow_backend.workers as workers


workers.execute_async_dispatch(sys.argv[1], sys.argv[2])
"""
                second_worker = subprocess.Popen(
                    [sys.executable, "-c", replay_worker, dispatch_id, aggregate_id],
                    cwd=backend_dir,
                    env=worker_environment,
                    stdout=subprocess.PIPE,
                    stderr=subprocess.STDOUT,
                    text=True,
                )

                consumed = False
                deadline = time.monotonic() + 20
                while time.monotonic() < deadline:
                    with session_factory() as session:
                        persisted_dispatch = session.get(AsyncDispatch, dispatch_id)
                        if persisted_dispatch is not None and persisted_dispatch.status.value == "consumed":
                            consumed = True
                            assert persisted_dispatch.attempts == 2
                            break
                    if second_worker.poll() is not None:
                        output, _ = second_worker.communicate(timeout=2)
                        pytest.fail(f"replayed Agent sync worker exited before consumption: {output!r}")
                    time.sleep(0.05)
                assert consumed
                second_worker.communicate(timeout=10)
                second_worker = None

                run_record_path = agent_data_root / "runs" / run_id / "run.json"
                assert run_record_path.exists()
                run_record = json.loads(run_record_path.read_text())
                assert run_record["turn_ids"] == [harness_turn_id]
                assert _CountingFailingProviderHandler.request_count == 1
        finally:
            if first_worker is not None and first_worker.poll() is None:
                first_worker.kill()
                worker_output["first"] = first_worker.communicate(timeout=10)[0]
            if second_worker is not None and second_worker.poll() is None:
                second_worker.kill()
                worker_output["second"] = second_worker.communicate(timeout=10)[0]
            if agent_process is not None and agent_process.poll() is None:
                agent_process.kill()
                agent_process.wait(timeout=15)
            if api_server is not None:
                api_server.should_exit = True
            if api_thread is not None:
                api_thread.join(timeout=15)
            if provider_server is not None:
                provider_server.shutdown()
                provider_server.server_close()
            if provider_thread is not None:
                provider_thread.join(timeout=15)
            _reset_database_state()


def test_browser_sse_reconnects_from_cursor_on_postgresql(
    monkeypatch: pytest.MonkeyPatch,
    tmp_path: Path,
) -> None:
    base_database_url = os.getenv("DATABASE_URL", "").strip()
    if not base_database_url:
        pytest.fail("DATABASE_URL must be provided by the development environment", pytrace=False)

    chrome = shutil.which("google-chrome")
    if chrome is None:
        pytest.fail("google-chrome is required for the real browser SSE gate", pytrace=False)

    token = "live-browser-sse-token-0123456789"
    admin_key = "live-browser-sse-admin-key"
    api_port = _free_tcp_port()
    browser_port = _free_tcp_port()
    productflow_url = f"http://127.0.0.1:{api_port}"
    browser_url = f"http://127.0.0.1:{browser_port}"
    api_server: uvicorn.Server | None = None
    api_thread: threading.Thread | None = None
    browser_server: ThreadingHTTPServer | None = None
    browser_thread: threading.Thread | None = None
    chrome_process: subprocess.Popen[str] | None = None

    with _temporary_postgres_database(base_database_url) as database_url:
        try:
            with monkeypatch.context() as environment:
                environment.setenv("DATABASE_URL", database_url.render_as_string(hide_password=False))
                environment.setenv("REDIS_URL", os.getenv("REDIS_URL", "redis://127.0.0.1:6379/9"))
                environment.setenv("STORAGE_ROOT", str(tmp_path / "storage"))
                environment.setenv("ADMIN_ACCESS_KEY", admin_key)
                environment.setenv("SESSION_SECRET", "live-browser-sse-session-secret-0123456789")
                environment.setenv("AGENT_SERVICE_INTERNAL_TOKEN", token)
                environment.setenv("BACKEND_CORS_ORIGINS", browser_url)
                _reset_database_state()
                backend_dir = Path(__file__).resolve().parents[1]
                config = Config(str(backend_dir / "alembic.ini"))
                config.set_main_option("script_location", str(backend_dir / "alembic"))
                command.upgrade(config, "head")

                session_factory = get_session_factory()
                with session_factory() as session:
                    agent_session = create_agent_session(session, title=f"live-browser-sse-{uuid4().hex}")
                    conversation = agent_session.conversations[0]
                    reservation = reserve_agent_turn(
                        session,
                        product_id=None,
                        conversation_id=conversation.id,
                        input_text="验证浏览器 SSE cursor 重连",
                        input_asset_ids=[],
                        idempotency_key=f"live-browser-sse-turn-{uuid4().hex}",
                    )
                    lease = claim_agent_turn_execution(
                        session,
                        conversation_id=conversation.id,
                        task_id=None,
                        idempotency_key=reservation.projection.idempotency_key,
                        harness_turn_id="browser-sse-harness-turn",
                        owner_id="browser-sse-owner",
                        lease_seconds=60,
                    )
                    append_agent_turn_event(
                        session,
                        conversation_id=conversation.id,
                        execution_id=lease.execution_id,
                        owner_id=lease.owner_id,
                        lease_token=lease.lease_token,
                        sequence=1,
                        schema_version=1,
                        run_id=conversation.harness_run_id,
                        turn_id=lease.harness_turn_id,
                        kind="turn.queued",
                        payload={"status": "queued"},
                        created_at=now_utc(),
                    )
                    conversation_id = conversation.id
                    projection_id = reservation.projection.id

                app = create_app()
                api_config = uvicorn.Config(
                    app,
                    host="127.0.0.1",
                    port=api_port,
                    log_level="error",
                )
                api_server = uvicorn.Server(api_config)
                api_thread = threading.Thread(target=api_server.run, name="live-browser-sse-productflow", daemon=True)
                api_thread.start()
                _wait_for_http_health(productflow_url + "/healthz")

                event_url = (
                    f"{productflow_url}/api/v2/agent-conversations/{conversation_id}/turns/{projection_id}/events"
                )
                _BrowserSSEHarnessHandler.reset(
                    _browser_sse_html(
                        api_url=productflow_url,
                        event_url=event_url,
                        control_url=browser_url,
                        admin_key=admin_key,
                    )
                )
                browser_server = ThreadingHTTPServer(("127.0.0.1", browser_port), _BrowserSSEHarnessHandler)
                browser_thread = threading.Thread(
                    target=browser_server.serve_forever,
                    name="live-browser-sse-harness",
                    daemon=True,
                )
                browser_thread.start()

                chrome_process = subprocess.Popen(
                    [
                        chrome,
                        "--headless=new",
                        "--disable-gpu",
                        "--disable-dev-shm-usage",
                        "--disable-web-security",
                        "--no-sandbox",
                        f"--user-data-dir={tmp_path / 'chrome-profile'}",
                        "--virtual-time-budget=15000",
                        "--dump-dom",
                        browser_url + "/",
                    ],
                    stdout=subprocess.PIPE,
                    stderr=subprocess.STDOUT,
                    text=True,
                )

                assert _BrowserSSEHarnessHandler.first_seen.wait(timeout=15), (
                    "Chrome did not receive the first SSE event: "
                    f"process_exit={chrome_process.poll()}"
                )
                with session_factory() as session:
                    append_agent_turn_event(
                        session,
                        conversation_id=conversation_id,
                        execution_id=lease.execution_id,
                        owner_id=lease.owner_id,
                        lease_token=lease.lease_token,
                        sequence=2,
                        schema_version=1,
                        run_id=conversation.harness_run_id,
                        turn_id=lease.harness_turn_id,
                        kind="turn.succeeded",
                        payload={"status": "succeeded", "output": "SSE replay complete"},
                        created_at=now_utc(),
                    )
                    project_agent_turn_state(
                        session,
                        product_id=None,
                        conversation_id=conversation_id,
                        projection_id=projection_id,
                        harness_turn_id=lease.harness_turn_id,
                        status=AgentTurnStatus.SUCCEEDED,
                        output_text="SSE replay complete",
                        error_text=None,
                        question_json=None,
                        finished_at=now_utc(),
                    )
                    assert release_agent_turn_execution(
                        session,
                        conversation_id=conversation_id,
                        execution_id=lease.execution_id,
                        owner_id=lease.owner_id,
                        lease_token=lease.lease_token,
                        phase=AgentExecutionPhase.TERMINAL,
                    )

                assert _BrowserSSEHarnessHandler.result_event.wait(timeout=15), (
                    "Chrome did not finish the SSE replay: "
                    f"process_exit={chrome_process.poll()}"
                )
                stdout, _ = chrome_process.communicate(timeout=10)
                result = _BrowserSSEHarnessHandler.result_payload
                assert result is not None, stdout
                assert result["events"] == [1, 2]
                assert result["urls"] == [f"{event_url}?after=0", f"{event_url}?after=1"]
                assert result["loads"] == 2
                assert result["errors"] == []
        finally:
            if chrome_process is not None and chrome_process.poll() is None:
                chrome_process.terminate()
                chrome_process.wait(timeout=10)
            if browser_server is not None:
                browser_server.shutdown()
                browser_server.server_close()
            if browser_thread is not None:
                browser_thread.join(timeout=10)
            if api_server is not None:
                api_server.should_exit = True
            if api_thread is not None:
                api_thread.join(timeout=15)
            _reset_database_state()


class _BrowserSSEHarnessHandler(BaseHTTPRequestHandler):
    html = ""
    first_seen = threading.Event()
    result_event = threading.Event()
    result_payload: dict[str, object] | None = None

    @classmethod
    def reset(cls, html: str) -> None:
        cls.html = html
        cls.first_seen = threading.Event()
        cls.result_event = threading.Event()
        cls.result_payload = None

    def do_GET(self) -> None:
        path = urlsplit(self.path).path
        if path == "/first":
            type(self).first_seen.set()
            self.send_response(204)
            self.send_header("Access-Control-Allow-Origin", "*")
            self.end_headers()
            return
        if path != "/":
            self.send_error(404)
            return
        body = type(self).html.encode("utf-8")
        self.send_response(200)
        self.send_header("Content-Type", "text/html; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_POST(self) -> None:
        if urlsplit(self.path).path != "/result":
            self.send_error(404)
            return
        length = int(self.headers.get("Content-Length", "0"))
        payload = json.loads(self.rfile.read(length).decode("utf-8"))
        type(self).result_payload = payload
        type(self).result_event.set()
        self.send_response(204)
        self.send_header("Access-Control-Allow-Origin", "*")
        self.end_headers()

    def log_message(self, _format: str, *_args) -> None:
        return


def _browser_sse_html(*, api_url: str, event_url: str, control_url: str, admin_key: str) -> str:
    template = """<!doctype html>
<html><body><script>
const API_URL = __API_URL__;
const EVENT_URL = __EVENT_URL__;
const CONTROL_URL = __CONTROL_URL__;
const ADMIN_KEY = __ADMIN_KEY__;
const STORAGE_KEY = "productflow-browser-sse-reload";
const saved = sessionStorage.getItem(STORAGE_KEY);
const result = saved ? JSON.parse(saved) : {events: [], urls: [], errors: [], loads: 0};
result.loads += 1;
let finished = false;

function persist() {
  sessionStorage.setItem(STORAGE_KEY, JSON.stringify(result));
}

persist();

async function finish() {
  if (finished) return;
  finished = true;
  sessionStorage.removeItem(STORAGE_KEY);
  document.body.textContent = JSON.stringify(result);
  await fetch(CONTROL_URL + "/result", {
    method: "POST",
    headers: {"Content-Type": "application/json"},
    body: JSON.stringify(result),
  });
}

function connect(after) {
  const url = EVENT_URL + "?after=" + after;
  result.urls.push(url);
  persist();
  const source = new EventSource(url, {withCredentials: true});
  const receive = (event) => {
    if (finished) return;
    const payload = JSON.parse(event.data);
    result.events.push(payload.sequence);
    persist();
    if (payload.sequence === 1) {
      source.close();
      fetch(CONTROL_URL + "/first").then(() => setTimeout(() => location.reload(), 50));
    } else if (payload.sequence === 2) {
      source.close();
      void finish();
    } else {
      result.errors.push("unexpected sequence " + payload.sequence);
      void finish();
    }
  };
  source.addEventListener("turn.queued", receive);
  source.addEventListener("turn.succeeded", receive);
  source.onerror = () => {
    if (!finished) {
      result.errors.push("sse error");
      void finish();
    }
  };
}

(async () => {
  const login = await fetch(API_URL + "/api/auth/session", {
    method: "POST",
    credentials: "include",
    headers: {"Content-Type": "application/json"},
    body: JSON.stringify({admin_key: ADMIN_KEY}),
  });
  if (!login.ok) throw new Error("login failed: " + login.status);
  connect(result.events.length > 0 ? result.events[result.events.length - 1] : 0);
})().catch((error) => {
  result.errors.push(String(error));
  void finish();
});
</script></body></html>"""
    return (
        template
        .replace("__API_URL__", json.dumps(api_url))
        .replace("__EVENT_URL__", json.dumps(event_url))
        .replace("__CONTROL_URL__", json.dumps(control_url))
        .replace("__ADMIN_KEY__", json.dumps(admin_key))
    )


class _FailingProviderHandler(BaseHTTPRequestHandler):
    def do_GET(self) -> None:
        self._respond()

    def do_POST(self) -> None:
        self._respond()

    def log_message(self, _format: str, *_args) -> None:
        return

    def _respond(self) -> None:
        body = b"live provider failure"
        self.send_response(503)
        self.send_header("Content-Type", "text/plain")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)


class _QuestionThenTextProviderHandler(BaseHTTPRequestHandler):
    request_count = 0
    request_lock = threading.Lock()

    def do_POST(self) -> None:
        content_length = int(self.headers.get("Content-Length", "0"))
        if content_length:
            self.rfile.read(content_length)
        with self.request_lock:
            type(self).request_count += 1
            request_number = type(self).request_count
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.send_header("Connection", "keep-alive")
        self.send_header("Cache-Control", "no-cache")
        self.end_headers()
        if request_number == 1:
            tool_call = {
                "type": "function_call",
                "id": "fc-live-question",
                "call_id": "call-live-question",
                "name": "ask_user",
                "arguments": (
                    '{"header":"语言","question":"图片中的文字使用哪种语言？",'
                    '"options":[{"label":"中文"},{"label":"英文"}]}'
                ),
                "status": "completed",
            }
            _write_provider_sse(self.wfile, {
                "type": "response.created",
                "response": {"id": "resp-live-question-1", "object": "response", "status": "in_progress", "output": []},
            })
            _write_provider_sse(self.wfile, {
                "type": "response.output_item.added",
                "output_index": 0,
                "item": {**tool_call, "arguments": ""},
            })
            _write_provider_sse(self.wfile, {
                "type": "response.function_call_arguments.delta",
                "output_index": 0,
                "delta": tool_call["arguments"],
            })
            _write_provider_sse(self.wfile, {
                "type": "response.function_call_arguments.done",
                "output_index": 0,
                "arguments": tool_call["arguments"],
            })
            _write_provider_sse(self.wfile, {"type": "response.output_item.done", "output_index": 0, "item": tool_call})
            _write_provider_sse(self.wfile, {
                "type": "response.completed",
                "response": {
                    "id": "resp-live-question-1",
                    "object": "response",
                    "status": "completed",
                    "output": [tool_call],
                    "usage": {"input_tokens": 1, "output_tokens": 3, "total_tokens": 4},
                },
            })
        else:
            message = {
                "type": "message",
                "id": "msg-live-question-2",
                "role": "assistant",
                "status": "completed",
                "phase": "final_answer",
                "content": [{"type": "output_text", "text": "已根据回答继续执行", "annotations": []}],
            }
            response_body = {
                "id": "resp-live-question-2",
                "object": "response",
                "status": "completed",
                "output": [message],
                "usage": {"input_tokens": 1, "output_tokens": 4, "total_tokens": 5},
            }
            _write_provider_sse(self.wfile, {
                "type": "response.created",
                "response": {"id": response_body["id"], "object": "response", "status": "in_progress", "output": []},
            })
            _write_provider_sse(self.wfile, {
                "type": "response.output_item.added",
                "output_index": 0,
                "item": {
                    "type": "message",
                    "id": message["id"],
                    "role": "assistant",
                    "status": "in_progress",
                    "content": [],
                },
            })
            _write_provider_sse(self.wfile, {
                "type": "response.output_text.delta",
                "output_index": 0,
                "delta": "已根据回答继续执行",
            })
            _write_provider_sse(self.wfile, {
                "type": "response.output_text.done",
                "output_index": 0,
                "text": "已根据回答继续执行",
            })
            _write_provider_sse(self.wfile, {"type": "response.output_item.done", "output_index": 0, "item": message})
            _write_provider_sse(self.wfile, {"type": "response.completed", "response": response_body})
        self.wfile.flush()
        self.close_connection = True

    def log_message(self, _format: str, *_args) -> None:
        return


def _write_provider_sse(stream, event: dict[str, object]) -> None:
    stream.write(f"event: {event['type']}\ndata: {json.dumps(event, ensure_ascii=False)}\n\n".encode())


def _free_tcp_port() -> int:
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
        sock.bind(("127.0.0.1", 0))
        return int(sock.getsockname()[1])


def _spawn_live_agent_process(
    *,
    node: str,
    agent_dir: Path,
    data_root: Path,
    productflow_url: str,
    token: str,
    provider_base_url: str | None = None,
    output: list[str],
) -> tuple[subprocess.Popen[str], int]:
    port = _free_tcp_port()
    process = subprocess.Popen(
        [node, "--import", "tsx/esm", "src/main.ts"],
        cwd=agent_dir,
        env={
            **os.environ,
            "AGENT_LISTEN_ADDRESS": f"127.0.0.1:{port}",
            "AGENT_DATA_ROOT": str(data_root),
            "PRODUCTFLOW_INTERNAL_BASE_URL": productflow_url,
            "AGENT_SERVICE_INTERNAL_TOKEN": token,
            "AGENT_PROVIDER_API_KEY": "live-agent-provider-key",
            "AGENT_PROVIDER_BASE_URL": provider_base_url or "http://127.0.0.1:1/v1",
            "AGENT_PROVIDER_MODEL": "live-failing-model",
            "AGENT_EVENT_POLL_INTERVAL": "10ms",
            "AGENT_HEARTBEAT_INTERVAL": "100ms",
            "PRODUCTFLOW_REQUEST_TIMEOUT": "5s",
            "AGENT_MAX_CONCURRENT_TURNS": "1",
            "AGENT_MODEL_CONTEXT_WINDOW": "128000",
            "AGENT_AUTO_COMPACT_TOKEN_LIMIT": "96000",
            "AGENT_MAX_ITERATIONS": "1",
        },
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        text=True,
        bufsize=1,
    )

    def collect_output() -> None:
        if process.stdout is None:
            return
        for line in process.stdout:
            output.append(line)

    threading.Thread(target=collect_output, name="live-agent-output", daemon=True).start()
    return process, port


def _agent_health_url(port: int) -> str:
    return f"http://127.0.0.1:{port}/healthz"


def _wait_for_http_health(url: str, *, timeout: float = 15) -> None:
    _wait_for_condition(
        lambda: _http_ok(url),
        timeout=timeout,
    )


def _http_ok(url: str) -> bool:
    try:
        response = httpx.get(url, timeout=1)
    except httpx.HTTPError:
        return False
    return response.is_success


def _start_agent_turn(port: int, conversation_id: str, payload: dict[str, object]) -> None:
    try:
        httpx.post(
            f"http://127.0.0.1:{port}/internal/v1/conversations/{conversation_id}/turns",
            headers={"Authorization": "Bearer live-agent-process-handoff-token-0123456789"},
            json=payload,
            timeout=20,
        )
    except httpx.HTTPError:
        return


def _wait_for_condition(predicate, *, timeout: float) -> None:
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        if predicate():
            return
        time.sleep(0.05)
    raise AssertionError("live Agent process condition was not reached")
