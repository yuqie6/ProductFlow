from __future__ import annotations

import json
from pathlib import Path

import pytest
from fastapi.testclient import TestClient
from helpers import _login, _make_demo_image_bytes
from pydantic import ValidationError
from sqlalchemy import event, func, select

from productflow_backend.application.agent.agent_context import (
    get_agent_contract,
    get_agent_product_context,
)
from productflow_backend.application.agent.gallery_tools import finalize_agent_product_intake
from productflow_backend.application.agent.product_workspaces import (
    attach_agent_workspace_to_product,
    create_agent_product_draft_workspace,
    create_agent_product_draft_workspace_from_global_conversation,
    create_agent_product_workspace,
    finalize_agent_product_workspace_intake,
    finalize_agent_product_workspace_intake_from_assets,
    get_agent_product_workspace,
    reconcile_agent_product_draft_workspace_from_global_conversation,
    reconcile_agent_product_intake_from_assets,
)
from productflow_backend.application.agent.sessions import create_agent_session, list_agent_sessions
from productflow_backend.application.agent.turn_projection import reserve_agent_turn
from productflow_backend.application.delivery_renditions.presets import get_delivery_preset
from productflow_backend.application.product_intake import (
    AgentProductSelectionV1,
    WorkflowIntakeV1,
    parse_agent_product_selection,
)
from productflow_backend.application.product_workflow.graph_commands import (
    apply_graph_change_set,
    get_active_workflow_graph,
    load_applied_graph,
)
from productflow_backend.application.product_workflow.graph_contracts import CreateNodeOp, WorkflowChangeSet
from productflow_backend.application.product_workflow.graph_direct_create import create_product_with_direct_graph
from productflow_backend.application.product_workflow.graph_template import DirectCreateImageType
from productflow_backend.application.products import add_canonical_product_images
from productflow_backend.domain.enums import (
    AgentConversationScope,
    GraphActorType,
    GraphNodeType,
)
from productflow_backend.domain.errors import BusinessValidationError, ConflictError
from productflow_backend.domain.image_type_catalog import AGENT_PRODUCT_IMAGE_TYPE_CATALOG
from productflow_backend.infrastructure.db.models import (
    AgentConversation,
    AgentSession,
    AgentTurnProjection,
    MediaObject,
    Product,
    ProductImageAsset,
    WorkflowDraft,
    WorkflowDraftRevision,
    WorkflowGraphNode,
)
from productflow_backend.infrastructure.storage import LocalStorage


def _selection(*items: tuple[str, int]) -> AgentProductSelectionV1:
    return AgentProductSelectionV1.model_validate(
        {
            "schema_version": 1,
            "image_types": [
                {"key": key, "quantity": quantity, "order": order}
                for order, (key, quantity) in enumerate(items)
            ],
        }
    )


def _workspace_uploads() -> list[tuple[bytes, str, str]]:
    content = _make_demo_image_bytes()
    return [
        (content, "front.png", "image/png"),
        (content, "detail.png", "image/png"),
    ]


def _media_files(storage_root: Path) -> list[Path]:
    return [path for path in storage_root.glob("media/**/*") if path.is_file()]


def test_agent_product_image_type_catalog_and_selection_contract_are_strict() -> None:
    assert [(item.key, item.order) for item in AGENT_PRODUCT_IMAGE_TYPE_CATALOG] == [
        ("hero", 0),
        ("selling_point", 1),
        ("scene", 2),
        ("detail", 3),
        ("sku", 4),
        ("dimensions", 5),
        ("specifications", 6),
        ("after_sales", 7),
        ("brand_story", 8),
        ("precautions", 9),
        ("certification", 10),
        ("faq", 11),
        ("factory", 12),
        ("packaging", 13),
        ("shipping", 14),
    ]
    assert _selection(("hero", 2)).model_dump(mode="json") == {
        "schema_version": 1,
        "image_types": [{"key": "hero", "quantity": 2, "order": 0}],
    }

    invalid_payloads = [
        {"schema_version": 1, "image_types": []},
        {"schema_version": 2, "image_types": [{"key": "hero", "quantity": 2, "order": 0}]},
        {"schema_version": 1, "image_types": [{"key": "unknown", "quantity": 2, "order": 0}]},
        {"schema_version": 1, "image_types": [{"key": "hero", "quantity": 0, "order": 0}]},
        {"schema_version": 1, "image_types": [{"key": "hero", "quantity": 7, "order": 0}]},
        {
            "schema_version": 1,
            "image_types": [
                {"key": "hero", "quantity": 2, "order": 0},
                {"key": "hero", "quantity": 2, "order": 1},
            ],
        },
        {
            "schema_version": 1,
            "image_types": [
                {"key": "hero", "quantity": 2, "order": 1},
                {"key": "scene", "quantity": 2, "order": 0},
            ],
        },
        {
            "schema_version": 1,
            "image_types": [
                {"key": "hero", "quantity": 6, "order": 0},
                {"key": "scene", "quantity": 6, "order": 1},
                {"key": "detail", "quantity": 6, "order": 2},
                {"key": "sku", "quantity": 6, "order": 3},
                {"key": "faq", "quantity": 6, "order": 4},
                {"key": "shipping", "quantity": 1, "order": 5},
            ],
        },
        {
            "schema_version": 1,
            "image_types": [{"key": "hero", "quantity": 2, "order": 0, "hidden": True}],
        },
    ]
    for payload in invalid_payloads:
        with pytest.raises(ValidationError):
            AgentProductSelectionV1.model_validate(payload)

    legacy_selection = {
        "schema_version": 1,
        "image_types": [{"key": "hero", "quantity": 2, "order": 0}],
    }
    legacy_intake = {
        **legacy_selection,
        "reference_asset_ids": ["asset-1"],
    }
    assert AgentProductSelectionV1.model_validate(legacy_selection).model_dump(mode="json") == legacy_selection
    assert WorkflowIntakeV1.model_validate(legacy_intake).model_dump(mode="json") == legacy_intake
    with pytest.raises(BusinessValidationError, match="图片类型选择"):
        parse_agent_product_selection(
            json.dumps({**legacy_selection, "delivery_preset_key": "not-a-real-preset"})
        )


def test_agent_intake_persists_delivery_preset_snapshot_and_changes_idempotency_hash(
    configured_env: Path,
    db_session,
) -> None:
    preset = get_delivery_preset("jd_hero")
    selected = AgentProductSelectionV1.model_validate(
        {
            "schema_version": 1,
            "image_types": [{"key": "hero", "quantity": 2, "order": 0}],
            "delivery_preset_key": preset.key,
        }
    )
    draft_workspace = create_agent_product_draft_workspace(
        db_session,
        name="带平台默认交付规格商品",
        idempotency_key="preset-draft-key",
    )
    creation = finalize_agent_product_workspace_intake(
        db_session,
        conversation_id=draft_workspace.conversation.id,
        selection=selected,
        image_uploads=_workspace_uploads(),
        idempotency_key="preset-intake-key",
    )

    expected_spec = preset.spec.model_dump(mode="json")
    assert creation.product.intake_json == {
        "schema_version": 1,
        "image_types": [{"key": "hero", "quantity": 2, "order": 0}],
        "reference_asset_ids": [asset.id for asset in creation.created_assets],
        "delivery_preset_key": "jd_hero",
        "delivery_spec": expected_spec,
    }
    context = get_agent_product_context(db_session, creation.conversation.id)
    assert context["intake"] == creation.product.intake_json
    assert "workflow_draft" not in context
    system_prompt = get_agent_contract(db_session, creation.conversation.id)["system_prompt"]
    assert "不得提交第二份完整拓扑" in system_prompt
    assert get_agent_contract(db_session, creation.conversation.id)["has_live_graph"] is True
    assert db_session.scalar(
        select(func.count()).select_from(WorkflowDraft).where(WorkflowDraft.product_id == creation.product.id)
    ) == 0

    with pytest.raises(ConflictError, match="已经确认"):
        finalize_agent_product_workspace_intake(
            db_session,
            conversation_id=draft_workspace.conversation.id,
            selection=AgentProductSelectionV1.model_validate(
                {
                    "schema_version": 1,
                    "image_types": [{"key": "hero", "quantity": 2, "order": 0}],
                    "delivery_preset_key": "taobao_tmall_hero",
                }
            ),
            image_uploads=_workspace_uploads(),
            idempotency_key="preset-intake-key",
        )


def test_create_agent_product_workspace_is_atomic_coverless_and_has_no_dag(
    configured_env: Path,
    db_session,
) -> None:
    creation = create_agent_product_workspace(
        db_session,
        name="  工业刀具收纳套装  ",
        selection=_selection(("hero", 3), ("detail", 2)),
        image_uploads=_workspace_uploads(),
        idempotency_key=" workspace-create-1 ",
    )

    assert creation.created is True
    assert creation.product.name == "工业刀具收纳套装"
    assert creation.product.cover_image_asset_id is None
    assert creation.product.current_fact_set_version_id is not None
    fact_set = creation.product.current_fact_set_version
    assert fact_set is not None
    assert any(
        item.get("key") == "product_name" and item.get("value") == "工业刀具收纳套装"
        for item in fact_set.payload_json.get("facts") or []
    )
    assert [asset.original_filename for asset in creation.created_assets] == ["front.png", "detail.png"]
    assert creation.conversation.workflow_draft_id is None
    assert creation.product.intake_schema_version == 1
    intake = WorkflowIntakeV1.model_validate(creation.product.intake_json)
    assert [(item.key, item.quantity, item.order) for item in intake.image_types] == [
        ("hero", 3, 0),
        ("detail", 2, 1),
    ]
    assert intake.reference_asset_ids == [asset.id for asset in creation.created_assets]
    assert creation.conversation.harness_run_id == creation.conversation.id
    assert db_session.scalar(
        select(func.count()).select_from(WorkflowDraft).where(WorkflowDraft.product_id == creation.product.id)
    ) == 0
    agent_session = db_session.get(AgentSession, creation.conversation.session_id)
    assert agent_session is not None
    assert agent_session.product_id == creation.product.id
    global_conversation = db_session.scalar(
        select(AgentConversation).where(
            AgentConversation.session_id == creation.conversation.session_id,
            AgentConversation.scope_type == AgentConversationScope.GLOBAL,
        )
    )
    assert global_conversation is None
    assert creation.conversation.creation_idempotency_key == "workspace-create-1"
    assert len(creation.conversation.creation_request_hash or "") == 64

    assert db_session.scalar(select(func.count()).select_from(Product)) == 1
    assert db_session.scalar(select(func.count()).select_from(ProductImageAsset)) == 2
    assert db_session.scalar(select(func.count()).select_from(MediaObject)) == 2
    assert db_session.scalar(select(func.count()).select_from(WorkflowDraftRevision)) == 0
    assert db_session.scalar(select(func.count()).select_from(WorkflowGraphNode)) > 0
    assert len(_media_files(configured_env)) == 6  # 两张原图，各带 preview 和 thumbnail


def test_agent_product_draft_workspace_creates_only_durable_identity_and_replays(db_session) -> None:
    first = create_agent_product_draft_workspace(
        db_session,
        name="  两阶段 Agent 商品  ",
        idempotency_key=" draft-workspace-1 ",
    )

    assert first.created is True
    assert first.product.name == "两阶段 Agent 商品"
    assert first.product.cover_image_asset_id is None
    assert first.product.current_fact_set_version_id is not None
    assert first.created_assets == []
    assert first.conversation.workflow_draft_id is None
    assert first.product.intake_schema_version is None
    assert first.product.intake_json is None
    assert first.conversation.creation_idempotency_key == "draft-workspace-1"
    assert first.conversation.intake_idempotency_key is None
    assert first.conversation.intake_request_hash is None
    agent_session = db_session.get(AgentSession, first.conversation.session_id)
    assert agent_session is not None
    assert agent_session.product_id == first.product.id
    assert db_session.scalar(
        select(func.count()).select_from(WorkflowDraft).where(WorkflowDraft.product_id == first.product.id)
    ) == 0
    assert agent_session.summary == "暂无 Agent Task"

    replay = create_agent_product_draft_workspace(
        db_session,
        name="两阶段 Agent 商品",
        idempotency_key="draft-workspace-1",
    )
    restored = get_agent_product_workspace(
        db_session,
        conversation_id=first.conversation.id,
    )
    assert replay.created is False
    assert replay.product.id == first.product.id
    assert restored.created is False
    assert restored.conversation.id == first.conversation.id
    assert restored.created_assets == []
    assert db_session.scalar(select(func.count()).select_from(Product)) == 1
    assert db_session.scalar(select(func.count()).select_from(ProductImageAsset)) == 0
    assert db_session.scalar(select(func.count()).select_from(MediaObject)) == 0
    assert db_session.scalar(select(func.count()).select_from(WorkflowDraftRevision)) == 0

    with pytest.raises(ConflictError, match="相同 Idempotency-Key"):
        create_agent_product_draft_workspace(
            db_session,
            name="不同商品",
            idempotency_key="draft-workspace-1",
        )
    graph = get_active_workflow_graph(db_session, product_id=first.product.id)
    assert graph is not None
    nodes = list(
        db_session.scalars(select(WorkflowGraphNode).where(WorkflowGraphNode.graph_id == graph.id))
    )
    assert [node.node_type for node in nodes] == [GraphNodeType.PRODUCT_SOURCE]
    assert db_session.scalar(
        select(func.count()).select_from(AgentTurnProjection).where(
            AgentTurnProjection.conversation_id == first.conversation.id
        )
    ) == 0
    contract = get_agent_contract(db_session, first.conversation.id)
    assert contract["has_live_graph"] is True
    assert "不得提交第二份完整拓扑" in contract["system_prompt"]


def test_direct_create_has_live_graph_without_conversation(configured_env: Path, db_session) -> None:
    created = create_product_with_direct_graph(
        db_session,
        name="直接创建无对话",
        category=None,
        price=None,
        source_note=None,
        image_uploads=_workspace_uploads()[:1],
        image_types=[DirectCreateImageType(key="hero", quantity=1, order=0)],
    )
    assert created.graph is not None
    assert db_session.scalar(
        select(func.count()).select_from(AgentConversation).where(
            AgentConversation.product_id == created.product.id
        )
    ) == 0
    assert list_agent_sessions(db_session) == []
    assert list_agent_sessions(db_session, product_id=created.product.id) == []


def test_canvas_session_list_omits_global_sessions_and_rejects_global_attach(db_session) -> None:
    global_session = create_agent_session(db_session, title="全局 Dock")
    workspace = create_agent_product_draft_workspace(
        db_session,
        name="画布会话商品",
        idempotency_key="canvas-own-1",
    )
    dock = list_agent_sessions(db_session)
    canvas = list_agent_sessions(db_session, product_id=workspace.product.id)
    assert [item.id for item in dock] == [global_session.id]
    assert [item.id for item in canvas] == [workspace.conversation.session_id]
    with pytest.raises(ConflictError, match="全局 Agent Session"):
        attach_agent_workspace_to_product(
            db_session,
            product_id=workspace.product.id,
            idempotency_key="attach-global",
            agent_session_id=global_session.id,
            force_new=True,
        )


def test_agent_product_draft_workspace_reconciliation_is_read_only_and_classifies_outcomes(db_session) -> None:
    session = create_agent_session(db_session, title="工作区对账")
    global_conversation = next(
        conversation
        for conversation in session.conversations
        if conversation.scope_type == AgentConversationScope.GLOBAL
    )

    missing = reconcile_agent_product_draft_workspace_from_global_conversation(
        db_session,
        global_conversation_id=global_conversation.id,
        name="尚未创建",
        idempotency_key="workspace-reconcile-missing",
    )
    assert missing.state == "not_applied"
    assert missing.creation is None

    created = create_agent_product_draft_workspace_from_global_conversation(
        db_session,
        global_conversation_id=global_conversation.id,
        name="需要对账的商品",
        idempotency_key="workspace-reconcile-applied",
    )
    reconciled = reconcile_agent_product_draft_workspace_from_global_conversation(
        db_session,
        global_conversation_id=global_conversation.id,
        name="需要对账的商品",
        idempotency_key="workspace-reconcile-applied",
    )
    assert reconciled.state == "applied"
    assert reconciled.creation is not None
    assert reconciled.creation.created is False
    assert reconciled.creation.conversation.id == created.conversation.id

    conflict = reconcile_agent_product_draft_workspace_from_global_conversation(
        db_session,
        global_conversation_id=global_conversation.id,
        name="另一个商品",
        idempotency_key="workspace-reconcile-applied",
    )
    assert conflict.state == "conflict"
    assert conflict.creation is None


def test_agent_product_workspace_can_join_existing_session_without_duplicate_global_conversation(db_session) -> None:
    agent_session = create_agent_session(db_session, title="春季商品素材")

    first = create_agent_product_draft_workspace(
        db_session,
        name="商品 A",
        idempotency_key="session-draft-a",
        agent_session_id=agent_session.id,
    )
    second = create_agent_product_draft_workspace(
        db_session,
        name="商品 B",
        idempotency_key="session-draft-b",
        agent_session_id=agent_session.id,
    )

    conversations = list(
        db_session.scalars(
            select(AgentConversation).where(AgentConversation.session_id == agent_session.id)
        ).all()
    )
    global_conversations = [
        conversation
        for conversation in conversations
        if conversation.scope_type == AgentConversationScope.GLOBAL
    ]
    product_conversations = [
        conversation
        for conversation in conversations
        if conversation.scope_type == AgentConversationScope.PRODUCT_WORKFLOW
    ]
    assert first.conversation.session_id != agent_session.id
    assert second.conversation.session_id != agent_session.id
    assert first.conversation.session_id != second.conversation.session_id
    assert db_session.get(AgentSession, first.conversation.session_id).product_id == first.product.id
    assert db_session.get(AgentSession, second.conversation.session_id).product_id == second.product.id
    assert len(global_conversations) == 1
    assert len(product_conversations) == 0

    other_session = create_agent_session(db_session, title="另一个会话")
    with pytest.raises(ConflictError, match="相同 Idempotency-Key"):
        create_agent_product_draft_workspace(
            db_session,
            name="商品 A",
            idempotency_key="session-draft-a",
            agent_session_id=other_session.id,
        )


def test_global_conversation_launches_product_workspace_in_same_session(db_session) -> None:
    agent_session = create_agent_session(db_session, title="全局创建入口")
    global_conversation = db_session.scalar(
        select(AgentConversation).where(
            AgentConversation.session_id == agent_session.id,
            AgentConversation.scope_type == AgentConversationScope.GLOBAL,
        )
    )
    assert global_conversation is not None

    first = create_agent_product_draft_workspace_from_global_conversation(
        db_session,
        global_conversation_id=global_conversation.id,
        name="全局发起的商品",
        idempotency_key="global-product-create-1",
    )
    replay = create_agent_product_draft_workspace_from_global_conversation(
        db_session,
        global_conversation_id=global_conversation.id,
        name="全局发起的商品",
        idempotency_key="global-product-create-1",
    )

    assert first.created is True
    assert replay.created is False
    assert first.conversation.session_id != agent_session.id
    assert db_session.get(AgentSession, first.conversation.session_id).product_id == first.product.id
    assert first.conversation.scope_type == AgentConversationScope.PRODUCT_WORKFLOW
    assert replay.conversation.id == first.conversation.id
    assert replay.conversation.harness_run_id != global_conversation.harness_run_id


def test_global_agent_product_workspace_launch_endpoint_is_scoped_and_idempotent(
    configured_env: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    from productflow_backend.config import get_settings
    from productflow_backend.infrastructure.db.session import get_session_factory
    from productflow_backend.presentation.api import create_app

    session = get_session_factory()()
    try:
        agent_session = create_agent_session(session, title="全局 API 创建入口")
        global_conversation = next(
            conversation
            for conversation in agent_session.conversations
            if conversation.scope_type == AgentConversationScope.GLOBAL
        )
    finally:
        session.close()

    internal_token = "agent-internal-token-with-at-least-32-characters"
    monkeypatch.setenv("AGENT_SERVICE_INTERNAL_TOKEN", internal_token)
    get_settings.cache_clear()
    client = TestClient(create_app())
    path = f"/api/internal/v1/agent-conversations/{global_conversation.id}/product-workspaces"
    headers = {
        "Authorization": f"Bearer {internal_token}",
        "Idempotency-Key": "global-product-api-create-1",
    }

    first = client.post(path, headers=headers, json={"name": "全局 API 商品"})
    assert first.status_code == 201, first.text
    payload = first.json()
    assert payload["created"] is True
    assert payload["session_id"] != agent_session.id
    assert payload["global_conversation_id"] == global_conversation.id
    assert payload["product_name"] == "全局 API 商品"
    assert payload["task_id"] is None
    assert payload["navigation_path"].startswith(f"/products/{payload['product_id']}?")
    assert f"agent_session_id={payload['session_id']}" in payload["navigation_path"]
    assert "agent_task_id=" not in payload["navigation_path"]

    replay = client.post(path, headers=headers, json={"name": "全局 API 商品"})
    assert replay.status_code == 201, replay.text
    assert replay.json()["created"] is False
    assert replay.json()["product_conversation_id"] == payload["product_conversation_id"]

    reconciled = client.post(
        path + "/reconcile",
        headers=headers,
        json={"name": "全局 API 商品"},
    )
    assert reconciled.status_code == 200, reconciled.text
    assert reconciled.json()["state"] == "applied"
    assert reconciled.json()["result"]["created"] is False
    assert reconciled.json()["result"]["product_conversation_id"] == payload["product_conversation_id"]


def test_agent_product_workspace_intake_finalization_is_atomic_idempotent_and_coverless(
    configured_env: Path,
    db_session,
) -> None:
    draft_creation = create_agent_product_draft_workspace(
        db_session,
        name="待确认商品",
        idempotency_key="draft-before-intake",
    )
    birth_graph = get_active_workflow_graph(db_session, product_id=draft_creation.product.id)
    assert birth_graph is not None
    birth_nodes = list(
        db_session.scalars(select(WorkflowGraphNode).where(WorkflowGraphNode.graph_id == birth_graph.id))
    )
    assert [node.node_type for node in birth_nodes] == [GraphNodeType.PRODUCT_SOURCE]
    birth_source_id = birth_nodes[0].id
    selection = _selection(("hero", 2), ("scene", 3))
    uploads = _workspace_uploads()

    finalized = finalize_agent_product_workspace_intake(
        db_session,
        conversation_id=draft_creation.conversation.id,
        selection=selection,
        image_uploads=uploads,
        idempotency_key="intake-finalize-1",
    )

    assert finalized.created is True
    assert finalized.product.id == draft_creation.product.id
    assert finalized.product.cover_image_asset_id is None
    assert [asset.original_filename for asset in finalized.created_assets] == ["front.png", "detail.png"]
    intake = WorkflowIntakeV1.model_validate(finalized.product.intake_json)
    assert [(item.key, item.quantity, item.order) for item in intake.image_types] == [
        ("hero", 2, 0),
        ("scene", 3, 1),
    ]
    assert intake.reference_asset_ids == [asset.id for asset in finalized.created_assets]
    assert finalized.conversation.workflow_draft_id is None
    assert finalized.conversation.intake_idempotency_key == "intake-finalize-1"
    assert len(finalized.conversation.intake_request_hash or "") == 64
    live_graph = get_active_workflow_graph(db_session, product_id=finalized.product.id)
    assert live_graph is not None
    graph_nodes = list(
        db_session.scalars(select(WorkflowGraphNode).where(WorkflowGraphNode.graph_id == live_graph.id))
    )
    node_types = {node.node_type for node in graph_nodes}
    assert GraphNodeType.PRODUCT_SOURCE in node_types
    assert GraphNodeType.VISUAL_SYSTEM in node_types
    assert GraphNodeType.IMAGE_ASSET in node_types
    assert GraphNodeType.CREATIVE_BRIEF in node_types
    assert GraphNodeType.PROMPT_GENERATION in node_types
    assert GraphNodeType.IMAGE_GENERATION in node_types
    sources = [node for node in graph_nodes if node.node_type == GraphNodeType.PRODUCT_SOURCE]
    assert len(sources) == 1
    assert sources[0].id == birth_source_id
    birth_node_count = len(graph_nodes)
    agent_session = db_session.get(AgentSession, finalized.conversation.session_id)
    assert agent_session is not None
    assert agent_session.summary == "暂无 Agent Task"
    assert db_session.scalar(select(func.count()).select_from(WorkflowGraphNode)) > 0

    first_files = sorted(path.relative_to(configured_env) for path in _media_files(configured_env))
    replay = finalize_agent_product_workspace_intake(
        db_session,
        conversation_id=draft_creation.conversation.id,
        selection=selection,
        image_uploads=uploads,
        idempotency_key="intake-finalize-1",
    )
    assert replay.created is False
    assert [asset.id for asset in replay.created_assets] == [asset.id for asset in finalized.created_assets]
    assert sorted(path.relative_to(configured_env) for path in _media_files(configured_env)) == first_files
    assert db_session.scalar(select(func.count()).select_from(ProductImageAsset)) == 2
    assert (
        db_session.scalar(
            select(func.count()).select_from(WorkflowGraphNode).where(WorkflowGraphNode.graph_id == live_graph.id)
        )
        == birth_node_count
    )

    with pytest.raises(ConflictError, match="已经确认"):
        finalize_agent_product_workspace_intake(
            db_session,
            conversation_id=draft_creation.conversation.id,
            selection=_selection(("detail", 1)),
            image_uploads=uploads,
            idempotency_key="intake-finalize-1",
        )


def test_intake_skips_template_when_graph_already_has_other_nodes(
    configured_env: Path,
    db_session,
) -> None:
    draft_creation = create_agent_product_draft_workspace(
        db_session,
        name="已改过的出生图",
        idempotency_key="draft-before-skip-expand",
    )
    graph = get_active_workflow_graph(db_session, product_id=draft_creation.product.id)
    assert graph is not None
    applied = load_applied_graph(db_session, graph)
    apply_graph_change_set(
        db_session,
        product_id=draft_creation.product.id,
        graph_id=graph.id,
        change_set=WorkflowChangeSet(
            base_graph_revision=applied.revision,
            summary="手动加素材",
            actor_type=GraphActorType.USER,
            operations=[
                CreateNodeOp(
                    client_ref="manual-asset",
                    node_type=GraphNodeType.IMAGE_ASSET,
                    title="已有素材",
                    config={"role": "product_identity"},
                )
            ],
        ),
    )
    before_ids = set(
        db_session.scalars(select(WorkflowGraphNode.id).where(WorkflowGraphNode.graph_id == graph.id))
    )

    finalize_agent_product_workspace_intake(
        db_session,
        conversation_id=draft_creation.conversation.id,
        selection=_selection(("hero", 1)),
        image_uploads=_workspace_uploads()[:1],
        idempotency_key="intake-skip-expand",
    )

    after_nodes = list(
        db_session.scalars(select(WorkflowGraphNode).where(WorkflowGraphNode.graph_id == graph.id))
    )
    assert {node.id for node in after_nodes} == before_ids
    assert GraphNodeType.PROMPT_GENERATION not in {node.node_type for node in after_nodes}


def test_agent_product_workspace_intake_failure_preserves_empty_draft_and_cleans_storage(
    configured_env: Path,
    db_session,
) -> None:
    class FailSecondMediaStorage(LocalStorage):
        def __init__(self, root: Path) -> None:
            super().__init__(root)
            self.calls = 0

        def save_media_image(self, media_id: str, filename: str, content: bytes) -> str:
            self.calls += 1
            if self.calls == 2:
                raise OSError("injected finalization storage failure")
            return super().save_media_image(media_id, filename, content)

    workspace = create_agent_product_draft_workspace(
        db_session,
        name="确认失败商品",
        idempotency_key="draft-finalize-failure",
    )
    with pytest.raises(OSError, match="injected finalization storage failure"):
        finalize_agent_product_workspace_intake(
            db_session,
            conversation_id=workspace.conversation.id,
            selection=_selection(("hero", 2)),
            image_uploads=_workspace_uploads(),
            idempotency_key="intake-finalize-failure",
            storage=FailSecondMediaStorage(configured_env),
        )

    restored = get_agent_product_workspace(db_session, conversation_id=workspace.conversation.id)
    assert restored.product.intake_json is None
    assert restored.conversation.intake_idempotency_key is None
    assert restored.created_assets == []
    assert db_session.scalar(select(func.count()).select_from(Product)) == 1
    assert db_session.scalar(select(func.count()).select_from(MediaObject)) == 0
    assert _media_files(configured_env) == []


def test_empty_agent_product_draft_allows_turn_and_asks_for_intake(
    configured_env: Path,
    db_session,
) -> None:
    workspace = create_agent_product_draft_workspace(
        db_session,
        name="Turn 边界商品",
        idempotency_key="draft-turn-boundary",
    )
    reservation = reserve_agent_turn(
        db_session,
        product_id=workspace.product.id,
        conversation_id=workspace.conversation.id,
        input_text="开始",
        input_asset_ids=[],
        idempotency_key="turn-before-intake",
    )
    assert reservation.created is True
    contract = get_agent_contract(db_session, workspace.conversation.id)
    assert contract["has_live_graph"] is True
    assert "不得提交第二份完整拓扑" in contract["system_prompt"]

    finalized = finalize_agent_product_workspace_intake(
        db_session,
        conversation_id=workspace.conversation.id,
        selection=_selection(("hero", 2)),
        image_uploads=_workspace_uploads(),
        idempotency_key="turn-intake",
    )
    assert finalized.created_assets
    assert finalized.product.intake_json is not None
    assert finalized.conversation.workflow_draft_id is None
    reservation = reserve_agent_turn(
        db_session,
        product_id=workspace.product.id,
        conversation_id=workspace.conversation.id,
        input_text="开始",
        input_asset_ids=[asset.id for asset in finalized.created_assets],
        idempotency_key="turn-after-intake",
    )
    assert reservation.created is True


def test_agent_finalizes_intake_from_conversation_assets(
    configured_env: Path,
    db_session,
) -> None:
    workspace = create_agent_product_draft_workspace(
        db_session,
        name="会话上传商品",
        idempotency_key="draft-conversation-intake",
    )
    assets = add_canonical_product_images(
        db_session,
        product_id=workspace.product.id,
        image_uploads=_workspace_uploads(),
    )
    asset_ids = [asset.id for asset in assets]
    reservation = reserve_agent_turn(
        db_session,
        product_id=workspace.product.id,
        conversation_id=workspace.conversation.id,
        input_text="主图两张，用刚传的参考图",
        input_asset_ids=asset_ids,
        idempotency_key="turn-with-uploads",
    )
    assert reservation.created is True

    first = finalize_agent_product_intake(
        db_session,
        conversation_id=workspace.conversation.id,
        selection=_selection(("hero", 2)).model_dump(mode="json"),
        reference_asset_ids=asset_ids,
        idempotency_key="conversation-intake",
    )
    assert first["intake_finalized"] is True
    assert first["reference_asset_ids"] == asset_ids
    assert first["intake"]["image_types"][0]["key"] == "hero"
    assert first["workflow_draft_id"] is None

    replay = finalize_agent_product_workspace_intake_from_assets(
        db_session,
        conversation_id=workspace.conversation.id,
        selection=_selection(("hero", 2)),
        reference_asset_ids=asset_ids,
        idempotency_key="conversation-intake",
    )
    assert [asset.id for asset in replay.created_assets] == asset_ids
    assert replay.product.intake_json == first["intake"]

    reconciled = reconcile_agent_product_intake_from_assets(
        db_session,
        conversation_id=workspace.conversation.id,
        selection=_selection(("hero", 2)),
        reference_asset_ids=asset_ids,
        idempotency_key="conversation-intake",
    )
    assert reconciled.state == "applied"

    with pytest.raises(ConflictError, match="不能提交不同请求"):
        finalize_agent_product_workspace_intake_from_assets(
            db_session,
            conversation_id=workspace.conversation.id,
            selection=_selection(("scene", 1)),
            reference_asset_ids=asset_ids,
            idempotency_key="conversation-intake",
        )


def test_agent_product_workspace_idempotency_replays_and_rejects_payload_drift(
    configured_env: Path,
    db_session,
) -> None:
    kwargs = {
        "name": "幂等商品",
        "selection": _selection(("hero", 2), ("scene", 1)),
        "image_uploads": _workspace_uploads(),
        "idempotency_key": "workspace-stable-key",
    }
    first = create_agent_product_workspace(db_session, **kwargs)
    first_files = sorted(path.relative_to(configured_env) for path in _media_files(configured_env))
    replay = create_agent_product_workspace(db_session, **kwargs)

    assert replay.created is False
    assert replay.product.id == first.product.id
    assert replay.conversation.id == first.conversation.id
    assert replay.conversation.workflow_draft_id is None
    assert replay.conversation.id == first.conversation.id
    assert [asset.id for asset in replay.created_assets] == [asset.id for asset in first.created_assets]
    assert sorted(path.relative_to(configured_env) for path in _media_files(configured_env)) == first_files
    assert db_session.scalar(select(func.count()).select_from(Product)) == 1

    with pytest.raises(ConflictError, match="相同 Idempotency-Key"):
        create_agent_product_workspace(db_session, **{**kwargs, "name": "漂移商品"})
    with pytest.raises(ConflictError, match="相同 Idempotency-Key"):
        create_agent_product_draft_workspace(
            db_session,
            name=kwargs["name"],
            idempotency_key=kwargs["idempotency_key"],
        )
    assert db_session.scalar(select(func.count()).select_from(Product)) == 1


@pytest.mark.parametrize("failure_model", [AgentConversation])
def test_agent_product_workspace_flush_failures_rollback_database_and_storage(
    configured_env: Path,
    db_session,
    failure_model,
) -> None:
    def fail_target_flush(session, _flush_context, _instances) -> None:
        if any(isinstance(item, failure_model) for item in session.new):
            raise RuntimeError(f"injected {failure_model.__name__} flush failure")

    event.listen(db_session, "before_flush", fail_target_flush)
    try:
        with pytest.raises(RuntimeError, match="injected"):
            create_agent_product_workspace(
                db_session,
                name="故障注入商品",
                selection=_selection(("hero", 2)),
                image_uploads=_workspace_uploads(),
                idempotency_key=f"failure-{failure_model.__name__}",
            )
    finally:
        event.remove(db_session, "before_flush", fail_target_flush)

    assert db_session.scalar(select(func.count()).select_from(Product)) == 0
    assert db_session.scalar(select(func.count()).select_from(MediaObject)) == 0
    assert db_session.scalar(select(func.count()).select_from(WorkflowDraft)) == 0
    assert db_session.scalar(select(func.count()).select_from(AgentConversation)) == 0
    assert _media_files(configured_env) == []


def test_agent_product_workspace_commit_failure_rolls_back_database_and_storage(
    configured_env: Path,
    db_session,
) -> None:
    def fail_commit(_session) -> None:
        raise RuntimeError("injected commit failure")

    event.listen(db_session, "before_commit", fail_commit)
    try:
        with pytest.raises(RuntimeError, match="injected commit failure"):
            create_agent_product_workspace(
                db_session,
                name="提交故障商品",
                selection=_selection(("hero", 2)),
                image_uploads=_workspace_uploads(),
                idempotency_key="commit-failure",
            )
    finally:
        event.remove(db_session, "before_commit", fail_commit)

    assert db_session.scalar(select(func.count()).select_from(Product)) == 0
    assert db_session.scalar(select(func.count()).select_from(MediaObject)) == 0
    assert db_session.scalar(select(func.count()).select_from(WorkflowDraft)) == 0
    assert db_session.scalar(select(func.count()).select_from(AgentConversation)) == 0
    assert _media_files(configured_env) == []


def test_agent_product_workspace_storage_failure_compensates_prior_upload(
    configured_env: Path,
    db_session,
) -> None:
    class FailSecondMediaStorage(LocalStorage):
        def __init__(self, root: Path) -> None:
            super().__init__(root)
            self.calls = 0

        def save_media_image(self, media_id: str, filename: str, content: bytes) -> str:
            self.calls += 1
            if self.calls == 2:
                raise OSError("injected storage failure")
            return super().save_media_image(media_id, filename, content)

    with pytest.raises(OSError, match="injected storage failure"):
        create_agent_product_workspace(
            db_session,
            name="存储故障商品",
            selection=_selection(("hero", 2)),
            image_uploads=_workspace_uploads(),
            idempotency_key="storage-failure",
            storage=FailSecondMediaStorage(configured_env),
        )

    assert db_session.scalar(select(func.count()).select_from(Product)) == 0
    assert db_session.scalar(select(func.count()).select_from(MediaObject)) == 0
    assert _media_files(configured_env) == []


def test_agent_product_workspace_rejects_invalid_later_image_without_residue(
    configured_env: Path,
    db_session,
) -> None:
    with pytest.raises(BusinessValidationError, match="声明媒体类型"):
        create_agent_product_workspace(
            db_session,
            name="无效图片商品",
            selection=_selection(("hero", 2)),
            image_uploads=[
                (_make_demo_image_bytes(), "first.png", "image/png"),
                (_make_demo_image_bytes(), "wrong.jpg", "image/jpeg"),
            ],
            idempotency_key="invalid-image",
        )
    assert db_session.scalar(select(func.count()).select_from(Product)) == 0
    assert db_session.scalar(select(func.count()).select_from(MediaObject)) == 0
    assert _media_files(configured_env) == []


def test_agent_product_workspace_api_exposes_options_and_bounded_create(configured_env: Path) -> None:
    from productflow_backend.infrastructure.db.session import get_session_factory
    from productflow_backend.presentation.api import create_app

    client = TestClient(create_app())
    _login(client)
    options_response = client.get("/api/v2/agent-product-workspaces/options")
    assert options_response.status_code == 200
    options = options_response.json()
    assert options["schema_version"] == 1
    assert [item["key"] for item in options["image_types"]] == [
        item.key for item in AGENT_PRODUCT_IMAGE_TYPE_CATALOG
    ]
    assert options["limits"] == {
        "min_image_types": 1,
        "default_images_per_type": 2,
        "min_images_per_type": 1,
        "max_images_per_type": 6,
        "max_total_images": 30,
        "min_reference_images": 1,
        "max_reference_images": 6,
        "allowed_image_mime_types": ["image/png", "image/jpeg", "image/webp"],
    }

    selection = _selection(("hero", 2), ("dimensions", 1))
    request = {
        "data": {"name": "API Agent 商品", "selection": selection.model_dump_json()},
        "files": [
            ("images", ("front.png", _make_demo_image_bytes(), "image/png")),
            ("images", ("side.png", _make_demo_image_bytes(), "image/png")),
        ],
        "headers": {"Idempotency-Key": "api-agent-workspace-1"},
    }
    created_response = client.post("/api/v2/agent-product-workspaces", **request)
    assert created_response.status_code == 201, created_response.text
    created = created_response.json()
    assert set(created) == {"task_id", "product", "created_assets", "workflow_draft", "conversation"}
    assert created["task_id"] is None
    assert created["product"]["cover_image_asset_id"] is None
    assert [asset["original_filename"] for asset in created["created_assets"]] == [
        "front.png",
        "side.png",
    ]
    assert created["workflow_draft"] is None
    assert created["product"]["intake"] == {
        "schema_version": 1,
        "image_types": [
            {"key": "hero", "quantity": 2, "order": 0},
            {"key": "dimensions", "quantity": 1, "order": 1},
        ],
        "reference_asset_ids": [asset["id"] for asset in created["created_assets"]],
    }
    assert created["conversation"]["harness_run_id"] == created["conversation"]["id"]

    replay_response = client.post("/api/v2/agent-product-workspaces", **request)
    assert replay_response.status_code == 201
    assert replay_response.json() == created

    session = get_session_factory()()
    try:
        assert session.scalar(select(func.count()).select_from(Product)) == 1
    finally:
        session.close()


def test_agent_product_workspace_api_supports_draft_resume_and_intake_finalization(
    configured_env: Path,
) -> None:
    from productflow_backend.infrastructure.db.session import get_session_factory
    from productflow_backend.presentation.api import create_app

    session_factory = get_session_factory()
    session = session_factory()
    try:
        agent_session = create_agent_session(session, title="分阶段商品 Session")
    finally:
        session.close()

    client = TestClient(create_app())
    _login(client)
    draft_response = client.post(
        "/api/v2/agent-product-workspaces/drafts",
        json={"name": "分阶段 API 商品", "agent_session_id": agent_session.id},
        headers={"Idempotency-Key": "api-draft-workspace-1"},
    )
    assert draft_response.status_code == 201, draft_response.text
    draft = draft_response.json()
    assert set(draft) == {
        "task_id",
        "created",
        "intake_finalized",
        "product",
        "created_assets",
        "workflow_draft",
        "conversation",
    }
    assert draft["created"] is True
    assert draft["intake_finalized"] is False
    assert draft["created_assets"] == []
    assert draft["product"]["cover_image_asset_id"] is None
    assert draft["workflow_draft"] is None
    assert draft["product"]["intake"] is None
    assert draft["conversation"]["session_id"] != agent_session.id
    assert draft["task_id"] is None

    replay_response = client.post(
        "/api/v2/agent-product-workspaces/drafts",
        json={"name": "分阶段 API 商品", "agent_session_id": agent_session.id},
        headers={"Idempotency-Key": "api-draft-workspace-1"},
    )
    assert replay_response.status_code == 201
    assert replay_response.json()["created"] is False
    assert replay_response.json()["conversation"]["id"] == draft["conversation"]["id"]

    restored_response = client.get(
        f"/api/v2/agent-product-workspaces/{draft['conversation']['id']}"
    )
    assert restored_response.status_code == 200
    assert restored_response.json()["created"] is False
    assert restored_response.json()["intake_finalized"] is False

    selection = _selection(("hero", 2), ("detail", 1))
    finalization_request = {
        "data": {"selection": selection.model_dump_json()},
        "files": [
            ("images", ("front.png", _make_demo_image_bytes(), "image/png")),
            ("images", ("detail.png", _make_demo_image_bytes(), "image/png")),
        ],
        "headers": {"Idempotency-Key": "api-intake-finalize-1"},
    }
    finalized_response = client.post(
        f"/api/v2/agent-product-workspaces/{draft['conversation']['id']}/intake",
        **finalization_request,
    )
    assert finalized_response.status_code == 200, finalized_response.text
    finalized = finalized_response.json()
    assert finalized["created"] is True
    assert finalized["intake_finalized"] is True
    assert [asset["original_filename"] for asset in finalized["created_assets"]] == [
        "front.png",
        "detail.png",
    ]
    assert finalized["product"]["cover_image_asset_id"] is None
    assert finalized["workflow_draft"] is None
    assert finalized["product"]["intake"]["reference_asset_ids"] == [
        asset["id"] for asset in finalized["created_assets"]
    ]
    assert finalized["task_id"] is None

    finalization_replay = client.post(
        f"/api/v2/agent-product-workspaces/{draft['conversation']['id']}/intake",
        **finalization_request,
    )
    assert finalization_replay.status_code == 200
    assert finalization_replay.json()["created"] is False
    assert [asset["id"] for asset in finalization_replay.json()["created_assets"]] == [
        asset["id"] for asset in finalized["created_assets"]
    ]

    invalid_draft = client.post(
        "/api/v2/agent-product-workspaces/drafts",
        json={"name": "拒绝未知字段", "template_key": "unexpected"},
        headers={"Idempotency-Key": "strict-draft"},
    )
    assert invalid_draft.status_code == 422

    session = get_session_factory()()
    try:
        assert session.scalar(select(func.count()).select_from(Product)) == 1
        assert session.scalar(select(func.count()).select_from(ProductImageAsset)) == 2
    finally:
        session.close()


def test_agent_product_workspace_api_rejects_invalid_selection(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    client = TestClient(create_app())
    _login(client)
    response = client.post(
        "/api/v2/agent-product-workspaces",
        data={
            "name": "无效选择",
            "selection": json.dumps({"schema_version": 1, "image_types": []}),
        },
        files=[("images", ("front.png", _make_demo_image_bytes(), "image/png"))],
        headers={"Idempotency-Key": "invalid-selection"},
    )
    assert response.status_code == 400
    assert response.json() == {"detail": "图片类型选择不符合 AgentProductSelectionV1"}


def test_product_path_refuses_workflow_draft_writers(db_session) -> None:
    from workflow_draft_helpers import make_workflow_draft_payload

    from productflow_backend.application.legacy_archive_rebuilds import create_legacy_archive_rebuild
    from productflow_backend.application.workflow_drafts.service import (
        append_workflow_draft_revision,
        confirm_workflow_draft_revision,
        create_workflow_draft,
        persist_confirmed_draft_graph,
    )

    workspace = create_agent_product_draft_workspace(
        db_session,
        name="拒绝 Draft 商品",
        idempotency_key="retire-draft-writers",
    )
    payload = make_workflow_draft_payload(reference_asset_id=workspace.product.id)
    with pytest.raises(ConflictError, match="不再使用 WorkflowDraft"):
        create_workflow_draft(
            db_session,
            product_id=workspace.product.id,
            payload=payload,
            ready_for_confirmation=True,
        )
    with pytest.raises(ConflictError, match="不再使用 WorkflowDraft"):
        append_workflow_draft_revision(
            db_session,
            product_id=workspace.product.id,
            draft_id="missing",
            expected_draft_version=0,
            payload=payload,
            ready_for_confirmation=True,
        )
    with pytest.raises(ConflictError, match="不再使用 WorkflowDraft"):
        confirm_workflow_draft_revision(
            db_session,
            product_id=workspace.product.id,
            draft_id="missing",
            expected_draft_version=1,
        )
    with pytest.raises(ConflictError, match="不再使用 WorkflowDraft"):
        persist_confirmed_draft_graph(
            db_session,
            product_id=workspace.product.id,
            draft_id="missing",
            expected_draft_version=1,
        )
    with pytest.raises(ConflictError, match="不再使用 WorkflowDraft"):
        create_legacy_archive_rebuild(
            db_session,
            kind="workflow",
            archive_id="missing",
            target_product_id=workspace.product.id,
            idempotency_key="rebuild-retired",
        )
    assert db_session.scalar(
        select(func.count()).select_from(WorkflowDraft).where(WorkflowDraft.product_id == workspace.product.id)
    ) == 0
