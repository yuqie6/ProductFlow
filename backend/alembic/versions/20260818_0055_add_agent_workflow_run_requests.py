"""add Agent workflow run requests

Revision ID: 20260818_0055
Revises: 20260817_0054
Create Date: 2026-08-18
"""

from __future__ import annotations

import sqlalchemy as sa
from sqlalchemy.dialects import postgresql

from alembic import op

revision = "20260818_0055"
down_revision = "20260817_0054"
branch_labels = None
depends_on = None

AGENT_WORKFLOW_RUN_REQUEST_STATUS = postgresql.ENUM(
    "awaiting_confirmation",
    "confirmed",
    "succeeded",
    "failed",
    "cancelled",
    name="agentworkflowrunrequeststatus",
    create_type=False,
)


def upgrade() -> None:
    AGENT_WORKFLOW_RUN_REQUEST_STATUS.create(op.get_bind(), checkfirst=True)
    op.create_table(
        "agent_workflow_run_requests",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("conversation_id", sa.String(length=36), nullable=False),
        sa.Column("task_id", sa.String(length=36), nullable=True),
        sa.Column("product_id", sa.String(length=36), nullable=False),
        sa.Column("workflow_id", sa.String(length=36), nullable=False),
        sa.Column("expected_workflow_revision", sa.Integer(), nullable=False),
        sa.Column("idempotency_key", sa.String(length=200), nullable=False),
        sa.Column("request_hash", sa.String(length=64), nullable=False),
        sa.Column("source_step_id", sa.String(length=120), nullable=False),
        sa.Column("status", AGENT_WORKFLOW_RUN_REQUEST_STATUS, nullable=False),
        sa.Column("workflow_run_id", sa.String(length=36), nullable=True),
        sa.Column("failure_reason", sa.Text(), nullable=True),
        sa.Column("confirmed_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("finished_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), nullable=False),
        sa.CheckConstraint(
            "expected_workflow_revision > 0",
            name="ck_agent_workflow_run_requests_positive_revision",
        ),
        sa.CheckConstraint(
            "length(request_hash) = 64",
            name="ck_agent_workflow_run_requests_request_hash",
        ),
        sa.CheckConstraint(
            "length(source_step_id) > 0",
            name="ck_agent_workflow_run_requests_source_step_id",
        ),
        sa.ForeignKeyConstraint(
            ["conversation_id"],
            ["agent_conversations.id"],
            name="fk_agent_workflow_run_requests_conversation_id",
            ondelete="CASCADE",
        ),
        sa.ForeignKeyConstraint(
            ["task_id"],
            ["agent_tasks.id"],
            name="fk_agent_workflow_run_requests_task_id",
            ondelete="SET NULL",
        ),
        sa.ForeignKeyConstraint(
            ["product_id"],
            ["products.id"],
            name="fk_agent_workflow_run_requests_product_id",
            ondelete="CASCADE",
        ),
        sa.ForeignKeyConstraint(
            ["workflow_id"],
            ["product_workflows.id"],
            name="fk_agent_workflow_run_requests_workflow_id",
            ondelete="CASCADE",
        ),
        sa.ForeignKeyConstraint(
            ["workflow_run_id"],
            ["workflow_runs.id"],
            name="fk_agent_workflow_run_requests_workflow_run_id",
            ondelete="SET NULL",
        ),
        sa.PrimaryKeyConstraint("id"),
        sa.UniqueConstraint(
            "conversation_id",
            "idempotency_key",
            name="uq_agent_workflow_run_requests_conversation_key",
        ),
    )
    op.create_index(
        "ix_agent_workflow_run_requests_conversation_status_updated",
        "agent_workflow_run_requests",
        ["conversation_id", "status", "updated_at", "id"],
    )
    op.create_index(
        "ix_agent_workflow_run_requests_task_status_updated",
        "agent_workflow_run_requests",
        ["task_id", "status", "updated_at", "id"],
    )
    op.create_index(
        "ix_agent_workflow_run_requests_workflow_run_id",
        "agent_workflow_run_requests",
        ["workflow_run_id"],
    )
    with _alter_existing_table("agent_turn_projections") as batch_op:
        batch_op.add_column(sa.Column("workflow_run_request_id", sa.String(length=36), nullable=True))
        batch_op.create_foreign_key(
            "fk_agent_turn_projections_workflow_run_request_id",
            "agent_workflow_run_requests",
            ["workflow_run_request_id"],
            ["id"],
            ondelete="SET NULL",
        )
        batch_op.create_unique_constraint(
            "uq_agent_turn_projections_workflow_run_request_id",
            ["workflow_run_request_id"],
        )


def downgrade() -> None:
    with _alter_existing_table("agent_turn_projections") as batch_op:
        batch_op.drop_constraint("uq_agent_turn_projections_workflow_run_request_id", type_="unique")
        batch_op.drop_constraint("fk_agent_turn_projections_workflow_run_request_id", type_="foreignkey")
        batch_op.drop_column("workflow_run_request_id")
    op.drop_index(
        "ix_agent_workflow_run_requests_workflow_run_id",
        table_name="agent_workflow_run_requests",
    )
    op.drop_index(
        "ix_agent_workflow_run_requests_task_status_updated",
        table_name="agent_workflow_run_requests",
    )
    op.drop_index(
        "ix_agent_workflow_run_requests_conversation_status_updated",
        table_name="agent_workflow_run_requests",
    )
    op.drop_table("agent_workflow_run_requests")
    AGENT_WORKFLOW_RUN_REQUEST_STATUS.drop(op.get_bind(), checkfirst=True)


def _alter_existing_table(table_name: str):
    bind = op.get_bind()
    return op.batch_alter_table(table_name, recreate="always" if bind.dialect.name == "sqlite" else "auto")
