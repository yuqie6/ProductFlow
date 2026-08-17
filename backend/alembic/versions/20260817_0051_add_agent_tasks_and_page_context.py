"""add independent Agent tasks and page context snapshots

Revision ID: 20260817_0051
Revises: 20260817_0050
Create Date: 2026-08-17
"""

from __future__ import annotations

import sqlalchemy as sa
from sqlalchemy.dialects import postgresql

from alembic import op

revision = "20260817_0051"
down_revision = "20260817_0050"
branch_labels = None
depends_on = None

AGENT_TASK_STATUS = postgresql.ENUM(
    "queued",
    "running",
    "waiting_user",
    "awaiting_confirmation",
    "succeeded",
    "failed",
    "canceled",
    "paused",
    "unknown",
    name="agenttaskstatus",
    create_type=False,
)


def _alter_existing_table(table_name: str):
    bind = op.get_bind()
    return op.batch_alter_table(table_name, recreate="always" if bind.dialect.name == "sqlite" else "auto")


def upgrade() -> None:
    AGENT_TASK_STATUS.create(op.get_bind(), checkfirst=True)
    op.create_table(
        "agent_tasks",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("session_id", sa.String(length=36), nullable=False),
        sa.Column("conversation_id", sa.String(length=36), nullable=True),
        sa.Column("product_id", sa.String(length=36), nullable=True),
        sa.Column("workflow_id", sa.String(length=36), nullable=True),
        sa.Column("workflow_draft_id", sa.String(length=36), nullable=True),
        sa.Column("harness_run_id", sa.String(length=120), nullable=False),
        sa.Column("title", sa.String(length=160), nullable=False),
        sa.Column("goal", sa.Text(), nullable=False),
        sa.Column("status", AGENT_TASK_STATUS, nullable=False),
        sa.Column("waiting_reason", sa.String(length=160), nullable=True),
        sa.Column("failure_reason", sa.Text(), nullable=True),
        sa.Column("current_turn_id", sa.String(length=36), nullable=True),
        sa.Column("started_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("finished_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("canceled_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), nullable=False),
        sa.ForeignKeyConstraint(
            ["session_id"], ["agent_sessions.id"],
            name="fk_agent_tasks_session_id", ondelete="CASCADE",
        ),
        sa.ForeignKeyConstraint(
            ["conversation_id"], ["agent_conversations.id"],
            name="fk_agent_tasks_conversation_id", ondelete="SET NULL",
        ),
        sa.ForeignKeyConstraint(
            ["product_id"], ["products.id"],
            name="fk_agent_tasks_product_id", ondelete="SET NULL",
        ),
        sa.ForeignKeyConstraint(
            ["workflow_id"], ["product_workflows.id"],
            name="fk_agent_tasks_workflow_id", ondelete="SET NULL",
        ),
        sa.ForeignKeyConstraint(
            ["workflow_draft_id"], ["workflow_drafts.id"],
            name="fk_agent_tasks_workflow_draft_id", ondelete="SET NULL",
        ),
        sa.PrimaryKeyConstraint("id"),
        sa.UniqueConstraint("harness_run_id", name="uq_agent_tasks_harness_run_id"),
    )
    op.create_index(
        "ix_agent_tasks_session_status_updated",
        "agent_tasks",
        ["session_id", "status", "updated_at", "id"],
    )
    op.create_index("ix_agent_tasks_status_updated", "agent_tasks", ["status", "updated_at", "id"])
    op.create_index(
        "ix_agent_tasks_conversation_updated",
        "agent_tasks",
        ["conversation_id", "updated_at", "id"],
    )

    op.create_table(
        "agent_page_context_snapshots",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("task_id", sa.String(length=36), nullable=True),
        sa.Column("turn_id", sa.String(length=36), nullable=True),
        sa.Column("route", sa.String(length=512), nullable=False),
        sa.Column("page_type", sa.String(length=80), nullable=False),
        sa.Column("product_id", sa.String(length=36), nullable=True),
        sa.Column("workflow_id", sa.String(length=36), nullable=True),
        sa.Column("selected_asset_ids_json", sa.JSON(), nullable=False),
        sa.Column("visible_asset_ids_json", sa.JSON(), nullable=False),
        sa.Column("filters_json", sa.JSON(), nullable=False),
        sa.Column("workflow_revision", sa.Integer(), nullable=True),
        sa.Column("library_revision", sa.Integer(), nullable=True),
        sa.Column("digest", sa.String(length=64), nullable=False),
        sa.Column("captured_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.CheckConstraint("length(digest) = 64", name="ck_agent_page_context_snapshots_digest"),
        sa.ForeignKeyConstraint(
            ["task_id"], ["agent_tasks.id"],
            name="fk_agent_page_context_snapshots_task_id", ondelete="CASCADE",
        ),
        sa.PrimaryKeyConstraint("id"),
    )
    op.create_index(
        "ix_agent_page_context_snapshots_task_created",
        "agent_page_context_snapshots",
        ["task_id", "created_at", "id"],
    )

    with _alter_existing_table("agent_turn_projections") as batch_op:
        batch_op.add_column(sa.Column("task_id", sa.String(length=36), nullable=True))
        batch_op.add_column(sa.Column("page_context_snapshot_id", sa.String(length=36), nullable=True))
        batch_op.create_foreign_key(
            "fk_agent_turn_projections_task_id",
            "agent_tasks",
            ["task_id"],
            ["id"],
            ondelete="SET NULL",
        )
        batch_op.create_foreign_key(
            "fk_agent_turn_projections_page_context_snapshot_id",
            "agent_page_context_snapshots",
            ["page_context_snapshot_id"],
            ["id"],
            ondelete="SET NULL",
        )
        batch_op.create_index(
            "ix_agent_turn_projections_task_created",
            ["task_id", "created_at", "id"],
        )


def downgrade() -> None:
    with _alter_existing_table("agent_turn_projections") as batch_op:
        batch_op.drop_index("ix_agent_turn_projections_task_created")
        batch_op.drop_constraint("fk_agent_turn_projections_page_context_snapshot_id", type_="foreignkey")
        batch_op.drop_constraint("fk_agent_turn_projections_task_id", type_="foreignkey")
        batch_op.drop_column("page_context_snapshot_id")
        batch_op.drop_column("task_id")
    op.drop_index(
        "ix_agent_page_context_snapshots_task_created",
        table_name="agent_page_context_snapshots",
    )
    op.drop_table("agent_page_context_snapshots")
    op.drop_index("ix_agent_tasks_conversation_updated", table_name="agent_tasks")
    op.drop_index("ix_agent_tasks_status_updated", table_name="agent_tasks")
    op.drop_index("ix_agent_tasks_session_status_updated", table_name="agent_tasks")
    op.drop_table("agent_tasks")
    bind = op.get_bind()
    if bind.dialect.name == "postgresql":
        AGENT_TASK_STATUS.drop(bind, checkfirst=True)
