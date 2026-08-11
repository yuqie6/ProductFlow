"""add workflow drafts and atomic v2 materialization

Revision ID: 20260811_0032
Revises: 20260811_0031
Create Date: 2026-08-11
"""

from __future__ import annotations

import sqlalchemy as sa
from sqlalchemy.dialects import postgresql

from alembic import op

revision = "20260811_0032"
down_revision = "20260811_0031"
branch_labels = None
depends_on = None

WORKFLOW_DRAFT_STATUS = postgresql.ENUM(
    "collecting",
    "awaiting_confirmation",
    "confirmed",
    "materializing",
    "ready",
    "failed",
    "cancelled",
    name="workflowdraftstatus",
    create_type=False,
)
WORKFLOW_REVEAL_EVENT_KIND = postgresql.ENUM(
    "folder",
    "node",
    "edge",
    "completed",
    name="workflowrevealeventkind",
    create_type=False,
)

PRODUCT_CURRENT_FACT_FK = "fk_products_current_fact_set_version_id"
DRAFT_CURRENT_REVISION_FK = "fk_workflow_drafts_current_revision_id"
DRAFT_FINAL_WORKFLOW_FK = "fk_workflow_drafts_final_workflow_id"
WORKFLOW_SOURCE_REVISION_FK = "fk_product_workflows_source_draft_revision_id"
NODE_FOLDER_FK = "fk_workflow_nodes_folder_id"
NODE_BOUND_ASSET_FK = "fk_workflow_nodes_bound_image_asset_id"


def _alter_existing_table(table_name: str):
    bind = op.get_bind()
    return op.batch_alter_table(table_name, recreate="always" if bind.dialect.name == "sqlite" else "auto")


def _create_enums() -> None:
    bind = op.get_bind()
    if bind.dialect.name == "postgresql":
        with op.get_context().autocommit_block():
            op.execute("ALTER TYPE workflownodetype ADD VALUE IF NOT EXISTS 'prompt_generation'")
    WORKFLOW_DRAFT_STATUS.create(bind, checkfirst=True)
    WORKFLOW_REVEAL_EVENT_KIND.create(bind, checkfirst=True)


def _create_draft_tables() -> None:
    op.create_table(
        "workflow_drafts",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("product_id", sa.String(length=36), nullable=False),
        sa.Column("status", WORKFLOW_DRAFT_STATUS, nullable=False),
        sa.Column("current_revision_id", sa.String(length=36), nullable=True),
        sa.Column("final_workflow_id", sa.String(length=36), nullable=True),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), nullable=False),
        sa.ForeignKeyConstraint(
            ["product_id"],
            ["products.id"],
            name="fk_workflow_drafts_product_id",
            ondelete="CASCADE",
        ),
        sa.ForeignKeyConstraint(
            ["final_workflow_id"],
            ["product_workflows.id"],
            name=DRAFT_FINAL_WORKFLOW_FK,
            ondelete="SET NULL",
        ),
        sa.PrimaryKeyConstraint("id"),
    )
    op.create_index("ix_workflow_drafts_product_status", "workflow_drafts", ["product_id", "status"])

    op.create_table(
        "workflow_draft_revisions",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("draft_id", sa.String(length=36), nullable=False),
        sa.Column("version", sa.Integer(), nullable=False),
        sa.Column("schema_version", sa.Integer(), nullable=False),
        sa.Column("payload_json", sa.JSON(), nullable=False),
        sa.Column("payload_hash", sa.String(length=64), nullable=False),
        sa.Column("source_turn_id", sa.String(length=120), nullable=True),
        sa.Column("source_artifact_step_id", sa.String(length=120), nullable=True),
        sa.Column("confirmed_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.CheckConstraint("version > 0", name="ck_workflow_draft_revisions_positive_version"),
        sa.CheckConstraint("schema_version = 1", name="ck_workflow_draft_revisions_schema_version"),
        sa.CheckConstraint("length(payload_hash) = 64", name="ck_workflow_draft_revisions_payload_hash"),
        sa.ForeignKeyConstraint(
            ["draft_id"],
            ["workflow_drafts.id"],
            name="fk_workflow_draft_revisions_draft_id",
            ondelete="CASCADE",
        ),
        sa.PrimaryKeyConstraint("id"),
        sa.UniqueConstraint("draft_id", "version", name="uq_workflow_draft_revisions_draft_version"),
        sa.UniqueConstraint(
            "draft_id",
            "source_turn_id",
            "source_artifact_step_id",
            name="uq_workflow_draft_revisions_artifact_origin",
        ),
    )
    with _alter_existing_table("workflow_drafts") as batch_op:
        batch_op.create_foreign_key(
            DRAFT_CURRENT_REVISION_FK,
            "workflow_draft_revisions",
            ["current_revision_id"],
            ["id"],
            ondelete="SET NULL",
        )

    op.create_table(
        "product_fact_set_versions",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("product_id", sa.String(length=36), nullable=False),
        sa.Column("version", sa.Integer(), nullable=False),
        sa.Column("payload_json", sa.JSON(), nullable=False),
        sa.Column("payload_hash", sa.String(length=64), nullable=False),
        sa.Column("source_draft_revision_id", sa.String(length=36), nullable=True),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.CheckConstraint("version > 0", name="ck_product_fact_set_versions_positive_version"),
        sa.CheckConstraint("length(payload_hash) = 64", name="ck_product_fact_set_versions_payload_hash"),
        sa.ForeignKeyConstraint(
            ["product_id"],
            ["products.id"],
            name="fk_product_fact_set_versions_product_id",
            ondelete="CASCADE",
        ),
        sa.ForeignKeyConstraint(
            ["source_draft_revision_id"],
            ["workflow_draft_revisions.id"],
            name="fk_product_fact_set_versions_source_draft_revision_id",
            ondelete="SET NULL",
        ),
        sa.PrimaryKeyConstraint("id"),
        sa.UniqueConstraint("product_id", "version", name="uq_product_fact_set_versions_product_version"),
        sa.UniqueConstraint(
            "source_draft_revision_id",
            name="uq_product_fact_set_versions_source_draft_revision_id",
        ),
    )
    with _alter_existing_table("products") as batch_op:
        batch_op.add_column(sa.Column("current_fact_set_version_id", sa.String(length=36), nullable=True))
        batch_op.create_foreign_key(
            PRODUCT_CURRENT_FACT_FK,
            "product_fact_set_versions",
            ["current_fact_set_version_id"],
            ["id"],
            ondelete="SET NULL",
        )


def _extend_workflow_tables() -> None:
    with _alter_existing_table("product_workflows") as batch_op:
        batch_op.add_column(
            sa.Column("schema_version", sa.Integer(), nullable=False, server_default=sa.text("1"))
        )
        batch_op.add_column(sa.Column("revision", sa.Integer(), nullable=False, server_default=sa.text("1")))
        batch_op.add_column(sa.Column("source_draft_revision_id", sa.String(length=36), nullable=True))
        batch_op.create_foreign_key(
            WORKFLOW_SOURCE_REVISION_FK,
            "workflow_draft_revisions",
            ["source_draft_revision_id"],
            ["id"],
            ondelete="SET NULL",
        )
        batch_op.create_check_constraint("ck_product_workflows_schema_version", "schema_version IN (1, 2)")
        batch_op.create_check_constraint("ck_product_workflows_positive_revision", "revision > 0")
    op.create_index(
        "uq_product_workflows_product_v2_revision",
        "product_workflows",
        ["product_id", "revision"],
        unique=True,
        postgresql_where=sa.text("schema_version = 2"),
        sqlite_where=sa.text("schema_version = 2"),
    )

    op.create_table(
        "workflow_folders",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("workflow_id", sa.String(length=36), nullable=False),
        sa.Column("folder_key", sa.String(length=80), nullable=False),
        sa.Column("title", sa.String(length=255), nullable=False),
        sa.Column("sort_order", sa.Integer(), nullable=False),
        sa.Column("position_x", sa.Integer(), nullable=False),
        sa.Column("position_y", sa.Integer(), nullable=False),
        sa.Column("width", sa.Integer(), nullable=False),
        sa.Column("height", sa.Integer(), nullable=False),
        sa.Column("config_json", sa.JSON(), nullable=False),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), nullable=False),
        sa.CheckConstraint("sort_order >= 0", name="ck_workflow_folders_non_negative_order"),
        sa.CheckConstraint("width > 0 AND height > 0", name="ck_workflow_folders_positive_size"),
        sa.ForeignKeyConstraint(
            ["workflow_id"],
            ["product_workflows.id"],
            name="fk_workflow_folders_workflow_id",
            ondelete="CASCADE",
        ),
        sa.PrimaryKeyConstraint("id"),
        sa.UniqueConstraint("workflow_id", "folder_key", name="uq_workflow_folders_workflow_key"),
    )

    with _alter_existing_table("workflow_nodes") as batch_op:
        batch_op.add_column(sa.Column("schema_version", sa.Integer(), nullable=False, server_default=sa.text("1")))
        batch_op.add_column(sa.Column("node_key", sa.String(length=80), nullable=True))
        batch_op.add_column(sa.Column("folder_id", sa.String(length=36), nullable=True))
        batch_op.add_column(sa.Column("bound_image_asset_id", sa.String(length=36), nullable=True))
        batch_op.create_foreign_key(
            NODE_FOLDER_FK,
            "workflow_folders",
            ["folder_id"],
            ["id"],
            ondelete="SET NULL",
        )
        batch_op.create_foreign_key(
            NODE_BOUND_ASSET_FK,
            "product_image_assets",
            ["bound_image_asset_id"],
            ["id"],
            ondelete="RESTRICT",
        )
        batch_op.create_unique_constraint("uq_workflow_nodes_workflow_key", ["workflow_id", "node_key"])
        batch_op.create_check_constraint("ck_workflow_nodes_schema_version", "schema_version IN (1, 2)")

    with _alter_existing_table("workflow_edges") as batch_op:
        batch_op.add_column(sa.Column("edge_key", sa.String(length=80), nullable=True))
        batch_op.create_unique_constraint("uq_workflow_edges_workflow_key", ["workflow_id", "edge_key"])


def _create_materialization_tables() -> None:
    op.create_table(
        "workflow_materializations",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("product_id", sa.String(length=36), nullable=False),
        sa.Column("draft_id", sa.String(length=36), nullable=False),
        sa.Column("draft_revision_id", sa.String(length=36), nullable=False),
        sa.Column("workflow_id", sa.String(length=36), nullable=False),
        sa.Column("idempotency_key", sa.String(length=120), nullable=False),
        sa.Column("request_hash", sa.String(length=64), nullable=False),
        sa.Column("expected_draft_version", sa.Integer(), nullable=False),
        sa.Column("expected_workflow_revision", sa.Integer(), nullable=False),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.CheckConstraint("length(request_hash) = 64", name="ck_workflow_materializations_request_hash"),
        sa.CheckConstraint(
            "expected_draft_version > 0 AND expected_workflow_revision >= 0",
            name="ck_workflow_materializations_expected_versions",
        ),
        sa.ForeignKeyConstraint(
            ["product_id"],
            ["products.id"],
            name="fk_workflow_materializations_product_id",
            ondelete="CASCADE",
        ),
        sa.ForeignKeyConstraint(
            ["draft_id"],
            ["workflow_drafts.id"],
            name="fk_workflow_materializations_draft_id",
            ondelete="CASCADE",
        ),
        sa.ForeignKeyConstraint(
            ["draft_revision_id"],
            ["workflow_draft_revisions.id"],
            name="fk_workflow_materializations_draft_revision_id",
            ondelete="CASCADE",
        ),
        sa.ForeignKeyConstraint(
            ["workflow_id"],
            ["product_workflows.id"],
            name="fk_workflow_materializations_workflow_id",
            ondelete="CASCADE",
        ),
        sa.PrimaryKeyConstraint("id"),
        sa.UniqueConstraint(
            "product_id",
            "idempotency_key",
            name="uq_workflow_materializations_product_idempotency",
        ),
        sa.UniqueConstraint("draft_revision_id", name="uq_workflow_materializations_draft_revision_id"),
        sa.UniqueConstraint("workflow_id", name="uq_workflow_materializations_workflow_id"),
    )

    op.create_table(
        "workflow_materialization_keys",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("product_id", sa.String(length=36), nullable=False),
        sa.Column("materialization_id", sa.String(length=36), nullable=False),
        sa.Column("idempotency_key", sa.String(length=120), nullable=False),
        sa.Column("request_hash", sa.String(length=64), nullable=False),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.CheckConstraint("length(request_hash) = 64", name="ck_workflow_materialization_keys_request_hash"),
        sa.ForeignKeyConstraint(
            ["product_id"],
            ["products.id"],
            name="fk_workflow_materialization_keys_product_id",
            ondelete="CASCADE",
        ),
        sa.ForeignKeyConstraint(
            ["materialization_id"],
            ["workflow_materializations.id"],
            name="fk_workflow_materialization_keys_materialization_id",
            ondelete="CASCADE",
        ),
        sa.PrimaryKeyConstraint("id"),
        sa.UniqueConstraint(
            "product_id",
            "idempotency_key",
            name="uq_workflow_materialization_keys_product_key",
        ),
    )

    op.create_table(
        "workflow_reveal_events",
        sa.Column("id", sa.String(length=36), nullable=False),
        sa.Column("materialization_id", sa.String(length=36), nullable=False),
        sa.Column("sequence", sa.Integer(), nullable=False),
        sa.Column("kind", WORKFLOW_REVEAL_EVENT_KIND, nullable=False),
        sa.Column("entity_type", sa.String(length=40), nullable=True),
        sa.Column("entity_id", sa.String(length=36), nullable=True),
        sa.Column("payload_json", sa.JSON(), nullable=False),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.CheckConstraint("sequence > 0", name="ck_workflow_reveal_events_positive_sequence"),
        sa.ForeignKeyConstraint(
            ["materialization_id"],
            ["workflow_materializations.id"],
            name="fk_workflow_reveal_events_materialization_id",
            ondelete="CASCADE",
        ),
        sa.PrimaryKeyConstraint("id"),
        sa.UniqueConstraint(
            "materialization_id",
            "sequence",
            name="uq_workflow_reveal_events_materialization_sequence",
        ),
    )


def upgrade() -> None:
    _create_enums()
    _create_draft_tables()
    _extend_workflow_tables()
    _create_materialization_tables()


def downgrade() -> None:
    op.drop_table("workflow_reveal_events")
    op.drop_table("workflow_materialization_keys")
    op.drop_table("workflow_materializations")

    with _alter_existing_table("workflow_edges") as batch_op:
        batch_op.drop_constraint("uq_workflow_edges_workflow_key", type_="unique")
        batch_op.drop_column("edge_key")
    with _alter_existing_table("workflow_nodes") as batch_op:
        batch_op.drop_constraint("ck_workflow_nodes_schema_version", type_="check")
        batch_op.drop_constraint("uq_workflow_nodes_workflow_key", type_="unique")
        batch_op.drop_constraint(NODE_BOUND_ASSET_FK, type_="foreignkey")
        batch_op.drop_constraint(NODE_FOLDER_FK, type_="foreignkey")
        batch_op.drop_column("bound_image_asset_id")
        batch_op.drop_column("folder_id")
        batch_op.drop_column("node_key")
        batch_op.drop_column("schema_version")
    op.drop_table("workflow_folders")

    op.drop_index("uq_product_workflows_product_v2_revision", table_name="product_workflows")
    with _alter_existing_table("product_workflows") as batch_op:
        batch_op.drop_constraint("ck_product_workflows_positive_revision", type_="check")
        batch_op.drop_constraint("ck_product_workflows_schema_version", type_="check")
        batch_op.drop_constraint(WORKFLOW_SOURCE_REVISION_FK, type_="foreignkey")
        batch_op.drop_column("source_draft_revision_id")
        batch_op.drop_column("revision")
        batch_op.drop_column("schema_version")

    with _alter_existing_table("products") as batch_op:
        batch_op.drop_constraint(PRODUCT_CURRENT_FACT_FK, type_="foreignkey")
        batch_op.drop_column("current_fact_set_version_id")
    op.drop_table("product_fact_set_versions")

    with _alter_existing_table("workflow_drafts") as batch_op:
        batch_op.drop_constraint(DRAFT_CURRENT_REVISION_FK, type_="foreignkey")
    op.drop_table("workflow_draft_revisions")
    op.drop_index("ix_workflow_drafts_product_status", table_name="workflow_drafts")
    op.drop_table("workflow_drafts")

    bind = op.get_bind()
    WORKFLOW_REVEAL_EVENT_KIND.drop(bind, checkfirst=True)
    WORKFLOW_DRAFT_STATUS.drop(bind, checkfirst=True)
    # PostgreSQL enum values cannot be removed safely. prompt_generation remains unused after downgrade.
