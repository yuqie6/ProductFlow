from __future__ import annotations

from datetime import UTC, datetime

import pytest
from fastapi.testclient import TestClient
from helpers import _login, _make_demo_image_bytes
from sqlalchemy import select

from productflow_backend.application.agent.agent_context import get_agent_contract
from productflow_backend.application.agent.product_workspaces import create_agent_product_workspace
from productflow_backend.application.agent.sessions import (
    AGENT_SESSION_DEFAULT_TITLE,
    archive_agent_session,
    create_agent_session,
    list_agent_sessions,
    rename_agent_session,
)
from productflow_backend.application.agent.tasks import create_agent_task
from productflow_backend.application.agent.turn_projection import reserve_agent_turn
from productflow_backend.application.agent.workbenches import get_agent_workbench_bootstrap
from productflow_backend.application.product_intake import AgentProductSelectionV1
from productflow_backend.domain.enums import AgentConversationScope
from productflow_backend.domain.errors import ConflictError
from productflow_backend.infrastructure.db.models import AgentConversation, AgentSession
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
    dock = list_agent_sessions(db_session)
    canvas = list_agent_sessions(db_session, product_id=workspace.product.id)

    assert dock == []
    assert len(canvas) == 1
    assert canvas[0].status.value == "active"
    assert canvas[0].title == workspace.product.name
    assert canvas[0].product_id == workspace.product.id
    assert len(canvas[0].conversations) == 1
    assert canvas[0].conversations[0].id == workspace.conversation.id
    assert canvas[0].conversations[0].scope_type == AgentConversationScope.PRODUCT_WORKFLOW


def test_agent_session_list_prioritizes_recent_conversation_activity(db_session) -> None:
    older = _create_workspace(db_session, key="session-older")
    newer = _create_workspace(db_session, key="session-newer")
    older.conversation.updated_at = datetime(2020, 1, 1, tzinfo=UTC)
    newer.conversation.updated_at = datetime(2030, 1, 1, tzinfo=UTC)
    db_session.commit()

    assert list_agent_sessions(db_session) == []
    assert [item.id for item in list_agent_sessions(db_session, product_id=newer.product.id)] == [
        newer.conversation.session_id
    ]
    assert [item.id for item in list_agent_sessions(db_session, product_id=older.product.id)] == [
        older.conversation.session_id
    ]


def test_agent_session_list_prioritizes_recent_task_activity(db_session) -> None:
    older = _create_workspace(db_session, key="session-task-older")
    newer = _create_workspace(db_session, key="session-task-newer")
    older.conversation.updated_at = datetime(2020, 1, 1, tzinfo=UTC)
    newer.conversation.updated_at = datetime(2020, 1, 2, tzinfo=UTC)
    db_session.commit()

    create_agent_task(
        db_session,
        session_id=older.conversation.session_id,
        conversation_id=older.conversation.id,
        title="最近的任务",
        goal="检查最近任务活动是否影响 Session 排序",
    )

    assert list_agent_sessions(db_session) == []
    assert [item.id for item in list_agent_sessions(db_session, product_id=older.product.id)] == [
        older.conversation.session_id
    ]
    assert [item.id for item in list_agent_sessions(db_session, product_id=newer.product.id)] == [
        newer.conversation.session_id
    ]


def test_agent_session_rename_and_archive_keep_conversations(db_session) -> None:
    created = create_agent_session(db_session, title="春季素材")

    renamed = rename_agent_session(db_session, session_id=created.id, title="春季素材 v2")
    archived = archive_agent_session(db_session, session_id=renamed.id)

    assert archived.title == "春季素材 v2"
    assert archived.status.value == "archived"
    assert archived.archived_at is not None
    assert list_agent_sessions(db_session) == []
    assert list_agent_sessions(db_session, include_archived=True)[0].id == created.id


def test_new_agent_session_has_one_global_conversation_and_contract(db_session) -> None:
    created = create_agent_session(db_session, title="全局素材整理")

    conversation = db_session.scalar(
        select(AgentConversation).where(AgentConversation.session_id == created.id)
    )
    assert conversation is not None
    assert conversation.scope_type == AgentConversationScope.GLOBAL
    assert conversation.product_id is None
    assert conversation.workflow_draft_id is None

    contract = get_agent_contract(db_session, conversation.id)
    assert contract["scope_type"] == AgentConversationScope.GLOBAL
    assert contract["product_id"] is None
    assert contract["workflow_draft_id"] is None
    assert contract["workflow_draft_schema"] == {}

    task = create_agent_task(
        db_session,
        session_id=created.id,
        conversation_id=conversation.id,
        title="检查全局素材",
        goal="找出没有标签的图片",
    )
    assert task.product_id is None
    assert task.workflow_draft_id is None


def test_agent_session_name_is_derived_from_first_global_turn_without_overwriting_manual_name(db_session) -> None:
    created = create_agent_session(db_session)
    assert created.title == AGENT_SESSION_DEFAULT_TITLE

    conversation = created.conversations[0]
    reserve_agent_turn(
        db_session,
        product_id=None,
        conversation_id=conversation.id,
        input_text="请帮我整理春季新品主图和卖点图。需要保持商品事实不变。",
        input_asset_ids=[],
        idempotency_key="auto-session-title",
    )
    db_session.refresh(created)
    assert created.title == "请帮我整理春季新品主图和卖点图"

    named = create_agent_session(db_session)
    rename_agent_session(db_session, session_id=named.id, title="人工指定名称")
    reserve_agent_turn(
        db_session,
        product_id=None,
        conversation_id=named.conversations[0].id,
        input_text="这条消息不应覆盖人工指定的名称",
        input_asset_ids=[],
        idempotency_key="manual-session-title",
    )
    db_session.refresh(named)
    assert named.title == "人工指定名称"


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

    dock = client.get("/api/v2/agent-sessions?include_archived=true")
    assert dock.status_code == 200, dock.text
    assert dock.json()["items"] == []
    response = client.get(
        f"/api/v2/agent-sessions?include_archived=true&product_id={product_id}"
    )
    assert response.status_code == 200, response.text
    payload = response.json()
    assert payload["items"][0]["id"] == session_id
    assert payload["items"][0]["product_id"] == product_id
    assert any(
        conversation["product_id"] == product_id
        for conversation in payload["items"][0]["conversations"]
    )
    assert all(
        conversation["scope_type"] == AgentConversationScope.PRODUCT_WORKFLOW.value
        for conversation in payload["items"][0]["conversations"]
    )

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


def test_agent_session_api_creates_without_a_title(configured_env) -> None:
    client = TestClient(create_app())
    _login(client)

    response = client.post("/api/v2/agent-sessions")

    assert response.status_code == 201, response.text
    assert response.json()["title"] == AGENT_SESSION_DEFAULT_TITLE
    assert response.json()["conversations"][0]["scope_type"] == AgentConversationScope.GLOBAL.value
