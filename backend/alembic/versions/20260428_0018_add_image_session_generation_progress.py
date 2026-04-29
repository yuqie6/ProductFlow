"""add image session generation progress metadata

Revision ID: 20260428_0018
Revises: 20260428_0017
Create Date: 2026-04-28
"""

from __future__ import annotations

import sqlalchemy as sa

from alembic import op

revision = "20260428_0018"
down_revision = "20260428_0017"
branch_labels = None
depends_on = None


def upgrade() -> None:
    op.add_column(
        "image_session_generation_tasks",
        sa.Column("completed_candidates", sa.Integer(), nullable=False, server_default="0"),
    )
    op.add_column("image_session_generation_tasks", sa.Column("active_candidate_index", sa.Integer(), nullable=True))
    op.add_column("image_session_generation_tasks", sa.Column("progress_phase", sa.String(length=64), nullable=True))
    op.add_column(
        "image_session_generation_tasks",
        sa.Column("progress_updated_at", sa.DateTime(timezone=True), nullable=True),
    )
    op.add_column(
        "image_session_generation_tasks",
        sa.Column("provider_response_id", sa.String(length=255), nullable=True),
    )
    op.add_column(
        "image_session_generation_tasks",
        sa.Column("provider_response_status", sa.String(length=64), nullable=True),
    )
    op.add_column("image_session_generation_tasks", sa.Column("progress_metadata", sa.JSON(), nullable=True))
    with op.batch_alter_table("image_session_generation_tasks") as batch_op:
        batch_op.alter_column(
            "completed_candidates",
            existing_type=sa.Integer(),
            nullable=False,
            server_default=None,
        )


def downgrade() -> None:
    with op.batch_alter_table("image_session_generation_tasks") as batch_op:
        batch_op.drop_column("progress_metadata")
        batch_op.drop_column("provider_response_status")
        batch_op.drop_column("provider_response_id")
        batch_op.drop_column("progress_updated_at")
        batch_op.drop_column("progress_phase")
        batch_op.drop_column("active_candidate_index")
        batch_op.drop_column("completed_candidates")
