from __future__ import annotations

import json
import logging
from dataclasses import dataclass, replace
from hashlib import sha256

from sqlalchemy import select
from sqlalchemy.orm import Session

from productflow_backend.application.legacy_retirement.media_library import (
    LegacyGalleryEntry,
    load_legacy_gallery_entries,
)
from productflow_backend.application.media_library.contracts import (
    canonical_provenance_hash,
    parse_provenance_v1,
)
from productflow_backend.application.media_library.migration_audit import MediaLibraryMigrationAudit
from productflow_backend.application.time import now_utc
from productflow_backend.domain.enums import MediaVerificationStatus
from productflow_backend.infrastructure.db.models import (
    ImageSessionAsset,
    MediaLibraryAsset,
    MediaObject,
)
from productflow_backend.infrastructure.storage import LocalStorage

logger = logging.getLogger(__name__)


@dataclass(frozen=True, slots=True)
class GalleryBackfillBlocker:
    entry_id: str
    code: str


@dataclass(frozen=True, slots=True)
class GalleryBackfillSummary:
    scanned: int = 0
    created: int = 0
    skipped_existing: int = 0
    blocked: int = 0
    blockers: tuple[GalleryBackfillBlocker, ...] = ()


def capture_gallery_snapshot(session: Session) -> MediaLibraryMigrationAudit:
    entries = load_legacy_gallery_entries(session)
    source_rows = tuple(
        (
            entry.id,
            entry.asset.id if entry.asset is not None else None,
            entry.asset.media_object.id if entry.asset is not None and entry.asset.media_object is not None else None,
        )
        for entry in entries
    )
    asset_ids = list(
        session.scalars(
            select(ImageSessionAsset.id).order_by(ImageSessionAsset.id)
        ).all()
    )
    canonical_source = json.dumps(
        {"gallery": source_rows, "session_assets": asset_ids},
        ensure_ascii=False,
        separators=(",", ":"),
    ).encode("utf-8")
    source_hash = sha256(canonical_source).hexdigest()
    return MediaLibraryMigrationAudit(
        snapshot_token=source_hash,
        gallery_count=len(entries),
        session_asset_count=len(asset_ids),
        captured_at=now_utc(),
        source_hash=source_hash,
        source_rows=source_rows,
    )


def _gallery_source(entry: LegacyGalleryEntry) -> tuple[ImageSessionAsset, MediaObject]:
    asset = entry.asset
    if asset is None:
        raise RuntimeError(f"gallery entry {entry.id} has no source image session asset")
    media = asset.media_object
    if media is None:
        raise RuntimeError(f"gallery entry {entry.id} has no MediaObject")
    if media.verification_status != MediaVerificationStatus.VERIFIED:
        raise RuntimeError(f"gallery entry {entry.id} source media is not verified")
    if (
        media.sha256 is None
        or media.byte_size is None
        or media.width is None
        or media.height is None
        or media.byte_size <= 0
        or media.width <= 0
        or media.height <= 0
    ):
        raise RuntimeError(f"gallery entry {entry.id} source media has incomplete verification metadata")
    return asset, media


def _build_gallery_provenance(
    *,
    entry: LegacyGalleryEntry,
    media: MediaObject,
) -> dict[str, object]:
    if entry.created_at is None:
        raise RuntimeError(f"gallery entry {entry.id} has no created_at")
    return {
        "schema_version": 1,
        "source_type": "legacy_gallery",
        "source_id": entry.id,
        "sha256": media.sha256,
        "mime_type": media.mime_type,
        "byte_size": media.byte_size,
        "width": media.width,
        "height": media.height,
        "original_filename": entry.asset.original_filename,
        "captured_at": entry.created_at.isoformat(),
    }


def _append_blocker(
    summary: GalleryBackfillSummary,
    *,
    entry_id: str,
    code: str,
) -> GalleryBackfillSummary:
    return replace(
        summary,
        blocked=summary.blocked + 1,
        blockers=(*summary.blockers, GalleryBackfillBlocker(entry_id=entry_id, code=code)),
    )


def backfill_gallery_entry(
    session: Session,
    *,
    entry: LegacyGalleryEntry,
    storage: LocalStorage,
) -> bool:
    existing = session.scalar(
        select(MediaLibraryAsset).where(
            MediaLibraryAsset.source_type == "legacy_gallery",
            MediaLibraryAsset.source_id == entry.id,
        )
    )
    if existing is not None:
        return False
    asset, media = _gallery_source(entry)
    path = storage.resolve(media.storage_path)
    if not path.is_file():
        raise FileNotFoundError(f"media file missing for gallery entry {entry.id}")
    provenance = _build_gallery_provenance(entry=entry, media=media)
    library_asset = MediaLibraryAsset(
        id=entry.id,
        media_object_id=media.id,
        source_type="legacy_gallery",
        source_id=entry.id,
        source_image_session_asset_id=asset.id,
        provenance_json=provenance,
        provenance_hash=canonical_provenance_hash(parse_provenance_v1(provenance).model_dump(mode="json")),
        display_name=asset.original_filename,
        original_filename=asset.original_filename,
    )
    session.add(library_asset)
    session.flush()
    return True


def run_gallery_backfill(
    session: Session,
    *,
    storage: LocalStorage,
    limit: int = 100,
    offset: int = 0,
    apply: bool = False,
    snapshot: MediaLibraryMigrationAudit | None = None,
) -> GalleryBackfillSummary:
    if limit < 1 or offset < 0:
        raise ValueError("回填 limit/offset 无效")
    if snapshot is not None:
        current = capture_gallery_snapshot(session)
        if (
            current.gallery_count != snapshot.gallery_count
            or current.session_asset_count != snapshot.session_asset_count
            or current.source_hash != snapshot.source_hash
            or current.source_rows != snapshot.source_rows
        ):
            raise RuntimeError("media library backfill source changed during migration")
        snapshot_ids = [row[0] for row in snapshot.source_rows]
        page_ids = snapshot_ids[offset : offset + limit]
        if not page_ids:
            entries = []
        else:
            loaded = load_legacy_gallery_entries(session, ids=page_ids)
            entries_by_id = {entry.id: entry for entry in loaded}
            missing_ids = [entry_id for entry_id in page_ids if entry_id not in entries_by_id]
            if missing_ids:
                raise RuntimeError(f"media library snapshot entry missing: {missing_ids[0]}")
            entries = [entries_by_id[entry_id] for entry_id in page_ids]
    else:
        entries = load_legacy_gallery_entries(session, order="created", limit=limit, offset=offset)
    summary = GalleryBackfillSummary(scanned=len(entries))
    for entry in entries:
        existing = session.scalar(
            select(MediaLibraryAsset).where(
                MediaLibraryAsset.source_type == "legacy_gallery",
                MediaLibraryAsset.source_id == entry.id,
            )
        )
        if existing is not None:
            summary = replace(summary, skipped_existing=summary.skipped_existing + 1)
            continue
        try:
            _, media = _gallery_source(entry)
            path = storage.resolve(media.storage_path)
            if not path.is_file():
                raise FileNotFoundError(f"media file missing for gallery entry {entry.id}")
        except FileNotFoundError:
            summary = _append_blocker(summary, entry_id=entry.id, code="media_file_missing")
            continue
        except (RuntimeError, ValueError):
            summary = _append_blocker(summary, entry_id=entry.id, code="source_media_invalid")
            continue
        if not apply:
            summary = replace(summary, created=summary.created + 1)
            continue
        backfill_gallery_entry(session, entry=entry, storage=storage)
        summary = replace(summary, created=summary.created + 1)
    if apply:
        session.commit()
    return summary


def verify_gallery_backfill(
    session: Session,
    *,
    storage: LocalStorage,
    snapshot: MediaLibraryMigrationAudit,
) -> int:
    current = capture_gallery_snapshot(session)
    if (
        current.gallery_count != snapshot.gallery_count
        or current.session_asset_count != snapshot.session_asset_count
        or current.source_hash != snapshot.source_hash
        or current.source_rows != snapshot.source_rows
    ):
        raise RuntimeError("media library backfill source changed during migration")
    entries = load_legacy_gallery_entries(session)
    entry_ids = {entry.id for entry in entries}
    legacy_assets = list(
        session.scalars(
            select(MediaLibraryAsset)
            .where(MediaLibraryAsset.source_type == "legacy_gallery")
            .order_by(MediaLibraryAsset.source_id, MediaLibraryAsset.id)
        ).all()
    )
    legacy_source_ids = [asset.source_id for asset in legacy_assets]
    unexpected_source_ids = sorted(set(legacy_source_ids) - entry_ids)
    if unexpected_source_ids:
        raise RuntimeError(f"media library has unmapped legacy gallery asset {unexpected_source_ids[0]}")
    if len(legacy_source_ids) != len(set(legacy_source_ids)):
        raise RuntimeError("media library has duplicate legacy gallery source mappings")
    if set(legacy_source_ids) != entry_ids:
        missing_source_id = sorted(entry_ids - set(legacy_source_ids))[0]
        raise RuntimeError(f"missing media library asset for gallery entry {missing_source_id}")
    verified = 0
    for entry in entries:
        library_asset = session.scalar(
            select(MediaLibraryAsset).where(
                MediaLibraryAsset.source_type == "legacy_gallery",
                MediaLibraryAsset.source_id == entry.id,
            )
        )
        if library_asset is None:
            raise RuntimeError(f"missing media library asset for gallery entry {entry.id}")
        asset, media = _gallery_source(entry)
        path = storage.resolve(media.storage_path)
        if not path.is_file():
            raise RuntimeError(f"media file missing for gallery entry {entry.id}")
        actual_sha256 = sha256(path.read_bytes()).hexdigest()
        if actual_sha256 != media.sha256:
            raise RuntimeError(f"media file hash mismatch for gallery entry {entry.id}")
        expected_provenance = _build_gallery_provenance(entry=entry, media=media)
        expected_hash = canonical_provenance_hash(parse_provenance_v1(expected_provenance).model_dump(mode="json"))
        if (
            library_asset.media_object_id != media.id
            or library_asset.source_image_session_asset_id != asset.id
            or library_asset.provenance_hash != expected_hash
        ):
            raise RuntimeError(f"media library mapping mismatch for gallery entry {entry.id}")
        verified += 1
    return verified
