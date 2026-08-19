from __future__ import annotations

from datetime import UTC, datetime
from io import BytesIO
from pathlib import Path

import pytest
import sqlalchemy as sa
from PIL import Image
from sqlalchemy.orm import Session

from productflow_backend.application.legacy_retirement.gallery_bridge import (
    approve_legacy_gallery_bridge,
    export_legacy_gallery_bridge_manifest,
    inspect_legacy_gallery_source_retirement,
    retire_legacy_gallery_source,
    run_legacy_gallery_bridge,
    verify_legacy_gallery_bridge,
)
from productflow_backend.application.legacy_retirement.gallery_bridge_contracts import (
    gallery_bridge_manifest_sha256,
)
from productflow_backend.domain.errors import BusinessValidationError, ConflictError
from productflow_backend.infrastructure.db.models import Base, MediaLibraryAsset
from productflow_backend.infrastructure.storage import LocalStorage


def _image_bytes() -> bytes:
    output = BytesIO()
    Image.new("RGB", (3, 2), color=(220, 80, 60)).save(output, format="PNG")
    return output.getvalue()


def _legacy_engine(tmp_path: Path) -> tuple[sa.Engine, Path]:
    engine = sa.create_engine(f"sqlite:///{tmp_path / 'legacy-gallery.db'}", future=True)
    storage_root = tmp_path / "legacy-storage"
    storage_root.mkdir()
    metadata = sa.MetaData()
    version = sa.Table("alembic_version", metadata, sa.Column("version_num", sa.String(32), primary_key=True))
    assets = sa.Table(
        "image_session_assets",
        metadata,
        sa.Column("id", sa.String(36), primary_key=True),
        sa.Column("session_id", sa.String(36), nullable=False),
        sa.Column("original_filename", sa.String(255), nullable=False),
        sa.Column("mime_type", sa.String(100), nullable=False),
        sa.Column("storage_path", sa.String(500), nullable=False),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
    )
    gallery = sa.Table(
        "image_gallery_entries",
        metadata,
        sa.Column("id", sa.String(36), primary_key=True),
        sa.Column("image_session_asset_id", sa.String(36), nullable=False),
        sa.Column("image_session_round_id", sa.String(36)),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
    )
    metadata.create_all(engine)
    now = datetime(2026, 8, 19, 10, 0, tzinfo=UTC)
    content = _image_bytes()
    relative_path = "image_sessions/session-1/generated.png"
    path = storage_root / relative_path
    path.parent.mkdir(parents=True)
    path.write_bytes(content)
    with engine.begin() as connection:
        connection.execute(version.insert(), {"version_num": "20260518_0032"})
        connection.execute(
            assets.insert(),
            {
                "id": "session-asset-1",
                "session_id": "session-1",
                "original_filename": "generated.png",
                "mime_type": "image/png",
                "storage_path": relative_path,
                "created_at": now,
            },
        )
        connection.execute(
            gallery.insert(),
            {
                "id": "gallery-entry-1",
                "image_session_asset_id": "session-asset-1",
                "image_session_round_id": None,
                "created_at": now,
            },
        )
    return engine, storage_root


def _target_engine(tmp_path: Path) -> sa.Engine:
    engine = sa.create_engine(f"sqlite:///{tmp_path / 'current-gallery.db'}", future=True)
    Base.metadata.create_all(engine)
    return engine


def test_legacy_gallery_bridge_migrates_idempotently_and_retire_source(tmp_path: Path) -> None:
    source_engine, source_storage_root = _legacy_engine(tmp_path)
    target_engine = _target_engine(tmp_path)
    target_storage_root = tmp_path / "target-storage"
    try:
        manifest = export_legacy_gallery_bridge_manifest(
            source_engine,
            storage_root=source_storage_root,
            generated_at=datetime(2026, 8, 19, 10, 1, tzinfo=UTC),
        )
        assert len(manifest.records) == 1
        assert manifest.records[0].blocker_code is None
        assert gallery_bridge_manifest_sha256(manifest) == manifest.manifest_sha256

        target_session = Session(target_engine, expire_on_commit=False)
        try:
            first = run_legacy_gallery_bridge(
                target_session,
                manifest=manifest,
                source_storage_root=source_storage_root,
                target_storage=LocalStorage(root=target_storage_root),
                expected_source_report_sha256=manifest.source_report_sha256,
                apply=True,
            )
            assert first.applied is True
            assert first.created_count == 1
            reconciliation = verify_legacy_gallery_bridge(
                target_session,
                manifest=manifest,
                source_storage_root=source_storage_root,
                target_storage=LocalStorage(root=target_storage_root),
                expected_source_report_sha256=manifest.source_report_sha256,
            )
            second = run_legacy_gallery_bridge(
                target_session,
                manifest=manifest,
                source_storage_root=source_storage_root,
                target_storage=LocalStorage(root=target_storage_root),
                expected_source_report_sha256=manifest.source_report_sha256,
                apply=True,
            )
            assert second.applied is True
            assert second.created_count == 0
            assert second.unchanged_count == 1
            assert verify_legacy_gallery_bridge(
                target_session,
                manifest=manifest,
                source_storage_root=source_storage_root,
                target_storage=LocalStorage(root=target_storage_root),
                expected_source_report_sha256=manifest.source_report_sha256,
            ) == reconciliation
        finally:
            target_session.close()

        approval = approve_legacy_gallery_bridge(
            manifest,
            target_reconciliation_sha256=reconciliation,
            backup_restore_verified_at=datetime(2026, 8, 19, 11, 0, tzinfo=UTC),
            zero_delta_observed_at=datetime(2026, 8, 19, 12, 0, tzinfo=UTC),
        )
        dry_run = inspect_legacy_gallery_source_retirement(
            source_engine,
            manifest=manifest,
            approval=approval,
            storage_root=source_storage_root,
        )
        assert dry_run.dropped is False
        assert dry_run.source_row_count == 1

        retired = retire_legacy_gallery_source(
            source_engine,
            manifest=manifest,
            approval=approval,
            storage_root=source_storage_root,
            confirmation="RETIRE_LEGACY_GALLERY",
        )
        assert retired.dropped is True
        assert sa.inspect(source_engine).has_table("image_gallery_entries") is False
        with target_engine.begin() as connection:
            assert connection.execute(sa.text("SELECT COUNT(*) FROM media_library_assets")).scalar_one() == 1
        with Session(target_engine) as session:
            assert session.scalar(sa.select(MediaLibraryAsset.source_id)) == "gallery-entry-1"
    finally:
        source_engine.dispose()
        target_engine.dispose()


def test_legacy_gallery_bridge_rejects_mixed_source_revisions(tmp_path: Path) -> None:
    source_engine, source_storage_root = _legacy_engine(tmp_path)
    try:
        with source_engine.begin() as connection:
            connection.execute(
                sa.text("INSERT INTO alembic_version (version_num) VALUES ('other-revision')")
            )
        with pytest.raises(BusinessValidationError, match="只支持 source revision"):
            export_legacy_gallery_bridge_manifest(source_engine, storage_root=source_storage_root)
    finally:
        source_engine.dispose()


def test_legacy_gallery_bridge_fails_closed_on_source_file_drift(tmp_path: Path) -> None:
    source_engine, source_storage_root = _legacy_engine(tmp_path)
    target_engine = _target_engine(tmp_path)
    target_storage_root = tmp_path / "target-storage"
    try:
        manifest = export_legacy_gallery_bridge_manifest(source_engine, storage_root=source_storage_root)
        source_file = source_storage_root / manifest.records[0].storage_path
        source_file.write_bytes(b"changed")
        with Session(target_engine) as session:
            report = run_legacy_gallery_bridge(
                session,
                manifest=manifest,
                source_storage_root=source_storage_root,
                target_storage=LocalStorage(root=target_storage_root),
                expected_source_report_sha256=manifest.source_report_sha256,
                apply=True,
            )
        assert report.applied is False
        assert report.blocked_count == 1
        assert report.items[0].diagnostic_codes == ["source_file_changed"]
        with target_engine.begin() as connection:
            assert connection.execute(sa.text("SELECT COUNT(*) FROM media_library_assets")).scalar_one() == 0
    finally:
        source_engine.dispose()
        target_engine.dispose()


def test_legacy_gallery_bridge_blocker_cannot_be_approved(tmp_path: Path) -> None:
    source_engine, source_storage_root = _legacy_engine(tmp_path)
    try:
        (source_storage_root / "image_sessions/session-1/generated.png").unlink()
        manifest = export_legacy_gallery_bridge_manifest(source_engine, storage_root=source_storage_root)
        assert manifest.records[0].blocker_code == "source_file_missing"
        with pytest.raises(ConflictError, match="包含 blocker"):
            approve_legacy_gallery_bridge(
                manifest,
                target_reconciliation_sha256="a" * 64,
                backup_restore_verified_at=datetime(2026, 8, 19, 11, 0, tzinfo=UTC),
                zero_delta_observed_at=datetime(2026, 8, 19, 12, 0, tzinfo=UTC),
            )
    finally:
        source_engine.dispose()


def test_legacy_gallery_bridge_rejects_source_drift_before_physical_drop(tmp_path: Path) -> None:
    source_engine, source_storage_root = _legacy_engine(tmp_path)
    try:
        manifest = export_legacy_gallery_bridge_manifest(source_engine, storage_root=source_storage_root)
        approval = approve_legacy_gallery_bridge(
            manifest,
            target_reconciliation_sha256="a" * 64,
            backup_restore_verified_at=datetime(2026, 8, 19, 11, 0, tzinfo=UTC),
            zero_delta_observed_at=datetime(2026, 8, 19, 12, 0, tzinfo=UTC),
        )
        with source_engine.begin() as connection:
            connection.execute(
                sa.text(
                    "INSERT INTO image_gallery_entries "
                    "(id, image_session_asset_id, image_session_round_id, created_at) "
                    "VALUES ('late-entry', 'session-asset-1', NULL, :created_at)"
                ),
                {"created_at": datetime(2026, 8, 19, 13, 0, tzinfo=UTC)},
            )
        with pytest.raises(ConflictError, match="manifest"):
            retire_legacy_gallery_source(
                source_engine,
                manifest=manifest,
                approval=approval,
                storage_root=source_storage_root,
                confirmation="RETIRE_LEGACY_GALLERY",
            )
        assert sa.inspect(source_engine).has_table("image_gallery_entries") is True
    finally:
        source_engine.dispose()
