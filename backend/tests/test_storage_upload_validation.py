from __future__ import annotations

from pathlib import Path

import pytest
from fastapi.testclient import TestClient
from helpers import (
    _execute_workflow_queue_inline,
    _login,
    _make_demo_image_bytes,
    _make_demo_image_bytes_with_size,
    _read_image_size,
)


@pytest.fixture(autouse=True)
def _execute_workflow_queue_inline_fixture(monkeypatch: pytest.MonkeyPatch) -> None:
    """Keep API workflow tests deterministic while production delivery goes through Dramatiq."""

    _execute_workflow_queue_inline(monkeypatch)


def test_product_asset_variant_urls_serve_preview_and_thumbnail(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)

    create_product_response = client.post(
        "/api/products",
        data={"name": "大尺寸主图样例", "category": "个护", "price": "99.00"},
        files={"image": ("large.png", _make_demo_image_bytes_with_size(2400, 1800), "image/png")},
    )
    assert create_product_response.status_code == 201
    source_asset = next(
        asset for asset in create_product_response.json()["source_assets"] if asset["kind"] == "original_image"
    )

    assert source_asset["download_url"].startswith("/api/source-assets/")
    assert source_asset["preview_url"].endswith("variant=preview")
    assert source_asset["thumbnail_url"].endswith("variant=thumbnail")

    preview = client.get(source_asset["preview_url"])
    assert preview.status_code == 200
    assert preview.headers["content-type"].startswith("image/")
    assert max(_read_image_size(preview.content)) <= 1600

    thumbnail = client.get(source_asset["thumbnail_url"])
    assert thumbnail.status_code == 200
    assert thumbnail.headers["content-type"].startswith("image/")
    assert max(_read_image_size(thumbnail.content)) <= 320

def test_product_create_rejects_invalid_price_and_invalid_image(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)

    invalid_price = client.post(
        "/api/products",
        data={"name": "护手霜", "category": "个护", "price": "abc"},
        files={"image": ("cream.png", _make_demo_image_bytes(), "image/png")},
    )
    assert invalid_price.status_code == 400

    invalid_image = client.post(
        "/api/products",
        data={"name": "护手霜", "category": "个护", "price": "59.00"},
        files={"image": ("cream.png", b"not an image", "image/png")},
    )
    assert invalid_image.status_code == 400

def test_image_generation_calibrates_oversized_size(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)

    created = client.post("/api/image-sessions", json={"title": "尺寸校验"})
    assert created.status_code == 201
    generated = client.post(
        f"/api/image-sessions/{created.json()['id']}/generate",
        json={"prompt": "生成一张图", "size": "99999x99999"},
    )
    assert generated.status_code == 200
    assert generated.json()["rounds"][-1]["size"] == "3840x3840"
