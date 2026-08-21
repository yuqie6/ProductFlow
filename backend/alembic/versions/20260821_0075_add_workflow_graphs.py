"""add schema-v3 workflow graphs

Revision ID: 20260821_0075
Revises: 20260820_0070
Create Date: 2026-08-21
"""

from __future__ import annotations

import sqlalchemy as sa

from alembic import op

revision = "20260821_0075"
down_revision = "20260820_0070"
branch_labels = None
depends_on = None

GRAPH_NODE_TYPES = (
    "product_source",
    "image_asset",
    "creative_brief",
    "visual_system",
    "prompt_generation",
    "image_generation",
)
GRAPH_EDGE_DATA_TYPES = (
    "product_facts",
    "image_asset",
    "creative_brief",
    "visual_system",
    "prompt",
)
GRAPH_EDGE_ROLES = (
    "facts",
    "reference",
    "brief",
    "visual_guidance",
    "prompt",
)
GRAPH_ACTOR_TYPES = ("user", "agent", "recipe")


def _in_clause(values: tuple[str, ...]) -> str:
    return ", ".join(f"'{value}'" for value in values)


def upgrade() -> None:
    op.create_table(
        "workflow_graphs",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("product_id", sa.String(length=36), nullable=False),
        sa.Column("title", sa.String(length=255), nullable=False),
        sa.Column("active", sa.Boolean(), nullable=False),
        sa.Column("schema_version", sa.Integer(), nullable=False),
        sa.Column("revision", sa.Integer(), nullable=False),
        sa.Column("source_draft_revision_id", sa.String(length=36), nullable=True),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), nullable=False),
        sa.CheckConstraint("schema_version = 3", name="ck_workflow_graphs_schema_version"),
        sa.CheckConstraint("revision > 0", name="ck_workflow_graphs_positive_revision"),
        sa.ForeignKeyConstraint(
            ["product_id"],
            ["products.id"],
            name="fk_workflow_graphs_product_id",
            ondelete="CASCADE",
        ),
        sa.ForeignKeyConstraint(
            ["source_draft_revision_id"],
            ["workflow_draft_revisions.id"],
            name="fk_workflow_graphs_source_draft_revision_id",
            ondelete="SET NULL",
        ),
        sa.PrimaryKeyConstraint("id"),
    )
    op.create_index(
        "uq_workflow_graphs_one_active_per_product",
        "workflow_graphs",
        ["product_id"],
        unique=True,
        postgresql_where=sa.text("active = true"),
        sqlite_where=sa.text("active = 1"),
    )
    op.create_index("ix_workflow_graphs_product_id", "workflow_graphs", ["product_id"])

    op.create_table(
        "workflow_graph_groups",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("graph_id", sa.String(length=36), nullable=False),
        sa.Column("title", sa.String(length=255), nullable=False),
        sa.Column("sort_order", sa.Integer(), nullable=False),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), nullable=False),
        sa.CheckConstraint("sort_order >= 0", name="ck_workflow_graph_groups_non_negative_order"),
        sa.ForeignKeyConstraint(
            ["graph_id"],
            ["workflow_graphs.id"],
            name="fk_workflow_graph_groups_graph_id",
            ondelete="CASCADE",
        ),
        sa.PrimaryKeyConstraint("id"),
    )
    op.create_index("ix_workflow_graph_groups_graph_id", "workflow_graph_groups", ["graph_id"])

    op.create_table(
        "workflow_graph_nodes",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("graph_id", sa.String(length=36), nullable=False),
        sa.Column("node_type", sa.String(length=40), nullable=False),
        sa.Column("title", sa.String(length=255), nullable=False),
        sa.Column("position_x", sa.Integer(), nullable=False),
        sa.Column("position_y", sa.Integer(), nullable=False),
        sa.Column("config_json", sa.JSON(), nullable=False),
        sa.Column("bound_image_asset_id", sa.String(length=36), nullable=True),
        sa.Column("group_id", sa.String(length=36), nullable=True),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), nullable=False),
        sa.CheckConstraint(
            f"node_type IN ({_in_clause(GRAPH_NODE_TYPES)})",
            name="ck_workflow_graph_nodes_type",
        ),
        sa.ForeignKeyConstraint(
            ["graph_id"],
            ["workflow_graphs.id"],
            name="fk_workflow_graph_nodes_graph_id",
            ondelete="CASCADE",
        ),
        sa.ForeignKeyConstraint(
            ["bound_image_asset_id"],
            ["product_image_assets.id"],
            name="fk_workflow_graph_nodes_bound_image_asset_id",
            ondelete="RESTRICT",
        ),
        sa.ForeignKeyConstraint(
            ["group_id"],
            ["workflow_graph_groups.id"],
            name="fk_workflow_graph_nodes_group_id",
            ondelete="SET NULL",
        ),
        sa.PrimaryKeyConstraint("id"),
    )
    op.create_index("ix_workflow_graph_nodes_graph_id", "workflow_graph_nodes", ["graph_id"])
    op.create_index("ix_workflow_graph_nodes_bound_image_asset_id", "workflow_graph_nodes", ["bound_image_asset_id"])

    op.create_table(
        "workflow_graph_edges",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("graph_id", sa.String(length=36), nullable=False),
        sa.Column("source_node_id", sa.String(length=36), nullable=False),
        sa.Column("target_node_id", sa.String(length=36), nullable=False),
        sa.Column("data_type", sa.String(length=40), nullable=False),
        sa.Column("role", sa.String(length=40), nullable=False),
        sa.Column("sort_order", sa.Integer(), nullable=False),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.CheckConstraint(
            f"data_type IN ({_in_clause(GRAPH_EDGE_DATA_TYPES)})",
            name="ck_workflow_graph_edges_data_type",
        ),
        sa.CheckConstraint(
            f"role IN ({_in_clause(GRAPH_EDGE_ROLES)})",
            name="ck_workflow_graph_edges_role",
        ),
        sa.CheckConstraint("sort_order >= 0", name="ck_workflow_graph_edges_non_negative_order"),
        sa.ForeignKeyConstraint(
            ["graph_id"],
            ["workflow_graphs.id"],
            name="fk_workflow_graph_edges_graph_id",
            ondelete="CASCADE",
        ),
        sa.ForeignKeyConstraint(
            ["source_node_id"],
            ["workflow_graph_nodes.id"],
            name="fk_workflow_graph_edges_source_node_id",
            ondelete="CASCADE",
        ),
        sa.ForeignKeyConstraint(
            ["target_node_id"],
            ["workflow_graph_nodes.id"],
            name="fk_workflow_graph_edges_target_node_id",
            ondelete="CASCADE",
        ),
        sa.PrimaryKeyConstraint("id"),
        sa.UniqueConstraint(
            "graph_id",
            "source_node_id",
            "target_node_id",
            "role",
            name="uq_workflow_graph_edges_pair_role",
        ),
    )
    op.create_index("ix_workflow_graph_edges_graph_id", "workflow_graph_edges", ["graph_id"])

    op.create_table(
        "workflow_operation_groups",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("graph_id", sa.String(length=36), nullable=False),
        sa.Column("actor_type", sa.String(length=40), nullable=False),
        sa.Column("summary", sa.String(length=500), nullable=False),
        sa.Column("base_revision", sa.Integer(), nullable=False),
        sa.Column("result_revision", sa.Integer(), nullable=False),
        sa.Column("operations_json", sa.JSON(), nullable=False),
        sa.Column("inverse_operations_json", sa.JSON(), nullable=False),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.CheckConstraint("base_revision >= 0", name="ck_workflow_operation_groups_non_negative_base"),
        sa.CheckConstraint(
            "result_revision = base_revision + 1",
            name="ck_workflow_operation_groups_revision_step",
        ),
        sa.CheckConstraint(
            f"actor_type IN ({_in_clause(GRAPH_ACTOR_TYPES)})",
            name="ck_workflow_operation_groups_actor_type",
        ),
        sa.ForeignKeyConstraint(
            ["graph_id"],
            ["workflow_graphs.id"],
            name="fk_workflow_operation_groups_graph_id",
            ondelete="CASCADE",
        ),
        sa.PrimaryKeyConstraint("id"),
        sa.UniqueConstraint("graph_id", "result_revision", name="uq_workflow_operation_groups_graph_revision"),
    )
    op.create_index("ix_workflow_operation_groups_graph_id", "workflow_operation_groups", ["graph_id"])


def downgrade() -> None:
    bind = op.get_bind()
    if bind.execute(sa.text("SELECT 1 FROM workflow_graphs LIMIT 1")).first() is not None:
        raise RuntimeError("cannot downgrade while schema-v3 workflow graphs exist")
    op.drop_index("ix_workflow_operation_groups_graph_id", table_name="workflow_operation_groups")
    op.drop_table("workflow_operation_groups")
    op.drop_index("ix_workflow_graph_edges_graph_id", table_name="workflow_graph_edges")
    op.drop_table("workflow_graph_edges")
    op.drop_index("ix_workflow_graph_nodes_bound_image_asset_id", table_name="workflow_graph_nodes")
    op.drop_index("ix_workflow_graph_nodes_graph_id", table_name="workflow_graph_nodes")
    op.drop_table("workflow_graph_nodes")
    op.drop_index("ix_workflow_graph_groups_graph_id", table_name="workflow_graph_groups")
    op.drop_table("workflow_graph_groups")
    op.drop_index("ix_workflow_graphs_product_id", table_name="workflow_graphs")
    op.drop_index("uq_workflow_graphs_one_active_per_product", table_name="workflow_graphs")
    op.drop_table("workflow_graphs")
