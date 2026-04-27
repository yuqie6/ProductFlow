"""add image session task tool options

Revision ID: 20260427_0015
Revises: 20260427_0014
Create Date: 2026-04-27
"""

from __future__ import annotations

import sqlalchemy as sa

from alembic import op

revision = "20260427_0015"
down_revision = "20260427_0014"
branch_labels = None
depends_on = None


def upgrade() -> None:
    op.add_column("image_session_generation_tasks", sa.Column("tool_options", sa.JSON(), nullable=True))


def downgrade() -> None:
    op.drop_column("image_session_generation_tasks", "tool_options")
