"""add media library collection idempotency keys

Revision ID: 20260818_0056
Revises: 20260818_0055
Create Date: 2026-08-18
"""

from __future__ import annotations

import sqlalchemy as sa

from alembic import op

revision = "20260818_0056"
down_revision = "20260818_0055"
branch_labels = None
depends_on = None


def upgrade() -> None:
    op.create_table(
        "media_library_collection_keys",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("product_id", sa.String(length=36), nullable=False),
        sa.Column("idempotency_key", sa.String(length=200), nullable=False),
        sa.Column("request_hash", sa.String(length=64), nullable=False),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.CheckConstraint(
            "length(request_hash) = 64",
            name="ck_media_library_collection_keys_request_hash",
        ),
        sa.ForeignKeyConstraint(
            ["product_id"],
            ["products.id"],
            name="fk_media_library_collection_keys_product_id",
            ondelete="CASCADE",
        ),
        sa.PrimaryKeyConstraint("id"),
        sa.UniqueConstraint(
            "product_id",
            "idempotency_key",
            name="uq_media_library_collection_keys_product_key",
        ),
    )


def downgrade() -> None:
    op.drop_table("media_library_collection_keys")
