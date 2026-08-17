from __future__ import annotations

from fastapi.testclient import TestClient
from helpers import _login
from test_media_library import _create_product_asset

from productflow_backend.application.media_library.service import save_media_library_asset_from_product
from productflow_backend.infrastructure.db.models import ProductWorkflow
from productflow_backend.presentation.api import create_app


def test_workflow_media_library_api_keeps_association_explicit_and_non_destructive(db_session) -> None:
    product, source_asset = _create_product_asset(db_session)
    library_asset = save_media_library_asset_from_product(
        db_session,
        product_image_asset_id=source_asset.id,
    ).asset
    workflow = ProductWorkflow(product_id=product.id, title="素材 API 工作流")
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
