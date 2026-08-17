"""add global Agent sessions

Revision ID: 20260817_0050
Revises: 20260816_0049
Create Date: 2026-08-17
"""

from __future__ import annotations

from datetime import UTC, datetime
from uuid import uuid4

import sqlalchemy as sa
from sqlalchemy.dialects import postgresql

from alembic import op

revision = "20260817_0050"
down_revision = "20260816_0049"
branch_labels = None
depends_on = None

AGENT_SESSION_STATUS = postgresql.ENUM(
    "active",
    "archived",
    name="agentsessionstatus",
    create_type=False,
)


def _alter_existing_table(table_name: str):
    bind = op.get_bind()
    return op.batch_alter_table(table_name, recreate="always" if bind.dialect.name == "sqlite" else "auto")


def _create_enum() -> None:
    AGENT_SESSION_STATUS.create(op.get_bind(), checkfirst=True)


def upgrade() -> None:
    _create_enum()
    op.create_table(
        "agent_sessions",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("title", sa.String(length=160), nullable=False),
        sa.Column("status", AGENT_SESSION_STATUS, nullable=False),
        sa.Column("archived_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), nullable=False),
        sa.PrimaryKeyConstraint("id"),
    )
    op.create_index(
        "ix_agent_sessions_status_updated",
        "agent_sessions",
        ["status", "updated_at", "id"],
    )

    with _alter_existing_table("agent_conversations") as batch_op:
        batch_op.add_column(sa.Column("session_id", sa.String(length=36), nullable=True))
        batch_op.create_foreign_key(
            "fk_agent_conversations_session_id",
            "agent_sessions",
            ["session_id"],
            ["id"],
            ondelete="SET NULL",
        )
        batch_op.create_index(
            "ix_agent_conversations_session_updated",
            ["session_id", "updated_at", "id"],
        )

    bind = op.get_bind()
    now = datetime.now(UTC)
    rows = bind.execute(
        sa.text(
            "SELECT c.product_id, p.name "
            "FROM agent_conversations AS c "
            "JOIN products AS p ON p.id = c.product_id "
            "WHERE c.session_id IS NULL "
            "GROUP BY c.product_id, p.name"
        )
    ).mappings()
    sessions_by_product: dict[str, str] = {}
    for row in rows:
        session_id = str(uuid4())
        sessions_by_product[row["product_id"]] = session_id
        bind.execute(
            sa.text(
                "INSERT INTO agent_sessions "
                "(id, title, status, archived_at, created_at, updated_at) "
                "VALUES (:id, :title, 'active', NULL, :created_at, :updated_at)"
            ),
            {
                "id": session_id,
                "title": str(row["name"])[:160],
                "created_at": now,
                "updated_at": now,
            },
        )
    for product_id, session_id in sessions_by_product.items():
        bind.execute(
            sa.text("UPDATE agent_conversations SET session_id = :session_id WHERE product_id = :product_id"),
            {"session_id": session_id, "product_id": product_id},
        )


def downgrade() -> None:
    with _alter_existing_table("agent_conversations") as batch_op:
        batch_op.drop_index("ix_agent_conversations_session_updated")
        batch_op.drop_constraint("fk_agent_conversations_session_id", type_="foreignkey")
        batch_op.drop_column("session_id")
    op.drop_index("ix_agent_sessions_status_updated", table_name="agent_sessions")
    op.drop_table("agent_sessions")
    bind = op.get_bind()
    if bind.dialect.name == "postgresql":
        AGENT_SESSION_STATUS.drop(bind, checkfirst=True)
