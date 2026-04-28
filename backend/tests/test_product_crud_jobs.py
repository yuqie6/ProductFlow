from __future__ import annotations

from pathlib import Path

import pytest
from fastapi.testclient import TestClient
from helpers import (
    _enable_deletion,
    _execute_workflow_queue_inline,
    _login,
    _make_demo_image_bytes,
    _wait_for_workflow_run,
)

from productflow_backend.application.use_cases import (
    add_reference_images,
    create_product,
)
from productflow_backend.domain.enums import (
    SourceAssetKind,
)
from productflow_backend.infrastructure.db.models import (
    SourceAsset,
)


@pytest.fixture(autouse=True)
def _execute_workflow_queue_inline_fixture(monkeypatch: pytest.MonkeyPatch) -> None:
    """Keep API workflow tests deterministic while production delivery goes through Dramatiq."""

    _execute_workflow_queue_inline(monkeypatch)


def test_product_create_persists_source_note_for_ai_context(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)

    response = client.post(
        "/api/products",
        data={
            "name": "露营保温杯",
            "category": "户外",
            "price": "79.00",
            "source_note": "316 不锈钢，主打长效保温和车载杯架适配。",
        },
        files={"image": ("cup.png", _make_demo_image_bytes(), "image/png")},
    )

    assert response.status_code == 201
    payload = response.json()
    assert payload["source_note"] == "316 不锈钢，主打长效保温和车载杯架适配。"

    minimal = client.post(
        "/api/products",
        data={"name": "极简商品壳"},
        files={"image": ("minimal.png", _make_demo_image_bytes(), "image/png")},
    )
    assert minimal.status_code == 201
    minimal_payload = minimal.json()
    assert minimal_payload["category"] is None
    assert minimal_payload["price"] is None
    assert minimal_payload["source_note"] is None


def test_legacy_jobrun_routes_are_removed(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)

    created = client.post(
        "/api/products",
        data={"name": "无传统任务商品"},
        files={"image": ("legacy.png", _make_demo_image_bytes(), "image/png")},
    )
    assert created.status_code == 201
    product_id = created.json()["id"]

    assert client.post(f"/api/products/{product_id}/copy-jobs").status_code == 404
    assert client.post(f"/api/products/{product_id}/poster-jobs").status_code == 404
    assert client.post("/api/posters/missing/regenerate").status_code == 404
    assert client.get("/api/jobs/missing").status_code == 404

    history = client.get(f"/api/products/{product_id}/history")
    assert history.status_code == 200
    assert set(history.json()) == {"copy_sets", "poster_variants"}


def test_product_can_be_deleted_from_api(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)

    created = client.post(
        "/api/products",
        data={"name": "待删除商品"},
        files={"image": ("delete.png", _make_demo_image_bytes(), "image/png")},
    )
    assert created.status_code == 201
    product_id = created.json()["id"]
    product_root = configured_env / "products" / product_id
    assert product_root.exists()
    run = client.post(f"/api/products/{product_id}/workflow/run", json={})
    assert run.status_code == 200
    completed = _wait_for_workflow_run(client, product_id, status="succeeded")
    assert completed["runs"][0]["node_runs"]
    product_with_artifacts = client.get(f"/api/products/{product_id}")
    assert product_with_artifacts.status_code == 200
    assert product_with_artifacts.json()["copy_sets"]
    assert product_with_artifacts.json()["poster_variants"]

    _enable_deletion(client)
    deleted = client.delete(f"/api/products/{product_id}")
    assert deleted.status_code == 204
    assert deleted.content == b""

    listed = client.get("/api/products")
    assert listed.status_code == 200
    assert product_id not in {item["id"] for item in listed.json()["items"]}
    missing = client.get(f"/api/products/{product_id}")
    assert missing.status_code == 404
    assert not product_root.exists()

def test_reference_images_can_be_attached_to_product(db_session, configured_env: Path) -> None:
    product = create_product(
        db_session,
        name="陶瓷马克杯",
        category="家居",
        price="39.00",
        source_note=None,
        image_bytes=_make_demo_image_bytes(),
        filename="mug.png",
        content_type="image/png",
    )

    updated = add_reference_images(
        db_session,
        product_id=product.id,
        reference_image_uploads=[
            (_make_demo_image_bytes(), "sample-1.png", "image/png"),
            (_make_demo_image_bytes(), "sample-2.png", "image/png"),
        ],
    )

    reference_assets = [asset for asset in updated.source_assets if asset.kind == SourceAssetKind.REFERENCE_IMAGE]
    assert len(reference_assets) == 2
    assert all((Path(configured_env) / asset.storage_path).exists() for asset in reference_assets)

def test_product_reference_image_can_be_deleted(configured_env: Path, db_session) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)

    created = client.post(
        "/api/products",
        data={"name": "香薰蜡烛", "category": "家居", "price": "49.00"},
        files=[
            ("image", ("main.png", _make_demo_image_bytes(), "image/png")),
            ("reference_images", ("ref.png", _make_demo_image_bytes(), "image/png")),
        ],
    )
    assert created.status_code == 201
    payload = created.json()
    original_asset = next(asset for asset in payload["source_assets"] if asset["kind"] == "original_image")
    reference_asset = next(asset for asset in payload["source_assets"] if asset["kind"] == "reference_image")

    db_session.expire_all()
    persisted_reference = db_session.get(SourceAsset, reference_asset["id"])
    assert persisted_reference is not None
    reference_path = Path(configured_env) / persisted_reference.storage_path
    assert reference_path.exists()

    deleted = client.delete(f"/api/source-assets/{reference_asset['id']}")
    assert deleted.status_code == 200
    assert all(asset["id"] != reference_asset["id"] for asset in deleted.json()["source_assets"])

    db_session.expire_all()
    assert db_session.get(SourceAsset, reference_asset["id"]) is None
    assert not reference_path.exists()

    rejected = client.delete(f"/api/source-assets/{original_asset['id']}")
    assert rejected.status_code == 400
    assert "只能删除商品参考图" in rejected.json()["detail"]
