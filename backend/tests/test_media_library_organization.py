from __future__ import annotations

import pytest
from helpers import _make_demo_image_bytes

from productflow_backend.application.media_library.organization import (
    create_media_library_folder,
    create_media_library_tag,
    delete_media_library_folder,
    delete_media_library_tag,
    move_media_library_assets,
    normalize_media_library_key,
    normalize_media_library_name,
    rename_media_library_folder,
    set_media_library_asset_tags,
)
from productflow_backend.application.media_library.queries import (
    get_media_library_bootstrap,
    list_media_library_assets,
)
from productflow_backend.application.media_library.service import save_media_library_asset_from_product
from productflow_backend.application.use_cases import create_canonical_product
from productflow_backend.domain.errors import BusinessValidationError, ConflictError
from productflow_backend.infrastructure.db.models import MediaLibraryAsset, MediaLibraryAssetTag


def _asset(db_session):
    product = create_canonical_product(
        db_session,
        name="组织测试商品",
        category=None,
        price=None,
        source_note=None,
        image_uploads=[(_make_demo_image_bytes(), "asset.png", "image/png")],
    )
    return save_media_library_asset_from_product(db_session, product_image_asset_id=product.image_assets[0].id).asset


def test_media_library_names_normalize_unicode_case_and_whitespace() -> None:
    assert normalize_media_library_name("  春季\n  主图 ", kind="文件夹") == "春季 主图"
    assert normalize_media_library_key("  TeSt  ", kind="标签") == "test"
    assert normalize_media_library_key("Straße", kind="标签") == "strasse"
    with pytest.raises(BusinessValidationError):
        normalize_media_library_name(" \t", kind="标签")


def test_folder_and_tag_organization_is_revision_checked_and_deleting_is_non_destructive(db_session) -> None:
    asset = _asset(db_session)
    folder = create_media_library_folder(db_session, name="  Folder  ").folder
    assert create_media_library_folder(db_session, name="folder").created is False
    moved = move_media_library_assets(
        db_session,
        asset_ids=[asset.id],
        folder_id=folder.id,
        expected_revision={asset.id: asset.revision},
    )[0]
    assert moved.folder_id == folder.id
    with pytest.raises(ConflictError):
        move_media_library_assets(
            db_session,
            asset_ids=[asset.id],
            folder_id=None,
            expected_revision={asset.id: asset.revision - 1},
        )
    renamed = rename_media_library_folder(db_session, folder_id=folder.id, expected_name="Folder", name="New Folder")
    assert renamed.name == "New Folder"
    tag = create_media_library_tag(db_session, name="  Featured ").tag
    tagged = set_media_library_asset_tags(
        db_session,
        asset_ids=[asset.id],
        tag_names=[" featured ", "新品"],
        expected_revision={asset.id: moved.revision},
    )[0]
    assert {assignment.tag.normalized_name for assignment in tagged.tag_assignments} == {"featured", "新品"}
    assert delete_media_library_tag(db_session, tag_id=tag.id) == 1
    db_session.expire_all()
    assert db_session.query(MediaLibraryAssetTag).filter_by(tag_id=tag.id).count() == 0
    assert delete_media_library_folder(db_session, folder_id=folder.id) == 1
    db_session.expire_all()
    assert db_session.get(MediaLibraryAsset, asset.id).folder_id is None


def test_media_library_bootstrap_and_list_share_folder_tag_filters(db_session) -> None:
    first = _asset(db_session)
    second = _asset(db_session)
    folder = create_media_library_folder(db_session, name="A").folder
    first = move_media_library_assets(
        db_session,
        asset_ids=[first.id],
        folder_id=folder.id,
        expected_revision={first.id: first.revision},
    )[0]
    first = set_media_library_asset_tags(
        db_session,
        asset_ids=[first.id],
        tag_names=["Red"],
        expected_revision={first.id: first.revision},
    )[0]
    second = move_media_library_assets(
        db_session,
        asset_ids=[second.id],
        folder_id=folder.id,
        expected_revision={second.id: second.revision},
    )[0]
    set_media_library_asset_tags(
        db_session,
        asset_ids=[second.id],
        tag_names=["Red"],
        expected_revision={second.id: second.revision},
    )
    bootstrap = get_media_library_bootstrap(db_session)
    assert bootstrap.total_count >= 2
    assert any(name == "A" and count == 2 for _, name, count in bootstrap.folders)
    assert any(name == "Red" and count == 2 for _, name, count in bootstrap.tags)
    page = list_media_library_assets(db_session, folder_id=folder.id, tag=" RED ", limit=1)
    assert len(page.items) == 1
    assert page.items[0].id in {first.id, second.id}
    assert page.next_cursor is not None
    with pytest.raises(BusinessValidationError):
        list_media_library_assets(db_session, folder_id=folder.id, tag="different", cursor=page.next_cursor)
