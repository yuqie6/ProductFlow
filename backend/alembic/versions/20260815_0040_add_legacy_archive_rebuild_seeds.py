"""add legacy archive rebuild seeds

Revision ID: 20260815_0040
Revises: 20260815_0039
Create Date: 2026-08-15
"""

from __future__ import annotations

import sqlalchemy as sa

from alembic import op

revision = "20260815_0040"
down_revision = "20260815_0039"
branch_labels = None
depends_on = None


def upgrade() -> None:
    op.create_table(
        "workflow_draft_legacy_archive_seeds",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("workflow_draft_id", sa.String(length=36), nullable=False),
        sa.Column("product_id", sa.String(length=36), nullable=False),
        sa.Column("workflow_archive_id", sa.String(length=36), nullable=True),
        sa.Column("canvas_agent_archive_id", sa.String(length=36), nullable=True),
        sa.Column("user_template_archive_id", sa.String(length=36), nullable=True),
        sa.Column("schema_version", sa.Integer(), nullable=False),
        sa.Column("idempotency_key", sa.String(length=120), nullable=False),
        sa.Column("request_hash", sa.String(length=64), nullable=False),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.CheckConstraint(
            "schema_version = 1",
            name="ck_workflow_draft_legacy_archive_seeds_schema_version",
        ),
        sa.CheckConstraint(
            "length(request_hash) = 64",
            name="ck_workflow_draft_legacy_archive_seeds_request_hash",
        ),
        sa.CheckConstraint(
            "(workflow_archive_id IS NOT NULL AND canvas_agent_archive_id IS NULL "
            "AND user_template_archive_id IS NULL) OR "
            "(workflow_archive_id IS NULL AND canvas_agent_archive_id IS NOT NULL "
            "AND user_template_archive_id IS NULL) OR "
            "(workflow_archive_id IS NULL AND canvas_agent_archive_id IS NULL "
            "AND user_template_archive_id IS NOT NULL)",
            name="ck_workflow_draft_legacy_archive_seeds_one_archive",
        ),
        sa.ForeignKeyConstraint(
            ["workflow_draft_id"],
            ["workflow_drafts.id"],
            name="fk_workflow_draft_legacy_archive_seeds_draft_id",
            ondelete="CASCADE",
        ),
        sa.ForeignKeyConstraint(
            ["product_id"],
            ["products.id"],
            name="fk_workflow_draft_legacy_archive_seeds_product_id",
            ondelete="CASCADE",
        ),
        sa.ForeignKeyConstraint(
            ["workflow_archive_id"],
            ["legacy_workflow_archives.id"],
            name="fk_workflow_draft_legacy_archive_seeds_workflow_archive_id",
            ondelete="RESTRICT",
        ),
        sa.ForeignKeyConstraint(
            ["canvas_agent_archive_id"],
            ["legacy_canvas_agent_archives.id"],
            name="fk_workflow_draft_legacy_archive_seeds_canvas_archive_id",
            ondelete="RESTRICT",
        ),
        sa.ForeignKeyConstraint(
            ["user_template_archive_id"],
            ["legacy_user_template_archives.id"],
            name="fk_workflow_draft_legacy_archive_seeds_template_archive_id",
            ondelete="RESTRICT",
        ),
        sa.PrimaryKeyConstraint("id"),
        sa.UniqueConstraint(
            "workflow_draft_id",
            name="uq_workflow_draft_legacy_archive_seeds_draft_id",
        ),
        sa.UniqueConstraint(
            "product_id",
            "idempotency_key",
            name="uq_workflow_draft_legacy_archive_seeds_product_key",
        ),
    )
    op.create_index(
        "ix_workflow_draft_legacy_archive_seeds_product_created",
        "workflow_draft_legacy_archive_seeds",
        ["product_id", "created_at", "id"],
    )


def downgrade() -> None:
    bind = op.get_bind()
    if bind.execute(sa.text("SELECT COUNT(*) FROM workflow_draft_legacy_archive_seeds")).scalar_one() > 0:
        raise RuntimeError("cannot downgrade legacy archive rebuild seeds while seed data exists")
    op.drop_index(
        "ix_workflow_draft_legacy_archive_seeds_product_created",
        table_name="workflow_draft_legacy_archive_seeds",
    )
    op.drop_table("workflow_draft_legacy_archive_seeds")
