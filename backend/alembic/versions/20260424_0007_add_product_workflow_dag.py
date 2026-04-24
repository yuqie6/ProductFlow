"""add product workflow dag tables"""

import sqlalchemy as sa
from sqlalchemy.dialects import postgresql

from alembic import op

revision = "20260424_0007"
down_revision = "20260424_0006"
branch_labels = None
depends_on = None

workflow_node_type = postgresql.ENUM(
    "product_context",
    "reference_image",
    "copy_generation",
    "image_generation",
    name="workflownodetype",
    create_type=False,
)
workflow_node_status = postgresql.ENUM(
    "idle",
    "queued",
    "running",
    "succeeded",
    "failed",
    name="workflownodestatus",
    create_type=False,
)
workflow_run_status = postgresql.ENUM(
    "running",
    "succeeded",
    "failed",
    name="workflowrunstatus",
    create_type=False,
)


def upgrade() -> None:
    bind = op.get_bind()
    workflow_node_type.create(bind, checkfirst=True)
    workflow_node_status.create(bind, checkfirst=True)
    workflow_run_status.create(bind, checkfirst=True)

    op.create_table(
        "product_workflows",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("product_id", sa.String(length=36), nullable=False),
        sa.Column("title", sa.String(length=255), nullable=False),
        sa.Column("active", sa.Boolean(), nullable=False),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), nullable=False),
        sa.ForeignKeyConstraint(["product_id"], ["products.id"], ondelete="CASCADE"),
        sa.PrimaryKeyConstraint("id"),
    )
    op.create_index(
        "uq_product_workflows_one_active_per_product",
        "product_workflows",
        ["product_id"],
        unique=True,
        postgresql_where=sa.text("active = true"),
        sqlite_where=sa.text("active = 1"),
    )

    op.create_table(
        "workflow_nodes",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("workflow_id", sa.String(length=36), nullable=False),
        sa.Column("node_type", workflow_node_type, nullable=False),
        sa.Column("title", sa.String(length=255), nullable=False),
        sa.Column("position_x", sa.Integer(), nullable=False),
        sa.Column("position_y", sa.Integer(), nullable=False),
        sa.Column("config_json", sa.JSON(), nullable=False),
        sa.Column("status", workflow_node_status, nullable=False),
        sa.Column("output_json", sa.JSON(), nullable=True),
        sa.Column("failure_reason", sa.Text(), nullable=True),
        sa.Column("last_run_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), nullable=False),
        sa.ForeignKeyConstraint(["workflow_id"], ["product_workflows.id"], ondelete="CASCADE"),
        sa.PrimaryKeyConstraint("id"),
    )

    op.create_table(
        "workflow_edges",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("workflow_id", sa.String(length=36), nullable=False),
        sa.Column("source_node_id", sa.String(length=36), nullable=False),
        sa.Column("target_node_id", sa.String(length=36), nullable=False),
        sa.Column("source_handle", sa.String(length=80), nullable=True),
        sa.Column("target_handle", sa.String(length=80), nullable=True),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.ForeignKeyConstraint(["source_node_id"], ["workflow_nodes.id"], ondelete="CASCADE"),
        sa.ForeignKeyConstraint(["target_node_id"], ["workflow_nodes.id"], ondelete="CASCADE"),
        sa.ForeignKeyConstraint(["workflow_id"], ["product_workflows.id"], ondelete="CASCADE"),
        sa.PrimaryKeyConstraint("id"),
    )

    op.create_table(
        "workflow_runs",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("workflow_id", sa.String(length=36), nullable=False),
        sa.Column("status", workflow_run_status, nullable=False),
        sa.Column("started_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("finished_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("failure_reason", sa.Text(), nullable=True),
        sa.ForeignKeyConstraint(["workflow_id"], ["product_workflows.id"], ondelete="CASCADE"),
        sa.PrimaryKeyConstraint("id"),
    )

    op.create_table(
        "workflow_node_runs",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("workflow_run_id", sa.String(length=36), nullable=False),
        sa.Column("node_id", sa.String(length=36), nullable=False),
        sa.Column("status", workflow_node_status, nullable=False),
        sa.Column("output_json", sa.JSON(), nullable=True),
        sa.Column("failure_reason", sa.Text(), nullable=True),
        sa.Column("copy_set_id", sa.String(length=36), nullable=True),
        sa.Column("poster_variant_id", sa.String(length=36), nullable=True),
        sa.Column("image_session_asset_id", sa.String(length=36), nullable=True),
        sa.Column("started_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("finished_at", sa.DateTime(timezone=True), nullable=True),
        sa.ForeignKeyConstraint(["copy_set_id"], ["copy_sets.id"], ondelete="SET NULL"),
        sa.ForeignKeyConstraint(["image_session_asset_id"], ["image_session_assets.id"], ondelete="SET NULL"),
        sa.ForeignKeyConstraint(["node_id"], ["workflow_nodes.id"], ondelete="CASCADE"),
        sa.ForeignKeyConstraint(["poster_variant_id"], ["poster_variants.id"], ondelete="SET NULL"),
        sa.ForeignKeyConstraint(["workflow_run_id"], ["workflow_runs.id"], ondelete="CASCADE"),
        sa.PrimaryKeyConstraint("id"),
    )
    op.create_index("ix_workflow_node_runs_run_node", "workflow_node_runs", ["workflow_run_id", "node_id"])


def downgrade() -> None:
    op.drop_index("ix_workflow_node_runs_run_node", table_name="workflow_node_runs")
    op.drop_table("workflow_node_runs")
    op.drop_table("workflow_runs")
    op.drop_table("workflow_edges")
    op.drop_table("workflow_nodes")
    op.drop_index("uq_product_workflows_one_active_per_product", table_name="product_workflows")
    op.drop_table("product_workflows")

    bind = op.get_bind()
    workflow_run_status.drop(bind, checkfirst=True)
    workflow_node_status.drop(bind, checkfirst=True)
    workflow_node_type.drop(bind, checkfirst=True)
