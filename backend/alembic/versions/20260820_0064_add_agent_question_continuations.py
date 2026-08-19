"""persist Agent question answers and continuation Turn bindings

Revision ID: 20260820_0064
Revises: 20260820_0063
Create Date: 2026-08-20
"""

from __future__ import annotations

import sqlalchemy as sa

from alembic import op

revision = "20260820_0064"
down_revision = "20260820_0063"
branch_labels = None
depends_on = None


def upgrade() -> None:
    op.add_column(
        "agent_turn_projections",
        sa.Column("question_answer_json", sa.JSON(), nullable=True),
    )
    op.add_column(
        "agent_turn_projections",
        sa.Column("continuation_turn_id", sa.String(length=36), nullable=True),
    )
    op.create_index(
        "ix_agent_turn_projections_continuation_turn",
        "agent_turn_projections",
        ["continuation_turn_id"],
    )


def downgrade() -> None:
    op.drop_index(
        "ix_agent_turn_projections_continuation_turn",
        table_name="agent_turn_projections",
    )
    op.drop_column("agent_turn_projections", "continuation_turn_id")
    op.drop_column("agent_turn_projections", "question_answer_json")
