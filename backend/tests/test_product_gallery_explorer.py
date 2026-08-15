from __future__ import annotations

import zipfile
from datetime import UTC, datetime, timedelta
from io import BytesIO

import pytest
from fastapi.testclient import TestClient
from helpers import _login, _make_demo_image_bytes

from productflow_backend.application.gallery_assets import (
    GALLERY_UNCLASSIFIED_TYPE_KEY,
    GalleryAssetSort,
    GalleryDirectoryKind,
    get_gallery_asset_detail,
    get_gallery_bootstrap,
    list_gallery_assets,
)
from productflow_backend.application.gallery_mutations import (
    GalleryAssetMove,
    create_gallery_folder,
    delete_gallery_folder,
    move_gallery_assets,
    rename_gallery_asset,
    rename_gallery_folder,
)
from productflow_backend.domain.enums import MediaVerificationStatus, ProductImageOriginType
from productflow_backend.domain.errors import BusinessValidationError, ConflictError, NotFoundError
from productflow_backend.infrastructure.db.models import (
    MediaObject,
    Product,
    ProductAssetFolder,
    ProductImageAsset,
)


def _asset(
    product: Product,
    *,
    suffix: str,
    name: str,
    created_at: datetime,
    origin: ProductImageOriginType = ProductImageOriginType.UPLOAD,
    image_type_key: str | None = None,
    folder: ProductAssetFolder | None = None,
) -> ProductImageAsset:
    media = MediaObject(
        storage_path=f"gallery/{product.id}/{suffix}.png",
        mime_type="image/png",
        byte_size=1024,
        width=100,
        height=100,
        sha256="a" * 64,
        verification_status=MediaVerificationStatus.VERIFIED,
        created_at=created_at,
        verified_at=created_at,
    )
    return ProductImageAsset(
        product=product,
        media_object=media,
        origin_type=origin,
        display_name=name,
        original_filename=f"{name}.png",
        image_type_key=image_type_key,
        user_folder=folder,
        created_at=created_at,
        updated_at=created_at,
    )


def _seed_gallery(db_session):
    now = datetime(2026, 8, 12, 12, tzinfo=UTC)
    product = Product(name="图库测试商品", created_at=now, updated_at=now)
    other_product = Product(name="其他商品", created_at=now, updated_at=now)
    db_session.add_all([product, other_product])
    db_session.flush()
    folder = ProductAssetFolder(
        product=product,
        name="精选",
        sort_order=0,
        created_at=now,
        updated_at=now,
    )
    other_folder = ProductAssetFolder(
        product=other_product,
        name="其他目录",
        sort_order=0,
        created_at=now,
        updated_at=now,
    )
    db_session.add_all([folder, other_folder])
    db_session.flush()
    assets = [
        _asset(
            product,
            suffix="upload",
            name="上传 %_ 参考",
            created_at=now - timedelta(days=40),
            folder=folder,
        ),
        _asset(
            product,
            suffix="hero-a",
            name="主图 Alpha",
            created_at=now - timedelta(days=10),
            origin=ProductImageOriginType.WORKFLOW_GENERATION,
            image_type_key="hero",
        ),
        _asset(
            product,
            suffix="hero-b",
            name="主图 beta",
            created_at=now - timedelta(days=1),
            origin=ProductImageOriginType.WORKFLOW_GENERATION,
            image_type_key="hero",
            folder=folder,
        ),
        _asset(
            product,
            suffix="session",
            name="会话生成",
            created_at=now - timedelta(days=2),
            origin=ProductImageOriginType.IMAGE_SESSION_ATTACH,
        ),
        _asset(
            product,
            suffix="upload-extra",
            name="补充参考",
            created_at=now - timedelta(days=3),
            origin=ProductImageOriginType.UPLOAD,
        ),
    ]
    db_session.add_all(assets)
    db_session.commit()
    return now, product, folder, other_folder, assets


def _collect_ids(db_session, *, product_id: str, sort: GalleryAssetSort, limit: int = 2) -> list[str]:
    ids: list[str] = []
    after = ""
    while True:
        page = list_gallery_assets(
            db_session,
            product_id=product_id,
            sort=sort,
            after=after,
            limit=limit,
        )
        ids.extend(record.asset.id for record in page.items)
        if page.next_cursor is None:
            return ids
        after = page.next_cursor


def test_gallery_directories_bootstrap_search_and_detail(db_session) -> None:
    now, product, folder, other_folder, assets = _seed_gallery(db_session)

    bootstrap = get_gallery_bootstrap(db_session, product_id=product.id, current_time=now)
    system_counts = {directory.kind: directory.count for directory in bootstrap.system_directories}
    assert system_counts == {
        GalleryDirectoryKind.ALL: 5,
        GalleryDirectoryKind.RECENT_GENERATED: 3,
        GalleryDirectoryKind.UPLOADS: 2,
        GalleryDirectoryKind.GENERATED: 3,
        GalleryDirectoryKind.UNORGANIZED: 3,
    }
    assert [(item.directory_key, item.image_type_key, item.title, item.count) for item in bootstrap.image_types] == [
        (GALLERY_UNCLASSIFIED_TYPE_KEY, None, "未分类", 3),
        ("hero", "hero", "hero", 2),
    ]
    assert [(item.id, item.name, item.sort_order, item.count) for item in bootstrap.user_folders] == [
        (folder.id, "精选", 0, 2)
    ]
    assert bootstrap.unorganized_count == 3

    expected_by_directory = {
        (GalleryDirectoryKind.UPLOADS, None): {assets[0].id, assets[4].id},
        (GalleryDirectoryKind.GENERATED, None): {assets[1].id, assets[2].id, assets[3].id},
        (GalleryDirectoryKind.RECENT_GENERATED, None): {assets[1].id, assets[2].id, assets[3].id},
        (GalleryDirectoryKind.IMAGE_TYPE, "hero"): {assets[1].id, assets[2].id},
        (GalleryDirectoryKind.IMAGE_TYPE, GALLERY_UNCLASSIFIED_TYPE_KEY): {
            assets[0].id,
            assets[3].id,
            assets[4].id,
        },
        (GalleryDirectoryKind.SOURCE, ProductImageOriginType.UPLOAD.value): {assets[0].id, assets[4].id},
        (GalleryDirectoryKind.UNORGANIZED, None): {assets[1].id, assets[3].id, assets[4].id},
        (GalleryDirectoryKind.USER_FOLDER, folder.id): {assets[0].id, assets[2].id},
    }
    for (kind, key), expected_ids in expected_by_directory.items():
        page = list_gallery_assets(
            db_session,
            product_id=product.id,
            directory_kind=kind,
            directory_key=key,
            current_time=now,
        )
        assert {record.asset.id for record in page.items} == expected_ids

    literal_search = list_gallery_assets(db_session, product_id=product.id, query="%_")
    assert [record.asset.id for record in literal_search.items] == [assets[0].id]
    original_filename_search = list_gallery_assets(db_session, product_id=product.id, query="ALPHA.PNG")
    assert [record.asset.id for record in original_filename_search.items] == [assets[1].id]

    detail = get_gallery_asset_detail(db_session, product_id=product.id, asset_id=assets[2].id)
    assert detail.asset.user_folder_id == folder.id
    assert detail.image_type_title == "hero"
    assert detail.generation is None

    with pytest.raises(NotFoundError, match="文件夹不存在"):
        list_gallery_assets(
            db_session,
            product_id=product.id,
            directory_kind=GalleryDirectoryKind.USER_FOLDER,
            directory_key=other_folder.id,
        )
    with pytest.raises(NotFoundError, match="商品图片不存在"):
        get_gallery_asset_detail(db_session, product_id=product.id, asset_id="missing")


def test_gallery_keyset_sorts_are_stable_and_cursor_is_bound_to_filters(db_session) -> None:
    _, product, _, _, assets = _seed_gallery(db_session)
    for sort in GalleryAssetSort:
        ids = _collect_ids(db_session, product_id=product.id, sort=sort)
        assert len(ids) == len(assets)
        assert len(set(ids)) == len(assets)
        if sort == GalleryAssetSort.CREATED_ASC:
            assert ids == [
                asset.id for asset in sorted(assets, key=lambda asset: (asset.created_at, asset.id))
            ]
        elif sort == GalleryAssetSort.CREATED_DESC:
            assert ids == [
                asset.id
                for asset in sorted(
                    assets,
                    key=lambda asset: (asset.created_at, asset.id),
                    reverse=True,
                )
            ]
        elif sort == GalleryAssetSort.NAME_ASC:
            assert ids == [
                asset.id
                for asset in sorted(assets, key=lambda asset: (asset.display_name.lower(), asset.id))
            ]
        else:
            assert ids == [
                asset.id
                for asset in sorted(
                    assets,
                    key=lambda asset: (asset.display_name.lower(), asset.id),
                    reverse=True,
                )
            ]

    first_page = list_gallery_assets(db_session, product_id=product.id, limit=1)
    assert first_page.next_cursor is not None
    with pytest.raises(BusinessValidationError, match="查询条件不匹配"):
        list_gallery_assets(
            db_session,
            product_id=product.id,
            query="主图",
            after=first_page.next_cursor,
            limit=1,
        )
    with pytest.raises(BusinessValidationError, match="查询条件不匹配"):
        list_gallery_assets(
            db_session,
            product_id=product.id,
            sort=GalleryAssetSort.CREATED_ASC,
            after=first_page.next_cursor,
            limit=1,
        )
    with pytest.raises(BusinessValidationError, match="cursor 无效"):
        list_gallery_assets(db_session, product_id=product.id, after="not-a-cursor")


def test_recent_generated_cursor_keeps_first_page_time_anchor(db_session) -> None:
    now, product, _, _, assets = _seed_gallery(db_session)
    first_page = list_gallery_assets(
        db_session,
        product_id=product.id,
        directory_kind=GalleryDirectoryKind.RECENT_GENERATED,
        sort=GalleryAssetSort.CREATED_ASC,
        limit=1,
        current_time=now,
    )
    assert [record.asset.id for record in first_page.items] == [assets[1].id]
    assert first_page.next_cursor is not None

    second_page = list_gallery_assets(
        db_session,
        product_id=product.id,
        directory_kind=GalleryDirectoryKind.RECENT_GENERATED,
        sort=GalleryAssetSort.CREATED_ASC,
        limit=10,
        after=first_page.next_cursor,
        current_time=now + timedelta(days=60),
    )
    assert [record.asset.id for record in second_page.items] == [assets[3].id, assets[2].id]


def test_gallery_rejects_invalid_directory_key_combinations(db_session) -> None:
    _, product, _, _, _ = _seed_gallery(db_session)
    with pytest.raises(BusinessValidationError, match="必须提供"):
        list_gallery_assets(
            db_session,
            product_id=product.id,
            directory_kind=GalleryDirectoryKind.IMAGE_TYPE,
        )
    with pytest.raises(BusinessValidationError, match="不接受"):
        list_gallery_assets(
            db_session,
            product_id=product.id,
            directory_kind=GalleryDirectoryKind.ALL,
            directory_key="hero",
        )
    with pytest.raises(BusinessValidationError, match="图片来源"):
        list_gallery_assets(
            db_session,
            product_id=product.id,
            directory_kind=GalleryDirectoryKind.SOURCE,
            directory_key="unknown",
        )


def test_gallery_pages_one_thousand_assets_without_duplicates_or_omissions(db_session) -> None:
    started_at = datetime(2026, 1, 1, tzinfo=UTC)
    product = Product(name="千图商品", created_at=started_at, updated_at=started_at)
    db_session.add(product)
    db_session.flush()
    assets = [
        _asset(
            product,
            suffix=f"bulk-{index:04d}",
            name=f"批量图片 {999 - index:04d}",
            created_at=started_at + timedelta(seconds=index // 3),
            origin=(
                ProductImageOriginType.WORKFLOW_GENERATION
                if index % 2
                else ProductImageOriginType.UPLOAD
            ),
            image_type_key="hero" if index % 3 == 0 else None,
        )
        for index in range(1000)
    ]
    db_session.add_all(assets)
    db_session.commit()
    expected_ids = {asset.id for asset in assets}

    for sort in GalleryAssetSort:
        paged_ids = _collect_ids(db_session, product_id=product.id, sort=sort, limit=37)
        assert len(paged_ids) == 1000
        assert len(set(paged_ids)) == 1000
        assert set(paged_ids) == expected_ids


def test_gallery_folder_mutations_append_order_and_do_not_touch_product_time(db_session) -> None:
    now, product, _, _, assets = _seed_gallery(db_session)
    original_product_updated_at = product.updated_at

    first = create_gallery_folder(db_session, product_id=product.id, name="  新目录  ")
    second = create_gallery_folder(db_session, product_id=product.id, name="第二目录")
    assert first.name == "新目录"
    assert (first.sort_order, second.sort_order) == (1, 2)

    renamed = rename_gallery_folder(
        db_session,
        product_id=product.id,
        folder_id=first.id,
        expected_name="新目录",
        name="精选二组",
    )
    assert renamed.name == "精选二组"
    with pytest.raises(ConflictError, match="已被其他操作修改"):
        rename_gallery_folder(
            db_session,
            product_id=product.id,
            folder_id=first.id,
            expected_name="新目录",
            name="不会生效",
        )

    renamed_asset = rename_gallery_asset(
        db_session,
        product_id=product.id,
        asset_id=assets[0].id,
        expected_display_name=assets[0].display_name,
        display_name="商品原始参考",
    )
    assert renamed_asset.display_name == "商品原始参考"
    assert renamed_asset.original_filename == "上传 %_ 参考.png"
    db_session.refresh(product)
    assert product.updated_at.replace(tzinfo=UTC) == original_product_updated_at == now


def test_gallery_batch_move_is_atomic_and_folder_delete_returns_assets_to_unorganized(db_session) -> None:
    _, product, folder, _, assets = _seed_gallery(db_session)
    target = create_gallery_folder(db_session, product_id=product.id, name="目标目录")

    moved = move_gallery_assets(
        db_session,
        product_id=product.id,
        moves=[
            GalleryAssetMove(asset_id=assets[0].id, expected_folder_id=folder.id),
            GalleryAssetMove(asset_id=assets[1].id, expected_folder_id=None),
        ],
        folder_id=target.id,
    )
    assert [asset.id for asset in moved] == [assets[0].id, assets[1].id]
    assert all(asset.user_folder_id == target.id for asset in moved)

    with pytest.raises(ConflictError, match="所在文件夹已被其他操作修改"):
        move_gallery_assets(
            db_session,
            product_id=product.id,
            moves=[
                GalleryAssetMove(asset_id=assets[0].id, expected_folder_id=folder.id),
                GalleryAssetMove(asset_id=assets[2].id, expected_folder_id=folder.id),
            ],
            folder_id=None,
        )
    db_session.expire_all()
    assert db_session.get(ProductImageAsset, assets[0].id).user_folder_id == target.id
    assert db_session.get(ProductImageAsset, assets[2].id).user_folder_id == folder.id

    moved_count = delete_gallery_folder(
        db_session,
        product_id=product.id,
        folder_id=target.id,
        expected_name="目标目录",
    )
    assert moved_count == 2
    db_session.expire_all()
    assert db_session.get(ProductAssetFolder, target.id) is None
    assert db_session.get(ProductImageAsset, assets[0].id).user_folder_id is None
    assert db_session.get(ProductImageAsset, assets[1].id).user_folder_id is None
    assert db_session.get(MediaObject, assets[0].media_object_id) is not None


def test_gallery_mutations_reject_duplicates_cross_product_and_stale_before_values(db_session) -> None:
    _, product, folder, other_folder, assets = _seed_gallery(db_session)
    other_asset = other_folder.product.image_assets[0] if other_folder.product.image_assets else None
    if other_asset is None:
        other_asset = _asset(
            other_folder.product,
            suffix="other",
            name="其他图片",
            created_at=datetime(2026, 8, 12, tzinfo=UTC),
            folder=other_folder,
        )
        db_session.add(other_asset)
        db_session.commit()

    with pytest.raises(BusinessValidationError, match="重复图片"):
        move_gallery_assets(
            db_session,
            product_id=product.id,
            moves=[
                GalleryAssetMove(asset_id=assets[0].id, expected_folder_id=folder.id),
                GalleryAssetMove(asset_id=assets[0].id, expected_folder_id=folder.id),
            ],
            folder_id=None,
        )
    with pytest.raises(NotFoundError, match="商品图片不存在"):
        move_gallery_assets(
            db_session,
            product_id=product.id,
            moves=[GalleryAssetMove(asset_id=other_asset.id, expected_folder_id=None)],
            folder_id=None,
        )
    with pytest.raises(NotFoundError, match="文件夹不存在"):
        move_gallery_assets(
            db_session,
            product_id=product.id,
            moves=[GalleryAssetMove(asset_id=assets[1].id, expected_folder_id=None)],
            folder_id=other_folder.id,
        )
    with pytest.raises(ConflictError, match="图片显示名已被其他操作修改"):
        rename_gallery_asset(
            db_session,
            product_id=product.id,
            asset_id=assets[1].id,
            expected_display_name="旧名字",
            display_name="新名字",
        )


def test_gallery_browser_api_organizes_and_downloads_archive(
    configured_env,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    from productflow_backend.application import gallery_archives
    from productflow_backend.presentation.api import create_app
    from productflow_backend.presentation.routes import products as product_routes

    client = TestClient(create_app())
    _login(client)
    created = client.post(
        "/api/v2/products",
        data={"name": "图库 API 商品"},
        files=[
            ("images", ("front.png", _make_demo_image_bytes(), "image/png")),
            ("images", ("detail.png", _make_demo_image_bytes(), "image/png")),
        ],
    )
    assert created.status_code == 201, created.text
    payload = created.json()
    product_id = payload["product"]["id"]
    first_asset, second_asset = payload["created_assets"]

    invalid_folder = client.post(
        f"/api/v2/products/{product_id}/image-folders",
        json={"name": "精选", "unknown": True},
    )
    assert invalid_folder.status_code == 422
    folder_response = client.post(
        f"/api/v2/products/{product_id}/image-folders",
        json={"name": "精选"},
    )
    assert folder_response.status_code == 201, folder_response.text
    folder = folder_response.json()
    renamed_folder = client.patch(
        f"/api/v2/products/{product_id}/image-folders/{folder['id']}",
        json={"expected_name": "精选", "name": "首选"},
    )
    assert renamed_folder.status_code == 200
    assert renamed_folder.json()["name"] == "首选"

    renamed_first = client.patch(
        f"/api/v2/products/{product_id}/image-assets/{first_asset['id']}",
        json={"expected_display_name": first_asset["display_name"], "display_name": "同名图片"},
    )
    renamed_second = client.patch(
        f"/api/v2/products/{product_id}/image-assets/{second_asset['id']}",
        json={"expected_display_name": second_asset["display_name"], "display_name": "同名图片"},
    )
    assert renamed_first.status_code == 200, renamed_first.text
    assert renamed_second.status_code == 200, renamed_second.text

    moved = client.post(
        f"/api/v2/products/{product_id}/image-assets/move",
        json={
            "items": [
                {"asset_id": first_asset["id"], "expected_folder_id": None},
                {"asset_id": second_asset["id"], "expected_folder_id": None},
            ],
            "folder_id": folder["id"],
        },
    )
    assert moved.status_code == 200, moved.text
    assert {item["user_folder_id"] for item in moved.json()["items"]} == {folder["id"]}
    listed = client.get(
        f"/api/v2/products/{product_id}/image-assets",
        params={"directory_kind": "user_folder", "directory_key": folder["id"]},
    )
    assert listed.status_code == 200
    assert {item["id"] for item in listed.json()["items"]} == {
        first_asset["id"],
        second_asset["id"],
    }

    archive_paths = []
    real_build_archive = gallery_archives.build_gallery_archive

    def tracking_build_archive(*args, **kwargs):
        archive = real_build_archive(*args, **kwargs)
        archive_paths.append(archive.path)
        return archive

    monkeypatch.setattr(product_routes, "build_gallery_archive", tracking_build_archive)
    archive_response = client.post(
        f"/api/v2/products/{product_id}/image-assets/download-archive",
        json={"asset_ids": [first_asset["id"], second_asset["id"]]},
    )
    assert archive_response.status_code == 200, archive_response.text
    assert archive_response.headers["content-type"] == "application/zip"
    with zipfile.ZipFile(BytesIO(archive_response.content)) as archive:
        names = archive.namelist()
        assert names[0] == "同名图片.png"
        assert names[1] == f"同名图片-{second_asset['id'][:8]}.png"
    assert archive_paths and not archive_paths[0].exists()

    deleted = client.delete(
        f"/api/v2/products/{product_id}/image-folders/{folder['id']}",
        params={"expected_name": "首选"},
    )
    assert deleted.status_code == 200
    assert deleted.json()["moved_to_unorganized_count"] == 2
    bootstrap = client.get(f"/api/v2/products/{product_id}/image-library")
    assert bootstrap.status_code == 200
    assert bootstrap.json()["unorganized_count"] == 2
