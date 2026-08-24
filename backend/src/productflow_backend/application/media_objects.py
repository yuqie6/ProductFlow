from __future__ import annotations

from dataclasses import dataclass
from hashlib import sha256
from io import BytesIO

from PIL import Image, UnidentifiedImageError
from sqlalchemy import select
from sqlalchemy.orm import Session

from productflow_backend.application.storage_compensation import StorageWriteCompensation
from productflow_backend.application.time import now_utc
from productflow_backend.domain.enums import MediaVerificationStatus
from productflow_backend.domain.errors import BusinessValidationError
from productflow_backend.infrastructure.db.models import (
    ImageSessionAsset,
    LocalImageEditTask,
    MediaLibraryAsset,
    MediaObject,
    ProductImageAsset,
    new_id,
)
from productflow_backend.infrastructure.storage import LocalStorage

_IMAGE_FORMAT_MIME_TYPES = {
    "PNG": "image/png",
    "JPEG": "image/jpeg",
    "WEBP": "image/webp",
}


@dataclass(frozen=True, slots=True)
class VerifiedImageMetadata:
    mime_type: str
    byte_size: int
    width: int
    height: int
    sha256: str


def inspect_image_bytes(content: bytes, *, expected_mime_type: str | None = None) -> VerifiedImageMetadata:
    if not content:
        raise BusinessValidationError("图片内容不能为空")
    try:
        with Image.open(BytesIO(content)) as image:
            image.verify()
        with Image.open(BytesIO(content)) as image:
            width, height = image.size
            mime_type = _IMAGE_FORMAT_MIME_TYPES.get(image.format or "")
    except (OSError, UnidentifiedImageError) as exc:
        raise BusinessValidationError("图片内容不是可解码的 PNG、JPEG 或 WEBP") from exc
    if mime_type is None:
        raise BusinessValidationError("图片格式仅支持 PNG、JPEG 或 WEBP")
    if width <= 0 or height <= 0:
        raise BusinessValidationError("图片尺寸无效")
    normalized_expected = expected_mime_type.split(";", maxsplit=1)[0].strip().lower() if expected_mime_type else None
    if normalized_expected and normalized_expected != mime_type:
        raise BusinessValidationError("图片内容格式与声明媒体类型不一致")
    return VerifiedImageMetadata(
        mime_type=mime_type,
        byte_size=len(content),
        width=width,
        height=height,
        sha256=sha256(content).hexdigest(),
    )


def stage_verified_media_object(
    session: Session,
    *,
    content: bytes,
    filename: str,
    expected_mime_type: str | None,
    storage: LocalStorage,
    storage_writes: StorageWriteCompensation,
) -> MediaObject:
    metadata = inspect_image_bytes(content, expected_mime_type=expected_mime_type)
    media_id = new_id()
    storage_path = storage_writes.track(
        storage,
        storage.save_media_image(media_id, filename, content),
    )
    media = MediaObject(
        id=media_id,
        storage_path=storage_path,
        mime_type=metadata.mime_type,
        byte_size=metadata.byte_size,
        width=metadata.width,
        height=metadata.height,
        sha256=metadata.sha256,
        verification_status=MediaVerificationStatus.VERIFIED,
        verified_at=now_utc(),
    )
    session.add(media)
    return media


def media_object_has_references(session: Session, media_object_id: str) -> bool:
    return any(
        session.scalar(select(model.id).where(column == media_object_id).limit(1)) is not None
        for model, column in (
            (ProductImageAsset, ProductImageAsset.media_object_id),
            (ImageSessionAsset, ImageSessionAsset.media_object_id),
            (MediaLibraryAsset, MediaLibraryAsset.media_object_id),
            (LocalImageEditTask, LocalImageEditTask.mask_media_object_id),
        )
    )


def prune_unreferenced_media_objects(
    session: Session,
    media_object_ids: set[str],
) -> list[tuple[str, str]]:
    """在调用方事务内删除已无逻辑引用的媒体行，并返回待清理文件。"""
    if not media_object_ids:
        return []
    session.flush()
    deleted: list[tuple[str, str]] = []
    for media_id in sorted(media_object_ids):
        media = session.scalar(select(MediaObject).where(MediaObject.id == media_id).with_for_update())
        if media is None or media_object_has_references(session, media_id):
            continue
        deleted.append((media.id, media.storage_path))
        session.delete(media)
    return deleted
