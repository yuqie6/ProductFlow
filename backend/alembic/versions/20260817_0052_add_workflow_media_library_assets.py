"""add workflow media library associations

Revision ID: 20260817_0052
Revises: 20260817_0051
Create Date: 2026-08-17
"""

from __future__ import annotations

import sqlalchemy as sa

from alembic import op

revision = "20260817_0052"
down_revision = "20260817_0051"
branch_labels = None
depends_on = None


def upgrade() -> None:
    op.create_table(
        "workflow_media_library_assets",
        sa.Column("workflow_id", sa.String(length=36), nullable=False),
        sa.Column("media_library_asset_id", sa.String(length=36), nullable=False),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.ForeignKeyConstraint(
            ["workflow_id"],
            ["product_workflows.id"],
            name="fk_workflow_media_library_assets_workflow_id",
            ondelete="CASCADE",
        ),
        sa.ForeignKeyConstraint(
            ["media_library_asset_id"],
            ["media_library_assets.id"],
            name="fk_workflow_media_library_assets_library_asset_id",
            ondelete="RESTRICT",
        ),
        sa.PrimaryKeyConstraint("workflow_id", "media_library_asset_id"),
    )
    op.create_index(
        "ix_workflow_media_library_assets_workflow_created",
        "workflow_media_library_assets",
        ["workflow_id", "created_at", "media_library_asset_id"],
    )
    op.create_index(
        "ix_workflow_media_library_assets_library_asset_id",
        "workflow_media_library_assets",
        ["media_library_asset_id"],
    )


def downgrade() -> None:
    bind = op.get_bind()
    if bind.execute(sa.text("SELECT 1 FROM workflow_media_library_assets LIMIT 1")).first() is not None:
        raise RuntimeError("cannot downgrade while workflow media library associations exist")
    op.drop_index(
        "ix_workflow_media_library_assets_library_asset_id",
        table_name="workflow_media_library_assets",
    )
    op.drop_index(
        "ix_workflow_media_library_assets_workflow_created",
        table_name="workflow_media_library_assets",
    )
    op.drop_table("workflow_media_library_assets")
