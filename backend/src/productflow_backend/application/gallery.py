from __future__ import annotations

from dataclasses import dataclass

from sqlalchemy import desc, select
from sqlalchemy.exc import IntegrityError
from sqlalchemy.orm import Session, selectinload

from productflow_backend.domain.enums import ImageSessionAssetKind
from productflow_backend.domain.errors import BusinessValidationError, NotFoundError
from productflow_backend.infrastructure.db.models import (
    ImageGalleryEntry,
    ImageSession,
    ImageSessionAsset,
    ImageSessionRound,
)


@dataclass(frozen=True, slots=True)
class GallerySaveResult:
    entry: ImageGalleryEntry
    created: bool


def _gallery_entry_query():
    return (
        select(ImageGalleryEntry)
        .options(
            selectinload(ImageGalleryEntry.asset)
            .selectinload(ImageSessionAsset.session)
            .selectinload(ImageSession.product),
            selectinload(ImageGalleryEntry.round),
        )
        .order_by(desc(ImageGalleryEntry.created_at))
    )


def list_gallery_entries(session: Session) -> list[ImageGalleryEntry]:
    return list(session.scalars(_gallery_entry_query()).all())


def _get_gallery_entry_by_asset_id(session: Session, image_session_asset_id: str) -> ImageGalleryEntry | None:
    return session.scalar(
        _gallery_entry_query().where(ImageGalleryEntry.image_session_asset_id == image_session_asset_id)
    )


def save_generated_asset_to_gallery(session: Session, *, image_session_asset_id: str) -> GallerySaveResult:
    existing = _get_gallery_entry_by_asset_id(session, image_session_asset_id)
    if existing is not None:
        return GallerySaveResult(entry=existing, created=False)

    asset = session.scalar(
        select(ImageSessionAsset)
        .options(selectinload(ImageSessionAsset.session).selectinload(ImageSession.product))
        .where(ImageSessionAsset.id == image_session_asset_id)
    )
    if asset is None:
        raise NotFoundError("会话图片不存在")
    if asset.kind != ImageSessionAssetKind.GENERATED_IMAGE:
        raise BusinessValidationError("只有生成结果可以保存到画廊")

    round_item = session.scalar(select(ImageSessionRound).where(ImageSessionRound.generated_asset_id == asset.id))
    if round_item is None:
        raise NotFoundError("生成记录不存在")

    entry = ImageGalleryEntry(
        image_session_asset_id=asset.id,
        image_session_round_id=round_item.id,
    )
    session.add(entry)
    try:
        session.flush()
        entry_id = entry.id
        session.commit()
    except IntegrityError:
        session.rollback()
        existing = _get_gallery_entry_by_asset_id(session, image_session_asset_id)
        if existing is not None:
            return GallerySaveResult(entry=existing, created=False)
        raise
    session.expire_all()
    return GallerySaveResult(
        entry=session.scalar(_gallery_entry_query().where(ImageGalleryEntry.id == entry_id)) or entry,
        created=True,
    )
