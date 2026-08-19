"""add media_library_upload_keys table

Revision ID: 20260819_0061
Revises: 20260819_0060
Create Date: 2026-08-19
"""

from __future__ import annotations

import sqlalchemy as sa

from alembic import op

revision = "20260819_0061"
down_revision = "20260819_0060"
branch_labels = None
depends_on = None


def upgrade() -> None:
    op.create_table(
        "media_library_upload_keys",
        sa.Column("id", sa.String(length=36), primary_key=True),
        sa.Column("idempotency_key", sa.String(length=200), nullable=False),
        sa.Column("request_hash", sa.String(length=64), nullable=False),
        sa.Column("asset_ids_json", sa.Text(), nullable=False),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.UniqueConstraint("idempotency_key", name="uq_media_library_upload_keys_key"),
        sa.CheckConstraint("length(request_hash) = 64", name="ck_media_library_upload_keys_request_hash"),
    )


def downgrade() -> None:
    op.drop_table("media_library_upload_keys")
