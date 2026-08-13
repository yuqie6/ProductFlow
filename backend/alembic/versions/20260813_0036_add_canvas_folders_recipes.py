"""add canvas folder editing and workflow recipes

Revision ID: 20260813_0036
Revises: 20260812_0035
Create Date: 2026-08-13
"""

from __future__ import annotations

import sqlalchemy as sa
from sqlalchemy.dialects import postgresql

from alembic import op

revision = "20260813_0036"
down_revision = "20260812_0035"
branch_labels = None
depends_on = None

WORKFLOW_RECIPE_KIND = postgresql.ENUM(
    "workflow_recipe",
    "recipe_fragment",
    name="workflowrecipekind",
    create_type=False,
)

RECIPE_CURRENT_VERSION_FK = "fk_workflow_recipes_current_version_id"


def _alter_existing_table(table_name: str):
    bind = op.get_bind()
    return op.batch_alter_table(table_name, recreate="always" if bind.dialect.name == "sqlite" else "auto")


def _create_recipe_tables() -> None:
    bind = op.get_bind()
    WORKFLOW_RECIPE_KIND.create(bind, checkfirst=True)

    op.create_table(
        "workflow_recipes",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("kind", WORKFLOW_RECIPE_KIND, nullable=False),
        sa.Column("current_version_id", sa.String(length=36), nullable=True),
        sa.Column("archived_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), nullable=False),
        sa.PrimaryKeyConstraint("id"),
    )
    op.create_index("ix_workflow_recipes_archived_at", "workflow_recipes", ["archived_at"])

    op.create_table(
        "workflow_recipe_versions",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("recipe_id", sa.String(length=36), nullable=False),
        sa.Column("version", sa.Integer(), nullable=False),
        sa.Column("schema_version", sa.Integer(), nullable=False),
        sa.Column("title", sa.String(length=255), nullable=False),
        sa.Column("description", sa.Text(), nullable=True),
        sa.Column("payload_json", sa.JSON(), nullable=False),
        sa.Column("payload_hash", sa.String(length=64), nullable=False),
        sa.Column("preferred_visual_system_version_id", sa.String(length=36), nullable=True),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.CheckConstraint("version > 0", name="ck_workflow_recipe_versions_positive_version"),
        sa.CheckConstraint("schema_version = 1", name="ck_workflow_recipe_versions_schema_version"),
        sa.CheckConstraint(
            "length(payload_hash) = 64",
            name="ck_workflow_recipe_versions_payload_hash",
        ),
        sa.ForeignKeyConstraint(
            ["recipe_id"],
            ["workflow_recipes.id"],
            name="fk_workflow_recipe_versions_recipe_id",
            ondelete="CASCADE",
        ),
        sa.ForeignKeyConstraint(
            ["preferred_visual_system_version_id"],
            ["visual_system_versions.id"],
            name="fk_workflow_recipe_versions_preferred_visual_system_version_id",
            ondelete="RESTRICT",
        ),
        sa.PrimaryKeyConstraint("id"),
        sa.UniqueConstraint(
            "recipe_id",
            "version",
            name="uq_workflow_recipe_versions_recipe_version",
        ),
    )
    with _alter_existing_table("workflow_recipes") as batch_op:
        batch_op.create_foreign_key(
            RECIPE_CURRENT_VERSION_FK,
            "workflow_recipe_versions",
            ["current_version_id"],
            ["id"],
            ondelete="SET NULL",
        )

    op.create_table(
        "workflow_draft_recipe_seeds",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("workflow_draft_id", sa.String(length=36), nullable=False),
        sa.Column("recipe_version_id", sa.String(length=36), nullable=False),
        sa.Column("product_id", sa.String(length=36), nullable=False),
        sa.Column("base_workflow_id", sa.String(length=36), nullable=True),
        sa.Column("base_workflow_revision", sa.Integer(), nullable=True),
        sa.Column("schema_version", sa.Integer(), nullable=False),
        sa.Column("idempotency_key", sa.String(length=120), nullable=False),
        sa.Column("request_hash", sa.String(length=64), nullable=False),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.CheckConstraint("schema_version = 1", name="ck_workflow_draft_recipe_seeds_schema_version"),
        sa.CheckConstraint(
            "length(request_hash) = 64",
            name="ck_workflow_draft_recipe_seeds_request_hash",
        ),
        sa.CheckConstraint(
            "(base_workflow_id IS NULL AND base_workflow_revision IS NULL) OR "
            "(base_workflow_id IS NOT NULL AND base_workflow_revision > 0)",
            name="ck_workflow_draft_recipe_seeds_base_workflow",
        ),
        sa.ForeignKeyConstraint(
            ["workflow_draft_id"],
            ["workflow_drafts.id"],
            name="fk_workflow_draft_recipe_seeds_draft_id",
            ondelete="CASCADE",
        ),
        sa.ForeignKeyConstraint(
            ["recipe_version_id"],
            ["workflow_recipe_versions.id"],
            name="fk_workflow_draft_recipe_seeds_recipe_version_id",
            ondelete="RESTRICT",
        ),
        sa.ForeignKeyConstraint(
            ["product_id"],
            ["products.id"],
            name="fk_workflow_draft_recipe_seeds_product_id",
            ondelete="CASCADE",
        ),
        sa.ForeignKeyConstraint(
            ["base_workflow_id"],
            ["product_workflows.id"],
            name="fk_workflow_draft_recipe_seeds_base_workflow_id",
            ondelete="RESTRICT",
        ),
        sa.PrimaryKeyConstraint("id"),
        sa.UniqueConstraint(
            "workflow_draft_id",
            name="uq_workflow_draft_recipe_seeds_draft_id",
        ),
        sa.UniqueConstraint(
            "product_id",
            "idempotency_key",
            name="uq_workflow_draft_recipe_seeds_product_key",
        ),
    )
    op.create_index(
        "ix_workflow_draft_recipe_seeds_product_created",
        "workflow_draft_recipe_seeds",
        ["product_id", "created_at", "id"],
    )


def upgrade() -> None:
    with _alter_existing_table("product_workflows") as batch_op:
        batch_op.add_column(
            sa.Column("edit_version", sa.Integer(), nullable=False, server_default=sa.text("0"))
        )
        batch_op.create_check_constraint(
            "ck_product_workflows_non_negative_edit_version",
            "edit_version >= 0",
        )

    op.execute(
        sa.text(
            "DELETE FROM workflow_folders WHERE EXISTS ("
            "SELECT 1 FROM product_workflows "
            "WHERE product_workflows.id = workflow_folders.workflow_id "
            "AND product_workflows.schema_version = 2"
            ") AND NOT EXISTS ("
            "SELECT 1 FROM workflow_nodes WHERE workflow_nodes.folder_id = workflow_folders.id"
            ")"
        )
    )
    with _alter_existing_table("workflow_folders") as batch_op:
        batch_op.drop_constraint("ck_workflow_folders_positive_size", type_="check")
        batch_op.drop_column("config_json")
        batch_op.drop_column("height")
        batch_op.drop_column("width")
        batch_op.drop_column("position_y")
        batch_op.drop_column("position_x")

    _create_recipe_tables()


def _assert_downgrade_is_safe() -> None:
    bind = op.get_bind()
    for table_name in (
        "workflow_draft_recipe_seeds",
        "workflow_recipe_versions",
        "workflow_recipes",
    ):
        if bind.execute(sa.text(f"SELECT 1 FROM {table_name} LIMIT 1")).first() is not None:
            raise RuntimeError("cannot downgrade canvas recipes while saved recipe data exists")


def _restore_folder_geometry() -> None:
    with _alter_existing_table("workflow_folders") as batch_op:
        batch_op.add_column(sa.Column("position_x", sa.Integer(), nullable=True))
        batch_op.add_column(sa.Column("position_y", sa.Integer(), nullable=True))
        batch_op.add_column(sa.Column("width", sa.Integer(), nullable=True))
        batch_op.add_column(sa.Column("height", sa.Integer(), nullable=True))
        batch_op.add_column(sa.Column("config_json", sa.JSON(), nullable=True))
    op.execute(
        sa.text(
            "UPDATE workflow_folders SET position_x = 0, position_y = 0, "
            "width = 640, height = 420, config_json = '{}'"
        )
    )
    with _alter_existing_table("workflow_folders") as batch_op:
        batch_op.alter_column("position_x", existing_type=sa.Integer(), nullable=False)
        batch_op.alter_column("position_y", existing_type=sa.Integer(), nullable=False)
        batch_op.alter_column("width", existing_type=sa.Integer(), nullable=False)
        batch_op.alter_column("height", existing_type=sa.Integer(), nullable=False)
        batch_op.alter_column("config_json", existing_type=sa.JSON(), nullable=False)
        batch_op.create_check_constraint(
            "ck_workflow_folders_positive_size",
            "width > 0 AND height > 0",
        )


def downgrade() -> None:
    _assert_downgrade_is_safe()

    op.drop_index(
        "ix_workflow_draft_recipe_seeds_product_created",
        table_name="workflow_draft_recipe_seeds",
    )
    op.drop_table("workflow_draft_recipe_seeds")
    with _alter_existing_table("workflow_recipes") as batch_op:
        batch_op.drop_constraint(RECIPE_CURRENT_VERSION_FK, type_="foreignkey")
    op.drop_table("workflow_recipe_versions")
    op.drop_index("ix_workflow_recipes_archived_at", table_name="workflow_recipes")
    op.drop_table("workflow_recipes")

    bind = op.get_bind()
    if bind.dialect.name == "postgresql":
        WORKFLOW_RECIPE_KIND.drop(bind, checkfirst=True)

    _restore_folder_geometry()
    with _alter_existing_table("product_workflows") as batch_op:
        batch_op.drop_constraint("ck_product_workflows_non_negative_edit_version", type_="check")
        batch_op.drop_column("edit_version")
