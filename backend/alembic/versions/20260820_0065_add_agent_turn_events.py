"""add cross-instance Agent Turn event store

Revision ID: 20260820_0065
Revises: 20260820_0064
Create Date: 2026-08-20
"""

from __future__ import annotations

import sqlalchemy as sa

from alembic import op

revision = "20260820_0065"
down_revision = "20260820_0064"
branch_labels = None
depends_on = None


def upgrade() -> None:
    op.create_table(
        "agent_turn_events",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("turn_projection_id", sa.String(length=36), nullable=False),
        sa.Column("execution_id", sa.String(length=36), nullable=True),
        sa.Column("run_id", sa.String(length=120), nullable=False),
        sa.Column("turn_id", sa.String(length=120), nullable=False),
        sa.Column("schema_version", sa.Integer(), nullable=False, server_default="1"),
        sa.Column("sequence", sa.Integer(), nullable=False),
        sa.Column("attempt", sa.Integer(), nullable=True),
        sa.Column("fencing_token", sa.Integer(), nullable=True),
        sa.Column("kind", sa.String(length=120), nullable=False),
        sa.Column("payload_json", sa.JSON(), nullable=False),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.CheckConstraint("schema_version = 1", name="ck_agent_turn_events_schema_version"),
        sa.CheckConstraint("sequence > 0", name="ck_agent_turn_events_positive_sequence"),
        sa.CheckConstraint("attempt IS NULL OR attempt > 0", name="ck_agent_turn_events_attempt"),
        sa.CheckConstraint("fencing_token IS NULL OR fencing_token > 0", name="ck_agent_turn_events_fencing"),
        sa.ForeignKeyConstraint(
            ["turn_projection_id"],
            ["agent_turn_projections.id"],
            name="fk_agent_turn_events_turn_projection_id",
            ondelete="CASCADE",
        ),
        sa.ForeignKeyConstraint(
            ["execution_id"],
            ["agent_turn_executions.id"],
            name="fk_agent_turn_events_execution_id",
            ondelete="SET NULL",
        ),
        sa.PrimaryKeyConstraint("id"),
        sa.UniqueConstraint(
            "turn_projection_id",
            "sequence",
            name="uq_agent_turn_events_projection_sequence",
        ),
    )
    op.create_index(
        "ix_agent_turn_events_projection_sequence",
        "agent_turn_events",
        ["turn_projection_id", "sequence"],
    )


def downgrade() -> None:
    op.drop_index("ix_agent_turn_events_projection_sequence", table_name="agent_turn_events")
    op.drop_table("agent_turn_events")
