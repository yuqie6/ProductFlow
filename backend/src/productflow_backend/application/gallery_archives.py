from __future__ import annotations

import re
import tempfile
import zipfile
from dataclasses import dataclass
from pathlib import Path

from sqlalchemy import select
from sqlalchemy.orm import Session, selectinload

from productflow_backend.domain.enums import MediaVerificationStatus
from productflow_backend.domain.errors import BusinessValidationError, NotFoundError
from productflow_backend.infrastructure.db.models import Product, ProductImageAsset
from productflow_backend.infrastructure.storage import LocalStorage

GALLERY_ARCHIVE_MAX_ASSETS = 100
GALLERY_ARCHIVE_MAX_BYTES = 512 * 1024 * 1024

_MIME_EXTENSIONS = {
    "image/png": ".png",
    "image/jpeg": ".jpg",
    "image/webp": ".webp",
}
_INVALID_FILENAME_CHARS = re.compile(r"[\\/:*?\"<>|\x00-\x1f]")


@dataclass(frozen=True, slots=True)
class GalleryArchive:
    path: Path
    filename: str


def build_gallery_archive(
    session: Session,
    *,
    product_id: str,
    asset_ids: list[str],
    storage: LocalStorage | None = None,
) -> GalleryArchive:
    normalized_ids = _normalize_archive_ids(asset_ids)
    product = session.get(Product, product_id)
    if product is None:
        raise NotFoundError("商品不存在")
    assets = list(
        session.scalars(
            select(ProductImageAsset)
            .options(selectinload(ProductImageAsset.media_object))
            .where(
                ProductImageAsset.product_id == product_id,
                ProductImageAsset.id.in_(normalized_ids),
            )
        )
    )
    assets_by_id = {asset.id: asset for asset in assets}
    if len(assets_by_id) != len(normalized_ids):
        raise NotFoundError("商品图片不存在")
    ordered_assets = [assets_by_id[asset_id] for asset_id in normalized_ids]
    if any(
        asset.media_object.verification_status != MediaVerificationStatus.VERIFIED
        for asset in ordered_assets
    ):
        raise BusinessValidationError("只有已核验图片可以批量下载")
    total_bytes = sum(asset.media_object.byte_size or 0 for asset in ordered_assets)
    if total_bytes > GALLERY_ARCHIVE_MAX_BYTES:
        raise BusinessValidationError("批量下载图片总大小不能超过 512 MiB")

    storage = storage or LocalStorage()
    resolved: list[tuple[ProductImageAsset, Path]] = []
    for asset in ordered_assets:
        path = storage.resolve(asset.media_object.storage_path)
        if not path.is_file():
            raise NotFoundError("商品图片文件不存在")
        resolved.append((asset, path))

    temporary = tempfile.NamedTemporaryFile(prefix="productflow-gallery-", suffix=".zip", delete=False)
    archive_path = Path(temporary.name)
    temporary.close()
    try:
        names: set[str] = set()
        with zipfile.ZipFile(archive_path, mode="w", compression=zipfile.ZIP_DEFLATED) as archive:
            for asset, source_path in resolved:
                entry_name = _archive_entry_name(asset, names=names)
                archive.write(source_path, arcname=entry_name)
        return GalleryArchive(
            path=archive_path,
            filename=f"{_clean_filename(product.name, fallback='product')}-images.zip",
        )
    except BaseException:
        archive_path.unlink(missing_ok=True)
        raise


def cleanup_gallery_archive(archive: GalleryArchive) -> None:
    archive.path.unlink(missing_ok=True)


def _normalize_archive_ids(asset_ids: list[str]) -> list[str]:
    if not 1 <= len(asset_ids) <= GALLERY_ARCHIVE_MAX_ASSETS:
        raise BusinessValidationError(
            f"批量下载必须选择 1 到 {GALLERY_ARCHIVE_MAX_ASSETS} 张图片"
        )
    normalized = [asset_id.strip() for asset_id in asset_ids]
    if any(not asset_id for asset_id in normalized):
        raise BusinessValidationError("图片 ID 不能为空")
    if len(set(normalized)) != len(normalized):
        raise BusinessValidationError("批量下载不能包含重复图片 ID")
    return normalized


def _archive_entry_name(asset: ProductImageAsset, *, names: set[str]) -> str:
    extension = _MIME_EXTENSIONS.get(asset.media_object.mime_type)
    if extension is None:
        raise BusinessValidationError("批量下载包含不支持的图片格式")
    base = _clean_filename(asset.display_name, fallback="image")
    if base.lower().endswith(extension):
        base = base[: -len(extension)] or "image"
    candidate = f"{base}{extension}"
    if candidate.casefold() in names:
        candidate = f"{base}-{asset.id[:8]}{extension}"
    while candidate.casefold() in names:
        candidate = f"{base}-{asset.id}{extension}"
    names.add(candidate.casefold())
    return candidate


def _clean_filename(value: str, *, fallback: str) -> str:
    cleaned = _INVALID_FILENAME_CHARS.sub("_", value).strip().strip(".")
    cleaned = re.sub(r"\s+", " ", cleaned)
    return (cleaned or fallback)[:180]


__all__ = [
    "GALLERY_ARCHIVE_MAX_ASSETS",
    "GALLERY_ARCHIVE_MAX_BYTES",
    "GalleryArchive",
    "build_gallery_archive",
    "cleanup_gallery_archive",
]
