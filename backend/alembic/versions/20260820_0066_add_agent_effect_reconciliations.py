"""persist explicit Agent effect reconciliation verdicts

Revision ID: 20260820_0066
Revises: 20260820_0065
Create Date: 2026-08-20
"""

from __future__ import annotations

import sqlalchemy as sa

from alembic import op

revision = "20260820_0066"
down_revision = "20260820_0065"
branch_labels = None
depends_on = None


def upgrade() -> None:
    op.create_table(
        "agent_turn_effect_reconciliations",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("turn_projection_id", sa.String(length=36), nullable=False),
        sa.Column("tool_call_id", sa.String(length=120), nullable=False),
        sa.Column("tool_name", sa.String(length=120), nullable=False),
        sa.Column("idempotency_key", sa.String(length=200), nullable=False),
        sa.Column("effect_result", sa.String(length=20), nullable=False),
        sa.Column("reconciliation_state", sa.String(length=20), nullable=False),
        sa.Column("result_json", sa.JSON(), nullable=True),
        sa.Column("detail", sa.Text(), nullable=True),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), nullable=False),
        sa.CheckConstraint(
            "effect_result IN ('applied', 'failed', 'unknown')",
            name="ck_agent_turn_effect_reconciliations_effect_result",
        ),
        sa.CheckConstraint(
            "reconciliation_state IN ('applied', 'not_applied', 'conflict', 'unknown')",
            name="ck_agent_turn_effect_reconciliations_state",
        ),
        sa.ForeignKeyConstraint(
            ["turn_projection_id"],
            ["agent_turn_projections.id"],
            name="fk_agent_turn_effect_reconciliations_turn_projection_id",
            ondelete="CASCADE",
        ),
        sa.PrimaryKeyConstraint("id"),
        sa.UniqueConstraint(
            "turn_projection_id",
            "tool_call_id",
            name="uq_agent_turn_effect_reconciliations_projection_tool",
        ),
    )
    op.create_index(
        "ix_agent_turn_effect_reconciliations_projection_created",
        "agent_turn_effect_reconciliations",
        ["turn_projection_id", "created_at", "id"],
    )


def downgrade() -> None:
    op.drop_index(
        "ix_agent_turn_effect_reconciliations_projection_created",
        table_name="agent_turn_effect_reconciliations",
    )
    op.drop_table("agent_turn_effect_reconciliations")
