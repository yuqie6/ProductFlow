"""allow Agent conversations to use an explicit global scope

Revision ID: 20260817_0053
Revises: 20260817_0052
Create Date: 2026-08-17
"""

from __future__ import annotations

import sqlalchemy as sa
from sqlalchemy.dialects import postgresql

from alembic import op

revision = "20260817_0053"
down_revision = "20260817_0052"
branch_labels = None
depends_on = None

AGENT_CONVERSATION_SCOPE = postgresql.ENUM(
    "product_workflow",
    "global",
    name="agentconversationscope",
    create_type=False,
)


def _alter_existing_table(table_name: str):
    bind = op.get_bind()
    return op.batch_alter_table(table_name, recreate="always" if bind.dialect.name == "sqlite" else "auto")


def upgrade() -> None:
    AGENT_CONVERSATION_SCOPE.create(op.get_bind(), checkfirst=True)
    with _alter_existing_table("agent_conversations") as batch_op:
        batch_op.add_column(
            sa.Column(
                "scope_type",
                AGENT_CONVERSATION_SCOPE,
                nullable=True,
                server_default="product_workflow",
            )
        )
        batch_op.alter_column("product_id", existing_type=sa.String(length=36), nullable=True)
        batch_op.alter_column("workflow_draft_id", existing_type=sa.String(length=36), nullable=True)

    op.execute(
        sa.text(
            "UPDATE agent_conversations "
            "SET scope_type = 'product_workflow' "
            "WHERE scope_type IS NULL"
        )
    )

    with _alter_existing_table("agent_conversations") as batch_op:
        batch_op.alter_column(
            "scope_type",
            existing_type=AGENT_CONVERSATION_SCOPE,
            nullable=False,
            server_default=None,
        )
        batch_op.create_check_constraint(
            "ck_agent_conversations_scope_fields",
            "(scope_type = 'product_workflow' AND product_id IS NOT NULL AND workflow_draft_id IS NOT NULL) "
            "OR (scope_type = 'global' AND product_id IS NULL AND workflow_draft_id IS NULL)",
        )
        batch_op.create_check_constraint(
            "ck_agent_conversations_scope_type",
            "scope_type IN ('product_workflow', 'global')",
        )
        batch_op.create_index(
            "ix_agent_conversations_scope_updated",
            ["scope_type", "updated_at", "id"],
        )
        batch_op.create_index(
            "ux_agent_conversations_session_global",
            ["session_id"],
            unique=True,
            postgresql_where=sa.text("scope_type = 'global'"),
            sqlite_where=sa.text("scope_type = 'global'"),
        )
        batch_op.alter_column("scope_type", server_default=None)


def downgrade() -> None:
    bind = op.get_bind()
    global_count = bind.execute(
        sa.text("SELECT COUNT(*) FROM agent_conversations WHERE scope_type = 'global'")
    ).scalar_one()
    if global_count:
        raise RuntimeError("cannot downgrade while global Agent conversations exist")
    with _alter_existing_table("agent_conversations") as batch_op:
        batch_op.drop_index("ux_agent_conversations_session_global")
        batch_op.drop_index("ix_agent_conversations_scope_updated")
        batch_op.drop_constraint("ck_agent_conversations_scope_type", type_="check")
        batch_op.drop_constraint("ck_agent_conversations_scope_fields", type_="check")
        batch_op.alter_column("product_id", existing_type=sa.String(length=36), nullable=False)
        batch_op.alter_column("workflow_draft_id", existing_type=sa.String(length=36), nullable=False)
        batch_op.drop_column("scope_type")
    AGENT_CONVERSATION_SCOPE.drop(bind, checkfirst=True)
