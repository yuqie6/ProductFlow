from __future__ import annotations

import json
from datetime import UTC, datetime
from hashlib import sha256
from pathlib import Path

import pytest

from productflow_backend.application.legacy_retirement.media_library import LEGACY_GALLERY_ENTRIES
from productflow_backend.application.media_library.backfill import (
    GalleryBackfillBlocker,
    capture_gallery_snapshot,
    run_gallery_backfill,
    verify_gallery_backfill,
)
from productflow_backend.commands.backfill_media_library import _load_or_capture_snapshot, main
from productflow_backend.domain.enums import ImageSessionAssetKind, MediaVerificationStatus
from productflow_backend.infrastructure.db.models import (
    ImageSession,
    ImageSessionAsset,
    MediaLibraryAsset,
    MediaObject,
)
from productflow_backend.infrastructure.storage import LocalStorage


def _create_gallery_entry(db_session, configured_env: Path):
    LEGACY_GALLERY_ENTRIES.create(db_session.get_bind(), checkfirst=True)
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
    entry_id = "gallery-backfill-1"
    db_session.execute(
        LEGACY_GALLERY_ENTRIES.insert().values(
            id=entry_id,
            image_session_asset_id=asset.id,
            image_session_round_id=None,
            created_at=session.created_at,
        )
    )
    db_session.commit()
    return entry_id


def test_gallery_backfill_apply_and_reconcile(db_session, configured_env: Path) -> None:
    entry_id = _create_gallery_entry(db_session, configured_env)
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
    library_asset = db_session.get(MediaLibraryAsset, entry_id)
    assert library_asset is not None
    assert library_asset.source_type == "legacy_gallery"
    assert library_asset.source_id == entry_id
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


def test_gallery_backfill_reports_blocker_ids_and_codes(db_session, configured_env: Path) -> None:
    entry_id = _create_gallery_entry(db_session, configured_env)
    (configured_env / "media" / "backfill.png").unlink()

    summary = run_gallery_backfill(
        db_session,
        storage=LocalStorage(root=configured_env),
        apply=False,
    )

    assert summary.blocked == 1
    assert summary.blockers == (GalleryBackfillBlocker(entry_id=entry_id, code="media_file_missing"),)


def test_backfill_command_returns_blocking_exit_code(db_session, configured_env: Path, tmp_path: Path) -> None:
    _create_gallery_entry(db_session, configured_env)
    (configured_env / "media" / "backfill.png").unlink()

    assert main(["--snapshot-file", str(tmp_path / "gallery-snapshot.json")]) == 2


def test_gallery_backfill_reconcile_rejects_extra_legacy_mapping(db_session, configured_env: Path) -> None:
    entry_id = _create_gallery_entry(db_session, configured_env)
    run_gallery_backfill(
        db_session,
        storage=LocalStorage(root=configured_env),
        apply=True,
    )
    library_asset = db_session.get(MediaLibraryAsset, entry_id)
    assert library_asset is not None
    db_session.add(
        MediaLibraryAsset(
            id="legacy-extra",
            media_object_id=library_asset.media_object_id,
            source_type="legacy_gallery",
            source_id="legacy-extra",
            provenance_json={},
            provenance_hash="0" * 64,
            display_name="extra",
            original_filename="extra.png",
        )
    )
    db_session.commit()

    with pytest.raises(RuntimeError, match="unmapped legacy gallery asset legacy-extra"):
        verify_gallery_backfill(
            db_session,
            storage=LocalStorage(root=configured_env),
            snapshot=capture_gallery_snapshot(db_session),
        )


def test_snapshot_file_is_valid_json_with_a_real_trailing_newline(db_session, tmp_path: Path) -> None:
    snapshot_path = tmp_path / "gallery-snapshot.json"
    LEGACY_GALLERY_ENTRIES.create(db_session.get_bind(), checkfirst=True)

    snapshot = _load_or_capture_snapshot(db_session, snapshot_path, require_existing=False)
    content = snapshot_path.read_text(encoding="utf-8")

    assert content.endswith("\n")
    assert json.loads(content)["snapshot_token"] == snapshot.snapshot_token
