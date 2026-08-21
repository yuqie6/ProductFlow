"""add schema-v3 graph runs and artifacts

Revision ID: 20260821_0076
Revises: 20260821_0075
Create Date: 2026-08-21
"""

from __future__ import annotations

import sqlalchemy as sa

from alembic import op

revision = "20260821_0076"
down_revision = "20260821_0075"
branch_labels = None
depends_on = None


def upgrade() -> None:
    op.create_table(
        "workflow_graph_runs",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("graph_id", sa.String(length=36), nullable=False),
        sa.Column("status", sa.String(length=40), nullable=False),
        sa.Column("run_scope", sa.String(length=40), nullable=False),
        sa.Column("requested_node_id", sa.String(length=36), nullable=True),
        sa.Column("graph_revision", sa.Integer(), nullable=False),
        sa.Column("snapshot_json", sa.JSON(), nullable=False),
        sa.Column("failure_reason", sa.Text(), nullable=True),
        sa.Column("is_retryable", sa.Boolean(), nullable=False),
        sa.Column("progress_metadata", sa.JSON(), nullable=True),
        sa.Column("started_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("finished_at", sa.DateTime(timezone=True), nullable=True),
        sa.CheckConstraint("graph_revision > 0", name="ck_workflow_graph_runs_positive_revision"),
        sa.CheckConstraint(
            "run_scope IN ('node', 'to_node', 'graph')",
            name="ck_workflow_graph_runs_scope",
        ),
        sa.CheckConstraint(
            "status IN ('running', 'succeeded', 'failed', 'cancelled', 'unknown')",
            name="ck_workflow_graph_runs_status",
        ),
        sa.ForeignKeyConstraint(
            ["graph_id"],
            ["workflow_graphs.id"],
            name="fk_workflow_graph_runs_graph_id",
            ondelete="CASCADE",
        ),
        sa.PrimaryKeyConstraint("id"),
    )
    op.create_index("ix_workflow_graph_runs_graph_id", "workflow_graph_runs", ["graph_id"])
    op.create_index(
        "uq_workflow_graph_runs_one_active_per_graph",
        "workflow_graph_runs",
        ["graph_id"],
        unique=True,
        postgresql_where=sa.text("status = 'running'"),
        sqlite_where=sa.text("status = 'running'"),
    )

    op.create_table(
        "workflow_graph_node_runs",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("graph_run_id", sa.String(length=36), nullable=False),
        sa.Column("node_id", sa.String(length=36), nullable=False),
        sa.Column("status", sa.String(length=40), nullable=False),
        sa.Column("sort_order", sa.Integer(), nullable=False),
        sa.Column("compiled_context_json", sa.JSON(), nullable=True),
        sa.Column("output_json", sa.JSON(), nullable=True),
        sa.Column("failure_reason", sa.Text(), nullable=True),
        sa.Column("started_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("finished_at", sa.DateTime(timezone=True), nullable=True),
        sa.CheckConstraint("sort_order >= 0", name="ck_workflow_graph_node_runs_non_negative_order"),
        sa.CheckConstraint(
            "status IN ('idle', 'queued', 'running', 'succeeded', 'failed', 'unknown')",
            name="ck_workflow_graph_node_runs_status",
        ),
        sa.ForeignKeyConstraint(
            ["graph_run_id"],
            ["workflow_graph_runs.id"],
            name="fk_workflow_graph_node_runs_run_id",
            ondelete="CASCADE",
        ),
        sa.ForeignKeyConstraint(
            ["node_id"],
            ["workflow_graph_nodes.id"],
            name="fk_workflow_graph_node_runs_node_id",
            ondelete="CASCADE",
        ),
        sa.PrimaryKeyConstraint("id"),
    )
    op.create_index(
        "ix_workflow_graph_node_runs_run_node",
        "workflow_graph_node_runs",
        ["graph_run_id", "node_id"],
    )
    op.create_index(
        "uq_workflow_graph_node_runs_one_active_per_node",
        "workflow_graph_node_runs",
        ["node_id"],
        unique=True,
        postgresql_where=sa.text("status IN ('queued', 'running')"),
        sqlite_where=sa.text("status IN ('queued', 'running')"),
    )

    op.create_table(
        "workflow_graph_artifacts",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("graph_id", sa.String(length=36), nullable=False),
        sa.Column("node_id", sa.String(length=36), nullable=False),
        sa.Column("node_run_id", sa.String(length=36), nullable=True),
        sa.Column("artifact_type", sa.String(length=40), nullable=False),
        sa.Column("schema_version", sa.Integer(), nullable=False),
        sa.Column("graph_revision", sa.Integer(), nullable=False),
        sa.Column("payload_json", sa.JSON(), nullable=False),
        sa.Column("payload_hash", sa.String(length=64), nullable=False),
        sa.Column("input_digest", sa.String(length=64), nullable=False),
        sa.Column("product_image_asset_id", sa.String(length=36), nullable=True),
        sa.Column("provider_name", sa.String(length=80), nullable=True),
        sa.Column("provider_model", sa.String(length=255), nullable=True),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.CheckConstraint("graph_revision > 0", name="ck_workflow_graph_artifacts_positive_revision"),
        sa.CheckConstraint(
            "artifact_type IN ('prompt', 'image')",
            name="ck_workflow_graph_artifacts_type",
        ),
        sa.CheckConstraint("schema_version = 3", name="ck_workflow_graph_artifacts_schema_version"),
        sa.CheckConstraint("length(payload_hash) = 64", name="ck_workflow_graph_artifacts_payload_hash"),
        sa.CheckConstraint("length(input_digest) = 64", name="ck_workflow_graph_artifacts_input_digest"),
        sa.ForeignKeyConstraint(
            ["graph_id"],
            ["workflow_graphs.id"],
            name="fk_workflow_graph_artifacts_graph_id",
            ondelete="CASCADE",
        ),
        sa.ForeignKeyConstraint(
            ["node_id"],
            ["workflow_graph_nodes.id"],
            name="fk_workflow_graph_artifacts_node_id",
            ondelete="CASCADE",
        ),
        sa.ForeignKeyConstraint(
            ["node_run_id"],
            ["workflow_graph_node_runs.id"],
            name="fk_workflow_graph_artifacts_node_run_id",
            ondelete="SET NULL",
        ),
        sa.ForeignKeyConstraint(
            ["product_image_asset_id"],
            ["product_image_assets.id"],
            name="fk_workflow_graph_artifacts_product_image_asset_id",
            ondelete="RESTRICT",
        ),
        sa.PrimaryKeyConstraint("id"),
        sa.UniqueConstraint("node_run_id", name="uq_workflow_graph_artifacts_node_run_id"),
    )
    op.create_index("ix_workflow_graph_artifacts_graph_id", "workflow_graph_artifacts", ["graph_id"])
    op.create_index("ix_workflow_graph_artifacts_node_id", "workflow_graph_artifacts", ["node_id"])

    with op.batch_alter_table("workflow_graph_nodes") as batch_op:
        batch_op.add_column(sa.Column("current_artifact_id", sa.String(length=36), nullable=True))
        batch_op.create_foreign_key(
            "fk_workflow_graph_nodes_current_artifact_id",
            "workflow_graph_artifacts",
            ["current_artifact_id"],
            ["id"],
            ondelete="SET NULL",
        )


def downgrade() -> None:
    bind = op.get_bind()
    if bind.execute(sa.text("SELECT 1 FROM workflow_graph_runs LIMIT 1")).first() is not None:
        raise RuntimeError("cannot downgrade while schema-v3 graph runs exist")
    with op.batch_alter_table("workflow_graph_nodes") as batch_op:
        batch_op.drop_constraint("fk_workflow_graph_nodes_current_artifact_id", type_="foreignkey")
        batch_op.drop_column("current_artifact_id")
    op.drop_index("ix_workflow_graph_artifacts_node_id", table_name="workflow_graph_artifacts")
    op.drop_index("ix_workflow_graph_artifacts_graph_id", table_name="workflow_graph_artifacts")
    op.drop_table("workflow_graph_artifacts")
    op.drop_index(
        "uq_workflow_graph_node_runs_one_active_per_node",
        table_name="workflow_graph_node_runs",
    )
    op.drop_index("ix_workflow_graph_node_runs_run_node", table_name="workflow_graph_node_runs")
    op.drop_table("workflow_graph_node_runs")
    op.drop_index("uq_workflow_graph_runs_one_active_per_graph", table_name="workflow_graph_runs")
    op.drop_index("ix_workflow_graph_runs_graph_id", table_name="workflow_graph_runs")
    op.drop_table("workflow_graph_runs")
