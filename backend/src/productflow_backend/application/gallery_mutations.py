from __future__ import annotations

from dataclasses import dataclass

from sqlalchemy import func, select
from sqlalchemy.exc import IntegrityError
from sqlalchemy.orm import Session

from productflow_backend.application.time import now_utc
from productflow_backend.domain.errors import BusinessValidationError, ConflictError, NotFoundError
from productflow_backend.infrastructure.db.models import (
    Product,
    ProductAssetFolder,
    ProductImageAsset,
    new_id,
)

GALLERY_MOVE_MAX_ASSETS = 100


@dataclass(frozen=True, slots=True)
class GalleryAssetMove:
    asset_id: str
    expected_folder_id: str | None


def normalize_gallery_folder_name(value: str) -> str:
    normalized = value.strip()
    if not normalized:
        raise BusinessValidationError("文件夹名称不能为空")
    if len(normalized) > 120:
        raise BusinessValidationError("文件夹名称不能超过 120 个字符")
    return normalized


def normalize_gallery_display_name(value: str) -> str:
    normalized = value.strip()
    if not normalized:
        raise BusinessValidationError("图片显示名不能为空")
    if len(normalized) > 255:
        raise BusinessValidationError("图片显示名不能超过 255 个字符")
    return normalized


def create_gallery_folder(
    session: Session,
    *,
    product_id: str,
    name: str,
    folder_id: str | None = None,
) -> ProductAssetFolder:
    folder = stage_create_gallery_folder(
        session,
        product_id=product_id,
        name=name,
        folder_id=folder_id,
    )
    try:
        session.commit()
    except IntegrityError:
        session.rollback()
        raise ConflictError("当前商品已存在同名文件夹") from None
    session.refresh(folder)
    return folder


def stage_create_gallery_folder(
    session: Session,
    *,
    product_id: str,
    name: str,
    folder_id: str | None = None,
) -> ProductAssetFolder:
    normalized_name = normalize_gallery_folder_name(name)
    product = _get_product_for_update(session, product_id)
    stable_folder_id = (folder_id or new_id()).strip()
    if not stable_folder_id or len(stable_folder_id) > 36:
        raise BusinessValidationError("文件夹 ID 无效")
    existing_id = session.get(ProductAssetFolder, stable_folder_id)
    if existing_id is not None:
        raise ConflictError("文件夹 ID 已存在")
    current_max_sort_order = session.scalar(
        select(func.max(ProductAssetFolder.sort_order)).where(
            ProductAssetFolder.product_id == product.id
        )
    )
    folder = ProductAssetFolder(
        id=stable_folder_id,
        product_id=product.id,
        name=normalized_name,
        sort_order=0 if current_max_sort_order is None else current_max_sort_order + 1,
    )
    session.add(folder)
    return folder


def rename_gallery_folder(
    session: Session,
    *,
    product_id: str,
    folder_id: str,
    expected_name: str,
    name: str,
) -> ProductAssetFolder:
    folder = stage_rename_gallery_folder(
        session,
        product_id=product_id,
        folder_id=folder_id,
        expected_name=expected_name,
        name=name,
    )
    try:
        session.commit()
    except IntegrityError:
        session.rollback()
        raise ConflictError("当前商品已存在同名文件夹") from None
    session.refresh(folder)
    return folder


def stage_rename_gallery_folder(
    session: Session,
    *,
    product_id: str,
    folder_id: str,
    expected_name: str,
    name: str,
) -> ProductAssetFolder:
    _get_product_for_update(session, product_id)
    normalized_expected = normalize_gallery_folder_name(expected_name)
    normalized_name = normalize_gallery_folder_name(name)
    folder = _get_folder_for_update(session, product_id=product_id, folder_id=folder_id)
    if folder.name != normalized_expected:
        raise ConflictError("文件夹名称已被其他操作修改")
    if folder.name != normalized_name:
        folder.name = normalized_name
        folder.updated_at = now_utc()
    return folder


def delete_gallery_folder(
    session: Session,
    *,
    product_id: str,
    folder_id: str,
    expected_name: str,
) -> int:
    _get_product_for_update(session, product_id)
    normalized_expected = normalize_gallery_folder_name(expected_name)
    folder = _get_folder_for_update(session, product_id=product_id, folder_id=folder_id)
    if folder.name != normalized_expected:
        raise ConflictError("文件夹名称已被其他操作修改")
    assets = list(
        session.scalars(
            select(ProductImageAsset)
            .where(
                ProductImageAsset.product_id == product_id,
                ProductImageAsset.user_folder_id == folder.id,
            )
            .order_by(ProductImageAsset.id)
            .with_for_update()
        )
    )
    changed_at = now_utc()
    for asset in assets:
        asset.user_folder_id = None
        asset.updated_at = changed_at
    session.delete(folder)
    session.commit()
    return len(assets)


def rename_gallery_asset(
    session: Session,
    *,
    product_id: str,
    asset_id: str,
    expected_display_name: str,
    display_name: str,
) -> ProductImageAsset:
    asset = stage_rename_gallery_asset(
        session,
        product_id=product_id,
        asset_id=asset_id,
        expected_display_name=expected_display_name,
        display_name=display_name,
    )
    session.commit()
    session.refresh(asset)
    return asset


def stage_rename_gallery_asset(
    session: Session,
    *,
    product_id: str,
    asset_id: str,
    expected_display_name: str,
    display_name: str,
) -> ProductImageAsset:
    _get_product_for_update(session, product_id)
    normalized_expected = normalize_gallery_display_name(expected_display_name)
    normalized_name = normalize_gallery_display_name(display_name)
    asset = _get_asset_for_update(session, product_id=product_id, asset_id=asset_id)
    if asset.display_name != normalized_expected:
        raise ConflictError("图片显示名已被其他操作修改")
    if asset.display_name != normalized_name:
        asset.display_name = normalized_name
        asset.updated_at = now_utc()
    return asset


def move_gallery_assets(
    session: Session,
    *,
    product_id: str,
    moves: list[GalleryAssetMove],
    folder_id: str | None,
) -> list[ProductImageAsset]:
    assets = stage_move_gallery_assets(
        session,
        product_id=product_id,
        moves=moves,
        folder_id=folder_id,
    )
    session.commit()
    return assets


def stage_move_gallery_assets(
    session: Session,
    *,
    product_id: str,
    moves: list[GalleryAssetMove],
    folder_id: str | None,
) -> list[ProductImageAsset]:
    normalized_moves = _normalize_moves(moves)
    _get_product_for_update(session, product_id)

    folder_ids = {
        expected_folder_id
        for expected_folder_id in (move.expected_folder_id for move in normalized_moves)
        if expected_folder_id is not None
    }
    if folder_id is not None:
        normalized_target = folder_id.strip()
        if not normalized_target:
            raise BusinessValidationError("目标文件夹 ID 无效")
        folder_ids.add(normalized_target)
    else:
        normalized_target = None
    if folder_ids:
        folders = list(
            session.scalars(
                select(ProductAssetFolder)
                .where(
                    ProductAssetFolder.product_id == product_id,
                    ProductAssetFolder.id.in_(sorted(folder_ids)),
                )
                .order_by(ProductAssetFolder.id)
                .with_for_update()
            )
        )
        if {folder.id for folder in folders} != folder_ids:
            raise NotFoundError("商品图片文件夹不存在")

    requested_ids = [move.asset_id for move in normalized_moves]
    assets = list(
        session.scalars(
            select(ProductImageAsset)
            .where(
                ProductImageAsset.product_id == product_id,
                ProductImageAsset.id.in_(requested_ids),
            )
            .order_by(ProductImageAsset.id)
            .with_for_update()
        )
    )
    if len(assets) != len(requested_ids):
        raise NotFoundError("商品图片不存在")
    expected_by_id = {move.asset_id: move.expected_folder_id for move in normalized_moves}
    for asset in assets:
        if asset.user_folder_id != expected_by_id[asset.id]:
            raise ConflictError("图片所在文件夹已被其他操作修改")

    changed_at = now_utc()
    for asset in assets:
        if asset.user_folder_id != normalized_target:
            asset.user_folder_id = normalized_target
            asset.updated_at = changed_at
    assets_by_id = {asset.id: asset for asset in assets}
    return [assets_by_id[asset_id] for asset_id in requested_ids]


def _normalize_moves(moves: list[GalleryAssetMove]) -> list[GalleryAssetMove]:
    if not 1 <= len(moves) <= GALLERY_MOVE_MAX_ASSETS:
        raise BusinessValidationError(
            f"单次必须移动 1 到 {GALLERY_MOVE_MAX_ASSETS} 张图片"
        )
    normalized: list[GalleryAssetMove] = []
    seen: set[str] = set()
    for move in moves:
        asset_id = move.asset_id.strip()
        if not asset_id:
            raise BusinessValidationError("图片 ID 不能为空")
        if asset_id in seen:
            raise BusinessValidationError("移动列表不能包含重复图片 ID")
        seen.add(asset_id)
        expected_folder_id = (
            move.expected_folder_id.strip() if move.expected_folder_id is not None else None
        )
        if move.expected_folder_id is not None and not expected_folder_id:
            raise BusinessValidationError("expected_folder_id 无效")
        normalized.append(
            GalleryAssetMove(asset_id=asset_id, expected_folder_id=expected_folder_id)
        )
    return normalized


def _get_product_for_update(session: Session, product_id: str) -> Product:
    product = session.scalar(select(Product).where(Product.id == product_id).with_for_update())
    if product is None:
        raise NotFoundError("商品不存在")
    return product


def _get_folder_for_update(
    session: Session,
    *,
    product_id: str,
    folder_id: str,
) -> ProductAssetFolder:
    folder = session.scalar(
        select(ProductAssetFolder)
        .where(ProductAssetFolder.id == folder_id, ProductAssetFolder.product_id == product_id)
        .with_for_update()
    )
    if folder is None:
        raise NotFoundError("商品图片文件夹不存在")
    return folder


def _get_asset_for_update(
    session: Session,
    *,
    product_id: str,
    asset_id: str,
) -> ProductImageAsset:
    asset = session.scalar(
        select(ProductImageAsset)
        .where(ProductImageAsset.id == asset_id, ProductImageAsset.product_id == product_id)
        .with_for_update()
    )
    if asset is None:
        raise NotFoundError("商品图片不存在")
    return asset


__all__ = [
    "GALLERY_MOVE_MAX_ASSETS",
    "GalleryAssetMove",
    "create_gallery_folder",
    "delete_gallery_folder",
    "move_gallery_assets",
    "normalize_gallery_display_name",
    "normalize_gallery_folder_name",
    "rename_gallery_asset",
    "rename_gallery_folder",
    "stage_create_gallery_folder",
    "stage_move_gallery_assets",
    "stage_rename_gallery_asset",
    "stage_rename_gallery_folder",
]
