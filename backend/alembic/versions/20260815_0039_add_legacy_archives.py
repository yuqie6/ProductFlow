"""add immutable legacy workflow archives

Revision ID: 20260815_0039
Revises: 20260814_0038
Create Date: 2026-08-15
"""

from __future__ import annotations

import sqlalchemy as sa

from alembic import op

revision = "20260815_0039"
down_revision = "20260814_0038"
branch_labels = None
depends_on = None


def upgrade() -> None:
    op.create_table(
        "legacy_workflow_archives",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("source_profile", sa.String(length=80), nullable=False),
        sa.Column("legacy_workflow_id", sa.String(length=36), nullable=False),
        sa.Column("product_id", sa.String(length=36), nullable=False),
        sa.Column("source_title", sa.String(length=255), nullable=False),
        sa.Column("source_updated_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("archive_schema_version", sa.Integer(), nullable=False),
        sa.Column("payload_json", sa.JSON(), nullable=False),
        sa.Column("source_fingerprint_sha256", sa.String(length=64), nullable=False),
        sa.Column("payload_sha256", sa.String(length=64), nullable=False),
        sa.Column("node_count", sa.Integer(), nullable=False),
        sa.Column("edge_count", sa.Integer(), nullable=False),
        sa.Column("run_count", sa.Integer(), nullable=False),
        sa.Column("node_run_count", sa.Integer(), nullable=False),
        sa.Column("asset_count", sa.Integer(), nullable=False),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.CheckConstraint(
            "archive_schema_version = 1",
            name="ck_legacy_workflow_archives_schema_version",
        ),
        sa.CheckConstraint(
            "length(source_fingerprint_sha256) = 64",
            name="ck_legacy_workflow_archives_source_hash",
        ),
        sa.CheckConstraint(
            "length(payload_sha256) = 64",
            name="ck_legacy_workflow_archives_payload_hash",
        ),
        sa.CheckConstraint(
            "node_count >= 0 AND edge_count >= 0 AND run_count >= 0 "
            "AND node_run_count >= 0 AND asset_count >= 0",
            name="ck_legacy_workflow_archives_counts",
        ),
        sa.ForeignKeyConstraint(
            ["product_id"],
            ["products.id"],
            name="fk_legacy_workflow_archives_product_id",
            ondelete="CASCADE",
        ),
        sa.PrimaryKeyConstraint("id"),
        sa.UniqueConstraint(
            "source_profile",
            "legacy_workflow_id",
            name="uq_legacy_workflow_archives_source_id",
        ),
    )
    op.create_index(
        "ix_legacy_workflow_archives_product_created",
        "legacy_workflow_archives",
        ["product_id", "created_at", "id"],
    )

    op.create_table(
        "legacy_user_template_archives",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("source_profile", sa.String(length=80), nullable=False),
        sa.Column("legacy_template_id", sa.String(length=36), nullable=False),
        sa.Column("legacy_key", sa.String(length=80), nullable=False),
        sa.Column("title", sa.String(length=255), nullable=False),
        sa.Column("description", sa.Text(), nullable=True),
        sa.Column("archive_status", sa.String(length=40), nullable=False),
        sa.Column("archive_schema_version", sa.Integer(), nullable=False),
        sa.Column("payload_json", sa.JSON(), nullable=False),
        sa.Column("diagnostics_json", sa.JSON(), nullable=False),
        sa.Column("source_fingerprint_sha256", sa.String(length=64), nullable=False),
        sa.Column("payload_sha256", sa.String(length=64), nullable=False),
        sa.Column("source_updated_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.CheckConstraint(
            "archive_schema_version = 1",
            name="ck_legacy_user_template_archives_schema_version",
        ),
        sa.CheckConstraint(
            "archive_status IN ('archived', 'damaged')",
            name="ck_legacy_user_template_archives_status",
        ),
        sa.CheckConstraint(
            "length(source_fingerprint_sha256) = 64",
            name="ck_legacy_user_template_archives_source_hash",
        ),
        sa.CheckConstraint(
            "length(payload_sha256) = 64",
            name="ck_legacy_user_template_archives_payload_hash",
        ),
        sa.PrimaryKeyConstraint("id"),
        sa.UniqueConstraint(
            "source_profile",
            "legacy_template_id",
            name="uq_legacy_user_template_archives_source_id",
        ),
    )
    op.create_index(
        "ix_legacy_user_template_archives_created",
        "legacy_user_template_archives",
        ["created_at", "id"],
    )

    op.create_table(
        "legacy_canvas_agent_archives",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("source_profile", sa.String(length=80), nullable=False),
        sa.Column("legacy_thread_id", sa.String(length=36), nullable=False),
        sa.Column("product_id", sa.String(length=36), nullable=False),
        sa.Column("title", sa.String(length=255), nullable=False),
        sa.Column("source_status", sa.String(length=40), nullable=False),
        sa.Column("source_updated_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("archive_schema_version", sa.Integer(), nullable=False),
        sa.Column("payload_json", sa.JSON(), nullable=False),
        sa.Column("source_fingerprint_sha256", sa.String(length=64), nullable=False),
        sa.Column("payload_sha256", sa.String(length=64), nullable=False),
        sa.Column("message_count", sa.Integer(), nullable=False),
        sa.Column("run_count", sa.Integer(), nullable=False),
        sa.Column("tool_event_count", sa.Integer(), nullable=False),
        sa.Column("plan_count", sa.Integer(), nullable=False),
        sa.Column("task_plan_count", sa.Integer(), nullable=False),
        sa.Column("timeline_event_count", sa.Integer(), nullable=False),
        sa.Column("visible_event_count", sa.Integer(), nullable=False),
        sa.Column("technical_event_count", sa.Integer(), nullable=False),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.CheckConstraint(
            "archive_schema_version = 1",
            name="ck_legacy_canvas_agent_archives_schema_version",
        ),
        sa.CheckConstraint(
            "length(source_fingerprint_sha256) = 64",
            name="ck_legacy_canvas_agent_archives_source_hash",
        ),
        sa.CheckConstraint(
            "length(payload_sha256) = 64",
            name="ck_legacy_canvas_agent_archives_payload_hash",
        ),
        sa.CheckConstraint(
            "message_count >= 0 AND run_count >= 0 AND tool_event_count >= 0 "
            "AND plan_count >= 0 AND task_plan_count >= 0 AND timeline_event_count >= 0 "
            "AND visible_event_count >= 0 AND technical_event_count >= 0",
            name="ck_legacy_canvas_agent_archives_counts",
        ),
        sa.CheckConstraint(
            "visible_event_count + technical_event_count = timeline_event_count",
            name="ck_legacy_canvas_agent_archives_event_total",
        ),
        sa.ForeignKeyConstraint(
            ["product_id"],
            ["products.id"],
            name="fk_legacy_canvas_agent_archives_product_id",
            ondelete="CASCADE",
        ),
        sa.PrimaryKeyConstraint("id"),
        sa.UniqueConstraint(
            "source_profile",
            "legacy_thread_id",
            name="uq_legacy_canvas_agent_archives_source_id",
        ),
    )
    op.create_index(
        "ix_legacy_canvas_agent_archives_product_created",
        "legacy_canvas_agent_archives",
        ["product_id", "created_at", "id"],
    )

    op.create_table(
        "legacy_workflow_archive_assets",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("archive_id", sa.String(length=36), nullable=False),
        sa.Column("product_image_asset_id", sa.String(length=36), nullable=False),
        sa.Column("role", sa.String(length=80), nullable=False),
        sa.Column("legacy_source_type", sa.String(length=80), nullable=False),
        sa.Column("legacy_source_id", sa.String(length=80), nullable=False),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.ForeignKeyConstraint(
            ["archive_id"],
            ["legacy_workflow_archives.id"],
            name="fk_legacy_workflow_archive_assets_archive_id",
            ondelete="CASCADE",
        ),
        sa.ForeignKeyConstraint(
            ["product_image_asset_id"],
            ["product_image_assets.id"],
            name="fk_legacy_workflow_archive_assets_asset_id",
            ondelete="RESTRICT",
        ),
        sa.PrimaryKeyConstraint("id"),
        sa.UniqueConstraint(
            "archive_id",
            "product_image_asset_id",
            "role",
            "legacy_source_type",
            "legacy_source_id",
            name="uq_legacy_workflow_archive_assets_identity",
        ),
    )
    op.create_index(
        "ix_legacy_workflow_archive_assets_asset_id",
        "legacy_workflow_archive_assets",
        ["product_image_asset_id"],
    )


def downgrade() -> None:
    bind = op.get_bind()
    archive_tables = (
        "legacy_workflow_archive_assets",
        "legacy_canvas_agent_archives",
        "legacy_user_template_archives",
        "legacy_workflow_archives",
    )
    populated = [
        table_name
        for table_name in archive_tables
        if bind.execute(sa.text(f"SELECT COUNT(*) FROM {table_name}")).scalar_one() > 0
    ]
    if populated:
        raise RuntimeError(
            "cannot downgrade legacy archives while archive data exists: " + ",".join(populated)
        )

    op.drop_index(
        "ix_legacy_workflow_archive_assets_asset_id",
        table_name="legacy_workflow_archive_assets",
    )
    op.drop_table("legacy_workflow_archive_assets")
    op.drop_index(
        "ix_legacy_canvas_agent_archives_product_created",
        table_name="legacy_canvas_agent_archives",
    )
    op.drop_table("legacy_canvas_agent_archives")
    op.drop_index(
        "ix_legacy_user_template_archives_created",
        table_name="legacy_user_template_archives",
    )
    op.drop_table("legacy_user_template_archives")
    op.drop_index(
        "ix_legacy_workflow_archives_product_created",
        table_name="legacy_workflow_archives",
    )
    op.drop_table("legacy_workflow_archives")
