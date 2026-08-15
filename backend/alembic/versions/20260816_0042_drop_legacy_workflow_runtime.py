"""preserve legacy data and install the V1 cutover gate

Revision ID: 20260816_0042
Revises: 20260815_0041
Create Date: 2026-08-16

This revision deliberately does not remove legacy rows, source tables, or
archive tables.  The online application has already stopped importing the V1
executor, while deployed databases still need their historical source data
until the auditable archive/canonical migration is complete.
"""

from __future__ import annotations

import sqlalchemy as sa

from alembic import op

revision = "20260816_0042"
down_revision = "20260815_0041"
branch_labels = None
depends_on = None

LEGACY_CUTOVER_GATE_TABLE = "legacy_cutover_gates"
LEGACY_CUTOVER_GATE_ID = "singleton"
LEGACY_CUTOVER_GATE_SCHEMA_VERSION = 1
LEGACY_CUTOVER_PHASE_PENDING = "pending"
LEGACY_CUTOVER_PHASE_READY = "ready_for_cleanup"
LEGACY_CUTOVER_PHASE_CLEANED = "cleaned"


def upgrade() -> None:
    """Install only durable evidence state; never perform semantic migration here."""
    op.create_table(
        LEGACY_CUTOVER_GATE_TABLE,
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("schema_version", sa.Integer(), nullable=False),
        sa.Column("phase", sa.String(length=40), nullable=False),
        sa.Column("source_profile", sa.String(length=80), nullable=True),
        sa.Column("source_report_sha256", sa.String(length=64), nullable=True),
        sa.Column("archive_report_sha256", sa.String(length=64), nullable=True),
        sa.Column("canonical_report_sha256", sa.String(length=64), nullable=True),
        sa.Column("backup_restore_verified_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("active_run_count", sa.Integer(), nullable=False),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), nullable=False),
        sa.CheckConstraint(
            f"schema_version = {LEGACY_CUTOVER_GATE_SCHEMA_VERSION}",
            name="ck_legacy_cutover_gates_schema_version",
        ),
        sa.CheckConstraint(
            "phase IN ('pending', 'ready_for_cleanup', 'cleaned')",
            name="ck_legacy_cutover_gates_phase",
        ),
        sa.CheckConstraint("active_run_count >= 0", name="ck_legacy_cutover_gates_active_run_count"),
        sa.CheckConstraint(
            "source_report_sha256 IS NULL OR length(source_report_sha256) = 64",
            name="ck_legacy_cutover_gates_source_report_hash",
        ),
        sa.CheckConstraint(
            "archive_report_sha256 IS NULL OR length(archive_report_sha256) = 64",
            name="ck_legacy_cutover_gates_archive_report_hash",
        ),
        sa.CheckConstraint(
            "canonical_report_sha256 IS NULL OR length(canonical_report_sha256) = 64",
            name="ck_legacy_cutover_gates_canonical_report_hash",
        ),
        sa.PrimaryKeyConstraint("id"),
    )
    op.get_bind().execute(
        sa.text(
            "INSERT INTO legacy_cutover_gates "
            "(id, schema_version, phase, active_run_count, created_at, updated_at) "
            "VALUES (:id, :schema_version, :phase, 0, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)"
        ),
        {
            "id": LEGACY_CUTOVER_GATE_ID,
            "schema_version": LEGACY_CUTOVER_GATE_SCHEMA_VERSION,
            "phase": LEGACY_CUTOVER_PHASE_PENDING,
        },
    )


def downgrade() -> None:
    """Do not remove the evidence gate during a downgrade attempt."""
    raise RuntimeError("legacy cutover evidence is retained and this revision cannot be downgraded")
