from __future__ import annotations

from fastapi.testclient import TestClient
from helpers import _login
from test_media_library import _create_product_asset

from productflow_backend.application.media_library.service import save_media_library_asset_from_product
from productflow_backend.infrastructure.db.models import WorkflowGraph
from productflow_backend.presentation.api import create_app


def test_workflow_media_library_api_keeps_association_explicit_and_non_destructive(db_session) -> None:
    product, source_asset = _create_product_asset(db_session)
    library_asset = save_media_library_asset_from_product(
        db_session,
        product_image_asset_id=source_asset.id,
    ).asset
    workflow = WorkflowGraph(product_id=product.id, title="素材 API 工作流")
    db_session.add(workflow)
    db_session.commit()

    client = TestClient(create_app())
    _login(client)
    path = f"/api/media-library/workflows/{workflow.id}/media-library"
    query = {"product_id": product.id}

    initial = client.get(path, params=query)
    assert initial.status_code == 200, initial.text
    assert initial.json() == {"workflow_id": workflow.id, "items": []}

    synced = client.post(
        f"{path}/sync",
        params=query,
        json={"media_library_asset_ids": [library_asset.id]},
    )
    assert synced.status_code == 200, synced.text
    payload = synced.json()
    assert payload["workflow_id"] == workflow.id
    assert payload["items"][0]["asset"]["id"] == library_asset.id
    assert payload["items"][0]["product_image_asset_id"] == source_asset.id

    removed = client.delete(
        f"{path}/{library_asset.id}",
        params=query,
    )
    assert removed.status_code == 204, removed.text
    assert client.get(path, params=query).json()["items"] == []


def test_collect_media_library_api_binds_idempotency_key_to_request(db_session) -> None:
    product, source_asset = _create_product_asset(db_session)
    library_asset = save_media_library_asset_from_product(
        db_session,
        product_image_asset_id=source_asset.id,
    ).asset
    _, other_source_asset = _create_product_asset(db_session)
    other_library_asset = save_media_library_asset_from_product(
        db_session,
        product_image_asset_id=other_source_asset.id,
    ).asset

    client = TestClient(create_app())
    _login(client)
    payload = {
        "product_id": product.id,
        "media_library_asset_ids": [library_asset.id],
    }
    headers = {"Idempotency-Key": "collect-library-api-1"}

    first = client.post("/api/media-library/collect", json=payload, headers=headers)
    assert first.status_code == 200, first.text
    assert len(first.json()) == 1

    replay = client.post("/api/media-library/collect", json=payload, headers=headers)
    assert replay.status_code == 200, replay.text
    assert replay.json()[0]["id"] == first.json()[0]["id"]

    changed = client.post(
        "/api/media-library/collect",
        json={**payload, "media_library_asset_ids": [other_library_asset.id]},
        headers=headers,
    )
    assert changed.status_code == 409


def test_upload_media_library_api_creates_verified_direct_upload_asset(db_session) -> None:
    from helpers import _make_demo_image_bytes

    client = TestClient(create_app())
    _login(client)

    image_bytes = _make_demo_image_bytes()
    files = [("files", ("test_direct_upload.png", image_bytes, "image/png"))]

    response = client.post("/api/media-library/upload", files=files)
    assert response.status_code == 201, response.text
    payload = response.json()
    assert len(payload) == 1
    asset = payload[0]
    assert asset["display_name"] == "test_direct_upload.png"
    assert asset["original_filename"] == "test_direct_upload.png"
    assert asset["source_type"] == "direct_upload"
    assert asset["verification_status"] == "verified"
    assert asset["is_archived"] is False



def test_upload_media_library_api_idempotency_key_dedups(db_session) -> None:
    from helpers import _make_demo_image_bytes, _make_demo_image_bytes_with_size

    client = TestClient(create_app())
    _login(client)

    image_bytes = _make_demo_image_bytes()
    files = [("files", ("dup.png", image_bytes, "image/png"))]

    first = client.post("/api/media-library/upload", files=files, headers={"Idempotency-Key": "upload-dup-1"})
    assert first.status_code == 201
    first_ids = [asset["id"] for asset in first.json()]

    # reusing the same key + same params returns the SAME assets (no duplicates)
    second = client.post("/api/media-library/upload", files=files, headers={"Idempotency-Key": "upload-dup-1"})
    assert second.status_code == 201
    assert [asset["id"] for asset in second.json()] == first_ids

    # a different key with identical files still creates a NEW batch (key-scoped, not content-dedup)
    third = client.post("/api/media-library/upload", files=files, headers={"Idempotency-Key": "upload-dup-2"})
    assert third.status_code == 201
    assert [asset["id"] for asset in third.json()] != first_ids

    # same key with different params is a conflict (mirrors /collect)
    other_bytes = _make_demo_image_bytes_with_size(64, 64)
    conflict = client.post(
        "/api/media-library/upload",
        files=[("files", ("other.png", other_bytes, "image/png"))],
        headers={"Idempotency-Key": "upload-dup-1"},
    )
    assert conflict.status_code == 409
