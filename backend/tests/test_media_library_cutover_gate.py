from __future__ import annotations

from datetime import UTC, datetime
from hashlib import sha256
from pathlib import Path

import pytest
import sqlalchemy as sa

from productflow_backend.application.legacy_retirement.media_library import LEGACY_GALLERY_ENTRIES
from productflow_backend.application.media_library.backfill import (
    capture_gallery_snapshot,
    gallery_reconciliation_hash,
    run_gallery_backfill,
)
from productflow_backend.application.media_library.retirement import (
    LEGACY_GALLERY_RETIRE_CONFIRMATION,
    MEDIA_LIBRARY_CUTOVER_PHASE_CLEANED,
    MEDIA_LIBRARY_CUTOVER_PHASE_PENDING,
    MEDIA_LIBRARY_CUTOVER_PHASE_READY,
    approve_media_library_cutover_gate,
    assert_media_library_cutover_cleanup_ready,
    inspect_legacy_gallery_retirement,
    read_media_library_cutover_gate,
    retire_legacy_gallery,
)
from productflow_backend.domain.enums import ImageSessionAssetKind, MediaVerificationStatus
from productflow_backend.domain.errors import ConflictError
from productflow_backend.infrastructure.db.models import ImageSession, ImageSessionAsset, MediaLibraryAsset, MediaObject
from productflow_backend.infrastructure.storage import LocalStorage


def _create_gate(db_session) -> None:
    sa.Table(
        "media_library_cutover_gates",
        sa.MetaData(),
        sa.Column("id", sa.String(36), primary_key=True),
        sa.Column("schema_version", sa.Integer, nullable=False),
        sa.Column("phase", sa.String(40), nullable=False),
        sa.Column("source_snapshot_token", sa.String(255)),
        sa.Column("source_report_sha256", sa.String(64)),
        sa.Column("reconciliation_report_sha256", sa.String(64)),
        sa.Column("backup_restore_verified_at", sa.DateTime(timezone=True)),
        sa.Column("zero_delta_observed_at", sa.DateTime(timezone=True)),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), nullable=False),
    ).create(db_session.get_bind(), checkfirst=True)
    now = datetime(2026, 8, 18, 1, 0, tzinfo=UTC)
    db_session.execute(
        sa.text(
            "INSERT INTO media_library_cutover_gates "
            "(id, schema_version, phase, created_at, updated_at) "
            "VALUES ('singleton', 1, 'pending', :now, :now)"
        ),
        {"now": now},
    )
    db_session.commit()


def _create_gallery_entry(db_session, storage_root: Path) -> str:
    LEGACY_GALLERY_ENTRIES.create(db_session.get_bind(), checkfirst=True)
    path = storage_root / "media" / "retire.png"
    path.parent.mkdir(parents=True, exist_ok=True)
    content = b"retire-me"
    path.write_bytes(content)
    image_session = ImageSession(title="retire session")
    media = MediaObject(
        storage_path="media/retire.png",
        mime_type="image/png",
        byte_size=len(content),
        width=1,
        height=1,
        sha256=sha256(content).hexdigest(),
        verification_status=MediaVerificationStatus.VERIFIED,
        verified_at=datetime.now(UTC),
    )
    db_session.add(image_session)
    db_session.flush()
    asset = ImageSessionAsset(
        session=image_session,
        kind=ImageSessionAssetKind.GENERATED_IMAGE,
        original_filename="retire.png",
        mime_type="image/png",
        storage_path="media/retire.png",
        media_object=media,
    )
    db_session.add(asset)
    db_session.flush()
    entry_id = "retire-gallery-entry"
    db_session.execute(
        LEGACY_GALLERY_ENTRIES.insert().values(
            id=entry_id,
            image_session_asset_id=asset.id,
            image_session_round_id=None,
            created_at=image_session.created_at,
        )
    )
    db_session.commit()
    return entry_id


def test_media_library_gate_stays_pending_until_explicit_evidence(db_session) -> None:
    _create_gate(db_session)
    engine = db_session.get_bind()
    with engine.begin() as connection:
        assert read_media_library_cutover_gate(connection).phase == MEDIA_LIBRARY_CUTOVER_PHASE_PENDING
        with pytest.raises(ConflictError, match="清理闸门未通过"):
            assert_media_library_cutover_cleanup_ready(connection)


def test_retire_legacy_gallery_rechecks_mapping_and_preserves_canonical_asset(
    db_session,
    configured_env: Path,
) -> None:
    _create_gate(db_session)
    entry_id = _create_gallery_entry(db_session, configured_env)
    storage = LocalStorage(root=configured_env)
    snapshot = capture_gallery_snapshot(db_session)
    run_gallery_backfill(db_session, storage=storage, apply=True)
    reconciliation_hash = gallery_reconciliation_hash(db_session, storage=storage, snapshot=snapshot)
    now = datetime(2026, 8, 18, 2, 0, tzinfo=UTC)

    with db_session.get_bind().begin() as connection:
        approved = approve_media_library_cutover_gate(
            connection,
            source_snapshot_token="postgres-snapshot-1",
            source_report_sha256=snapshot.source_hash,
            reconciliation_report_sha256=reconciliation_hash,
            backup_restore_verified_at=now,
            zero_delta_observed_at=now,
        )
        assert approved.phase == MEDIA_LIBRARY_CUTOVER_PHASE_READY

    report = inspect_legacy_gallery_retirement(db_session, storage=storage)
    assert report.source_row_count == 1
    assert report.reconciliation_report_sha256 == reconciliation_hash
    assert report.database_snapshot_token == "sqlite:single-connection"
    assert report.storage_snapshot_id

    retired = retire_legacy_gallery(
        db_session,
        storage=storage,
        confirmation=LEGACY_GALLERY_RETIRE_CONFIRMATION,
    )
    assert retired.dropped is True
    assert retired.gate_phase == MEDIA_LIBRARY_CUTOVER_PHASE_CLEANED
    assert retired.database_snapshot_token == "sqlite:single-connection"
    assert retired.storage_snapshot_id
    assert sa.inspect(db_session.get_bind()).has_table("image_gallery_entries") is False
    assert db_session.get(MediaLibraryAsset, entry_id) is not None


def test_retire_legacy_gallery_rejects_source_drift_before_drop(db_session, configured_env: Path) -> None:
    _create_gate(db_session)
    _create_gallery_entry(db_session, configured_env)
    storage = LocalStorage(root=configured_env)
    snapshot = capture_gallery_snapshot(db_session)
    run_gallery_backfill(db_session, storage=storage, apply=True)
    reconciliation_hash = gallery_reconciliation_hash(db_session, storage=storage, snapshot=snapshot)
    now = datetime(2026, 8, 18, 2, 0, tzinfo=UTC)
    with db_session.get_bind().begin() as connection:
        approve_media_library_cutover_gate(
            connection,
            source_snapshot_token="postgres-snapshot-1",
            source_report_sha256=snapshot.source_hash,
            reconciliation_report_sha256=reconciliation_hash,
            backup_restore_verified_at=now,
            zero_delta_observed_at=now,
        )
    db_session.execute(
        sa.text(
            "INSERT INTO image_gallery_entries "
            "(id, image_session_asset_id, image_session_round_id, created_at) "
            "VALUES ('late-entry', 'missing-asset', NULL, :now)"
        ),
        {"now": now},
    )
    db_session.commit()

    with pytest.raises((ConflictError, RuntimeError)):
        retire_legacy_gallery(
            db_session,
            storage=storage,
            confirmation=LEGACY_GALLERY_RETIRE_CONFIRMATION,
        )
    assert sa.inspect(db_session.get_bind()).has_table("image_gallery_entries") is True
