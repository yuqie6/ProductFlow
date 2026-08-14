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
    payload = response.json()
    return {**payload["product"], "created_assets": payload["created_assets"]}


def _sse_events(response) -> list[dict]:
    events: list[dict] = []
    for block in response.text.strip().split("\n\n"):
        lines = block.splitlines()
        event_id = int(next(line.removeprefix("id: ") for line in lines if line.startswith("id: ")))
        event_name = next(line.removeprefix("event: ") for line in lines if line.startswith("event: "))
        data = json.loads(next(line.removeprefix("data: ") for line in lines if line.startswith("data: ")))
        events.append({"id": event_id, "event": event_name, "data": data})
    return events


def test_workflow_draft_api_materializes_v2_and_replays_reveal_events(configured_env, monkeypatch) -> None:
    from productflow_backend.application.product_workflow.execution import (
        execute_product_workflow_node_run,
        execute_product_workflow_run,
    )
    from productflow_backend.application.product_workflow.run_state import mark_workflow_run_failed
    from productflow_backend.application.product_workflow.v2_runs import (
        retry_v2_workflow_run as retry_v2_workflow_run_application,
    )
    from productflow_backend.application.product_workflow.v2_runs import (
        submit_v2_workflow_node_run,
        submit_v2_workflow_run,
    )
    from productflow_backend.presentation.api import create_app
    from productflow_backend.presentation.routes import workflow_drafts as workflow_draft_routes

    enqueued_run_ids: list[str] = []
    enqueued_full_run_ids: list[str] = []

    def submit_without_redis(session, *, node_id: str):
        return submit_v2_workflow_node_run(
            session,
            node_id=node_id,
            enqueue=enqueued_run_ids.append,
        )

    monkeypatch.setattr(workflow_draft_routes, "submit_v2_workflow_node_run", submit_without_redis)

    def submit_full_without_redis(session, *, product_id: str, workflow_id: str):
        return submit_v2_workflow_run(
            session,
            product_id=product_id,
            workflow_id=workflow_id,
            enqueue=enqueued_full_run_ids.append,
        )

    monkeypatch.setattr(workflow_draft_routes, "submit_v2_workflow_run", submit_full_without_redis)

    def retry_full_without_redis(session, *, product_id: str, workflow_id: str, run_id: str):
        return retry_v2_workflow_run_application(
            session,
            product_id=product_id,
            workflow_id=workflow_id,
            run_id=run_id,
            enqueue=enqueued_full_run_ids.append,
        )

    monkeypatch.setattr(workflow_draft_routes, "retry_v2_workflow_run", retry_full_without_redis)

    client = TestClient(create_app())
    _login(client)
    product = _create_canonical_product(client)
    product_id = product["id"]
    reference_asset_id = product["created_assets"][0]["id"]

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
    visual_system_version_id = confirmed.json()["current_revision"]["visual_system_version_id"]
    assert visual_system_version_id

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
    assert workflow["visual_system_version_id"] == visual_system_version_id
    assert len(workflow["folders"]) == 1
    assert len(workflow["nodes"]) == 5
    assert len(workflow["edges"]) == 4
    assert any(
        node["bound_image_asset_id"] == reference_asset_id
        for node in workflow["nodes"]
        if node["node_type"] == "reference_image"
    )
    prompt_node = next(node for node in workflow["nodes"] if node["node_type"] == "prompt_generation")
    assert prompt_node["current_prompt_artifact_version_id"]
    image_node = next(
        node
        for node in workflow["nodes"]
        if node["node_type"] == "image_generation" and node["config_json"]["image_plan_key"] == "hero-1"
    )
    assert image_node["current_prompt_artifact_version_id"] is None

    submitted_prompt = client.post(f"/api/v2/workflow-nodes/{prompt_node['id']}/run")
    assert submitted_prompt.status_code == 202, submitted_prompt.text
    prompt_submission = submitted_prompt.json()
    assert prompt_submission["created"] is True
    prompt_node_run_id = prompt_submission["node_run"]["id"]
    assert prompt_submission["node_run"]["status"] == "queued"
    assert prompt_submission["node_run"]["visual_system_version_id"] == visual_system_version_id

    duplicate_prompt = client.post(f"/api/v2/workflow-nodes/{prompt_node['id']}/run")
    assert duplicate_prompt.status_code == 202
    assert duplicate_prompt.json()["created"] is False
    assert duplicate_prompt.json()["node_run"]["id"] == prompt_node_run_id
    prompt_workflow_run_id = prompt_submission["node_run"]["workflow_run_id"]
    assert enqueued_run_ids == [prompt_workflow_run_id]

    execute_product_workflow_node_run(prompt_node_run_id)
    execute_product_workflow_run(prompt_workflow_run_id)
    completed_prompt = client.get(f"/api/v2/workflow-node-runs/{prompt_node_run_id}")
    assert completed_prompt.status_code == 200, completed_prompt.text
    completed_prompt_payload = completed_prompt.json()
    assert completed_prompt_payload["status"] == "succeeded"
    assert completed_prompt_payload["prompt_artifact_version_id"] != prompt_node[
        "current_prompt_artifact_version_id"
    ]
    assert completed_prompt_payload["generation_record_id"] is None
    assert completed_prompt_payload["reference_asset_ids"] == [reference_asset_id]

    submitted_image = client.post(f"/api/v2/workflow-nodes/{image_node['id']}/run")
    assert submitted_image.status_code == 202, submitted_image.text
    image_node_run_id = submitted_image.json()["node_run"]["id"]
    execute_product_workflow_node_run(image_node_run_id)
    execute_product_workflow_run(submitted_image.json()["node_run"]["workflow_run_id"])
    completed_image = client.get(f"/api/v2/workflow-node-runs/{image_node_run_id}")
    assert completed_image.status_code == 200, completed_image.text
    completed_image_payload = completed_image.json()
    assert completed_image_payload["status"] == "succeeded"
    assert completed_image_payload["generation_record_id"]
    assert completed_image_payload["result_asset_id"]
    assert completed_image_payload["requested_spec"] == image_node["config_json"]["generation_spec"]
    assert completed_image_payload["effective_parameters"]["adapter"] == "mock"
    assert completed_image_payload["actual_media"]["mime_type"] == "image/png"
    assert completed_image_payload["actual_media"]["width"] > 0
    assert completed_image_payload["actual_media"]["height"] > 0
    assert completed_image_payload["compiled_prompt"]
    assert completed_image_payload["reference_asset_ids"] == [reference_asset_id]
    assert "provider_request_json" not in completed_image_payload

    submitted_full = client.post(f"/api/v2/products/{product_id}/workflows/{workflow['id']}/runs")
    assert submitted_full.status_code == 202, submitted_full.text
    full_payload = submitted_full.json()
    full_run_id = full_payload["workflow_run"]["id"]
    assert full_payload["created"] is True
    assert full_payload["workflow"]["id"] == workflow["id"]
    assert len(full_payload["workflow_run"]["node_runs"]) == 3
    assert enqueued_full_run_ids == [full_run_id]

    duplicate_full = client.post(f"/api/v2/products/{product_id}/workflows/{workflow['id']}/runs")
    assert duplicate_full.status_code == 202, duplicate_full.text
    assert duplicate_full.json()["created"] is False
    assert duplicate_full.json()["workflow_run"]["id"] == full_run_id
    queried_full = client.get(
        f"/api/v2/products/{product_id}/workflows/{workflow['id']}/runs/{full_run_id}"
    )
    listed_full = client.get(f"/api/v2/products/{product_id}/workflows/{workflow['id']}/runs")
    assert queried_full.status_code == 200, queried_full.text
    assert queried_full.json()["workflow_run"]["id"] == full_run_id
    assert listed_full.status_code == 200, listed_full.text
    assert listed_full.json()["items"][0]["id"] == full_run_id
    cancelled_full = client.post(
        f"/api/v2/products/{product_id}/workflows/{workflow['id']}/runs/{full_run_id}/cancel"
    )
    assert cancelled_full.status_code == 200, cancelled_full.text
    assert cancelled_full.json()["workflow_run"]["status"] == "cancelled"

    image_runs = client.get(f"/api/v2/workflow-nodes/{image_node['id']}/runs", params={"limit": 1})
    assert image_runs.status_code == 200, image_runs.text
    full_image_node_run_id = next(
        item["id"]
        for item in full_payload["workflow_run"]["node_runs"]
        if item["node_id"] == image_node["id"]
    )
    assert [item["id"] for item in image_runs.json()["items"]] == [full_image_node_run_id]

    retry_source_response = client.post(
        f"/api/v2/products/{product_id}/workflows/{workflow['id']}/runs"
    )
    assert retry_source_response.status_code == 202, retry_source_response.text
    retry_source_run_id = retry_source_response.json()["workflow_run"]["id"]
    failure_session = get_session_factory()()
    try:
        mark_workflow_run_failed(
            failure_session,
            run_id=retry_source_run_id,
            failed_node_id=None,
            reason="API retry source failure",
        )
    finally:
        failure_session.close()
    retried_full = client.post(
        f"/api/v2/products/{product_id}/workflows/{workflow['id']}/runs/{retry_source_run_id}/retry"
    )
    assert retried_full.status_code == 202, retried_full.text
    retried_full_payload = retried_full.json()
    retried_full_run_id = retried_full_payload["workflow_run"]["id"]
    assert retried_full_payload["created"] is True
    assert retried_full_payload["workflow_run"]["progress_metadata"]["source_run_id"] == retry_source_run_id
    assert retried_full_payload["workflow_run"]["progress_metadata"]["manual_retry"] is True
    assert enqueued_full_run_ids[-1] == retried_full_run_id
    cancelled_retry = client.post(
        f"/api/v2/products/{product_id}/workflows/{workflow['id']}/runs/{retried_full_run_id}/cancel"
    )
    assert cancelled_retry.status_code == 200, cancelled_retry.text

    submitted_for_cancel = client.post(f"/api/v2/workflow-nodes/{prompt_node['id']}/run")
    assert submitted_for_cancel.status_code == 202, submitted_for_cancel.text
    cancelled = client.post(
        f"/api/v2/workflow-node-runs/{submitted_for_cancel.json()['node_run']['id']}/cancel"
    )
    assert cancelled.status_code == 200, cancelled.text
    assert cancelled.json()["status"] == "failed"
    assert cancelled.json()["failure_reason"] == "已取消"

    queried = client.get(f"/api/v2/products/{product_id}/workflow")
    assert queried.status_code == 200
    assert queried.json()["latest_revision"] == 1
    assert queried.json()["workflow"]["id"] == workflow["id"]
    refreshed_nodes = queried.json()["workflow"]["nodes"]
    refreshed_prompt = next(node for node in refreshed_nodes if node["id"] == prompt_node["id"])
    refreshed_image = next(node for node in refreshed_nodes if node["id"] == image_node["id"])
    assert refreshed_prompt["current_prompt_artifact_version_id"] == completed_prompt_payload[
        "prompt_artifact_version_id"
    ]
    assert refreshed_image["bound_image_asset_id"] == completed_image_payload["result_asset_id"]

    uploaded_replacement = client.post(
        f"/api/v2/products/{product_id}/image-assets",
        files=[("images", ("replacement.png", _make_demo_image_bytes(), "image/png"))],
    )
    assert uploaded_replacement.status_code == 201, uploaded_replacement.text
    replacement_asset_id = uploaded_replacement.json()["items"][0]["id"]
    bind_url = (
        f"/api/v2/products/{product_id}/workflows/{workflow['id']}"
        f"/reference-nodes/{next(node['id'] for node in workflow['nodes'] if node['node_type'] == 'reference_image')}"
    )
    invalid_bind = client.patch(
        bind_url,
        json={
            "asset_id": replacement_asset_id,
            "expected_workflow_revision": workflow["revision"],
            "expected_bound_asset_id": reference_asset_id,
            "unknown": True,
        },
    )
    assert invalid_bind.status_code == 422
    rebound = client.patch(
        bind_url,
        json={
            "asset_id": replacement_asset_id,
            "expected_workflow_revision": workflow["revision"],
            "expected_bound_asset_id": reference_asset_id,
        },
    )
    assert rebound.status_code == 200, rebound.text
    rebound_payload = rebound.json()
    assert rebound_payload["changed"] is True
    assert rebound_payload["previous_asset_id"] == reference_asset_id
    assert rebound_payload["reference_node"]["bound_image_asset_id"] == replacement_asset_id
    assert prompt_node["id"] in rebound_payload["affected_node_ids"]
    assert image_node["id"] in rebound_payload["affected_node_ids"]
    stale_image_run = client.post(f"/api/v2/workflow-nodes/{image_node['id']}/run")
    assert stale_image_run.status_code == 409
    assert "重新生成提示词" in stale_image_run.json()["detail"]

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
    payload = make_workflow_draft_payload(reference_asset_id=product["created_assets"][0]["id"])
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
    payload = make_workflow_draft_payload(reference_asset_id=product["created_assets"][0]["id"])
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
