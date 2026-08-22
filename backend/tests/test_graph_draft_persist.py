from __future__ import annotations

import pytest
from fastapi.testclient import TestClient
from helpers import _login, _make_demo_image_bytes
from sqlalchemy import func, select
from workflow_draft_helpers import clone_workflow_draft_payload, make_workflow_draft_payload

from productflow_backend.application.product_workflow.draft_graph_adapter import (
    build_draft_initial_graph_change_set,
)
from productflow_backend.application.workflow_drafts.contracts import parse_workflow_draft_payload
from productflow_backend.domain.errors import StructuredBusinessValidationError
from productflow_backend.infrastructure.db.models import WorkflowDraft, WorkflowGraph
from productflow_backend.infrastructure.db.session import get_session_factory
from productflow_backend.presentation.api import create_app


def test_confirmed_draft_persists_v3_graph_and_conflicts_when_graph_exists(configured_env) -> None:
    client = TestClient(create_app())
    _login(client)
    product = client.post(
        "/api/v2/products",
        data={"name": "Draft 物化商品", "category": "工业收纳", "price": "12.00"},
        files=[("images", ("product.png", _make_demo_image_bytes(), "image/png"))],
    )
    assert product.status_code == 201, product.text
    payload = product.json()
    product_id = payload["product"]["id"]
    asset_id = payload["created_assets"][0]["id"]
    created = client.post(
        f"/api/v2/products/{product_id}/workflow-drafts",
        json={
            "payload": make_workflow_draft_payload(reference_asset_id=asset_id),
            "ready_for_confirmation": True,
        },
    )
    assert created.status_code == 201, created.text
    draft_id = created.json()["id"]
    confirmed = client.post(
        f"/api/v2/products/{product_id}/workflow-drafts/{draft_id}/confirm",
        json={"expected_draft_version": 1},
    )
    assert confirmed.status_code == 200, confirmed.text
    persisted = client.post(
        f"/api/v3/products/{product_id}/workflow-drafts/{draft_id}/graphs",
        json={"expected_draft_version": 1},
    )
    assert persisted.status_code == 200, persisted.text
    graph = persisted.json()["graph"]
    assert persisted.json()["created"] is True
    assert graph["schema_version"] == 3
    assert graph["revision"] == 1
    assert any(node["node_type"] == "image_asset" for node in graph["nodes"])
    assert any(node["node_type"] == "product_source" for node in graph["nodes"])
    assert all("prompt_plan_key" not in (node.get("config") or {}) for node in graph["nodes"])
    replay = client.post(
        f"/api/v3/products/{product_id}/workflow-drafts/{draft_id}/graphs",
        json={"expected_draft_version": 1},
    )
    assert replay.status_code == 200, replay.text
    assert replay.json()["created"] is False
    assert replay.json()["graph"]["id"] == graph["id"]
    factory = get_session_factory()
    session = factory()
    try:
        assert session.scalar(select(func.count()).select_from(WorkflowGraph)) == 1
        assert session.scalar(select(WorkflowDraft.status).where(WorkflowDraft.id == draft_id)) == "ready"
    finally:
        session.close()

    direct = client.post(
        "/api/v3/products",
        data={
            "name": "挡住 Draft 的图",
            "image_types": '[{"key":"hero","quantity":1}]',
        },
        files=[("images", ("product.png", _make_demo_image_bytes(), "image/png"))],
    )
    assert direct.status_code == 201, direct.text
    blocked_product = direct.json()["product"]["id"]
    blocked_asset = direct.json()["created_assets"][0]["id"]
    blocked_draft = client.post(
        f"/api/v2/products/{blocked_product}/workflow-drafts",
        json={
            "payload": make_workflow_draft_payload(reference_asset_id=blocked_asset),
            "ready_for_confirmation": True,
        },
    )
    assert blocked_draft.status_code == 201, blocked_draft.text
    blocked_confirm = client.post(
        f"/api/v2/products/{blocked_product}/workflow-drafts/{blocked_draft.json()['id']}/confirm",
        json={"expected_draft_version": 1},
    )
    assert blocked_confirm.status_code == 200, blocked_confirm.text
    conflict = client.post(
        f"/api/v3/products/{blocked_product}/workflow-drafts/{blocked_draft.json()['id']}/graphs",
        json={"expected_draft_version": 1},
    )
    assert conflict.status_code == 409


def test_draft_adapter_maps_declared_reference_edges_only() -> None:
    payload = parse_workflow_draft_payload(make_workflow_draft_payload())
    change_set = build_draft_initial_graph_change_set(payload, draft_revision_id="rev-1")
    connect_ops = [op for op in change_set.operations if op.op == "connect_nodes"]
    image_refs = {
        (op.source_ref, op.target_ref)
        for op in connect_ops
        if op.target_ref in {"hero-image-1-node", "hero-image-2-node"} and op.source_ref == "product-reference-node"
    }
    assert image_refs == {
        ("product-reference-node", "hero-image-1-node"),
        ("product-reference-node", "hero-image-2-node"),
    }
    assert all(not op.client_ref.startswith("edge-reference-") for op in connect_ops)


def test_draft_adapter_fails_when_image_lacks_reference_edge() -> None:
    raw = clone_workflow_draft_payload(make_workflow_draft_payload())
    raw["edges"] = [edge for edge in raw["edges"] if not str(edge["key"]).startswith("reference-to-image")]
    payload = parse_workflow_draft_payload(raw)
    with pytest.raises(StructuredBusinessValidationError, match="参考图边") as caught:
        build_draft_initial_graph_change_set(payload, draft_revision_id="rev-1")
    assert caught.value.error_code == "draft_graph_image_missing_reference"
    assert {issue["path"] for issue in caught.value.issues} == {
        "nodes.hero-image-1-node",
        "nodes.hero-image-2-node",
    }
