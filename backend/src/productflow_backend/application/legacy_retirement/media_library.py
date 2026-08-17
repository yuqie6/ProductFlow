from __future__ import annotations

from collections.abc import Sequence
from dataclasses import dataclass
from datetime import datetime

import sqlalchemy as sa
from sqlalchemy.orm import Session, selectinload

from productflow_backend.infrastructure.db.models import ImageSessionAsset, ImageSessionRound

LEGACY_GALLERY_ENTRIES = sa.Table(
    "image_gallery_entries",
    sa.MetaData(),
    sa.Column("id", sa.String(36), nullable=False),
    sa.Column("image_session_asset_id", sa.String(36), nullable=False),
    sa.Column("image_session_round_id", sa.String(36), nullable=True),
    sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
)


@dataclass(frozen=True, slots=True)
class LegacyGalleryEntry:
    id: str
    image_session_asset_id: str | None
    image_session_round_id: str | None
    created_at: datetime | None
    asset: ImageSessionAsset | None
    round: ImageSessionRound | None


def load_legacy_gallery_entries(
    session: Session,
    *,
    order: str = "id",
    limit: int | None = None,
    offset: int = 0,
    ids: Sequence[str] | None = None,
) -> list[LegacyGalleryEntry]:
    if limit is not None and limit < 0:
        raise ValueError("旧 Gallery 读取 limit 无效")
    if offset < 0:
        raise ValueError("旧 Gallery 读取 offset 无效")
    if order not in {"id", "created"}:
        raise ValueError("旧 Gallery 读取 order 无效")

    statement = sa.select(LEGACY_GALLERY_ENTRIES)
    if ids is not None:
        if not ids:
            return []
        statement = statement.where(LEGACY_GALLERY_ENTRIES.c.id.in_(ids))
    if order == "created":
        statement = statement.order_by(
            LEGACY_GALLERY_ENTRIES.c.created_at.asc(),
            LEGACY_GALLERY_ENTRIES.c.id.asc(),
        )
    else:
        statement = statement.order_by(LEGACY_GALLERY_ENTRIES.c.id.asc())
    if limit is not None:
        statement = statement.limit(limit)
    if offset:
        statement = statement.offset(offset)

    rows = list(session.execute(statement).mappings())
    asset_ids = [str(row["image_session_asset_id"]) for row in rows if row["image_session_asset_id"] is not None]
    round_ids = [str(row["image_session_round_id"]) for row in rows if row["image_session_round_id"] is not None]
    assets = (
        session.scalars(
            sa.select(ImageSessionAsset)
            .options(
                selectinload(ImageSessionAsset.session),
                selectinload(ImageSessionAsset.media_object),
            )
            .where(ImageSessionAsset.id.in_(asset_ids))
        ).all()
        if asset_ids
        else []
    )
    rounds = (
        session.scalars(sa.select(ImageSessionRound).where(ImageSessionRound.id.in_(round_ids))).all()
        if round_ids
        else []
    )
    assets_by_id = {asset.id: asset for asset in assets}
    rounds_by_id = {round_item.id: round_item for round_item in rounds}
    return [
        LegacyGalleryEntry(
            id=str(row["id"]),
            image_session_asset_id=(
                str(row["image_session_asset_id"]) if row["image_session_asset_id"] is not None else None
            ),
            image_session_round_id=(
                str(row["image_session_round_id"]) if row["image_session_round_id"] is not None else None
            ),
            created_at=row["created_at"],
            asset=assets_by_id.get(str(row["image_session_asset_id"]))
            if row["image_session_asset_id"] is not None
            else None,
            round=rounds_by_id.get(str(row["image_session_round_id"]))
            if row["image_session_round_id"] is not None
            else None,
        )
        for row in rows
    ]
