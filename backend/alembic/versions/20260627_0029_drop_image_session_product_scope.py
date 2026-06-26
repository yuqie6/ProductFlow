"""drop image session product scope and workflow node asset link

Revision ID: 20260627_0029
Revises: 20260513_0028
Create Date: 2026-06-27
"""

from __future__ import annotations

import sqlalchemy as sa
from sqlalchemy import inspect

from alembic import op

revision = "20260627_0029"
down_revision = "20260513_0028"
branch_labels = None
depends_on = None


def _columns(table_name: str) -> set[str]:
    return {column["name"] for column in inspect(op.get_bind()).get_columns(table_name)}


def upgrade() -> None:
    if "image_session_asset_id" in _columns("workflow_node_runs"):
        with op.batch_alter_table("workflow_node_runs") as batch_op:
            batch_op.drop_column("image_session_asset_id")

    if "product_id" in _columns("image_sessions"):
        with op.batch_alter_table("image_sessions") as batch_op:
            batch_op.drop_column("product_id")


def downgrade() -> None:
    if "product_id" not in _columns("image_sessions"):
        with op.batch_alter_table("image_sessions") as batch_op:
            batch_op.add_column(
                sa.Column(
                    "product_id",
                    sa.String(length=36),
                    sa.ForeignKey("products.id", ondelete="CASCADE", name="fk_image_sessions_product_id"),
                    nullable=True,
                )
            )

    if "image_session_asset_id" not in _columns("workflow_node_runs"):
        with op.batch_alter_table("workflow_node_runs") as batch_op:
            batch_op.add_column(
                sa.Column(
                    "image_session_asset_id",
                    sa.String(length=36),
                    sa.ForeignKey(
                        "image_session_assets.id",
                        ondelete="SET NULL",
                        name="fk_workflow_node_runs_image_session_asset_id",
                    ),
                    nullable=True,
                )
            )
