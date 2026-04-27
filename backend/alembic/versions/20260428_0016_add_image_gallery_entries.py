"""add image gallery entries

Revision ID: 20260428_0016
Revises: 20260427_0015
Create Date: 2026-04-28
"""

from __future__ import annotations

import sqlalchemy as sa

from alembic import op

revision = "20260428_0016"
down_revision = "20260427_0015"
branch_labels = None
depends_on = None


def upgrade() -> None:
    op.create_table(
        "image_gallery_entries",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("image_session_asset_id", sa.String(length=36), nullable=False),
        sa.Column("image_session_round_id", sa.String(length=36), nullable=True),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.ForeignKeyConstraint(
            ["image_session_asset_id"],
            ["image_session_assets.id"],
            name="fk_image_gallery_entries_image_session_asset_id",
            ondelete="CASCADE",
        ),
        sa.ForeignKeyConstraint(
            ["image_session_round_id"],
            ["image_session_rounds.id"],
            name="fk_image_gallery_entries_image_session_round_id",
            ondelete="SET NULL",
        ),
        sa.PrimaryKeyConstraint("id"),
    )
    op.create_index(
        "uq_image_gallery_entries_asset_id",
        "image_gallery_entries",
        ["image_session_asset_id"],
        unique=True,
    )
    op.create_index("ix_image_gallery_entries_round_id", "image_gallery_entries", ["image_session_round_id"])
    op.create_index("ix_image_gallery_entries_created_at", "image_gallery_entries", ["created_at"])


def downgrade() -> None:
    op.drop_index("ix_image_gallery_entries_created_at", table_name="image_gallery_entries")
    op.drop_index("ix_image_gallery_entries_round_id", table_name="image_gallery_entries")
    op.drop_index("uq_image_gallery_entries_asset_id", table_name="image_gallery_entries")
    op.drop_table("image_gallery_entries")
