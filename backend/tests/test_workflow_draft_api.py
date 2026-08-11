from __future__ import annotations

import json

from fastapi.testclient import TestClient
from helpers import _login, _make_demo_image_bytes
from sqlalchemy import func, select
from workflow_draft_helpers import make_workflow_draft_payload

from productflow_backend.infrastructure.db.models import ProductWorkflow, WorkflowRevealEvent
from productflow_backend.infrastructure.db.session import get_session_factory


def _create_canonical_product(client: TestClient, *, name: str = "硬质刀具收纳套装") -> dict:
    response = client.post(
        "/api/v2/products",
        data={"name": name, "category": "工业收纳", "price": "299.00"},
        files=[("images", ("product.png", _make_demo_image_bytes(), "image/png"))],
    )
    assert response.status_code == 201, response.text
    return response.json()


def _sse_events(response) -> list[dict]:
    events: list[dict] = []
    for block in response.text.strip().split("\n\n"):
        lines = block.splitlines()
        event_id = int(next(line.removeprefix("id: ") for line in lines if line.startswith("id: ")))
        event_name = next(line.removeprefix("event: ") for line in lines if line.startswith("event: "))
        data = json.loads(next(line.removeprefix("data: ") for line in lines if line.startswith("data: ")))
        events.append({"id": event_id, "event": event_name, "data": data})
    return events


def test_workflow_draft_api_materializes_v2_and_replays_reveal_events(configured_env) -> None:
    from productflow_backend.presentation.api import create_app

    client = TestClient(create_app())
    _login(client)
    product = _create_canonical_product(client)
    product_id = product["id"]
    reference_asset_id = product["image_assets"][0]["id"]

    empty = client.get(f"/api/v2/products/{product_id}/workflow")
    assert empty.status_code == 200
    assert empty.json() == {"latest_revision": 0, "workflow": None}
    session = get_session_factory()()
    try:
        assert session.scalar(
            select(func.count()).select_from(ProductWorkflow).where(ProductWorkflow.product_id == product_id)
        ) == 0
    finally:
        session.close()

    draft_payload = make_workflow_draft_payload(reference_asset_id=reference_asset_id)
    created = client.post(
        f"/api/v2/products/{product_id}/workflow-drafts",
        json={
            "payload": draft_payload,
            "ready_for_confirmation": True,
            "source_turn_id": "turn-api-1",
            "source_artifact_step_id": "artifact-api-1",
        },
    )
    assert created.status_code == 201, created.text
    draft = created.json()
    assert draft["status"] == "awaiting_confirmation"
    assert draft["current_revision"]["version"] == 1
    assert draft["limits"] == {
        "min_image_types": 1,
        "min_images_per_type": 1,
        "max_images_per_type": 6,
        "max_total_images": 30,
        "max_reference_assets": 6,
    }

    confirmed = client.post(
        f"/api/v2/products/{product_id}/workflow-drafts/{draft['id']}/confirm",
        json={"expected_draft_version": 1},
    )
    assert confirmed.status_code == 200, confirmed.text
    assert confirmed.json()["status"] == "confirmed"
    assert confirmed.json()["current_revision"]["fact_set_version_id"]

    materialized = client.post(
        f"/api/v2/products/{product_id}/workflow-drafts/{draft['id']}/materialize",
        json={
            "expected_draft_version": 1,
            "expected_workflow_revision": 0,
            "idempotency_key": "api-materialization-1",
        },
    )
    assert materialized.status_code == 200, materialized.text
    materialization = materialized.json()
    workflow = materialization["workflow"]
    assert materialization["created"] is True
    assert workflow["schema_version"] == 2
    assert workflow["revision"] == 1
    assert len(workflow["folders"]) == 1
    assert len(workflow["nodes"]) == 5
    assert len(workflow["edges"]) == 4
    assert any(
        node["bound_image_asset_id"] == reference_asset_id
        for node in workflow["nodes"]
        if node["node_type"] == "reference_image"
    )

    queried = client.get(f"/api/v2/products/{product_id}/workflow")
    assert queried.status_code == 200
    assert queried.json()["latest_revision"] == 1
    assert queried.json()["workflow"]["id"] == workflow["id"]

    legacy_query = client.get(f"/api/products/{product_id}/workflow")
    legacy_run = client.post(f"/api/products/{product_id}/workflow/run", json={})
    legacy_patch = client.patch(
        f"/api/workflow-nodes/{workflow['nodes'][0]['id']}",
        json={"title": "旧入口不应改写"},
    )
    legacy_prompt_create = client.post(
        f"/api/products/{product_id}/workflow/nodes",
        json={
            "node_type": "prompt_generation",
            "title": "旧入口不应创建提示词节点",
            "position_x": 0,
            "position_y": 0,
            "config_json": {},
        },
    )
    assert legacy_query.status_code == 409
    assert legacy_run.status_code == 409
    assert legacy_patch.status_code == 409
    assert legacy_prompt_create.status_code == 409

    stream = client.get(materialization["reveal_events_url"])
    assert stream.status_code == 200
    assert stream.headers["content-type"].startswith("text/event-stream")
    events = _sse_events(stream)
    assert events[0]["id"] == 1
    assert events[-1]["event"] == "completed"
    assert [event["id"] for event in events] == list(range(1, len(events) + 1))
    cursor = events[3]["id"]

    replay = client.get(
        materialization["reveal_events_url"],
        params={"after": cursor - 1},
        headers={"Last-Event-ID": str(cursor)},
    )
    assert replay.status_code == 200
    replayed_events = _sse_events(replay)
    assert [event["id"] for event in replayed_events] == [
        event["id"] for event in events if event["id"] > cursor
    ]
    session = get_session_factory()()
    try:
        assert session.scalar(select(func.count()).select_from(WorkflowRevealEvent)) == len(events)
    finally:
        session.close()


def test_workflow_draft_api_returns_409_for_idempotency_key_parameter_drift(configured_env) -> None:
    from productflow_backend.presentation.api import create_app

    client = TestClient(create_app())
    _login(client)
    product = _create_canonical_product(client)
    payload = make_workflow_draft_payload(reference_asset_id=product["image_assets"][0]["id"])
    created = client.post(
        f"/api/v2/products/{product['id']}/workflow-drafts",
        json={"payload": payload, "ready_for_confirmation": True},
    ).json()
    confirmed = client.post(
        f"/api/v2/products/{product['id']}/workflow-drafts/{created['id']}/confirm",
        json={"expected_draft_version": 1},
    )
    assert confirmed.status_code == 200
    first = client.post(
        f"/api/v2/products/{product['id']}/workflow-drafts/{created['id']}/materialize",
        json={
            "expected_draft_version": 1,
            "expected_workflow_revision": 0,
            "idempotency_key": "stable-key",
        },
    )
    assert first.status_code == 200
    conflict = client.post(
        f"/api/v2/products/{product['id']}/workflow-drafts/{created['id']}/materialize",
        json={
            "expected_draft_version": 1,
            "expected_workflow_revision": 1,
            "idempotency_key": "stable-key",
        },
    )
    assert conflict.status_code == 409
    assert "相同 idempotency key" in conflict.json()["detail"]


def test_v2_workflow_query_ignores_v1_without_modifying_it(configured_env) -> None:
    from productflow_backend.presentation.api import create_app

    client = TestClient(create_app())
    _login(client)
    product = _create_canonical_product(client)
    product_id = product["id"]
    legacy = client.get(f"/api/products/{product_id}/workflow")
    assert legacy.status_code == 200
    legacy_payload = legacy.json()

    queried = client.get(f"/api/v2/products/{product_id}/workflow")
    assert queried.status_code == 200
    assert queried.json() == {"latest_revision": 0, "workflow": None}
    legacy_after = client.get(f"/api/products/{product_id}/workflow")
    assert legacy_after.status_code == 200
    assert legacy_after.json()["id"] == legacy_payload["id"]
    assert {node["id"] for node in legacy_after.json()["nodes"]} == {
        node["id"] for node in legacy_payload["nodes"]
    }


def test_legacy_node_create_rejects_prompt_without_creating_a_default_workflow(configured_env) -> None:
    from productflow_backend.presentation.api import create_app

    client = TestClient(create_app())
    _login(client)
    product = _create_canonical_product(client)

    rejected = client.post(
        f"/api/products/{product['id']}/workflow/nodes",
        json={
            "node_type": "prompt_generation",
            "title": "提示词",
            "position_x": 0,
            "position_y": 0,
            "config_json": {},
        },
    )

    assert rejected.status_code == 400
    assert "confirmed WorkflowDraft" in rejected.json()["detail"]
    session = get_session_factory()()
    try:
        assert session.scalar(
            select(func.count()).select_from(ProductWorkflow).where(ProductWorkflow.product_id == product["id"])
        ) == 0
    finally:
        session.close()


def test_workflow_draft_requests_reject_unknown_fields_and_bad_sse_cursor(configured_env) -> None:
    from productflow_backend.presentation.api import create_app

    client = TestClient(create_app())
    _login(client)
    product = _create_canonical_product(client)
    payload = make_workflow_draft_payload(reference_asset_id=product["image_assets"][0]["id"])
    invalid = client.post(
        f"/api/v2/products/{product['id']}/workflow-drafts",
        json={"payload": payload, "ready_for_confirmation": True, "unknown": True},
    )
    assert invalid.status_code == 422

    missing = client.get(
        "/api/v2/workflow-materializations/00000000-0000-0000-0000-000000000000/reveal-events",
        headers={"Last-Event-ID": "not-an-integer"},
    )
    assert missing.status_code == 400
    assert "Last-Event-ID" in missing.json()["detail"]
