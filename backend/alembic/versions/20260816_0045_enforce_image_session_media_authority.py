"""enforce canonical ImageSession media authority

Revision ID: 20260816_0045
Revises: 20260816_0044
Create Date: 2026-08-16

The historical canonical-media migration backfilled media_object_id but left the
column nullable. This repair validates database-only carrier coherence before
making the canonical reference required. It deliberately retains the duplicate
path and MIME columns for a separately verified retirement migration.
"""

from __future__ import annotations

import sqlalchemy as sa

from alembic import op

revision = "20260816_0045"
down_revision = "20260816_0044"
branch_labels = None
depends_on = None

TABLE_NAME = "image_session_assets"
MEDIA_OBJECT_ID_COLUMN = "media_object_id"


def _assert_canonical_media_coherence(connection: sa.Connection) -> None:
    missing_media_count = connection.scalar(
        sa.text(
            "SELECT COUNT(*) "
            "FROM image_session_assets AS asset "
            "LEFT JOIN media_objects AS media ON media.id = asset.media_object_id "
            "WHERE asset.media_object_id IS NULL OR media.id IS NULL"
        )
    )
    if missing_media_count:
        raise RuntimeError(
            "cannot enforce image_session_assets.media_object_id: "
            f"{missing_media_count} row(s) have no canonical MediaObject"
        )

    mismatched_carrier_count = connection.scalar(
        sa.text(
            "SELECT COUNT(*) "
            "FROM image_session_assets AS asset "
            "JOIN media_objects AS media ON media.id = asset.media_object_id "
            "WHERE asset.storage_path <> media.storage_path "
            "OR asset.mime_type <> media.mime_type"
        )
    )
    if mismatched_carrier_count:
        raise RuntimeError(
            "cannot enforce ImageSession media authority: "
            f"{mismatched_carrier_count} row(s) have path or MIME carrier drift"
        )


def _set_media_object_nullable(*, nullable: bool) -> None:
    bind = op.get_bind()
    if bind.dialect.name == "sqlite":
        with op.batch_alter_table(TABLE_NAME, recreate="always") as batch_op:
            batch_op.alter_column(
                MEDIA_OBJECT_ID_COLUMN,
                existing_type=sa.String(length=36),
                nullable=nullable,
            )
        return

    op.alter_column(
        TABLE_NAME,
        MEDIA_OBJECT_ID_COLUMN,
        existing_type=sa.String(length=36),
        nullable=nullable,
    )


def upgrade() -> None:
    _assert_canonical_media_coherence(op.get_bind())
    _set_media_object_nullable(nullable=False)


def downgrade() -> None:
    _set_media_object_nullable(nullable=True)
