"""add durable Agent Turn execution leases

Revision ID: 20260820_0062
Revises: 20260819_0061
Create Date: 2026-08-20
"""

from __future__ import annotations

import sqlalchemy as sa
from sqlalchemy.dialects import postgresql

from alembic import op

revision = "20260820_0062"
down_revision = "20260819_0061"
branch_labels = None
depends_on = None


AGENT_EXECUTION_PHASE = postgresql.ENUM(
    "claimed",
    "model",
    "tool",
    "waiting_input",
    "external_job",
    "terminal",
    name="agentexecutionphase",
    create_type=False,
)


def upgrade() -> None:
    bind = op.get_bind()
    if bind.dialect.name == "postgresql":
        AGENT_EXECUTION_PHASE.create(bind, checkfirst=True)
        phase_type: sa.types.TypeEngine = AGENT_EXECUTION_PHASE
    else:
        phase_type = sa.String(length=32)

    op.create_table(
        "agent_turn_executions",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("turn_projection_id", sa.String(length=36), nullable=False),
        sa.Column("harness_turn_id", sa.String(length=120), nullable=False),
        sa.Column("owner_id", sa.String(length=120), nullable=True),
        sa.Column("lease_token", sa.String(length=36), nullable=True),
        sa.Column("lease_expires_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("attempt", sa.Integer(), nullable=False),
        sa.Column("fencing_token", sa.Integer(), nullable=False),
        sa.Column("phase", phase_type, nullable=False),
        sa.Column("last_heartbeat_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("released_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), nullable=False),
        sa.CheckConstraint("attempt >= 0", name="ck_agent_turn_executions_non_negative_attempt"),
        sa.CheckConstraint("fencing_token >= 0", name="ck_agent_turn_executions_non_negative_fencing"),
        sa.ForeignKeyConstraint(
            ["turn_projection_id"],
            ["agent_turn_projections.id"],
            name="fk_agent_turn_executions_turn_projection_id",
            ondelete="CASCADE",
        ),
        sa.PrimaryKeyConstraint("id"),
        sa.UniqueConstraint("turn_projection_id", name="uq_agent_turn_executions_projection_id"),
    )
    op.create_index(
        "ix_agent_turn_executions_lease",
        "agent_turn_executions",
        ["lease_expires_at", "id"],
    )
    op.create_index(
        "ix_agent_turn_executions_owner",
        "agent_turn_executions",
        ["owner_id", "lease_expires_at", "id"],
    )


def downgrade() -> None:
    op.drop_index("ix_agent_turn_executions_owner", table_name="agent_turn_executions")
    op.drop_index("ix_agent_turn_executions_lease", table_name="agent_turn_executions")
    op.drop_table("agent_turn_executions")
    bind = op.get_bind()
    if bind.dialect.name == "postgresql":
        AGENT_EXECUTION_PHASE.drop(bind, checkfirst=True)
