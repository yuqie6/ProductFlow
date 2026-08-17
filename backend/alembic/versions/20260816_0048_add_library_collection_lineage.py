"""add media library collection lineage

Revision ID: 20260816_0048
Revises: 20260816_0047
Create Date: 2026-08-16
"""

from __future__ import annotations

import sqlalchemy as sa

from alembic import op

revision = "20260816_0048"
down_revision = "20260816_0047"
branch_labels = None
depends_on = None

SOURCE_LIBRARY_FK = "fk_product_image_assets_source_library_asset_id"
SOURCE_LIBRARY_INDEX = "ix_product_image_assets_source_library_asset_id"
SOURCE_LIBRARY_UNIQUE = "uq_product_image_assets_product_library_asset"


def _alter_existing_table(table_name: str):
    bind = op.get_bind()
    return op.batch_alter_table(table_name, recreate="always" if bind.dialect.name == "sqlite" else "auto")


def upgrade() -> None:
    with _alter_existing_table("product_image_assets") as batch_op:
        batch_op.add_column(sa.Column("source_library_asset_id", sa.String(length=36), nullable=True))
        batch_op.create_foreign_key(
            SOURCE_LIBRARY_FK,
            "media_library_assets",
            ["source_library_asset_id"],
            ["id"],
            ondelete="RESTRICT",
        )
        batch_op.create_index(
            SOURCE_LIBRARY_INDEX,
            ["source_library_asset_id"],
        )
        batch_op.create_index(
            SOURCE_LIBRARY_UNIQUE,
            ["product_id", "source_library_asset_id"],
            unique=True,
        )


def downgrade() -> None:
    bind = op.get_bind()
    if bind.execute(
        sa.text(
            "SELECT 1 FROM product_image_assets "
            "WHERE source_library_asset_id IS NOT NULL LIMIT 1"
        )
    ).first():
        raise RuntimeError("cannot downgrade while product assets retain media library lineage")
    with _alter_existing_table("product_image_assets") as batch_op:
        batch_op.drop_index(SOURCE_LIBRARY_UNIQUE)
        batch_op.drop_index(SOURCE_LIBRARY_INDEX)
        batch_op.drop_constraint(SOURCE_LIBRARY_FK, type_="foreignkey")
        batch_op.drop_column("source_library_asset_id")
