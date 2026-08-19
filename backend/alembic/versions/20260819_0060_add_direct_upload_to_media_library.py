"""add direct_upload to media library assets source type

Revision ID: 20260819_0060
Revises: 20260819_0059
Create Date: 2026-08-19
"""

from __future__ import annotations

from alembic import op

revision = "20260819_0060"
down_revision = "20260819_0059"
branch_labels = None
depends_on = None


def _alter_existing_table(table_name: str):
    bind = op.get_bind()
    return op.batch_alter_table(table_name, recreate="always" if bind.dialect.name == "sqlite" else "auto")


def upgrade() -> None:
    bind = op.get_bind()
    if bind.dialect.name == "postgresql":
        op.drop_constraint("ck_media_library_assets_source_type", "media_library_assets", type_="check")
        op.create_check_constraint(
            "ck_media_library_assets_source_type",
            "media_library_assets",
            "source_type IN ('legacy_gallery', 'image_session_generated', 'product_asset', 'direct_upload')",
        )
    else:
        with _alter_existing_table("media_library_assets") as batch_op:
            batch_op.drop_constraint("ck_media_library_assets_source_type", type_="check")
            batch_op.create_check_constraint(
                "ck_media_library_assets_source_type",
                "source_type IN ('legacy_gallery', 'image_session_generated', 'product_asset', 'direct_upload')",
            )


def downgrade() -> None:
    bind = op.get_bind()
    if bind.dialect.name == "postgresql":
        op.drop_constraint("ck_media_library_assets_source_type", "media_library_assets", type_="check")
        op.create_check_constraint(
            "ck_media_library_assets_source_type",
            "media_library_assets",
            "source_type IN ('legacy_gallery', 'image_session_generated', 'product_asset')",
        )
    else:
        with _alter_existing_table("media_library_assets") as batch_op:
            batch_op.drop_constraint("ck_media_library_assets_source_type", type_="check")
            batch_op.create_check_constraint(
                "ck_media_library_assets_source_type",
                "source_type IN ('legacy_gallery', 'image_session_generated', 'product_asset')",
            )
