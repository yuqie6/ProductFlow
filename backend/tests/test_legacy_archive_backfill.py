from __future__ import annotations

from datetime import UTC, datetime

import pytest
import sqlalchemy as sa

from productflow_backend.application.legacy_retirement.backfill import backfill_legacy_archive_page
from productflow_backend.application.legacy_retirement.contracts import (
    LegacyArchiveExportPage,
    LegacyArchiveSnapshot,
    archive_export_page_sha256,
    canonical_sha256,
)
from productflow_backend.application.legacy_retirement.profiles import (
    CURRENT_CUTOVER_PROFILE,
    LEGACY_CANVAS_PROFILE,
)
from productflow_backend.domain.errors import BusinessValidationError
from productflow_backend.infrastructure.db.models import LegacyUserTemplateArchive


def _install_current_archive_profile(db_session, *, revision: str = "20260815_0041") -> None:
    db_session.execute(sa.text("CREATE TABLE alembic_version (version_num VARCHAR(32) PRIMARY KEY)"))
    db_session.execute(
        sa.text("INSERT INTO alembic_version (version_num) VALUES (:revision)"),
        {"revision": revision},
    )
    db_session.execute(
        sa.text(
            "CREATE TABLE source_assets ("
            "id VARCHAR(36) PRIMARY KEY, product_id VARCHAR(36) NOT NULL, "
            "storage_path VARCHAR(500) NOT NULL, canonical_asset_id VARCHAR(36)"
            ")"
        )
    )
    db_session.execute(
        sa.text(
            "CREATE TABLE poster_variants ("
            "id VARCHAR(36) PRIMARY KEY, product_id VARCHAR(36) NOT NULL, "
            "storage_path VARCHAR(500) NOT NULL, canonical_asset_id VARCHAR(36)"
            ")"
        )
    )
    db_session.execute(
        sa.text(
            "CREATE TABLE user_canvas_templates ("
            "id VARCHAR(36) PRIMARY KEY, key VARCHAR(80) NOT NULL, schema_version INTEGER NOT NULL, "
            "template_json JSON NOT NULL, archived_at DATETIME"
            ")"
        )
    )
    db_session.execute(
        sa.text(
            "CREATE TABLE IF NOT EXISTS product_workflows ("
            "id VARCHAR(36) PRIMARY KEY, product_id VARCHAR(36) NOT NULL, title VARCHAR(255), "
            "active BOOLEAN NOT NULL, schema_version INTEGER NOT NULL, revision INTEGER NOT NULL, "
            "edit_version INTEGER NOT NULL"
            ")"
        )
    )
    db_session.execute(
        sa.text(
            "CREATE TABLE IF NOT EXISTS workflow_nodes ("
            "id VARCHAR(36) PRIMARY KEY, workflow_id VARCHAR(36) NOT NULL, "
            "schema_version INTEGER, node_key VARCHAR(80), node_type VARCHAR(40) NOT NULL, "
            "status VARCHAR(40) NOT NULL, folder_id VARCHAR(36), bound_image_asset_id VARCHAR(36)"
            ")"
        )
    )
    db_session.execute(
        sa.text(
            "CREATE TABLE IF NOT EXISTS workflow_edges ("
            "id VARCHAR(36) PRIMARY KEY, workflow_id VARCHAR(36) NOT NULL, "
            "source_node_id VARCHAR(36) NOT NULL, target_node_id VARCHAR(36) NOT NULL, "
            "edge_key VARCHAR(80)"
            ")"
        )
    )
    db_session.execute(
        sa.text(
            "CREATE TABLE IF NOT EXISTS workflow_runs ("
            "id VARCHAR(36) PRIMARY KEY, workflow_id VARCHAR(36) NOT NULL, status VARCHAR(40) NOT NULL"
            ")"
        )
    )
    db_session.execute(
        sa.text(
            "CREATE TABLE IF NOT EXISTS workflow_node_runs ("
            "id VARCHAR(36) PRIMARY KEY, workflow_run_id VARCHAR(36) NOT NULL, "
            "node_id VARCHAR(36) NOT NULL, status VARCHAR(40) NOT NULL"
            ")"
        )
    )
    db_session.commit()


def _legacy_template_page(*, generated_at: datetime) -> LegacyArchiveExportPage:
    payload = {
        "template": {"key": "legacy-saved-layout", "description": "用户保存的旧布局"},
        "template_json": {"nodes": [], "edges": []},
    }
    snapshot = LegacyArchiveSnapshot(
        kind="user_template",
        source_profile=LEGACY_CANVAS_PROFILE,
        source_id="legacy-template-1",
        title="旧布局",
        payload_json=payload,
        source_fingerprint_sha256="b" * 64,
        payload_sha256=canonical_sha256(payload),
        counts={},
    )
    provisional = LegacyArchiveExportPage(
        generated_at=generated_at,
        source_profile=LEGACY_CANVAS_PROFILE,
        source_report_sha256="a" * 64,
        source_fingerprint_sha256="b" * 64,
        blocking_issue_codes=["migration_bridge_required"],
        kind="user_template",
        items=[snapshot],
        page_sha256="0" * 64,
    )
    return provisional.model_copy(update={"page_sha256": archive_export_page_sha256(provisional)})


def test_archive_backfill_is_idempotent_for_approved_legacy_page(db_session) -> None:
    _install_current_archive_profile(db_session)
    page = _legacy_template_page(generated_at=datetime(2026, 8, 15, 11, 0, tzinfo=UTC))

    created = backfill_legacy_archive_page(
        db_session,
        page=page,
        expected_source_report_sha256=page.source_report_sha256,
        apply=True,
        allow_legacy_bridge=True,
        generated_at=page.generated_at,
    )
    assert created.applied is True
    assert created.created_count == 1
    assert created.items[0].status == "created"
    assert db_session.scalar(sa.select(sa.func.count()).select_from(LegacyUserTemplateArchive)) == 1

    unchanged = backfill_legacy_archive_page(
        db_session,
        page=page,
        expected_source_report_sha256=page.source_report_sha256,
        apply=True,
        allow_legacy_bridge=True,
        generated_at=page.generated_at,
    )
    assert unchanged.applied is True
    assert unchanged.unchanged_count == 1
    assert unchanged.items[0].status == "unchanged"
    assert db_session.scalar(sa.select(sa.func.count()).select_from(LegacyUserTemplateArchive)) == 1


def test_archive_backfill_accepts_the_0042_cutover_profile(db_session) -> None:
    _install_current_archive_profile(db_session, revision="20260816_0042")
    page = _legacy_template_page(generated_at=datetime(2026, 8, 15, 11, 0, tzinfo=UTC))

    result = backfill_legacy_archive_page(
        db_session,
        page=page,
        expected_source_report_sha256=page.source_report_sha256,
        allow_legacy_bridge=True,
    )

    assert result.target_profile == CURRENT_CUTOVER_PROFILE
    assert result.blocked_count == 0


def test_archive_backfill_rejects_report_or_page_hash_drift(db_session) -> None:
    _install_current_archive_profile(db_session)
    page = _legacy_template_page(generated_at=datetime(2026, 8, 15, 11, 0, tzinfo=UTC))

    with pytest.raises(BusinessValidationError, match="批准的 source report hash"):
        backfill_legacy_archive_page(
            db_session,
            page=page,
            expected_source_report_sha256="0" * 64,
        )

    tampered = page.model_copy(update={"page_sha256": "0" * 64})
    with pytest.raises(BusinessValidationError, match="page hash"):
        backfill_legacy_archive_page(
            db_session,
            page=tampered,
            expected_source_report_sha256=page.source_report_sha256,
        )
