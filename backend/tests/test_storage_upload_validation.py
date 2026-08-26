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
    """生产环境经 Dramatiq 投递；测试里内联执行，保证 API workflow 结果确定。"""

    _execute_workflow_queue_inline(monkeypatch)


def test_product_asset_variant_urls_serve_preview_and_thumbnail(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)

    create_product_response = client.post(
        "/api/v2/products",
        data={"name": "大尺寸主图样例", "category": "个护", "price": "99.00"},
        files=[
            (
                "images",
                ("large.png", _make_demo_image_bytes_with_size(2400, 1800), "image/png"),
            )
        ],
    )
    assert create_product_response.status_code == 201
    image_asset = create_product_response.json()["created_assets"][0]

    assert image_asset["download_url"].startswith("/api/v2/product-image-assets/")
    assert image_asset["preview_url"].endswith("variant=preview")
    assert image_asset["thumbnail_url"].endswith("variant=thumbnail")

    preview = client.get(image_asset["preview_url"])
    assert preview.status_code == 200
    assert preview.headers["content-type"].startswith("image/")
    assert max(_read_image_size(preview.content)) <= 1600

    thumbnail = client.get(image_asset["thumbnail_url"])
    assert thumbnail.status_code == 200
    assert thumbnail.headers["content-type"].startswith("image/")
    assert max(_read_image_size(thumbnail.content)) <= 320

def test_product_create_rejects_invalid_price_and_invalid_image(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    app = create_app()
    client = TestClient(app)
    _login(client)

    invalid_price = client.post(
        "/api/v2/products",
        data={"name": "护手霜", "category": "个护", "price": "abc"},
        files=[("images", ("cream.png", _make_demo_image_bytes(), "image/png"))],
    )
    assert invalid_price.status_code == 400

    invalid_image = client.post(
        "/api/v2/products",
        data={"name": "护手霜", "category": "个护", "price": "59.00"},
        files=[("images", ("cream.png", b"not an image", "image/png"))],
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
    assert generated.status_code == 202
    assert generated.json()["generation_tasks"][-1]["size"] == "3840x3840"


def test_media_library_upload_enforces_batch_limits_and_readable_errors(
    configured_env: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    from productflow_backend.config import get_settings
    from productflow_backend.presentation.api import create_app

    settings = get_settings()
    monkeypatch.setattr(settings, "upload_max_image_bytes", 500)
    monkeypatch.setattr(settings, "upload_max_batch_bytes", 1500)
    monkeypatch.setattr(settings, "upload_max_batch_files", 2)

    app = create_app()
    client = TestClient(app)
    _login(client)

    # 1. 超过单批次文件数
    res_batch_files = client.post(
        "/api/media-library/upload",
        files=[
            ("files", ("img1.png", _make_demo_image_bytes(), "image/png")),
            ("files", ("img2.png", _make_demo_image_bytes(), "image/png")),
            ("files", ("img3.png", _make_demo_image_bytes(), "image/png")),
        ],
    )
    assert res_batch_files.status_code == 400
    assert "单批次最多上传 2 张图片" in res_batch_files.json()["detail"]

    # 2. 单张大小超限
    large_img = _make_demo_image_bytes_with_size(100, 100)
    monkeypatch.setattr(settings, "upload_max_image_bytes", 50)
    res_single_large = client.post(
        "/api/media-library/upload",
        files=[("files", ("too_large.png", large_img, "image/png"))],
    )
    assert res_single_large.status_code == 413
    assert "too_large.png" in res_single_large.json()["detail"]
    assert "超过单张大小限制" in res_single_large.json()["detail"]



def _jpeg_demo_image_bytes() -> bytes:
    from io import BytesIO

    from PIL import Image as PILImage

    buffer = BytesIO()
    PILImage.new("RGB", (10, 10), (200, 30, 40)).save(buffer, format="JPEG")
    return buffer.getvalue()


def test_media_library_upload_normalizes_mime_aliases_and_octet_stream(configured_env: Path) -> None:
    """MIME 别名（image/jpg、image/pjpeg、image/x-png）以及 octet-stream 嗅探。"""
    from productflow_backend.presentation.api import create_app

    client = TestClient(create_app())
    _login(client)

    jpeg_bytes = _jpeg_demo_image_bytes()
    png_bytes = _make_demo_image_bytes()

    response = client.post(
        "/api/media-library/upload",
        files=[
            ("files", ("alias-jpg.jpg", jpeg_bytes, "image/jpg")),
            ("files", ("alias-pjpeg.jpg", jpeg_bytes, "image/pjpeg")),
            ("files", ("alias-xpng.png", png_bytes, "image/x-png")),
            ("files", ("sniff-octet.bin", png_bytes, "application/octet-stream")),
        ],
    )
    assert response.status_code == 201, response.text
    payload = response.json()
    assert [item["mime_type"] for item in payload] == ["image/jpeg", "image/jpeg", "image/png", "image/png"]
    assert [item["original_filename"] for item in payload] == [
        "alias-jpg.jpg",
        "alias-pjpeg.jpg",
        "alias-xpng.png",
        "sniff-octet.bin",
    ]


def test_media_library_upload_bounds_long_filenames(configured_env: Path) -> None:
    """文件名超过 255 字符时必须一致截断，且不能崩溃或与 provenance 对不上。"""
    from productflow_backend.presentation.api import create_app

    client = TestClient(create_app())
    _login(client)

    long_name = "a" * 300 + ".png"
    image_bytes = _make_demo_image_bytes()
    response = client.post("/api/media-library/upload", files=[("files", (long_name, image_bytes, "image/png"))])
    assert response.status_code == 201, response.text
    asset = response.json()[0]
    assert asset["original_filename"] == "a" * 255
    assert asset["display_name"] == "a" * 255
    assert len(asset["original_filename"]) == 255

    # provenance 必须可解析，并与落库列一致
    detail = client.get(f"/api/media-library/{asset['id']}")
    assert detail.status_code == 200


def test_media_library_upload_enforces_aggregate_batch_bytes(
    configured_env: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """即使每个文件都低于单文件上限，批量总字节上限仍会触发。"""
    from productflow_backend.config import get_settings
    from productflow_backend.presentation.api import create_app

    settings = get_settings()
    image_bytes = _make_demo_image_bytes_with_size(100, 100)
    monkeypatch.setattr(settings, "upload_max_image_bytes", len(image_bytes) + 100)
    monkeypatch.setattr(settings, "upload_max_batch_bytes", len(image_bytes) + 10)
    monkeypatch.setattr(settings, "upload_max_batch_files", 5)

    client = TestClient(create_app())
    _login(client)
    response = client.post(
        "/api/media-library/upload",
        files=[
            ("files", ("one.png", image_bytes, "image/png")),
            ("files", ("two.png", image_bytes, "image/png")),
        ],
    )
    assert response.status_code == 413, response.text
    assert "批量上传总大小超过限制" in response.json()["detail"]


def test_media_library_upload_respects_folder_id(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    client = TestClient(create_app())
    _login(client)

    folder = client.post("/api/media-library/folders", json={"name": "上传目录"}).json()
    image_bytes = _make_demo_image_bytes()
    ok = client.post(
        "/api/media-library/upload",
        data={"folder_id": folder["id"]},
        files=[("files", ("in-folder.png", image_bytes, "image/png"))],
    )
    assert ok.status_code == 201, ok.text
    assert ok.json()[0]["folder_id"] == folder["id"]
    assert ok.json()[0]["folder_name"] == "上传目录"

    missing = client.post(
        "/api/media-library/upload",
        data={"folder_id": "00000000-0000-0000-0000-000000000000"},
        files=[("files", ("no-folder.png", image_bytes, "image/png"))],
    )
    assert missing.status_code == 404


def test_media_library_batch_upload_rolls_back_all_and_compensates_storage(
    db_session,
    configured_env: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """批次中途保存失败时，不得留下任何资产，并删掉本次创建的全部文件。"""
    import sqlalchemy as sa

    from productflow_backend.application.media_library import service as media_service
    from productflow_backend.infrastructure.db.models import MediaLibraryAsset
    from productflow_backend.infrastructure.storage import LocalStorage

    real_stage = media_service.stage_verified_media_object
    calls = {"count": 0}

    def flaky_stage(session, *, content, filename, expected_mime_type, storage, storage_writes):
        calls["count"] += 1
        if calls["count"] == 2:
            raise RuntimeError("simulated storage write failure")
        return real_stage(
            session,
            content=content,
            filename=filename,
            expected_mime_type=expected_mime_type,
            storage=storage,
            storage_writes=storage_writes,
        )

    monkeypatch.setattr(media_service, "stage_verified_media_object", flaky_stage)

    storage = LocalStorage(root=configured_env)
    image_bytes = _make_demo_image_bytes()
    before = {path for path in configured_env.rglob("*") if path.is_file()}

    with pytest.raises(RuntimeError, match="simulated storage write failure"):
        media_service.save_media_library_assets_from_upload(
            db_session,
            items=[
                (image_bytes, "first.png", "image/png"),
                (image_bytes, "second.png", "image/png"),
            ],
            storage=storage,
        )

    db_session.expire_all()
    count = db_session.execute(
        sa.select(sa.func.count())
        .select_from(MediaLibraryAsset)
        .where(MediaLibraryAsset.source_type == "direct_upload")
    ).scalar_one()
    assert count == 0
    after = {path for path in configured_env.rglob("*") if path.is_file()}
    assert before == after


def test_product_create_accepts_octet_stream_content_sniffed(configured_env: Path) -> None:
    """声明为 application/octet-stream 时按内容嗅探，商品路径应接受。"""
    from productflow_backend.presentation.api import create_app

    client = TestClient(create_app())
    _login(client)

    response = client.post(
        "/api/v2/products",
        data={"name": "八比特流上报商品", "category": "个护", "price": "39.00"},
        files=[("images", ("octet.png", _make_demo_image_bytes(), "application/octet-stream"))],
    )
    assert response.status_code == 201, response.text
    asset = response.json()["created_assets"][0]
    assert asset["mime_type"] == "image/png"


def test_image_session_reference_upload_accepts_octet_stream(configured_env: Path) -> None:
    """声明为 application/octet-stream 时按内容嗅探，image-session 路径应接受。"""
    from productflow_backend.presentation.api import create_app

    client = TestClient(create_app())
    _login(client)

    created = client.post("/api/image-sessions", json={"title": "八比特流图片会话"})
    assert created.status_code == 201, created.text
    session_id = created.json()["id"]

    uploaded = client.post(
        f"/api/image-sessions/{session_id}/reference-images",
        files=[("reference_images", ("octet-ref.bin", _make_demo_image_bytes(), "application/octet-stream"))],
    )
    assert uploaded.status_code == 200, uploaded.text
    assets = uploaded.json()["assets"]
    assert any(
        asset["mime_type"] == "image/png" and asset["original_filename"] == "octet-ref.bin" for asset in assets
    )
