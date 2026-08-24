"""archive official workflow recipe seeds; they are not an online catalog

Revision ID: 20260825_0091
Revises: 20260824_0090
Create Date: 2026-08-25
"""

from __future__ import annotations

from datetime import UTC, datetime

import sqlalchemy as sa

from alembic import op

revision = "20260825_0091"
down_revision = "20260824_0090"
branch_labels = None
depends_on = None


def upgrade() -> None:
    now = datetime.now(UTC)
    op.get_bind().execute(
        sa.text(
            """
            UPDATE workflow_recipes
            SET archived_at = COALESCE(archived_at, :now),
                updated_at = :now
            WHERE origin = 'official' AND archived_at IS NULL
            """
        ),
        {"now": now},
    )


def downgrade() -> None:
    op.get_bind().execute(
        sa.text(
            """
            UPDATE workflow_recipes
            SET archived_at = NULL,
                updated_at = :now
            WHERE origin = 'official'
            """
        ),
        {"now": datetime.now(UTC)},
    )
