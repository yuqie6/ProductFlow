from __future__ import annotations

import json
import os
import socket
import threading
import time
from collections.abc import Iterator
from contextlib import contextmanager
from pathlib import Path
from urllib.parse import urlsplit
from uuid import uuid4

import httpx
import pytest
import sqlalchemy as sa
import uvicorn
from alembic.config import Config
from sqlalchemy.engine import URL, make_url
from test_agent_workflow_run_requests import _create_requestable_workspace

from alembic import command
from productflow_backend.application.agent.sessions import create_agent_session
from productflow_backend.config import get_settings
from productflow_backend.domain.enums import AgentConversationScope
from productflow_backend.infrastructure.db.models import (
    AgentConversation,
    AgentToolMutation,
    AgentWorkflowRunRequest,
    ProductImageAsset,
)
from productflow_backend.infrastructure.db.session import get_engine, get_session_factory
from productflow_backend.presentation.api import create_app

LIVE_AGENT_EFFECTS_SWITCH = "PRODUCTFLOW_RUN_LIVE_AGENT_EFFECTS"

pytestmark = [
    pytest.mark.live_dependencies,
    pytest.mark.skipif(
        os.getenv(LIVE_AGENT_EFFECTS_SWITCH) != "1",
        reason=f"set {LIVE_AGENT_EFFECTS_SWITCH}=1 to run the real ProductFlow effect response-loss gate",
    ),
]


@contextmanager
def _temporary_postgres_database(base_database_url: str) -> Iterator[URL]:
    base_url = make_url(base_database_url)
    if base_url.get_backend_name() != "postgresql":
        pytest.fail("DATABASE_URL must point to PostgreSQL for the live Agent effect gate", pytrace=False)

    database_name = f"productflow_live_agent_effects_{uuid4().hex}"
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


def _free_tcp_port() -> int:
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as listener:
        listener.bind(("127.0.0.1", 0))
        return int(listener.getsockname()[1])


def _wait_for_http_health(url: str, *, timeout: float = 15) -> None:
    deadline = time.monotonic() + timeout
    last_error: Exception | None = None
    while time.monotonic() < deadline:
        try:
            response = httpx.get(url, timeout=1)
            if response.status_code == 200:
                return
            last_error = RuntimeError(f"health returned {response.status_code}: {response.text}")
        except Exception as exc:  # pragma: no cover - only used while the live server starts
            last_error = exc
        time.sleep(0.1)
    raise AssertionError(f"HTTP health did not become ready: {url}; last_error={last_error!r}")


def _post_and_drop_response_body(
    url: str,
    *,
    token: str,
    idempotency_key: str,
    payload: dict[str, object],
) -> int:
    parsed = urlsplit(url)
    if parsed.scheme != "http" or parsed.hostname is None or parsed.port is None:
        raise AssertionError(f"live response-loss URL must be an HTTP URL with an explicit port: {url}")
    body = json.dumps(payload, separators=(",", ":")).encode("utf-8")
    path = parsed.path or "/"
    if parsed.query:
        path += f"?{parsed.query}"
    request = (
        f"POST {path} HTTP/1.1\r\n"
        f"Host: {parsed.netloc}\r\n"
        f"Authorization: Bearer {token}\r\n"
        "Accept: application/json\r\n"
        "Content-Type: application/json\r\n"
        f"Idempotency-Key: {idempotency_key}\r\n"
        f"Content-Length: {len(body)}\r\n"
        "Connection: close\r\n"
        "\r\n"
    ).encode("ascii") + body

    with socket.create_connection((parsed.hostname, parsed.port), timeout=10) as connection:
        connection.sendall(request)
        response_headers = bytearray()
        while b"\r\n\r\n" not in response_headers:
            chunk = connection.recv(4096)
            if not chunk:
                break
            response_headers.extend(chunk)
        status_line = bytes(response_headers).split(b"\r\n", 1)[0].decode("ascii", errors="replace")
        parts = status_line.split(" ", 2)
        if len(parts) < 2 or not parts[1].isdigit():
            raise AssertionError(f"ProductFlow returned an invalid HTTP status line: {status_line!r}")
        # The application has already committed before FastAPI serializes this body. The client deliberately
        # closes here and discards everything after the headers, reproducing a response-body loss.
        return int(parts[1])


def _internal_headers(token: str, idempotency_key: str | None = None) -> dict[str, str]:
    headers = {"Authorization": f"Bearer {token}", "Accept": "application/json"}
    if idempotency_key is not None:
        headers["Idempotency-Key"] = idempotency_key
    return headers


def test_real_productflow_effects_reconcile_after_response_body_loss(
    monkeypatch: pytest.MonkeyPatch,
    tmp_path: Path,
) -> None:
    base_database_url = os.getenv("DATABASE_URL", "").strip()
    if not base_database_url:
        pytest.fail("DATABASE_URL must be provided by the development environment", pytrace=False)

    token = "live-agent-effects-token-0123456789"
    api_port = _free_tcp_port()
    productflow_url = f"http://127.0.0.1:{api_port}"
    api_server: uvicorn.Server | None = None
    api_thread: threading.Thread | None = None

    with _temporary_postgres_database(base_database_url) as database_url:
        try:
            with monkeypatch.context() as environment:
                environment.setenv("DATABASE_URL", database_url.render_as_string(hide_password=False))
                environment.setenv("REDIS_URL", os.getenv("REDIS_URL", "redis://127.0.0.1:6379/9"))
                environment.setenv("STORAGE_ROOT", str(tmp_path / "storage"))
                environment.setenv("ADMIN_ACCESS_KEY", "live-agent-effects-admin-key")
                environment.setenv("SESSION_SECRET", "live-agent-effects-session-secret-0123456789")
                environment.setenv("AGENT_SERVICE_INTERNAL_TOKEN", token)
                _reset_database_state()

                backend_dir = Path(__file__).resolve().parents[1]
                config = Config(str(backend_dir / "alembic.ini"))
                config.set_main_option("script_location", str(backend_dir / "alembic"))
                command.upgrade(config, "head")

                session_factory = get_session_factory()
                with session_factory() as session:
                    agent_session = create_agent_session(session, title=f"live-agent-effects-{uuid4().hex}")
                    global_conversation = next(
                        conversation
                        for conversation in agent_session.conversations
                        if conversation.scope_type == AgentConversationScope.GLOBAL
                    )
                    global_conversation_id = global_conversation.id
                    workspace, workflow = _create_requestable_workspace(
                        session,
                        name="真实响应丢失工作流商品",
                        idempotency_key=f"live-agent-effects-workspace-{uuid4().hex}",
                    )
                    product_conversation_id = workspace.conversation.id
                    product_id = workspace.product.id
                    asset_id = workspace.created_assets[0].id
                    workflow_id = workflow.id
                    workflow_revision = workflow.revision

                app = create_app()
                api_config = uvicorn.Config(
                    app,
                    host="127.0.0.1",
                    port=api_port,
                    log_level="error",
                )
                api_server = uvicorn.Server(api_config)
                api_thread = threading.Thread(target=api_server.run, name="live-agent-effects-productflow", daemon=True)
                api_thread.start()
                _wait_for_http_health(productflow_url + "/healthz")

                workspace_key = f"live-agent-effects-create-{uuid4().hex}"
                workspace_path = f"/api/internal/v1/agent-conversations/{global_conversation_id}/product-workspaces"
                workspace_url = productflow_url + workspace_path
                assert _post_and_drop_response_body(
                    workspace_url,
                    token=token,
                    idempotency_key=workspace_key,
                    payload={"name": "响应丢失后仍唯一的商品"},
                ) == 201

                with httpx.Client(timeout=10) as client:
                    workspace_reconcile = client.post(
                        workspace_url + "/reconcile",
                        headers=_internal_headers(token, workspace_key),
                        json={"name": "响应丢失后仍唯一的商品"},
                    )
                    assert workspace_reconcile.status_code == 200, workspace_reconcile.text
                    workspace_result = workspace_reconcile.json()
                    assert workspace_result["state"] == "applied"
                    reconciled_product_id = workspace_result["result"]["product_id"]

                    workspace_replay = client.post(
                        workspace_url,
                        headers=_internal_headers(token, workspace_key),
                        json={"name": "响应丢失后仍唯一的商品"},
                    )
                    assert workspace_replay.status_code == 201, workspace_replay.text
                    assert workspace_replay.json()["created"] is False
                    assert workspace_replay.json()["product_id"] == reconciled_product_id

                    prepare_url = (
                        f"{productflow_url}/api/internal/v1/agent-conversations/"
                        f"{product_conversation_id}/workflow-run-requests/prepare"
                    )
                    prepared_response = client.post(
                        prepare_url,
                        headers=_internal_headers(token),
                        json={"expected_workflow_revision": workflow_revision, "task_id": None, "source_run_id": None},
                    )
                    assert prepared_response.status_code == 200, prepared_response.text
                    prepared = prepared_response.json()
                    assert prepared["workflow_id"] == workflow_id
                    request_payload = {
                        "expected_workflow_revision": prepared["workflow_revision"],
                        "workflow_id": prepared["workflow_id"],
                        "source_step_id": "live-agent-effects-step",
                        "task_id": None,
                        "source_run_id": None,
                    }
                    request_key = f"live-agent-effects-request-{uuid4().hex}"
                    request_url = (
                        f"{productflow_url}/api/internal/v1/agent-conversations/"
                        f"{product_conversation_id}/workflow-run-requests"
                    )
                    assert _post_and_drop_response_body(
                        request_url,
                        token=token,
                        idempotency_key=request_key,
                        payload=request_payload,
                    ) == 200

                    request_reconcile = client.post(
                        request_url + "/reconcile",
                        headers=_internal_headers(token, request_key),
                        json=request_payload,
                    )
                    assert request_reconcile.status_code == 200, request_reconcile.text
                    request_result = request_reconcile.json()
                    assert request_result["state"] == "applied"
                    assert request_result["result"]["status"] == "awaiting_confirmation"

                    request_replay = client.post(
                        request_url,
                        headers=_internal_headers(token, request_key),
                        json=request_payload,
                    )
                    assert request_replay.status_code == 200, request_replay.text
                    assert request_replay.json()["id"] == request_result["result"]["id"]

                    rename_prepare_url = (
                        f"{productflow_url}/api/internal/v1/agent-conversations/"
                        f"{product_conversation_id}/asset-renames/prepare"
                    )
                    rename_prepared_response = client.post(
                        rename_prepare_url,
                        headers=_internal_headers(token),
                        json={"asset_id": asset_id, "target_display_name": "响应丢失后的参考图"},
                    )
                    assert rename_prepared_response.status_code == 200, rename_prepared_response.text
                    rename_payload = rename_prepared_response.json()
                    rename_key = f"live-agent-effects-rename-{uuid4().hex}"
                    rename_url = (
                        f"{productflow_url}/api/internal/v1/agent-conversations/"
                        f"{product_conversation_id}/asset-renames"
                    )
                    assert _post_and_drop_response_body(
                        rename_url,
                        token=token,
                        idempotency_key=rename_key,
                        payload=rename_payload,
                    ) == 200

                    rename_reconcile = client.post(
                        rename_url + "/reconcile",
                        headers=_internal_headers(token, rename_key),
                        json=rename_payload,
                    )
                    assert rename_reconcile.status_code == 200, rename_reconcile.text
                    rename_result = rename_reconcile.json()
                    assert rename_result["state"] == "applied"
                    assert rename_result["result"]["asset_id"] == asset_id

                    rename_replay = client.post(
                        rename_url,
                        headers=_internal_headers(token, rename_key),
                        json=rename_payload,
                    )
                    assert rename_replay.status_code == 200, rename_replay.text
                    assert rename_replay.json() == rename_result["result"]

                with session_factory() as session:
                    workspace_conversation = session.scalar(
                        sa.select(AgentConversation).where(
                            AgentConversation.creation_idempotency_key == workspace_key
                        )
                    )
                    assert workspace_conversation is not None
                    assert workspace_conversation.product_id == reconciled_product_id
                    assert session.scalar(
                        sa.select(sa.func.count())
                        .select_from(AgentConversation)
                        .where(AgentConversation.creation_idempotency_key == workspace_key)
                    ) == 1
                    assert session.scalar(
                        sa.select(sa.func.count())
                        .select_from(AgentWorkflowRunRequest)
                        .where(AgentWorkflowRunRequest.idempotency_key == request_key)
                    ) == 1
                    asset = session.get(ProductImageAsset, asset_id)
                    assert asset is not None
                    assert asset.display_name == rename_result["result"]["display_name"]
                    assert session.scalar(
                        sa.select(sa.func.count())
                        .select_from(AgentToolMutation)
                        .where(
                            AgentToolMutation.conversation_id == product_conversation_id,
                            AgentToolMutation.idempotency_key == rename_key,
                        )
                    ) == 1
                    assert product_id != reconciled_product_id
        finally:
            if api_server is not None:
                api_server.should_exit = True
            if api_thread is not None:
                api_thread.join(timeout=15)
            _reset_database_state()
