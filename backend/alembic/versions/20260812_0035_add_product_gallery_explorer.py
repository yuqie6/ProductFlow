"""add product gallery explorer organization

Revision ID: 20260812_0035
Revises: 20260812_0034
Create Date: 2026-08-12
"""

from __future__ import annotations

import sqlalchemy as sa

from alembic import op

revision = "20260812_0035"
down_revision = "20260812_0034"
branch_labels = None
depends_on = None

ASSET_FOLDER_FK = "fk_product_image_assets_user_folder_id"
MUTATION_ASSET_FK = "fk_agent_tool_mutations_asset_id"
GENERATION_RESULT_UNIQUE = "uq_workflow_image_generation_records_result_asset_id"
RENAME_TOOL_NAME = "rename_product_image_asset_v1"


def _alter_existing_table(table_name: str):
    bind = op.get_bind()
    return op.batch_alter_table(table_name, recreate="always" if bind.dialect.name == "sqlite" else "auto")


def _assert_generation_results_are_unique() -> None:
    duplicate = op.get_bind().execute(
        sa.text(
            "SELECT result_asset_id, COUNT(*) AS record_count "
            "FROM workflow_image_generation_records "
            "GROUP BY result_asset_id HAVING COUNT(*) > 1 LIMIT 1"
        )
    ).mappings().first()
    if duplicate is not None:
        raise RuntimeError(
            "workflow image generation records contain duplicate result_asset_id "
            f"{duplicate['result_asset_id']}"
        )


def _backfill_agent_mutation_payloads() -> None:
    bind = op.get_bind()
    rows = bind.execute(
        sa.text(
            "SELECT m.id, m.conversation_id, c.product_id, m.asset_id, "
            "m.expected_display_name, m.target_display_name "
            "FROM agent_tool_mutations AS m "
            "JOIN agent_conversations AS c ON c.id = m.conversation_id"
        )
    ).mappings()
    mutation_table = sa.table(
        "agent_tool_mutations",
        sa.column("id", sa.String()),
        sa.column("prepared_json", sa.JSON()),
    )
    for row in rows:
        payload = {
            "schema_version": 1,
            "operation": "rename_asset",
            "scope": {
                "conversation_id": row["conversation_id"],
                "product_id": row["product_id"],
            },
            "before": {
                "asset_id": row["asset_id"],
                "display_name": row["expected_display_name"],
            },
            "target": {
                "asset_id": row["asset_id"],
                "display_name": row["target_display_name"],
            },
        }
        bind.execute(
            mutation_table.update().where(mutation_table.c.id == row["id"]).values(prepared_json=payload)
        )


def upgrade() -> None:
    _assert_generation_results_are_unique()

    op.create_table(
        "product_asset_folders",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("product_id", sa.String(length=36), nullable=False),
        sa.Column("name", sa.String(length=120), nullable=False),
        sa.Column("sort_order", sa.Integer(), nullable=False),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), nullable=False),
        sa.CheckConstraint(
            "sort_order >= 0",
            name="ck_product_asset_folders_non_negative_sort_order",
        ),
        sa.ForeignKeyConstraint(
            ["product_id"],
            ["products.id"],
            name="fk_product_asset_folders_product_id",
            ondelete="CASCADE",
        ),
        sa.PrimaryKeyConstraint("id"),
        sa.UniqueConstraint(
            "product_id",
            "name",
            name="uq_product_asset_folders_product_name",
        ),
    )
    op.create_index(
        "ix_product_asset_folders_product_sort",
        "product_asset_folders",
        ["product_id", "sort_order", "name", "id"],
    )

    with _alter_existing_table("product_image_assets") as batch_op:
        batch_op.add_column(sa.Column("image_type_key", sa.String(length=80), nullable=True))
        batch_op.add_column(sa.Column("user_folder_id", sa.String(length=36), nullable=True))
        batch_op.create_foreign_key(
            ASSET_FOLDER_FK,
            "product_asset_folders",
            ["user_folder_id"],
            ["id"],
            ondelete="SET NULL",
        )
        batch_op.create_index(
            "ix_product_image_assets_product_folder_created",
            ["product_id", "user_folder_id", "created_at", "id"],
        )
        batch_op.create_index(
            "ix_product_image_assets_product_type_created",
            ["product_id", "image_type_key", "created_at", "id"],
        )
        batch_op.create_index(
            "ix_product_image_assets_product_origin_created",
            ["product_id", "origin_type", "created_at", "id"],
        )

    op.execute(
        sa.text(
            "UPDATE product_image_assets SET image_type_key = ("
            "SELECT prompt.image_type_key "
            "FROM workflow_image_generation_records AS generation "
            "JOIN image_prompt_artifact_versions AS version "
            "ON version.id = generation.prompt_artifact_version_id "
            "JOIN image_prompt_artifacts AS prompt ON prompt.id = version.artifact_id "
            "WHERE generation.result_asset_id = product_image_assets.id"
            ") WHERE EXISTS ("
            "SELECT 1 FROM workflow_image_generation_records AS generation "
            "WHERE generation.result_asset_id = product_image_assets.id"
            ")"
        )
    )
    with _alter_existing_table("workflow_image_generation_records") as batch_op:
        batch_op.create_unique_constraint(GENERATION_RESULT_UNIQUE, ["result_asset_id"])

    with _alter_existing_table("agent_tool_mutations") as batch_op:
        batch_op.add_column(sa.Column("prepared_json", sa.JSON(), nullable=True))
    _backfill_agent_mutation_payloads()
    with _alter_existing_table("agent_tool_mutations") as batch_op:
        batch_op.drop_constraint(MUTATION_ASSET_FK, type_="foreignkey")
        batch_op.alter_column(
            "asset_id",
            existing_type=sa.String(length=36),
            nullable=True,
        )
        batch_op.alter_column(
            "expected_display_name",
            existing_type=sa.String(length=255),
            nullable=True,
        )
        batch_op.alter_column(
            "target_display_name",
            existing_type=sa.String(length=255),
            nullable=True,
        )
        batch_op.alter_column("prepared_json", existing_type=sa.JSON(), nullable=False)
        batch_op.create_foreign_key(
            MUTATION_ASSET_FK,
            "product_image_assets",
            ["asset_id"],
            ["id"],
            ondelete="SET NULL",
        )


def _assert_downgrade_is_safe() -> None:
    bind = op.get_bind()
    if bind.execute(sa.text("SELECT 1 FROM product_asset_folders LIMIT 1")).first() is not None:
        raise RuntimeError("cannot downgrade gallery explorer while user folders exist")
    unsafe_mutation = bind.execute(
        sa.text(
            "SELECT id FROM agent_tool_mutations "
            "WHERE tool_name <> :rename_tool OR asset_id IS NULL "
            "OR expected_display_name IS NULL OR target_display_name IS NULL LIMIT 1"
        ),
        {"rename_tool": RENAME_TOOL_NAME},
    ).first()
    if unsafe_mutation is not None:
        raise RuntimeError("cannot downgrade gallery explorer while v2-only mutation ledger rows exist")


def downgrade() -> None:
    _assert_downgrade_is_safe()

    with _alter_existing_table("agent_tool_mutations") as batch_op:
        batch_op.drop_constraint(MUTATION_ASSET_FK, type_="foreignkey")
        batch_op.alter_column(
            "asset_id",
            existing_type=sa.String(length=36),
            nullable=False,
        )
        batch_op.alter_column(
            "expected_display_name",
            existing_type=sa.String(length=255),
            nullable=False,
        )
        batch_op.alter_column(
            "target_display_name",
            existing_type=sa.String(length=255),
            nullable=False,
        )
        batch_op.create_foreign_key(
            MUTATION_ASSET_FK,
            "product_image_assets",
            ["asset_id"],
            ["id"],
            ondelete="RESTRICT",
        )
        batch_op.drop_column("prepared_json")

    with _alter_existing_table("workflow_image_generation_records") as batch_op:
        batch_op.drop_constraint(GENERATION_RESULT_UNIQUE, type_="unique")

    with _alter_existing_table("product_image_assets") as batch_op:
        batch_op.drop_index("ix_product_image_assets_product_origin_created")
        batch_op.drop_index("ix_product_image_assets_product_type_created")
        batch_op.drop_index("ix_product_image_assets_product_folder_created")
        batch_op.drop_constraint(ASSET_FOLDER_FK, type_="foreignkey")
        batch_op.drop_column("user_folder_id")
        batch_op.drop_column("image_type_key")

    op.drop_index("ix_product_asset_folders_product_sort", table_name="product_asset_folders")
    op.drop_table("product_asset_folders")
