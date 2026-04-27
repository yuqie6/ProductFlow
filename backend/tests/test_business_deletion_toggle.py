from __future__ import annotations

from pathlib import Path

from fastapi.testclient import TestClient
from helpers import (
    _login,
    _make_demo_image_bytes,
)

from productflow_backend.infrastructure.db.models import (
    ImageSession,
    ImageSessionAsset,
    Product,
    SourceAsset,
)

DELETION_DISABLED_DETAIL = "删除功能已关闭，请联系管理员"


def test_product_and_image_session_delete_are_disabled_by_default_and_preserve_records_and_files(
    configured_env: Path,
    db_session,
) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)

    created_product = client.post(
        "/api/products",
        data={"name": "默认禁止删除商品"},
        files=[
            ("image", ("main.png", _make_demo_image_bytes(), "image/png")),
            ("reference_images", ("ref.png", _make_demo_image_bytes(), "image/png")),
        ],
    )
    assert created_product.status_code == 201
    product_payload = created_product.json()
    product_id = product_payload["id"]
    product_root = configured_env / "products" / product_id
    source_asset = next(asset for asset in product_payload["source_assets"] if asset["kind"] == "reference_image")

    created_session = client.post("/api/image-sessions", json={"title": "默认禁止删除会话"})
    assert created_session.status_code == 201
    image_session_id = created_session.json()["id"]
    uploaded_reference = client.post(
        f"/api/image-sessions/{image_session_id}/reference-images",
        files={"reference_images": ("session-ref.png", _make_demo_image_bytes(), "image/png")},
    )
    assert uploaded_reference.status_code == 200
    session_reference_asset = next(
        asset for asset in uploaded_reference.json()["assets"] if asset["kind"] == "reference_upload"
    )
    db_session.expire_all()
    source_row = db_session.get(SourceAsset, source_asset["id"])
    session_reference_row = db_session.get(ImageSessionAsset, session_reference_asset["id"])
    assert source_row is not None
    assert session_reference_row is not None
    source_path = configured_env / source_row.storage_path
    session_reference_path = configured_env / session_reference_row.storage_path
    assert product_root.exists()
    assert source_path.exists()
    assert session_reference_path.exists()

    delete_product = client.delete(f"/api/products/{product_id}")
    assert delete_product.status_code == 403
    assert delete_product.json()["detail"] == DELETION_DISABLED_DETAIL

    delete_image_session = client.delete(f"/api/image-sessions/{image_session_id}")
    assert delete_image_session.status_code == 403
    assert delete_image_session.json()["detail"] == DELETION_DISABLED_DETAIL

    db_session.expire_all()
    assert db_session.get(Product, product_id) is not None
    assert db_session.get(SourceAsset, source_asset["id"]) is not None
    assert db_session.get(ImageSession, image_session_id) is not None
    assert db_session.get(ImageSessionAsset, session_reference_asset["id"]) is not None
    assert product_root.exists()
    assert source_path.exists()
    assert session_reference_path.exists()


def test_logout_delete_endpoint_is_not_blocked_by_business_deletion_toggle(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)

    logout = client.delete("/api/auth/session")
    assert logout.status_code == 200
    assert logout.json() == {"ok": True}
