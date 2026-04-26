"""add image session branching metadata

Revision ID: 20260427_0013
Revises: 20260426_0012
Create Date: 2026-04-27
"""

import sqlalchemy as sa

from alembic import op

revision = "20260427_0013"
down_revision = "20260426_0012"
branch_labels = None
depends_on = None


def upgrade() -> None:
    with op.batch_alter_table("image_session_rounds") as batch_op:
        batch_op.add_column(sa.Column("generation_group_id", sa.String(length=36), nullable=True))
        batch_op.add_column(sa.Column("candidate_index", sa.Integer(), nullable=False, server_default="1"))
        batch_op.add_column(sa.Column("candidate_count", sa.Integer(), nullable=False, server_default="1"))
        batch_op.add_column(
            sa.Column(
                "base_asset_id",
                sa.String(length=36),
                sa.ForeignKey(
                    "image_session_assets.id",
                    name="fk_image_session_rounds_base_asset_id",
                    ondelete="SET NULL",
                ),
                nullable=True,
            )
        )
        batch_op.add_column(sa.Column("selected_reference_asset_ids", sa.JSON(), nullable=True))
        batch_op.alter_column(
            "candidate_index",
            existing_type=sa.Integer(),
            nullable=False,
            server_default=None,
        )
        batch_op.alter_column(
            "candidate_count",
            existing_type=sa.Integer(),
            nullable=False,
            server_default=None,
        )
    op.create_index(
        "ix_image_session_rounds_generation_group_id",
        "image_session_rounds",
        ["generation_group_id"],
    )
    op.create_index("ix_image_session_rounds_base_asset_id", "image_session_rounds", ["base_asset_id"])


def downgrade() -> None:
    op.drop_index("ix_image_session_rounds_base_asset_id", table_name="image_session_rounds")
    op.drop_index("ix_image_session_rounds_generation_group_id", table_name="image_session_rounds")
    with op.batch_alter_table("image_session_rounds") as batch_op:
        batch_op.drop_column("selected_reference_asset_ids")
        batch_op.drop_column("base_asset_id")
        batch_op.drop_column("candidate_count")
        batch_op.drop_column("candidate_index")
        batch_op.drop_column("generation_group_id")
