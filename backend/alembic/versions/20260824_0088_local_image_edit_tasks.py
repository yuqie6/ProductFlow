"""add durable local image edit tasks, provider attempts, and adoption events

Revision ID: 20260824_0088
Revises: 20260824_0087
Create Date: 2026-08-24
"""

from __future__ import annotations

import sqlalchemy as sa
from sqlalchemy.dialects import postgresql

from alembic import op

revision = "20260824_0088"
down_revision = "20260824_0087"
branch_labels = None
depends_on = None


LOCAL_EDIT_TASK_STATUS = postgresql.ENUM(
    "draft",
    "queued",
    "running",
    "succeeded",
    "failed",
    "cancelled",
    "unknown",
    name="localimageedittaskstatus",
    create_type=False,
)


def _status_type(bind: sa.Connection) -> sa.types.TypeEngine:
    if bind.dialect.name == "postgresql":
        return LOCAL_EDIT_TASK_STATUS
    return sa.String(length=20)


def upgrade() -> None:
    bind = op.get_bind()
    if bind.dialect.name == "postgresql":
        with op.get_context().autocommit_block():
            op.execute("ALTER TYPE productimageorigintype ADD VALUE IF NOT EXISTS 'local_edit'")
        LOCAL_EDIT_TASK_STATUS.create(bind, checkfirst=True)

    op.create_table(
        "local_image_edit_tasks",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("product_id", sa.String(length=36), nullable=False),
        sa.Column("source_asset_id", sa.String(length=36), nullable=False),
        sa.Column("source_media_sha256", sa.String(length=64), nullable=False),
        sa.Column("mask_media_object_id", sa.String(length=36), nullable=False),
        sa.Column("target_graph_id", sa.String(length=36), nullable=True),
        sa.Column("target_node_id", sa.String(length=36), nullable=True),
        sa.Column("target_graph_revision", sa.Integer(), nullable=True),
        sa.Column("source_artifact_id", sa.String(length=36), nullable=True),
        sa.Column("source_artifact_asset_id", sa.String(length=36), nullable=True),
        sa.Column("source_artifact_input_digest", sa.String(length=64), nullable=True),
        sa.Column("operation", sa.String(length=32), nullable=False),
        sa.Column("instruction", sa.Text(), nullable=True),
        sa.Column("source_text", sa.Text(), nullable=True),
        sa.Column("replacement_text", sa.Text(), nullable=True),
        sa.Column("mask_geometry_json", sa.JSON(), nullable=False),
        sa.Column("status", _status_type(bind), nullable=False),
        sa.Column("revision", sa.Integer(), nullable=False),
        sa.Column("idempotency_key", sa.String(length=120), nullable=True),
        sa.Column("request_hash", sa.String(length=64), nullable=True),
        sa.Column("attempts", sa.Integer(), nullable=False),
        sa.Column("active_attempt_id", sa.String(length=36), nullable=True),
        sa.Column("progress_phase", sa.String(length=80), nullable=True),
        sa.Column("failure_reason", sa.Text(), nullable=True),
        sa.Column("is_retryable", sa.Boolean(), nullable=False),
        sa.Column("provider_name", sa.String(length=80), nullable=True),
        sa.Column("provider_model", sa.String(length=255), nullable=True),
        sa.Column("provider_response_id", sa.String(length=255), nullable=True),
        sa.Column("provider_status", sa.String(length=80), nullable=True),
        sa.Column("result_asset_id", sa.String(length=36), nullable=True),
        sa.Column("queued_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("started_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("finished_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), nullable=False),
        sa.CheckConstraint(
            "status IN ('draft', 'queued', 'running', 'succeeded', 'failed', 'cancelled', 'unknown')",
            name="ck_local_image_edit_tasks_status",
        ),
        sa.CheckConstraint("revision >= 1", name="ck_local_image_edit_tasks_revision"),
        sa.CheckConstraint("attempts >= 0", name="ck_local_image_edit_tasks_attempts"),
        sa.CheckConstraint(
            "length(source_media_sha256) = 64",
            name="ck_local_image_edit_tasks_source_media_hash",
        ),
        sa.CheckConstraint(
            "request_hash IS NULL OR length(request_hash) = 64",
            name="ck_local_image_edit_tasks_request_hash",
        ),
        sa.CheckConstraint(
            "(status = 'running' AND active_attempt_id IS NOT NULL AND started_at IS NOT NULL "
            "AND finished_at IS NULL) OR "
            "(status != 'running' AND active_attempt_id IS NULL)",
            name="ck_local_image_edit_tasks_active_attempt",
        ),
        sa.CheckConstraint(
            "(status = 'succeeded' AND result_asset_id IS NOT NULL AND finished_at IS NOT NULL) OR "
            "(status IN ('draft', 'queued', 'running', 'failed', 'cancelled', 'unknown') "
            "AND result_asset_id IS NULL)",
            name="ck_local_image_edit_tasks_result_state",
        ),
        sa.ForeignKeyConstraint(
            ["product_id"],
            ["products.id"],
            name="fk_local_image_edit_tasks_product_id",
            ondelete="CASCADE",
        ),
        sa.ForeignKeyConstraint(
            ["source_asset_id"],
            ["product_image_assets.id"],
            name="fk_local_image_edit_tasks_source_asset_id",
            ondelete="RESTRICT",
        ),
        sa.ForeignKeyConstraint(
            ["mask_media_object_id"],
            ["media_objects.id"],
            name="fk_local_image_edit_tasks_mask_media_object_id",
            ondelete="RESTRICT",
        ),
        sa.ForeignKeyConstraint(
            ["target_graph_id"],
            ["workflow_graphs.id"],
            name="fk_local_image_edit_tasks_target_graph_id",
            ondelete="SET NULL",
        ),
        sa.ForeignKeyConstraint(
            ["target_node_id"],
            ["workflow_graph_nodes.id"],
            name="fk_local_image_edit_tasks_target_node_id",
            ondelete="SET NULL",
        ),
        sa.ForeignKeyConstraint(
            ["source_artifact_id"],
            ["workflow_graph_artifacts.id"],
            name="fk_local_image_edit_tasks_source_artifact_id",
            ondelete="SET NULL",
        ),
        sa.ForeignKeyConstraint(
            ["source_artifact_asset_id"],
            ["product_image_assets.id"],
            name="fk_local_image_edit_tasks_source_artifact_asset_id",
            ondelete="RESTRICT",
        ),
        sa.ForeignKeyConstraint(
            ["result_asset_id"],
            ["product_image_assets.id"],
            name="fk_local_image_edit_tasks_result_asset_id",
            ondelete="RESTRICT",
        ),
        sa.PrimaryKeyConstraint("id"),
        sa.UniqueConstraint(
            "product_id",
            "idempotency_key",
            name="uq_local_image_edit_tasks_product_idempotency",
        ),
    )
    op.create_index(
        "ix_local_image_edit_tasks_product_status_created",
        "local_image_edit_tasks",
        ["product_id", "status", "created_at", "id"],
    )
    op.create_index(
        "ix_local_image_edit_tasks_source_asset",
        "local_image_edit_tasks",
        ["source_asset_id", "created_at", "id"],
    )
    op.create_index(
        "ix_local_image_edit_tasks_target_node_status",
        "local_image_edit_tasks",
        ["target_node_id", "status", "created_at", "id"],
    )

    op.create_table(
        "local_image_edit_task_references",
        sa.Column("task_id", sa.String(length=36), nullable=False),
        sa.Column("asset_id", sa.String(length=36), nullable=False),
        sa.Column("sort_order", sa.Integer(), nullable=False),
        sa.CheckConstraint(
            "sort_order >= 0 AND sort_order < 6",
            name="ck_local_image_edit_task_references_bounded_order",
        ),
        sa.ForeignKeyConstraint(
            ["task_id"],
            ["local_image_edit_tasks.id"],
            name="fk_local_image_edit_task_references_task_id",
            ondelete="CASCADE",
        ),
        sa.ForeignKeyConstraint(
            ["asset_id"],
            ["product_image_assets.id"],
            name="fk_local_image_edit_task_references_asset_id",
            ondelete="RESTRICT",
        ),
        sa.PrimaryKeyConstraint("task_id", "asset_id"),
        sa.UniqueConstraint(
            "task_id",
            "sort_order",
            name="uq_local_image_edit_task_references_task_order",
        ),
    )
    op.create_index(
        "ix_local_image_edit_task_references_asset",
        "local_image_edit_task_references",
        ["asset_id"],
    )

    op.create_table(
        "local_image_edit_provider_attempts",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("task_id", sa.String(length=36), nullable=False),
        sa.Column("attempt_id", sa.String(length=36), nullable=False),
        sa.Column("attempt_number", sa.Integer(), nullable=False),
        sa.Column("operation_key", sa.String(length=255), nullable=False),
        sa.Column("request_hash", sa.String(length=64), nullable=False),
        sa.Column("phase", sa.String(length=40), nullable=False),
        sa.Column("effect_result", sa.String(length=20), nullable=False),
        sa.Column("provider_name", sa.String(length=80), nullable=False),
        sa.Column("provider_model", sa.String(length=255), nullable=True),
        sa.Column("provider_response_id", sa.String(length=255), nullable=True),
        sa.Column("provider_status", sa.String(length=80), nullable=True),
        sa.Column("request_json", sa.JSON(), nullable=True),
        sa.Column("effective_parameters_json", sa.JSON(), nullable=True),
        sa.Column("result_json", sa.JSON(), nullable=True),
        sa.Column("late_result_asset_id", sa.String(length=36), nullable=True),
        sa.Column("detail", sa.Text(), nullable=True),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), nullable=False),
        sa.CheckConstraint(
            "attempt_number >= 1",
            name="ck_local_image_edit_provider_attempts_positive_number",
        ),
        sa.CheckConstraint(
            "length(request_hash) = 64",
            name="ck_local_image_edit_provider_attempts_request_hash",
        ),
        sa.CheckConstraint(
            "phase IN ('claimed', 'provider_pending', 'provider_call', 'provider_result_received', "
            "'succeeded', 'failed', 'unknown')",
            name="ck_local_image_edit_provider_attempts_phase",
        ),
        sa.CheckConstraint(
            "effect_result IN ('pending', 'applied', 'failed', 'unknown', 'unsupported')",
            name="ck_local_image_edit_provider_attempts_effect_result",
        ),
        sa.ForeignKeyConstraint(
            ["task_id"],
            ["local_image_edit_tasks.id"],
            name="fk_local_image_edit_provider_attempts_task_id",
            ondelete="CASCADE",
        ),
        sa.ForeignKeyConstraint(
            ["late_result_asset_id"],
            ["product_image_assets.id"],
            name="fk_local_image_edit_provider_attempts_late_result_asset_id",
            ondelete="RESTRICT",
        ),
        sa.PrimaryKeyConstraint("id"),
        sa.UniqueConstraint(
            "task_id",
            "attempt_number",
            name="uq_local_image_edit_provider_attempts_task_number",
        ),
        sa.UniqueConstraint(
            "task_id",
            "attempt_id",
            name="uq_local_image_edit_provider_attempts_task_attempt",
        ),
    )
    op.create_index(
        "ix_local_image_edit_provider_attempts_task_created",
        "local_image_edit_provider_attempts",
        ["task_id", "created_at", "id"],
    )

    op.create_table(
        "local_image_edit_adoption_events",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("product_id", sa.String(length=36), nullable=False),
        sa.Column("task_id", sa.String(length=36), nullable=False),
        sa.Column("graph_id", sa.String(length=36), nullable=False),
        sa.Column("node_id", sa.String(length=36), nullable=False),
        sa.Column("event_type", sa.String(length=16), nullable=False),
        sa.Column("from_artifact_id", sa.String(length=36), nullable=False),
        sa.Column("to_artifact_id", sa.String(length=36), nullable=False),
        sa.Column("related_event_id", sa.String(length=36), nullable=True),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.CheckConstraint(
            "event_type IN ('adopt', 'revert')",
            name="ck_local_image_edit_adoption_events_type",
        ),
        sa.CheckConstraint(
            "from_artifact_id IS NOT NULL AND to_artifact_id IS NOT NULL",
            name="ck_local_image_edit_adoption_events_artifacts",
        ),
        sa.ForeignKeyConstraint(
            ["product_id"],
            ["products.id"],
            name="fk_local_image_edit_adoption_events_product_id",
            ondelete="CASCADE",
        ),
        sa.ForeignKeyConstraint(
            ["task_id"],
            ["local_image_edit_tasks.id"],
            name="fk_local_image_edit_adoption_events_task_id",
            ondelete="RESTRICT",
        ),
        sa.ForeignKeyConstraint(
            ["graph_id"],
            ["workflow_graphs.id"],
            name="fk_local_image_edit_adoption_events_graph_id",
            ondelete="RESTRICT",
        ),
        sa.ForeignKeyConstraint(
            ["node_id"],
            ["workflow_graph_nodes.id"],
            name="fk_local_image_edit_adoption_events_node_id",
            ondelete="RESTRICT",
        ),
        sa.ForeignKeyConstraint(
            ["from_artifact_id"],
            ["workflow_graph_artifacts.id"],
            name="fk_local_image_edit_adoption_events_from_artifact_id",
            ondelete="RESTRICT",
        ),
        sa.ForeignKeyConstraint(
            ["to_artifact_id"],
            ["workflow_graph_artifacts.id"],
            name="fk_local_image_edit_adoption_events_to_artifact_id",
            ondelete="RESTRICT",
        ),
        sa.ForeignKeyConstraint(
            ["related_event_id"],
            ["local_image_edit_adoption_events.id"],
            name="fk_local_image_edit_adoption_events_related_event_id",
            ondelete="SET NULL",
        ),
        sa.PrimaryKeyConstraint("id"),
    )
    op.create_index(
        "ix_local_image_edit_adoption_events_node_created",
        "local_image_edit_adoption_events",
        ["node_id", "created_at", "id"],
    )


def downgrade() -> None:
    bind = op.get_bind()
    blockers = {
        "task": bind.execute(sa.text("SELECT 1 FROM local_image_edit_tasks LIMIT 1")).first(),
        "adoption": bind.execute(
            sa.text("SELECT 1 FROM local_image_edit_adoption_events LIMIT 1")
        ).first(),
        "local_edit_asset": bind.execute(
            sa.text("SELECT 1 FROM product_image_assets WHERE origin_type = 'local_edit' LIMIT 1")
        ).first(),
    }
    if any(blockers.values()):
        raise RuntimeError("局部编辑 task、adoption 或 local_edit 资产已存在，不能 downgrade 丢失审计")

    op.drop_index(
        "ix_local_image_edit_adoption_events_node_created",
        table_name="local_image_edit_adoption_events",
    )
    op.drop_table("local_image_edit_adoption_events")
    op.drop_index(
        "ix_local_image_edit_provider_attempts_task_created",
        table_name="local_image_edit_provider_attempts",
    )
    op.drop_table("local_image_edit_provider_attempts")
    op.drop_index(
        "ix_local_image_edit_task_references_asset",
        table_name="local_image_edit_task_references",
    )
    op.drop_table("local_image_edit_task_references")
    op.drop_index(
        "ix_local_image_edit_tasks_target_node_status",
        table_name="local_image_edit_tasks",
    )
    op.drop_index(
        "ix_local_image_edit_tasks_source_asset",
        table_name="local_image_edit_tasks",
    )
    op.drop_index(
        "ix_local_image_edit_tasks_product_status_created",
        table_name="local_image_edit_tasks",
    )
    op.drop_table("local_image_edit_tasks")

    # PostgreSQL enum values are intentionally left in place: in-place removal
    # would require rebuilding the shared product asset enum and can corrupt history.
    if bind.dialect.name == "postgresql":
        LOCAL_EDIT_TASK_STATUS.drop(bind, checkfirst=True)
