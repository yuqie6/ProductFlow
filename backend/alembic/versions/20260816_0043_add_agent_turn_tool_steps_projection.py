"""add bounded tool-step snapshots to Agent Turn projections

Revision ID: 20260816_0043
Revises: 20260816_0042
Create Date: 2026-08-16
"""

from __future__ import annotations

import sqlalchemy as sa

from alembic import op

revision = "20260816_0043"
down_revision = "20260816_0042"
branch_labels = None
depends_on = None


def upgrade() -> None:
    op.add_column(
        "agent_turn_projections",
        sa.Column(
            "tool_steps_json",
            sa.JSON(),
            nullable=False,
            server_default=sa.text("'[]'"),
        ),
    )


def downgrade() -> None:
    op.drop_column("agent_turn_projections", "tool_steps_json")
