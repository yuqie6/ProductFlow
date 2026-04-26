from __future__ import annotations

from pathlib import Path

import pytest
from fastapi.testclient import TestClient
from helpers import (
    _execute_workflow_queue_inline,
    _login,
    _make_demo_image_bytes,
    _read_image_size,
)

from productflow_backend.infrastructure.db.models import (
    ImageSession,
    ImageSessionAsset,
)


@pytest.fixture(autouse=True)
def _execute_workflow_queue_inline_fixture(monkeypatch: pytest.MonkeyPatch) -> None:
    """Keep API workflow tests deterministic while production delivery goes through Dramatiq."""

    _execute_workflow_queue_inline(monkeypatch)


def test_image_session_rounds_support_same_conversation(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)

    _login(client)

    created = client.post("/api/image-sessions", json={"title": "护手霜连续生图"})
    assert created.status_code == 201
    session_id = created.json()["id"]

    first = client.post(
        f"/api/image-sessions/{session_id}/generate",
        json={
            "prompt": "做一张奶油质感的护手霜广告图，柔光，白底，产品居中",
            "size": "1024x1024",
        },
    )
    assert first.status_code == 200
    first_payload = first.json()
    assert len(first_payload["rounds"]) == 1
    assert first_payload["rounds"][0]["generated_asset"]["download_url"].startswith("/api/image-session-assets/")
    assert first_payload["rounds"][0]["generated_asset"]["preview_url"].endswith("variant=preview")
    assert first_payload["rounds"][0]["generated_asset"]["thumbnail_url"].endswith("variant=thumbnail")
    thumbnail = client.get(first_payload["rounds"][0]["generated_asset"]["thumbnail_url"])
    assert thumbnail.status_code == 200
    assert max(_read_image_size(thumbnail.content)) <= 320

    upload = client.post(
        f"/api/image-sessions/{session_id}/reference-images",
        files={"reference_images": ("sample.png", _make_demo_image_bytes(), "image/png")},
    )
    assert upload.status_code == 200
    upload_payload = upload.json()
    assert any(asset["kind"] == "reference_upload" for asset in upload_payload["assets"])

    second = client.post(
        f"/api/image-sessions/{session_id}/generate",
        json={
            "prompt": "保持同样产品和光线，把背景改成浴室台面，增加一点水珠",
            "size": "1024x1024",
        },
    )
    assert second.status_code == 200
    second_payload = second.json()
    assert len(second_payload["rounds"]) == 2
    assert second_payload["rounds"][-1]["provider_name"] == "mock"
    assert second_payload["rounds"][-1]["assistant_message"].startswith("已基于当前对话继续生成")


def test_image_session_generation_accepts_custom_size_and_rejects_invalid_dimensions(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)

    _login(client)

    created = client.post("/api/image-sessions", json={"title": "自定义尺寸"})
    assert created.status_code == 201
    session_id = created.json()["id"]

    generated = client.post(
        f"/api/image-sessions/{session_id}/generate",
        json={"prompt": "做一张 16:9 展示图", "size": "1280x720"},
    )
    assert generated.status_code == 200
    assert generated.json()["rounds"][-1]["size"] == "1280x720"

    zero = client.post(
        f"/api/image-sessions/{session_id}/generate",
        json={"prompt": "尺寸非法", "size": "0x720"},
    )
    assert zero.status_code == 422
    assert "宽高必须大于 0" in zero.text

    oversized = client.post(
        f"/api/image-sessions/{session_id}/generate",
        json={"prompt": "尺寸过大", "size": "5000x5000"},
    )
    assert oversized.status_code == 200
    assert oversized.json()["rounds"][-1]["size"] == "3840x3840"


def test_image_session_reference_image_can_be_deleted(configured_env: Path, db_session) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)

    created = client.post("/api/image-sessions", json={"title": "参考图删除"})
    assert created.status_code == 201
    session_id = created.json()["id"]
    upload = client.post(
        f"/api/image-sessions/{session_id}/reference-images",
        files={"reference_images": ("sample.png", _make_demo_image_bytes(), "image/png")},
    )
    assert upload.status_code == 200
    reference_asset = next(asset for asset in upload.json()["assets"] if asset["kind"] == "reference_upload")

    db_session.expire_all()
    persisted_asset = db_session.get(ImageSessionAsset, reference_asset["id"])
    assert persisted_asset is not None
    reference_path = Path(configured_env) / persisted_asset.storage_path
    assert reference_path.exists()

    deleted = client.delete(f"/api/image-sessions/{session_id}/reference-images/{reference_asset['id']}")
    assert deleted.status_code == 200
    assert all(asset["id"] != reference_asset["id"] for asset in deleted.json()["assets"])

    db_session.expire_all()
    assert db_session.get(ImageSessionAsset, reference_asset["id"]) is None
    assert not reference_path.exists()

def test_image_session_can_be_deleted_with_files(configured_env: Path, db_session) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)

    created = client.post("/api/image-sessions", json={"title": "整会话删除"})
    assert created.status_code == 201
    session_id = created.json()["id"]
    upload = client.post(
        f"/api/image-sessions/{session_id}/reference-images",
        files={"reference_images": ("sample.png", _make_demo_image_bytes(), "image/png")},
    )
    assert upload.status_code == 200
    generated = client.post(
        f"/api/image-sessions/{session_id}/generate",
        json={"prompt": "做一张白底商品图", "size": "1024x1024"},
    )
    assert generated.status_code == 200

    db_session.expire_all()
    asset_paths = [
        Path(configured_env) / asset.storage_path
        for asset in db_session.query(ImageSessionAsset).filter(ImageSessionAsset.session_id == session_id).all()
    ]
    assert asset_paths
    assert all(path.exists() for path in asset_paths)
    session_root = Path(configured_env) / "image_sessions" / session_id
    assert session_root.exists()

    deleted = client.delete(f"/api/image-sessions/{session_id}")
    assert deleted.status_code == 204

    listed = client.get("/api/image-sessions")
    assert listed.status_code == 200
    assert all(item["id"] != session_id for item in listed.json()["items"])

    db_session.expire_all()
    assert db_session.get(ImageSession, session_id) is None
    assert all(not path.exists() for path in asset_paths)
    assert not session_root.exists()

def test_image_session_result_can_write_back_to_product(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)

    create_product_response = client.post(
        "/api/products",
        data={"name": "护手霜", "category": "个护", "price": "59.00"},
        files={"image": ("cream.png", _make_demo_image_bytes(), "image/png")},
    )
    assert create_product_response.status_code == 201
    product_id = create_product_response.json()["id"]

    created = client.post("/api/image-sessions", json={"product_id": product_id})
    assert created.status_code == 201
    session_id = created.json()["id"]

    generated = client.post(
        f"/api/image-sessions/{session_id}/generate",
        json={"prompt": "做一张高级浴室台面护手霜广告图", "size": "1024x1024"},
    )
    assert generated.status_code == 200
    generated_payload = generated.json()
    generated_asset_id = generated_payload["rounds"][-1]["generated_asset"]["id"]

    attach_reference = client.post(
        f"/api/image-sessions/{session_id}/assets/{generated_asset_id}/attach-to-product",
        json={"target": "reference"},
    )
    assert attach_reference.status_code == 200
    assert attach_reference.json()["message"] == "已加入商品参考图"

    product_after_reference = client.get(f"/api/products/{product_id}")
    assert product_after_reference.status_code == 200
    reference_assets = [
        asset for asset in product_after_reference.json()["source_assets"] if asset["kind"] == "reference_image"
    ]
    assert len(reference_assets) >= 1

    attach_main = client.post(
        f"/api/image-sessions/{session_id}/assets/{generated_asset_id}/attach-to-product",
        json={"target": "main_source"},
    )
    assert attach_main.status_code == 200
    assert attach_main.json()["message"] == "已设为商品主图"

    product_after_main = client.get(f"/api/products/{product_id}")
    assert product_after_main.status_code == 200
    original_assets = [
        asset for asset in product_after_main.json()["source_assets"] if asset["kind"] == "original_image"
    ]
    all_reference_assets = [
        asset for asset in product_after_main.json()["source_assets"] if asset["kind"] == "reference_image"
    ]
    assert len(original_assets) == 1
    assert len(all_reference_assets) >= 2
