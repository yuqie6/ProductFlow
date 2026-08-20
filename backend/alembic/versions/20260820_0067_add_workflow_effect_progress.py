"""persist WorkflowNodeRun provider effect boundaries

Revision ID: 20260820_0067
Revises: 20260820_0066
Create Date: 2026-08-20
"""

from __future__ import annotations

import sqlalchemy as sa

from alembic import op

revision = "20260820_0067"
down_revision = "20260820_0066"
branch_labels = None
depends_on = None


def upgrade() -> None:
    bind = op.get_bind()
    if bind.dialect.name == "postgresql":
        with op.get_context().autocommit_block():
            op.execute("ALTER TYPE workflownodestatus ADD VALUE IF NOT EXISTS 'unknown'")
            op.execute("ALTER TYPE workflowrunstatus ADD VALUE IF NOT EXISTS 'unknown'")
    op.add_column("workflow_node_runs", sa.Column("progress_phase", sa.String(length=64), nullable=True))
    op.add_column("workflow_node_runs", sa.Column("progress_metadata", sa.JSON(), nullable=True))


def downgrade() -> None:
    # PostgreSQL enum values cannot be safely removed without rebuilding the type.
    op.drop_column("workflow_node_runs", "progress_metadata")
    op.drop_column("workflow_node_runs", "progress_phase")
