from __future__ import annotations

from io import BytesIO
from pathlib import Path

import pytest
from fastapi.testclient import TestClient
from helpers import _enable_deletion, _login, _make_demo_image_bytes
from PIL import Image
from sqlalchemy.exc import IntegrityError

from productflow_backend.application.image_sessions.service import (
    attach_image_session_asset_to_product_canonical,
    create_image_session,
    delete_image_session,
)
from productflow_backend.application.media_objects import (
    inspect_image_bytes,
)
from productflow_backend.application.product_images.assets import (
    clear_product_cover,
    create_product_image_asset,
    delete_product_image_asset,
    set_product_cover,
    set_product_cover_if_empty,
)
from productflow_backend.application.products import create_canonical_product, delete_product
from productflow_backend.domain.enums import (
    ImageSessionAssetKind,
    MediaVerificationStatus,
    ProductImageOriginType,
)
from productflow_backend.domain.errors import BusinessValidationError, ConflictError, NotFoundError
from productflow_backend.infrastructure.db.models import (
    ImageSessionAsset,
    MediaObject,
    Product,
    ProductImageAsset,
    new_id,
)
from productflow_backend.infrastructure.storage import LocalStorage


def _make_jpeg_bytes() -> bytes:
    image = Image.new("RGB", (640, 480), (20, 40, 60))
    output = BytesIO()
    image.save(output, format="JPEG")
    return output.getvalue()


def test_inspect_image_bytes_uses_real_format_and_hash() -> None:
    content = _make_jpeg_bytes()

    metadata = inspect_image_bytes(content, expected_mime_type="image/jpeg")

    assert metadata.mime_type == "image/jpeg"
    assert metadata.byte_size == len(content)
    assert (metadata.width, metadata.height) == (640, 480)
    assert len(metadata.sha256) == 64

    with pytest.raises(BusinessValidationError, match="声明媒体类型"):
        inspect_image_bytes(content, expected_mime_type="image/png")
    with pytest.raises(BusinessValidationError, match="可解码"):
        inspect_image_bytes(b"not-an-image")


def test_local_storage_saves_media_by_identity_and_real_extension(configured_env: Path) -> None:
    storage = LocalStorage(configured_env)
    media_id = new_id()

    relative_path = storage.save_media_image(media_id, "misleading.png", _make_jpeg_bytes())

    assert relative_path == f"media/{media_id[:2]}/{media_id}.jpg"
    assert storage.resolve(relative_path).exists()
    assert storage.resolve_for_variant(relative_path, "preview")[0].exists()
    assert storage.resolve_for_variant(relative_path, "thumbnail")[0].exists()


def test_verified_media_requires_measured_metadata(db_session) -> None:
    db_session.add(
        MediaObject(
            storage_path="media/invalid.png",
            mime_type="image/png",
            verification_status=MediaVerificationStatus.VERIFIED,
        )
    )

    with pytest.raises(IntegrityError):
        db_session.commit()
    db_session.rollback()


def test_create_canonical_product_writes_only_canonical_assets(configured_env: Path, db_session) -> None:
    product = create_canonical_product(
        db_session,
        name="规范化商品",
        category="工具",
        price="199.00",
        source_note="测试",
        image_uploads=[
            (_make_demo_image_bytes(), "front.png", "image/png"),
            (_make_jpeg_bytes(), "detail.jpg", "image/jpeg"),
        ],
    )

    assert db_session.query(ProductImageAsset).filter_by(product_id=product.id).count() == 2
    assert db_session.query(MediaObject).count() == 2
    assert product.cover_image_asset_id == product.image_assets[0].id
    assert all(asset.origin_type == ProductImageOriginType.UPLOAD for asset in product.image_assets)
    assert all(
        asset.media_object.verification_status == MediaVerificationStatus.VERIFIED
        for asset in product.image_assets
    )
    assert all(asset.media_object.storage_path.startswith("media/") for asset in product.image_assets)
    assert all(asset.media_object.byte_size and asset.media_object.byte_size > 0 for asset in product.image_assets)
    assert all(asset.media_object.width and asset.media_object.width > 0 for asset in product.image_assets)
    assert all(asset.media_object.height and asset.media_object.height > 0 for asset in product.image_assets)
    assert all(asset.media_object.sha256 and len(asset.media_object.sha256) == 64 for asset in product.image_assets)

    storage = LocalStorage(configured_env)
    assert all(storage.resolve(asset.media_object.storage_path).exists() for asset in product.image_assets)


def test_canonical_product_rolls_back_files_and_database_on_invalid_later_image(
    configured_env: Path,
    db_session,
) -> None:
    with pytest.raises(BusinessValidationError, match="声明媒体类型"):
        create_canonical_product(
            db_session,
            name="回滚商品",
            category=None,
            price=None,
            source_note=None,
            image_uploads=[
                (_make_demo_image_bytes(), "front.png", "image/png"),
                (_make_jpeg_bytes(), "wrong.png", "image/png"),
            ],
        )

    assert db_session.query(Product).count() == 0
    assert db_session.query(ProductImageAsset).count() == 0
    assert db_session.query(MediaObject).count() == 0
    assert not list(configured_env.glob("media/**/*.*"))


def test_generated_asset_has_one_media_object_and_three_files(configured_env: Path, db_session) -> None:
    product = Product(name="工作流商品")
    db_session.add(product)
    db_session.commit()

    asset = create_product_image_asset(
        db_session,
        product_id=product.id,
        content=_make_demo_image_bytes(),
        filename="generated.png",
        expected_mime_type="image/png",
        display_name="首屏海报候选",
        origin_type=ProductImageOriginType.WORKFLOW_GENERATION,
    )

    assert asset.origin_type == ProductImageOriginType.WORKFLOW_GENERATION
    assert db_session.query(MediaObject).count() == 1
    assert db_session.query(ProductImageAsset).count() == 1
    media_files = [path for path in configured_env.glob("media/**/*") if path.is_file()]
    assert len(media_files) == 3  # 原图 + preview + thumbnail


def test_cover_blocks_asset_delete_until_cleared(configured_env: Path, db_session) -> None:
    product = create_canonical_product(
        db_session,
        name="封面商品",
        category=None,
        price=None,
        source_note=None,
        image_uploads=[(_make_demo_image_bytes(), "cover.png", "image/png")],
    )
    asset = product.image_assets[0]
    storage_path = asset.media_object.storage_path

    set_product_cover(db_session, product_id=product.id, asset_id=asset.id)
    with pytest.raises(ConflictError, match="封面"):
        delete_product_image_asset(db_session, asset_id=asset.id)

    clear_product_cover(db_session, product_id=product.id)
    deleted_product_id = delete_product_image_asset(db_session, asset_id=asset.id)

    assert deleted_product_id == product.id
    assert db_session.get(ProductImageAsset, asset.id) is None
    assert db_session.query(MediaObject).count() == 0
    assert not LocalStorage(configured_env).resolve(storage_path).exists()


def test_cover_if_empty_is_deterministic_and_parent_reference_blocks_delete(configured_env: Path, db_session) -> None:
    product = create_canonical_product(
        db_session,
        name="引用规则商品",
        category=None,
        price=None,
        source_note=None,
        image_uploads=[
            (_make_demo_image_bytes(), "parent.png", "image/png"),
            (_make_jpeg_bytes(), "child.jpg", "image/jpeg"),
        ],
    )
    parent, child = product.image_assets
    clear_product_cover(db_session, product_id=product.id)

    assert set_product_cover_if_empty(db_session, product_id=product.id, asset_id=parent.id) is True
    assert set_product_cover_if_empty(db_session, product_id=product.id, asset_id=child.id) is False
    db_session.refresh(product)
    assert product.cover_image_asset_id == parent.id

    clear_product_cover(db_session, product_id=product.id)
    child.parent_asset_id = parent.id
    db_session.commit()
    with pytest.raises(ConflictError, match="派生图片"):
        delete_product_image_asset(db_session, asset_id=parent.id)


def test_cover_if_empty_reports_a_missing_product_before_asset_membership(configured_env: Path, db_session) -> None:
    product = create_canonical_product(
        db_session,
        name="封面来源商品",
        category=None,
        price=None,
        source_note=None,
        image_uploads=[(_make_demo_image_bytes(), "cover.png", "image/png")],
    )

    with pytest.raises(NotFoundError, match="商品不存在"):
        set_product_cover_if_empty(db_session, product_id="missing-product", asset_id=product.image_assets[0].id)


def test_image_session_attach_reuses_media_and_session_delete_preserves_product_asset(
    configured_env: Path,
    db_session,
) -> None:
    storage = LocalStorage(configured_env)
    image_session = create_image_session(db_session, title="共享媒体")
    content = _make_demo_image_bytes()
    metadata = inspect_image_bytes(content, expected_mime_type="image/png")
    media = MediaObject(
        storage_path="pending",
        mime_type=metadata.mime_type,
        byte_size=metadata.byte_size,
        width=metadata.width,
        height=metadata.height,
        sha256=metadata.sha256,
        verification_status=MediaVerificationStatus.VERIFIED,
        verified_at=image_session.created_at,
    )
    db_session.add(media)
    db_session.flush()
    media.storage_path = storage.save_media_image(media.id, "generated.png", content)
    session_asset = ImageSessionAsset(
        session_id=image_session.id,
        kind=ImageSessionAssetKind.GENERATED_IMAGE,
        original_filename="generated.png",
        mime_type=metadata.mime_type,
        storage_path=media.storage_path,
        media_object_id=media.id,
    )
    db_session.add(session_asset)
    db_session.commit()
    product = create_canonical_product(
        db_session,
        name="共享媒体商品",
        category=None,
        price=None,
        source_note=None,
        image_uploads=[(_make_demo_image_bytes(), "reference.png", "image/png")],
    )

    attached = attach_image_session_asset_to_product_canonical(
        db_session,
        image_session_id=image_session.id,
        asset_id=session_asset.id,
        product_id=product.id,
    )
    attached_again = attach_image_session_asset_to_product_canonical(
        db_session,
        image_session_id=image_session.id,
        asset_id=session_asset.id,
        product_id=product.id,
    )

    assert attached_again.id == attached.id
    assert attached.media_object_id == session_asset.media_object_id
    assert attached.origin_type == ProductImageOriginType.IMAGE_SESSION_ATTACH
    assert db_session.query(ProductImageAsset).filter_by(source_image_session_asset_id=session_asset.id).count() == 1
    shared_path = attached.media_object.storage_path

    delete_image_session(db_session, image_session_id=image_session.id, storage=storage)

    db_session.expire_all()
    preserved_asset = db_session.get(ProductImageAsset, attached.id)
    assert preserved_asset is not None
    assert preserved_asset.source_image_session_asset_id is None
    assert db_session.get(MediaObject, attached.media_object_id) is not None
    assert storage.resolve(shared_path).exists()


def test_product_delete_preserves_media_still_owned_by_image_session(configured_env: Path, db_session) -> None:
    storage = LocalStorage(configured_env)
    product = create_canonical_product(
        db_session,
        name="待删除商品",
        category=None,
        price=None,
        source_note=None,
        image_uploads=[(_make_demo_image_bytes(), "shared.png", "image/png")],
    )
    product_asset = product.image_assets[0]
    media_id = product_asset.media_object_id
    storage_path = product_asset.media_object.storage_path
    image_session = create_image_session(db_session, title="保留共享文件")
    session_asset = ImageSessionAsset(
        session_id=image_session.id,
        kind=ImageSessionAssetKind.GENERATED_IMAGE,
        original_filename="shared.png",
        mime_type=product_asset.media_object.mime_type,
        storage_path=storage_path,
        media_object_id=media_id,
    )
    db_session.add(session_asset)
    db_session.commit()

    delete_product(db_session, product_id=product.id, storage=storage)

    assert db_session.get(Product, product.id) is None
    assert db_session.get(ImageSessionAsset, session_asset.id) is not None
    assert db_session.get(MediaObject, media_id) is not None
    assert storage.resolve(storage_path).exists()

    delete_image_session(db_session, image_session_id=image_session.id, storage=storage)

    assert db_session.get(MediaObject, media_id) is None
    assert not storage.resolve(storage_path).exists()


def test_canonical_product_api_exposes_assets_without_storage_paths(configured_env: Path) -> None:
    from productflow_backend.presentation.api import create_app

    client = TestClient(create_app())
    _login(client)
    created = client.post(
        "/api/v2/products",
        data={"name": "API 商品", "category": "工具", "price": "88.00"},
        files=[
            ("images", ("front.png", _make_demo_image_bytes(), "image/png")),
            ("images", ("detail.jpg", _make_jpeg_bytes(), "image/jpeg")),
        ],
    )

    assert created.status_code == 201, created.text
    payload = created.json()
    product_payload = payload["product"]
    created_assets = payload["created_assets"]
    assert product_payload["name"] == "API 商品"
    assert product_payload["price"] == "88.00"
    assert "image_assets" not in product_payload
    assert len(created_assets) == 2
    assert product_payload["cover_image_asset_id"] == created_assets[0]["id"]
    assert all(asset["verification_status"] == "verified" for asset in created_assets)
    assert "storage_path" not in created.text
    product_id = product_payload["id"]
    first_asset_id = created_assets[0]["id"]

    listed = client.get(f"/api/v2/products/{product_id}/image-assets")
    assert listed.status_code == 200
    assert [asset["id"] for asset in listed.json()["items"]] == [
        asset["id"] for asset in reversed(created_assets)
    ]
    assert listed.json()["next_cursor"] is None
    bootstrap = client.get(f"/api/v2/products/{product_id}/image-library")
    assert bootstrap.status_code == 200
    assert bootstrap.json()["unorganized_count"] == 2
    detail = client.get(f"/api/v2/products/{product_id}/image-assets/{first_asset_id}")
    assert detail.status_code == 200
    assert detail.json()["id"] == first_asset_id
    downloaded = client.get(f"/api/v2/product-image-assets/{first_asset_id}/download")
    assert downloaded.status_code == 200
    assert downloaded.headers["content-type"] == "image/png"
    preview = client.get(f"/api/v2/product-image-assets/{first_asset_id}/download?variant=preview")
    assert preview.status_code == 200
    assert preview.headers["content-type"] in {"image/webp", "image/jpeg"}

    added = client.post(
        f"/api/v2/products/{product_id}/image-assets",
        files=[
            ("images", ("extra.png", _make_demo_image_bytes(), "image/png")),
            ("images", ("extra-detail.jpg", _make_jpeg_bytes(), "image/jpeg")),
        ],
    )
    assert added.status_code == 201
    assert [asset["original_filename"] for asset in added.json()["items"]] == [
        "extra.png",
        "extra-detail.jpg",
    ]
    extra_asset_id = added.json()["items"][0]["id"]
    changed_cover = client.put(
        f"/api/v2/products/{product_id}/cover",
        json={"asset_id": extra_asset_id},
    )
    assert changed_cover.status_code == 200
    assert changed_cover.json()["cover_image_asset_id"] == extra_asset_id

    _enable_deletion(client)
    blocked = client.delete(f"/api/v2/product-image-assets/{extra_asset_id}")
    assert blocked.status_code == 409
    assert "封面" in blocked.json()["detail"]
    cleared = client.delete(f"/api/v2/products/{product_id}/cover")
    assert cleared.status_code == 200
    assert cleared.json()["cover_image_asset_id"] is None
    deleted = client.delete(f"/api/v2/product-image-assets/{extra_asset_id}")
    assert deleted.status_code == 204


def test_canonical_image_session_attach_api_keeps_shared_media_after_session_delete(
    configured_env: Path,
    db_session,
) -> None:
    from productflow_backend.presentation.api import create_app

    client = TestClient(create_app())
    _login(client)
    product_response = client.post(
        "/api/v2/products",
        data={"name": "ImageChat 目标商品"},
        files={"images": ("reference.png", _make_demo_image_bytes(), "image/png")},
    )
    assert product_response.status_code == 201
    product_id = product_response.json()["product"]["id"]
    session_response = client.post("/api/image-sessions", json={"title": "共享写回"})
    assert session_response.status_code == 201
    image_session_id = session_response.json()["id"]
    generated = client.post(
        f"/api/image-sessions/{image_session_id}/generate",
        json={"prompt": "生成一张工具产品图", "size": "1024x1024"},
    )
    assert generated.status_code == 202
    generated_asset_id = generated.json()["rounds"][-1]["generated_asset"]["id"]

    attached = client.post(
        f"/api/v2/image-sessions/{image_session_id}/assets/{generated_asset_id}/attach-to-product",
        json={"product_id": product_id},
    )
    attached_again = client.post(
        f"/api/v2/image-sessions/{image_session_id}/assets/{generated_asset_id}/attach-to-product",
        json={"product_id": product_id},
    )

    assert attached.status_code == 200, attached.text
    assert attached_again.status_code == 200
    assert attached_again.json()["id"] == attached.json()["id"]
    assert attached.json()["origin_type"] == "image_session_attach"
    db_session.expire_all()
    session_asset = db_session.get(ImageSessionAsset, generated_asset_id)
    assert session_asset is not None
    assert session_asset.media_object_id == attached.json()["media_object_id"]
    _enable_deletion(client)
    deleted_session = client.delete(f"/api/image-sessions/{image_session_id}")
    assert deleted_session.status_code == 204
    product_after_delete = client.get(f"/api/v2/products/{product_id}")
    assert product_after_delete.status_code == 200
    assert "image_assets" not in product_after_delete.json()
    gallery_after_delete = client.get(f"/api/v2/products/{product_id}/image-assets")
    assert gallery_after_delete.status_code == 200
    attached_after_delete = next(
        asset for asset in gallery_after_delete.json()["items"] if asset["id"] == attached.json()["id"]
    )
    assert attached_after_delete["source_image_session_asset_id"] is None
    assert client.get(attached_after_delete["download_url"]).status_code == 200
