"""add bounded Agent Session and Task summaries

Revision ID: 20260818_0058
Revises: 20260818_0057
Create Date: 2026-08-18

These fields are projections for the application UI and recovery context. The
durable harness journal remains the source for the complete transcript.
"""

from __future__ import annotations

import sqlalchemy as sa

from alembic import op

revision = "20260818_0058"
down_revision = "20260818_0057"
branch_labels = None
depends_on = None


def upgrade() -> None:
    op.add_column("agent_sessions", sa.Column("summary", sa.Text(), nullable=True))
    op.add_column("agent_tasks", sa.Column("summary", sa.Text(), nullable=True))


def downgrade() -> None:
    op.drop_column("agent_tasks", "summary")
    op.drop_column("agent_sessions", "summary")
