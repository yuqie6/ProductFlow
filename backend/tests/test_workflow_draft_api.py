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
    assert created.status_code == 409, created.text
    assert "不再使用 WorkflowDraft" in created.json()["detail"]
    empty = client.post(f"/api/v3/products/{product_id}/workflows")
    assert empty.status_code == 201, empty.text
    graph = empty.json()
    assert graph["schema_version"] == 3


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
