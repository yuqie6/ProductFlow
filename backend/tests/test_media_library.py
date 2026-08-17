from __future__ import annotations

from datetime import UTC, datetime

import pytest
from helpers import _make_demo_image_bytes

from productflow_backend.application.media_library.contracts import (
    canonical_provenance_hash,
    parse_provenance_v1,
)
from productflow_backend.application.media_library.queries import list_media_library_assets
from productflow_backend.application.media_library.service import (
    archive_media_library_asset,
    collect_media_library_asset_to_product,
    collect_media_library_assets_to_product,
    restore_media_library_asset,
    save_media_library_asset_from_product,
    save_media_library_asset_from_session,
)
from productflow_backend.application.use_cases import create_canonical_product
from productflow_backend.domain.enums import ImageSessionAssetKind, MediaVerificationStatus
from productflow_backend.domain.errors import BusinessValidationError, ConflictError, NotFoundError
from productflow_backend.infrastructure.db.models import (
    ImageSession,
    ImageSessionAsset,
    MediaLibraryAsset,
    MediaObject,
    Product,
    ProductImageAsset,
)
from productflow_backend.presentation.schemas.media_library import serialize_media_library_asset


def _create_product_asset(db_session) -> tuple[Product, ProductImageAsset]:
    product = create_canonical_product(
        db_session,
        name="素材库测试商品",
        category=None,
        price=None,
        source_note=None,
        image_uploads=[(_make_demo_image_bytes(), "library.png", "image/png")],
    )
    return product, product.image_assets[0]


def _create_session_asset(db_session, *, kind: ImageSessionAssetKind = ImageSessionAssetKind.GENERATED_IMAGE):
    from uuid import uuid4

    session = ImageSession(title="素材库测试会话")
    storage_path = f"media/library-session-{uuid4().hex}.png"
    media = MediaObject(
        storage_path=storage_path,
        mime_type="image/png",
        byte_size=123,
        width=64,
        height=64,
        sha256="a" * 64,
        verification_status=MediaVerificationStatus.VERIFIED,
        verified_at=datetime.now(UTC),
    )
    db_session.add(session)
    db_session.flush()
    asset = ImageSessionAsset(
        session=session,
        kind=kind,
        original_filename="session.png",
        mime_type="image/png",
        storage_path=storage_path,
        media_object=media,
    )
    db_session.add(asset)
    db_session.commit()
    return session, asset


def test_provenance_hash_is_stable_and_parseable() -> None:
    payload = {
        "schema_version": 1,
        "source_type": "product_asset",
        "source_id": "asset-1",
        "sha256": "b" * 64,
        "mime_type": "image/png",
        "byte_size": 100,
        "width": 10,
        "height": 10,
        "original_filename": "1.png",
        "captured_at": "2026-08-16T00:00:00+00:00",
    }
    assert canonical_provenance_hash(payload) == canonical_provenance_hash(payload)
    parsed = parse_provenance_v1(payload)
    assert parsed.schema_version == 1
    assert parsed.source_id == "asset-1"
    with pytest.raises(ValueError):
        parse_provenance_v1({**payload, "storage_path": "media/secret.png"})


def test_save_media_library_asset_from_product_is_idempotent(db_session) -> None:
    _, asset = _create_product_asset(db_session)
    first = save_media_library_asset_from_product(db_session, product_image_asset_id=asset.id)
    second = save_media_library_asset_from_product(db_session, product_image_asset_id=asset.id)
    assert first.created is True
    assert second.created is False
    assert first.asset.id == second.asset.id
    assert first.asset.source_type == "product_asset"
    assert first.asset.media_object_id == asset.media_object_id
    assert len(first.asset.provenance_hash) == 64


def test_media_library_serialization_rejects_missing_media_object(db_session) -> None:
    _, source_asset = _create_product_asset(db_session)
    library_asset = save_media_library_asset_from_product(
        db_session,
        product_image_asset_id=source_asset.id,
    ).asset
    library_asset.media_object = None

    with pytest.raises(NotFoundError):
        serialize_media_library_asset(library_asset)


def test_media_library_cursor_is_filter_bound_and_accepts_sqlite_timestamps(db_session) -> None:
    assets = [_create_product_asset(db_session)[1] for _ in range(3)]
    for asset in assets:
        save_media_library_asset_from_product(db_session, product_image_asset_id=asset.id)

    first = list_media_library_assets(db_session, limit=1)
    assert first.next_cursor is not None
    second = list_media_library_assets(db_session, limit=1, cursor=first.next_cursor)
    assert len(first.items) == 1
    assert len(second.items) == 1
    assert first.items[0].id != second.items[0].id
    with pytest.raises(BusinessValidationError):
        list_media_library_assets(db_session, limit=1, cursor=first.next_cursor, search="different")


def test_collect_library_assets_to_product_batch_is_idempotent(db_session) -> None:
    product_a, source_a = _create_product_asset(db_session)
    product_b, source_b = _create_product_asset(db_session)
    library_a = save_media_library_asset_from_product(
        db_session,
        product_image_asset_id=source_a.id,
    ).asset
    library_b = save_media_library_asset_from_product(
        db_session,
        product_image_asset_id=source_b.id,
    ).asset

    first = collect_media_library_assets_to_product(
        db_session,
        product_id=product_a.id,
        library_asset_ids=[library_a.id],
    )
    second = collect_media_library_assets_to_product(
        db_session,
        product_id=product_a.id,
        library_asset_ids=[library_a.id, library_b.id],
    )
    third = collect_media_library_assets_to_product(
        db_session,
        product_id=product_a.id,
        library_asset_ids=[library_a.id, library_b.id],
    )

    assert [item.created for item in first] == [True]
    assert [item.created for item in second] == [False, True]
    assert [item.created for item in third] == [False, False]
    assert [item.asset.source_library_asset_id for item in second] == [library_a.id, library_b.id]


def test_collect_rejects_duplicate_ids(db_session) -> None:
    product, source_asset = _create_product_asset(db_session)
    library_asset = save_media_library_asset_from_product(
        db_session,
        product_image_asset_id=source_asset.id,
    ).asset
    with pytest.raises(BusinessValidationError):
        collect_media_library_assets_to_product(
            db_session,
            product_id=product.id,
            library_asset_ids=[library_asset.id, library_asset.id],
        )


def test_collect_rejects_archived_library_asset(db_session) -> None:
    product, source_asset = _create_product_asset(db_session)
    library_asset = save_media_library_asset_from_product(
        db_session,
        product_image_asset_id=source_asset.id,
    ).asset
    archive_media_library_asset(db_session, asset_id=library_asset.id)
    with pytest.raises(ConflictError):
        collect_media_library_assets_to_product(
            db_session,
            product_id=product.id,
            library_asset_ids=[library_asset.id],
        )


def test_collect_rejects_incoherent_library_provenance(db_session) -> None:
    product, source_asset = _create_product_asset(db_session)
    library_asset = save_media_library_asset_from_product(
        db_session,
        product_image_asset_id=source_asset.id,
    ).asset
    library_asset.provenance_json = {
        **library_asset.provenance_json,
        "sha256": "f" * 64,
    }
    db_session.commit()

    with pytest.raises(ConflictError):
        collect_media_library_assets_to_product(
            db_session,
            product_id=product.id,
            library_asset_ids=[library_asset.id],
        )


def test_product_delete_does_not_delete_library_asset(db_session) -> None:
    product, source_asset = _create_product_asset(db_session)
    library_asset = save_media_library_asset_from_product(
        db_session,
        product_image_asset_id=source_asset.id,
    ).asset
    collect_media_library_asset_to_product(
        db_session,
        product_id=product.id,
        library_asset_id=library_asset.id,
    )

    db_session.delete(product)
    db_session.commit()

    db_session.expire_all()
    assert db_session.get(MediaLibraryAsset, library_asset.id) is not None


def test_library_hard_delete_blocked_by_collected_product_asset(db_session) -> None:
    product, source_asset = _create_product_asset(db_session)
    library_asset = save_media_library_asset_from_product(
        db_session,
        product_image_asset_id=source_asset.id,
    ).asset
    collect_media_library_asset_to_product(
        db_session,
        product_id=product.id,
        library_asset_id=library_asset.id,
    )

    fk = next(
        fk
        for fk in ProductImageAsset.__table__.c.source_library_asset_id.foreign_keys
        if fk.target_fullname.endswith("media_library_assets.id")
    )
    assert fk.ondelete == "RESTRICT"


def test_save_media_library_asset_from_session_requires_generated_image(db_session) -> None:
    _, upload_asset = _create_session_asset(db_session, kind=ImageSessionAssetKind.REFERENCE_UPLOAD)
    with pytest.raises(BusinessValidationError):
        save_media_library_asset_from_session(db_session, image_session_asset_id=upload_asset.id)

    _, generated = _create_session_asset(db_session)
    result = save_media_library_asset_from_session(db_session, image_session_asset_id=generated.id)
    assert result.created is True
    assert result.asset.source_type == "image_session_generated"
    assert result.asset.source_image_session_asset_id == generated.id


def test_archive_and_restore_increment_revision(db_session) -> None:
    _, asset = _create_product_asset(db_session)
    saved = save_media_library_asset_from_product(db_session, product_image_asset_id=asset.id).asset
    initial_revision = saved.revision

    archived = archive_media_library_asset(db_session, asset_id=saved.id)
    assert archived.is_archived is True
    assert archived.archived_at is not None
    assert archived.revision == initial_revision + 1

    restored = restore_media_library_asset(db_session, asset_id=saved.id)
    assert restored.is_archived is False
    assert restored.archived_at is None
    assert restored.revision == initial_revision + 2
