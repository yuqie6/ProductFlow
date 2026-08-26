from __future__ import annotations

import json

from fastapi.testclient import TestClient
from helpers import _login
from sqlalchemy import select
from test_local_image_edits import SupportingImageProvider, _context, _mask_bytes

from productflow_backend.application.async_delivery import delivery_key_for_actor
from productflow_backend.infrastructure.db.models import AsyncDispatch
from productflow_backend.infrastructure.db.session import get_session_factory
from productflow_backend.presentation.api import create_app


def _geometry() -> str:
    return json.dumps(
        {
            "source_width": 4,
            "source_height": 3,
            "viewport_width": 4,
            "viewport_height": 3,
            "viewport_to_source": [1, 0, 0, 1, 0, 0],
        }
    )


def _create_payload(source_asset_id: str) -> tuple[dict[str, str], dict[str, tuple[str, bytes, str]]]:
    return (
        {
            "source_asset_id": source_asset_id,
            "operation": "inpaint",
            "instruction": "擦除选区",
            "mask_geometry_json": _geometry(),
            "reference_asset_ids_json": "[]",
        },
        {"mask": ("mask.png", _mask_bytes(4, 3), "image/png")},
    )


def test_local_image_edit_api_uses_structured_multipart_and_durable_submit(
    configured_env,
    db_session,
    monkeypatch,
) -> None:
    monkeypatch.setattr(
        "productflow_backend.application.local_image_edits.service.get_image_provider",
        lambda: SupportingImageProvider(),
    )
    context = _context(db_session)
    client = TestClient(create_app())
    _login(client)

    data, files = _create_payload(context.source_asset_id)
    created = client.post(
        f"/api/v3/products/{context.product_id}/image-edits",
        data=data,
        files=files,
    )
    assert created.status_code == 201, created.text
    task_id = created.json()["id"]
    assert created.json()["status"] == "draft"

    malformed = dict(data)
    malformed["mask_geometry_json"] = "{bad-json"
    rejected = client.post(
        f"/api/v3/products/{context.product_id}/image-edits",
        data=malformed,
        files=files,
    )
    assert rejected.status_code == 400

    legacy_alias = dict(data)
    legacy_alias.pop("mask_geometry_json")
    legacy_alias["mask_geometry"] = _geometry()
    alias_rejected = client.post(
        f"/api/v3/products/{context.product_id}/image-edits",
        data=legacy_alias,
        files=files,
    )
    assert alias_rejected.status_code == 400

    submitted = client.post(
        f"/api/v3/products/{context.product_id}/image-edits/{task_id}/submit",
        json={"idempotency_key": "api-edit-1"},
    )
    assert submitted.status_code == 202, submitted.text
    assert submitted.json()["status"] == "queued"
    assert submitted.json()["requested_provider_name"] == "fixture-image-provider"
    assert submitted.json()["requested_local_edit_mode"] == "masked_edit"
    replay = client.post(
        f"/api/v3/products/{context.product_id}/image-edits/{task_id}/submit",
        json={"idempotency_key": "api-edit-1"},
    )
    assert replay.status_code == 202

    session = get_session_factory()()
    try:
        dispatch = session.scalar(
            select(AsyncDispatch).where(
                AsyncDispatch.delivery_key == delivery_key_for_actor("run_local_image_edit_task", task_id)
            )
        )
        assert dispatch is not None
        assert dispatch.payload_json["task_id"] == task_id
    finally:
        session.close()


def test_local_image_edit_api_hides_cross_product_task_and_capability_is_secret_free(
    configured_env,
    db_session,
) -> None:
    context = _context(db_session)
    other = _context(db_session)
    client = TestClient(create_app())
    _login(client)
    data, files = _create_payload(context.source_asset_id)
    created = client.post(
        f"/api/v3/products/{context.product_id}/image-edits",
        data=data,
        files=files,
    )
    assert created.status_code == 201
    task_id = created.json()["id"]

    hidden = client.get(f"/api/v3/products/{other.product_id}/image-edits/{task_id}")
    assert hidden.status_code == 404

    capability = client.get("/api/v3/local-image-edits/capability")
    assert capability.status_code == 200, capability.text
    assert "api_key" not in capability.text
    assert "base_url" not in capability.text
    assert "secret" not in capability.text.lower()


def test_submit_replay_from_equivalent_draft_returns_authoritative_task(
    configured_env,
    db_session,
    monkeypatch,
) -> None:
    monkeypatch.setattr(
        "productflow_backend.application.local_image_edits.service.get_image_provider",
        lambda: SupportingImageProvider(),
    )
    context = _context(db_session)
    client = TestClient(create_app())
    _login(client)
    data, files = _create_payload(context.source_asset_id)

    first = client.post(
        f"/api/v3/products/{context.product_id}/image-edits",
        data=data,
        files=files,
    )
    second = client.post(
        f"/api/v3/products/{context.product_id}/image-edits",
        data=data,
        files=files,
    )
    assert first.status_code == 201 and second.status_code == 201
    first_id = first.json()["id"]
    second_id = second.json()["id"]
    assert first_id != second_id

    first_submit = client.post(
        f"/api/v3/products/{context.product_id}/image-edits/{first_id}/submit",
        json={"idempotency_key": "equivalent-draft-key"},
    )
    second_replay = client.post(
        f"/api/v3/products/{context.product_id}/image-edits/{second_id}/submit",
        json={"idempotency_key": "equivalent-draft-key"},
    )
    assert first_submit.status_code == 202, first_submit.text
    assert second_replay.status_code == 202, second_replay.text
    assert second_replay.json()["id"] == first_id
    assert second_replay.json()["status"] == "queued"

    session = get_session_factory()()
    try:
        dispatches = list(
            session.scalars(
                select(AsyncDispatch).where(AsyncDispatch.actor_name == "run_local_image_edit_task")
            )
        )
        assert len(dispatches) == 1
        assert dispatches[0].aggregate_id == first_id
    finally:
        session.close()

    different_data = dict(data)
    different_data["instruction"] = "改成不同意图"
    different = client.post(
        f"/api/v3/products/{context.product_id}/image-edits",
        data=different_data,
        files=files,
    )
    assert different.status_code == 201, different.text
    different_id = different.json()["id"]
    conflict = client.post(
        f"/api/v3/products/{context.product_id}/image-edits/{different_id}/submit",
        json={"idempotency_key": "equivalent-draft-key"},
    )
    assert conflict.status_code == 409, conflict.text


def test_submit_rejects_provider_without_explicit_local_edit_capability(
    configured_env,
    db_session,
) -> None:
    context = _context(db_session)
    client = TestClient(create_app())
    _login(client)
    data, files = _create_payload(context.source_asset_id)
    created = client.post(
        f"/api/v3/products/{context.product_id}/image-edits",
        data=data,
        files=files,
    )
    assert created.status_code == 201, created.text

    submitted = client.post(
        f"/api/v3/products/{context.product_id}/image-edits/{created.json()['id']}/submit",
        json={"idempotency_key": "unsupported-provider"},
    )

    assert submitted.status_code == 400, submitted.text
    assert "未显式声明" in submitted.json()["detail"]
    session = get_session_factory()()
    try:
        assert session.scalar(
            select(AsyncDispatch).where(AsyncDispatch.aggregate_id == created.json()["id"])
        ) is None
    finally:
        session.close()
