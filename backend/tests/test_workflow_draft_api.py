from __future__ import annotations

from fastapi.testclient import TestClient
from helpers import _login, _make_demo_image_bytes
from sqlalchemy import select
from workflow_draft_helpers import make_workflow_draft_payload

from productflow_backend.infrastructure.db.models import AsyncDispatch
from productflow_backend.infrastructure.db.session import get_session_factory
from productflow_backend.presentation.api import create_app


def _create_canonical_product(client: TestClient, *, name: str = "硬质刀具收纳套装") -> dict:
    response = client.post(
        "/api/v2/products",
        data={"name": name, "category": "工业收纳", "price": "299.00"},
        files=[("images", ("product.png", _make_demo_image_bytes(), "image/png"))],
    )
    assert response.status_code == 201, response.text
    payload = response.json()
    return {**payload["product"], "created_assets": payload["created_assets"]}


def test_workflow_draft_api_confirms_persists_v3_graph_and_submits_run(configured_env) -> None:
    client = TestClient(create_app())
    _login(client)
    product = _create_canonical_product(client)
    product_id = product["id"]
    reference_asset_id = product["created_assets"][0]["id"]

    created = client.post(
        f"/api/v2/products/{product_id}/workflow-drafts",
        json={
            "payload": make_workflow_draft_payload(reference_asset_id=reference_asset_id),
            "ready_for_confirmation": True,
            "source_turn_id": "turn-api-v3",
            "source_artifact_step_id": "artifact-api-v3",
        },
    )
    assert created.status_code == 201, created.text
    draft = created.json()
    assert draft["status"] == "awaiting_confirmation"
    assert draft["current_revision"]["version"] == 1

    confirmed = client.post(
        f"/api/v2/products/{product_id}/workflow-drafts/{draft['id']}/confirm",
        json={"expected_draft_version": 1},
    )
    assert confirmed.status_code == 200, confirmed.text
    confirmed_payload = confirmed.json()
    assert confirmed_payload["status"] == "confirmed"
    assert confirmed_payload["current_revision"]["fact_set_version_id"]

    persisted = client.post(
        f"/api/v3/products/{product_id}/workflow-drafts/{draft['id']}/graphs",
        json={"expected_draft_version": 1},
    )
    assert persisted.status_code == 200, persisted.text
    persisted_payload = persisted.json()
    graph = persisted_payload["graph"]
    assert persisted_payload["created"] is True
    assert graph["schema_version"] == 3
    assert graph["revision"] == 1
    assert graph["source_draft_revision_id"] == confirmed_payload["current_revision"]["id"]
    assert graph["title"] == "工业刀具收纳图片工作流"

    replay = client.post(
        f"/api/v3/products/{product_id}/workflow-drafts/{draft['id']}/graphs",
        json={"expected_draft_version": 1},
    )
    assert replay.status_code == 200, replay.text
    assert replay.json()["created"] is False
    assert replay.json()["graph"]["id"] == graph["id"]

    image_node = next(node for node in graph["nodes"] if node["node_type"] == "image_generation")
    run = client.post(
        f"/api/v3/products/{product_id}/workflows/{graph['id']}/runs",
        json={"scope": "to_node", "node_id": image_node["id"]},
    )
    assert run.status_code == 201, run.text
    run_payload = run.json()
    assert run_payload["graph_id"] == graph["id"]
    assert run_payload["status"] == "running"
    assert run_payload["scope"] == "to_node"
    assert run_payload["requested_node_id"] == image_node["id"]
    assert run_payload["graph_revision"] == 1
    assert {item["status"] for item in run_payload["node_runs"]} == {"queued"}
    factory = get_session_factory()
    session = factory()
    try:
        dispatch = session.scalar(
            select(AsyncDispatch).where(AsyncDispatch.aggregate_id == run_payload["id"])
        )
        assert dispatch is not None
        assert dispatch.status.value == "pending"
        assert dispatch.actor_name == "run_workflow_graph_run"
    finally:
        session.close()


def test_workflow_draft_api_rejects_unknown_fields(configured_env) -> None:
    client = TestClient(create_app())
    _login(client)
    product = _create_canonical_product(client)
    invalid = client.post(
        f"/api/v2/products/{product['id']}/workflow-drafts",
        json={
            "payload": make_workflow_draft_payload(reference_asset_id=product["created_assets"][0]["id"]),
            "ready_for_confirmation": True,
            "unknown": True,
        },
    )
    assert invalid.status_code == 422
