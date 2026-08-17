"""add async dispatches

Revision ID: 20260816_0046
Revises: 20260816_0045
Create Date: 2026-08-16
"""

from __future__ import annotations

import sqlalchemy as sa
from sqlalchemy.dialects import postgresql

from alembic import op

revision = "20260816_0046"
down_revision = "20260816_0045"
branch_labels = None
depends_on = None


ASYNC_DISPATCH_STATUS = postgresql.ENUM(
    "pending",
    "sent",
    "consumed",
    "dead",
    name="asyncdispatchstatus",
    create_type=False,
)


def upgrade() -> None:
    bind = op.get_bind()
    if bind.dialect.name == "postgresql":
        ASYNC_DISPATCH_STATUS.create(bind, checkfirst=True)
        status_type: sa.types.TypeEngine = ASYNC_DISPATCH_STATUS
    else:
        status_type = sa.String(length=16)

    op.create_table(
        "async_dispatches",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("delivery_key", sa.String(length=255), nullable=False),
        sa.Column("actor_name", sa.String(length=120), nullable=False),
        sa.Column("aggregate_id", sa.String(length=36), nullable=False),
        sa.Column("payload_json", sa.JSON(), nullable=True),
        sa.Column("status", status_type, nullable=False),
        sa.Column("available_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("lease_token", sa.String(length=36), nullable=True),
        sa.Column("lease_expires_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("attempts", sa.Integer(), nullable=False),
        sa.Column("last_error", sa.Text(), nullable=True),
        sa.Column("sent_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("consumed_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), nullable=False),
        sa.CheckConstraint(
            "status IN ('pending', 'sent', 'consumed', 'dead')",
            name="ck_async_dispatches_status",
        ),
        sa.CheckConstraint(
            "attempts >= 0",
            name="ck_async_dispatches_non_negative_attempts",
        ),
        sa.PrimaryKeyConstraint("id"),
        sa.UniqueConstraint("delivery_key", name="uq_async_dispatches_delivery_key"),
    )
    op.create_index(
        "ix_async_dispatches_status_available",
        "async_dispatches",
        ["status", "available_at", "id"],
    )
    op.create_index(
        "ix_async_dispatches_lease_expiry",
        "async_dispatches",
        ["status", "lease_expires_at", "id"],
    )


def downgrade() -> None:
    bind = op.get_bind()
    if bind.execute(sa.text("SELECT 1 FROM async_dispatches LIMIT 1")).first() is not None:
        raise RuntimeError("cannot downgrade async dispatches while dispatch rows exist")
    op.drop_index("ix_async_dispatches_lease_expiry", table_name="async_dispatches")
    op.drop_index("ix_async_dispatches_status_available", table_name="async_dispatches")
    op.drop_table("async_dispatches")
    if bind.dialect.name == "postgresql":
        ASYNC_DISPATCH_STATUS.drop(bind, checkfirst=True)
