"""canvas sessions belong to a product; global sessions have product_id null

Revision ID: 20260825_0092
Revises: 20260825_0091
Create Date: 2026-08-25
"""

from __future__ import annotations

import sqlalchemy as sa

from alembic import op

revision = "20260825_0092"
down_revision = "20260825_0091"
branch_labels = None
depends_on = None


def _alter_existing_table(table_name: str):
    bind = op.get_bind()
    return op.batch_alter_table(table_name, recreate="always" if bind.dialect.name == "sqlite" else "auto")


def upgrade() -> None:
    with _alter_existing_table("agent_sessions") as batch_op:
        batch_op.add_column(sa.Column("product_id", sa.String(length=36), nullable=True))
        batch_op.create_foreign_key(
            "fk_agent_sessions_product_id",
            "products",
            ["product_id"],
            ["id"],
            ondelete="SET NULL",
        )
        batch_op.create_index(
            "ix_agent_sessions_product_status_updated",
            ["product_id", "status", "updated_at", "id"],
        )
    op.execute(
        sa.text(
            """
            UPDATE agent_sessions
            SET product_id = (
                SELECT agent_conversations.product_id
                FROM agent_conversations
                WHERE agent_conversations.session_id = agent_sessions.id
                  AND agent_conversations.product_id IS NOT NULL
                ORDER BY agent_conversations.created_at DESC, agent_conversations.id DESC
                LIMIT 1
            )
            WHERE EXISTS (
                SELECT 1
                FROM agent_conversations
                WHERE agent_conversations.session_id = agent_sessions.id
                  AND agent_conversations.product_id IS NOT NULL
            )
            """
        )
    )


def downgrade() -> None:
    with _alter_existing_table("agent_sessions") as batch_op:
        batch_op.drop_index("ix_agent_sessions_product_status_updated")
        batch_op.drop_constraint("fk_agent_sessions_product_id", type_="foreignkey")
        batch_op.drop_column("product_id")
