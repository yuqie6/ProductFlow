"""add unique running workflow run constraint

Revision ID: 20260424_0011
Revises: 20260424_0010
Create Date: 2026-04-24
"""

import sqlalchemy as sa

from alembic import op

revision = "20260424_0011"
down_revision = "20260424_0010"
branch_labels = None
depends_on = None


def upgrade() -> None:
    op.execute(
        """
        UPDATE workflow_runs
        SET status = 'failed',
            finished_at = CURRENT_TIMESTAMP,
            failure_reason = COALESCE(failure_reason, '重复运行已关闭')
        WHERE id IN (
            SELECT id
            FROM (
                SELECT
                    id,
                    ROW_NUMBER() OVER (
                        PARTITION BY workflow_id
                        ORDER BY started_at DESC, id DESC
                    ) AS duplicate_rank
                FROM workflow_runs
                WHERE status = 'running'
            ) ranked_running_runs
            WHERE duplicate_rank > 1
        )
        """
    )
    op.create_index(
        "uq_workflow_runs_one_running_per_workflow",
        "workflow_runs",
        ["workflow_id"],
        unique=True,
        postgresql_where=sa.text("status = 'running'"),
        sqlite_where=sa.text("status = 'running'"),
    )


def downgrade() -> None:
    op.drop_index("uq_workflow_runs_one_running_per_workflow", table_name="workflow_runs")
