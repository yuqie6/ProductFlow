"""add source asset poster source metadata

Revision ID: 20260426_0012
Revises: 20260424_0011
Create Date: 2026-04-26
"""

import sqlalchemy as sa

from alembic import op

revision = "20260426_0012"
down_revision = "20260424_0011"
branch_labels = None
depends_on = None


def upgrade() -> None:
    op.add_column("source_assets", sa.Column("source_poster_variant_id", sa.String(length=36), nullable=True))
    op.create_index(
        "ix_source_assets_source_poster_variant_id",
        "source_assets",
        ["source_poster_variant_id"],
    )


def downgrade() -> None:
    op.drop_index("ix_source_assets_source_poster_variant_id", table_name="source_assets")
    op.drop_column("source_assets", "source_poster_variant_id")
