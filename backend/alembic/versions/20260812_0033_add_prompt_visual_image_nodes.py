"""add prompt, visual system, and one-image node persistence

Revision ID: 20260812_0033
Revises: 20260811_0032
Create Date: 2026-08-12
"""

from __future__ import annotations

import sqlalchemy as sa

from alembic import op

revision = "20260812_0033"
down_revision = "20260811_0032"
branch_labels = None
depends_on = None

DRAFT_VISUAL_VERSION_FK = "fk_workflow_draft_revisions_visual_system_version_id"
WORKFLOW_VISUAL_VERSION_FK = "fk_product_workflows_visual_system_version_id"
NODE_PROMPT_VERSION_FK = "fk_workflow_nodes_current_prompt_artifact_version_id"


def _alter_existing_table(table_name: str):
    bind = op.get_bind()
    return op.batch_alter_table(table_name, recreate="always" if bind.dialect.name == "sqlite" else "auto")


def _create_visual_system_tables() -> None:
    op.create_table(
        "visual_systems",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("name", sa.String(length=255), nullable=False),
        sa.Column("archived_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), nullable=False),
        sa.PrimaryKeyConstraint("id"),
    )
    op.create_index("ix_visual_systems_archived_at", "visual_systems", ["archived_at"])

    op.create_table(
        "visual_system_versions",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("visual_system_id", sa.String(length=36), nullable=False),
        sa.Column("version", sa.Integer(), nullable=False),
        sa.Column("schema_version", sa.Integer(), nullable=False),
        sa.Column("payload_json", sa.JSON(), nullable=False),
        sa.Column("payload_hash", sa.String(length=64), nullable=False),
        sa.Column("source_markdown", sa.Text(), nullable=True),
        sa.Column("source_draft_revision_id", sa.String(length=36), nullable=True),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.CheckConstraint("version > 0", name="ck_visual_system_versions_positive_version"),
        sa.CheckConstraint("schema_version = 1", name="ck_visual_system_versions_schema_version"),
        sa.CheckConstraint("length(payload_hash) = 64", name="ck_visual_system_versions_payload_hash"),
        sa.ForeignKeyConstraint(
            ["visual_system_id"],
            ["visual_systems.id"],
            name="fk_visual_system_versions_system_id",
            ondelete="CASCADE",
        ),
        sa.ForeignKeyConstraint(
            ["source_draft_revision_id"],
            ["workflow_draft_revisions.id"],
            name="fk_visual_system_versions_source_draft_revision_id",
            ondelete="SET NULL",
        ),
        sa.PrimaryKeyConstraint("id"),
        sa.UniqueConstraint(
            "visual_system_id",
            "version",
            name="uq_visual_system_versions_system_version",
        ),
        sa.UniqueConstraint(
            "source_draft_revision_id",
            name="uq_visual_system_versions_source_draft_revision_id",
        ),
    )

    op.create_table(
        "visual_system_version_references",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("visual_system_version_id", sa.String(length=36), nullable=False),
        sa.Column("asset_id", sa.String(length=36), nullable=False),
        sa.Column("role", sa.String(length=120), nullable=False),
        sa.Column("label", sa.String(length=255), nullable=False),
        sa.Column("position", sa.Integer(), nullable=False),
        sa.CheckConstraint(
            "position >= 0",
            name="ck_visual_system_version_references_non_negative_position",
        ),
        sa.ForeignKeyConstraint(
            ["visual_system_version_id"],
            ["visual_system_versions.id"],
            name="fk_visual_system_version_references_version_id",
            ondelete="CASCADE",
        ),
        sa.ForeignKeyConstraint(
            ["asset_id"],
            ["product_image_assets.id"],
            name="fk_visual_system_version_references_asset_id",
            ondelete="RESTRICT",
        ),
        sa.PrimaryKeyConstraint("id"),
        sa.UniqueConstraint(
            "visual_system_version_id",
            "position",
            name="uq_visual_system_version_references_position",
        ),
        sa.UniqueConstraint(
            "visual_system_version_id",
            "asset_id",
            "role",
            name="uq_visual_system_version_references_asset_role",
        ),
    )

    with _alter_existing_table("workflow_draft_revisions") as batch_op:
        batch_op.add_column(sa.Column("visual_system_version_id", sa.String(length=36), nullable=True))
        batch_op.create_foreign_key(
            DRAFT_VISUAL_VERSION_FK,
            "visual_system_versions",
            ["visual_system_version_id"],
            ["id"],
            ondelete="SET NULL",
        )

    with _alter_existing_table("product_workflows") as batch_op:
        batch_op.add_column(sa.Column("visual_system_version_id", sa.String(length=36), nullable=True))
        batch_op.create_foreign_key(
            WORKFLOW_VISUAL_VERSION_FK,
            "visual_system_versions",
            ["visual_system_version_id"],
            ["id"],
            ondelete="RESTRICT",
        )


def _create_prompt_artifact_tables() -> None:
    op.create_table(
        "image_prompt_artifacts",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("workflow_id", sa.String(length=36), nullable=False),
        sa.Column("image_type_key", sa.String(length=80), nullable=False),
        sa.Column("title", sa.String(length=255), nullable=False),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), nullable=False),
        sa.ForeignKeyConstraint(
            ["workflow_id"],
            ["product_workflows.id"],
            name="fk_image_prompt_artifacts_workflow_id",
            ondelete="CASCADE",
        ),
        sa.PrimaryKeyConstraint("id"),
        sa.UniqueConstraint(
            "workflow_id",
            "image_type_key",
            name="uq_image_prompt_artifacts_workflow_type",
        ),
    )

    op.create_table(
        "image_prompt_artifact_versions",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("artifact_id", sa.String(length=36), nullable=False),
        sa.Column("version", sa.Integer(), nullable=False),
        sa.Column("schema_version", sa.Integer(), nullable=False),
        sa.Column("payload_json", sa.JSON(), nullable=False),
        sa.Column("payload_hash", sa.String(length=64), nullable=False),
        sa.Column("source_draft_revision_id", sa.String(length=36), nullable=True),
        sa.Column("source_node_run_id", sa.String(length=36), nullable=True),
        sa.Column("provider_name", sa.String(length=80), nullable=True),
        sa.Column("provider_model", sa.String(length=255), nullable=True),
        sa.Column("provider_response_id", sa.String(length=255), nullable=True),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.CheckConstraint("version > 0", name="ck_image_prompt_artifact_versions_positive_version"),
        sa.CheckConstraint("schema_version = 1", name="ck_image_prompt_artifact_versions_schema_version"),
        sa.CheckConstraint(
            "length(payload_hash) = 64",
            name="ck_image_prompt_artifact_versions_payload_hash",
        ),
        sa.ForeignKeyConstraint(
            ["artifact_id"],
            ["image_prompt_artifacts.id"],
            name="fk_image_prompt_artifact_versions_artifact_id",
            ondelete="CASCADE",
        ),
        sa.ForeignKeyConstraint(
            ["source_draft_revision_id"],
            ["workflow_draft_revisions.id"],
            name="fk_image_prompt_artifact_versions_source_draft_revision_id",
            ondelete="SET NULL",
        ),
        sa.ForeignKeyConstraint(
            ["source_node_run_id"],
            ["workflow_node_runs.id"],
            name="fk_image_prompt_artifact_versions_source_node_run_id",
            ondelete="SET NULL",
        ),
        sa.PrimaryKeyConstraint("id"),
        sa.UniqueConstraint(
            "artifact_id",
            "version",
            name="uq_image_prompt_artifact_versions_artifact_version",
        ),
        sa.UniqueConstraint(
            "source_node_run_id",
            name="uq_image_prompt_artifact_versions_source_node_run_id",
        ),
    )

    op.create_table(
        "image_prompt_artifact_version_references",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("prompt_artifact_version_id", sa.String(length=36), nullable=False),
        sa.Column("asset_id", sa.String(length=36), nullable=False),
        sa.Column("purpose", sa.String(length=120), nullable=False),
        sa.Column("position", sa.Integer(), nullable=False),
        sa.CheckConstraint(
            "position >= 0",
            name="ck_prompt_artifact_version_references_non_negative_position",
        ),
        sa.ForeignKeyConstraint(
            ["prompt_artifact_version_id"],
            ["image_prompt_artifact_versions.id"],
            name="fk_image_prompt_artifact_version_references_version_id",
            ondelete="CASCADE",
        ),
        sa.ForeignKeyConstraint(
            ["asset_id"],
            ["product_image_assets.id"],
            name="fk_image_prompt_artifact_version_references_asset_id",
            ondelete="RESTRICT",
        ),
        sa.PrimaryKeyConstraint("id"),
        sa.UniqueConstraint(
            "prompt_artifact_version_id",
            "position",
            name="uq_image_prompt_artifact_version_references_position",
        ),
        sa.UniqueConstraint(
            "prompt_artifact_version_id",
            "asset_id",
            "purpose",
            name="uq_image_prompt_artifact_version_references_asset_purpose",
        ),
    )

    with _alter_existing_table("workflow_nodes") as batch_op:
        batch_op.add_column(
            sa.Column("current_prompt_artifact_version_id", sa.String(length=36), nullable=True)
        )
        batch_op.create_foreign_key(
            NODE_PROMPT_VERSION_FK,
            "image_prompt_artifact_versions",
            ["current_prompt_artifact_version_id"],
            ["id"],
            ondelete="SET NULL",
        )


def _create_visual_exception_table() -> None:
    op.create_table(
        "visual_exceptions",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("workflow_id", sa.String(length=36), nullable=False),
        sa.Column("source_draft_revision_id", sa.String(length=36), nullable=True),
        sa.Column("exception_key", sa.String(length=80), nullable=False),
        sa.Column("scope_type", sa.String(length=40), nullable=False),
        sa.Column("scope_key", sa.String(length=80), nullable=True),
        sa.Column("overrides_json", sa.JSON(), nullable=False),
        sa.Column("reason", sa.Text(), nullable=False),
        sa.Column("confirmed_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.CheckConstraint(
            "(scope_type = 'workflow' AND scope_key IS NULL) OR "
            "(scope_type IN ('image_type', 'image_plan') AND scope_key IS NOT NULL)",
            name="ck_visual_exceptions_scope",
        ),
        sa.ForeignKeyConstraint(
            ["workflow_id"],
            ["product_workflows.id"],
            name="fk_visual_exceptions_workflow_id",
            ondelete="CASCADE",
        ),
        sa.ForeignKeyConstraint(
            ["source_draft_revision_id"],
            ["workflow_draft_revisions.id"],
            name="fk_visual_exceptions_source_draft_revision_id",
            ondelete="SET NULL",
        ),
        sa.PrimaryKeyConstraint("id"),
        sa.UniqueConstraint(
            "workflow_id",
            "exception_key",
            name="uq_visual_exceptions_workflow_key",
        ),
    )


def _create_image_generation_tables() -> None:
    op.create_table(
        "workflow_image_generation_records",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("workflow_node_run_id", sa.String(length=36), nullable=False),
        sa.Column("product_id", sa.String(length=36), nullable=False),
        sa.Column("workflow_id", sa.String(length=36), nullable=False),
        sa.Column("node_id", sa.String(length=36), nullable=False),
        sa.Column("result_asset_id", sa.String(length=36), nullable=False),
        sa.Column("visual_system_version_id", sa.String(length=36), nullable=False),
        sa.Column("prompt_artifact_version_id", sa.String(length=36), nullable=False),
        sa.Column("requested_spec_json", sa.JSON(), nullable=False),
        sa.Column("effective_parameters_json", sa.JSON(), nullable=False),
        sa.Column("actual_media_json", sa.JSON(), nullable=False),
        sa.Column("compiled_prompt", sa.Text(), nullable=False),
        sa.Column("compiled_prompt_hash", sa.String(length=64), nullable=False),
        sa.Column("provider_name", sa.String(length=80), nullable=False),
        sa.Column("provider_model", sa.String(length=255), nullable=False),
        sa.Column("provider_response_id", sa.String(length=255), nullable=True),
        sa.Column("provider_status", sa.String(length=80), nullable=False),
        sa.Column("provider_request_json", sa.JSON(), nullable=True),
        sa.Column("provider_output_json", sa.JSON(), nullable=True),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.CheckConstraint(
            "length(compiled_prompt_hash) = 64",
            name="ck_workflow_image_generation_records_prompt_hash",
        ),
        sa.ForeignKeyConstraint(
            ["workflow_node_run_id"],
            ["workflow_node_runs.id"],
            name="fk_workflow_image_generation_records_node_run_id",
            ondelete="CASCADE",
        ),
        sa.ForeignKeyConstraint(
            ["product_id"],
            ["products.id"],
            name="fk_workflow_image_generation_records_product_id",
            ondelete="CASCADE",
        ),
        sa.ForeignKeyConstraint(
            ["workflow_id"],
            ["product_workflows.id"],
            name="fk_workflow_image_generation_records_workflow_id",
            ondelete="CASCADE",
        ),
        sa.ForeignKeyConstraint(
            ["node_id"],
            ["workflow_nodes.id"],
            name="fk_workflow_image_generation_records_node_id",
            ondelete="CASCADE",
        ),
        sa.ForeignKeyConstraint(
            ["result_asset_id"],
            ["product_image_assets.id"],
            name="fk_workflow_image_generation_records_result_asset_id",
            ondelete="RESTRICT",
        ),
        sa.ForeignKeyConstraint(
            ["visual_system_version_id"],
            ["visual_system_versions.id"],
            name="fk_workflow_image_generation_records_visual_system_version_id",
            ondelete="RESTRICT",
        ),
        sa.ForeignKeyConstraint(
            ["prompt_artifact_version_id"],
            ["image_prompt_artifact_versions.id"],
            name="fk_workflow_image_generation_records_prompt_artifact_version_id",
            ondelete="RESTRICT",
        ),
        sa.PrimaryKeyConstraint("id"),
        sa.UniqueConstraint(
            "workflow_node_run_id",
            name="uq_workflow_image_generation_records_node_run_id",
        ),
    )

    op.create_table(
        "workflow_image_generation_references",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("generation_record_id", sa.String(length=36), nullable=False),
        sa.Column("asset_id", sa.String(length=36), nullable=False),
        sa.Column("role", sa.String(length=120), nullable=False),
        sa.Column("label", sa.String(length=255), nullable=False),
        sa.Column("position", sa.Integer(), nullable=False),
        sa.CheckConstraint(
            "position >= 0",
            name="ck_workflow_image_generation_references_non_negative_position",
        ),
        sa.ForeignKeyConstraint(
            ["generation_record_id"],
            ["workflow_image_generation_records.id"],
            name="fk_workflow_image_generation_references_record_id",
            ondelete="CASCADE",
        ),
        sa.ForeignKeyConstraint(
            ["asset_id"],
            ["product_image_assets.id"],
            name="fk_workflow_image_generation_references_asset_id",
            ondelete="RESTRICT",
        ),
        sa.PrimaryKeyConstraint("id"),
        sa.UniqueConstraint(
            "generation_record_id",
            "position",
            name="uq_workflow_image_generation_references_position",
        ),
        sa.UniqueConstraint(
            "generation_record_id",
            "asset_id",
            "role",
            name="uq_workflow_image_generation_references_asset_role",
        ),
    )


def upgrade() -> None:
    _create_visual_system_tables()
    _create_prompt_artifact_tables()
    _create_visual_exception_table()
    _create_image_generation_tables()


def downgrade() -> None:
    op.drop_table("workflow_image_generation_references")
    op.drop_table("workflow_image_generation_records")
    op.drop_table("visual_exceptions")

    with _alter_existing_table("workflow_nodes") as batch_op:
        batch_op.drop_constraint(NODE_PROMPT_VERSION_FK, type_="foreignkey")
        batch_op.drop_column("current_prompt_artifact_version_id")
    op.drop_table("image_prompt_artifact_version_references")
    op.drop_table("image_prompt_artifact_versions")
    op.drop_table("image_prompt_artifacts")

    with _alter_existing_table("product_workflows") as batch_op:
        batch_op.drop_constraint(WORKFLOW_VISUAL_VERSION_FK, type_="foreignkey")
        batch_op.drop_column("visual_system_version_id")
    with _alter_existing_table("workflow_draft_revisions") as batch_op:
        batch_op.drop_constraint(DRAFT_VISUAL_VERSION_FK, type_="foreignkey")
        batch_op.drop_column("visual_system_version_id")
    op.drop_table("visual_system_version_references")
    op.drop_index("ix_visual_systems_archived_at", table_name="visual_systems")
    op.drop_table("visual_system_versions")
    op.drop_table("visual_systems")
