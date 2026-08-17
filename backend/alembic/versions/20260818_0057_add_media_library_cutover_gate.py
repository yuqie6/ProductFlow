"""add a guarded media library cutover gate

Revision ID: 20260818_0057
Revises: 20260818_0056
Create Date: 2026-08-18

The gate records deployment evidence for retiring the old Gallery table.  It
does not read storage, migrate rows, or drop any legacy table.
"""

from __future__ import annotations

import sqlalchemy as sa

from alembic import op

revision = "20260818_0057"
down_revision = "20260818_0056"
branch_labels = None
depends_on = None

MEDIA_LIBRARY_CUTOVER_GATE_TABLE = "media_library_cutover_gates"
MEDIA_LIBRARY_CUTOVER_GATE_ID = "singleton"
MEDIA_LIBRARY_CUTOVER_GATE_SCHEMA_VERSION = 1


def upgrade() -> None:
    op.create_table(
        MEDIA_LIBRARY_CUTOVER_GATE_TABLE,
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("schema_version", sa.Integer(), nullable=False),
        sa.Column("phase", sa.String(length=40), nullable=False),
        sa.Column("source_snapshot_token", sa.String(length=255), nullable=True),
        sa.Column("source_report_sha256", sa.String(length=64), nullable=True),
        sa.Column("reconciliation_report_sha256", sa.String(length=64), nullable=True),
        sa.Column("backup_restore_verified_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("zero_delta_observed_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), nullable=False),
        sa.CheckConstraint(
            f"schema_version = {MEDIA_LIBRARY_CUTOVER_GATE_SCHEMA_VERSION}",
            name="ck_media_library_cutover_gates_schema_version",
        ),
        sa.CheckConstraint(
            "phase IN ('pending', 'ready_for_cleanup', 'cleaned')",
            name="ck_media_library_cutover_gates_phase",
        ),
        sa.CheckConstraint(
            "source_report_sha256 IS NULL OR length(source_report_sha256) = 64",
            name="ck_media_library_cutover_gates_source_report_hash",
        ),
        sa.CheckConstraint(
            "reconciliation_report_sha256 IS NULL OR length(reconciliation_report_sha256) = 64",
            name="ck_media_library_cutover_gates_reconciliation_hash",
        ),
        sa.PrimaryKeyConstraint("id"),
    )
    op.get_bind().execute(
        sa.text(
            "INSERT INTO media_library_cutover_gates "
            "(id, schema_version, phase, created_at, updated_at) "
            "VALUES (:id, :schema_version, 'pending', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)"
        ),
        {
            "id": MEDIA_LIBRARY_CUTOVER_GATE_ID,
            "schema_version": MEDIA_LIBRARY_CUTOVER_GATE_SCHEMA_VERSION,
        },
    )


def downgrade() -> None:
    raise RuntimeError("media library cutover evidence is retained and this revision cannot be downgraded")
