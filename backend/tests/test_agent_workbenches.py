from __future__ import annotations

from datetime import UTC, datetime

import pytest
from fastapi.testclient import TestClient
from helpers import _login, _make_demo_image_bytes
from sqlalchemy import event, func, select
from workflow_draft_helpers import make_workflow_draft_payload

from productflow_backend.application.agent.conversations import create_agent_conversation
from productflow_backend.application.agent.product_intake import AgentProductSelectionV1
from productflow_backend.application.agent.product_workspaces import create_agent_product_workspace
from productflow_backend.application.agent.workbenches import (
    AgentV2WorkbenchBootstrap,
    get_agent_workbench_bootstrap,
)
from productflow_backend.application.workflow_drafts.materialization import materialize_workflow_draft
from productflow_backend.application.workflow_drafts.service import (
    append_workflow_draft_revision,
    confirm_workflow_draft_revision,
    create_workflow_draft,
)
from productflow_backend.domain.errors import ConflictError
from productflow_backend.infrastructure.db.models import (
    AgentConversation,
    ProductWorkflow,
    WorkflowDraft,
    WorkflowEdge,
    WorkflowNode,
)


def _create_agent_workspace(db_session, *, key: str = "workbench-agent"):
    selection = AgentProductSelectionV1.model_validate(
        {
            "schema_version": 1,
            "image_types": [{"key": "hero", "quantity": 2, "order": 0}],
        }
    )
    return create_agent_product_workspace(
        db_session,
        name=f"Agent 工作台商品 {key}",
        selection=selection,
        image_uploads=[(_make_demo_image_bytes(), "reference.png", "image/png")],
        idempotency_key=key,
    )


def test_agent_workbench_bootstrap_uses_persisted_conversation_and_is_read_only(db_session) -> None:
    workspace = _create_agent_workspace(db_session)
    flushes = 0
    commits = 0

    def record_flush(_session, _flush_context, _instances) -> None:
        nonlocal flushes
        flushes += 1

    def record_commit(_session) -> None:
        nonlocal commits
        commits += 1

    event.listen(db_session, "before_flush", record_flush)
    event.listen(db_session, "before_commit", record_commit)
    try:
        bootstrap = get_agent_workbench_bootstrap(
            db_session,
            product_id=workspace.product.id,
        )
    finally:
        event.remove(db_session, "before_flush", record_flush)
        event.remove(db_session, "before_commit", record_commit)

    assert isinstance(bootstrap, AgentV2WorkbenchBootstrap)
    assert bootstrap.conversation.id == workspace.conversation.id
    assert bootstrap.workflow_draft.id == workspace.workflow_draft.id
    assert bootstrap.active_workflow.workflow is None
    assert bootstrap.active_workflow.latest_revision == 0
    assert flushes == 0
    assert commits == 0
    assert db_session.scalar(select(func.count()).select_from(ProductWorkflow)) == 0
    assert db_session.scalar(select(func.count()).select_from(WorkflowNode)) == 0
    assert db_session.scalar(select(func.count()).select_from(WorkflowEdge)) == 0


def test_agent_workbench_bootstrap_selects_latest_conversation_by_created_at_and_id(db_session) -> None:
    workspace = _create_agent_workspace(db_session, key="latest-conversation")
    payload = make_workflow_draft_payload(reference_asset_id=workspace.created_assets[0].id)
    second_draft = create_workflow_draft(
        db_session,
        product_id=workspace.product.id,
        payload=payload,
        ready_for_confirmation=False,
    )
    second_conversation = create_agent_conversation(
        db_session,
        product_id=workspace.product.id,
        workflow_draft_id=second_draft.id,
    )
    tied_at = datetime(2026, 8, 14, 9, 0, tzinfo=UTC)
    workspace.conversation.created_at = tied_at
    second_conversation.created_at = tied_at
    db_session.commit()

    bootstrap = get_agent_workbench_bootstrap(db_session, product_id=workspace.product.id)
    assert isinstance(bootstrap, AgentV2WorkbenchBootstrap)
    expected = max((workspace.conversation, second_conversation), key=lambda item: item.id)
    assert bootstrap.conversation.id == expected.id
    assert bootstrap.workflow_draft.id == expected.workflow_draft_id


def test_agent_workbench_bootstrap_rejects_product_without_agent_workspace(db_session) -> None:
    workspace = _create_agent_workspace(db_session, key="missing-workspace")
    db_session.delete(workspace.conversation)
    db_session.commit()

    with pytest.raises(ConflictError, match="还没有 Agent 工作区"):
        get_agent_workbench_bootstrap(db_session, product_id=workspace.product.id)


def test_agent_workbench_bootstrap_returns_complete_active_v2_snapshot(db_session) -> None:
    workspace = _create_agent_workspace(db_session, key="agent-active-v2")
    asset_id = workspace.created_assets[0].id
    draft = append_workflow_draft_revision(
        db_session,
        product_id=workspace.product.id,
        draft_id=workspace.workflow_draft.id,
        expected_draft_version=0,
        payload=make_workflow_draft_payload(reference_asset_id=asset_id),
        ready_for_confirmation=True,
        source_turn_id="workbench-turn",
        source_artifact_step_id="workbench-artifact",
    )
    confirm_workflow_draft_revision(
        db_session,
        product_id=workspace.product.id,
        draft_id=draft.id,
        expected_draft_version=1,
    )
    materialized = materialize_workflow_draft(
        db_session,
        product_id=workspace.product.id,
        draft_id=draft.id,
        expected_draft_version=1,
        expected_workflow_revision=0,
        idempotency_key="workbench-materialize",
    )

    bootstrap = get_agent_workbench_bootstrap(db_session, product_id=workspace.product.id)
    assert isinstance(bootstrap, AgentV2WorkbenchBootstrap)
    assert bootstrap.active_workflow.workflow is not None
    assert bootstrap.active_workflow.workflow.id == materialized.workflow.id
    assert bootstrap.active_workflow.latest_revision == 1
    assert len(bootstrap.active_workflow.workflow.nodes) == len(materialized.workflow.nodes)


def test_agent_workbench_bootstrap_api_has_no_database_write_side_effects(configured_env) -> None:
    from productflow_backend.infrastructure.db.session import get_engine, get_session_factory
    from productflow_backend.presentation.api import create_app

    factory = get_session_factory()
    with factory() as session:
        agent_workspace = _create_agent_workspace(session, key="bootstrap-api-agent")
        missing_workspace = _create_agent_workspace(session, key="bootstrap-api-missing")
        session.delete(missing_workspace.conversation)
        session.commit()
        product_ids = (agent_workspace.product.id, missing_workspace.product.id)
        counts_before = {
            model.__tablename__: session.scalar(select(func.count()).select_from(model))
            for model in (ProductWorkflow, WorkflowDraft, AgentConversation, WorkflowNode, WorkflowEdge)
        }

    client = TestClient(create_app())
    _login(client)
    dml_statements: list[str] = []

    def record_dml(_connection, _cursor, statement, _parameters, _context, _executemany) -> None:
        normalized = statement.lstrip().upper()
        if normalized.startswith(("INSERT", "UPDATE", "DELETE")):
            dml_statements.append(statement)

    engine = get_engine()
    event.listen(engine, "before_cursor_execute", record_dml)
    try:
        agent_response = client.get(f"/api/v2/products/{product_ids[0]}/agent-workbench")
        missing_response = client.get(f"/api/v2/products/{product_ids[1]}/agent-workbench")
    finally:
        event.remove(engine, "before_cursor_execute", record_dml)

    assert agent_response.status_code == 200, agent_response.text
    assert agent_response.json()["mode"] == "agent_v2"
    assert agent_response.json()["workflow_draft"]["intake"]["schema_version"] == 1
    assert agent_response.json()["active_workflow"] is None
    assert missing_response.status_code == 409
    assert missing_response.json()["detail"] == "商品还没有 Agent 工作区"
    assert dml_statements == []

    with factory() as session:
        counts_after = {
            model.__tablename__: session.scalar(select(func.count()).select_from(model))
            for model in (ProductWorkflow, WorkflowDraft, AgentConversation, WorkflowNode, WorkflowEdge)
        }
    assert counts_after == counts_before
