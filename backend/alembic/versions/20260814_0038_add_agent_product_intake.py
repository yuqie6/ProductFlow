"""add Agent product creation intake

Revision ID: 20260814_0038
Revises: 20260814_0037
Create Date: 2026-08-14
"""

from __future__ import annotations

import sqlalchemy as sa

from alembic import op

revision = "20260814_0038"
down_revision = "20260814_0037"
branch_labels = None
depends_on = None


def _alter_existing_table(table_name: str):
    bind = op.get_bind()
    return op.batch_alter_table(table_name, recreate="always" if bind.dialect.name == "sqlite" else "auto")


def upgrade() -> None:
    with _alter_existing_table("workflow_drafts") as batch_op:
        batch_op.add_column(sa.Column("intake_schema_version", sa.Integer(), nullable=True))
        batch_op.add_column(sa.Column("intake_json", sa.JSON(), nullable=True))
        batch_op.create_check_constraint(
            "ck_workflow_drafts_intake_pair",
            "(intake_schema_version IS NULL AND intake_json IS NULL) OR "
            "(intake_schema_version = 1 AND intake_json IS NOT NULL)",
        )

    with _alter_existing_table("agent_conversations") as batch_op:
        batch_op.add_column(sa.Column("creation_idempotency_key", sa.String(length=200), nullable=True))
        batch_op.add_column(sa.Column("creation_request_hash", sa.String(length=64), nullable=True))
        batch_op.create_unique_constraint(
            "uq_agent_conversations_creation_idempotency_key",
            ["creation_idempotency_key"],
        )
        batch_op.create_check_constraint(
            "ck_agent_conversations_creation_idempotency_pair",
            "(creation_idempotency_key IS NULL AND creation_request_hash IS NULL) OR "
            "(creation_idempotency_key IS NOT NULL AND length(creation_idempotency_key) > 0 "
            "AND creation_request_hash IS NOT NULL AND length(creation_request_hash) = 64)",
        )


def downgrade() -> None:
    with _alter_existing_table("agent_conversations") as batch_op:
        batch_op.drop_constraint(
            "ck_agent_conversations_creation_idempotency_pair",
            type_="check",
        )
        batch_op.drop_constraint(
            "uq_agent_conversations_creation_idempotency_key",
            type_="unique",
        )
        batch_op.drop_column("creation_request_hash")
        batch_op.drop_column("creation_idempotency_key")

    with _alter_existing_table("workflow_drafts") as batch_op:
        batch_op.drop_constraint("ck_workflow_drafts_intake_pair", type_="check")
        batch_op.drop_column("intake_json")
        batch_op.drop_column("intake_schema_version")
