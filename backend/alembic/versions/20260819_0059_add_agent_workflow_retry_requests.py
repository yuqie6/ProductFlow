"""add source_run_id to agent workflow run requests

Revision ID: 20260819_0059
Revises: 20260818_0058
Create Date: 2026-08-19
"""

from __future__ import annotations

import sqlalchemy as sa

from alembic import op

revision = "20260819_0059"
down_revision = "20260818_0058"
branch_labels = None
depends_on = None


def _alter_existing_table(table_name: str):
    bind = op.get_bind()
    return op.batch_alter_table(table_name, recreate="always" if bind.dialect.name == "sqlite" else "auto")


def upgrade() -> None:
    with _alter_existing_table("agent_workflow_run_requests") as batch_op:
        batch_op.add_column(
            sa.Column("source_run_id", sa.String(length=36), nullable=True),
        )
        batch_op.create_foreign_key(
            "fk_agent_workflow_run_requests_source_run_id",
            "workflow_runs",
            ["source_run_id"],
            ["id"],
            ondelete="RESTRICT",
        )
        batch_op.create_index(
            "ix_agent_workflow_run_requests_source_run_id",
            ["source_run_id"],
        )


def downgrade() -> None:
    with _alter_existing_table("agent_workflow_run_requests") as batch_op:
        batch_op.drop_index("ix_agent_workflow_run_requests_source_run_id")
        batch_op.drop_constraint(
            "fk_agent_workflow_run_requests_source_run_id",
            type_="foreignkey",
        )
        batch_op.drop_column("source_run_id")
