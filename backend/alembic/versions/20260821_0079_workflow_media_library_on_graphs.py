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
    bind = op.get_bind()
    ambiguous = bind.execute(
        sa.text(
            """
            SELECT
                links.workflow_id AS source_workflow_id,
                links.media_library_asset_id AS media_library_asset_id
            FROM workflow_media_library_assets AS links
            LEFT JOIN product_workflows AS workflows
                ON workflows.id = links.workflow_id
            LEFT JOIN workflow_graphs AS graphs
                ON graphs.product_id = workflows.product_id
                AND graphs.active = true
            GROUP BY links.workflow_id, links.media_library_asset_id
            HAVING count(graphs.id) <> 1
            LIMIT 1
            """
        )
    ).first()
    if ambiguous is not None:
        raise RuntimeError(
            "存在无法唯一映射到 active schema-v3 graph 的工作流子图库关联；"
            "升级已停止且不会删除原关联"
        )

    duplicate_target = bind.execute(
        sa.text(
            """
            SELECT
                graphs.id AS target_graph_id,
                links.media_library_asset_id AS media_library_asset_id
            FROM workflow_media_library_assets AS links
            JOIN product_workflows AS workflows
                ON workflows.id = links.workflow_id
            JOIN workflow_graphs AS graphs
                ON graphs.product_id = workflows.product_id
                AND graphs.active = true
            GROUP BY graphs.id, links.media_library_asset_id
            HAVING count(*) > 1
            LIMIT 1
            """
        )
    ).first()
    if duplicate_target is not None:
        raise RuntimeError(
            "多个旧工作流子图库关联会映射到同一 schema-v3 graph 关联；"
            "升级已停止且不会合并原关联"
        )

    # Split the constraint replacement so rows can be deterministically retargeted
    # between the old and new owners on both PostgreSQL and SQLite.
    with op.batch_alter_table("workflow_media_library_assets") as batch_op:
        batch_op.drop_constraint("fk_workflow_media_library_assets_workflow_id", type_="foreignkey")

    bind.execute(
        sa.text(
            """
            UPDATE workflow_media_library_assets
            SET workflow_id = (
                SELECT graphs.id
                FROM product_workflows AS workflows
                JOIN workflow_graphs AS graphs
                    ON graphs.product_id = workflows.product_id
                    AND graphs.active = true
                WHERE workflows.id = workflow_media_library_assets.workflow_id
            )
            """
        )
    )

    with op.batch_alter_table("workflow_media_library_assets") as batch_op:
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
