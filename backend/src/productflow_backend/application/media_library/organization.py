"""全局图库一层文件夹与标签。

删除文件夹只解除组织关系，不删素材或媒体文件。标签是分类元数据，不拥有 bytes。
"""

from __future__ import annotations

import json
import re
from dataclasses import dataclass
from typing import Literal

from sqlalchemy import select
from sqlalchemy.exc import IntegrityError
from sqlalchemy.orm import Session, selectinload

from productflow_backend.application.time import now_utc
from productflow_backend.domain.errors import BusinessValidationError, ConflictError, NotFoundError
from productflow_backend.infrastructure.db.models import (
    MediaLibraryAsset,
    MediaLibraryAssetTag,
    MediaLibraryFolder,
    MediaLibraryTag,
)

MAX_ORGANIZATION_ASSETS = 100
MAX_ORGANIZATION_REQUEST_BYTES = 256 * 1024
MAX_FOLDER_NAME_LENGTH = 120
MAX_TAG_NAME_LENGTH = 80
_WHITESPACE = re.compile(r"\s+")


MediaLibraryNameKind = Literal["folder", "tag", "文件夹", "标签"]


def normalize_media_library_name(value: str, *, kind: MediaLibraryNameKind) -> str:
    normalized = _WHITESPACE.sub(" ", value.strip())
    is_folder = kind in {"folder", "文件夹"}
    label = "文件夹" if is_folder else "标签"
    maximum = MAX_FOLDER_NAME_LENGTH if is_folder else MAX_TAG_NAME_LENGTH
    if not normalized:
        raise BusinessValidationError(f"{label}名称不能为空")
    if len(normalized) > maximum:
        raise BusinessValidationError(f"{label}名称不能超过 {maximum} 个字符")
    return normalized


def normalize_media_library_key(value: str, *, kind: MediaLibraryNameKind) -> str:
    return normalize_media_library_name(value, kind=kind).casefold()


@dataclass(frozen=True, slots=True)
class MediaLibraryFolderMutation:
    folder: MediaLibraryFolder
    created: bool


@dataclass(frozen=True, slots=True)
class MediaLibraryTagMutation:
    tag: MediaLibraryTag
    created: bool


def create_media_library_folder(session: Session, *, name: str) -> MediaLibraryFolderMutation:
    display_name = normalize_media_library_name(name, kind="folder")
    normalized_name = normalize_media_library_key(display_name, kind="folder")
    existing = session.scalar(
        select(MediaLibraryFolder).where(MediaLibraryFolder.normalized_name == normalized_name)
    )
    if existing is not None:
        return MediaLibraryFolderMutation(folder=existing, created=False)
    folder = MediaLibraryFolder(name=display_name, normalized_name=normalized_name)
    session.add(folder)
    try:
        session.commit()
    except IntegrityError:
        session.rollback()
        existing = session.scalar(
            select(MediaLibraryFolder).where(MediaLibraryFolder.normalized_name == normalized_name)
        )
        if existing is None:
            raise
        return MediaLibraryFolderMutation(folder=existing, created=False)
    session.refresh(folder)
    return MediaLibraryFolderMutation(folder=folder, created=True)


def rename_media_library_folder(
    session: Session, *, folder_id: str, expected_name: str, name: str
) -> MediaLibraryFolder:
    folder = _get_folder_for_update(session, folder_id)
    if normalize_media_library_name(expected_name, kind="folder") != folder.name:
        raise ConflictError("文件夹名称已被其他操作修改")
    display_name = normalize_media_library_name(name, kind="folder")
    normalized_name = normalize_media_library_key(display_name, kind="folder")
    if folder.name != display_name:
        folder.name = display_name
        folder.normalized_name = normalized_name
        folder.updated_at = now_utc()
    session.commit()
    session.refresh(folder)
    return folder


def delete_media_library_folder(session: Session, *, folder_id: str) -> int:
    """删除文件夹并把素材 folder_id 置空；不删 MediaLibraryAsset。"""

    folder = _get_folder_for_update(session, folder_id)
    # 只解除组织；节点/封面/lineage 引用走商品图片身份，不受本删除影响。
    moved_count = session.query(MediaLibraryAsset).filter(MediaLibraryAsset.folder_id == folder.id).count()
    session.query(MediaLibraryAsset).filter(MediaLibraryAsset.folder_id == folder.id).update(
        {MediaLibraryAsset.folder_id: None, MediaLibraryAsset.updated_at: now_utc()},
        synchronize_session=False,
    )
    session.delete(folder)
    session.commit()
    return moved_count


def create_media_library_tag(session: Session, *, name: str) -> MediaLibraryTagMutation:
    display_name = normalize_media_library_name(name, kind="tag")
    normalized_name = normalize_media_library_key(display_name, kind="tag")
    existing = session.scalar(select(MediaLibraryTag).where(MediaLibraryTag.normalized_name == normalized_name))
    if existing is not None:
        return MediaLibraryTagMutation(tag=existing, created=False)
    tag = MediaLibraryTag(name=display_name, normalized_name=normalized_name)
    session.add(tag)
    try:
        session.commit()
    except IntegrityError:
        session.rollback()
        existing = session.scalar(select(MediaLibraryTag).where(MediaLibraryTag.normalized_name == normalized_name))
        if existing is None:
            raise
        return MediaLibraryTagMutation(tag=existing, created=False)
    session.refresh(tag)
    return MediaLibraryTagMutation(tag=tag, created=True)


def rename_media_library_tag(
    session: Session, *, tag_id: str, expected_name: str, name: str
) -> MediaLibraryTag:
    tag = _get_tag_for_update(session, tag_id)
    if normalize_media_library_name(expected_name, kind="tag") != tag.name:
        raise ConflictError("标签名称已被其他操作修改")
    display_name = normalize_media_library_name(name, kind="tag")
    normalized_name = normalize_media_library_key(display_name, kind="tag")
    tag.name = display_name
    tag.normalized_name = normalized_name
    tag.updated_at = now_utc()
    session.commit()
    session.refresh(tag)
    return tag


def delete_media_library_tag(session: Session, *, tag_id: str) -> int:
    """删除标签 assignment，不删素材。"""

    tag = _get_tag_for_update(session, tag_id)
    count = session.scalar(select(MediaLibraryAssetTag.asset_id).where(MediaLibraryAssetTag.tag_id == tag.id).limit(1))
    deleted_count = session.query(MediaLibraryAssetTag).filter(MediaLibraryAssetTag.tag_id == tag.id).delete(
        synchronize_session=False
    )
    session.delete(tag)
    session.commit()
    return deleted_count if count is not None else 0


def move_media_library_assets(
    session: Session, *, asset_ids: list[str], folder_id: str | None, expected_revision: dict[str, int]
) -> list[MediaLibraryAsset]:
    """只改文件夹组织；不改素材身份或媒体 bytes。"""

    normalized_ids = _validate_asset_ids(session, asset_ids)
    if set(expected_revision) != set(normalized_ids):
        raise BusinessValidationError("expected_revision 必须覆盖全部素材")
    target_folder = None
    if folder_id is not None:
        target_folder = _get_folder_for_update(session, folder_id)
    assets = _lock_assets(session, normalized_ids)
    for asset in assets:
        _check_expected_revision(asset, expected_revision)
        asset.folder_id = target_folder.id if target_folder else None
        asset.revision += 1
        asset.updated_at = now_utc()
    session.commit()
    return _reload_assets(session, normalized_ids)


def set_media_library_asset_tags(
    session: Session, *, asset_ids: list[str], tag_names: list[str], expected_revision: dict[str, int]
) -> list[MediaLibraryAsset]:
    normalized_ids = _validate_asset_ids(session, asset_ids)
    if set(expected_revision) != set(normalized_ids):
        raise BusinessValidationError("expected_revision 必须覆盖全部素材")
    normalized_names = list(dict.fromkeys(normalize_media_library_key(name, kind="tag") for name in tag_names))
    display_names = {}
    for name in tag_names:
        display = normalize_media_library_name(name, kind="tag")
        display_names.setdefault(normalize_media_library_key(display, kind="tag"), display)
    tags = list(
        session.scalars(
            select(MediaLibraryTag).where(MediaLibraryTag.normalized_name.in_(normalized_names))
        ).all()
    )
    tags_by_key = {tag.normalized_name: tag for tag in tags}
    for key in normalized_names:
        if key not in tags_by_key:
            tag = MediaLibraryTag(name=display_names[key], normalized_name=key)
            session.add(tag)
            session.flush()
            tags_by_key[key] = tag
    assets = _lock_assets(session, normalized_ids)
    for asset in assets:
        _check_expected_revision(asset, expected_revision)
        asset.tag_assignments.clear()
        asset.tag_assignments.extend(MediaLibraryAssetTag(tag=tags_by_key[key]) for key in normalized_names)
        asset.revision += 1
        asset.updated_at = now_utc()
    session.commit()
    return _reload_assets(session, normalized_ids)


def _validate_asset_ids(session: Session, asset_ids: list[str]) -> list[str]:
    if not 1 <= len(asset_ids) <= MAX_ORGANIZATION_ASSETS:
        raise BusinessValidationError(f"单次最多整理 {MAX_ORGANIZATION_ASSETS} 个素材")
    if len(set(asset_ids)) != len(asset_ids):
        raise BusinessValidationError("整理请求包含重复素材 ID")
    request_bytes = len(json.dumps(asset_ids, ensure_ascii=False, separators=(",", ":")).encode("utf-8"))
    if request_bytes > MAX_ORGANIZATION_REQUEST_BYTES:
        raise BusinessValidationError("素材整理请求过大")
    return asset_ids


def _lock_assets(session: Session, asset_ids: list[str]) -> list[MediaLibraryAsset]:
    assets = list(
        session.scalars(
            select(MediaLibraryAsset)
            .where(MediaLibraryAsset.id.in_(asset_ids))
            .order_by(MediaLibraryAsset.id)
            .options(selectinload(MediaLibraryAsset.tag_assignments))
            .with_for_update()
        ).all()
    )
    if len(assets) != len(asset_ids):
        raise NotFoundError("素材库资产不存在")
    return assets


def _reload_assets(session: Session, asset_ids: list[str]) -> list[MediaLibraryAsset]:
    rows = list(
        session.scalars(
            select(MediaLibraryAsset)
            .where(MediaLibraryAsset.id.in_(asset_ids))
            .order_by(MediaLibraryAsset.id)
            .options(selectinload(MediaLibraryAsset.tag_assignments).selectinload(MediaLibraryAssetTag.tag))
        ).all()
    )
    by_id = {row.id: row for row in rows}
    return [by_id[asset_id] for asset_id in asset_ids]


def _check_expected_revision(asset: MediaLibraryAsset, expected_revision: dict[str, int]) -> None:
    expected = expected_revision.get(asset.id)
    if expected is None or expected != asset.revision:
        raise ConflictError("素材库资产 revision 已变化")


def _get_folder_for_update(session: Session, folder_id: str) -> MediaLibraryFolder:
    folder = session.scalar(select(MediaLibraryFolder).where(MediaLibraryFolder.id == folder_id).with_for_update())
    if folder is None:
        raise NotFoundError("素材库文件夹不存在")
    return folder


def _get_tag_for_update(session: Session, tag_id: str) -> MediaLibraryTag:
    tag = session.scalar(select(MediaLibraryTag).where(MediaLibraryTag.id == tag_id).with_for_update())
    if tag is None:
        raise NotFoundError("素材库标签不存在")
    return tag
