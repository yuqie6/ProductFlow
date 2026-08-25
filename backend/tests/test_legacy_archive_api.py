from __future__ import annotations

from copy import deepcopy
from datetime import UTC, datetime, timedelta

import pytest
from fastapi.testclient import TestClient
from helpers import _login, _make_demo_image_bytes
from sqlalchemy import event, func, select
from workflow_draft_helpers import make_workflow_draft_payload

from productflow_backend.application.agent.product_workspaces import attach_agent_workspace_to_product
from productflow_backend.application.legacy_archives import (
    get_legacy_archive_detail,
    legacy_archive_export_bytes,
    list_legacy_archives,
)
from productflow_backend.application.product_workflow.graph_commands import create_empty_workflow_graph
from productflow_backend.application.products import create_canonical_product
from productflow_backend.application.workflow_drafts.service import PRODUCT_WORKFLOW_DRAFT_RETIRED
from productflow_backend.config import get_settings
from productflow_backend.domain.errors import BusinessValidationError, ConflictError, NotFoundError
from productflow_backend.infrastructure.db.models import (
    AgentConversation,
    LegacyCanvasAgentArchive,
    LegacyUserTemplateArchive,
    LegacyWorkflowArchive,
    LegacyWorkflowArchiveAsset,
    WorkflowDraft,
    WorkflowDraftLegacyArchiveSeed,
    WorkflowDraftRevision,
    WorkflowGraph,
)
from productflow_backend.infrastructure.db.session import get_engine, get_session_factory
from productflow_backend.presentation.api import create_app


def _seed_archives(db_session):
    product = create_canonical_product(
        db_session,
        name="橙蓝刀具收纳套装",
        category="工业收纳",
        price=None,
        source_note=None,
        image_uploads=[(_make_demo_image_bytes(), "legacy-reference.png", "image/png")],
    )
    other_product = create_canonical_product(
        db_session,
        name="百分号%_测试商品",
        category=None,
        price=None,
        source_note=None,
        image_uploads=[(_make_demo_image_bytes(), "other.png", "image/png")],
    )
    created_at = datetime(2026, 8, 15, 8, 30, tzinfo=UTC)
    workflow = LegacyWorkflowArchive(
        id="archive-workflow-main",
        source_profile="legacy_canvas_agent_20260518_0032",
        legacy_workflow_id="legacy-workflow-main",
        product_id=product.id,
        source_title="主图与场景图工作流",
        source_updated_at=created_at - timedelta(days=2),
        archive_schema_version=1,
        payload_json={
            "nodes": [{"id": "prompt-1", "config": {"prompt": "工业橙蓝视觉"}}],
            "edges": [],
        },
        source_fingerprint_sha256="1" * 64,
        payload_sha256="2" * 64,
        node_count=1,
        edge_count=0,
        run_count=2,
        node_run_count=3,
        asset_count=1,
        created_at=created_at,
    )
    workflow.assets.append(
        LegacyWorkflowArchiveAsset(
            id="archive-asset-main",
            asset=product.image_assets[0],
            role="reference",
            legacy_source_type="source_asset",
            legacy_source_id="legacy-source-main",
            created_at=created_at,
        )
    )
    other_workflow = LegacyWorkflowArchive(
        id="archive-workflow-other",
        source_profile="legacy_canvas_agent_20260518_0032",
        legacy_workflow_id="legacy-workflow-other",
        product_id=other_product.id,
        source_title="百分号%_文字工作流",
        source_updated_at=None,
        archive_schema_version=1,
        payload_json={"nodes": [], "edges": []},
        source_fingerprint_sha256="3" * 64,
        payload_sha256="4" * 64,
        node_count=0,
        edge_count=0,
        run_count=0,
        node_run_count=0,
        asset_count=0,
        created_at=created_at - timedelta(minutes=2),
    )
    canvas = LegacyCanvasAgentArchive(
        id="archive-canvas-main",
        source_profile="legacy_canvas_agent_20260518_0032",
        legacy_thread_id="legacy-thread-main",
        product_id=product.id,
        title="商品视觉需求沟通",
        source_status="completed",
        source_updated_at=created_at - timedelta(days=1),
        archive_schema_version=1,
        payload_json={
            "messages": [
                {"role": "user", "content": "图片文字使用中文"},
                {"role": "assistant", "content": "已记录"},
            ]
        },
        source_fingerprint_sha256="5" * 64,
        payload_sha256="6" * 64,
        message_count=2,
        run_count=1,
        tool_event_count=0,
        plan_count=1,
        task_plan_count=0,
        timeline_event_count=2,
        visible_event_count=2,
        technical_event_count=0,
        created_at=created_at,
    )
    template = LegacyUserTemplateArchive(
        id="archive-template-main",
        source_profile="legacy_canvas_agent_20260518_0032",
        legacy_template_id="legacy-template-main",
        legacy_key="industrial-orange-blue",
        title="工业橙蓝设计体系",
        description="用户保存的旧节点组",
        archive_status="archived",
        archive_schema_version=1,
        payload_json={"nodes": [{"id": "copy"}], "edges": []},
        diagnostics_json=[],
        source_fingerprint_sha256="7" * 64,
        payload_sha256="8" * 64,
        source_updated_at=None,
        created_at=created_at,
    )
    db_session.add_all([workflow, other_workflow, canvas, template])
    db_session.commit()
    return product, other_product, workflow, canvas, template


def test_archive_list_keyset_filtering_and_literal_search(db_session) -> None:
    product, _, workflow, canvas, template = _seed_archives(db_session)

    seen: list[str] = []
    cursor = ""
    while True:
        page = list_legacy_archives(db_session, after=cursor, limit=1)
        seen.extend(item.id for item in page.items)
        if page.next_cursor is None:
            assert page.total == 4
            assert page.kind_counts == {
                "workflow": 2,
                "canvas_agent_thread": 1,
                "user_template": 1,
            }
            break
        cursor = page.next_cursor

    assert seen == [workflow.id, canvas.id, template.id, "archive-workflow-other"]
    assert len(seen) == len(set(seen))

    product_page = list_legacy_archives(db_session, product_id=product.id)
    assert [item.id for item in product_page.items] == [workflow.id, canvas.id]
    assert product_page.kind_counts["user_template"] == 0

    template_page = list_legacy_archives(db_session, kind="user_template", query="ORANGE-BLUE")
    assert [item.id for item in template_page.items] == [template.id]
    literal_page = list_legacy_archives(db_session, query="%_")
    assert [item.id for item in literal_page.items] == ["archive-workflow-other"]

    first = list_legacy_archives(db_session, limit=1)
    assert first.next_cursor is not None
    with pytest.raises(BusinessValidationError, match="查询条件不匹配"):
        list_legacy_archives(db_session, product_id=product.id, after=first.next_cursor, limit=1)
    with pytest.raises(BusinessValidationError, match="cursor 无效"):
        list_legacy_archives(db_session, after="not-a-cursor")


def test_archive_detail_and_export_are_safe_and_deterministic(db_session) -> None:
    _, _, workflow, _, _ = _seed_archives(db_session)

    detail = get_legacy_archive_detail(db_session, kind="workflow", archive_id=workflow.id)
    assert detail.item.counts == {"nodes": 1, "edges": 0, "runs": 2, "node_runs": 3, "assets": 1}
    assert [asset.original_filename for asset in detail.assets] == ["legacy-reference.png"]
    assert legacy_archive_export_bytes(detail) == legacy_archive_export_bytes(detail)
    exported = legacy_archive_export_bytes(detail).decode()
    assert '"schema_version":1' in exported
    assert "storage_path" not in exported
    assert "data:image/" not in exported

    with pytest.raises(NotFoundError, match="不存在"):
        get_legacy_archive_detail(db_session, kind="workflow", archive_id="missing")


def test_archive_http_surface_is_read_only_and_reuses_canonical_asset_download(configured_env, db_session) -> None:
    product, _, workflow, _, _ = _seed_archives(db_session)
    product_id = product.id
    workflow_archive_id = workflow.id
    before_graph_count = db_session.scalar(select(func.count()).select_from(WorkflowGraph))
    assert before_graph_count == 0

    client = TestClient(create_app())
    _login(client)
    statements: list[str] = []

    def capture_statement(_connection, _cursor, statement, _parameters, _context, _executemany) -> None:
        statements.append(statement.lstrip().split(maxsplit=1)[0].upper())

    engine = get_engine()
    event.listen(engine, "before_cursor_execute", capture_statement)
    try:
        page_response = client.get(
            "/api/v2/legacy-archives",
            params={"product_id": product_id, "q": "视觉", "limit": 10},
        )
        assert page_response.status_code == 200, page_response.text
        assert [item["kind"] for item in page_response.json()["items"]] == ["canvas_agent_thread"]

        detail_response = client.get(f"/api/v2/legacy-archives/workflow/{workflow_archive_id}")
        assert detail_response.status_code == 200, detail_response.text
        detail = detail_response.json()
        assert detail["item"]["product_id"] == product_id
        assert detail["payload"]["nodes"][0]["config"]["prompt"] == "工业橙蓝视觉"
        assert len(detail["assets"]) == 1
        asset = detail["assets"][0]
        assert asset["download_url"].endswith(
            f"/api/v2/product-image-assets/{asset['product_image_asset_id']}/download"
        )
        assert "storage_path" not in detail_response.text

        first_export = client.get(f"/api/v2/legacy-archives/workflow/{workflow_archive_id}/export")
        second_export = client.get(f"/api/v2/legacy-archives/workflow/{workflow_archive_id}/export")
        assert first_export.status_code == 200
        assert first_export.content == second_export.content
        assert first_export.headers["etag"] == second_export.headers["etag"]
        assert first_export.headers["content-disposition"].endswith(f'{workflow_archive_id}.json"')

        download = client.get(asset["download_url"])
        assert download.status_code == 200
        assert download.headers["content-type"] == "image/png"

        unsupported_mutation = client.post(f"/api/v2/legacy-archives/workflow/{workflow_archive_id}")
        assert unsupported_mutation.status_code == 405
    finally:
        event.remove(engine, "before_cursor_execute", capture_statement)

    assert not {"INSERT", "UPDATE", "DELETE"} & set(statements)
    verification_session = get_session_factory()()
    try:
        assert verification_session.scalar(select(func.count()).select_from(WorkflowGraph)) == 0
    finally:
        verification_session.close()


def test_archive_agent_rebuild_creates_an_empty_idempotent_draft_without_touching_archive(
    configured_env,
    db_session,
) -> None:
    product, other_product, workflow, canvas, template = _seed_archives(db_session)
    original_payload = deepcopy(workflow.payload_json)
    original_payload_hash = workflow.payload_sha256
    client = TestClient(create_app())
    _login(client)
    request = {
        "target_product_id": product.id,
        "idempotency_key": "rebuild-workflow-main",
    }

    created = client.post(
        f"/api/v2/legacy-archives/workflow/{workflow.id}/agent-rebuilds",
        json=request,
    )
    assert created.status_code == 409, created.text
    assert PRODUCT_WORKFLOW_DRAFT_RETIRED in created.json()["detail"]

    db_session.expire_all()
    assert db_session.scalar(select(func.count()).select_from(WorkflowDraftRevision)) == 0
    assert db_session.scalar(select(func.count()).select_from(WorkflowDraft)) == 0
    assert db_session.scalar(select(func.count()).select_from(AgentConversation)) == 0
    assert db_session.scalar(select(func.count()).select_from(WorkflowDraftLegacyArchiveSeed)) == 0
    persisted_workflow = db_session.get(LegacyWorkflowArchive, workflow.id)
    assert persisted_workflow is not None
    assert persisted_workflow.payload_json == original_payload
    assert persisted_workflow.payload_sha256 == original_payload_hash

    repeated = client.post(
        f"/api/v2/legacy-archives/workflow/{workflow.id}/agent-rebuilds",
        json=request,
    )
    assert repeated.status_code == 409, repeated.text
    assert PRODUCT_WORKFLOW_DRAFT_RETIRED in repeated.json()["detail"]

    conflicting = client.post(
        f"/api/v2/legacy-archives/canvas_agent_thread/{canvas.id}/agent-rebuilds",
        json=request,
    )
    assert conflicting.status_code == 409
    assert PRODUCT_WORKFLOW_DRAFT_RETIRED in conflicting.json()["detail"]

    foreign_product = client.post(
        f"/api/v2/legacy-archives/workflow/{workflow.id}/agent-rebuilds",
        json={"target_product_id": other_product.id, "idempotency_key": "foreign-product"},
    )
    assert foreign_product.status_code == 409
    assert PRODUCT_WORKFLOW_DRAFT_RETIRED in foreign_product.json()["detail"]

    template_rebuild = client.post(
        f"/api/v2/legacy-archives/user_template/{template.id}/agent-rebuilds",
        json={"target_product_id": other_product.id, "idempotency_key": "template-on-other-product"},
    )
    assert template_rebuild.status_code == 409, template_rebuild.text
    assert PRODUCT_WORKFLOW_DRAFT_RETIRED in template_rebuild.json()["detail"]
    assert db_session.scalar(select(func.count()).select_from(WorkflowDraft)) == 0


def test_archive_seeded_version_zero_draft_accepts_first_agent_revision(
    configured_env,
    db_session,
) -> None:
    from productflow_backend.application.workflow_drafts.service import append_workflow_draft_revision

    product, _, workflow, _, _ = _seed_archives(db_session)
    archived_payload = deepcopy(workflow.payload_json)
    client = TestClient(create_app())
    _login(client)
    rebuilt = client.post(
        f"/api/v2/legacy-archives/workflow/{workflow.id}/agent-rebuilds",
        json={"target_product_id": product.id, "idempotency_key": "archive-first-revision"},
    )
    assert rebuilt.status_code == 409, rebuilt.text
    assert PRODUCT_WORKFLOW_DRAFT_RETIRED in rebuilt.json()["detail"]

    with pytest.raises(ConflictError, match=PRODUCT_WORKFLOW_DRAFT_RETIRED):
        append_workflow_draft_revision(
            db_session,
            product_id=product.id,
            draft_id="missing",
            expected_draft_version=0,
            payload=make_workflow_draft_payload(reference_asset_id=product.image_assets[0].id),
            ready_for_confirmation=True,
            source_turn_id="archive-agent-turn",
            source_artifact_step_id="archive-agent-artifact",
        )

    confirmed = client.post(
        f"/api/v2/products/{product.id}/workflow-drafts/missing/confirm",
        json={"expected_draft_version": 1},
    )
    assert confirmed.status_code == 409, confirmed.text
    assert PRODUCT_WORKFLOW_DRAFT_RETIRED in confirmed.json()["detail"]

    persisted = client.post(
        f"/api/v3/products/{product.id}/workflow-drafts/missing/graphs",
        json={"expected_draft_version": 1},
    )
    assert persisted.status_code == 409, persisted.text
    assert PRODUCT_WORKFLOW_DRAFT_RETIRED in persisted.json()["detail"]

    persisted_archive = db_session.get(LegacyWorkflowArchive, workflow.id)
    assert persisted_archive is not None
    assert persisted_archive.payload_json == archived_payload
    assert db_session.scalar(select(func.count()).select_from(WorkflowDraft)) == 0


def test_archive_rebuild_conflicts_before_writing_when_target_has_live_v3_graph(
    configured_env,
    db_session,
) -> None:
    product, _, workflow, _, _ = _seed_archives(db_session)
    create_empty_workflow_graph(db_session, product_id=product.id)
    client = TestClient(create_app())
    _login(client)

    response = client.post(
        f"/api/v2/legacy-archives/workflow/{workflow.id}/agent-rebuilds",
        json={"target_product_id": product.id, "idempotency_key": "archive-live-graph-conflict"},
    )

    assert response.status_code == 409, response.text
    assert PRODUCT_WORKFLOW_DRAFT_RETIRED in response.json()["detail"]
    db_session.expire_all()
    assert db_session.scalar(select(func.count()).select_from(WorkflowDraft)) == 0
    assert db_session.scalar(select(func.count()).select_from(AgentConversation)) == 0
    assert db_session.scalar(select(func.count()).select_from(WorkflowDraftLegacyArchiveSeed)) == 0


def test_agent_archive_tools_are_product_scoped_sectioned_and_metadata_only(
    configured_env,
    db_session,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    product, _, workflow, _, _ = _seed_archives(db_session)
    workflow.payload_json = {
        "nodes": [{"id": f"prompt-{index}", "config": {"prompt": f"提示词 {index}"}} for index in range(12)],
        "edges": [],
    }
    workflow.node_count = 12
    huge_archive = LegacyWorkflowArchive(
        id="archive-workflow-huge",
        source_profile=workflow.source_profile,
        legacy_workflow_id="legacy-workflow-huge",
        product_id=product.id,
        source_title="超大旧工作流",
        source_updated_at=None,
        archive_schema_version=1,
        payload_json={"nodes": [{"id": "huge", "config": {"prompt": "x" * (257 * 1024)}}], "edges": []},
        source_fingerprint_sha256="9" * 64,
        payload_sha256="a" * 64,
        node_count=1,
        edge_count=0,
        run_count=0,
        node_run_count=0,
        asset_count=0,
    )
    db_session.add(huge_archive)
    db_session.commit()

    create_empty_workflow_graph(db_session, product_id=product.id)
    workspace = attach_agent_workspace_to_product(
        db_session,
        product_id=product.id,
        idempotency_key="agent-tool-scope",
    )
    conversation_id = workspace.conversation.id
    client = TestClient(create_app())
    _login(client)

    internal_token = "agent-internal-token-with-at-least-32-characters"
    monkeypatch.setenv("AGENT_SERVICE_INTERNAL_TOKEN", internal_token)
    get_settings.cache_clear()
    headers = {"Authorization": f"Bearer {internal_token}"}
    base = f"/api/internal/v1/agent-conversations/{conversation_id}"

    context = client.get(f"{base}/product-context", headers=headers)
    assert context.status_code == 200, context.text
    payload = context.json()
    assert "legacy_archive_seed" not in payload
    assert "workflow_draft" not in payload

    listed = client.get(
        f"{base}/legacy-archives",
        headers=headers,
        params={"kind": "workflow", "limit": 10},
    )
    assert listed.status_code == 200, listed.text
    assert {item["id"] for item in listed.json()["items"]} == {workflow.id, huge_archive.id}
    assert all("payload" not in item for item in listed.json()["items"])
    assert "storage_path" not in listed.text
    assert "download_url" not in listed.text

    nodes = client.post(
        f"{base}/legacy-archives/inspect",
        headers=headers,
        json={
            "kind": "workflow",
            "archive_id": workflow.id,
            "section": "nodes",
            "offset": 3,
            "limit": 5,
        },
    )
    assert nodes.status_code == 200, nodes.text
    assert nodes.json()["total"] == 12
    assert [item["id"] for item in nodes.json()["items"]] == [f"prompt-{index}" for index in range(3, 8)]
    assert nodes.json()["has_more"] is True
    assert "edges" not in nodes.json()

    assets = client.post(
        f"{base}/legacy-archives/inspect",
        headers=headers,
        json={
            "kind": "workflow",
            "archive_id": workflow.id,
            "section": "assets",
            "offset": 0,
            "limit": 10,
        },
    )
    assert assets.status_code == 200, assets.text
    assert assets.json()["total"] == 1
    assert assets.json()["items"][0]["product_image_asset_id"] == product.image_assets[0].id
    assert "storage_path" not in assets.text
    assert "download_url" not in assets.text
    assert "data:image/" not in assets.text

    other_product_archive = client.post(
        f"{base}/legacy-archives/inspect",
        headers=headers,
        json={
            "kind": "workflow",
            "archive_id": "archive-workflow-other",
            "section": "summary",
            "offset": 0,
            "limit": 1,
        },
    )
    assert other_product_archive.status_code == 404

    unbounded = client.post(
        f"{base}/legacy-archives/inspect",
        headers=headers,
        json={
            "kind": "workflow",
            "archive_id": workflow.id,
            "section": "nodes",
            "offset": 0,
            "limit": 11,
        },
    )
    assert unbounded.status_code == 422

    oversized = client.post(
        f"{base}/legacy-archives/inspect",
        headers=headers,
        json={
            "kind": "workflow",
            "archive_id": huge_archive.id,
            "section": "nodes",
            "offset": 0,
            "limit": 1,
        },
    )
    assert oversized.status_code == 409
    assert "256 KiB" in oversized.json()["detail"]
