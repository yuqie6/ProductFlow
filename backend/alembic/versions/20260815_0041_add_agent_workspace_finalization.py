"""add Agent workspace intake finalization identity

Revision ID: 20260815_0041
Revises: 20260815_0040
Create Date: 2026-08-15
"""

from __future__ import annotations

import sqlalchemy as sa

from alembic import op

revision = "20260815_0041"
down_revision = "20260815_0040"
branch_labels = None
depends_on = None


def _alter_agent_conversations():
    bind = op.get_bind()
    return op.batch_alter_table(
        "agent_conversations",
        recreate="always" if bind.dialect.name == "sqlite" else "auto",
    )


def upgrade() -> None:
    with _alter_agent_conversations() as batch_op:
        batch_op.add_column(sa.Column("intake_idempotency_key", sa.String(length=200), nullable=True))
        batch_op.add_column(sa.Column("intake_request_hash", sa.String(length=64), nullable=True))
        batch_op.create_check_constraint(
            "ck_agent_conversations_intake_idempotency_pair",
            "(intake_idempotency_key IS NULL AND intake_request_hash IS NULL) OR "
            "(intake_idempotency_key IS NOT NULL AND length(intake_idempotency_key) > 0 "
            "AND intake_request_hash IS NOT NULL AND length(intake_request_hash) = 64)",
        )


def downgrade() -> None:
    bind = op.get_bind()
    finalized_count = bind.execute(
        sa.text(
            "SELECT COUNT(*) FROM agent_conversations "
            "WHERE intake_idempotency_key IS NOT NULL OR intake_request_hash IS NOT NULL"
        )
    ).scalar_one()
    if finalized_count > 0:
        raise RuntimeError("cannot downgrade Agent workspace finalization while intake identity exists")
    with _alter_agent_conversations() as batch_op:
        batch_op.drop_constraint(
            "ck_agent_conversations_intake_idempotency_pair",
            type_="check",
        )
        batch_op.drop_column("intake_request_hash")
        batch_op.drop_column("intake_idempotency_key")
