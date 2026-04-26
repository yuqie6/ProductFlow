"""add image session generation tasks

Revision ID: 20260427_0014
Revises: 20260427_0013
Create Date: 2026-04-27
"""

import sqlalchemy as sa
from sqlalchemy.dialects import postgresql

from alembic import op

revision = "20260427_0014"
down_revision = "20260427_0013"
branch_labels = None
depends_on = None


job_status = postgresql.ENUM("queued", "running", "succeeded", "failed", name="jobstatus", create_type=False)


def upgrade() -> None:
    op.create_table(
        "image_session_generation_tasks",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("session_id", sa.String(length=36), nullable=False),
        sa.Column("status", job_status, nullable=False),
        sa.Column("prompt", sa.Text(), nullable=False),
        sa.Column("size", sa.String(length=32), nullable=False),
        sa.Column("base_asset_id", sa.String(length=36), nullable=True),
        sa.Column("selected_reference_asset_ids", sa.JSON(), nullable=True),
        sa.Column("generation_count", sa.Integer(), nullable=False),
        sa.Column("failure_reason", sa.Text(), nullable=True),
        sa.Column("result_generation_group_id", sa.String(length=36), nullable=True),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("started_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("finished_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("attempts", sa.Integer(), nullable=False),
        sa.Column("is_retryable", sa.Boolean(), nullable=False),
        sa.ForeignKeyConstraint(
            ["base_asset_id"],
            ["image_session_assets.id"],
            name="fk_image_session_generation_tasks_base_asset_id",
            ondelete="SET NULL",
        ),
        sa.ForeignKeyConstraint(["session_id"], ["image_sessions.id"], ondelete="CASCADE"),
        sa.PrimaryKeyConstraint("id"),
    )
    op.create_index(
        "ix_image_session_generation_tasks_session_id",
        "image_session_generation_tasks",
        ["session_id"],
    )
    op.create_index(
        "ix_image_session_generation_tasks_status",
        "image_session_generation_tasks",
        ["status"],
    )


def downgrade() -> None:
    op.drop_index("ix_image_session_generation_tasks_status", table_name="image_session_generation_tasks")
    op.drop_index("ix_image_session_generation_tasks_session_id", table_name="image_session_generation_tasks")
    op.drop_table("image_session_generation_tasks")
