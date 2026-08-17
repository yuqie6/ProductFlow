from __future__ import annotations

from fastapi.testclient import TestClient
from helpers import _login

from productflow_backend.presentation.api import create_app


def test_legacy_gallery_api_is_retired(configured_env) -> None:  # noqa: ARG001
    client = TestClient(create_app())
    _login(client)

    assert client.get("/api/gallery").status_code == 404
    assert client.post("/api/gallery", json={"image_session_asset_id": "legacy-asset"}).status_code == 404
