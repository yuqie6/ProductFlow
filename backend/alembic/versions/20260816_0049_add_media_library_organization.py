"""add media library organization

Revision ID: 20260816_0049
Revises: 20260816_0048
Create Date: 2026-08-16
"""

from __future__ import annotations

import sqlalchemy as sa

from alembic import op

revision = "20260816_0049"
down_revision = "20260816_0048"
branch_labels = None
depends_on = None


def upgrade() -> None:
    op.create_table(
        "media_library_folders",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("name", sa.String(length=120), nullable=False),
        sa.Column("normalized_name", sa.String(length=120), nullable=False),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), nullable=False),
        sa.PrimaryKeyConstraint("id"),
        sa.UniqueConstraint("normalized_name", name="uq_media_library_folders_normalized_name"),
    )
    op.create_table(
        "media_library_tags",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("name", sa.String(length=80), nullable=False),
        sa.Column("normalized_name", sa.String(length=80), nullable=False),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), nullable=False),
        sa.PrimaryKeyConstraint("id"),
        sa.UniqueConstraint("normalized_name", name="uq_media_library_tags_normalized_name"),
    )
    op.create_table(
        "media_library_asset_tags",
        sa.Column("asset_id", sa.String(length=36), nullable=False),
        sa.Column("tag_id", sa.String(length=36), nullable=False),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.ForeignKeyConstraint(
            ["asset_id"], ["media_library_assets.id"],
            name="fk_media_library_asset_tags_asset_id", ondelete="CASCADE",
        ),
        sa.ForeignKeyConstraint(
            ["tag_id"], ["media_library_tags.id"],
            name="fk_media_library_asset_tags_tag_id", ondelete="CASCADE",
        ),
        sa.PrimaryKeyConstraint("asset_id", "tag_id"),
    )
    op.create_index("ix_media_library_asset_tags_tag_id", "media_library_asset_tags", ["tag_id"])
    with op.batch_alter_table("media_library_assets") as batch_op:
        batch_op.add_column(sa.Column("folder_id", sa.String(length=36), nullable=True))
        batch_op.create_foreign_key(
            "fk_media_library_assets_folder_id",
            "media_library_folders",
            ["folder_id"], ["id"], ondelete="SET NULL",
        )
        batch_op.create_index("ix_media_library_assets_folder_id", ["folder_id"])


def downgrade() -> None:
    bind = op.get_bind()
    if bind.execute(sa.text("SELECT 1 FROM media_library_assets WHERE folder_id IS NOT NULL LIMIT 1")).first():
        raise RuntimeError("cannot downgrade while media library assets retain folders")
    with op.batch_alter_table("media_library_assets") as batch_op:
        batch_op.drop_index("ix_media_library_assets_folder_id")
        batch_op.drop_constraint("fk_media_library_assets_folder_id", type_="foreignkey")
        batch_op.drop_column("folder_id")
    op.drop_index("ix_media_library_asset_tags_tag_id", table_name="media_library_asset_tags")
    op.drop_table("media_library_asset_tags")
    op.drop_table("media_library_tags")
    op.drop_table("media_library_folders")
