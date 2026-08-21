"""keep graph run history when a live node is deleted

Revision ID: 20260821_0077
Revises: 20260821_0076
Create Date: 2026-08-21
"""

from __future__ import annotations

import sqlalchemy as sa

from alembic import op

revision = "20260821_0077"
down_revision = "20260821_0076"
branch_labels = None
depends_on = None


def upgrade() -> None:
    with op.batch_alter_table("workflow_graph_artifacts") as batch_op:
        batch_op.drop_constraint("fk_workflow_graph_artifacts_node_id", type_="foreignkey")
        batch_op.alter_column("node_id", existing_type=sa.String(length=36), nullable=True)
        batch_op.create_foreign_key(
            "fk_workflow_graph_artifacts_node_id",
            "workflow_graph_nodes",
            ["node_id"],
            ["id"],
            ondelete="SET NULL",
        )
    with op.batch_alter_table("workflow_graph_node_runs") as batch_op:
        batch_op.drop_constraint("fk_workflow_graph_node_runs_node_id", type_="foreignkey")
        batch_op.alter_column("node_id", existing_type=sa.String(length=36), nullable=True)
        batch_op.create_foreign_key(
            "fk_workflow_graph_node_runs_node_id",
            "workflow_graph_nodes",
            ["node_id"],
            ["id"],
            ondelete="SET NULL",
        )


def downgrade() -> None:
    bind = op.get_bind()
    orphan_artifacts = bind.execute(
        sa.text("SELECT 1 FROM workflow_graph_artifacts WHERE node_id IS NULL LIMIT 1")
    ).first()
    orphan_runs = bind.execute(
        sa.text("SELECT 1 FROM workflow_graph_node_runs WHERE node_id IS NULL LIMIT 1")
    ).first()
    if orphan_artifacts is not None or orphan_runs is not None:
        raise RuntimeError("cannot restore CASCADE node FKs while graph history rows have null node_id")
    with op.batch_alter_table("workflow_graph_node_runs") as batch_op:
        batch_op.drop_constraint("fk_workflow_graph_node_runs_node_id", type_="foreignkey")
        batch_op.alter_column("node_id", existing_type=sa.String(length=36), nullable=False)
        batch_op.create_foreign_key(
            "fk_workflow_graph_node_runs_node_id",
            "workflow_graph_nodes",
            ["node_id"],
            ["id"],
            ondelete="CASCADE",
        )
    with op.batch_alter_table("workflow_graph_artifacts") as batch_op:
        batch_op.drop_constraint("fk_workflow_graph_artifacts_node_id", type_="foreignkey")
        batch_op.alter_column("node_id", existing_type=sa.String(length=36), nullable=False)
        batch_op.create_foreign_key(
            "fk_workflow_graph_artifacts_node_id",
            "workflow_graph_nodes",
            ["node_id"],
            ["id"],
            ondelete="CASCADE",
        )
