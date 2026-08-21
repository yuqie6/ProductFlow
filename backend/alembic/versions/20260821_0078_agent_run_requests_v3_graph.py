"""let Agent workflow run requests target schema-v3 graphs

Revision ID: 20260821_0078
Revises: 20260821_0077
Create Date: 2026-08-21
"""

from __future__ import annotations

import sqlalchemy as sa

from alembic import op

revision = "20260821_0078"
down_revision = "20260821_0077"
branch_labels = None
depends_on = None


def upgrade() -> None:
    with op.batch_alter_table("agent_workflow_run_requests") as batch_op:
        batch_op.alter_column("workflow_id", existing_type=sa.String(length=36), nullable=True)
        batch_op.add_column(sa.Column("graph_id", sa.String(length=36), nullable=True))
        batch_op.add_column(sa.Column("graph_run_id", sa.String(length=36), nullable=True))
        batch_op.add_column(sa.Column("source_graph_run_id", sa.String(length=36), nullable=True))
        batch_op.create_foreign_key(
            "fk_agent_workflow_run_requests_graph_id",
            "workflow_graphs",
            ["graph_id"],
            ["id"],
            ondelete="CASCADE",
        )
        batch_op.create_foreign_key(
            "fk_agent_workflow_run_requests_graph_run_id",
            "workflow_graph_runs",
            ["graph_run_id"],
            ["id"],
            ondelete="SET NULL",
        )
        batch_op.create_foreign_key(
            "fk_agent_workflow_run_requests_source_graph_run_id",
            "workflow_graph_runs",
            ["source_graph_run_id"],
            ["id"],
            ondelete="RESTRICT",
        )
        batch_op.create_check_constraint(
            "ck_agent_workflow_run_requests_v2_or_v3",
            "(workflow_id IS NULL AND graph_id IS NOT NULL) OR "
            "(workflow_id IS NOT NULL AND graph_id IS NULL)",
        )


def downgrade() -> None:
    bind = op.get_bind()
    v3_rows = bind.execute(
        sa.text("SELECT 1 FROM agent_workflow_run_requests WHERE workflow_id IS NULL LIMIT 1")
    ).first()
    if v3_rows is not None:
        raise RuntimeError("存在只绑定 schema-v3 图的 Agent 运行请求，不能回退到 workflow_id NOT NULL")
    with op.batch_alter_table("agent_workflow_run_requests") as batch_op:
        batch_op.drop_constraint("ck_agent_workflow_run_requests_v2_or_v3", type_="check")
        batch_op.drop_constraint("fk_agent_workflow_run_requests_source_graph_run_id", type_="foreignkey")
        batch_op.drop_constraint("fk_agent_workflow_run_requests_graph_run_id", type_="foreignkey")
        batch_op.drop_constraint("fk_agent_workflow_run_requests_graph_id", type_="foreignkey")
        batch_op.drop_column("source_graph_run_id")
        batch_op.drop_column("graph_run_id")
        batch_op.drop_column("graph_id")
        batch_op.alter_column("workflow_id", existing_type=sa.String(length=36), nullable=False)
