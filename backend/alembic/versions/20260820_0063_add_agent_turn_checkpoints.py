"""add semantic Agent Turn checkpoints

Revision ID: 20260820_0063
Revises: 20260820_0062
Create Date: 2026-08-20
"""

from __future__ import annotations

import sqlalchemy as sa
from sqlalchemy.dialects import postgresql

from alembic import op

revision = "20260820_0063"
down_revision = "20260820_0062"
branch_labels = None
depends_on = None


AGENT_CHECKPOINT_KIND = postgresql.ENUM(
    "before_model_request",
    "tool_effect_intent",
    "tool_effect_result",
    "question_required",
    "external_job_submitted",
    "terminal",
    name="agentcheckpointkind",
    create_type=False,
)


def upgrade() -> None:
    bind = op.get_bind()
    if bind.dialect.name == "postgresql":
        AGENT_CHECKPOINT_KIND.create(bind, checkfirst=True)
        kind_type: sa.types.TypeEngine = AGENT_CHECKPOINT_KIND
    else:
        kind_type = sa.String(length=40)

    op.add_column(
        "agent_turn_executions",
        sa.Column("last_checkpoint_sequence", sa.Integer(), nullable=False, server_default="0"),
    )
    op.add_column(
        "agent_turn_executions",
        sa.Column("last_checkpoint_at", sa.DateTime(timezone=True), nullable=True),
    )
    op.create_table(
        "agent_turn_checkpoints",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("turn_projection_id", sa.String(length=36), nullable=False),
        sa.Column("execution_id", sa.String(length=36), nullable=False),
        sa.Column("attempt", sa.Integer(), nullable=False),
        sa.Column("fencing_token", sa.Integer(), nullable=False),
        sa.Column("sequence", sa.Integer(), nullable=False),
        sa.Column("kind", kind_type, nullable=False),
        sa.Column("payload_json", sa.JSON(), nullable=False),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.CheckConstraint("attempt > 0", name="ck_agent_turn_checkpoints_positive_attempt"),
        sa.CheckConstraint("sequence > 0", name="ck_agent_turn_checkpoints_positive_sequence"),
        sa.CheckConstraint("fencing_token > 0", name="ck_agent_turn_checkpoints_positive_fencing"),
        sa.ForeignKeyConstraint(
            ["turn_projection_id"],
            ["agent_turn_projections.id"],
            name="fk_agent_turn_checkpoints_turn_projection_id",
            ondelete="CASCADE",
        ),
        sa.ForeignKeyConstraint(
            ["execution_id"],
            ["agent_turn_executions.id"],
            name="fk_agent_turn_checkpoints_execution_id",
            ondelete="CASCADE",
        ),
        sa.PrimaryKeyConstraint("id"),
        sa.UniqueConstraint(
            "execution_id",
            "attempt",
            "sequence",
            name="uq_agent_turn_checkpoints_execution_attempt_sequence",
        ),
    )
    op.create_index(
        "ix_agent_turn_checkpoints_projection_sequence",
        "agent_turn_checkpoints",
        ["turn_projection_id", "sequence"],
    )


def downgrade() -> None:
    op.drop_index("ix_agent_turn_checkpoints_projection_sequence", table_name="agent_turn_checkpoints")
    op.drop_table("agent_turn_checkpoints")
    op.drop_column("agent_turn_executions", "last_checkpoint_at")
    op.drop_column("agent_turn_executions", "last_checkpoint_sequence")
    bind = op.get_bind()
    if bind.dialect.name == "postgresql":
        AGENT_CHECKPOINT_KIND.drop(bind, checkfirst=True)
