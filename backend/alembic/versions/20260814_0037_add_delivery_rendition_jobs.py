"""add delivery rendition jobs

Revision ID: 20260814_0037
Revises: 20260813_0036
Create Date: 2026-08-14
"""

from __future__ import annotations

import sqlalchemy as sa
from sqlalchemy.dialects import postgresql

from alembic import op

revision = "20260814_0037"
down_revision = "20260813_0036"
branch_labels = None
depends_on = None

JOB_STATUS = postgresql.ENUM(
    "queued",
    "running",
    "succeeded",
    "failed",
    "cancelled",
    name="jobstatus",
    create_type=False,
)


def upgrade() -> None:
    op.create_table(
        "delivery_rendition_jobs",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("product_id", sa.String(length=36), nullable=False),
        sa.Column("source_asset_id", sa.String(length=36), nullable=False),
        sa.Column("result_asset_id", sa.String(length=36), nullable=True),
        sa.Column("spec_schema_version", sa.Integer(), nullable=False),
        sa.Column("spec_json", sa.JSON(), nullable=False),
        sa.Column("spec_hash", sa.String(length=64), nullable=False),
        sa.Column("status", JOB_STATUS, nullable=False),
        sa.Column("attempts", sa.Integer(), nullable=False),
        sa.Column("active_attempt_id", sa.String(length=36), nullable=True),
        sa.Column("is_retryable", sa.Boolean(), nullable=False),
        sa.Column("failure_reason", sa.Text(), nullable=True),
        sa.Column("started_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("finished_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), nullable=False),
        sa.CheckConstraint(
            "spec_schema_version = 1",
            name="ck_delivery_rendition_jobs_schema_version",
        ),
        sa.CheckConstraint(
            "length(spec_hash) = 64",
            name="ck_delivery_rendition_jobs_spec_hash",
        ),
        sa.CheckConstraint(
            "attempts >= 0",
            name="ck_delivery_rendition_jobs_non_negative_attempts",
        ),
        sa.CheckConstraint(
            "status IN ('queued', 'running', 'succeeded', 'failed')",
            name="ck_delivery_rendition_jobs_status",
        ),
        sa.CheckConstraint(
            "(status = 'running' AND active_attempt_id IS NOT NULL AND started_at IS NOT NULL "
            "AND finished_at IS NULL) OR "
            "(status != 'running' AND active_attempt_id IS NULL)",
            name="ck_delivery_rendition_jobs_active_attempt",
        ),
        sa.CheckConstraint(
            "(status = 'succeeded' AND result_asset_id IS NOT NULL AND finished_at IS NOT NULL) OR "
            "(status = 'failed' AND result_asset_id IS NULL AND finished_at IS NOT NULL) OR "
            "(status IN ('queued', 'running') AND result_asset_id IS NULL AND finished_at IS NULL)",
            name="ck_delivery_rendition_jobs_result_state",
        ),
        sa.ForeignKeyConstraint(
            ["product_id"],
            ["products.id"],
            name="fk_delivery_rendition_jobs_product_id",
            ondelete="CASCADE",
        ),
        sa.ForeignKeyConstraint(
            ["source_asset_id"],
            ["product_image_assets.id"],
            name="fk_delivery_rendition_jobs_source_asset_id",
            ondelete="RESTRICT",
        ),
        sa.ForeignKeyConstraint(
            ["result_asset_id"],
            ["product_image_assets.id"],
            name="fk_delivery_rendition_jobs_result_asset_id",
            ondelete="RESTRICT",
        ),
        sa.PrimaryKeyConstraint("id"),
        sa.UniqueConstraint(
            "source_asset_id",
            "spec_hash",
            name="uq_delivery_rendition_jobs_source_spec",
        ),
        sa.UniqueConstraint(
            "result_asset_id",
            name="uq_delivery_rendition_jobs_result_asset_id",
        ),
    )
    op.create_index(
        "ix_delivery_rendition_jobs_product_status_created",
        "delivery_rendition_jobs",
        ["product_id", "status", "created_at", "id"],
    )
    op.create_index(
        "ix_delivery_rendition_jobs_source_created",
        "delivery_rendition_jobs",
        ["source_asset_id", "created_at", "id"],
    )


def downgrade() -> None:
    bind = op.get_bind()
    if bind.execute(sa.text("SELECT 1 FROM delivery_rendition_jobs LIMIT 1")).first() is not None:
        raise RuntimeError("cannot downgrade delivery renditions while rendition jobs exist")
    op.drop_index(
        "ix_delivery_rendition_jobs_source_created",
        table_name="delivery_rendition_jobs",
    )
    op.drop_index(
        "ix_delivery_rendition_jobs_product_status_created",
        table_name="delivery_rendition_jobs",
    )
    op.drop_table("delivery_rendition_jobs")
