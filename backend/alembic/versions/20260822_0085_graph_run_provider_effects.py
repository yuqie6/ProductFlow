"""graph-run provider effect phases, attempt fencing, and heartbeat

Revision ID: 20260822_0085
Revises: 20260822_0084
Create Date: 2026-08-23
"""

from __future__ import annotations

import sqlalchemy as sa

from alembic import op

revision = "20260822_0085"
down_revision = "20260822_0084"
branch_labels = None
depends_on = None

_NODE_RUN_PHASES = (
    "claimed",
    "prepared",
    "provider_call",
    "provider_result_received",
    "unknown_provider_effect",
    "requeued_after_idle",
)


def upgrade() -> None:
    with op.batch_alter_table("workflow_graph_node_runs") as batch_op:
        batch_op.add_column(sa.Column("active_attempt_id", sa.String(length=36), nullable=True))
        batch_op.add_column(sa.Column("progress_phase", sa.String(length=80), nullable=True))
        batch_op.add_column(sa.Column("progress_updated_at", sa.DateTime(timezone=True), nullable=True))
        batch_op.create_check_constraint(
            "ck_workflow_graph_node_runs_progress_phase",
            "progress_phase IS NULL OR progress_phase IN ("
            + ", ".join(f"'{phase}'" for phase in _NODE_RUN_PHASES)
            + ")",
        )

    op.create_table(
        "workflow_graph_provider_effects",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("node_run_id", sa.String(length=36), nullable=False),
        sa.Column("operation_key", sa.String(length=255), nullable=False),
        sa.Column("effect_kind", sa.String(length=80), nullable=False),
        sa.Column("request_hash", sa.String(length=64), nullable=False),
        sa.Column("provider_name", sa.String(length=80), nullable=False),
        sa.Column("attempt_id", sa.String(length=36), nullable=False),
        sa.Column("effect_result", sa.String(length=20), nullable=False),
        sa.Column("reconciliation_state", sa.String(length=20), nullable=False),
        sa.Column("provider_response_id", sa.String(length=255), nullable=True),
        sa.Column("provider_status", sa.String(length=80), nullable=True),
        sa.Column("request_json", sa.JSON(), nullable=True),
        sa.Column("result_json", sa.JSON(), nullable=True),
        sa.Column("detail", sa.Text(), nullable=True),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), nullable=False),
        sa.CheckConstraint(
            "effect_result IN ('pending', 'applied', 'failed', 'unknown')",
            name="ck_workflow_graph_provider_effects_effect_result",
        ),
        sa.CheckConstraint(
            "reconciliation_state IN ('not_requested', 'applied', 'not_applied', 'unknown', 'unsupported')",
            name="ck_workflow_graph_provider_effects_reconciliation_state",
        ),
        sa.CheckConstraint(
            "length(request_hash) = 64",
            name="ck_workflow_graph_provider_effects_request_hash",
        ),
        sa.ForeignKeyConstraint(
            ["node_run_id"],
            ["workflow_graph_node_runs.id"],
            name="fk_workflow_graph_provider_effects_node_run_id",
            ondelete="CASCADE",
        ),
        sa.PrimaryKeyConstraint("id"),
        sa.UniqueConstraint("node_run_id", name="uq_workflow_graph_provider_effects_node_run_id"),
        sa.UniqueConstraint("operation_key", name="uq_workflow_graph_provider_effects_operation_key"),
    )
    op.create_index(
        "ix_workflow_graph_provider_effects_reconciliation",
        "workflow_graph_provider_effects",
        ["effect_result", "reconciliation_state", "updated_at", "id"],
    )


def downgrade() -> None:
    op.drop_index(
        "ix_workflow_graph_provider_effects_reconciliation",
        table_name="workflow_graph_provider_effects",
    )
    op.drop_table("workflow_graph_provider_effects")
    with op.batch_alter_table("workflow_graph_node_runs") as batch_op:
        batch_op.drop_constraint("ck_workflow_graph_node_runs_progress_phase", type_="check")
        batch_op.drop_column("progress_updated_at")
        batch_op.drop_column("progress_phase")
        batch_op.drop_column("active_attempt_id")
