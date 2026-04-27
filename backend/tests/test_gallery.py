from __future__ import annotations

from pathlib import Path

import pytest
from fastapi.testclient import TestClient
from helpers import _login, _make_demo_image_bytes

from productflow_backend.application import gallery as gallery_app
from productflow_backend.domain.enums import ImageSessionAssetKind
from productflow_backend.infrastructure.db.models import (
    ImageGalleryEntry,
    ImageSession,
    ImageSessionAsset,
    ImageSessionRound,
)
from productflow_backend.infrastructure.db.session import get_session_factory
from productflow_backend.main import app


def test_generated_image_can_be_saved_to_gallery_idempotently(configured_env: Path, db_session) -> None:
    client = TestClient(app)
    _login(client)
    product = client.post(
        "/api/products",
        data={"name": "画廊商品"},
        files={"image": ("source.png", _make_demo_image_bytes(), "image/png")},
    )
    assert product.status_code == 201
    product_id = product.json()["id"]

    created_session = client.post("/api/image-sessions", json={"product_id": product_id, "title": "画廊会话"})
    assert created_session.status_code == 201
    session_id = created_session.json()["id"]

    generated = client.post(
        f"/api/image-sessions/{session_id}/generate",
        json={"prompt": "一张用于画廊的图", "size": "1024x1024", "generation_count": 2},
    )
    assert generated.status_code == 202
    first_round = generated.json()["rounds"][0]
    asset_id = first_round["generated_asset"]["id"]

    saved = client.post("/api/gallery", json={"image_session_asset_id": asset_id})
    assert saved.status_code == 201
    payload = saved.json()
    assert payload["image_session_asset_id"] == asset_id
    assert payload["image_session_round_id"] == first_round["id"]
    assert payload["image_session_id"] == session_id
    assert payload["image_session_title"] == "画廊会话"
    assert payload["product_id"] == product_id
    assert payload["product_name"] == "画廊商品"
    assert payload["prompt"] == "一张用于画廊的图"
    assert payload["size"] == "1024x1024"
    assert payload["actual_size"] == "1024x1024"
    assert payload["provider_name"] == "mock"
    assert payload["candidate_index"] == 1
    assert payload["candidate_count"] == 2
    assert payload["image"]["thumbnail_url"].endswith("variant=thumbnail")

    saved_again = client.post("/api/gallery", json={"image_session_asset_id": asset_id})
    assert saved_again.status_code == 200
    assert saved_again.json()["id"] == payload["id"]
    assert db_session.query(ImageGalleryEntry).count() == 1

    listed = client.get("/api/gallery")
    assert listed.status_code == 200
    items = listed.json()["items"]
    assert len(items) == 1
    assert items[0]["id"] == payload["id"]
    assert items[0]["image"]["download_url"].startswith("/api/image-session-assets/")


def test_gallery_rejects_non_generated_session_assets(configured_env: Path) -> None:
    client = TestClient(app)
    _login(client)
    created_session = client.post("/api/image-sessions", json={"title": "参考图会话"})
    assert created_session.status_code == 201
    session_id = created_session.json()["id"]
    uploaded = client.post(
        f"/api/image-sessions/{session_id}/reference-images",
        files=[("reference_images", ("reference.png", _make_demo_image_bytes(), "image/png"))],
    )
    assert uploaded.status_code == 200
    reference_asset_id = uploaded.json()["assets"][0]["id"]

    saved = client.post("/api/gallery", json={"image_session_asset_id": reference_asset_id})
    assert saved.status_code == 400
    assert saved.json()["detail"] == "只有生成结果可以保存到画廊"


def test_gallery_rejects_generated_asset_without_round(configured_env: Path, db_session) -> None:
    session = ImageSession(title="孤立生成图")
    db_session.add(session)
    db_session.flush()
    asset = ImageSessionAsset(
        session_id=session.id,
        kind=ImageSessionAssetKind.GENERATED_IMAGE,
        original_filename="orphan.png",
        mime_type="image/png",
        storage_path="image-sessions/orphan.png",
    )
    db_session.add(asset)
    db_session.commit()

    client = TestClient(app)
    _login(client)

    saved = client.post("/api/gallery", json={"image_session_asset_id": asset.id})
    assert saved.status_code == 404
    assert saved.json()["detail"] == "生成记录不存在"


def test_gallery_save_handles_integrity_race(
    configured_env: Path,
    db_session,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    session = ImageSession(title="并发保存会话")
    db_session.add(session)
    db_session.flush()
    asset = ImageSessionAsset(
        session_id=session.id,
        kind=ImageSessionAssetKind.GENERATED_IMAGE,
        original_filename="race.png",
        mime_type="image/png",
        storage_path="image-sessions/race.png",
    )
    db_session.add(asset)
    db_session.flush()

    round_item = ImageSessionRound(
        session_id=session.id,
        prompt="并发保存",
        assistant_message="ok",
        size="1024x1024",
        model_name="mock",
        provider_name="mock",
        prompt_version="v1",
        generated_asset_id=asset.id,
    )
    db_session.add(round_item)
    db_session.commit()
    existing = ImageGalleryEntry(
        image_session_asset_id=asset.id,
        image_session_round_id=round_item.id,
    )
    db_session.add(existing)
    db_session.commit()
    existing_id = existing.id

    real_get_gallery_entry = gallery_app._get_gallery_entry_by_asset_id
    calls = {"count": 0}

    def stale_initial_gallery_lookup(session, image_session_asset_id: str):
        calls["count"] += 1
        if calls["count"] == 1:
            return None
        return real_get_gallery_entry(session, image_session_asset_id)

    monkeypatch.setattr(gallery_app, "_get_gallery_entry_by_asset_id", stale_initial_gallery_lookup)

    factory = get_session_factory()
    race_session = factory()
    try:
        result = gallery_app.save_generated_asset_to_gallery(race_session, image_session_asset_id=asset.id)

        assert result.created is False
        assert result.entry.id == existing_id
        assert calls["count"] == 2
    finally:
        race_session.close()

    assert db_session.query(ImageGalleryEntry).count() == 1
