from __future__ import annotations

import json
from datetime import UTC, datetime
from hashlib import sha256
from pathlib import Path

from productflow_backend.application.media_library.backfill import (
    capture_gallery_snapshot,
    run_gallery_backfill,
    verify_gallery_backfill,
)
from productflow_backend.commands.backfill_media_library import _load_or_capture_snapshot
from productflow_backend.domain.enums import ImageSessionAssetKind, MediaVerificationStatus
from productflow_backend.infrastructure.db.models import (
    ImageGalleryEntry,
    ImageSession,
    ImageSessionAsset,
    MediaLibraryAsset,
    MediaObject,
)
from productflow_backend.infrastructure.storage import LocalStorage


def _create_gallery_entry(db_session, configured_env: Path):
    storage_root = Path(configured_env)
    media_path = storage_root / "media" / "backfill.png"
    media_path.parent.mkdir(parents=True, exist_ok=True)
    media_path.write_bytes(b"fake-png")

    session = ImageSession(title="backfill session")
    media = MediaObject(
        storage_path="media/backfill.png",
        mime_type="image/png",
        byte_size=8,
        width=1,
        height=1,
        sha256=sha256(b"fake-png").hexdigest(),
        verification_status=MediaVerificationStatus.VERIFIED,
        verified_at=datetime.now(UTC),
    )
    db_session.add(session)
    db_session.flush()
    asset = ImageSessionAsset(
        session=session,
        kind=ImageSessionAssetKind.GENERATED_IMAGE,
        original_filename="backfill.png",
        mime_type="image/png",
        storage_path="media/backfill.png",
        media_object=media,
    )
    db_session.add(asset)
    db_session.flush()
    entry = ImageGalleryEntry(
        id="gallery-backfill-1",
        image_session_asset_id=asset.id,
    )
    db_session.add(entry)
    db_session.commit()
    return entry


def test_gallery_backfill_apply_and_reconcile(db_session, configured_env: Path) -> None:
    entry = _create_gallery_entry(db_session, configured_env)
    snapshot = capture_gallery_snapshot(db_session)
    assert snapshot.gallery_count == 1

    summary = run_gallery_backfill(
        db_session,
        storage=LocalStorage(root=configured_env),
        apply=True,
    )
    assert summary.created == 1
    assert summary.skipped_existing == 0

    verified = verify_gallery_backfill(
        db_session,
        storage=LocalStorage(root=configured_env),
        snapshot=snapshot,
    )
    assert verified == 1

    db_session.expire_all()
    library_asset = db_session.get(MediaLibraryAsset, entry.id)
    assert library_asset is not None
    assert library_asset.source_type == "legacy_gallery"
    assert library_asset.source_id == entry.id
    assert library_asset.provenance_hash


def test_gallery_backfill_is_idempotent(db_session, configured_env: Path) -> None:
    _create_gallery_entry(db_session, configured_env)
    first = run_gallery_backfill(
        db_session,
        storage=LocalStorage(root=configured_env),
        apply=True,
    )
    second = run_gallery_backfill(
        db_session,
        storage=LocalStorage(root=configured_env),
        apply=True,
    )
    assert first.created == 1
    assert second.created == 0
    assert second.skipped_existing == 1


def test_snapshot_file_is_valid_json_with_a_real_trailing_newline(db_session, tmp_path: Path) -> None:
    snapshot_path = tmp_path / "gallery-snapshot.json"

    snapshot = _load_or_capture_snapshot(db_session, snapshot_path, require_existing=False)
    content = snapshot_path.read_text(encoding="utf-8")

    assert content.endswith("\n")
    assert json.loads(content)["snapshot_token"] == snapshot.snapshot_token
