"""distinguish edit/undo/redo operation groups

Revision ID: 20260822_0081
Revises: 20260821_0080
Create Date: 2026-08-22
"""

from __future__ import annotations

import sqlalchemy as sa

from alembic import op

revision = "20260822_0081"
down_revision = "20260821_0080"
branch_labels = None
depends_on = None


def upgrade() -> None:
    with op.batch_alter_table("workflow_operation_groups") as batch_op:
        batch_op.add_column(
            sa.Column("history_kind", sa.String(length=16), nullable=False, server_default="edit"),
        )
        batch_op.create_check_constraint(
            "ck_workflow_operation_groups_history_kind",
            "history_kind IN ('edit', 'undo', 'redo')",
        )
    op.execute(
        sa.text(
            "UPDATE workflow_operation_groups SET history_kind = 'undo' WHERE summary LIKE '撤销：%'"
        )
    )


def downgrade() -> None:
    with op.batch_alter_table("workflow_operation_groups") as batch_op:
        batch_op.drop_constraint("ck_workflow_operation_groups_history_kind", type_="check")
        batch_op.drop_column("history_kind")
