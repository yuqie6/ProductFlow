"""add generation attempt fencing

Revision ID: 20260816_0044
Revises: 20260816_0043
Create Date: 2026-08-16

This revision requires a coordinated worker stop. Existing running work is
reset to queued so the new workers can claim it with an explicit attempt token;
old workers must not remain online across the constraint installation.
"""

from __future__ import annotations

import sqlalchemy as sa

from alembic import op

revision = "20260816_0044"
down_revision = "20260816_0043"
branch_labels = None
depends_on = None

WORKFLOW_ACTIVE_ATTEMPT_CHECK = "ck_workflow_node_runs_active_attempt"
WORKFLOW_ATTEMPTS_CHECK = "ck_workflow_node_runs_non_negative_attempts"
WORKFLOW_RECOVERY_INDEX = "ix_workflow_node_runs_recovery"
IMAGE_ACTIVE_ATTEMPT_CHECK = "ck_image_session_generation_tasks_active_attempt"
IMAGE_ATTEMPTS_CHECK = "ck_image_session_generation_tasks_non_negative_attempts"


def _add_attempt_constraints() -> None:
    bind = op.get_bind()
    workflow_active_sql = (
        "(status = 'running' AND active_attempt_id IS NOT NULL) OR "
        "(status != 'running' AND active_attempt_id IS NULL)"
    )
    image_active_sql = (
        "(status = 'running' AND active_attempt_id IS NOT NULL AND started_at IS NOT NULL "
        "AND finished_at IS NULL) OR (status != 'running' AND active_attempt_id IS NULL)"
    )
    if bind.dialect.name == "sqlite":
        with op.batch_alter_table("workflow_node_runs", recreate="always") as batch_op:
            batch_op.create_check_constraint(WORKFLOW_ATTEMPTS_CHECK, "attempts >= 0")
            batch_op.create_check_constraint(WORKFLOW_ACTIVE_ATTEMPT_CHECK, workflow_active_sql)
        with op.batch_alter_table("image_session_generation_tasks", recreate="always") as batch_op:
            batch_op.create_check_constraint(IMAGE_ATTEMPTS_CHECK, "attempts >= 0")
            batch_op.create_check_constraint(IMAGE_ACTIVE_ATTEMPT_CHECK, image_active_sql)
        return

    op.create_check_constraint(WORKFLOW_ATTEMPTS_CHECK, "workflow_node_runs", "attempts >= 0")
    op.create_check_constraint(WORKFLOW_ACTIVE_ATTEMPT_CHECK, "workflow_node_runs", workflow_active_sql)
    op.create_check_constraint(IMAGE_ATTEMPTS_CHECK, "image_session_generation_tasks", "attempts >= 0")
    op.create_check_constraint(
        IMAGE_ACTIVE_ATTEMPT_CHECK,
        "image_session_generation_tasks",
        image_active_sql,
    )


def _drop_attempt_constraints() -> None:
    bind = op.get_bind()
    if bind.dialect.name == "sqlite":
        with op.batch_alter_table("workflow_node_runs", recreate="always") as batch_op:
            batch_op.drop_constraint(WORKFLOW_ACTIVE_ATTEMPT_CHECK, type_="check")
            batch_op.drop_constraint(WORKFLOW_ATTEMPTS_CHECK, type_="check")
        with op.batch_alter_table("image_session_generation_tasks", recreate="always") as batch_op:
            batch_op.drop_constraint(IMAGE_ACTIVE_ATTEMPT_CHECK, type_="check")
            batch_op.drop_constraint(IMAGE_ATTEMPTS_CHECK, type_="check")
        return

    op.drop_constraint(WORKFLOW_ACTIVE_ATTEMPT_CHECK, "workflow_node_runs", type_="check")
    op.drop_constraint(WORKFLOW_ATTEMPTS_CHECK, "workflow_node_runs", type_="check")
    op.drop_constraint(IMAGE_ACTIVE_ATTEMPT_CHECK, "image_session_generation_tasks", type_="check")
    op.drop_constraint(IMAGE_ATTEMPTS_CHECK, "image_session_generation_tasks", type_="check")


def upgrade() -> None:
    op.add_column(
        "workflow_node_runs",
        sa.Column("attempts", sa.Integer(), nullable=False, server_default=sa.text("0")),
    )
    op.add_column(
        "workflow_node_runs",
        sa.Column("active_attempt_id", sa.String(length=36), nullable=True),
    )
    op.add_column(
        "image_session_generation_tasks",
        sa.Column("active_attempt_id", sa.String(length=36), nullable=True),
    )

    bind = op.get_bind()
    bind.execute(
        sa.text(
            "UPDATE workflow_nodes SET status = 'queued', failure_reason = NULL "
            "WHERE id IN (SELECT node_id FROM workflow_node_runs WHERE status = 'running')"
        )
    )
    bind.execute(
        sa.text(
            "UPDATE workflow_node_runs SET status = 'queued', active_attempt_id = NULL, "
            "failure_reason = NULL, finished_at = NULL WHERE status = 'running'"
        )
    )
    bind.execute(
        sa.text(
            "UPDATE image_session_generation_tasks SET status = 'queued', active_attempt_id = NULL, "
            "started_at = NULL, finished_at = NULL, active_candidate_index = NULL, "
            "provider_response_id = NULL, provider_response_status = NULL, "
            "progress_phase = 'requeued_after_attempt_fencing_migration', "
            "progress_updated_at = CURRENT_TIMESTAMP WHERE status = 'running'"
        )
    )
    bind.execute(
        sa.text(
            "UPDATE workflow_node_runs SET active_attempt_id = NULL WHERE status != 'running'"
        )
    )
    bind.execute(
        sa.text(
            "UPDATE image_session_generation_tasks SET active_attempt_id = NULL WHERE status != 'running'"
        )
    )

    _add_attempt_constraints()
    op.create_index(
        WORKFLOW_RECOVERY_INDEX,
        "workflow_node_runs",
        ["status", "started_at", "id"],
        unique=False,
    )


def downgrade() -> None:
    op.drop_index(WORKFLOW_RECOVERY_INDEX, table_name="workflow_node_runs")
    _drop_attempt_constraints()
    op.drop_column("image_session_generation_tasks", "active_attempt_id")
    op.drop_column("workflow_node_runs", "active_attempt_id")
    op.drop_column("workflow_node_runs", "attempts")
