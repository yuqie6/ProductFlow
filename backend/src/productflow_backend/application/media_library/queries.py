"""全局图库列表与 bootstrap。

按 MediaLibraryAsset 身份分页；文件夹/标签/未整理是组织投影，provenance 不进入列表载荷。
"""

from __future__ import annotations

import base64
import binascii
import hashlib
import json
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Any

from sqlalchemy import and_, func, or_, select
from sqlalchemy.orm import Session, defer, selectinload

from productflow_backend.application.media_library.contracts import MediaLibrarySourceType
from productflow_backend.domain.errors import BusinessValidationError, NotFoundError
from productflow_backend.infrastructure.db.models import (
    MediaLibraryAsset,
    MediaLibraryAssetTag,
    MediaLibraryFolder,
    MediaLibraryTag,
)


def _asset_query():
    return select(MediaLibraryAsset).options(
        defer(MediaLibraryAsset.provenance_json),
        selectinload(MediaLibraryAsset.media_object),
        selectinload(MediaLibraryAsset.source_image_session_asset),
        selectinload(MediaLibraryAsset.source_product_asset),
        selectinload(MediaLibraryAsset.folder),
        selectinload(MediaLibraryAsset.tag_assignments).selectinload(MediaLibraryAssetTag.tag),
    )


@dataclass(frozen=True, slots=True)
class MediaLibraryAssetPage:
    items: tuple[MediaLibraryAsset, ...]
    next_cursor: str | None


@dataclass(frozen=True, slots=True)
class MediaLibraryBootstrap:
    total_count: int
    active_count: int
    archived_count: int
    unorganized_count: int
    folders: tuple[tuple[str, str, int], ...]
    tags: tuple[tuple[str, str, int], ...]


def get_media_library_asset(session: Session, *, asset_id: str) -> MediaLibraryAsset:
    """按全局素材 id 读取一条资产及其 MediaObject。"""

    asset = session.scalar(_asset_query().where(MediaLibraryAsset.id == asset_id))
    if asset is None:
        raise NotFoundError("素材库资产不存在")
    return asset


_CURSOR_VERSION = 2


def _normalize_search(search: str | None) -> str:
    return " ".join((search or "").split())


def _cursor_filter_signature(
    *,
    search: str,
    source_type: MediaLibrarySourceType | None,
    include_archived: bool,
    folder_id: str | None,
    tag: str | None,
) -> str:
    payload = json.dumps(
        {
            "folder_id": folder_id or "",
            "include_archived": include_archived,
            "search": search,
            "source_type": source_type or "",
            "tag": tag or "",
        },
        sort_keys=True,
        separators=(",", ":"),
    )
    return hashlib.sha256(payload.encode("utf-8")).hexdigest()


def _coerce_utc(value: datetime) -> datetime:
    if value.tzinfo is None:
        return value.replace(tzinfo=UTC)
    return value.astimezone(UTC)


def _encode_cursor(*, created_at: datetime, asset_id: str, signature: str, as_of: datetime) -> str:
    payload = {
        "v": _CURSOR_VERSION,
        "sort": "created_desc",
        "filter": signature,
        "key": _coerce_utc(created_at).isoformat(),
        "id": asset_id,
        "as_of": _coerce_utc(as_of).isoformat(),
    }
    encoded = json.dumps(payload, sort_keys=True, separators=(",", ":")).encode()
    return base64.urlsafe_b64encode(encoded).decode().rstrip("=")


def _decode_cursor(cursor: str, *, signature: str) -> tuple[datetime, str, datetime]:
    try:
        padded = cursor + "=" * (-len(cursor) % 4)
        payload: Any = json.loads(base64.urlsafe_b64decode(padded.encode("ascii")))
        if (
            not isinstance(payload, dict)
            or payload.get("v") != _CURSOR_VERSION
            or payload.get("sort") != "created_desc"
            or payload.get("filter") != signature
            or not all(isinstance(payload.get(key), str) and payload[key] for key in ("key", "id", "as_of"))
        ):
            raise ValueError
        created_at = datetime.fromisoformat(payload["key"])
        as_of = datetime.fromisoformat(payload["as_of"])
        if created_at.tzinfo is None:
            created_at = created_at.replace(tzinfo=UTC)
        if as_of.tzinfo is None:
            as_of = as_of.replace(tzinfo=UTC)
        return created_at, payload["id"], as_of
    except (ValueError, TypeError, UnicodeDecodeError, json.JSONDecodeError, binascii.Error) as exc:
        raise BusinessValidationError("素材库分页游标无效或与当前筛选条件不匹配") from exc


def _filtered_asset_statement(
    *,
    search: str,
    source_type: MediaLibrarySourceType | None,
    include_archived: bool,
    folder_id: str | None,
    tag: str | None,
):
    statement = select(MediaLibraryAsset)
    if not include_archived:
        statement = statement.where(MediaLibraryAsset.is_archived.is_(False))
    if search:
        pattern = f"%{search}%"
        statement = statement.where(
            or_(MediaLibraryAsset.display_name.ilike(pattern), MediaLibraryAsset.original_filename.ilike(pattern))
        )
    if source_type:
        statement = statement.where(MediaLibraryAsset.source_type == source_type)
    if folder_id:
        statement = statement.where(MediaLibraryAsset.folder_id == folder_id)
    if tag:
        statement = statement.join(MediaLibraryAssetTag, MediaLibraryAssetTag.asset_id == MediaLibraryAsset.id).join(
            MediaLibraryTag, MediaLibraryTag.id == MediaLibraryAssetTag.tag_id
        ).where(MediaLibraryTag.normalized_name == tag)
    return statement


def get_media_library_bootstrap(session: Session) -> MediaLibraryBootstrap:
    """活跃/归档/未整理与一层文件夹计数，都是查询投影。"""

    total_count = session.scalar(select(func.count(MediaLibraryAsset.id))) or 0
    active_count = (
        session.scalar(
            select(func.count(MediaLibraryAsset.id)).where(MediaLibraryAsset.is_archived.is_(False))
        )
        or 0
    )
    archived_count = total_count - active_count
    unorganized_count = session.scalar(
        select(func.count(MediaLibraryAsset.id)).where(
            MediaLibraryAsset.is_archived.is_(False), MediaLibraryAsset.folder_id.is_(None)
        )
    ) or 0
    folder_rows = session.execute(
        select(MediaLibraryFolder.id, MediaLibraryFolder.name, func.count(MediaLibraryAsset.id))
        .outerjoin(
            MediaLibraryAsset,
            (MediaLibraryAsset.folder_id == MediaLibraryFolder.id)
            & MediaLibraryAsset.is_archived.is_(False),
        )
        .group_by(MediaLibraryFolder.id)
        .order_by(MediaLibraryFolder.name, MediaLibraryFolder.id)
    ).all()
    tag_rows = session.execute(
        select(MediaLibraryTag.id, MediaLibraryTag.name, func.count(MediaLibraryAsset.id))
        .outerjoin(MediaLibraryAssetTag, MediaLibraryAssetTag.tag_id == MediaLibraryTag.id)
        .outerjoin(
            MediaLibraryAsset,
            (MediaLibraryAsset.id == MediaLibraryAssetTag.asset_id)
            & MediaLibraryAsset.is_archived.is_(False),
        )
        .group_by(MediaLibraryTag.id)
        .order_by(MediaLibraryTag.name, MediaLibraryTag.id)
    ).all()
    return MediaLibraryBootstrap(
        total_count=total_count,
        active_count=active_count,
        archived_count=archived_count,
        unorganized_count=unorganized_count,
        folders=tuple(folder_rows),
        tags=tuple(tag_rows),
    )


def list_media_library_assets(
    session: Session,
    *,
    limit: int = 20,
    cursor: str | None = None,
    include_archived: bool = False,
    search: str | None = None,
    source_type: MediaLibrarySourceType | None = None,
    folder_id: str | None = None,
    tag: str | None = None,
) -> MediaLibraryAssetPage:
    """按全局素材身份分页；文件夹/标签只是筛选投影。"""

    bounded_limit = min(max(limit, 1), 100)
    normalized_search = _normalize_search(search)
    normalized_tag = tag.casefold().strip() if tag else None
    signature = _cursor_filter_signature(
        search=normalized_search,
        source_type=source_type,
        include_archived=include_archived,
        folder_id=folder_id.strip() if folder_id else None,
        tag=normalized_tag,
    )
    as_of = datetime.now(UTC)
    if cursor:
        cursor_created_at, cursor_id, as_of = _decode_cursor(cursor, signature=signature)
    else:
        cursor_created_at = cursor_id = None
    statement = _filtered_asset_statement(
        search=normalized_search,
        source_type=source_type,
        include_archived=include_archived,
        folder_id=folder_id.strip() if folder_id else None,
        tag=normalized_tag,
    ).options(
        defer(MediaLibraryAsset.provenance_json),
        selectinload(MediaLibraryAsset.media_object),
        selectinload(MediaLibraryAsset.source_image_session_asset),
        selectinload(MediaLibraryAsset.source_product_asset),
        selectinload(MediaLibraryAsset.folder),
        selectinload(MediaLibraryAsset.tag_assignments).selectinload(MediaLibraryAssetTag.tag),
    ).order_by(MediaLibraryAsset.created_at.desc(), MediaLibraryAsset.id.desc()).limit(bounded_limit + 1)
    if cursor_created_at is not None:
        statement = statement.where(
            or_(
                MediaLibraryAsset.created_at < cursor_created_at,
                and_(MediaLibraryAsset.created_at == cursor_created_at, MediaLibraryAsset.id < cursor_id),
            )
        )
    rows = list(session.scalars(statement).unique().all())
    has_more = len(rows) > bounded_limit
    items = tuple(rows[:bounded_limit])
    next_cursor = (
        _encode_cursor(created_at=items[-1].created_at, asset_id=items[-1].id, signature=signature, as_of=as_of)
        if has_more and items
        else None
    )
    return MediaLibraryAssetPage(items=items, next_cursor=next_cursor)
