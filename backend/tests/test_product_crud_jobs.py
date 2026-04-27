from __future__ import annotations

from pathlib import Path

import httpx
import pytest
from fastapi.testclient import TestClient
from helpers import (
    _execute_workflow_queue_inline,
    _login,
    _make_demo_image_bytes,
    _wait_for_workflow_run,
)

from productflow_backend.application.use_cases import (
    _is_retryable_exception,
    add_reference_images,
    confirm_copy_set,
    create_copy_job,
    create_poster_job,
    create_product,
    execute_copy_job,
    execute_poster_job,
    get_product_detail,
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


def test_end_to_end_copy_and_poster_workflow(db_session, configured_env: Path) -> None:
    product = create_product(
        db_session,
        name="防滑菜板",
        category="家居百货",
        price="29.90",
        source_note=None,
        image_bytes=_make_demo_image_bytes(),
        filename="product.png",
        content_type="image/png",
    )

    copy_job = create_copy_job(db_session, product_id=product.id).job
    execute_copy_job(copy_job.id)

    db_session.expire_all()
    product_after_copy = get_product_detail(db_session, product.id)
    assert len(product_after_copy.copy_sets) == 1
    copy_set = product_after_copy.copy_sets[0]
    assert "防滑菜板" in copy_set.title

    confirm_copy_set(db_session, copy_set_id=copy_set.id)
    poster_job = create_poster_job(db_session, product_id=product.id).job
    execute_poster_job(poster_job.id)

    db_session.expire_all()
    product_after_poster = get_product_detail(db_session, product.id)
    assert product_after_poster.confirmed_copy_set is not None
    assert len(product_after_poster.poster_variants) == 2

    poster_paths = [Path(configured_env) / poster.storage_path for poster in product_after_poster.poster_variants]
    assert all(path.exists() for path in poster_paths)


def test_sanitized_provider_runtime_errors_preserve_retryable_cause() -> None:
    try:
        raise RuntimeError("图片供应商请求失败，请检查供应商配置后重试") from httpx.TimeoutException("provider timeout")
    except RuntimeError as exc:
        assert _is_retryable_exception(exc) is True

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

def test_duplicate_active_copy_job_reuses_existing_job(db_session, configured_env: Path) -> None:
    product = create_product(
        db_session,
        name="收纳盒",
        category="家居",
        price="19.90",
        source_note=None,
        image_bytes=_make_demo_image_bytes(),
        filename="box.png",
        content_type="image/png",
    )

    first_result = create_copy_job(db_session, product_id=product.id)
    second_result = create_copy_job(db_session, product_id=product.id)

    assert first_result.job.id == second_result.job.id
    assert first_result.created is True
    assert second_result.created is False
