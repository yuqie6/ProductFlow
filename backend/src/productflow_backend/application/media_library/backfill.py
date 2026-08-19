from __future__ import annotations

import json
import logging
from dataclasses import dataclass, replace
from hashlib import sha256
from pathlib import Path

from sqlalchemy import func, select, text
from sqlalchemy.orm import Session

from productflow_backend.application.legacy_retirement.contracts import canonical_json_bytes
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
    ProductImageAsset,
    WorkflowMediaLibraryAsset,
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


def capture_gallery_snapshot(
    session: Session,
    *,
    storage: LocalStorage | None = None,
) -> MediaLibraryMigrationAudit:
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
        database_snapshot_token=_database_snapshot_token(session),
        storage_snapshot_id=_storage_snapshot_id(session, storage=storage) if storage is not None else None,
    )


def _database_snapshot_token(session: Session) -> str | None:
    """Return a monotonic, comparable database anchor for the capture transaction.

    ``pg_current_wal_lsn`` is chosen over ``pg_current_snapshot`` because it is
    monotonic: a later observation can prove the WAL is at or past the recorded
    point, so the token can actually be compared across runs. A fresh
    ``pg_current_snapshot`` id is a brand-new value in every transaction and can
    never be matched again, which made it record-only evidence. SQLite has no
    snapshot machinery, so it is honestly reported as single-connection.
    """

    dialect = session.get_bind().dialect.name
    if dialect == "postgresql":
        lsn = session.execute(text("SELECT pg_current_wal_lsn()")).scalar()
        return f"postgres:pg_current_wal_lsn:{lsn}"
    if dialect == "sqlite":
        return "sqlite:single-connection"
    return None


def _storage_snapshot_id(session: Session, *, storage: LocalStorage) -> str:
    entries = load_legacy_gallery_entries(session)
    files: list[tuple[str, int, str | None]] = []
    for entry in entries:
        if entry.asset is not None and entry.asset.media_object is not None:
            media = entry.asset.media_object
            path = storage.resolve(media.storage_path) if media.storage_path else None
            if path is not None and path.is_file():
                byte_size = media.byte_size
                file_sha256 = sha256(path.read_bytes()).hexdigest()
            else:
                byte_size = media.byte_size
                file_sha256 = None
            relative = Path(media.storage_path).as_posix() if media.storage_path else "<none>"
            files.append((relative, byte_size, file_sha256))
    payload = {
        "schema_version": 1,
        "storage_root": str(Path(storage.root).resolve()),
        "files": sorted(set(files)),
    }
    return sha256(canonical_json_bytes(payload)).hexdigest()


def collect_gallery_backfill_blockers(
    session: Session,
    *,
    storage: LocalStorage,
    snapshot: MediaLibraryMigrationAudit,
) -> tuple[tuple[str, str], ...]:
    """Return a durable full-preflight blocker report for a frozen snapshot.

    This is independent from ``run_gallery_backfill`` page execution so the
    snapshot JSON can carry the complete blocker evidence even when a later
    apply step only processes a bounded page.
    """

    snapshot_ids = [row[0] for row in snapshot.source_rows]
    blockers: list[tuple[str, str]] = []
    for offset in range(0, len(snapshot_ids), 100):
        page_ids = snapshot_ids[offset : offset + 100]
        entries_by_id = {
            entry.id: entry for entry in load_legacy_gallery_entries(session, ids=page_ids)
        }
        for entry_id in page_ids:
            entry = entries_by_id.get(entry_id)
            if entry is None:
                blockers.append((entry_id, "missing_snapshot_row"))
                continue
            code = _entry_blocker_code(entry, storage=storage)
            if code is not None:
                blockers.append((entry.id, code))
    return tuple(blockers)


def _entry_blocker_code(entry: LegacyGalleryEntry, *, storage: LocalStorage) -> str | None:
    try:
        _, media = _gallery_source(entry)
        path = storage.resolve(media.storage_path)
        if not path.is_file():
            return "media_file_missing"
        return None
    except FileNotFoundError:
        return "media_file_missing"
    except (RuntimeError, ValueError):
        return "source_media_invalid"


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
        code = _entry_blocker_code(entry, storage=storage)
        if code is not None:
            summary = _append_blocker(summary, entry_id=entry.id, code=code)
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
    # Recompute the storage fingerprint over the original files and compare it
    # against the recorded snapshot so an unverified storage change is caught
    # here instead of being accepted on the strength of a recorded hash.  The
    # recompute re-reads every media file; this runs only during the bounded
    # maintenance-window reconcile, never in page execution.
    if snapshot.storage_snapshot_id is not None:
        current_storage_id = _storage_snapshot_id(session, storage=storage)
        if current_storage_id != snapshot.storage_snapshot_id:
            raise RuntimeError("media library storage fingerprint changed since snapshot capture")
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


def gallery_reconciliation_hash(
    session: Session,
    *,
    storage: LocalStorage,
    snapshot: MediaLibraryMigrationAudit,
) -> str:
    """Return a stable hash for the verified old-to-new Gallery mapping.

    The source snapshot alone cannot prove that the canonical mapping and
    workflow-side references stayed unchanged.  Include those relationships
    in the report hash so the retirement gate can recheck the same facts
    immediately before dropping the legacy table.
    """

    verify_gallery_backfill(session, storage=storage, snapshot=snapshot)
    rows: list[dict[str, object]] = []
    for entry in load_legacy_gallery_entries(session):
        if entry.asset is None or entry.asset.media_object is None:
            raise RuntimeError(f"gallery entry {entry.id} has no verified canonical source")
        library_asset = session.scalar(
            select(MediaLibraryAsset).where(
                MediaLibraryAsset.source_type == "legacy_gallery",
                MediaLibraryAsset.source_id == entry.id,
            )
        )
        if library_asset is None:
            raise RuntimeError(f"missing media library asset for gallery entry {entry.id}")
        workflow_link_count = int(
            session.scalar(
                select(func.count())
                .select_from(WorkflowMediaLibraryAsset)
                .where(WorkflowMediaLibraryAsset.media_library_asset_id == library_asset.id)
            )
            or 0
        )
        product_reference_count = int(
            session.scalar(
                select(func.count())
                .select_from(ProductImageAsset)
                .where(ProductImageAsset.source_library_asset_id == library_asset.id)
            )
            or 0
        )
        rows.append(
            {
                "entry_id": entry.id,
                "image_session_asset_id": entry.asset.id,
                "media_object_id": entry.asset.media_object.id,
                "media_sha256": entry.asset.media_object.sha256,
                "library_asset_id": library_asset.id,
                "provenance_hash": library_asset.provenance_hash,
                "workflow_link_count": workflow_link_count,
                "product_reference_count": product_reference_count,
            }
        )
    payload = {
        "schema_version": 1,
        "snapshot_token": snapshot.snapshot_token,
        "source_hash": snapshot.source_hash,
        "gallery_count": snapshot.gallery_count,
        "rows": rows,
    }
    return sha256(canonical_json_bytes(payload)).hexdigest()
