from __future__ import annotations

from datetime import UTC, datetime

import pytest
from fastapi.testclient import TestClient
from helpers import _login, _make_demo_image_bytes
from sqlalchemy import event, func, select
from productflow_backend.application.agent.conversations import (
    reserve_agent_turn,
)
from productflow_backend.application.agent.product_intake import AgentProductSelectionV1
from productflow_backend.application.agent.product_workspaces import (
    attach_agent_workspace_to_product,
    create_agent_product_workspace,
    finalize_agent_product_workspace_intake_from_assets,
)
from productflow_backend.application.agent.tools import (
    get_agent_contract,
    get_agent_product_context,
    validate_agent_workflow_draft,
)
from productflow_backend.application.agent.workbenches import (
    AgentWorkbenchBootstrap,
    ensure_agent_workbench_bootstrap,
    get_agent_workbench_bootstrap,
)
from productflow_backend.application.product_workflow.graph_commands import get_active_workflow_graph
from productflow_backend.application.product_workflow.graph_direct_create import create_product_with_direct_graph
from productflow_backend.application.product_workflow.graph_template import DirectCreateImageType
from productflow_backend.domain.errors import ConflictError
from productflow_backend.infrastructure.db.models import (
    AgentConversation,
    WorkflowDraft,
    WorkflowGraphEdge,
    WorkflowGraphNode,
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

    assert isinstance(bootstrap, AgentWorkbenchBootstrap)
    assert bootstrap.conversation.id == workspace.conversation.id
    assert bootstrap.workflow_draft is None
    assert bootstrap.conversation.workflow_draft_id is None
    assert bootstrap.graph is not None
    assert bootstrap.latest_workflow_revision >= 1
    assert flushes == 0
    assert commits == 0
    assert db_session.scalar(select(func.count()).select_from(WorkflowGraphNode)) > 0


def test_agent_workbench_bootstrap_selects_latest_conversation_by_created_at_and_id(db_session) -> None:
    workspace = _create_agent_workspace(db_session, key="latest-conversation")
    second = attach_agent_workspace_to_product(
        db_session,
        product_id=workspace.product.id,
        idempotency_key="latest-conversation-second",
        force_new=True,
    )
    tied_at = datetime(2026, 8, 14, 9, 0, tzinfo=UTC)
    workspace.conversation.created_at = tied_at
    second.conversation.created_at = tied_at
    db_session.commit()

    bootstrap = get_agent_workbench_bootstrap(db_session, product_id=workspace.product.id)
    assert isinstance(bootstrap, AgentWorkbenchBootstrap)
    expected = max((workspace.conversation, second.conversation), key=lambda item: item.id)
    assert bootstrap.conversation.id == expected.id
    assert bootstrap.workflow_draft is None


def test_agent_workbench_bootstrap_rejects_product_without_agent_workspace(db_session) -> None:
    workspace = _create_agent_workspace(db_session, key="missing-workspace")
    db_session.delete(workspace.conversation)
    db_session.commit()

    with pytest.raises(ConflictError, match="还没有 Agent 工作区"):
        get_agent_workbench_bootstrap(db_session, product_id=workspace.product.id)


def test_ensure_agent_workbench_attaches_conversation_to_direct_created_graph(db_session) -> None:
    created = create_product_with_direct_graph(
        db_session,
        name="直接创建工作台商品",
        category=None,
        price=None,
        source_note=None,
        image_uploads=[(_make_demo_image_bytes(), "reference.png", "image/png")],
        image_types=[DirectCreateImageType(key="hero", quantity=1, order=0)],
    )
    with pytest.raises(ConflictError, match="还没有 Agent 工作区"):
        get_agent_workbench_bootstrap(db_session, product_id=created.product.id)

    first = ensure_agent_workbench_bootstrap(
        db_session,
        product_id=created.product.id,
        idempotency_key="ensure-direct-graph",
    )
    second = ensure_agent_workbench_bootstrap(
        db_session,
        product_id=created.product.id,
        idempotency_key="ensure-direct-graph",
    )
    loaded = get_agent_workbench_bootstrap(db_session, product_id=created.product.id)

    assert first.conversation.id == second.conversation.id == loaded.conversation.id
    assert first.graph is not None
    assert first.graph.id == created.graph.id
    assert loaded.conversation.product_id == created.product.id
    assert loaded.workflow_draft is None


def test_direct_created_graph_allows_agent_turn_without_draft_intake(db_session) -> None:
    created = create_product_with_direct_graph(
        db_session,
        name="直接创建后对话",
        category=None,
        price=None,
        source_note=None,
        image_uploads=[(_make_demo_image_bytes(), "reference.png", "image/png")],
        image_types=[DirectCreateImageType(key="hero", quantity=1, order=0)],
    )
    bootstrap = ensure_agent_workbench_bootstrap(
        db_session,
        product_id=created.product.id,
        idempotency_key="direct-graph-turn",
    )

    reservation = reserve_agent_turn(
        db_session,
        product_id=created.product.id,
        conversation_id=bootstrap.conversation.id,
        input_text="这张图画了什么",
        input_asset_ids=[],
        idempotency_key="direct-graph-first-turn",
    )

    assert reservation.created is True
    contract = get_agent_contract(db_session, bootstrap.conversation.id)
    assert "不得调用 propose_workflow_draft" in contract["system_prompt"]
    assert "request_workflow_run_v1" in contract["system_prompt"]
    context = get_agent_product_context(db_session, bootstrap.conversation.id)
    assert context["live_graph"] is not None
    assert context["live_graph"]["id"] == created.graph.id
    assert any(node["node_type"] == "image_generation" for node in context["live_graph"]["nodes"])
    assert context["workflow_draft"] is None
    assert context["intake"] is None
    with pytest.raises(ConflictError, match="不再使用 WorkflowDraft"):
        validate_agent_workflow_draft(
            db_session,
            conversation_id=bootstrap.conversation.id,
            value={},
        )


def test_live_graph_blocks_conversation_intake(db_session) -> None:
    created = create_product_with_direct_graph(
        db_session,
        name="已有画布不能再提交创建输入",
        category=None,
        price=None,
        source_note=None,
        image_uploads=[(_make_demo_image_bytes(), "reference.png", "image/png")],
        image_types=[DirectCreateImageType(key="hero", quantity=1, order=0)],
    )
    bootstrap = ensure_agent_workbench_bootstrap(
        db_session,
        product_id=created.product.id,
        idempotency_key="live-graph-blocks-intake",
    )
    graph_id = created.graph.id
    finalized = finalize_agent_product_workspace_intake_from_assets(
        db_session,
        conversation_id=bootstrap.conversation.id,
        selection=AgentProductSelectionV1.model_validate(
            {
                "schema_version": 1,
                "image_types": [{"key": "hero", "quantity": 1, "order": 0}],
            }
        ),
        reference_asset_ids=[created.created_assets[0].id],
        idempotency_key="live-graph-intake",
    )
    assert finalized.product.intake_json is not None
    assert get_active_workflow_graph(db_session, product_id=created.product.id).id == graph_id


def test_agent_workbench_bootstrap_exposes_persisted_graph(db_session) -> None:
    workspace = _create_agent_workspace(db_session, key="agent-active-v2")
    live = get_active_workflow_graph(db_session, product_id=workspace.product.id)
    assert live is not None
    bootstrap = get_agent_workbench_bootstrap(db_session, product_id=workspace.product.id)
    assert isinstance(bootstrap, AgentWorkbenchBootstrap)
    assert bootstrap.graph is not None
    assert bootstrap.graph.id == live.id
    assert bootstrap.graph.revision == live.revision
    assert bootstrap.latest_workflow_revision == live.revision


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
            for model in (WorkflowDraft, AgentConversation, WorkflowGraphNode, WorkflowGraphEdge)
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
    assert agent_response.json()["mode"] == "agent"
    assert agent_response.json()["workflow_draft"] is None
    assert agent_response.json()["product"]["intake"]["schema_version"] == 1
    assert agent_response.json()["graph"] is not None
    assert "active_workflow" not in agent_response.json()
    assert missing_response.status_code == 409
    assert missing_response.json()["detail"] == "商品还没有 Agent 工作区"
    assert dml_statements == []

    with factory() as session:
        counts_after = {
            model.__tablename__: session.scalar(select(func.count()).select_from(model))
            for model in (WorkflowDraft, AgentConversation, WorkflowGraphNode, WorkflowGraphEdge)
        }
    assert counts_after == counts_before


def test_ensure_agent_workbench_api_creates_workspace_for_direct_created_product(configured_env) -> None:
    from productflow_backend.infrastructure.db.session import get_session_factory
    from productflow_backend.presentation.api import create_app

    factory = get_session_factory()
    with factory() as session:
        created = create_product_with_direct_graph(
            session,
            name="API 直接创建工作台商品",
            category=None,
            price=None,
            source_note=None,
            image_uploads=[(_make_demo_image_bytes(), "reference.png", "image/png")],
            image_types=[DirectCreateImageType(key="hero", quantity=1, order=0)],
        )
        product_id = created.product.id

    client = TestClient(create_app())
    _login(client)
    missing = client.get(f"/api/v2/products/{product_id}/agent-workbench")
    assert missing.status_code == 409, missing.text
    created_workspace = client.post(
        f"/api/v2/products/{product_id}/agent-workbench",
        headers={"Idempotency-Key": f"agent-workbench:{product_id}"},
    )
    assert created_workspace.status_code == 200, created_workspace.text
    payload = created_workspace.json()
    assert payload["mode"] == "agent"
    assert payload["graph"]["id"]
    replay = client.post(
        f"/api/v2/products/{product_id}/agent-workbench",
        headers={"Idempotency-Key": f"agent-workbench:{product_id}"},
    )
    assert replay.status_code == 200, replay.text
    assert replay.json()["conversation"]["id"] == payload["conversation"]["id"]
    loaded = client.get(f"/api/v2/products/{product_id}/agent-workbench")
    assert loaded.status_code == 200, loaded.text
    assert loaded.json()["conversation"]["id"] == payload["conversation"]["id"]


def test_ensure_agent_workbench_api_bootstraps_graph_product_without_get_409(configured_env) -> None:
    from productflow_backend.infrastructure.db.session import get_session_factory
    from productflow_backend.presentation.api import create_app

    factory = get_session_factory()
    with factory() as session:
        created = create_product_with_direct_graph(
            session,
            name="首次确保工作台商品",
            category=None,
            price=None,
            source_note=None,
            image_uploads=[(_make_demo_image_bytes(), "reference.png", "image/png")],
            image_types=[DirectCreateImageType(key="hero", quantity=1, order=0)],
        )
        product_id = created.product.id

    client = TestClient(create_app())
    _login(client)
    first = client.post(
        f"/api/v2/products/{product_id}/agent-workbench",
        headers={"Idempotency-Key": f"agent-workbench:{product_id}"},
    )
    assert first.status_code == 200, first.text
    assert first.json()["mode"] == "agent"
    assert first.json()["graph"]["id"]
