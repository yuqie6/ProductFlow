from __future__ import annotations

from datetime import UTC, datetime

import pytest
import sqlalchemy as sa

from productflow_backend.application.legacy_retirement.gate import (
    LEGACY_CUTOVER_PHASE_CLEANED,
    LEGACY_CUTOVER_PHASE_PENDING,
    LEGACY_CUTOVER_PHASE_READY,
    approve_legacy_cutover_gate,
    assert_legacy_cutover_cleanup_ready,
    count_legacy_active_runs,
    mark_legacy_cutover_cleaned,
    read_legacy_cutover_gate,
)
from productflow_backend.domain.errors import BusinessValidationError, ConflictError

_HASHES = {
    "source_report_sha256": "a" * 64,
    "archive_report_sha256": "b" * 64,
    "canonical_report_sha256": "c" * 64,
}
_VERIFIED_AT = datetime(2026, 8, 16, 12, 0, tzinfo=UTC)


def _gate_engine(*, active_run: bool = False) -> sa.Engine:
    engine = sa.create_engine("sqlite://", future=True)
    metadata = sa.MetaData()
    sa.Table(
        "legacy_cutover_gates",
        metadata,
        sa.Column("id", sa.String(36), primary_key=True),
        sa.Column("schema_version", sa.Integer, nullable=False),
        sa.Column("phase", sa.String(40), nullable=False),
        sa.Column("source_profile", sa.String(80)),
        sa.Column("source_report_sha256", sa.String(64)),
        sa.Column("archive_report_sha256", sa.String(64)),
        sa.Column("canonical_report_sha256", sa.String(64)),
        sa.Column("backup_restore_verified_at", sa.DateTime(timezone=True)),
        sa.Column("active_run_count", sa.Integer, nullable=False),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), nullable=False),
    )
    sa.Table(
        "product_workflows",
        metadata,
        sa.Column("id", sa.String(36), primary_key=True),
        sa.Column("schema_version", sa.Integer, nullable=False),
    )
    sa.Table(
        "workflow_runs",
        metadata,
        sa.Column("id", sa.String(36), primary_key=True),
        sa.Column("workflow_id", sa.String(36), nullable=False),
        sa.Column("status", sa.String(40), nullable=False),
    )
    sa.Table(
        "workflow_node_runs",
        metadata,
        sa.Column("id", sa.String(36), primary_key=True),
        sa.Column("workflow_run_id", sa.String(36), nullable=False),
        sa.Column("status", sa.String(40), nullable=False),
    )
    metadata.create_all(engine)
    now = datetime(2026, 8, 16, 10, 0, tzinfo=UTC)
    with engine.begin() as connection:
        connection.execute(
            sa.text(
                "INSERT INTO legacy_cutover_gates "
                "(id, schema_version, phase, active_run_count, created_at, updated_at) "
                "VALUES ('singleton', 1, 'pending', 0, :now, :now)"
            ),
            {"now": now},
        )
        if active_run:
            connection.execute(
                sa.text("INSERT INTO product_workflows (id, schema_version) VALUES ('workflow-v1', 1)")
            )
            connection.execute(
                sa.text(
                    "INSERT INTO workflow_runs (id, workflow_id, status) "
                    "VALUES ('run-v1', 'workflow-v1', 'running')"
                )
            )
            connection.execute(
                sa.text(
                    "INSERT INTO workflow_node_runs (id, workflow_run_id, status) "
                    "VALUES ('node-run-v1', 'run-v1', 'queued')"
                )
            )
    return engine


def test_pending_gate_rejects_cleanup_and_active_legacy_runs_block_approval() -> None:
    engine = _gate_engine(active_run=True)
    try:
        with engine.begin() as connection:
            assert count_legacy_active_runs(connection) == 2
            with pytest.raises(ConflictError, match="未到终态"):
                approve_legacy_cutover_gate(
                    connection,
                    source_profile="current_with_agent_workspace_finalization_20260815_0041",
                    **_HASHES,
                    backup_restore_verified_at=_VERIFIED_AT,
                )
            with pytest.raises(ConflictError, match="清理门槛未通过"):
                assert_legacy_cutover_cleanup_ready(connection)
            assert read_legacy_cutover_gate(connection).phase == LEGACY_CUTOVER_PHASE_PENDING
    finally:
        engine.dispose()


def test_gate_persists_all_evidence_before_cleanup_and_records_cleaned() -> None:
    engine = _gate_engine()
    try:
        with engine.begin() as connection:
            approved = approve_legacy_cutover_gate(
                connection,
                source_profile="current_with_agent_workspace_finalization_20260815_0041",
                **_HASHES,
                backup_restore_verified_at=_VERIFIED_AT,
            )
            assert approved.phase == LEGACY_CUTOVER_PHASE_READY
            assert approved.active_run_count == 0
            assert assert_legacy_cutover_cleanup_ready(connection).source_report_sha256 == "a" * 64

            cleaned = mark_legacy_cutover_cleaned(connection)
            assert cleaned.phase == LEGACY_CUTOVER_PHASE_CLEANED
            with pytest.raises(ConflictError, match="已经标记为 cleaned"):
                approve_legacy_cutover_gate(
                    connection,
                    source_profile="current_with_agent_workspace_finalization_20260815_0041",
                    **_HASHES,
                    backup_restore_verified_at=_VERIFIED_AT,
                )
    finally:
        engine.dispose()


def test_gate_rejects_invalid_hashes_and_timezone_less_backup_evidence() -> None:
    engine = _gate_engine()
    try:
        with engine.begin() as connection:
            with pytest.raises(BusinessValidationError, match="64 位小写 SHA-256"):
                approve_legacy_cutover_gate(
                    connection,
                    source_profile="profile",
                    source_report_sha256="not-a-hash",
                    archive_report_sha256="b" * 64,
                    canonical_report_sha256="c" * 64,
                    backup_restore_verified_at=_VERIFIED_AT,
                )
            with pytest.raises(BusinessValidationError, match="必须带时区"):
                approve_legacy_cutover_gate(
                    connection,
                    source_profile="profile",
                    **_HASHES,
                    backup_restore_verified_at=datetime(2026, 8, 16, 12, 0),
                )
    finally:
        engine.dispose()
