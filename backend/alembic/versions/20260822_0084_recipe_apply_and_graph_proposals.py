"""recipe live-graph apply records and unapplied graph proposals

Revision ID: 20260822_0084
Revises: 20260822_0083
Create Date: 2026-08-22
"""

from __future__ import annotations

import sqlalchemy as sa

from alembic import op

revision = "20260822_0084"
down_revision = "20260822_0083"
branch_labels = None
depends_on = None


def upgrade() -> None:
    op.create_table(
        "workflow_graph_proposals",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("graph_id", sa.String(length=36), nullable=False),
        sa.Column("conversation_id", sa.String(length=36), nullable=True),
        sa.Column("status", sa.String(length=16), nullable=False),
        sa.Column("summary", sa.String(length=500), nullable=False),
        sa.Column("base_graph_revision", sa.Integer(), nullable=False),
        sa.Column("change_set_json", sa.JSON(), nullable=False),
        sa.Column("operation_group_id", sa.String(length=36), nullable=True),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("resolved_at", sa.DateTime(timezone=True), nullable=True),
        sa.CheckConstraint(
            "status IN ('pending', 'confirmed', 'discarded')",
            name="ck_workflow_graph_proposals_status",
        ),
        sa.CheckConstraint(
            "base_graph_revision >= 0",
            name="ck_workflow_graph_proposals_non_negative_base",
        ),
        sa.ForeignKeyConstraint(
            ["graph_id"],
            ["workflow_graphs.id"],
            name="fk_workflow_graph_proposals_graph_id",
            ondelete="CASCADE",
        ),
        sa.ForeignKeyConstraint(
            ["conversation_id"],
            ["agent_conversations.id"],
            name="fk_workflow_graph_proposals_conversation_id",
            ondelete="SET NULL",
        ),
        sa.ForeignKeyConstraint(
            ["operation_group_id"],
            ["workflow_operation_groups.id"],
            name="fk_workflow_graph_proposals_operation_group_id",
            ondelete="SET NULL",
        ),
        sa.PrimaryKeyConstraint("id"),
    )
    op.create_index(
        "uq_workflow_graph_proposals_one_pending_per_graph",
        "workflow_graph_proposals",
        ["graph_id"],
        unique=True,
        sqlite_where=sa.text("status = 'pending'"),
        postgresql_where=sa.text("status = 'pending'"),
    )
    op.create_table(
        "workflow_recipe_applications",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("product_id", sa.String(length=36), nullable=False),
        sa.Column("recipe_version_id", sa.String(length=36), nullable=False),
        sa.Column("graph_id", sa.String(length=36), nullable=False),
        sa.Column("operation_group_id", sa.String(length=36), nullable=False),
        sa.Column("mode", sa.String(length=16), nullable=False),
        sa.Column("schema_version", sa.Integer(), nullable=False),
        sa.Column("idempotency_key", sa.String(length=120), nullable=False),
        sa.Column("request_hash", sa.String(length=64), nullable=False),
        sa.Column("added_node_ids_json", sa.JSON(), nullable=False),
        sa.Column("added_edge_ids_json", sa.JSON(), nullable=False),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.CheckConstraint("schema_version = 1", name="ck_workflow_recipe_applications_schema_version"),
        sa.CheckConstraint(
            "length(request_hash) = 64",
            name="ck_workflow_recipe_applications_request_hash",
        ),
        sa.CheckConstraint("mode IN ('create', 'merge')", name="ck_workflow_recipe_applications_mode"),
        sa.ForeignKeyConstraint(
            ["product_id"],
            ["products.id"],
            name="fk_workflow_recipe_applications_product_id",
            ondelete="CASCADE",
        ),
        sa.ForeignKeyConstraint(
            ["recipe_version_id"],
            ["workflow_recipe_versions.id"],
            name="fk_workflow_recipe_applications_recipe_version_id",
            ondelete="RESTRICT",
        ),
        sa.ForeignKeyConstraint(
            ["graph_id"],
            ["workflow_graphs.id"],
            name="fk_workflow_recipe_applications_graph_id",
            ondelete="CASCADE",
        ),
        sa.ForeignKeyConstraint(
            ["operation_group_id"],
            ["workflow_operation_groups.id"],
            name="fk_workflow_recipe_applications_operation_group_id",
            ondelete="RESTRICT",
        ),
        sa.PrimaryKeyConstraint("id"),
        sa.UniqueConstraint(
            "product_id",
            "idempotency_key",
            name="uq_workflow_recipe_applications_product_key",
        ),
    )
    op.create_index(
        "ix_workflow_recipe_applications_product_created",
        "workflow_recipe_applications",
        ["product_id", "created_at", "id"],
    )


def downgrade() -> None:
    op.drop_index(
        "ix_workflow_recipe_applications_product_created",
        table_name="workflow_recipe_applications",
    )
    op.drop_table("workflow_recipe_applications")
    op.drop_index(
        "uq_workflow_graph_proposals_one_pending_per_graph",
        table_name="workflow_graph_proposals",
    )
    op.drop_table("workflow_graph_proposals")
