"""point workflow media library associations at schema-v3 graphs

Revision ID: 20260821_0079
Revises: 20260821_0078
Create Date: 2026-08-21
"""

from __future__ import annotations

import sqlalchemy as sa

from alembic import op

revision = "20260821_0079"
down_revision = "20260821_0078"
branch_labels = None
depends_on = None


def upgrade() -> None:
    op.execute(sa.text("DELETE FROM workflow_media_library_assets"))
    with op.batch_alter_table("workflow_media_library_assets") as batch_op:
        batch_op.drop_constraint("fk_workflow_media_library_assets_workflow_id", type_="foreignkey")
        batch_op.create_foreign_key(
            "fk_workflow_media_library_assets_workflow_id",
            "workflow_graphs",
            ["workflow_id"],
            ["id"],
            ondelete="CASCADE",
        )


def downgrade() -> None:
    bind = op.get_bind()
    graph_rows = bind.execute(sa.text("SELECT 1 FROM workflow_media_library_assets LIMIT 1")).first()
    if graph_rows is not None:
        raise RuntimeError("存在 schema-v3 工作流子图库关联，不能改回 product_workflows")
    with op.batch_alter_table("workflow_media_library_assets") as batch_op:
        batch_op.drop_constraint("fk_workflow_media_library_assets_workflow_id", type_="foreignkey")
        batch_op.create_foreign_key(
            "fk_workflow_media_library_assets_workflow_id",
            "product_workflows",
            ["workflow_id"],
            ["id"],
            ondelete="CASCADE",
        )
