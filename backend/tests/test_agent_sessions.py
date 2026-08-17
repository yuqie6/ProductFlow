from __future__ import annotations

from datetime import UTC, datetime

import pytest
from fastapi.testclient import TestClient
from helpers import _login, _make_demo_image_bytes

from productflow_backend.application.agent_product_intake import AgentProductSelectionV1
from productflow_backend.application.agent_product_workspaces import create_agent_product_workspace
from productflow_backend.application.agent_sessions import (
    archive_agent_session,
    create_agent_session,
    list_agent_sessions,
    rename_agent_session,
)
from productflow_backend.application.agent_workbenches import get_agent_workbench_bootstrap
from productflow_backend.domain.errors import ConflictError
from productflow_backend.infrastructure.db.models import AgentSession
from productflow_backend.presentation.api import create_app


def _create_workspace(db_session, *, key: str):
    selection = AgentProductSelectionV1.model_validate(
        {
            "schema_version": 1,
            "image_types": [{"key": "hero", "quantity": 2, "order": 0}],
        }
    )
    return create_agent_product_workspace(
        db_session,
        name=f"Agent Session 商品 {key}",
        selection=selection,
        image_uploads=[(_make_demo_image_bytes(), "reference.png", "image/png")],
        idempotency_key=key,
    )


def test_product_workspace_creates_an_active_global_agent_session(db_session) -> None:
    workspace = _create_workspace(db_session, key="session-created")

    assert workspace.conversation.session_id is not None
    sessions = list_agent_sessions(db_session)

    assert len(sessions) == 1
    assert sessions[0].status.value == "active"
    assert sessions[0].title == workspace.product.name
    assert sessions[0].conversations[0].id == workspace.conversation.id


def test_agent_session_list_prioritizes_recent_conversation_activity(db_session) -> None:
    older = _create_workspace(db_session, key="session-older")
    newer = _create_workspace(db_session, key="session-newer")
    older.conversation.updated_at = datetime(2020, 1, 1, tzinfo=UTC)
    newer.conversation.updated_at = datetime(2030, 1, 1, tzinfo=UTC)
    db_session.commit()

    sessions = list_agent_sessions(db_session)

    assert [item.id for item in sessions] == [newer.conversation.session_id, older.conversation.session_id]


def test_agent_session_rename_and_archive_keep_conversations(db_session) -> None:
    created = create_agent_session(db_session, title="春季素材")

    renamed = rename_agent_session(db_session, session_id=created.id, title="春季素材 v2")
    archived = archive_agent_session(db_session, session_id=renamed.id)

    assert archived.title == "春季素材 v2"
    assert archived.status.value == "archived"
    assert archived.archived_at is not None
    assert list_agent_sessions(db_session) == []
    assert list_agent_sessions(db_session, include_archived=True)[0].id == created.id


def test_session_scoped_workbench_does_not_fall_back_to_another_session(db_session) -> None:
    first = _create_workspace(db_session, key="session-product-a")
    second = _create_workspace(db_session, key="session-product-b")

    scoped = get_agent_workbench_bootstrap(
        db_session,
        product_id=first.product.id,
        agent_session_id=first.conversation.session_id,
    )
    assert scoped.conversation.id == first.conversation.id

    with pytest.raises(ConflictError, match="没有这个商品的工作区"):
        get_agent_workbench_bootstrap(
            db_session,
            product_id=first.product.id,
            agent_session_id=second.conversation.session_id,
        )


def test_agent_session_api_returns_bounded_session_projection_and_mutations(configured_env) -> None:
    from productflow_backend.infrastructure.db.session import get_session_factory

    factory = get_session_factory()
    with factory() as session:
        workspace = _create_workspace(session, key="session-api")
        session_id = workspace.conversation.session_id
        product_id = workspace.product.id

    client = TestClient(create_app())
    _login(client)

    response = client.get("/api/v2/agent-sessions?include_archived=true")
    assert response.status_code == 200, response.text
    payload = response.json()
    assert payload["items"][0]["id"] == session_id
    assert payload["items"][0]["conversations"][0]["product_id"] == product_id

    rename_response = client.patch(
        f"/api/v2/agent-sessions/{session_id}",
        json={"title": "API 重命名"},
    )
    assert rename_response.status_code == 200, rename_response.text
    assert rename_response.json()["title"] == "API 重命名"

    archive_response = client.post(f"/api/v2/agent-sessions/{session_id}/archive")
    assert archive_response.status_code == 200, archive_response.text
    assert archive_response.json()["status"] == "archived"

    active_response = client.get("/api/v2/agent-sessions")
    assert active_response.status_code == 200, active_response.text
    assert active_response.json()["items"] == []

    with factory() as session:
        persisted = session.get(AgentSession, session_id)
        assert persisted is not None
        assert persisted.status.value == "archived"
