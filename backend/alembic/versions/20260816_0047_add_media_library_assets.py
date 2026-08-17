"""add media library assets

Revision ID: 20260816_0047
Revises: 20260816_0046
Create Date: 2026-08-16
"""

from __future__ import annotations

import sqlalchemy as sa

from alembic import op

revision = "20260816_0047"
down_revision = "20260816_0046"
branch_labels = None
depends_on = None


def upgrade() -> None:
    op.create_table(
        "media_library_assets",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("media_object_id", sa.String(length=36), nullable=False),
        sa.Column("source_type", sa.String(length=40), nullable=False),
        sa.Column("source_id", sa.String(length=36), nullable=False),
        sa.Column("source_image_session_asset_id", sa.String(length=36), nullable=True),
        sa.Column("source_product_asset_id", sa.String(length=36), nullable=True),
        sa.Column("provenance_json", sa.JSON(), nullable=False),
        sa.Column("provenance_hash", sa.String(length=64), nullable=False),
        sa.Column("revision", sa.Integer(), nullable=False),
        sa.Column("display_name", sa.String(length=255), nullable=False),
        sa.Column("original_filename", sa.String(length=255), nullable=False),
        sa.Column("is_archived", sa.Boolean(), nullable=False),
        sa.Column("archived_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), nullable=False),
        sa.CheckConstraint(
            "source_type IN ('legacy_gallery', 'image_session_generated', 'product_asset')",
            name="ck_media_library_assets_source_type",
        ),
        sa.CheckConstraint(
            "revision >= 1",
            name="ck_media_library_assets_revision",
        ),
        sa.CheckConstraint(
            "length(provenance_hash) = 64",
            name="ck_media_library_assets_provenance_hash",
        ),
        sa.ForeignKeyConstraint(
            ["media_object_id"],
            ["media_objects.id"],
            name="fk_media_library_assets_media_object_id",
            ondelete="RESTRICT",
        ),
        sa.ForeignKeyConstraint(
            ["source_image_session_asset_id"],
            ["image_session_assets.id"],
            name="fk_media_library_assets_source_image_session_asset_id",
            ondelete="SET NULL",
        ),
        sa.ForeignKeyConstraint(
            ["source_product_asset_id"],
            ["product_image_assets.id"],
            name="fk_media_library_assets_source_product_asset_id",
            ondelete="SET NULL",
        ),
        sa.PrimaryKeyConstraint("id"),
        sa.UniqueConstraint("source_type", "source_id", name="uq_media_library_assets_source"),
    )
    op.create_index(
        "ix_media_library_assets_media_object_id",
        "media_library_assets",
        ["media_object_id"],
    )
    op.create_index(
        "ix_media_library_assets_source_image_session_asset_id",
        "media_library_assets",
        ["source_image_session_asset_id"],
    )
    op.create_index(
        "ix_media_library_assets_source_product_asset_id",
        "media_library_assets",
        ["source_product_asset_id"],
    )


def downgrade() -> None:
    bind = op.get_bind()
    if bind.execute(sa.text("SELECT 1 FROM media_library_assets LIMIT 1")).first() is not None:
        raise RuntimeError("cannot downgrade media library assets while rows exist")
    op.drop_index("ix_media_library_assets_source_product_asset_id", table_name="media_library_assets")
    op.drop_index("ix_media_library_assets_source_image_session_asset_id", table_name="media_library_assets")
    op.drop_index("ix_media_library_assets_media_object_id", table_name="media_library_assets")
    op.drop_table("media_library_assets")
