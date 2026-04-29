"""mark failed image session tasks retryable

Revision ID: 20260430_0019
Revises: 20260428_0018
Create Date: 2026-04-30
"""

from __future__ import annotations

from alembic import op

revision = "20260430_0019"
down_revision = "20260428_0018"
branch_labels = None
depends_on = None


def upgrade() -> None:
    op.execute(
        """
        UPDATE image_session_generation_tasks
        SET is_retryable = TRUE
        WHERE status = 'failed'
          AND is_retryable = FALSE
          AND completed_candidates < generation_count
        """
    )


def downgrade() -> None:
    pass
